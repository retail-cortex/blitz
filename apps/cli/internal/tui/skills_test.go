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

package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillsListAndShow(t *testing.T) {
	app, _ := newCommandApp(t, "")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "gh", "scripts"), 0o755)
	os.WriteFile(filepath.Join(dir, "gh", "scripts", "list.py"), []byte("print(1)"), 0o644)
	os.WriteFile(filepath.Join(dir, "gh", "SKILL.md"), []byte(`---
name: gh-issues
description: Triage issues
version: "1.2"
license: MIT
tool_requirements:
  - {name: Bash, scopes: ["gh:*"], description: read issues}
execution_hints:
  environment_variables: [GITHUB_TOKEN, AWS_SECRET_ACCESS_KEY]
scripts:
  - name: list
    language: python
    relative_path: scripts/list.py
    dependencies: ["requests>=2.31", "git+https://x/y.git"]
  - name: fmt
    language: python
    inline_code: "print(2)"
    timeout_seconds: 30
---
`), 0o644)
	require.NoError(t, local(app).Skills().DiscoverExternal([]string{dir}))
	local(app).Config().Skills.Policy.EnvPassthrough = []string{"GITHUB_TOKEN"}
	run := func(cmd string) string {
		return captureStdout(t, func() { HandleCommand(context.Background(), cmd, app) })
	}

	assert.Contains(t, run("/skills list"), "2 scripts · TIER_2_AUDITED_WRITE · allowed by skills.policy", "list")
	out := run("/skills show gh-issues")
	for _, want := range []string{
		"1.2 · MIT", "Content: sha256:", "Needs Bash (gh:*): read issues", "approval tier TIER_2_AUDITED_WRITE",
		"Network: not needed, off", "Receives: GITHUB_TOKEN", "won't receive (skills.policy.env_passthrough): AWS_SECRET_ACCESS_KEY",
		"✗ list", "isn't a plain package requirement", "✓ fmt", "inline, 30s",
	} {
		assert.Contains(t, out, want, "show lacks %q:\n%s", want, out)
	}
	out = run("/skills show nope")
	assert.Contains(t, out, "No skill named nope", "unknown:\n%s", out)
	out = run("/skills show")
	assert.Contains(t, out, "Usage: /skills show", "usage:\n%s", out)
	local(app).Config().Skills.Policy.Languages = nil
	out = run("/skills list")
	assert.Contains(t, out, `2 scripts · blocked: language "python"`, "blocked list:\n%s", out)
}
