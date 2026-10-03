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

package search

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memSource is a source held in memory; it counts its reads.
type memSource struct {
	name string

	mu    sync.Mutex
	items map[string]memItem
	reads map[string]int
}

type memItem struct {
	doc  Doc
	mod  time.Time
	err  error
	size int64
}

func newSource(name string) *memSource {
	return &memSource{name: name, items: map[string]memItem{}, reads: map[string]int{}}
}

func (m *memSource) Name() string { return m.name }

func (m *memSource) List(context.Context) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Item
	for ref, it := range m.items {
		out = append(out, Item{Ref: ref, Size: it.size, Modified: it.mod})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, nil
}

func (m *memSource) Read(_ context.Context, ref string) (Doc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads[ref]++
	it := m.items[ref]
	return it.doc, it.err
}

// set puts ref with text, as changed at mod.
func (m *memSource) set(ref, text string, mod time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[ref] = memItem{doc: Doc{Title: ref, Text: text}, mod: mod, size: int64(len(text))}
}

func (m *memSource) put(ref string, it memItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[ref] = it
}

func (m *memSource) remove(ref string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, ref)
}

func (m *memSource) readsOf(ref string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reads[ref]
}

func openTest(t *testing.T, allowed Allowed, sources ...Source) *Index {
	t.Helper()
	x, err := Open(t.TempDir(), sources, allowed)
	require.NoError(t, err)
	t.Cleanup(func() { x.Close() })
	return x
}

func refs(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Ref
	}
	return out
}

var t0 = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

// A scan reads only what changed; touched files aren't indexed again;
// gone ones are dropped.
func TestScanIsIncremental(t *testing.T) {
	ctx := context.Background()
	files := newSource("files")
	files.set("a.go", "func turnEnded() {}", t0)
	files.set("b.md", "notes about indexing", t0)
	x := openTest(t, nil, files)

	require.NoError(t, x.Scan(ctx))
	assert.Equal(t, map[string]int{"files": 2}, x.Status().Items)
	require.NoError(t, x.Scan(ctx))
	assert.Equal(t, 1, files.readsOf("a.go"), "unchanged: not read again")

	files.set("a.go", "func turnEnded() {}", t0.Add(time.Second)) // touched
	files.set("b.md", "notes about rewinding", t0.Add(time.Second))
	require.NoError(t, x.Scan(ctx))
	assert.Equal(t, 2, files.readsOf("a.go"))
	hits, err := x.Search(ctx, Query{Text: "rewinding"})
	require.NoError(t, err)
	assert.Equal(t, []string{"b.md"}, refs(hits))
	hits, err = x.Search(ctx, Query{Text: "indexing"})
	require.NoError(t, err)
	assert.Empty(t, hits, "the old text is gone")

	files.remove("b.md")
	require.NoError(t, x.Scan(ctx))
	assert.Equal(t, map[string]int{"files": 1}, x.Status().Items)
	assert.False(t, x.Status().Scanning)
	assert.False(t, x.Status().LastScan.IsZero())
}

// Items a source can't read, or that aren't searchable, stay out; one
// that becomes unsearchable is dropped.
func TestScanSkipsAndUnreadable(t *testing.T) {
	ctx := context.Background()
	files := newSource("files")
	files.set("ok.txt", "plain words", t0)
	files.put("bin.dat", memItem{err: ErrSkip, mod: t0})
	files.put("locked.txt", memItem{err: errors.New("permission denied"), mod: t0})
	x := openTest(t, nil, files)

	require.NoError(t, x.Scan(ctx))
	st := x.Status()
	assert.Equal(t, map[string]int{"files": 1}, st.Items)
	assert.Equal(t, 1, st.Unreadable)
	require.NoError(t, x.Scan(ctx))
	assert.Equal(t, 1, files.readsOf("locked.txt"), "unreadable: not read again until it changes")
	assert.Equal(t, 1, x.Status().Unreadable)
	files.put("locked.txt", memItem{doc: Doc{Title: "locked.txt", Text: "readable now"}, mod: t0.Add(time.Second)})
	require.NoError(t, x.Scan(ctx))
	assert.Equal(t, 0, x.Status().Unreadable)
	hits, err := x.Search(ctx, Query{Text: "readable"})
	require.NoError(t, err)
	assert.Equal(t, []string{"locked.txt"}, refs(hits))

	files.put("ok.txt", memItem{err: ErrSkip, mod: t0.Add(time.Second)})
	require.NoError(t, x.Scan(ctx))
	hits, err = x.Search(ctx, Query{Text: "plain"})
	require.NoError(t, err)
	assert.Empty(t, hits, "now unsearchable: dropped")
	assert.Equal(t, 1, x.Status().Items["files"], "locked.txt, readable since")
}

