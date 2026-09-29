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

package config

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every setting is in the reference, with a description.
func TestReferenceDescribesEverySetting(t *testing.T) {
	ref := Reference()
	var undocumented []string
	for _, s := range ref {
		if s.Doc == "" {
			undocumented = append(undocumented, s.Key)
		}
	}
	assert.Empty(t, undocumented, "settings without a doc comment on their field")

	byKey := map[string]Setting{}
	for _, s := range ref {
		byKey[s.Key] = s
	}
	cases := []struct{ key, typ, def string }{
		{"llm", "table", ""},
		{"llm.provider", "string", `"gemini"`},
		{"llm.max_retries", "integer", "3"},
		{"blitz.temperature", "number", "0.2"},
		{"tools.max_parallel", "integer", "8"},
		{"skills.paths", "list of strings", `["~/.blitz/skills", "./skills", ".agents/skills"]`},
		{"permissions.read_only_defaults", "boolean", ""},
		{"llm.anthropic.api_key", "string", ""},
		{"agent_models.<agent>", "string", ""},
		{"model_settings.<model>.reasoning_effort", "string", ""},
		{"pricing.<model>.input_per_mtok", "number", ""},
		{"hooks.pre_tool[]", "array of tables", ""},
		{"mcp.servers[].env.<variable>", "string", ""},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			s, ok := byKey[c.key]
			require.True(t, ok, "not in the reference")
			assert.Equal(t, c.typ, s.Type)
			assert.Equal(t, c.def, s.Default)
		})
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret")
	assert.False(t, slices.ContainsFunc(ref, func(s Setting) bool { return s.Default == `"sk-secret"` }), "a key from the environment is shown")
}
