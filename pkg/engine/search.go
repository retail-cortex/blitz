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
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/search"
	"github.com/retail-cortex/blitz/pkg/engine/table"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/pdftext"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"google.golang.org/adk/v2/model"
)

// searchInterval is how often an idle workspace's index is brought up to
// date; a turn's end does it too.
var searchInterval = time.Minute

// newEmbedder builds search.embedding_model's embedder; tests replace it.
var newEmbedder = func(ctx context.Context, cfg *config.Config, ref string) (search.Embedder, error) {
	return runtime.NewEmbedder(ctx, cfg, ref)
}

// maxDocumentText bounds the text indexed for one PDF.
const maxDocumentText = 2_000_000

// searchState is a workspace's open index and what keeps it current: the
// settings it was opened with, its own background context, and what kept
// it from being all they ask (an embedding model that couldn't be built).
type searchState struct {
	settings config.SearchConfig
	index    *search.Index
	root     *os.Root
	kick     chan struct{}
	stop     context.CancelFunc
	done     chan struct{}
	problems []string
}

// searchNow is the workspace's search state (nil: off).
func (w *Workspace) searchNow() *searchState {
	w.searchMu.Lock()
	defer w.searchMu.Unlock()
	return w.search
}

// openSearch opens the workspace's search index for its settings and keeps
// it up to date in the background until it's closed. Off, or a failure,
// leaves search off (with a warning, kept for the status).
func (w *Workspace) openSearch(ctx context.Context) {
	sc := w.cfg.Search
	w.searchMu.Lock()
	w.searchSettings, w.searchProblem = sc, ""
	w.searchMu.Unlock()
	if !sc.Enabled {
		return
	}
	dir := config.WorkspaceSettingsDir(w.cfg.Dir, w.Dir())
	if dir == "" {
		return
	}
	fail := func(err error) {
		msg := i18n.T("search.open_failed", "error", err)
		w.warn(msg)
		w.searchMu.Lock()
		w.searchProblem = msg
		w.searchMu.Unlock()
	}
	root, err := os.OpenRoot(w.Dir())
	if err != nil {
		fail(err)
		return
	}
	st := &searchState{settings: sc, root: root, kick: make(chan struct{}, 1), done: make(chan struct{})}
	sources := []search.Source{
		fileSource{w: w, root: root},
		documentSource{w: w, root: root},
		chatSource{w: w},
		noteSource{w: w, root: root},
	}
	var opts []search.Option
	if ref := sc.EmbeddingModel; ref != "" {
		if e, err := newEmbedder(ctx, w.cfg, ref); err != nil {
			st.problems = append(st.problems, i18n.T("search.embed_failed", "model", ref, "error", err))
		} else {
			opts = append(opts, search.WithEmbedder(e))
		}
	}
	if sc.Enrich {
		ref := cmp.Or(sc.EnrichModel, w.cfg.Suggestions.Model, w.cfg.Permissions.Auto.Model)
		if llm, err := w.newModel(ctx, w.cfg, ref); err != nil {
			st.problems = append(st.problems, i18n.T("search.enrich_failed", "error", err))
		} else {
			opts = append(opts, search.WithEnricher(describer{llm: llm, redact: SecretRedactor(w.cfg)}, sc.EnrichDailyLimit, w.Busy))
		}
	}
	for _, p := range st.problems {
		w.warn(p)
	}
	st.index, err = search.Open(filepath.Join(dir, "search"), sources, w.searchAllowed, opts...)
	if err != nil {
		root.Close()
		fail(err)
		return
	}
	var sctx context.Context
	sctx, st.stop = context.WithCancel(w.bgCtx)
	w.searchMu.Lock()
	w.search = st
	w.searchProblem = strings.Join(st.problems, "\n")
	w.searchMu.Unlock()
	w.bg.Add(1)
	go w.keepIndexed(sctx, st)
}

