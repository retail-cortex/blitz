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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/search"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/charmap"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// searchPDF is a two-page PDF.
func searchPDF(t *testing.T, pages ...string) []byte {
	t.Helper()
	f := fpdf.New("P", "mm", "A4", "")
	f.SetFont("Helvetica", "", 12)
	for _, p := range pages {
		f.AddPage()
		f.Cell(0, 10, p)
	}
	var buf bytes.Buffer
	require.NoError(t, f.Output(&buf))
	return buf.Bytes()
}

// searchWorkspace opens a workspace holding a little of everything search
// reads, and waits for its first scan.
func searchWorkspace(t *testing.T, replies ...*genai.Content) *Workspace {
	t.Helper()
	nb, err := json.Marshal(map[string]any{
		"nbformat": 4, "nbformat_minor": 5, "metadata": map[string]any{},
		"cells": []any{
			map[string]any{"cell_type": "markdown", "metadata": map[string]any{}, "source": "# Intro"},
			map[string]any{"cell_type": "code", "metadata": map[string]any{}, "source": "fit_regression(data)", "outputs": []any{}, "execution_count": nil},
		},
	})
	require.NoError(t, err)
	w, _ := openTestWith(t, func(c *config.Config) {
		dir := c.Tools.WorkspaceDir
		write(t, dir, "engine/rewind.go", "package engine\n\n// turnEnded ends a turn.\nfunc turnEnded() {}\n")
		write(t, dir, ".env", "TOKEN=zeppelin-secret\n")
		write(t, dir, ".agents/AGENTS.md", "Always cite the zeppelin registry.\n")
		write(t, dir, ".github/workflows/ci.yml", "name: hangar checks\n")
		write(t, dir, ".blitz/worktrees/task-1/copy.md", "a zeppelin in a worktree\n")
		write(t, dir, "docs/airships.md", "# Airships\n\nA zeppelin is a rigid airship.\n")
		write(t, dir, ".blitz/plans/s-1.md", "Plan: migrate the hangar to zeppelins.\n")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "report.pdf"), searchPDF(t, "Summary", "Quarterly zeppelin revenue"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "model.ipynb"), nb, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.bin"), []byte{0, 1, 2, 'z', 'e', 'p'}, 0o644))
		write(t, dir, "broken.pdf", "%PDF-1.7 cut short")
		write(t, dir, "broken.ipynb", "{not a notebook")
		write(t, dir, ".blitz/plans/notes.txt", "not a plan")
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".blitz", "plans", "old.md"), 0o700))
		_, err := memory.SaveNote(memory.NotesDir(dir), "fact", "The user prefers zeppelin diagrams.")
		require.NoError(t, err)
	}, replies...)
	waitScanned(t, w, time.Time{})
	return w
}

// waitScanned waits for a scan that finished after since.
func waitScanned(t *testing.T, w *Workspace, since time.Time) {
	t.Helper()
	require.Eventually(t, func() bool {
		s := w.SearchStatus()
		return !s.Scanning && s.LastScan.After(since)
	}, 20*time.Second, 20*time.Millisecond)
}

func hitRefs(hits []api.SearchHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Source + ":" + h.Ref
	}
	return out
}

