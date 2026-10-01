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

package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMarkdownSpec(t *testing.T) {
	sample := `---
name: test-agent
display_name: "Test Agent 🤖"
description: "A test agent for unit testing"
agency_level: "extreme"
tools:
  - read_file
  - run_shell_command
---
You are {puppy_name}, working for {owner_name}.
{agency_instructions}
`

	spec, err := ParseMarkdownSpec([]byte(sample))
	require.NoError(t, err, "ParseMarkdownSpec failed")

	assert.Equal(t, "test-agent", spec.Name, "expected 'test-agent', got '%s'", spec.Name)
	assert.Equal(t, "Test Agent 🤖", spec.DisplayName, "expected 'Test Agent 🤖', got '%s'", spec.DisplayName)
	assert.Len(t, spec.Tools, 2, "unexpected tools: %v", spec.Tools)
	assert.Equal(t, "read_file", spec.Tools[0], "unexpected tools: %v", spec.Tools)

	interpolated := spec.InterpolatePrompt("extreme")
	assert.True(t, contains(interpolated, "EXTREME agency"), "expected extreme agency instructions, got: %s", interpolated)
}

func TestEmbeddedRegistry(t *testing.T) {
	reg, err := NewRegistry()
	require.NoError(t, err, "failed to create registry")

	list := reg.List()
	assert.GreaterOrEqual(t, len(list), 7, "expected at least 7 embedded agents, got %d", len(list))

	puppy, ok := reg.Get("blitz")
	require.True(t, ok, "expected to find 'blitz' agent")
	require.NotNil(t, puppy, "expected to find 'blitz' agent")

	assert.Equal(t, "Blitz", puppy.DisplayName, "expected 'Blitz', got '%s'", puppy.DisplayName)

	helios, ok := reg.Get("helios")
	require.True(t, ok, "expected to find 'helios' agent")
	require.NotNil(t, helios, "expected to find 'helios' agent")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && len(substr) > 0 && (s[:len(substr)] == substr || (len(s) > len(substr) && (s[len(s)-len(substr):] == substr || checkSubstr(s, substr)))))
}

func checkSubstr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestAgentRunDefaults(t *testing.T) {
	cases := []struct {
		name    string
		front   string
		want    AgentMetadata
		problem string
	}{
		{name: "none", front: "name: a", want: AgentMetadata{Name: "a"}},
		{name: "all", front: "name: a\npermission_mode: accept-edits\nmax_turns: 12\nbackground: true", want: AgentMetadata{Name: "a", PermissionMode: "accept-edits", MaxTurns: 12, Background: true}},
		{name: "an unknown mode", front: "name: a\npermission_mode: yolo", problem: "permission_mode"},
		{name: "negative turns", front: "name: a\nmax_turns: -1", problem: "max_turns"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec, err := ParseMarkdownSpec([]byte("---\n" + c.front + "\n---\nprompt"))
			if c.problem != "" {
				assert.ErrorContains(t, err, c.problem)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, spec.AgentMetadata)
		})
	}
}

// TestParseMarkdownSpecRejects checks that each malformed spec is refused
// with an error naming the problem.
func TestParseMarkdownSpecRejects(t *testing.T) {
	cases := map[string]struct{ spec, want string }{
		"no closing delimiter": {"---\nname: x\n", "closing"},
		"bad yaml":             {"---\nname: [x\n---\nbody", "YAML"},
		"no name":              {"---\ndescription: x\n---\nbody", "name"},
		"bad permission mode":  {"---\nname: x\npermission_mode: wild\n---\nbody", "permission_mode"},
		"negative max turns":   {"---\nname: x\nmax_turns: -1\n---\nbody", "max_turns"},
		"bad isolation":        {"---\nname: x\nisolation: vm\n---\nbody", "isolation"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseMarkdownSpec([]byte(tc.spec))
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// TestInterpolatePromptLevels checks each agency level's instructions,
// with "" meaning high.
func TestInterpolatePromptLevels(t *testing.T) {
	spec := &AgentSpec{SystemPrompt: "{agency_instructions}|{{agency_instructions}}"}
	cases := map[string]string{
		"low":     "LOW agency",
		"Medium":  "MEDIUM agency",
		"extreme": "EXTREME agency",
		"high":    "Complete the requested task autonomously",
		"":        "Complete the requested task autonomously",
		"unknown": "Complete the requested task autonomously",
	}
	for level, want := range cases {
		t.Run(level, func(t *testing.T) {
			got := spec.InterpolatePrompt(level)
			assert.Contains(t, got, want)
			assert.NotContains(t, got, "agency_instructions", "both placeholder forms are filled")
		})
	}
}