// keepIndexed scans at once, then after each kick (a turn ended, a
// Reindex) and searchInterval after the last scan ended while no turn
// runs, so a scan that takes longer than the interval doesn't run back to
// back; until ctx ends.
func (w *Workspace) keepIndexed(ctx context.Context, st *searchState) {
	defer w.bg.Done()
	defer close(st.done)
	st.index.Scan(ctx)
	idle := time.NewTimer(searchInterval)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-idle.C:
			if !w.Busy() {
				st.index.Scan(ctx)
			}
		case <-st.kick:
			st.index.Scan(ctx)
		}
		idle.Reset(searchInterval)
	}
}

// kickSearch asks for a scan soon (one is enough however many ask).
func (w *Workspace) kickSearch() {
	st := w.searchNow()
	if st == nil {
		return
	}
	select {
	case st.kick <- struct{}{}:
	default:
	}
}

// closeSearch stops the index's background work and closes it.
func (w *Workspace) closeSearch() {
	w.searchMu.Lock()
	st := w.search
	w.search = nil
	w.searchMu.Unlock()
	if st == nil {
		return
	}
	st.stop()
	<-st.done
	st.index.Close()
	st.root.Close()
}

// ReloadSearch applies cfg's [search] settings while the workspace is open:
// changed, the index is closed and opened again with them (an embedding
// model or the describer started or stopped); the same, nothing happens.
func (w *Workspace) ReloadSearch(cfg *config.Config) {
	w.searchMu.Lock()
	same := reflect.DeepEqual(w.searchSettings, cfg.Search)
	w.searchMu.Unlock()
	if same {
		return
	}
	w.closeSearch()
	w.cfg.Search = cfg.Search
	w.openSearch(w.bgCtx)
}

// searchAllowed keeps blocked paths (deny read rules, .env and the like)
// out of the index and the results.
func (w *Workspace) searchAllowed(source, ref string) bool {
	switch source {
	case api.SearchFiles, api.SearchDocuments:
		return w.tools.Workspace().AgentRule(ref) != "blocked"
	case api.SearchNotes:
		if strings.HasPrefix(ref, plansDir+"/") {
			return w.tools.Workspace().AgentRule(ref) != "blocked"
		}
	}
	return true
}

// Search searches the workspace's index (spec_search_035).
func (w *Workspace) Search(ctx context.Context, q api.SearchQuery) (api.SearchResult, error) {
	st := w.searchNow()
	if st == nil {
		return api.SearchResult{}, api.ErrSearchDisabled
	}
	sources := q.Sources
	if len(sources) == 0 {
		sources = st.settings.Sources
	}
	for _, s := range sources {
		if !slices.Contains(api.SearchSources, s) {
			return api.SearchResult{}, fmt.Errorf("%w: %q", api.ErrUnknownSearchSource, s)
		}
	}
	hits, err := st.index.Search(ctx, search.Query{Text: q.Text, Sources: sources, Limit: q.Limit, Mode: q.Mode, Hidden: q.Hidden})
	switch {
	case errors.Is(err, search.ErrNoTerms):
		return api.SearchResult{}, api.ErrEmptySearch
	case errors.Is(err, search.ErrNoEmbeddings):
		return api.SearchResult{}, api.ErrNoEmbeddings
	case errors.Is(err, search.ErrUnknownMode):
		return api.SearchResult{}, fmt.Errorf("%w: %q", api.ErrUnknownSearchMode, q.Mode)
	case err != nil:
		return api.SearchResult{}, err
	}
	out := api.SearchResult{Sources: sources, Hits: make([]api.SearchHit, len(hits))}
	for i, h := range hits {
		out.Hits[i] = api.SearchHit(h)
	}
	return out, nil
}