// Each source finds its own; blocked paths and binaries never do; a PDF's
// hit says its page and a notebook's its cell.
func TestSearchSources(t *testing.T) {
	w := searchWorkspace(t)
	ctx := context.Background()
	st := w.SearchStatus()
	assert.True(t, st.Enabled)
	assert.Equal(t, 4, st.Items[api.SearchFiles], "rewind.go, airships.md, .agents/AGENTS.md and the CI workflow: not .env, a worktree or the binary")
	assert.Equal(t, 2, st.Items[api.SearchDocuments])
	assert.Equal(t, 2, st.Unreadable, "the broken PDF and notebook")
	assert.Equal(t, 2, st.Items[api.SearchNotes], "the note and the plan")

	for _, tc := range []struct {
		name    string
		q       api.SearchQuery
		want    []string
		section string
	}{
		{"defaults: files and documents", api.SearchQuery{Text: "zeppelin"}, []string{"files:docs/airships.md", "documents:report.pdf"}, ""},
		{"dot folders, when hidden ones are asked for", api.SearchQuery{Text: "zeppelin", Hidden: true}, []string{"files:docs/airships.md", "files:.agents/AGENTS.md", "documents:report.pdf"}, ""},
		{"dot folders, else not", api.SearchQuery{Text: "hangar checks"}, []string{}, ""},
		{"notes and plans", api.SearchQuery{Text: "zeppelin", Sources: []string{api.SearchNotes}}, nil, ""},
		{"identifier", api.SearchQuery{Text: "turnEnded"}, []string{"files:engine/rewind.go"}, ""},
		{"pdf page", api.SearchQuery{Text: "quarterly revenue", Sources: []string{api.SearchDocuments}}, []string{"documents:report.pdf"}, "page 2"},
		{"notebook cell", api.SearchQuery{Text: "fit_regression", Sources: []string{api.SearchDocuments}}, []string{"documents:model.ipynb"}, "cell 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := w.Search(ctx, tc.q)
			require.NoError(t, err)
			got := hitRefs(res.Hits)
			assert.NotContains(t, got, "files:.env")
			if tc.want != nil {
				assert.ElementsMatch(t, tc.want, got)
			}
			if tc.section != "" {
				require.NotEmpty(t, res.Hits)
				assert.Equal(t, tc.section, res.Hits[0].Section)
			}
		})
	}
	res, err := w.Search(ctx, api.SearchQuery{Text: "zeppelin", Sources: []string{api.SearchNotes}})
	require.NoError(t, err)
	assert.Len(t, res.Hits, 2)
	assert.Contains(t, hitRefs(res.Hits), "notes:.blitz/plans/s-1.md")
	assert.Equal(t, []string{api.SearchNotes}, res.Sources)
}

// A chat is found by what was said in it, once its turn has ended; a
// changed file is found by its new text after a scan.
func TestSearchChatsAndChanges(t *testing.T) {
	w := searchWorkspace(t, text("Dirigibles float on lifting gas."))
	ctx := context.Background()
	sess, err := w.NewSession()
	require.NoError(t, err)
	before := time.Now()
	_, err = w.Run(ctx, sess.ID, api.Turn{Text: "how do dirigibles stay up?"}, func(api.Event) {})
	require.NoError(t, err)
	waitScanned(t, w, before) // the turn's end asked for one

	res, err := w.Search(ctx, api.SearchQuery{Text: "lifting gas", Sources: []string{api.SearchChats}})
	require.NoError(t, err)
	require.Len(t, res.Hits, 1)
	assert.Equal(t, sess.ID, res.Hits[0].Ref)

	write(t, w.Dir(), "docs/airships.md", "# Airships\n\nBlimps have no rigid frame.\n")
	before = time.Now()
	require.NoError(t, w.Reindex())
	waitScanned(t, w, before)
	res, err = w.Search(ctx, api.SearchQuery{Text: "blimps"})
	require.NoError(t, err)
	assert.Equal(t, []string{"files:docs/airships.md"}, hitRefs(res.Hits))
}

// Searches that can't run say why.
func TestSearchErrors(t *testing.T) {
	w := searchWorkspace(t)
	ctx := context.Background()
	_, err := w.Search(ctx, api.SearchQuery{Text: "x", Sources: []string{"email"}})
	assert.ErrorIs(t, err, api.ErrUnknownSearchSource)
	_, err = w.Search(ctx, api.SearchQuery{Text: "  "})
	assert.ErrorIs(t, err, api.ErrEmptySearch)

	off, _ := openTestWith(t, func(c *config.Config) { c.Search.Enabled = false })
	_, err = off.Search(ctx, api.SearchQuery{Text: "x"})
	assert.ErrorIs(t, err, api.ErrSearchDisabled)
	assert.ErrorIs(t, off.Reindex(), api.ErrSearchDisabled)
	assert.False(t, off.SearchStatus().Enabled)
}

