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
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// failingEmbedder can't embed anything.
type failingEmbedder struct{}

func (failingEmbedder) Model() string { return "fake/failing" }

func (failingEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("quota")
}

// Near cases scored by embeddings: by meaning, the expected output's
// vector reused; an embedder that fails is each case's error, one that
// can't be built the run's.
func TestRunEvalEmbeddings(t *testing.T) {
	old := newEmbedder
	t.Cleanup(func() { newEmbedder = old })
	const vectors = "---\nnear: embedding\n---\n| ID | Prompt | Output | Output type | Threshold |\n|-|-|-|-|-|\n| a | Fly? | airship | Near | 0.8 |\n| b | Float? | airship | Near | 0.8 |\n"
	tests := []struct {
		name     string
		embedder func(context.Context, *config.Config, string) (search.Embedder, error)
		status   string
		want     string
	}{
		{"by meaning", func(context.Context, *config.Config, string) (search.Embedder, error) { return synonymEmbedder{}, nil }, api.EvalPassed, ""},
		{"an embedder that fails", func(context.Context, *config.Config, string) (search.Embedder, error) { return failingEmbedder{}, nil }, api.EvalFailed, "embedding the outputs"},
		{"none built", func(context.Context, *config.Config, string) (search.Embedder, error) {
			return nil, errors.New("no key")
		}, api.EvalFailed, "the embedding model fake/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newEmbedder = tt.embedder
			w, _ := openTestWith(t, nil, text("A blimp."), text("A dirigible."))
			w.cfg.Search.EmbeddingModel = "fake/x"
			writeEval(t, w, "vectors", vectors)
			run, err := w.RunEval(context.Background(), "vectors", api.EvalOptions{})
			require.NoError(t, err)
			assert.Equal(t, tt.status, run.Status, "%+v", run)
			if tt.want == "" {
				require.Len(t, run.Results, 2)
				assert.Greater(t, run.Results[1].Score, 0.8)
				return
			}
			all := run.Error
			for _, r := range run.Results {
				all += r.Reason
			}
			assert.Contains(t, all, tt.want)
		})
	}
}

// A judge that can't be had is the case's error; a run whose context is
// done stops before its cases.
func TestRunEvalJudgeMissingAndStopped(t *testing.T) {
	w, _ := openTestWith(t, nil, text("Lyon."))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no key") }
	writeEval(t, w, "near", "| ID | Prompt | Output |\n|-|-|-|\n| a | Capital? | Paris |\n")
	run, err := w.RunEval(context.Background(), "near", api.EvalOptions{})
	require.NoError(t, err)
	require.Len(t, run.Results, 1)
	assert.Equal(t, api.EvalError, run.Results[0].Status)
	assert.Contains(t, run.Results[0].Reason, "scoring")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run, err = w.RunEval(ctx, "near", api.EvalOptions{})
	require.NoError(t, err)
	assert.Equal(t, api.EvalStopped, run.Status)
	assert.Empty(t, run.Results)
}
