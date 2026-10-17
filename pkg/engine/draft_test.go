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

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// A draft comes from the suggestions model with the kind's brief and the
// item; a reply that isn't one, or a model that fails, is a problem.
func TestDraftManifest(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name, reply string
		err         error
		wantText    string
		wantChanges int
		wantProblem string
	}{
		{name: "drafted", reply: `{"text": "# Reviewer\nYou review.", "changes": ["Wrote it"]}`, wantText: "# Reviewer\nYou review.", wantChanges: 1},
		{name: "fenced, changes capped", reply: "```json\n{\"text\": \"x\", \"changes\": [\"1\",\"2\",\"3\",\"4\",\"5\",\"6\",\" \"]}\n```", wantText: "x", wantChanges: 5},
		{name: "not a draft", reply: "Sure, here are ideas.", wantProblem: "wasn't a draft"},
		{name: "model fails", err: errors.New("quota exceeded"), wantProblem: "quota exceeded"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := openTest(t)
			llm := &promptLLM{reply: tt.reply, err: tt.err}
			w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }
			got, err := w.DraftManifest(ctx, DraftRequest{Kind: "script", Name: "fetch", Description: "Fetch the feed", Current: "print(1)", Request: "add retries", Language: "python"})
			require.NoError(t, err)
			if tt.wantProblem != "" {
				assert.Contains(t, got.Problem, tt.wantProblem)
				assert.Empty(t, got.Text)
				return
			}
			assert.Equal(t, tt.wantText, got.Text)
			assert.Len(t, got.Changes, tt.wantChanges)
			require.Len(t, llm.prompts, 1)
			for _, want := range []string{"fetch", "Fetch the feed", "python", "add retries", "print(1)"} {
				assert.Contains(t, llm.prompts[0], want, "the model sees the item")
			}
		})
	}
}

func TestDraftManifestRefuses(t *testing.T) {
	w := openTest(t)
	_, err := w.DraftManifest(context.Background(), DraftRequest{Kind: "poem", Request: "x"})
	assert.ErrorContains(t, err, "no drafts")
	_, err = w.DraftManifest(context.Background(), DraftRequest{Kind: "agent"})
	assert.ErrorContains(t, err, "say what it should do")
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no key") }
	got, err := w.DraftManifest(context.Background(), DraftRequest{Kind: "agent", Description: "a reviewer"})
	require.NoError(t, err)
	assert.Contains(t, got.Problem, "no key")
}