// The agent's search_workspace finds files with where they are.
func TestSearchWorkspaceTool(t *testing.T) {
	call := toolCall("search_workspace", map[string]any{"query": "turnEnded"})
	w := searchWorkspace(t, call, text("found it"))
	sess, err := w.NewSession()
	require.NoError(t, err)
	var result map[string]any
	_, err = w.Run(context.Background(), sess.ID, api.Turn{Text: "where do turns end?"}, func(ev api.Event) {
		if ev.ToolResult != nil && ev.ToolResult.Name == "search_workspace" {
			result = ev.ToolResult.Result
		}
	})
	require.NoError(t, err)
	require.NotNil(t, result, "the tool ran")
	b, err := json.Marshal(result)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"where":"engine/rewind.go:3"`)
}

// A workspace whose index can't be made opens with search off; the agent's
// tool says so rather than failing the turn.
func TestSearchIndexUnavailable(t *testing.T) {
	call := toolCall("search_workspace", map[string]any{"query": "x"})
	w, _ := openTestWith(t, func(c *config.Config) {
		dir := config.WorkspaceSettingsDir(c.Dir, c.Tools.WorkspaceDir)
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "search"), []byte("a file where the index goes"), 0o600))
	}, call, text("ok"))
	assert.False(t, w.SearchStatus().Enabled)
	_, err := w.Search(context.Background(), api.SearchQuery{Text: "x"})
	assert.ErrorIs(t, err, api.ErrSearchDisabled)

	sess, err := w.NewSession()
	require.NoError(t, err)
	var result map[string]any
	_, err = w.Run(context.Background(), sess.ID, api.Turn{Text: "find x"}, func(ev api.Event) {
		if ev.ToolResult != nil && ev.ToolResult.Name == "search_workspace" {
			result = ev.ToolResult.Result
		}
	})
	require.NoError(t, err)
	assert.Contains(t, result["error"], "workspace search is off")
}

// The agent's tool reports a search it can't run (a source search doesn't
// have) as its result.
func TestSearchWorkspaceToolError(t *testing.T) {
	call := toolCall("search_workspace", map[string]any{"query": "x", "sources": []any{"email"}})
	w := searchWorkspace(t, call, text("ok"))
	sess, err := w.NewSession()
	require.NoError(t, err)
	var result map[string]any
	_, err = w.Run(context.Background(), sess.ID, api.Turn{Text: "find x"}, func(ev api.Event) {
		if ev.ToolResult != nil && ev.ToolResult.Name == "search_workspace" {
			result = ev.ToolResult.Result
		}
	})
	require.NoError(t, err)
	assert.Contains(t, result["error"], "unknown search source")
}

// A file over the size limit stays out; a chat or note that's gone, or a
// chat with nothing said in it, isn't indexed.
func TestSearchSourceSkips(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Tools.MaxFileSizeBytes = 64
		write(t, c.Tools.WorkspaceDir, "small.md", "a zeppelin\n")
		write(t, c.Tools.WorkspaceDir, "big.md", "zeppelin "+strings.Repeat("x", 100)+"\n")
	})
	waitScanned(t, w, time.Time{})
	res, err := w.Search(context.Background(), api.SearchQuery{Text: "zeppelin"})
	require.NoError(t, err)
	assert.Equal(t, []string{"files:small.md"}, hitRefs(res.Hits))

	ctx := context.Background()
	_, err = chatSource{w: w}.Read(ctx, "session-gone")
	assert.Error(t, err)
	_, err = noteSource{w: w, root: w.searchNow().root}.Read(ctx, "20260101-000000-gone")
	assert.ErrorIs(t, err, search.ErrSkip)
	_, err = fileSource{w: w, root: w.searchNow().root}.Read(ctx, "missing.md")
	assert.Error(t, err)

	empty, err := w.NewSession()
	require.NoError(t, err)
	_, err = chatSource{w: w}.Read(ctx, empty.ID)
	assert.Error(t, err, "a chat never saved, or with nothing said, isn't indexed")
}

// An idle workspace's index catches up by itself, every searchInterval.
func TestSearchKeepsUp(t *testing.T) {
	old := searchInterval
	searchInterval = 50 * time.Millisecond
	t.Cleanup(func() { searchInterval = old })
	w := searchWorkspace(t)
	write(t, w.Dir(), "docs/new.md", "Blimps are not rigid.\n")
	require.Eventually(t, func() bool {
		res, err := w.Search(context.Background(), api.SearchQuery{Text: "blimps"})
		return err == nil && len(res.Hits) == 1
	}, 20*time.Second, 20*time.Millisecond, "found without a Reindex: %+v", w.SearchStatus())
}

// A chat is its prompts and replies: tool output is left out, a chat of
// tool output alone isn't searchable, and one without a title is named by
// its ID.
func TestSearchChatText(t *testing.T) {
	w := searchWorkspace(t)
	ctx := context.Background()
	toolOnly, untitled := session.NewSessionID(), session.NewSessionID()
	require.NoError(t, w.storage.AppendTo(toolOnly, session.Message{Role: "tool", Content: "zeppelin tool output"}))
	require.NoError(t, w.storage.AppendTo(untitled, session.Message{Role: "tool", Content: "hidden tool output"}))
	require.NoError(t, w.storage.AppendTo(untitled, session.Message{Role: "model", Content: "Airships drift."}))

	_, err := chatSource{w: w}.Read(ctx, toolOnly)
	assert.ErrorIs(t, err, search.ErrSkip)
	doc, err := chatSource{w: w}.Read(ctx, untitled)
	require.NoError(t, err)
	assert.Equal(t, untitled, doc.Title)
	assert.Equal(t, "Airships drift.\n\n", doc.Text)
}

// A search after the workspace closed fails rather than panicking.
func TestSearchAfterClose(t *testing.T) {
	w := searchWorkspace(t)
	require.NoError(t, w.Close())
	_, err := w.Search(context.Background(), api.SearchQuery{Text: "zeppelin"})
	assert.Error(t, err)
}

// A source that can't read what it listed (it vanished, its folder can't
// be read) reports the error, which the scan counts or reports.
func TestSearchSourceErrors(t *testing.T) {
	w := searchWorkspace(t)
	ctx := context.Background()
	_, err := documentSource{w: w, root: w.searchNow().root}.Read(ctx, "gone.pdf")
	assert.Error(t, err)
	_, err = noteSource{w: w, root: w.searchNow().root}.Read(ctx, plansDir+"/gone.md")
	assert.Error(t, err)

	notes := memory.NotesDir(w.Dir())
	require.NoError(t, os.RemoveAll(notes))
	require.NoError(t, os.WriteFile(notes, []byte("a file where the notes go"), 0o600))
	_, err = noteSource{w: w, root: w.searchNow().root}.List(ctx)
	assert.Error(t, err)
	_, err = noteSource{w: w, root: w.searchNow().root}.Read(ctx, "20260101-000000-a-note")
	assert.Error(t, err)

	if os.Geteuid() != 0 {
		sessions := w.cfg.Session.StorageDir
		require.NoError(t, os.Chmod(sessions, 0))
		t.Cleanup(func() { os.Chmod(sessions, 0o700) })
		_, err = chatSource{w: w}.List(ctx)
		assert.Error(t, err)
	}
}

// synonymEmbedder embeds by meaning, crudely: airship words share one
// dimension, cake words another.
type synonymEmbedder struct{}

func (synonymEmbedder) Model() string { return "fake/synonyms" }

func (synonymEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := []float32{0, 0, 0.01}
		for _, w := range strings.Fields(strings.ToLower(t)) {
			switch strings.Trim(w, ".,#") {
			case "zeppelin", "airship", "blimp", "dirigible":
				v[0]++
			case "cake", "pastry", "dessert":
				v[1]++
			}
		}
		out[i] = v
	}
	return out, nil
}

// With an embedding model, the workspace's chunks are embedded and a
// search finds by meaning; one that can't be built leaves keyword search.
func TestSemanticWorkspaceSearch(t *testing.T) {
	old := newEmbedder
	t.Cleanup(func() { newEmbedder = old })
	newEmbedder = func(context.Context, *config.Config, string) (search.Embedder, error) { return synonymEmbedder{}, nil }
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Search.EmbeddingModel = "fake/synonyms"
		write(t, c.Tools.WorkspaceDir, "docs/hangar.md", "The dirigible docks here.\n")
		write(t, c.Tools.WorkspaceDir, "docs/bakery.md", "Fresh pastry daily.\n")
	})
	waitScanned(t, w, time.Time{})
	st := w.SearchStatus()
	assert.Equal(t, "fake/synonyms", st.EmbeddingModel)
	assert.Equal(t, 2, st.Embedded)

	ctx := context.Background()
	res, err := w.Search(ctx, api.SearchQuery{Text: "zeppelin", Mode: api.SearchSemantic})
	require.NoError(t, err)
	require.NotEmpty(t, res.Hits)
	assert.Equal(t, "docs/hangar.md", res.Hits[0].Ref, "found by meaning")
	res, err = w.Search(ctx, api.SearchQuery{Text: "zeppelin", Mode: api.SearchKeyword})
	require.NoError(t, err)
	assert.Empty(t, res.Hits, "no such word")
	_, err = w.Search(ctx, api.SearchQuery{Text: "zeppelin", Mode: "psychic"})
	assert.ErrorIs(t, err, api.ErrUnknownSearchMode)

	newEmbedder = func(context.Context, *config.Config, string) (search.Embedder, error) {
		return nil, errors.New("no key")
	}
	kw, _ := openTestWith(t, func(c *config.Config) { c.Search.EmbeddingModel = "gemini/x" })
	assert.Empty(t, kw.SearchStatus().EmbeddingModel)
	_, err = kw.Search(ctx, api.SearchQuery{Text: "x", Mode: api.SearchSemantic})
	assert.ErrorIs(t, err, api.ErrNoEmbeddings)
}

// The describer's reply is read fenced or not; its summary is cut and its
// tags cleaned (lowercase, short, no repeats, ten at most); a reply with
// no JSON, or bad JSON, is an error.
func TestParseDescription(t *testing.T) {
	long := strings.Repeat("s", 400)
	many := make([]string, 14)
	for i := range many {
		many[i] = fmt.Sprintf(`"t%d"`, i)
	}
	for name, tc := range map[string]struct {
		reply   string
		summary string
		tags    []string
		err     string
	}{
		"plain":  {reply: `{"summary": "Docks airships.", "tags": ["Hangar", " aviation ", "hangar"]}`, summary: "Docks airships.", tags: []string{"hangar", "aviation"}},
		"fenced": {reply: "```json\n{\"summary\": \"x\", \"tags\": []}\n```", summary: "x"},
		"cut":    {reply: `{"summary": "` + long + `", "tags": ["` + strings.Repeat("t", 50) + `", ` + strings.Join(many, ", ") + `]}`, summary: textutil.Ellipsize(long, 300), tags: []string{"t0", "t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9"}},
		"none":   {reply: "I can't.", err: "no JSON"},
		"bad":    {reply: `{"summary": }`, err: "JSON"},
	} {
		t.Run(name, func(t *testing.T) {
			d, err := parseDescription(tc.reply, nil)
			if tc.err != "" {
				assert.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.summary, d.Summary)
			assert.Equal(t, tc.tags, d.Tags)
		})
	}
}

