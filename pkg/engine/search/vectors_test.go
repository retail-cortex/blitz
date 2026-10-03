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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEmbedder embeds by meaning, crudely: each word counts toward its
// concept's dimension, synonyms toward the same one.
type fakeEmbedder struct {
	model string

	mu    sync.Mutex
	calls int
	texts int
	fail  error
}

var concepts = map[string]int{
	"car": 0, "automobile": 0, "vehicle": 0,
	"cake": 1, "dessert": 1, "pastry": 1,
	"river": 2, "stream": 2, "water": 2,
}

func (f *fakeEmbedder) Model() string { return f.model }

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	f.calls++
	f.texts += len(texts)
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, 4)
		v[3] = 0.01 // never the zero vector
		for _, w := range strings.Fields(strings.ToLower(t)) {
			if c, ok := concepts[strings.Trim(w, ".,")]; ok {
				v[c]++
			}
		}
		out[i] = v
	}
	return out, nil
}

func (f *fakeEmbedder) setFail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = err
}

func (f *fakeEmbedder) embeddedTexts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.texts
}

// Chunks are embedded after each scan; semantic search finds by meaning
// what keywords miss, and hybrid (the default with an embedder) fuses
// both.
func TestSemanticSearch(t *testing.T) {
	ctx := context.Background()
	files := newSource("files")
	files.set("garage.md", "The car is parked in the garage.", t0)
	files.set("bakery.md", "A pastry shop on the corner.", t0)
	files.set("bridge.md", "The bridge crosses the stream.", t0)
	emb := &fakeEmbedder{model: "fake/one"}
	x, err := Open(t.TempDir(), []Source{files}, nil, WithEmbedder(emb))
	require.NoError(t, err)
	t.Cleanup(func() { x.Close() })
	require.NoError(t, x.Scan(ctx))
	st := x.Status()
	assert.Equal(t, 3, st.Embedded)
	assert.Equal(t, "fake/one", st.EmbeddingModel)
	assert.Empty(t, st.EmbedErr)

	for _, tc := range []struct {
		q     Query
		first string
		none  bool
	}{
		{q: Query{Text: "automobile", Mode: ModeKeyword}, none: true},
		{q: Query{Text: "automobile", Mode: ModeSemantic}, first: "garage.md"},
		{q: Query{Text: "dessert"}, first: "bakery.md"},
		{q: Query{Text: "bridge"}, first: "bridge.md"},
	} {
		hits, err := x.Search(ctx, tc.q)
		require.NoError(t, err)
		if tc.none {
			assert.Empty(t, hits, "%+v", tc.q)
			continue
		}
		require.NotEmpty(t, hits, "%+v", tc.q)
		assert.Equal(t, tc.first, hits[0].Ref, "%+v", tc.q)
	}

	before := emb.embeddedTexts()
	require.NoError(t, x.Scan(ctx))
	assert.Equal(t, before, emb.embeddedTexts(), "a scan with nothing changed embeds nothing")

	files.remove("bakery.md")
	files.set("garage.md", "The vehicle is out.", t0.Add(time.Second))
	require.NoError(t, x.Scan(ctx))
	assert.Equal(t, 2, x.Status().Embedded, "the gone chunk's vector removed, the changed one's replaced")
	assert.Equal(t, 2, x.vectors.GetCollection("files", nil).Count(), "two vectors left")
	hits, err := x.Search(ctx, Query{Text: "dessert", Mode: ModeSemantic})
	require.NoError(t, err)
	assert.NotContains(t, refs(hits), "bakery.md")
}

// Searching by meaning needs an embedder; a mode that doesn't exist is
// refused.
func TestSearchModes(t *testing.T) {
	ctx := context.Background()
	files := newSource("files")
	files.set("a.txt", "words", t0)
	x := openTest(t, nil, files)
	require.NoError(t, x.Scan(ctx))
	_, err := x.Search(ctx, Query{Text: "words", Mode: ModeSemantic})
	assert.ErrorIs(t, err, ErrNoEmbeddings)
	_, err = x.Search(ctx, Query{Text: "words", Mode: ModeHybrid})
	assert.ErrorIs(t, err, ErrNoEmbeddings)
	_, err = x.Search(ctx, Query{Text: "words", Mode: "psychic"})
	assert.ErrorIs(t, err, ErrUnknownMode)
	hits, err := x.Search(ctx, Query{Text: "words"})
	require.NoError(t, err)
	assert.Len(t, hits, 1, "keyword, without an embedder")
}

// Another model's vectors are thrown away and every chunk embedded again;
// an embedder that fails is reported, keywords still work, and the next
// scan goes on where it stopped.
func TestEmbeddingModelChangeAndFailure(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	files := newSource("files")
	files.set("a.md", "car", t0)
	files.set("b.md", "cake", t0)

	one := &fakeEmbedder{model: "fake/one"}
	x, err := Open(dir, []Source{files}, nil, WithEmbedder(one))
	require.NoError(t, err)
	require.NoError(t, x.Scan(ctx))
	require.Equal(t, 2, x.Status().Embedded)
	require.NoError(t, x.Close())

	two := &fakeEmbedder{model: "fake/two"}
	two.setFail(errors.New("quota exceeded"))
	x, err = Open(dir, []Source{files}, nil, WithEmbedder(two))
	require.NoError(t, err)
	defer x.Close()
	assert.Equal(t, 0, x.Status().Embedded, "the old model's vectors are gone")
	require.NoError(t, x.Scan(ctx), "an embedding failure doesn't fail the scan")
	st := x.Status()
	assert.Contains(t, st.EmbedErr, "quota exceeded")
	hits, err := x.Search(ctx, Query{Text: "car", Mode: ModeKeyword})
	require.NoError(t, err)
	assert.Len(t, hits, 1)
	_, err = x.Search(ctx, Query{Text: "car"})
	assert.ErrorContains(t, err, "quota exceeded", "the query can't be embedded either")

	two.setFail(nil)
	require.NoError(t, x.Scan(ctx))
	assert.Equal(t, 2, x.Status().Embedded)
	assert.Empty(t, x.Status().EmbedErr)
}
