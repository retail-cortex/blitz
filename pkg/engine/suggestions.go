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
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// The welcome screen's suggestions: tiles from the workspace's state, which
// cost nothing, and ideas a model writes from the recent conversations,
// kept in a cache and written again in the background only when those
// conversations change.

// harnessFiles are the files that give a workspace agent instructions;
// without any, setup (/setup) is suggested.
var harnessFiles = []string{".agents/AGENT.md", "AGENTS.md", "CLAUDE.md", "GEMINI.md", "BLITZ.md"}

const (
	// ideaSessions is how many recent conversations the ideas come from.
	ideaSessions = 6
	// maxIdeas is how many ideas are kept.
	maxIdeas = 3
	// ideaRetry is how long a failed attempt waits before the next.
	ideaRetry = time.Hour
	// ideaTimeout bounds writing the ideas.
	ideaTimeout = 90 * time.Second
)

// HarnessMissing reports whether the workspace has no agent instructions:
// none of .agents/AGENT.md, AGENTS.md, CLAUDE.md, GEMINI.md or BLITZ.md.
func (w *Workspace) HarnessMissing() bool {
	for _, f := range harnessFiles {
		if info, err := os.Stat(filepath.Join(w.Dir(), filepath.FromSlash(f))); err == nil && info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// Suggestions returns the workspace's welcome tiles: setup when it has no
// agent instructions, the latest conversation, uncommitted changes and a
// failed worker, then up to three ideas from the recent conversations. When
// those changed since the ideas were written, new ones are written in the
// background (Pending) and the earlier ones are returned meanwhile.
func (w *Workspace) Suggestions(ctx context.Context) api.Suggestions {
	out := api.Suggestions{HarnessMissing: w.HarnessMissing()}
	if out.HarnessMissing {
		out.Tiles = append(out.Tiles, api.Suggestion{Kind: api.SuggestSetup})
	}
	recent := w.recentSessions()
	if len(recent) > 0 {
		out.Tiles = append(out.Tiles, api.Suggestion{Kind: api.SuggestContinue, SessionID: recent[0].ID, Title: recent[0].Title})
	}
	if st, err := w.GitStatus(ctx); err == nil && st.Changed > 0 {
		out.Tiles = append(out.Tiles, api.Suggestion{Kind: api.SuggestChanges, Count: st.Changed})
	}
	if list, err := w.ListWorkers(); err == nil {
		for _, wk := range list {
			if r := wk.LastRun; r != nil && (r.Status == api.RunFailed || r.Status == api.RunLimited) {
				out.Tiles = append(out.Tiles, api.Suggestion{Kind: api.SuggestWorkerFailed, Worker: wk.Name, Detail: textutil.Ellipsize(r.Error, 300)})
				break // one is enough to start from
			}
		}
	}
	if !w.cfg.Suggestions.Ideas || len(recent) == 0 {
		return out
	}
	c := w.readIdeas()
	for _, idea := range c.Ideas {
		out.Tiles = append(out.Tiles, api.Suggestion{Kind: api.SuggestIdea, Title: idea.Title, Prompt: idea.Prompt})
	}
	stamp := sessionsStamp(recent)
	if c.Stamp != stamp && (c.FailedStamp != stamp || time.Since(c.Failed) > ideaRetry) {
		out.Pending = w.writeIdeas(recent, stamp)
	}
	return out
}

// recentSessions are the workspace's latest conversations with messages,
// newest first: not snapshots, not workers' runs, not the empty chat the
// welcome screen shows in.
func (w *Workspace) recentSessions() []*session.SessionRecord {
	list, err := w.storage.ListWorkspace(w.Dir())
	if err != nil {
		return nil
	}
	var out []*session.SessionRecord
	for _, s := range list {
		if s.MessageCount > 0 && s.Name == "" && !strings.HasPrefix(s.Title, "⏰") {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b *session.SessionRecord) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	if len(out) > ideaSessions {
		out = out[:ideaSessions]
	}
	return out
}

// sessionsStamp identifies the recent conversations as they are: any new
// message changes it.
func sessionsStamp(recent []*session.SessionRecord) string {
	h := sha256.New()
	for _, s := range recent {
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", s.ID, s.MessageCount, s.UpdatedAt.UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// idea is one model-written tile.
type idea struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

// ideaCache is what's kept per workspace: the ideas, the conversations
// they came from, and the last attempt that failed.
type ideaCache struct {
	Stamp       string    `json:"stamp"`
	Ideas       []idea    `json:"ideas"`
	Written     time.Time `json:"written"`
	FailedStamp string    `json:"failed_stamp,omitempty"`
	Failed      time.Time `json:"failed,omitzero"`
}

// ideasPath is the workspace's cache, beside the sessions.
func (w *Workspace) ideasPath() string {
	sum := sha256.Sum256([]byte(w.Dir()))
	return filepath.Join(config.ExpandHome(w.cfg.Session.StorageDir), "suggestions", hex.EncodeToString(sum[:8])+".json")
}

func (w *Workspace) readIdeas() ideaCache {
	var c ideaCache
	if data, err := os.ReadFile(w.ideasPath()); err == nil {
		_ = json.Unmarshal(data, &c) // a damaged cache is an empty one
	}
	return c
}

func (w *Workspace) saveIdeas(c ideaCache) {
	path := w.ideasPath()
	data, _ := json.MarshalIndent(c, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		err = writeFileAtomic(filepath.Dir(path), path, data)
		if err != nil {
			slog.Warn("saving suggestions", "error", err)
		}
	}
}

// writeIdeas starts writing ideas from recent in the background, unless
// that's already happening, and reports that ideas are on their way. It
// stops when the workspace closes.
func (w *Workspace) writeIdeas(recent []*session.SessionRecord, stamp string) bool {
	w.ideasMu.Lock()
	defer w.ideasMu.Unlock()
	if w.ideasRunning {
		return true
	}
	if w.bgCtx == nil || w.bgCtx.Err() != nil {
		return false
	}
	w.ideasRunning = true
	w.bg.Add(1)
	go func() {
		defer w.bg.Done()
		defer func() {
			w.ideasMu.Lock()
			w.ideasRunning = false
			w.ideasMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(w.bgCtx, ideaTimeout)
		defer cancel()
		ideas, err := w.askIdeas(ctx, recent)
		c := w.readIdeas()
		if err != nil {
			slog.Warn("writing suggestions", "workspace", w.Dir(), "error", err)
			c.FailedStamp, c.Failed = stamp, time.Now()
		} else {
			c = ideaCache{Stamp: stamp, Ideas: ideas, Written: time.Now()}
		}
		if w.bgCtx.Err() == nil {
			w.saveIdeas(c)
		}
	}()
	return true
}

// ideasInstruction asks for the ideas, as JSON.
const ideasInstruction = `You suggest what a developer will most likely want to ask their coding agent next in this workspace, from their recent conversations with it. Prefer continuing unfinished work, follow-ups the agent's replies proposed or left open, and checking recent changes; never suggest what was already done.

Reply with JSON only: an array of at most 3 objects {"title": "...", "prompt": "..."}.
- title: what the tile shows, imperative and specific, at most 70 characters.
- prompt: the request sent to the agent when it's chosen, self-contained (it starts a new conversation), at most 400 characters.
No Markdown, no commentary.`

// askIdeas has the suggestions model write ideas from recent.
func (w *Workspace) askIdeas(ctx context.Context, recent []*session.SessionRecord) ([]idea, error) {
	ref := cmp.Or(w.cfg.Suggestions.Model, w.cfg.Permissions.Auto.Model)
	llm, err := w.newModel(ctx, w.cfg, ref)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("Recent conversations in this workspace, newest first:\n")
	for _, rec := range recent {
		full, err := w.storage.Load(rec.ID)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\n## %s (%s, %d messages)\n", cmp.Or(full.Title, "untitled"), full.UpdatedAt.Format("2006-01-02"), full.MessageCount)
		var prompts []string
		lastReply := ""
		for _, m := range full.Messages {
			switch {
			case m.Role == "user" && m.Kind == "":
				prompts = append(prompts, m.Content)
			case m.Role == "model":
				lastReply = m.Content
			}
		}
		if len(prompts) > 0 {
			fmt.Fprintf(&b, "First request: %s\n", textutil.Ellipsize(prompts[0], 400))
			if len(prompts) > 1 {
				fmt.Fprintf(&b, "Latest request: %s\n", textutil.Ellipsize(prompts[len(prompts)-1], 400))
			}
		}
		if lastReply != "" {
			fmt.Fprintf(&b, "End of the agent's latest reply: %s\n", tail(lastReply, 600))
		}
	}
	if st, err := w.GitStatus(ctx); err == nil && st.Repo {
		fmt.Fprintf(&b, "\nGit: branch %s, %d uncommitted files.\n", st.Branch, st.Changed)
	}
	text, served, usage, err := runtime.Ask(ctx, llm, ideasInstruction+i18n.ReplyInstruction(w.reply), b.String())
	if err != nil {
		return nil, err
	}
	if usage != nil {
		slog.Info("suggestions written", "workspace", w.Dir(), "model", served, "input_tokens", usage.PromptTokenCount, "output_tokens", usage.CandidatesTokenCount)
	}
	return parseIdeas(text)
}

// parseIdeas reads the model's JSON array, fenced or not, keeping up to
// maxIdeas usable ideas.
func parseIdeas(text string) ([]idea, error) {
	start, end := strings.Index(text, "["), strings.LastIndex(text, "]")
	if start < 0 || end < start {
		return nil, fmt.Errorf("no JSON array in the reply: %q", textutil.Ellipsize(text, 200))
	}
	var raw []idea
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return nil, fmt.Errorf("the reply's JSON: %w", err)
	}
	var out []idea
	for _, i := range raw {
		i.Title, i.Prompt = strings.TrimSpace(i.Title), strings.TrimSpace(i.Prompt)
		if i.Title == "" || i.Prompt == "" {
			continue
		}
		i.Title, i.Prompt = textutil.Ellipsize(i.Title, 90), textutil.Ellipsize(i.Prompt, 600)
		if out = append(out, i); len(out) == maxIdeas {
			break
		}
	}
	return out, nil
}

// tail is the end of s, at most n bytes, on a rune boundary.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return "…" + s[i:]
}