// What isn't allowed is neither indexed nor shown, even if it was indexed
// before the rule changed.
func TestAllowed(t *testing.T) {
	ctx := context.Background()
	files := newSource("files")
	files.set(".env", "SECRET=token", t0)
	files.set("main.go", "token handling", t0)
	blocked := map[string]bool{".env": true}
	var mu sync.Mutex
	allowed := func(_, ref string) bool { mu.Lock(); defer mu.Unlock(); return !blocked[ref] }
	x := openTest(t, allowed, files)

	require.NoError(t, x.Scan(ctx))
	hits, err := x.Search(ctx, Query{Text: "token"})
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, refs(hits))

	mu.Lock()
	blocked["main.go"] = true
	mu.Unlock()
	hits, err = x.Search(ctx, Query{Text: "token"})
	require.NoError(t, err)
	assert.Empty(t, hits, "blocked since: not shown before the next scan")
	require.NoError(t, x.Scan(ctx))
	assert.Empty(t, x.Status().Items["files"], "and dropped by it")
}

// Words rank by BM25 (an item about the words beats a passing mention);
// identifiers and substrings match through the trigram table; the
// sources asked for are the ones searched.
func TestSearchRanking(t *testing.T) {
	ctx := context.Background()
	files := newSource("files")
	notes := newSource("notes")
	files.set("engine/rewind.go", "// turnEnded ends a turn.\nfunc (w *Workspace) turnEnded(session string) {\n\tw.turnsMu.Lock()\n}", t0)
	files.set("docs/turns.md", "A turn ends when the model stops. Turns end; turns ended.", t0)
	files.set("README.md", "Blitz is an agent. It has turns.", t0)
	notes.set("note-1", "remember: turnEnded must unlock", t0)
	x := openTest(t, nil, files, notes)
	require.NoError(t, x.Scan(ctx))

	for _, tc := range []struct {
		name    string
		q       Query
		want    []string
		first   string
		missing string
	}{
		{name: "identifier", q: Query{Text: "turnEnded", Sources: []string{"files"}}, first: "engine/rewind.go"},
		{name: "substring", q: Query{Text: "turnsMu"}, want: []string{"engine/rewind.go"}},
		{name: "stemmed words", q: Query{Text: "turns ending"}, first: "docs/turns.md"},
		{name: "one source", q: Query{Text: "turnEnded", Sources: []string{"notes"}}, want: []string{"note-1"}},
		{name: "phrase", q: Query{Text: `"model stops"`}, want: []string{"docs/turns.md"}},
		{name: "word being typed", q: Query{Text: "Blit"}, want: []string{"README.md"}},
		{name: "limit", q: Query{Text: "turn", Limit: 1}, missing: "README.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits, err := x.Search(ctx, tc.q)
			require.NoError(t, err)
			got := refs(hits)
			if tc.want != nil {
				assert.ElementsMatch(t, tc.want, got)
			}
			if tc.first != "" {
				require.NotEmpty(t, got)
				assert.Equal(t, tc.first, got[0], "%v", got)
			}
			if tc.missing != "" {
				assert.Len(t, got, 1)
				assert.NotContains(t, got, tc.missing)
			}
		})
	}
	_, err := x.Search(ctx, Query{Text: "   "})
	assert.ErrorIs(t, err, ErrNoTerms)
}

