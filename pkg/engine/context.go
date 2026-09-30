// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/images"
)

// What the model sees: usage and context size, compaction, project memory,
// the reply language, images, and searches whose results go to the agent.

func (w *Workspace) activeID() (string, error) {
	r := w.storage.Active()
	if r == nil {
		return "", api.ErrNoActiveSession
	}
	return r.ID, nil
}

// SessionUsage returns the active session's usage.
func (w *Workspace) SessionUsage() (api.Usage, error) {
	id, err := w.activeID()
	if err != nil {
		return api.Usage{}, err
	}
	return w.engine.Usage(id), nil
}

// Context returns the active session's context size and compaction
// setting, with its breakdown by category when parts is set (it reads the
// session's events).
func (w *Workspace) Context(parts bool) (api.ContextInfo, error) {
	u, err := w.SessionUsage()
	if err != nil {
		return api.ContextInfo{}, err
	}
	c := w.cfg.Context
	info := api.ContextInfo{Tokens: u.LastPrompt, AutoCompact: c.Compaction && c.TokenThreshold > 0, Threshold: c.TokenThreshold, Keep: c.RetainEvents}
	if parts {
		id, _ := w.activeID()
		info.Parts = w.engine.ContextParts(context.Background(), id, u.LastPrompt)
	}
	return info, nil
}

// Compact summarises the active session's older turns, keeping the latest,
// to shrink the model's context. focus steers what the summary keeps.
func (w *Workspace) Compact(ctx context.Context, focus string) (api.CompactResult, error) {
	id, err := w.activeID()
	if err != nil {
		return api.CompactResult{}, err
	}
	before := w.engine.Usage(id)
	res, err := w.engine.Compact(ctx, id, focus, 1)
	if err != nil {
		return api.CompactResult{}, err
	}
	return api.CompactResult{EventsCompacted: res.EventsCompacted, SummaryChars: res.SummaryChars, Before: before, After: w.engine.Usage(id)}, nil
}

// MemoryFiles are the project instruction file names looked for, in order.
func (w *Workspace) MemoryFiles() []string { return w.cfg.Memory.Files }

// ReloadMemory re-reads project instruction files into the engine and
// returns their paths.
func (w *Workspace) ReloadMemory(ctx context.Context) ([]string, error) {
	w.loadMemory()
	w.engine.SetScopedRules(w.memory.Rules)
	if err := w.engine.SetInstructions(ctx, w.instructions()); err != nil {
		return nil, err
	}
	var paths []string
	for _, d := range w.memory.Docs {
		paths = append(paths, d.Path)
	}
	for _, r := range w.memory.Rules {
		paths = append(paths, r.Path+" ("+strings.Join(r.Paths, ", ")+")")
	}
	return paths, nil
}

// ListHooks are the configured hooks, with their sources and recent
// failures (spec_parity_027 PAR-HK-13).
func (w *Workspace) ListHooks() []api.HookInfo { return w.tools.ScriptHooks().List() }

// ListNotes are the notes the agent saved in this workspace with its
// remember tool, newest first (spec_parity_027 PAR-MEM-11).
func (w *Workspace) ListNotes() ([]api.Note, error) {
	notes, err := memory.Notes(memory.NotesDir(w.Dir()))
	if err != nil {
		return nil, err
	}
	out := make([]api.Note, len(notes))
	for i, n := range notes {
		out[i] = api.Note{Name: n.Name, Kind: n.Kind, Text: n.Text, Time: n.Time, Path: n.Path}
	}
	return out, nil
}

// ForgetNote deletes the note name (or the only one whose name starts so);
// the agent's instructions lose it at the next reload or session.
func (w *Workspace) ForgetNote(name string) error {
	err := memory.DeleteNote(memory.NotesDir(w.Dir()), name)
	if errors.Is(err, memory.ErrNoNote) {
		return fmt.Errorf("%w: %s", api.ErrNoNote, name)
	}
	return err
}

// AddMemory appends text to the last project instruction file (BLITZ.md
// when none is configured), reloads memory, and returns the file's path.
func (w *Workspace) AddMemory(ctx context.Context, text string) (string, error) {
	file := "BLITZ.md"
	if files := w.cfg.Memory.Files; len(files) > 0 {
		file = files[len(files)-1]
	}
	p, err := memory.Append(w.Dir(), file, text)
	if err != nil {
		return "", err
	}
	_, err = w.ReloadMemory(ctx)
	return p, err
}

// AvailableLocales returns the interface languages with a catalog, and
// the directory custom catalogs are read from ("" if none is configured).
func (w *Workspace) AvailableLocales() (list []api.LocaleInfo, customDir string) {
	for _, m := range w.locales.Available() {
		list = append(list, api.LocaleInfo{Tag: m.Locale, Name: m.Name})
	}
	return list, w.cfg.UI.LocalesDir
}