// SearchStatus is how the workspace's index is, with the settings it
// follows (even off) and what kept them from applying in full.
func (w *Workspace) SearchStatus() api.SearchStatus {
	w.searchMu.Lock()
	st, sc, problem := w.search, w.searchSettings, w.searchProblem
	w.searchMu.Unlock()
	out := api.SearchStatus{Problem: problem, Settings: api.SearchSettings{
		Enabled: sc.Enabled, Sources: slices.Clone(sc.Sources), EmbeddingModel: sc.EmbeddingModel,
		Enrich: sc.Enrich, EnrichModel: sc.EnrichModel, EnrichDailyLimit: sc.EnrichDailyLimit, IncludeIgnored: sc.IncludeIgnored,
	}}
	if st == nil {
		return out
	}
	s := st.index.Status()
	out.Enabled, out.Items, out.Scanning, out.LastScan, out.Unreadable, out.Error = true, s.Items, s.Scanning, s.LastScan, s.Unreadable, s.Err
	out.EmbeddingModel, out.Embedded, out.EmbedError, out.Enriched, out.EnrichError = s.EmbeddingModel, s.Embedded, s.EmbedErr, s.Enriched, s.EnrichErr
	return out
}

// Reindex starts a scan for search and returns at once.
func (w *Workspace) Reindex() error {
	if w.searchNow() == nil {
		return api.ErrSearchDisabled
	}
	w.kickSearch()
	return nil
}

// isDocument reports whether a file is read as a document (its text
// extracted) rather than as text.
func isDocument(p string) bool {
	return pdftext.IsPDFPath(p) || strings.EqualFold(path.Ext(p), ".ipynb")
}

// searchItems are the workspace's files that keep (by isDocument), with
// their size and time, dotfiles marked hidden; with search.include_ignored,
// the files git ignores too, hidden. What can't be stat'ed through root is
// left out.
func searchItems(ctx context.Context, w *Workspace, root *os.Root, keep func(string) bool) ([]search.Item, error) {
	paths, err := w.listFiles(ctx, true)
	if err != nil {
		return nil, err
	}
	hidden := map[string]bool{}
	for _, p := range paths {
		if dotted(p) {
			hidden[p] = true
		}
	}
	if st := w.searchNow(); st != nil && st.settings.IncludeIgnored {
		for _, p := range w.ignoredFiles(ctx) {
			paths, hidden[p] = append(paths, p), true
		}
	}
	var out []search.Item
	for _, p := range paths {
		if !keep(p) {
			continue
		}
		info, err := root.Stat(filepath.FromSlash(p)) // a link out of the workspace fails
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, search.Item{Ref: p, Size: info.Size(), Modified: info.ModTime(), Hidden: hidden[p]})
	}
	return out, nil
}

// dotted reports whether a path is, or is in, a dot folder or dotfile.
func dotted(p string) bool {
	return slices.ContainsFunc(strings.Split(p, "/"), func(seg string) bool { return strings.HasPrefix(seg, ".") })
}

// clutterDirs are folders of dependencies, builds and caches, left out
// even when search indexes what git ignores.
var clutterDirs = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "build": true, "dist": true, "out": true,
	"__pycache__": true, ".venv": true, "venv": true, "env": true, ".tox": true, ".cache": true,
	".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true, ".gradle": true, ".next": true,
	".nuxt": true, "coverage": true, ".terraform": true, "bazel-out": true,
}

// ignoredFiles are the files git ignores in the workspace (none outside a
// repository), without clutterDirs, .git or the top .blitz, blocked paths
// or links out of it, up to maxWalked.
func (w *Workspace) ignoredFiles(ctx context.Context) []string {
	out, err := w.git(ctx, nil, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return nil
	}
	fsys := w.files()
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" || len(paths) >= maxWalked {
			continue
		}
		segs := strings.Split(p, "/")
		if slices.ContainsFunc(segs[:len(segs)-1], func(s string) bool { return clutterDirs[s] || s == ".git" || strings.HasPrefix(s, "bazel-") }) || segs[0] == ".blitz" {
			continue
		}
		if fsys.AgentRule(filepath.FromSlash(p)) == "blocked" {
			continue
		}
		paths = append(paths, p)
	}
	return paths
}

