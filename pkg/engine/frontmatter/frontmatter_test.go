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

package frontmatter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The front matter runs from the newline after the opening fence, so its
// YAML's line numbers are the file's.
func TestSplit(t *testing.T) {
	tests := []struct {
		name, in, front, body string
		ok                    bool
	}{
		{"front matter and a body", "---\na: 1\n---\n\nBody.\n", "\na: 1\n", "Body.", true},
		{"a byte order mark first", "\ufeff---\na: 1\n---\nB", "\na: 1\n", "B", true},
		{"empty", "---\n---\nB", "\n", "B", true},
		{"CRLF lines", "---\r\na: 1\r\n---\r\nB", "\na: 1\r\n", "B", true},
		// N26 engine-sub-2#2: only a --- line ends it.
		{"--- inside a value", "---\nd: a --- b\nagents: [x]\n---\nB", "\nd: a --- b\nagents: [x]\n", "B", true},
		{"a longer rule isn't a fence", "---\na: 1\n----\nb: 2\n---  \nB", "\na: 1\n----\nb: 2\n", "B", true},
		{"not opened by a --- line", "---a: 1\n---\nB", "", "---a: 1\n---\nB", false},
		{"none", "Just text.", "", "Just text.", false},
		{"never closed", "---\na: 1\n", "", "---\na: 1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			front, body, ok := Split([]byte(tt.in))
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.front, string(front))
			assert.Equal(t, tt.body, string(body))
		})
	}
}

// Edit sets and removes the fields asked, keeps the others in order, and
// puts the body after a blank line.
func TestEdit(t *testing.T) {
	out, err := Edit([]byte("---\nkeep: yes\nwhen: before_agent\norder: 3\ntags: [a]\n---\nold\n"), "New body.", func(f Fields) {
		f.String("when", "after_agent")
		f.Int("order", 0)
		f.List("tags", nil)
		f.List("agents", []string{"reviewer", "qa"})
		f.Int("rank", 7)
		f.String("gone", "")
		f.Bool("on", true)
		f.Bool("off", false)
		f.Flag("enabled", false, true)
		f.Flag("usual", true, true)
		f.Map("limits", map[string]any{"max_turns": 5, "commands": []string{"rm -rf *"}})
		f.Map("none", nil)
	})
	require.NoError(t, err)
	assert.Equal(t, "---\nkeep: yes\nwhen: after_agent\nagents: [reviewer, qa]\nrank: 7\non: true\nenabled: false\nlimits:\n    commands:\n        - rm -rf *\n    max_turns: 5\n---\n\nNew body.\n", string(out))

	out, err = Edit(nil, "", func(f Fields) { f.String("name", "x") })
	require.NoError(t, err)
	assert.Equal(t, "---\nname: x\n---\n\n", string(out))

	_, err = Edit([]byte("---\n[unclosed\n---\n"), "", func(Fields) {})
	assert.Error(t, err)
	_, err = Edit([]byte("---\n- a list\n---\n"), "", func(Fields) {})
	assert.ErrorContains(t, err, "isn't a mapping")
}

func TestStrict(t *testing.T) {
	type doc struct {
		Name  string   `yaml:"name"`
		Tools []string `yaml:"tools"`
	}
	tests := []struct {
		name, in, problem string
		want              doc
	}{
		{name: "known keys", in: "name: a\ntools: [x]\n", want: doc{Name: "a", Tools: []string{"x"}}},
		{name: "empty", in: ""},
		{name: "a misspelt key", in: "name: a\ntool: [x]\n", problem: `line 2: unknown key "tool"`},
		{name: "two unknown keys", in: "nme: a\nmodel: b\n", problem: `line 1: unknown key "nme"; line 2: unknown key "model"`},
		{name: "a wrong type", in: "tools: {a: b}\n", problem: "cannot unmarshal"},
		{name: "not YAML", in: "name: [\n", problem: "yaml:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d doc
			err := Strict([]byte(tt.in), &d)
			if tt.problem != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.problem)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, d)
		})
	}
}
