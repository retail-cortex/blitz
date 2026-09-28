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
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const scriptSkill = `---
name: demo
execution_hints:
  environment_variables: [CP_PASSED, CP_WITHHELD]
scripts:
  - name: work
    language: python
    relative_path: scripts/work.py
    environment_variables: {GREETING: hi}
  - name: entry
    language: python
    relative_path: scripts/work.py
    entry_point: main
  - name: inline
    language: python
    inline_code: "import sys; print('inline', sys.argv[1:])"
  - name: slow
    language: python
    inline_code: "import time; time.sleep(30)"
    timeout_seconds: 1
  - name: ts
    language: typescript
    inline_code: "console.log(1)"
  - name: needs-pkgs
    language: python
    inline_code: "import six"
    dependencies: ["six==1.16.0"]
---
`

const workPy = `import os, sys
print("args", sys.argv[1:])
print("readme", open("README.md").read().strip())
print("env", os.environ.get("GREETING"), os.environ.get("CP_PASSED"), os.environ.get("CP_WITHHELD"), os.environ.get("CP_SECRET"))
try:
    open("README.md", "w").write("overwritten")
    print("workspace write: ALLOWED")
except OSError as e:
    print("workspace write: refused")
out = os.environ.get("SKILL_OUTPUT")
if out:
    open(os.path.join(out, "result.txt"), "w").write("done")
print("output", bool(out))

def main():
    print("entry point")
    return 3

if __name__ == "__main__":
    pass
`

type scriptFixture struct {
	ws     string
	runner *SkillScripts
	reqs   *[]api.ApprovalRequest
}

func newScriptFixture(t *testing.T, tier string, approve bool, edit func(*config.SkillPolicy)) scriptFixture {
	t.Helper()
	skillsDir := t.TempDir()
	dir := filepath.Join(skillsDir, "demo")
	os.MkdirAll(filepath.Join(dir, "scripts"), 0o755)
	doc := scriptSkill
	if tier != "" {
		doc = strings.Replace(doc, "execution_hints:\n", "execution_hints:\n  hitl_tier: "+tier+"\n", 1)
	}
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(doc), 0o644)
	os.WriteFile(filepath.Join(dir, "scripts", "work.py"), []byte(workPy), 0o644)
	prov, _ := skills.NewProvider()
	require.NoError(t, prov.DiscoverExternal([]string{skillsDir}))

	wsDir, _ := filepath.EvalSymlinks(t.TempDir())
	os.WriteFile(filepath.Join(wsDir, "README.md"), []byte("original"), 0o644)
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: wsDir})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	policy := config.DefaultConfig().Skills.Policy
	policy.EnvPassthrough = []string{"CP_PASSED"}
	if edit != nil {
		edit(&policy)
	}
	hooks, reqs := approverHooks(approve)
	r := NewSkillScripts(prov, policy, ws, hooks, NewPyEnvs(filepath.Join(t.TempDir(), "envs"), policy.Packages),
		ScriptBoxConfig{Mode: os.Getenv("BLITZ_PYENV_SANDBOX"), Blocked: ws.Blocked(), StateDir: t.TempDir()})
	if _, err := r.Box(); err != nil {
		t.Skipf("no script sandbox: %v", err)
	}
	if _, err := SystemPython(); err != nil {
		t.Skip(err)
	}
	return scriptFixture{ws: wsDir, runner: r, reqs: reqs}
}

func TestRunSkillScriptTier2(t *testing.T) {
	f := newScriptFixture(t, "", true, nil)
	t.Setenv("CP_PASSED", "passed")
	t.Setenv("CP_WITHHELD", "withheld")
	t.Setenv("CP_SECRET", "secret")
	out := f.runner.Run(context.Background(), RunSkillScriptInput{Skill: "demo", Script: "work", Args: []string{"a b", "c"}})
	require.Equal(t, "", out.Error, "%+v", out)
	require.Equal(t, 0, out.ExitCode, "%+v", out)
	for _, want := range []string{"args ['a b', 'c']", "readme original", "env hi passed None None", "workspace write: refused", "output True"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, out.Stdout, want, "stdout lacks %q:\n%s\n%s", want, out.Stdout, out.Stderr)
		})
	}
	b, _ := os.ReadFile(filepath.Join(f.ws, "README.md"))
	require.Equal(t, "original", string(b), "the script changed the workspace: %q", b)
	assert.Equal(t, "TIER_2_AUDITED_WRITE", out.Tier, "tier 2 should run without asking: %s, %d prompts", out.Tier, len(*f.reqs))
	assert.Len(t, *f.reqs, 0, "tier 2 should run without asking: %s, %d prompts", out.Tier, len(*f.reqs))
	require.True(t, strings.HasPrefix(out.OutputDir, SkillOutputDir+"/demo/work-"), "output: %q %v", out.OutputDir, out.Files)
	require.Len(t, out.Files, 1, "output: %q %v", out.OutputDir, out.Files)
	require.True(t, strings.HasSuffix(out.Files[0], "result.txt"), "output: %q %v", out.OutputDir, out.Files)
	b, _ = os.ReadFile(filepath.Join(f.ws, out.Files[0]))
	require.Equal(t, "done", string(b), "result file: %q", b)
}