// With search.enrich on, the model describes the workspace's files in the
// background (their secrets redacted first), and a search finds a file by
// a tag its text doesn't hold.
func TestSearchEnrichment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	t.Chdir(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Session.StorageDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "a.json")
	cfg.Search.Enrich = true
	cfg.Search.EnrichModel = "describer"
	write(t, cfg.Tools.WorkspaceDir, "hangar.go", "package hangar // token=sk-abcdefghijklmnopqrstuvwxyz0123456789\n")
	describerLLM := runtime.NewMockLLM("describer", genai.NewContentFromText(`{"summary": "Docks airships.", "tags": ["aviation"]}`, genai.RoleModel))
	w, err := Open(context.Background(), cfg, Options{
		Model: runtime.NewMockLLM("gemini-3.8-flash"),
		NewModel: func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
			if ref == "describer" {
				return describerLLM, nil
			}
			return runtime.NewMockLLM(ref), nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { w.Close() })
	waitScanned(t, w, time.Time{})
	assert.Equal(t, 1, w.SearchStatus().Enriched)

	res, err := w.Search(context.Background(), api.SearchQuery{Text: "aviation"})
	require.NoError(t, err)
	require.Len(t, res.Hits, 1)
	assert.Equal(t, "hangar.go", res.Hits[0].Ref)
	assert.Equal(t, "Docks airships.", res.Hits[0].Summary)
	assert.Equal(t, []string{"aviation"}, res.Hits[0].Tags)
	require.NotEmpty(t, describerLLM.Requests)
	var sent strings.Builder
	for _, r := range describerLLM.Requests {
		for _, c := range r.Contents {
			for _, p := range c.Parts {
				sent.WriteString(p.Text)
			}
		}
	}
	assert.Contains(t, sent.String(), "package hangar", "the file was sent")
	assert.NotContains(t, sent.String(), "sk-abcdefghijklmnopqrstuvwxyz0123456789", "its secret wasn't")
}