// fileSource is the workspace's text files: those git doesn't ignore (or
// all outside a repository), read through root so a link can't lead out.
type fileSource struct {
	w    *Workspace
	root *os.Root
}

func (fileSource) Name() string { return api.SearchFiles }

func (s fileSource) List(ctx context.Context) ([]search.Item, error) {
	return searchItems(ctx, s.w, s.root, func(p string) bool { return !isDocument(p) })
}

func (s fileSource) Read(_ context.Context, ref string) (search.Doc, error) {
	if table.IsTable(ref) {
		return readTableHead(s.root, ref)
	}
	data, err := readCapped(s.root, ref, s.w.cfg.Tools.MaxFileSizeBytes)
	if err != nil {
		return search.Doc{}, err
	}
	if isBinary(data) {
		return search.Doc{}, search.ErrSkip
	}
	return search.Doc{Title: ref, Text: string(data)}, nil
}

// tableIndexBytes is how much of a table is indexed: its header and the
// rows that fit, so its columns and their values can be found, however
// large it is.
const tableIndexBytes = 1 << 20

// readTableHead reads a table's first tableIndexBytes, to the last whole
// row, decoded (Latin-1 too); one that isn't text is skipped.
func readTableHead(root *os.Root, ref string) (search.Doc, error) {
	f, err := root.Open(filepath.FromSlash(ref))
	if err != nil {
		return search.Doc{}, err
	}
	defer f.Close()
	head := make([]byte, tableIndexBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return search.Doc{}, err
	}
	head = head[:n]
	if n == tableIndexBytes {
		if i := bytes.LastIndexByte(head, '\n'); i > 0 {
			head = head[:i+1]
		}
	}
	format, err := table.Sniff(ref, head)
	if err != nil {
		return search.Doc{}, search.ErrSkip
	}
	return search.Doc{Title: ref, Text: table.Decode(head, format)}, nil
}

// readCapped reads ref through root; one over max bytes (0: no limit) is
// skipped.
func readCapped(root *os.Root, ref string, max int64) ([]byte, error) {
	name := filepath.FromSlash(ref)
	if max > 0 {
		info, err := root.Stat(name)
		if err != nil {
			return nil, err
		}
		if info.Size() > max {
			return nil, search.ErrSkip
		}
	}
	return root.ReadFile(name)
}

// pageRE finds the page headings pdftext puts in a PDF's text.
var pageRE = regexp.MustCompile(`(?m)^--- Page (\d+) ---$`)

// cellRE finds the cell headings of a notebook's text.
var cellRE = regexp.MustCompile(`(?m)^## Cell (\d+) \[`)

// documentSource is the text of the workspace's PDFs (read in pdftext's
// helper process) and notebooks, with their pages and cells as sections.
type documentSource struct {
	w    *Workspace
	root *os.Root
}

func (documentSource) Name() string { return api.SearchDocuments }

func (s documentSource) List(ctx context.Context) ([]search.Item, error) {
	return searchItems(ctx, s.w, s.root, isDocument)
}

func (s documentSource) Read(ctx context.Context, ref string) (search.Doc, error) {
	data, err := readCapped(s.root, ref, s.w.cfg.Tools.MaxFileSizeBytes)
	if err != nil {
		return search.Doc{}, err
	}
	var text, label string
	var re *regexp.Regexp
	if pdftext.IsPDFPath(ref) {
		text, _, err = pdftext.Text(ctx, data, maxDocumentText)
		re, label = pageRE, i18n.T("search.page")
	} else {
		text, err = tools.NotebookText(data)
		re, label = cellRE, i18n.T("search.cell")
	}
	if err != nil {
		return search.Doc{}, err
	}
	return search.Doc{Title: ref, Text: text, Sections: sections(text, re, label)}, nil
}

