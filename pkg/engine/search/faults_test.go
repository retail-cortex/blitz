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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A write the vectors or the summaries can't make (a trigger, or folders
// that turned read-only, stand in for a full disk) stops that pass with
// its reason in the status; the scan itself, and keyword search, go on.
func TestEmbedAndEnrichFaults(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		break_ func(t *testing.T, x *Index, dir string)
		embed  bool // the fault is the embedding pass's, else enrichment's
	}{
		{"vectors refused", trigger("BEFORE INSERT ON vectors"), true},
		{"old vectors kept", trigger("BEFORE DELETE ON stale"), true},
		{"vector files read-only", readOnly("vectors"), true},
		{"summary refused", trigger("BEFORE UPDATE OF meta_hash ON docs"), false},
		{"budget refused", func(t *testing.T, x *Index, dir string) {
			trigger("BEFORE INSERT ON meta WHEN NEW.key LIKE 'enrich%'")(t, x, dir)
			trigger("BEFORE UPDATE ON meta WHEN NEW.key LIKE 'enrich%'")(t, x, dir)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := newSource("files")
			files.set("a.md", "the car is parked", t0)
			files.set("b.md", "a cake for dessert", t0)
			x, err := Open(dir, []Source{files}, nil, WithEmbedder(&fakeEmbedder{model: "fake/one"}), WithEnricher(&fakeEnricher{}, 10, nil))
			require.NoError(t, err)
			t.Cleanup(func() { x.Close() })
			require.NoError(t, x.Scan(ctx))
			require.Empty(t, x.Status().EmbedErr)
			require.Empty(t, x.Status().EnrichErr)

			tc.break_(t, x, dir)
			files.set("a.md", "the vehicle has gone", t0.Add(time.Second))
			require.NoError(t, x.Scan(ctx), "the scan itself succeeds")
			st := x.Status()
			if tc.embed {
				assert.NotEmpty(t, st.EmbedErr)
			} else {
				assert.NotEmpty(t, st.EnrichErr)
			}
			hits, err := x.Search(ctx, Query{Text: "vehicle", Mode: ModeKeyword})
			require.NoError(t, err)
			assert.Len(t, hits, 1, "keywords still work")
		})
	}
}

// triggers numbers the triggers made, for their names.
var triggers int

// trigger refuses the writes a trigger's timing names.
func trigger(when string) func(t *testing.T, x *Index, dir string) {
	return func(t *testing.T, x *Index, _ string) {
		t.Helper()
		triggers++
		_, err := x.db.Exec(fmt.Sprintf(`CREATE TRIGGER refuse_%d %s BEGIN SELECT RAISE(ABORT, 'disk full'); END`, triggers, when))
		require.NoError(t, err)
	}
}

// readOnly makes a folder of the index's, and everything in it, read-only.
func readOnly(sub string) func(t *testing.T, x *Index, dir string) {
	return func(t *testing.T, _ *Index, dir string) {
		t.Helper()
		root := filepath.Join(dir, sub)
		var dirs []string
		require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				dirs = append(dirs, p)
			}
			return err
		}))
		for _, d := range dirs {
			require.NoError(t, os.Chmod(d, 0o500))
		}
		t.Cleanup(func() {
			for _, d := range dirs {
				os.Chmod(d, 0o700)
			}
		})
	}
}

// Another model's vectors that can't be removed (a folder in them is
// read-only) fail Open, rather than mixing two models' vectors.
func TestVectorsUnremovable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	dir := t.TempDir()
	stuck := filepath.Join(dir, "vectors", "stuck")
	require.NoError(t, os.MkdirAll(stuck, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stuck, "x"), nil, 0o600))
	require.NoError(t, os.Chmod(stuck, 0o500))
	t.Cleanup(func() { os.Chmod(stuck, 0o700) })
	_, err := Open(dir, nil, nil, WithEmbedder(&fakeEmbedder{model: "fake/one"}))
	assert.ErrorContains(t, err, "vectors")
}

// Every pass and query reports a database that's gone, rather than
// passing for done or empty.
func TestClosedIndex(t *testing.T) {
	ctx := context.Background()
	files := newSource("files")
	files.set("a.md", "the car is parked", t0)
	x, err := Open(t.TempDir(), []Source{files}, nil, WithEmbedder(&fakeEmbedder{model: "fake/one"}), WithEnricher(&fakeEnricher{}, 10, nil))
	require.NoError(t, err)
	require.NoError(t, x.Scan(ctx))
	require.NoError(t, x.Close())

	for name, call := range map[string]func() error{
		"embed":      func() error { return x.embedPending(ctx) },
		"drop stale": func() error { return x.dropStale(ctx) },
		"add":        func() error { return x.addVectors(ctx, []int64{1}, []string{"files"}, [][]float32{{1, 0, 0, 0}}) },
		"similar":    func() error { _, err := x.similar(ctx, "car", []string{"files"}, 5); return err },
		"candidates": func() error { _, err := x.candidates(ctx, []int64{1}); return err },
		"enrich":     func() error { return x.enrichPending(ctx) },
		"text":       func() error { _, err := x.docText(ctx, 1); return err },
		"about":      func() error { return x.setAbout(1, "h", Description{Summary: "s", Tags: []string{"t"}}) },
		"columns":    func() error { _, err := x.Columns(ctx, "files", "a.md"); return err },
		"spend":      func() error { return x.spend("2026-10-03", 1) },
		"matches":    func() error { _, err := x.aboutMatch(ctx, `"car"`, []string{"files"}, 5); return err },
		"abouts":     func() error { return x.abouts(ctx, []Hit{{Source: "files", Ref: "a.md"}}) },
		"known":      func() error { _, err := x.known("files"); return err },
		"counts":     func() error { _, err := x.counts(); return err },
		"embedded":   func() error { _, err := x.embedded(); return err },
		"enriched":   func() error { _, err := x.enriched(); return err },
		"unreadable": func() error { _, err := x.unreadable(); return err },
	} {
		assert.Error(t, call(), name)
	}
}