// failingLLM fails every call.
type failingLLM struct{}

func (failingLLM) Name() string { return "failing" }

func (failingLLM) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) { yield(nil, errors.New("model unavailable")) }
}

// The default embedder is the runtime's; a describer whose model can't be
// built leaves search without summaries; one whose model fails says so,
// whatever the file's length.
func TestSearchModelsUnavailable(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultConfig()
	cfg.LLM.OpenAI.APIKey = "sk"
	e, err := newEmbedder(ctx, cfg, "openai/text-embedding-3-small")
	require.NoError(t, err)
	assert.Equal(t, "openai/text-embedding-3-small", e.Model())

	w, _ := openTestWith(t, func(c *config.Config) {
		c.Search.Enrich = true
		c.Search.EnrichModel = "broken"
		write(t, c.Tools.WorkspaceDir, "a.md", "plain words\n")
	})
	waitScanned(t, w, time.Time{})
	assert.Zero(t, w.SearchStatus().Enriched)
	res, err := w.Search(ctx, api.SearchQuery{Text: "plain"})
	require.NoError(t, err)
	assert.Len(t, res.Hits, 1, "search works without summaries")

	d := describer{llm: failingLLM{}, redact: SecretRedactor(cfg)}
	_, err = d.Describe(ctx, "big.md", strings.Repeat("a", maxDescribeChars+10))
	assert.ErrorContains(t, err, "model unavailable")
}

