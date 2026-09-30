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

package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const writerSkill = `---
name: writer
execution_hints:
  writes_workspace: true
scripts:
  - name: edit
    language: python
    inline_code: |
      import os
      open("README.md", "w").write("rewritten\n")
      open("new.txt", "w").write("new\n")
      os.remove("gone.txt")
      os.makedirs("sub", exist_ok=True)
      open("sub/deep.txt", "w").write("deep\n")
  - name: broken
    language: python
    inline_code: |
      open("README.md", "w").write("half done\n")
      raise SystemExit(1)
---
`

// writerFixture is a workspace with checkpoints and a skill whose scripts
// write it, approving with d.
func writerFixture(t *testing.T, d api.Decision) (string, *SkillScripts, *Checkpoints, *[]api.ApprovalRequest) {
	t.Helper()
	skillsDir := t.TempDir()
	os.MkdirAll(filepath.Join(skillsDir, "writer"), 0o755)
	os.WriteFile(filepath.Join(skillsDir, "writer", "SKILL.md"), []byte(writerSkill), 0o644)
	prov, _ := skills.NewProvider()
	require.NoError(t, prov.DiscoverExternal([]string{skillsDir}))
	wsDir, _ := filepath.EvalSymlinks(t.TempDir())
	for name, body := range map[string]string{"README.md": "original\n", "gone.txt": "bye\n", ".git/config": "[core]\n", ".env": "SECRET=1\n"} {
		os.MkdirAll(filepath.Dir(filepath.Join(wsDir, name)), 0o755)
		os.WriteFile(filepath.Join(wsDir, name), []byte(body), 0o644)
	}
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: wsDir, BlockedPaths: config.DefaultBlockedPaths})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	cps := NewCheckpoints(ws, 0)
	hooks, reqs := decisionHooks(d)
	policy := config.DefaultConfig().Skills.Policy
	r := NewSkillScripts(prov, policy, ws, hooks, NewPyEnvs(filepath.Join(t.TempDir(), "envs"), policy.Packages),
		ScriptBoxConfig{Mode: os.Getenv("BLITZ_PYENV_SANDBOX"), Blocked: ws.Blocked(), StateDir: t.TempDir()})
	if _, err := r.Box(); err != nil {
		t.Skipf("no script sandbox: %v", err)
	}
	if _, err := SystemPython(); err != nil {
		t.Skip(err)
	}
	return wsDir, r, cps, reqs
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "<none>"
	}
	return string(b)
}

// A skill that writes the workspace works on a copy; its changes are kept
// once approved, diff shown, and /undo reverts them (BL-SK-02).
func TestScriptWritesWorkspace(t *testing.T) {
	dir, r, cps, reqs := writerFixture(t, api.DecisionOnce)
	cps.BeginTurn("s", 0, "use the writer")
	out := r.Run(context.Background(), RunSkillScriptInput{Skill: "writer", Script: "edit"})
	require.Equal(t, "", out.Error, "%+v", out)
	assert.Equal(t, []string{"README.md", "gone.txt", "new.txt", "sub/deep.txt"}, out.Changed)
	require.Len(t, *reqs, 1)
	req := (*reqs)[0]
	assert.True(t, req.MustAsk, "asked every time, as tier 3")
	assert.Empty(t, req.Key, "not rememberable")
	assert.Contains(t, req.Diff, "+rewritten")
	assert.Contains(t, req.Diff, "-bye")
	assert.Equal(t, "rewritten\n", read(t, dir, "README.md"))
	assert.Equal(t, "deep\n", read(t, dir, "sub/deep.txt"))
	assert.Equal(t, "<none>", read(t, dir, "gone.txt"))
	assert.Equal(t, "SECRET=1\n", read(t, dir, ".env"), "a blocked file isn't copied, so it can't change")

	_, err := cps.Undo(false)
	require.NoError(t, err)
	assert.Equal(t, "original\n", read(t, dir, "README.md"), "/undo reverts the script's changes")
	assert.Equal(t, "bye\n", read(t, dir, "gone.txt"))
	assert.Equal(t, "<none>", read(t, dir, "new.txt"))
}

func TestScriptWritesRefusedOrFailed(t *testing.T) {
	dir, r, _, reqs := writerFixture(t, api.DecisionDeny)
	out := r.Run(context.Background(), RunSkillScriptInput{Skill: "writer", Script: "edit"})
	assert.Contains(t, out.Error, "weren't kept", "%+v", out)
	assert.Empty(t, out.Changed)
	assert.Len(t, *reqs, 1)
	assert.Equal(t, "original\n", read(t, dir, "README.md"), "refused: the workspace is as it was")

	out = r.Run(context.Background(), RunSkillScriptInput{Skill: "writer", Script: "broken"})
	assert.Equal(t, 1, out.ExitCode)
	assert.Contains(t, out.Error, "the script failed")
	assert.Len(t, *reqs, 1, "a failed script's changes aren't offered")
	assert.Equal(t, "original\n", read(t, dir, "README.md"))
}

func TestActivateSaysTheSkillWrites(t *testing.T) {
	_, r, _, _ := writerFixture(t, api.DecisionOnce)
	p := config.DefaultConfig().Skills.Policy
	out := runTool(t, toolOf(t)(NewActivateSkillTool(r.provider, &p)), map[string]any{"skill_name": "writer"})
	assert.Equal(t, true, out["writes_workspace"], "%v", out)
}