// sections are the lines where re matches in text, labelled label and the
// number re captured ("page 3").
func sections(text string, re *regexp.Regexp, label string) []search.Section {
	var out []search.Section
	for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
		line := strings.Count(text[:m[0]], "\n") + 1
		out = append(out, search.Section{Line: line, Label: label + " " + text[m[2]:m[3]]})
	}
	return out
}

// chatSource is the workspace's chats: what was asked and answered, not
// tool output.
type chatSource struct{ w *Workspace }

func (chatSource) Name() string { return api.SearchChats }

func (s chatSource) List(context.Context) ([]search.Item, error) {
	recs, err := s.w.storage.ListWorkspace(s.w.storage.Workspace())
	if err != nil {
		return nil, err
	}
	out := make([]search.Item, 0, len(recs))
	for _, r := range recs {
		out = append(out, search.Item{Ref: r.ID, Size: int64(r.MessageCount), Modified: r.UpdatedAt})
	}
	return out, nil
}

func (s chatSource) Read(_ context.Context, ref string) (search.Doc, error) {
	rec, err := s.w.storage.Get(ref)
	if err != nil {
		return search.Doc{}, err
	}
	var b strings.Builder
	for _, m := range rec.Messages {
		if m.Role == "tool" || strings.TrimSpace(m.Content) == "" {
			continue
		}
		b.WriteString(strings.TrimSpace(m.Content))
		b.WriteString("\n\n")
	}
	if b.Len() == 0 {
		return search.Doc{}, search.ErrSkip
	}
	title := rec.Title
	if title == "" {
		title = ref
	}
	return search.Doc{Title: title, Text: b.String()}, nil
}

// plansDir is where approved plans are saved in the workspace.
const plansDir = ".blitz/plans"

// noteSource is the workspace's notes (the remember tool's, by name) and
// its saved plans (by path).
type noteSource struct {
	w    *Workspace
	root *os.Root
}

func (noteSource) Name() string { return api.SearchNotes }

func (s noteSource) List(context.Context) ([]search.Item, error) {
	var out []search.Item
	notes, err := memory.Notes(memory.NotesDir(s.w.Dir()))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, n := range notes {
		if info, err := os.Stat(n.Path); err == nil {
			out = append(out, search.Item{Ref: n.Name, Size: info.Size(), Modified: info.ModTime()})
		}
	}
	entries, _ := fs.ReadDir(s.root.FS(), plansDir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			out = append(out, search.Item{Ref: plansDir + "/" + e.Name(), Size: info.Size(), Modified: info.ModTime()})
		}
	}
	return out, nil
}

func (s noteSource) Read(_ context.Context, ref string) (search.Doc, error) {
	if strings.HasPrefix(ref, plansDir+"/") {
		data, err := readCapped(s.root, ref, s.w.cfg.Tools.MaxFileSizeBytes)
		if err != nil {
			return search.Doc{}, err
		}
		return search.Doc{Title: ref, Text: string(data)}, nil
	}
	notes, err := memory.Notes(memory.NotesDir(s.w.Dir()))
	if err != nil {
		return search.Doc{}, err
	}
	for _, n := range notes {
		if n.Name == ref {
			return search.Doc{Title: n.Name, Text: n.Text}, nil
		}
	}
	return search.Doc{}, search.ErrSkip
}

// describeInstruction asks for a file's summary and tags (search.enrich).
const describeInstruction = `You describe one file of a software project for a search index. Reply with JSON only, no other text: {"summary": "...", "tags": ["..."]}.
summary: one or two plain sentences saying what the file is and what it does, for someone deciding whether to open it.
tags: 3 to 8 lowercase keywords or short phrases for the concepts it covers, including words someone might search for that aren't in the text (synonyms, the domain).
Never repeat secrets, keys or credentials, even if the file holds some.`

