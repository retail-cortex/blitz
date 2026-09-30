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

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func read(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(data)
}

func TestImportClaude(t *testing.T) {
	src := files(t, t.TempDir(), map[string]string{
		".claude-plugin/plugin.json": `{"name": "Review Kit", "version": "2.0.1", "description": "Reviews", "author": {"name": "Ada"}}`,
		"commands/review.md":         "---\ndescription: Review\n---\nRun ${CLAUDE_PLUGIN_ROOT}/scripts/review.sh on $ARGUMENTS\n",
		"agents/reviewer.md":         "---\nname: reviewer\ndescription: Reviews code\ntools: Read, Grep, Bash(git diff *), Teleport\nmodel: sonnet\ncolor: blue\n---\nYou review.\n",
		"skills/diff/SKILL.md":       "---\nname: diff\ndescription: Diffs\n---\nDiff.\n",
		"hooks/hooks.json": `{"hooks": {
			"PreToolUse": [{"matcher": "Edit|Write|Frobnicate", "hooks": [{"type": "command", "command": "${CLAUDE_PLUGIN_ROOT}/check.sh", "timeout": 10}]}],
			"UserPromptSubmit": [{"hooks": [{"type": "prompt", "prompt": "x"}]}],
			"Elsewhere": [{"hooks": [{"type": "command", "command": "x"}]}]
		}}`,
		".mcp.json":              `{"mcpServers": {"reviewdb": {"command": "${CLAUDE_PLUGIN_ROOT}/db", "args": ["--ro"]}, "old": {"type": "sse", "url": "https://x/sse"}}}`,
		"output-styles/terse.md": "x",
	})
	out := filepath.Join(t.TempDir(), "review-kit")
	r, err := ImportClaude(src, out)
	require.NoError(t, err)
	assert.Equal(t, Meta{Name: "review-kit", Version: "2.0.1", Description: "Reviews", Author: "Ada"}, r.Plugin)

	p, err := Read(out)
	require.NoError(t, err, "the result loads")
	assert.Equal(t, []string{"review"}, p.Commands)
	assert.Equal(t, []string{"diff"}, p.Skills)
	assert.Equal(t, []string{"reviewer"}, p.Agents)
	require.Len(t, p.Hooks.PreTool, 2, "Edit and Write")
	assert.Equal(t, "replace_in_file", p.Hooks.PreTool[0].Match)
	assert.Equal(t, "create_file", p.Hooks.PreTool[1].Match)
	assert.Equal(t, RootVar+"/check.sh", p.Hooks.PreTool[0].Command)
	assert.Equal(t, 10, p.Hooks.PreTool[0].TimeoutSeconds)
	require.Len(t, p.MCP, 1)
	assert.Equal(t, RootVar+"/db", p.MCP[0].Command)
	assert.Contains(t, read(t, filepath.Join(out, "commands", "review.md")), RootVar+"/scripts/review.sh")

	agent := read(t, filepath.Join(out, "agents", "reviewer.md"))
	assert.Contains(t, agent, "- read_file\n    - grep\n    - run_shell_command")
	assert.NotContains(t, agent, "color")

	skipped := strings.Join(r.Skipped, "\n")
	for _, want := range []string{"Frobnicate", "UserPromptSubmit hook of type prompt", "hook event Elsewhere", "old: SSE", "Teleport", `model "sonnet"`, "output-styles"} {
		assert.Contains(t, skipped, want)
	}

	_, err = ImportClaude(src, out)
	assert.ErrorContains(t, err, "isn't empty")
	_, err = ImportClaude(t.TempDir(), filepath.Join(t.TempDir(), "x"))
	assert.ErrorContains(t, err, "not a Claude Code plugin")
}

func TestImportGemini(t *testing.T) {
	src := files(t, t.TempDir(), map[string]string{
		"gemini-extension.json": `{"name": "gcp-tools", "version": "0.3.0", "contextFileName": "GEMINI.md", "excludeTools": ["run_shell_command(rm)"],
			"mcpServers": {"gcloud": {"command": "node", "args": ["${extensionPath}/server.js"], "timeout": 30000, "cwd": "."},
			               "remote": {"httpUrl": "https://mcp.example/"}, "legacy": {"url": "https://mcp.example/sse"}}}`,
		"commands/deploy.toml":   "description = \"Deploy\"\nprompt = \"Deploy {{args}} now. !{git status}\"\n",
		"commands/gcs/list.toml": "prompt = \"List the buckets\"\n",
		"commands/broken.toml":   "description = \"no prompt\"\n",
		"GEMINI.md":              "context",
	})
	out := filepath.Join(t.TempDir(), "gcp")
	r, err := ImportGemini(src, out)
	require.NoError(t, err)
	p, err := Read(out)
	require.NoError(t, err)
	assert.Equal(t, "gcp-tools", p.Name)
	assert.ElementsMatch(t, []string{"deploy", "gcs-list"}, p.Commands)
	assert.Contains(t, read(t, filepath.Join(out, "commands", "deploy.md")), "Deploy $ARGUMENTS now.")
	require.Len(t, p.MCP, 2)
	assert.Equal(t, []string{RootVar + "/server.js"}, p.MCP[0].Args)
	assert.Equal(t, 30, p.MCP[0].TimeoutSeconds)
	assert.Equal(t, "https://mcp.example/", p.MCP[1].URL)
	skipped := strings.Join(r.Skipped, "\n")
	for _, want := range []string{"legacy: SSE", "gcloud: cwd", "broken.toml: no prompt", "!{…}", "context file", "excludeTools"} {
		assert.Contains(t, skipped, want)
	}
	_, err = ImportGemini(t.TempDir(), filepath.Join(t.TempDir(), "x"))
	assert.ErrorContains(t, err, "not a Gemini CLI extension")
}

func TestToolMatches(t *testing.T) {
	for _, tt := range []struct {
		matcher        string
		want, unmapped []string
	}{
		{"", []string{""}, nil},
		{"*", []string{""}, nil},
		{"Bash", []string{"run_shell_command"}, nil},
		{"Edit|MultiEdit|Write", []string{"replace_in_file", "create_file"}, nil},
		{"mcp__github__.*", []string{"github__*"}, nil},
		{"Nope", nil, []string{"Nope"}},
	} {
		t.Run(tt.matcher, func(t *testing.T) {
			got, unmapped := toolMatches(tt.matcher)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.unmapped, unmapped)
		})
	}
}