func TestRunSkillScriptEntryInlineAndTier1(t *testing.T) {
	f := newScriptFixture(t, "TIER_1_AUTO_READ", true, func(p *config.SkillPolicy) { p.MinHITLTier = 1 })
	out := f.runner.Run(context.Background(), RunSkillScriptInput{Skill: "demo", Script: "entry"})
	require.Equal(t, 3, out.ExitCode, "entry point: %+v", out)
	require.Contains(t, out.Stdout, "entry point", "entry point: %+v", out)
	require.False(t, strings.Contains(out.Stdout, "args") && strings.Contains(out.Stdout, "__main__"), "entry point: %+v", out)
	assert.Equal(t, "", out.OutputDir, "tier 1 got an output directory: %+v", out)
	assert.Contains(t, out.Stdout, "output False", "tier 1 got an output directory: %+v", out)
	out = f.runner.Run(context.Background(), RunSkillScriptInput{Skill: "demo", Script: "inline", Args: []string{"x"}})
	require.Equal(t, 0, out.ExitCode, "inline: %+v", out)
	require.Contains(t, out.Stdout, "inline ['x']", "inline: %+v", out)
}

func TestRunSkillScriptTier3AsksEveryTime(t *testing.T) {
	f := newScriptFixture(t, "TIER_3_MANDATORY_APPROVAL", false, nil)
	out := f.runner.Run(context.Background(), RunSkillScriptInput{Skill: "demo", Script: "inline"})
	require.Contains(t, out.Error, "not approved", "tier 3 denied: %+v %+v", out, *f.reqs)
	require.Len(t, *f.reqs, 1, "tier 3 denied: %+v %+v", out, *f.reqs)
	require.Equal(t, "", (*f.reqs)[0].Key, "tier 3 denied: %+v %+v", out, *f.reqs)
}

func TestRunSkillScriptRefusals(t *testing.T) {
	f := newScriptFixture(t, "", false, nil)
	for script, want := range map[string]string{
		"ts":      `language "typescript" isn't in skills.policy.languages`,
		"missing": `has no script "missing"`,
	} {
		t.Run(script, func(t *testing.T) {
			out := f.runner.Run(context.Background(), RunSkillScriptInput{Skill: "demo", Script: script})
			assert.Contains(t, out.Error, want, "%s: %+v", script, out)
		})
	}
	out := f.runner.Run(context.Background(), RunSkillScriptInput{Skill: "nope", Script: "x"})
	assert.Contains(t, out.Error, "no skill", "unknown skill: %+v", out)
	// Installing packages needs its own approval, rememberable per package list.
	out = f.runner.Run(context.Background(), RunSkillScriptInput{Skill: "demo", Script: "needs-pkgs"})
	require.Contains(t, out.Error, "not approved", "install: %+v %+v", out, *f.reqs)
	require.Len(t, *f.reqs, 1, "install: %+v %+v", out, *f.reqs)
	r := (*f.reqs)[0]
	assert.Equal(t, api.ActionNetwork, r.Kind, "install approval: %+v", r)
	assert.True(t, strings.HasPrefix(r.Key, "pyenv:"), "install approval: %+v", r)
	assert.Contains(t, r.Detail, "six==1.16.0", "install approval: %+v", r)
	assert.Contains(t, r.Detail, "--only-binary :all:", "install approval: %+v", r)
}

func TestRunSkillScriptTimeout(t *testing.T) {
	f := newScriptFixture(t, "", true, nil)
	out := f.runner.Run(context.Background(), RunSkillScriptInput{Skill: "demo", Script: "slow"})
	require.True(t, out.TimedOut, "%+v", out)
	require.Contains(t, out.Error, "timeout", "%+v", out)
}

func TestActivateSkillListsScripts(t *testing.T) {
	f := newScriptFixture(t, "", true, nil)
	p := config.DefaultConfig().Skills.Policy
	skill, _ := f.runner.provider.Get("demo")
	ev := skills.Evaluate(skill, p)
	allowed, blocked := 0, 0
	for _, v := range ev.Scripts {
		if v.Allowed {
			allowed++
		} else {
			blocked++
		}
	}
	require.Equal(t, 5, allowed, "allowed %d, blocked %d", allowed, blocked)
	require.Equal(t, 1, blocked, "allowed %d, blocked %d", allowed, blocked)
}

// The whole path with packages: approval, build, and a run that imports
// them. Needs the network: BLITZ_PYENV_TESTS=1.
func TestRunSkillScriptInstallsPackages(t *testing.T) {
	if os.Getenv("BLITZ_PYENV_TESTS") != "1" {
		t.Skip("set BLITZ_PYENV_TESTS=1 (needs the network)")
	}
	f := newScriptFixture(t, "", true, nil)
	out := f.runner.Run(context.Background(), RunSkillScriptInput{Skill: "demo", Script: "needs-pkgs"})
	require.Equal(t, "", out.Error, "%+v (%d approvals)", out, len(*f.reqs))
	require.Equal(t, 0, out.ExitCode, "%+v (%d approvals)", out, len(*f.reqs))
	require.Len(t, *f.reqs, 1, "%+v (%d approvals)", out, len(*f.reqs))
	// Built once: the next run doesn't ask or install again.
	second := f.runner.Run(context.Background(), RunSkillScriptInput{Skill: "demo", Script: "needs-pkgs"})
	require.Equal(t, 0, second.ExitCode, "second run: %+v", second)
	require.Len(t, *f.reqs, 1, "the second run asks nothing more")
	envs := f.runner.Envs().List()
	require.Len(t, envs, 1, "envs: %+v", envs)
	require.Equal(t, "demo", envs[0].Skills[0], "envs: %+v", envs)
	t.Logf("sandbox %s", out.Sandbox)
}
