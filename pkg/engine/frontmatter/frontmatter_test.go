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

func TestSplit(t *testing.T) {
	tests := []struct {
		name, in, front, body string
		ok                    bool
	}{
		{"front matter and a body", "---\na: 1\n---\n\nBody.\n", "\na: 1", "Body.", true},
		{"a byte order mark first", "\ufeff---\na: 1\n---\nB", "\na: 1", "B", true},
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
		f.Map("limits", map[string]any{"max_turns": 5, "commands": []string{"rm -rf *"}})
		f.Map("none", nil)
	})
	require.NoError(t, err)
	assert.Equal(t, "---\nkeep: yes\nwhen: after_agent\nagents: [reviewer, qa]\nrank: 7\non: true\nlimits:\n    commands:\n        - rm -rf *\n    max_turns: 5\n---\n\nNew body.\n", string(out))

	out, err = Edit(nil, "", func(f Fields) { f.String("name", "x") })
	require.NoError(t, err)
	assert.Equal(t, "---\nname: x\n---\n\n", string(out))

	_, err = Edit([]byte("---\n[unclosed\n---\n"), "", func(Fields) {})
	assert.Error(t, err)
	_, err = Edit([]byte("---\n- a list\n---\n"), "", func(Fields) {})
	assert.ErrorContains(t, err, "isn't a mapping")
}
