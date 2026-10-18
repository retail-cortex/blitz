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

package flows

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each step type's own settings, and the settings every step has, are
// checked before a workflow runs.
func TestStepSettingsChecks(t *testing.T) {
	form := &Form{Components: []map[string]any{{"id": "root", "component": "Column"}}}
	tests := []struct {
		name    string
		node    Node
		problem string
	}{
		{"extract without fields", Node{Type: Extract}, "say which fields to pull out"},
		{"call without a workflow", Node{Type: Call}, "name the workflow it runs"},
		{"call itself", Node{Type: Call, Workflow: "x"}, "can't run itself"},
		{"human task without a form", Node{Type: HumanTask}, "give its form"},
		{"human task without root", Node{Type: HumanTask, Form: &Form{Components: []map[string]any{{"id": "a"}}}}, "give its form"},
		{"timer without a time", Node{Type: Timer}, "how long it waits"},
		{"mcp without a tool", Node{Type: MCPCall, Server: "github"}, "name the MCP server and its tool"},
		{"api without a URL", Node{Type: APICall}, "give the URL"},
		{"a2a without a URL", Node{Type: A2ACall}, "give the URL"},
		{"bad template", Node{Type: ReadFile, Path: "{{ 1 + }}"}, "path"},
		{"bad query", Node{Type: APICall, URL: "u", Query: []Pair{{Name: "q", Value: "{{ ) }}"}}}, "query q"},
		{"bad header", Node{Type: APICall, URL: "u", Headers: []Pair{{Name: "h", Value: "{{ ) }}"}}}, "header h"},
		{"nameless header", Node{Type: APICall, URL: "u", Headers: []Pair{{Value: "v"}}}, "without a name"},
		{"bad arguments", Node{Type: MCPCall, Server: "s", Tool: "t", Arguments: map[string]any{"a": []any{"{{ ) }}"}}}, "arguments"},
		{"bad method", Node{Type: APICall, URL: "u", Method: "FETCH"}, `method "FETCH"`},
		{"body and json", Node{Type: APICall, URL: "u", Body: "b", JSON: map[string]any{"a": 1}}, "a body or JSON, not both"},
		{"bearer without a token", Node{Type: APICall, URL: "u", Auth: &Auth{Type: "bearer"}}, "needs a token"},
		{"basic without a user", Node{Type: APICall, URL: "u", Auth: &Auth{Type: "basic"}}, "needs a username"},
		{"api key without a value", Node{Type: APICall, URL: "u", Auth: &Auth{Type: "api_key", Name: "k"}}, "its name and value"},
		{"api key in the body", Node{Type: APICall, URL: "u", Auth: &Auth{Type: "api_key", Name: "k", Value: "v", In: "body"}}, "header or query"},
		{"oauth2 without a secret", Node{Type: APICall, URL: "u", Auth: &Auth{Type: "oauth2", TokenURL: "t", ClientID: "c"}}, "client secret"},
		{"unknown auth", Node{Type: APICall, URL: "u", Auth: &Auth{Type: "magic"}}, `auth "magic"`},
		{"bad auth template", Node{Type: APICall, URL: "u", Auth: &Auth{Type: "bearer", Token: "{{ ) }}"}}, "auth:"},
		{"bad script args", Node{Type: Script, Skill: "s", ScriptName: "x", Args: []string{"{{ ) }}"}}, "args:"},
		{"bad field name", Node{Type: Extract, Fields: []Field{{Name: "1x"}}}, `field "1x"`},
		{"field twice", Node{Type: Extract, Fields: []Field{{Name: "a"}, {Name: "a"}}}, `field "a" twice`},
		{"bad field type", Node{Type: Extract, Fields: []Field{{Name: "a", Type: "date"}}}, `type "date"`},
		{"gateway repeats", Node{Type: Exclusive, ForEach: "input"}, "a gateway doesn't repeat"},
		{"a person repeats", Node{Type: Timer, Wait: "1h", ForEach: "input"}, "a person or a timer doesn't repeat"},
		{"a timer retries", Node{Type: Timer, Wait: "1h", Retry: &Retry{Attempts: 2}}, "a person or a timer isn't tried again"},
		{"bad for_each", Node{Type: Accumulate, ForEach: "1 +"}, "for_each"},
		{"bad wait", Node{Type: Timer, Wait: "soon"}, "wait:"},
		{"bad until", Node{Type: Timer, Until: "tomorrow"}, `until "tomorrow"`},
		{"too concurrent", Node{Type: Accumulate, Concurrency: 40}, "concurrency 40"},
		{"retry attempts", Node{Type: Accumulate, Retry: &Retry{Attempts: 20}}, "retry attempts 20"},
		{"retry delay", Node{Type: Accumulate, Retry: &Retry{Attempts: 2, Delay: "later"}}, "retry delay"},
		{"bad timeout", Node{Type: Accumulate, Timeout: "a while"}, "timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.node.ID = "a"
			probs := Check(Flow{Name: "x", Nodes: []Node{tt.node}})
			assert.NotEmpty(t, probs)
			assert.Contains(t, joined(probs), tt.problem)
		})
	}
	ok := Check(Flow{Name: "x", Nodes: []Node{
		{ID: "a", Type: HumanTask, Form: form},
		{ID: "b", Type: Timer, Until: "2026-10-20T09:00:00Z"},
		{ID: "c", Type: APICall, URL: "u", Auth: &Auth{Type: "oauth2", TokenURL: "t", ClientID: "c", ClientSecret: "s"}},
	}, Edges: []Edge{{From: "a", To: "b"}, {From: "b", To: "c"}}})
	assert.Empty(t, ok)
}

