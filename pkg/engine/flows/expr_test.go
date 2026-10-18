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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpressions(t *testing.T) {
	vars := Vars{
		Input:  "refund order 7",
		Output: &Result{Text: "approved", Data: map[string]any{"total": 150, "tags": []string{"vip"}}},
		Steps:  map[string]Result{"look-up": {Text: "found", Data: map[string]any{"name": "Ada"}}},
	}
	tests := []struct {
		expr string
		want bool
	}{
		{"output.data.total > 100", true},
		{`output.text.contains("approved")`, true},
		{`"vip" in output.data.tags`, true},
		{`steps["look-up"].data.name == "Ada"`, true},
		{`input.startsWith("refund")`, true},
		{"output.data.total < 100", false},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			require.NoError(t, CheckExpr(tt.expr))
			got, err := Holds(tt.expr, vars)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
	_, err := Holds("output.data.total", vars)
	assert.ErrorContains(t, err, "not true or false")
	_, err = Holds("output.data.missing > 1", vars)
	assert.Error(t, err, "no such key")
	assert.Error(t, CheckExpr("output.data.total >"), "a syntax error")
	assert.Error(t, CheckExpr("nope == 1"), "an unknown variable")

	got, err := Fill(`Hi {{ steps["look-up"].data.name }}, total {{ output.data.total }}, tags {{output.data.tags}}`, vars)
	require.NoError(t, err)
	assert.Equal(t, `Hi Ada, total 150, tags ["vip"]`, got)
	_, err = Fill("{{ output.data.missing }}", vars)
	assert.Error(t, err)
	assert.Error(t, CheckTemplate("{{ ) }}"))
	assert.NoError(t, CheckTemplate("no templates"))

	assert.Equal(t, map[string]any{"a": float64(1)}, DataOf("```json\n{\"a\": 1}\n```"))
	assert.Equal(t, []any{"x"}, DataOf(` ["x"] `))
	assert.Nil(t, DataOf("plain text"))
	assert.Nil(t, DataOf("{broken"))
	assert.Equal(t, map[string]any{"text": "t", "data": nil}, Result{Text: "t"}.Value())
}

func TestControlFlowChecks(t *testing.T) {
	tests := []struct{ name, in, problem string }{
		{"both conditions", "nodes: [{id: a, type: accumulate}, {id: b, type: accumulate}]\nedges: [{from: a, to: b, when: 'true', if: yes}]", "not both"},
		{"conditional default", "nodes: [{id: a, type: accumulate}, {id: b, type: accumulate}]\nedges: [{from: a, to: b, default: true, when: 'true'}]", "no condition"},
		{"default and error", "nodes: [{id: a, type: accumulate}, {id: b, type: accumulate}]\nedges: [{from: a, to: b, default: true, error: true}]", "not both"},
		{"bad when", "nodes: [{id: a, type: accumulate}, {id: b, type: accumulate}]\nedges: [{from: a, to: b, when: 'x >'}]", "when:"},
		{"two defaults", "nodes: [{id: a, type: accumulate}, {id: b, type: accumulate}, {id: c, type: accumulate}]\nedges: [{from: a, to: b, default: true}, {from: a, to: c, default: true}]", "one default link"},
		{"two error links", "nodes: [{id: a, type: accumulate}, {id: b, type: accumulate}, {id: c, type: accumulate}]\nedges: [{from: a, to: b, error: true}, {from: a, to: c, error: true}]", "one error link"},
		{"parallel with a condition", "nodes: [{id: a, type: parallel}, {id: b, type: accumulate}]\nedges: [{from: a, to: b, when: 'true'}]", "every link"},
		{"gateway with one way out", "nodes: [{id: a, type: exclusive}, {id: b, type: accumulate}]\nedges: [{from: a, to: b}]", "two links out"},
		{"extract without fields", "nodes: [{id: a, type: extract}]", "which fields"},
		{"bad field", "nodes: [{id: a, type: extract, fields: [{name: 1x}, {name: ok, type: date}]}]", `field "1x"`},
		{"field twice", "nodes: [{id: a, type: extract, fields: [{name: a}, {name: a}]}]", "twice"},
		{"bad field type", "nodes: [{id: a, type: extract, fields: [{name: ok, type: date}]}]", `type "date"`},
		{"bad retry", "nodes: [{id: a, type: accumulate, retry: {attempts: 20}}]", "retry attempts"},
		{"bad retry delay", "nodes: [{id: a, type: accumulate, retry: {attempts: 2, delay: soon}}]", "retry delay"},
		{"bad timeout", "nodes: [{id: a, type: accumulate, timeout: forever}]", "timeout"},
		{"bad template", "nodes: [{id: a, type: read_file, path: '{{ ) }}'}]", "path:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, Parse("x", "", []byte(tt.in)).Problem, tt.problem)
		})
	}
	ok := Parse("x", "", []byte("nodes: [{id: g, type: exclusive}, {id: b, type: accumulate}, {id: c, type: accumulate}]\nedges: [{from: g, to: b, when: 'output.data.n > 1', label: big}, {from: g, to: c, default: true}]"))
	assert.Empty(t, ok.Problem)
	assert.True(t, ok.Edges[0].Conditional())
	assert.True(t, IsGateway(Inclusive))
	assert.True(t, IsAgentType(Browser))
	_, err := ParseDuration("-1s", 0)
	assert.Error(t, err)
}