// Search settings apply while the workspace is open: turned off and on
// again, given an embedding model, or one that can't be built (the status
// says why); the same settings leave the index alone. A change to the
// workspace's settings file is picked up by itself.
func TestReloadSearch(t *testing.T) {
	old := newEmbedder
	t.Cleanup(func() { newEmbedder = old })
	newEmbedder = func(_ context.Context, _ *config.Config, ref string) (search.Embedder, error) {
		if ref == "broken/model" {
			return nil, errors.New("no key")
		}
		return synonymEmbedder{}, nil
	}
	w := searchWorkspace(t)
	ctx := context.Background()
	with := func(f func(*config.SearchConfig)) *config.Config {
		cfg := *w.cfg
		f(&cfg.Search)
		return &cfg
	}

	before := w.searchNow()
	w.ReloadSearch(with(func(*config.SearchConfig) {}))
	assert.Same(t, before, w.searchNow(), "the same settings: left alone")

	w.ReloadSearch(with(func(s *config.SearchConfig) { s.Enabled = false }))
	st := w.SearchStatus()
	assert.False(t, st.Enabled)
	assert.False(t, st.Settings.Enabled, "the settings shown, off too")
	_, err := w.Search(ctx, api.SearchQuery{Text: "zeppelin"})
	assert.ErrorIs(t, err, api.ErrSearchDisabled)

	reloaded := time.Now()
	w.ReloadSearch(with(func(s *config.SearchConfig) { s.Enabled, s.EmbeddingModel = true, "fake/synonyms" }))
	waitScanned(t, w, reloaded)
	st = w.SearchStatus()
	assert.Equal(t, "fake/synonyms", st.EmbeddingModel)
	assert.Equal(t, "fake/synonyms", st.Settings.EmbeddingModel)
	res, err := w.Search(ctx, api.SearchQuery{Text: "dirigible", Mode: api.SearchSemantic})
	require.NoError(t, err)
	assert.NotEmpty(t, res.Hits)

	w.ReloadSearch(with(func(s *config.SearchConfig) { s.EmbeddingModel = "broken/model" }))
	st = w.SearchStatus()
	assert.True(t, st.Enabled, "keyword search goes on")
	assert.Empty(t, st.EmbeddingModel)
	assert.Contains(t, st.Problem, "no key")

	_, err = config.SetValue(w.cfg.Dir, w.Dir(), "search.sources", "notes")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return slices.Equal(w.SearchStatus().Settings.Sources, []string{"notes"})
	}, 20*time.Second, 50*time.Millisecond, "the settings file's change applied")
	waitScanned(t, w, time.Time{})
	res, err = w.Search(ctx, api.SearchQuery{Text: "zeppelin"})
	require.NoError(t, err)
	assert.Equal(t, []string{api.SearchNotes}, res.Sources, "the new default sources")
}