func joined(probs []string) string {
	s := ""
	for _, p := range probs {
		s += p + "\n"
	}
	return s
}

// Save refuses an edited workflow that isn't there, and reports what it
// can't write.
func TestSaveErrors(t *testing.T) {
	dir := t.TempDir()
	f := Flow{Name: "a", Nodes: []Node{{ID: "x", Type: Accumulate}}}
	_, _, err := Save(dir, f, "../bad")
	assert.ErrorIs(t, err, ErrUnknown, "a previous name that isn't one")

	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	_, _, err = Save(filepath.Join(file, "sub"), f, "")
	assert.Error(t, err, "a folder that can't be made")

	ro := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(ro, 0o555))
	t.Cleanup(func() { os.Chmod(ro, 0o755) })
	if _, err := os.Create(filepath.Join(ro, "probe")); err == nil {
		t.Skip("running as a user that writes anywhere")
	}
	_, _, err = Save(ro, f, "")
	assert.Error(t, err, "a folder that can't be written")
}

// Expressions that don't compile, values that aren't lists, and templates
// in nested values fail with the expression that failed.
func TestExpressionErrors(t *testing.T) {
	vars := Vars{Input: "in", Item: map[string]any{"n": 2}, Index: 1}
	_, err := Eval("1 +", vars)
	assert.Error(t, err, "a syntax error")
	_, err = Eval("1 / 0", vars)
	assert.Error(t, err, "a runtime error")
	assert.Nil(t, plain(make(chan int)), "what isn't JSON")

	assert.Error(t, checkValue(map[string]any{"a": "{{ ) }}"}))
	assert.Error(t, checkValue([]any{"ok", "{{ ) }}"}))
	assert.NoError(t, checkValue(map[string]any{"a": []any{"{{ input }}", 3}}))

	tests := []struct {
		name string
		in   any
		want any
		err  bool
	}{
		{"a template alone keeps its type", "{{ item }}", map[string]any{"n": float64(2)}, false},
		{"text around it", "#{{ index }}", "#1", false},
		{"a map", map[string]any{"a": "{{ index + 1 }}"}, map[string]any{"a": float64(2)}, false},
		{"a list", []any{"{{ input }}", 5}, []any{"in", 5}, false},
		{"anything else", 7, 7, false},
		{"a failing template", "{{ 1 / 0 }}", nil, true},
		{"a failing map", map[string]any{"a": "{{ 1 / 0 }}"}, nil, true},
		{"a failing list", []any{"{{ 1 / 0 }}"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FillValue(tt.in, vars)
			if tt.err {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	_, err = List("1 / 0", vars)
	assert.Error(t, err)
	_, err = List("input", vars)
	assert.ErrorContains(t, err, "not a list")
	items, err := List("[1, 2]", vars)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	got, err := Fill("{{ {'a': [1]} }}", vars)
	require.NoError(t, err)
	assert.Equal(t, `{"a":[1]}`, got)
}