// A hit says the line of its passage and the section it's in, and shows
// the lines around it; one hit per item.
func TestHitLineSectionSnippet(t *testing.T) {
	ctx := context.Background()
	var b strings.Builder
	for i := 1; i <= 150; i++ {
		switch i {
		case 1:
			b.WriteString("--- Page 1 ---\n")
		case 100:
			b.WriteString("--- Page 2 ---\n")
		case 120:
			b.WriteString("the quarterly revenue table\n")
		case 130:
			b.WriteString("revenue again, later\n")
		default:
			fmt.Fprintf(&b, "filler line %d\n", i)
		}
	}
	docs := newSource("documents")
	docs.put("report.pdf", memItem{doc: Doc{Title: "report.pdf", Text: b.String(), Sections: []Section{{1, "page 1"}, {100, "page 2"}}}, mod: t0})
	x := openTest(t, nil, docs)
	require.NoError(t, x.Scan(ctx))

	hits, err := x.Search(ctx, Query{Text: "quarterly revenue"})
	require.NoError(t, err)
	require.Len(t, hits, 1, "one hit per item")
	h := hits[0]
	assert.Equal(t, 120, h.Line)
	assert.Equal(t, "page 2", h.Section)
	assert.Equal(t, "filler line 119\nthe quarterly revenue table\nfiller line 121", h.Snippet)
	assert.Equal(t, "documents", h.Source)
	assert.Positive(t, h.Score)
}

// Chunks split at sections and at their size; a very long line is cut.
func TestChunk(t *testing.T) {
	long := strings.Repeat("x", maxLine+10)
	lines := make([]string, 130)
	for i := range lines {
		lines[i] = fmt.Sprint(i + 1)
	}
	for _, tc := range []struct {
		name  string
		doc   Doc
		lines []int
	}{
		{"by lines", Doc{Text: strings.Join(lines, "\n")}, []int{1, 61, 121}},
		{"by sections", Doc{Text: "a\nb\nc", Sections: []Section{{1, "one"}, {3, "two"}}}, []int{1, 3}},
		{"blank skipped", Doc{Text: "\n\n"}, nil},
		{"long line", Doc{Text: long + "\nend"}, []int{1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []int
			for _, p := range chunk(tc.doc) {
				got = append(got, p.line)
				for _, l := range strings.Split(p.text, "\n") {
					assert.LessOrEqual(t, len(l), maxLine)
				}
			}
			assert.Equal(t, tc.lines, got)
		})
	}
	ps := chunk(Doc{Text: "a\nb\nc", Sections: []Section{{1, "one"}, {3, "two"}}})
	assert.Equal(t, "two", ps[1].section)
}

// An index of another version is rebuilt; one of this version is kept.
func TestReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	files := newSource("files")
	files.set("a.txt", "kept words", t0)
	x, err := Open(dir, []Source{files}, nil)
	require.NoError(t, err)
	require.NoError(t, x.Scan(ctx))
	require.NoError(t, x.Close())

	x, err = Open(dir, []Source{files}, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"files": 1}, x.Status().Items, "kept")
	assert.False(t, x.Status().LastScan.IsZero(), "when it was last scanned, kept too")
	_, err = x.db.Exec(`UPDATE meta SET value = '0' WHERE key = 'version'`)
	require.NoError(t, err)
	require.NoError(t, x.Close())

	x, err = Open(dir, []Source{files}, nil)
	require.NoError(t, err)
	defer x.Close()
	assert.Empty(t, x.Status().Items, "rebuilt")
}

// A cancelled scan stops and says nothing of when it last finished.
func TestScanCancelled(t *testing.T) {
	files := newSource("files")
	for i := range 10 {
		files.set(fmt.Sprintf("f%d", i), "text", t0)
	}
	x := openTest(t, nil, files)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, x.Scan(ctx))
	assert.True(t, x.Status().LastScan.IsZero())
	assert.Equal(t, []string{"files"}, x.Sources())
}

// failingSource can't list its items.
type failingSource struct{}

func (failingSource) Name() string                         { return "broken" }
func (failingSource) List(context.Context) ([]Item, error) { return nil, errors.New("disk on fire") }
func (failingSource) Read(context.Context, string) (Doc, error) {
	return Doc{}, errors.New("unreachable")
}