// describeTableInstruction asks for a table's summary, tags and what each
// column holds, from a profile of its columns.
const describeTableInstruction = `You describe one data table (CSV) for a search index, from a profile of its columns: each column's name, type, range or empty count, and sample values. Reply with JSON only, no other text: {"summary": "...", "tags": ["..."], "columns": [{"name": "...", "intent": "..."}]}.
summary: one or two plain sentences saying what the table records and what one row is, for someone deciding whether to open it.
tags: 3 to 8 lowercase keywords for its subject and domain, including words someone might search for that aren't in the column names.
columns: every column, in order, with its exact name and, in a few words, what it holds and its unit if it has one ("water temperature, °C"; "station identifier"). Say "unclear" rather than guess.
Never repeat secrets, keys or credentials, even if the samples hold some.`

// maxDescribeChars is how much of a file the describer sends.
const maxDescribeChars = 24_000

// tableProfileRows is how many rows of a table's head are profiled.
const tableProfileRows = 2000

// describer has a model write a file's summary and tags (a table's from a
// profile of its columns, with what each holds), from its text with
// secrets redacted (SRCH-60).
type describer struct {
	llm    model.LLM
	redact interface{ String(string) string }
}

func (d describer) Describe(ctx context.Context, title, text string) (search.Description, error) {
	instruction, input := describeInstruction, text
	var columns []string
	if table.IsTable(title) {
		format, err := table.Sniff(title, []byte(text))
		if err == nil {
			cols, rows, err := table.Profile(table.Reader(strings.NewReader(text), format), tableProfileRows)
			if err == nil {
				instruction, input = describeTableInstruction, table.Describe(cols, rows)
				for _, c := range cols {
					columns = append(columns, c.Name)
				}
			}
		}
	}
	if len(input) > maxDescribeChars {
		input = input[:maxDescribeChars]
	}
	reply, served, usage, err := runtime.Ask(ctx, d.llm, instruction, "Path: "+title+"\n\n"+d.redact.String(input))
	if err != nil {
		return search.Description{}, err
	}
	if usage != nil {
		slog.Info("file described for search", "file", title, "model", served, "input_tokens", usage.PromptTokenCount, "output_tokens", usage.CandidatesTokenCount)
	}
	return parseDescription(reply, columns)
}

// parseDescription reads the describer's JSON, fenced or not: a summary of
// up to 300 characters, up to 10 short, lowercase tags, and for a table
// (columns, its names) what each of its columns holds, up to 120
// characters, for columns it has.
func parseDescription(reply string, columns []string) (search.Description, error) {
	start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if start < 0 || end < start {
		return search.Description{}, fmt.Errorf("no JSON in the description: %q", textutil.Ellipsize(reply, 200))
	}
	var raw struct {
		Summary string              `json:"summary"`
		Tags    []string            `json:"tags"`
		Columns []search.ColumnNote `json:"columns"`
	}
	if err := json.Unmarshal([]byte(reply[start:end+1]), &raw); err != nil {
		return search.Description{}, fmt.Errorf("the description's JSON: %w", err)
	}
	d := search.Description{Summary: textutil.Ellipsize(strings.TrimSpace(raw.Summary), 300)}
	for _, t := range raw.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || len(t) > 40 || slices.Contains(d.Tags, t) {
			continue
		}
		if d.Tags = append(d.Tags, t); len(d.Tags) == 10 {
			break
		}
	}
	for _, c := range raw.Columns {
		c.Name, c.Intent = strings.TrimSpace(c.Name), textutil.Ellipsize(strings.TrimSpace(c.Intent), 120)
		if c.Intent == "" || !slices.Contains(columns, c.Name) || slices.ContainsFunc(d.Columns, func(x search.ColumnNote) bool { return x.Name == c.Name }) {
			continue
		}
		d.Columns = append(d.Columns, c)
	}
	return d, nil
}