// A model change the database refuses to record fails Open.
func TestModelChangeRefused(t *testing.T) {
	for _, when := range []string{"BEFORE DELETE ON vectors", "BEFORE INSERT ON meta WHEN NEW.key = 'embedding_model'"} {
		t.Run(when, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			files := newSource("files")
			files.set("a.md", "car", t0)
			x, err := Open(dir, []Source{files}, nil, WithEmbedder(&fakeEmbedder{model: "fake/one"}))
			require.NoError(t, err)
			require.NoError(t, x.Scan(ctx))
			_, err = x.db.Exec(`DELETE FROM meta WHERE key = 'embedding_model'`)
			require.NoError(t, err)
			trigger(when)(t, x, dir)
			require.NoError(t, x.Close())
			_, err = Open(dir, []Source{files}, nil, WithEmbedder(&fakeEmbedder{model: "fake/two"}))
			assert.ErrorContains(t, err, "disk full")
		})
	}
}

// With the vectors read-only, a new item's vectors (in an existing
// collection, or a new source's) can't be stored: the pass says so.
func TestVectorsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	for _, newSource_ := range []bool{false, true} {
		ctx := context.Background()
		dir := t.TempDir()
		files, notes := newSource("files"), newSource("notes")
		files.set("a.md", "car", t0)
		x, err := Open(dir, []Source{files, notes}, nil, WithEmbedder(&fakeEmbedder{model: "fake/one"}))
		require.NoError(t, err)
		require.NoError(t, x.Scan(ctx))
		readOnly("vectors")(t, x, dir)
		if newSource_ {
			notes.set("n1", "cake", t0)
		} else {
			files.set("b.md", "cake", t0)
		}
		require.NoError(t, x.Scan(ctx))
		assert.NotEmpty(t, x.Status().EmbedErr, "new source: %v", newSource_)
		x.Close()
	}
}

// cancellingEmbedder cancels its context on its first call.
type cancellingEmbedder struct {
	fakeEmbedder
	cancel context.CancelFunc
}

func (c *cancellingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	c.cancel()
	return c.fakeEmbedder.Embed(ctx, texts)
}

// A scan cancelled while embedding stops there, its vectors kept for next
// time.
func TestEmbedCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	files := newSource("files")
	for i := range 3 * embedPassSize {
		files.set(fmt.Sprintf("f%03d.md", i), "car", t0)
	}
	emb := &cancellingEmbedder{fakeEmbedder: fakeEmbedder{model: "fake/one"}, cancel: cancel}
	x, err := Open(t.TempDir(), []Source{files}, nil, WithEmbedder(emb))
	require.NoError(t, err)
	defer x.Close()
	require.NoError(t, x.Scan(context.Background()))
	assert.Equal(t, 3*embedPassSize, x.Status().Embedded, "a scan of its own finishes")
	assert.ErrorIs(t, x.embedPending(ctx), context.Canceled)
}

// A damaged index (a table gone) fails the search that needs it, rather
// than answering wrongly.
func TestDamagedIndex(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ table, query string }{
		{"about", "car"},
		{"grams", "turnEnded"},
	} {
		t.Run(tc.table, func(t *testing.T) {
			files := newSource("files")
			files.set("a.md", "car turnEnded", t0)
			x := openTest(t, nil, files)
			require.NoError(t, x.Scan(ctx))
			_, err := x.db.Exec(`DROP TABLE ` + tc.table)
			require.NoError(t, err)
			_, err = x.Search(ctx, Query{Text: tc.query})
			assert.Error(t, err)
		})
	}
	t.Run("summaries", func(t *testing.T) {
		files := newSource("files")
		files.set("a.md", "car", t0)
		x := openTest(t, nil, files)
		require.NoError(t, x.Scan(ctx))
		_, err := x.db.Exec(`ALTER TABLE docs DROP COLUMN tags`)
		require.NoError(t, err)
		_, err = x.Search(ctx, Query{Text: "car"})
		assert.Error(t, err)
	})
}

// closingSource closes its index's database while an item is read.
type closingSource struct {
	*memSource
	x **Index
}

func (c closingSource) Read(ctx context.Context, ref string) (Doc, error) {
	(*c.x).db.Close()
	return c.memSource.Read(ctx, ref)
}

// A database gone in the middle of a scan fails it.
func TestScanDatabaseGone(t *testing.T) {
	files := newSource("files")
	files.set("a.md", "car", t0)
	var x *Index
	src := closingSource{memSource: files, x: &x}
	x, err := Open(t.TempDir(), []Source{src}, nil)
	require.NoError(t, err)
	assert.Error(t, x.Scan(context.Background()))
}