// A source that can't list is reported, and the others are still
// scanned; an index that can't be used, or opened, says why.
func TestFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("a source fails", func(t *testing.T) {
		files := newSource("files")
		files.set("a.txt", "fine words", t0)
		x := openTest(t, nil, failingSource{}, files)
		err := x.Scan(ctx)
		require.ErrorContains(t, err, "disk on fire")
		st := x.Status()
		assert.Contains(t, st.Err, "disk on fire")
		assert.Equal(t, 1, st.Items["files"], "the other source was scanned")
	})
	t.Run("closed", func(t *testing.T) {
		files := newSource("files")
		files.set("a.txt", "fine words", t0)
		x := openTest(t, nil, files)
		require.NoError(t, x.Close())
		assert.Error(t, x.Scan(ctx))
		_, err := x.Search(ctx, Query{Text: "fine"})
		assert.Error(t, err)
		_, err = x.Search(ctx, Query{Text: "fine", Sources: []string{}})
		assert.Error(t, err)
	})
	t.Run("not a folder", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		_, err := Open(filepath.Join(file, "search"), nil, nil)
		assert.Error(t, err)
	})
	t.Run("not a database", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "index.db"), []byte(strings.Repeat("not sqlite ", 200)), 0o600))
		_, err := Open(dir, nil, nil)
		assert.ErrorContains(t, err, "the search index")
	})
}

// A snippet's very long line is cut; hits that score the same keep a
// steady order.
func TestSnippetCutAndTies(t *testing.T) {
	ctx := context.Background()
	files := newSource("files")
	files.set("long.txt", "needle "+strings.Repeat("x", 400), t0)
	files.set("a.txt", "needle", t0)
	files.set("b.txt", "needle", t0)
	x := openTest(t, nil, files)
	require.NoError(t, x.Scan(ctx))
	hits, err := x.Search(ctx, Query{Text: "needle"})
	require.NoError(t, err)
	require.Len(t, hits, 3)
	for _, h := range hits {
		if h.Ref == "long.txt" {
			assert.True(t, strings.HasSuffix(h.Snippet, "…"))
			assert.LessOrEqual(t, len([]rune(h.Snippet)), snippetWidth+1)
		}
	}
	again, err := x.Search(ctx, Query{Text: "needle"})
	require.NoError(t, err)
	assert.Equal(t, refs(hits), refs(again))
}

// A write the index refuses (a trigger stands in for a full disk) fails
// the scan with its reason, whichever write it is, and leaves the index
// as it was.
func TestScanWriteFailures(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		trigger string
		change  func(files *memSource)
	}{
		{"new item", "BEFORE INSERT ON docs", func(f *memSource) { f.set("new.txt", "new words", t0) }},
		{"changed item", "BEFORE UPDATE ON docs", func(f *memSource) { f.set("a.txt", "other words", t0.Add(time.Second)) }},
		{"touched item", "BEFORE UPDATE ON docs", func(f *memSource) { f.set("a.txt", "fine words", t0.Add(time.Second)) }},
		{"its chunks", "BEFORE INSERT ON chunks", func(f *memSource) { f.set("new.txt", "new words", t0) }},
		{"gone item", "BEFORE DELETE ON docs", func(f *memSource) { f.remove("a.txt") }},
		{"its old chunks", "BEFORE DELETE ON chunks", func(f *memSource) { f.set("a.txt", "other words", t0.Add(time.Second)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := newSource("files")
			files.set("a.txt", "fine words", t0)
			x := openTest(t, nil, files)
			require.NoError(t, x.Scan(ctx))
			_, err := x.db.Exec(`CREATE TRIGGER refuse ` + tc.trigger + ` BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
			require.NoError(t, err)
			tc.change(files)

			err = x.Scan(ctx)
			require.ErrorContains(t, err, "disk full")
			assert.Contains(t, x.Status().Err, "disk full")
			hits, err := x.Search(ctx, Query{Text: "fine"})
			require.NoError(t, err)
			assert.Equal(t, []string{"a.txt"}, refs(hits), "the index as it was")
		})
	}
}

// A gone item whose chunks can't be removed fails the scan and stays.
func TestScanDropFailure(t *testing.T) {
	ctx := context.Background()
	files := newSource("files")
	files.set("a.txt", "fine words", t0)
	x := openTest(t, nil, files)
	require.NoError(t, x.Scan(ctx))
	_, err := x.db.Exec(`CREATE TRIGGER refuse BEFORE DELETE ON chunks BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
	require.NoError(t, err)
	files.remove("a.txt")
	require.ErrorContains(t, x.Scan(ctx), "disk full")
	assert.Equal(t, 1, x.Status().Items["files"])
}

// An index with no sources finds nothing.
func TestNoSources(t *testing.T) {
	x := openTest(t, nil)
	hits, err := x.Search(context.Background(), Query{Text: "anything"})
	require.NoError(t, err)
	assert.Empty(t, hits)
	assert.Empty(t, x.Sources())
}