// SetLocale switches the language the model replies in, for this
// workspace, and saves it as ui.locale in the config file. input is a
// language tag or name. The interface language belongs to the client: a
// front end switches its own (i18n.SetCurrent) with the returned Tag.
func (w *Workspace) SetLocale(ctx context.Context, input string) (api.LocaleChange, error) {
	tag, err := w.locales.Resolve(input)
	if err != nil {
		return api.LocaleChange{}, api.ErrUnknownLocale
	}
	l := w.locales.Localizer(tag)
	w.reply = l
	out := api.LocaleChange{Tag: tag.String(), NativeName: l.NativeName(), LanguageName: l.LanguageName(), HasCatalog: l.HasCatalog()}
	if err := w.engine.SetInstructions(ctx, w.instructions()); err != nil {
		out.Saved.Err = err
		return out, nil
	}
	w.cfg.UI.Locale = out.Tag
	out.Saved.Path, out.Saved.Err = config.SaveUILocale(config.ConfigDir(""), w.cfg.UI.Locale)
	return out, nil
}

// SandboxSummary describes the sandbox commands run in, one line each.
func (w *Workspace) SandboxSummary() []string { return w.tools.SandboxSummary() }

// LoadImage reads an image from the workspace for a prompt.
func (w *Workspace) LoadImage(path string) (*images.Image, error) { return w.tools.LoadImage(path) }

// AddImage stores image data (e.g. pasted from the clipboard) for a prompt.
func (w *Workspace) AddImage(name string, data []byte) (*images.Image, error) {
	if w.tools.Images() == nil {
		return nil, api.ErrImagesDisabled
	}
	return w.tools.AddImage(name, data)
}

// Search limits: how many results go to the agent, how many are asked for
// (room to drop ones it couldn't read), and how many transcript passages a
// session search sends.
const (
	searchLinks     = 5
	searchFetch     = 10
	sessionPassages = 12
)

// SearchProvider names the web search provider, or ErrNoFetch.
func (w *Workspace) SearchProvider() (string, error) {
	if !w.tools.CanFetch() {
		return "", api.ErrNoFetch
	}
	return w.tools.SearchProvider(), nil
}

// SearchWeb searches the web for terms and prepares the agent's prompt from
// the results it can read.
func (w *Workspace) SearchWeb(ctx context.Context, terms string) (api.WebSearch, error) {
	if !w.tools.CanFetch() {
		return api.WebSearch{}, api.ErrNoFetch
	}
	if id, err := w.activeID(); err == nil {
		ctx = tools.WithOwnerSession(ctx, id) // its queries count for the session
	}
	out, err := w.tools.WebSearch(ctx, terms, searchFetch)
	if err != nil {
		return api.WebSearch{}, err
	}
	links := viableLinks(out.Results, searchLinks)
	if len(links) == 0 {
		return api.WebSearch{}, nil
	}
	res := api.WebSearch{Prompt: runtime.WebSearchPrompt(terms, links, out.Answer)}
	for _, r := range links {
		res.Links = append(res.Links, api.Link{Title: r.Title, URL: r.URL})
	}
	return res, nil
}

// SearchSession looks for terms in the active session's transcript (which
// keeps what compaction removed from the model's context) and returns how
// many passages matched and the prompt that asks the agent about them.
func (w *Workspace) SearchSession(terms string) (found int, prompt string) {
	var msgs []session.Message
	if r := w.storage.Active(); r != nil {
		msgs = r.Messages
	}
	matches, total := session.Search(msgs, terms, sessionPassages)
	return total, runtime.SessionSearchPrompt(terms, matches, total)
}

// unreadable are extensions web_fetch can't turn into text.
var unreadable = map[string]bool{
	".pdf": true, ".zip": true, ".gz": true, ".tgz": true, ".tar": true, ".exe": true, ".dmg": true, ".iso": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true,
	".mp3": true, ".mp4": true, ".mov": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
}

// viableLinks keeps up to n results the agent can read: http(s), not a
// binary or document download, not an unresolved Google redirect (web_fetch
// won't follow it to another host), and not a duplicate. Search already
// removed denied domains.
func viableLinks(results []tools.SearchResult, n int) []tools.SearchResult {
	seen := map[string]bool{}
	var out []tools.SearchResult
	for _, r := range results {
		u, err := url.Parse(r.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			unreadable[strings.ToLower(path.Ext(u.Path))] || strings.HasPrefix(u.Path, "/grounding-api-redirect/") {
			continue
		}
		u.Fragment = ""
		if key := strings.ToLower(u.Host) + u.RequestURI(); !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
		if len(out) == n {
			break
		}
	}
	return out
}
