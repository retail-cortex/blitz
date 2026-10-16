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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

func TestRewriteWorker(t *testing.T) {
	ctx := context.Background()
	spec := api.WorkerSpec{
		Name: "news", Description: "AI news", Schedule: "Daily at 6 AM", Timezone: "America/Chicago",
		Permissions: []string{"web:*", "write:./daily-news/*"},
		Prompt:      "Using Google News find the latest AI news and write daily-news/<date>.md",
	}
	tests := []struct {
		name        string
		reply       string
		err         error
		wantPrompt  string
		wantPerms   []string
		wantChanges int
		wantProblem string
	}{
		{
			name:        "rewritten",
			reply:       `{"instructions": "## Sources\nFetch the feed once.", "permissions": ["web:news.google.com", "write:daily-news/*"], "changes": ["Named the feed", "Narrowed web access"]}`,
			wantPrompt:  "## Sources\nFetch the feed once.",
			wantPerms:   []string{"web:news.google.com", "write:daily-news/*"},
			wantChanges: 2,
		},
		{
			name:        "fenced, a bad and a repeated permission dropped",
			reply:       "Here it is:\n```json\n{\"instructions\": \"Do it.\", \"permissions\": [\"write:/etc/*\", \"nope\", \"write:./out/*\", \"write:out/*\"], \"changes\": [\"a\",\"b\",\"c\",\"d\",\"e\",\"f\"]}\n```",
			wantPrompt:  "Do it.",
			wantPerms:   []string{"write:out/*"},
			wantChanges: 5,
		},
		{name: "not a rewrite", reply: "Sure! I'd suggest naming the feed.", wantProblem: "wasn't a rewrite"},
		{name: "model fails", err: errors.New("quota exceeded"), wantProblem: "quota exceeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := openTest(t)
			llm := &promptLLM{reply: tt.reply, err: tt.err}
			var asked string
			w.newModel = func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
				asked = ref
				return llm, nil
			}
			got, err := w.RewriteWorker(ctx, spec)
			require.NoError(t, err)
			assert.Equal(t, w.cfg.Suggestions.Model, asked, "the suggestions model drafts it")
			if tt.wantProblem != "" {
				assert.Contains(t, got.Problem, tt.wantProblem)
				assert.Empty(t, got.Prompt)
				return
			}
			assert.Empty(t, got.Problem)
			assert.Equal(t, tt.wantPrompt, got.Prompt)
			assert.Equal(t, tt.wantPerms, got.Permissions)
			assert.Len(t, got.Changes, tt.wantChanges)
			require.Len(t, llm.prompts, 1)
			for _, want := range []string{"Daily at 6 AM", "America/Chicago", "web:*", "Using Google News"} {
				assert.Contains(t, llm.prompts[0], want, "the model sees the worker")
			}
		})
	}
}

func TestRewriteWorkerNeedsInstructions(t *testing.T) {
	w := openTest(t)
	_, err := w.RewriteWorker(context.Background(), api.WorkerSpec{Name: "x", Prompt: "  "})
	require.ErrorIs(t, err, ErrNoInstructions)
}

// A model that can't be built is a problem to show, not an error.
func TestRewriteWorkerWithoutAModel(t *testing.T) {
	w := openTest(t)
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no key") }
	got, err := w.RewriteWorker(context.Background(), api.WorkerSpec{Prompt: "Do it."})
	require.NoError(t, err)
	assert.Contains(t, got.Problem, "no key")
}
