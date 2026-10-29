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

	// /spec's and /skills update's agents: they interview and write, with
	// no shell.
	for _, name := range []string{"project-spec", "harness-update"} {
		t.Run(name, func(t *testing.T) {
			a, ok := reg.Get(name)
			require.True(t, ok)
			assert.True(t, reg.IsBuiltin(name))
			assert.Contains(t, a.Tools, "ask_user_question")
			assert.Contains(t, a.Tools, "create_file")
			assert.NotContains(t, a.Tools, "run_shell_command")
			assert.Contains(t, a.SystemPrompt, ".agents/specs/")
		})
	}

	// /reflect's agent: reads the evidence with its tool, writes the
	// experience only when chosen, with no shell (spec_harness_learning_048
	// HE-03).
	reflect, ok := reg.Get("harness-reflect")
	require.True(t, ok)
	assert.True(t, reg.IsBuiltin("harness-reflect"))
	for _, tool := range []string{"harness_evidence", "ask_user_question", "create_file", "replace_in_file"} {
		assert.Contains(t, reflect.Tools, tool)
	}
	assert.NotContains(t, reflect.Tools, "run_shell_command")
	for _, want := range []string{".agents/EXPERIENCE.md", "`## How to work`", "`## This project`", "`@EXPERIENCE.md`", "multi_select", "at most five", "60 entries", "Write only what was chosen", "then worked", "never guess the fix", "`.agents/scripts/`", "`scratch_script`"} {
		assert.Contains(t, reflect.SystemPrompt, want)
	}

	// Any kind of project, not only software: the spec agent asks the kind
	// and records it; setup and the updates read it.
	spec, _ := reg.Get("project-spec")
	for _, kind := range []string{"Software", "Research or science", "Data analysis", "Writing or documentation", "Something else", "`Kind: software|research|analysis|writing|other`", "Objectives"} {
		assert.Contains(t, spec.SystemPrompt, kind)
	}
	for _, name := range []string{"project-setup", "harness-update"} {
		a, _ := reg.Get(name)
		assert.Contains(t, a.SystemPrompt, "`Kind:`", "%s reads the kind", name)
		assert.Contains(t, a.SystemPrompt, "research", "%s knows research workspaces", name)
	}

	// /setup (and /init) leads with the specification: with none, the
	// setup agent runs the spec agent itself, in the foreground so its
	// questions reach the person, and carries on after it.
	setup, _ := reg.Get("project-setup")
	assert.Contains(t, setup.Tools, "invoke_agent")
	assert.Contains(t, setup.SystemPrompt, "invoke the `project-spec` agent")
	assert.Contains(t, setup.SystemPrompt, "not in the background")
	assert.Contains(t, setup.SystemPrompt, "Unattended, don't invoke it")
	assert.NotContains(t, setup.SystemPrompt, "Set up without a specification", "it no longer asks whether to")
	assert.Contains(t, spec.SystemPrompt, "When /setup invoked you", "the spec agent doesn't send people back to /setup")

	// What the harness learns (spec_harness_learning_048): setup starts
	// the experience file and its import, keeps how to work apart from
	// the project's facts, and offers rules and guardrails; the updates
	// read the evidence and cite it.
	for _, want := range []string{"`@EXPERIENCE.md`", "`## How to work` and `## This project`", `"How to work here"`, "**Rules and guardrails.**", "`builtin: limits`", "`paths:", "skip this step unattended"} {
		assert.Contains(t, setup.SystemPrompt, want)
	}
	update, _ := reg.Get("harness-update")
	assert.Contains(t, update.Tools, "harness_evidence")
	assert.Contains(t, update.SystemPrompt, "citing both a spec and the evidence first")

	// Blitz is the generalist; the coder is its coding specialist. Both
	// keep the working rules word for word: they're what makes the code
	// good, whoever writes it.
	coder, ok := reg.Get("coder")
	require.True(t, ok)
	assert.True(t, reg.IsBuiltin("coder"))
	assert.NotContains(t, puppy.SystemPrompt, "a coding agent")
	assert.Contains(t, puppy.Tools, "invoke_agent")
	assert.Contains(t, puppy.SystemPrompt, "goes to `coder`")
	assert.Contains(t, puppy.SystemPrompt, "Make a small change yourself")
	assert.Contains(t, puppy.SystemPrompt, "Never answer that you have no access, no history or no context: look.")
	assert.Contains(t, puppy.SystemPrompt, "When the user just talks (a greeting, small talk, a thank-you), answer in a sentence of plain text, with no tools.")
	for _, tool := range []string{"activate_skill", "list_or_search_skills", "run_shell_command", "edit", "lsp"} {
		assert.Contains(t, coder.Tools, tool)
	}
	assert.NotContains(t, coder.Tools, "invoke_agent", "a specialist doesn't hand on")
	assert.Contains(t, coder.SystemPrompt, "Load the stack's skills")
	for _, rule := range []string{
		"Be terse. No greetings, filler, jokes or commentary on yourself",
		"Hold to sound engineering: small, focused changes; DRY, YAGNI and clear names; tests alongside behaviour changes.",
		"Keep files under about 600 lines",
		"Read existing files with `read_file` before modifying them",
		"Prefer `edit` or `replace_in_file` over `create_file` for existing files. Keep diffs targeted",
		"never with `find`",
	} {
		assert.Contains(t, puppy.SystemPrompt, rule)
		assert.Contains(t, coder.SystemPrompt, rule)
	}
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
			assert.NotContains(t, got, "{", "double-brace placeholder leaves no stray braces")
		})
	}
}
