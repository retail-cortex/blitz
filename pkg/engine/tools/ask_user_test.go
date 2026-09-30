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

package tools

import (
	"context"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Several questions in one call, and multi-select ones.
func TestAskSeveralQuestions(t *testing.T) {
	type asked struct {
		q     string
		multi bool
	}
	var got []asked
	hooks := NewHooks(Policy{})
	hooks.SetUserPrompter(func(ctx context.Context, q string, options []string) (string, error) {
		got = append(got, asked{q, api.IsMultiSelect(ctx)})
		if api.IsMultiSelect(ctx) {
			return options[0] + "\n" + options[2] + "\n", nil
		}
		return "Postgres", nil
	})
	ask := toolOf(t)(NewAskUserQuestionTool(hooks))

	out := runTool(t, ask, map[string]any{"questions": []any{
		map[string]any{"question": "Which database?", "options": []any{"Postgres", "SQLite"}},
		map[string]any{"question": "Which checks?", "options": []any{"lint", "unit", "e2e"}, "multi_select": true},
	}})
	require.Empty(t, errOf(out))
	assert.Equal(t, []asked{{"Which database?", false}, {"Which checks?", true}}, got)
	answers := out["answers"].([]any)
	require.Len(t, answers, 2)
	assert.Equal(t, "Postgres", answers[0].(map[string]any)["answer"])
	assert.Equal(t, "lint, e2e", answers[1].(map[string]any)["answer"])
	assert.Equal(t, []any{"lint", "e2e"}, answers[1].(map[string]any)["selected"])

	out = runTool(t, ask, map[string]any{"question": "Which checks?", "options": []any{"lint", "unit", "e2e"}, "multi_select": true})
	assert.Equal(t, []any{"lint", "e2e"}, out["selected"])

	for _, tt := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"nothing to ask", map[string]any{}, "question cannot be empty"},
		{"an empty one of several", map[string]any{"questions": []any{map[string]any{"question": " "}}}, "question cannot be empty"},
		{"multi-select without options", map[string]any{"question": "Which?", "multi_select": true}, "multi_select needs options"},
		{"too many", map[string]any{"questions": func() []any {
			var qs []any
			for range maxQuestions + 1 {
				qs = append(qs, map[string]any{"question": "q"})
			}
			return qs
		}()}, "at most"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, errOf(runTool(t, ask, tt.args)), tt.want)
		})
	}
	assert.Equal(t, []string{"a", "b"}, splitSelected("a\n\n b \n"))
}
