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
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Model settings: an unknown key has no value, "" clears a number, and
// saving needs a model.
func TestModelSettingsEdges(t *testing.T) {
	var s ModelSettings
	_, ok := s.Get("nope")
	assert.False(t, ok)
	require.NoError(t, s.Set("temperature", "0.5"))
	require.NoError(t, s.Set("temperature", ""))
	assert.Nil(t, s.Temperature)
	_, err := SaveModelSettings(t.TempDir(), "  ", s)
	assert.ErrorContains(t, err, "no model name")
}

// sameValue compares decoded TOML with text as written.
func TestSameValue(t *testing.T) {
	cases := map[string]struct {
		v    any
		text string
		want bool
	}{
		"string":       {"high", `"high"`, true},
		"bad quote":    {"high", `high`, false},
		"int":          {int64(3), "3", true},
		"float":        {0.5, "0.5", true},
		"not a number": {int64(3), "x", false},
		"other type":   {true, "1", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, sameValue(tc.v, tc.text))
		})
	}
}

// removeEmptyTable drops a table with only comments, up to the next
// table, and leaves a missing table alone.
func TestRemoveEmptyTable(t *testing.T) {
	doc := "[a]\n# note\n[b]\nx = 1\n"
	assert.Equal(t, "# note\n[b]\nx = 1\n", removeEmptyTable(doc, "a"))
	assert.Equal(t, doc, removeEmptyTable(doc, "missing"))
	assert.Equal(t, doc, removeEmptyTable(doc, "b"), "b has a value")
}

// Agent pins need an agent's name.
func TestAgentModelNeedsName(t *testing.T) {
	_, err := SaveAgentModel(t.TempDir(), "", "x/y")
	assert.ErrorContains(t, err, "no agent name")
	_, err = UnpinAgentModel(t.TempDir(), "", false)
	assert.ErrorContains(t, err, "no agent name")
}

// AddMCPServer needs a name and one of command or URL, writes a command
// server whole, and refuses a name configured twice already.
func TestAddMCPServerEdges(t *testing.T) {
	keysEnv(t)
	_, err := AddMCPServer("", MCPServerConfig{Name: "x"})
	assert.ErrorContains(t, err, "needs a name")

	dir := ConfigDir("")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), []byte("[ui]\ntheme = \"dark\""), 0o600))
	path, err := AddMCPServer("", MCPServerConfig{Name: "db", Command: "npx", Args: []string{"db-mcp"}, Env: map[string]string{"A": "1"}, Prefix: "db_", Agents: []string{"qa"}})
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got struct {
		MCP MCPConfig `toml:"mcp"`
	}
	_, err = toml.Decode(string(data), &got)
	require.NoError(t, err)
	require.Len(t, got.MCP.Servers, 1)
	s := got.MCP.Servers[0]
	assert.Equal(t, []string{"db-mcp"}, s.Args)
	assert.Equal(t, map[string]string{"A": "1"}, s.Env)
	assert.Equal(t, "db_", s.Prefix)
	assert.Equal(t, []string{"qa"}, s.Agents)

	require.NoError(t, os.WriteFile(path, append(data, []byte("\n[[mcp.servers]]\nname = \"db\"\nurl = \"https://x\"\n")...), 0o600))
	_, err = AddMCPServer("", MCPServerConfig{Name: "db", URL: "https://y"})
	assert.Error(t, err, "two named db already")
}

// Problems on one line list errors first; lines are found by key, by
// table, or not at all.
func TestCheckHelpers(t *testing.T) {
	fakeRuleCheck(t)
	problems := CheckSettings("permissions = { mystery = 1, allow = [\"bad(x)\"] }\n")
	require.Len(t, problems, 2)
	assert.True(t, problems[0].Error, "the error first")
	assert.False(t, problems[1].Error)
	assert.Equal(t, 1, problems[0].Line)
	assert.Equal(t, 1, problems[1].Line)

	assert.Equal(t, 0, lineOf("a\nb", "c"))
	text := "# c\n[ui]\nnot a key\ntheme = 1\n"
	assert.Equal(t, 4, keyLine(text, toml.Key{"ui", "theme"}))
	assert.Equal(t, 0, keyLine(text, toml.Key{"llm", "x"}))
	assert.Equal(t, toml.Key{"a", `"b`}, splitKey(`a."b`), "an unterminated quote keeps the rest")
	assert.Equal(t, toml.Key{"a", "b.c"}, splitKey(`a."b.c"`))
}

// Reference helpers name and write the remaining kinds of value.
func TestReferenceHelpers(t *testing.T) {
	assert.Equal(t, "map[string]string", typeName(reflect.TypeOf(map[string]string{})))
	n := 3
	assert.Equal(t, "3", tomlValue(reflect.ValueOf(&n)))
}