// gitRepo makes dir a git repository with a first commit of what's there.
func gitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "first"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
}

// Hidden items (dotfiles; files git ignores, with include_ignored) are
// found only when asked for; the agent's tool asks. Ignored files come in
// without dependency folders, and a Latin-1 table is read.
func TestSearchHiddenAndIgnored(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	latin1, err := charmap.Windows1252.NewEncoder().String("Station,Salinité\nCalCOFI,33.4\n")
	require.NoError(t, err)
	w, _ := openTestWith(t, func(c *config.Config) {
		dir := c.Tools.WorkspaceDir
		c.Search.IncludeIgnored = true
		write(t, dir, ".gitignore", "data/\nnode_modules/\n")
		write(t, dir, "README.md", "The CalCOFI study.\n")
		write(t, dir, ".agents/AGENTS.md", "Cite CalCOFI.\n")
		write(t, dir, "data/bottle.csv", latin1)
		write(t, dir, "node_modules/x/index.js", "// CalCOFI in a dependency\n")
		gitRepo(t, dir)
	})
	waitScanned(t, w, time.Time{})
	ctx := context.Background()
	res, err := w.Search(ctx, api.SearchQuery{Text: "CalCOFI"})
	require.NoError(t, err)
	assert.Equal(t, []string{"files:README.md"}, hitRefs(res.Hits), "hidden ones left out")
	res, err = w.Search(ctx, api.SearchQuery{Text: "CalCOFI", Hidden: true})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"files:README.md", "files:.agents/AGENTS.md", "files:data/bottle.csv"}, hitRefs(res.Hits), "not node_modules")
	res, err = w.Search(ctx, api.SearchQuery{Text: "Salinité", Hidden: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"files:data/bottle.csv"}, hitRefs(res.Hits), "Latin-1, decoded")
}

// A table is described from its columns' profile: what each holds is kept
// (columns it doesn't have are dropped), and it's found by them.
func TestDescribeTable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	t.Chdir(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Session.StorageDir = t.TempDir()
	cfg.Tools.ApprovalsFile = filepath.Join(t.TempDir(), "a.json")
	cfg.Search.Enrich, cfg.Search.EnrichModel = true, "describer"
	write(t, cfg.Tools.WorkspaceDir, "bottle.csv", "Sta_ID,Salnty\n054.0 056.0,33.44\n054.0 056.0,33.5\n")
	reply := `{"summary": "Bottle samples.", "tags": ["oceanography"], "columns": [{"name": "Sta_ID", "intent": "station identifier"}, {"name": "Salnty", "intent": "salinity, PSU"}, {"name": "Made_Up", "intent": "nothing"}]}`
	describerLLM := runtime.NewMockLLM("describer", genai.NewContentFromText(reply, genai.RoleModel))
	w, err := Open(context.Background(), cfg, Options{
		Model: runtime.NewMockLLM("gemini-3.8-flash"),
		NewModel: func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
			if ref == "describer" {
				return describerLLM, nil
			}
			return runtime.NewMockLLM(ref), nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { w.Close() })
	waitScanned(t, w, time.Time{})

	cols, err := w.searchNow().index.Columns(context.Background(), api.SearchFiles, "bottle.csv")
	require.NoError(t, err)
	assert.Equal(t, []search.ColumnNote{{Name: "Sta_ID", Intent: "station identifier"}, {Name: "Salnty", Intent: "salinity, PSU"}}, cols)
	res, err := w.Search(context.Background(), api.SearchQuery{Text: "salinity"})
	require.NoError(t, err)
	assert.Equal(t, []string{"files:bottle.csv"}, hitRefs(res.Hits))
	var sent strings.Builder
	for _, r := range describerLLM.Requests {
		for _, c := range r.Contents {
			for _, p := range c.Parts {
				sent.WriteString(p.Text)
			}
		}
	}
	assert.Contains(t, sent.String(), "- Salnty (number, 33.44 to 33.5)", "the profile, not the raw rows")
}
