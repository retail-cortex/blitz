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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// passthroughSandbox makes the OS sandbox run commands as they are for the
// rest of the test, so the logic around a script run (approvals,
// environments, output, workspace copies) can be tested on hosts without a
// working sandbox. It checks nothing about isolation: the backend tests do.
func passthroughSandbox(t *testing.T) {
	t.Helper()
	wrap, probe := platformSandbox, platformSandboxProbe
	platformSandbox = func(OSSandboxSpec) (sandboxWrapper, error) { return prefixWrapper(), nil }
	platformSandboxProbe = func() error { return nil }
	t.Cleanup(func() { platformSandbox, platformSandboxProbe = wrap, probe })
}

// fakeRuntimes are stand-ins for python3, uv and node, so that installing
// packages or interpreters needs no network. The fake python3 runs scripts
// with the real one; the others only print what they were asked to run.
type fakeRuntimes struct {
	root   string // mode files live here
	bin    string // python3, uv, node
	noUV   string // python3 and node only
	realPy string
}

const fakePython = `#!/bin/sh
F=@ROOT@
mode=$(cat "$F/python.mode" 2>/dev/null)
if [ "$1" = --version ]; then
  case "$mode" in
  broken) exit 9;;
  garbled) echo "Python ???"; exit 0;;
  esac
  echo "Python 3.12.3"; exit 0
fi
if [ "$1" = -m ] && [ "$2" = venv ]; then mkdir -p "$3/bin" && cp "$F/bin/python3" "$3/bin/python"; exit $?; fi
if [ "$1" = -m ] && [ "$2" = pip ]; then
  for a in "$@"; do case "$a" in fail-me*) echo "ERROR: No matching distribution found for $a" >&2; exit 1;; esac; done
  echo "$*" >> "$F/pip.log"
  exit 0
fi
exec @REALPY@ "$@"
`

const fakeUV = `#!/bin/sh
F=@ROOT@
mode=$(cat "$F/uv.mode" 2>/dev/null)
case "$1 $2" in
"python find")
  [ "$mode" = lost ] && exit 2
  [ "$mode" = dangling ] && { echo "$F/no-such-python"; exit 0; }
  [ -f "$F/uv-python/installed-$(echo "$4" | tr -c 'a-zA-Z0-9' _)" ] && { echo "@REALPY@"; exit 0; }
  exit 2;;
"python dir") [ "$mode" = nodir ] && exit 1; echo "$F/uv-python";;
"cache dir") [ "$mode" = nocache ] && exit 1; echo "$F/uv-cache";;
"python install")
  for a in "$@"; do case "$a" in *9.9*) echo "error: no download found for $a" >&2; exit 1;; esac; done
  touch "$F/uv-python/installed-$(echo "$4" | tr -c 'a-zA-Z0-9' _)";;
"venv --quiet") mkdir -p "$5/bin" && cp "$F/bin/python3" "$5/bin/python";;
"pip install")
  for a in "$@"; do case "$a" in fail-me*) echo "No solution found for $a" >&2; exit 1;; esac; done
  echo "$*" >> "$F/uv.log";;
*) exit 3;;
esac
`

const fakeNode = `#!/bin/sh
F=@ROOT@
if [ "$1" = --version ]; then
  [ -f "$F/node.broken" ] && exit 1
  cat "$F/node.version" 2>/dev/null || echo v22.11.0
  exit 0
fi
if [ "$2" = install ]; then
  for a in "$@"; do case "$a" in fail-me*) echo "npm error 404 Not Found - $a" >&2; exit 1;; esac; done
  mkdir -p node_modules; exit 0
fi
echo "node $*"
`

// newFakeRuntimes installs the fakes and puts them first on PATH, with a
// private HOME (so ~/.blitz holds nothing of the user's).
func newFakeRuntimes(t *testing.T) *fakeRuntimes {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found on PATH")
	}
	py, err = filepath.EvalSymlinks(py)
	require.NoError(t, err)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	f := &fakeRuntimes{root: root, bin: filepath.Join(root, "bin"), noUV: filepath.Join(root, "bin-nouv"), realPy: py}
	expand := strings.NewReplacer("@ROOT@", root, "@REALPY@", py)
	for dir, scripts := range map[string]map[string]string{
		f.bin:  {"python3": fakePython, "uv": fakeUV, "node": fakeNode},
		f.noUV: {"python3": fakePython, "node": fakeNode},
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		for name, body := range scripts {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(expand.Replace(body)), 0o755))
		}
	}
	cli := filepath.Join(root, "lib", "node_modules", "npm", "bin", "npm-cli.js")
	require.NoError(t, os.MkdirAll(filepath.Dir(cli), 0o755))
	require.NoError(t, os.WriteFile(cli, []byte("// npm\n"), 0o644))
	t.Setenv("PATH", f.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	return f
}

// mode sets how the fake named behaves ("" for normally).
func (f *fakeRuntimes) mode(t *testing.T, name, mode string) {
	t.Helper()
	p := filepath.Join(f.root, name+".mode")
	if mode == "" {
		os.Remove(p)
		return
	}
	require.NoError(t, os.WriteFile(p, []byte(mode), 0o644))
}

// withoutUV leaves uv off PATH (python3 and node stay).
func (f *fakeRuntimes) withoutUV(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", f.noUV+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	if _, err := findUV(); err == nil {
		t.Skip("uv is in /usr/bin")
	}
}

const covSkill = `---
name: cov
execution_hints:
  environment_variables: [COV_PASSED, COV_WITHHELD]
scripts:
  - name: echo
    language: python
    inline_code: |
      import os, sys
      print("args", sys.argv[1:])
      print("env", os.environ.get("GREETING"), os.environ.get("COV_PASSED"), os.environ.get("COV_WITHHELD"), os.environ.get("COV_SECRET"), os.environ.get("PYTHONNOUSERSITE"))
      out = os.environ.get("SKILL_OUTPUT")
      if out:
          for i in range(3):
              open(os.path.join(out, "r%d.txt" % i), "w").write("x")
    environment_variables: {GREETING: hi}
  - name: entry
    language: python
    relative_path: scripts/work.py
    entry_point: main
  - name: meta
    language: python
    relative_path: scripts/meta.py
  - name: fail
    language: python
    inline_code: "raise SystemExit(2)"
  - name: slow
    language: python
    inline_code: "import time; time.sleep(30)"
    timeout_seconds: 1
  - name: deps
    language: python
    inline_code: "print('with deps')"
    dependencies: ["six==1.16.0"]
  - name: deps-fail
    language: python
    inline_code: "print('never')"
    dependencies: ["fail-me==1.0"]
  - name: pinned
    language: python
    requires_python: ">=3.10"
    inline_code: "print('pinned')"
  - name: old
    language: python
    requires_python: "<3"
    inline_code: "print('old')"
  - name: unreadable-spec
    language: python
    requires_python: "~~3"
    inline_code: "print('spec')"
  - name: newer
    language: python
    requires_python: "==3.13.*"
    inline_code: "print('newer')"
  - name: impossible
    language: python
    requires_python: "==9.9.*"
    inline_code: "print('impossible')"
  - name: ts
    language: typescript
    inline_code: "console.log(1)"
  - name: ts-entry
    language: typescript
    relative_path: scripts/a.ts
    entry_point: main
  - name: ts-deps
    language: typescript
    inline_code: "import isOdd from 'is-odd'"
    dependencies: ["is-odd@3.0.1"]
  - name: ts-deps-fail
    language: typescript
    inline_code: "import x from 'fail-me'"
    dependencies: ["fail-me@1.0.0"]
---
`

const covWork = `import sys

def main():
    print("entry point", sys.argv[1:])
    return 3

if __name__ == "__main__":
    print("ran as main")
`

// covFixture is a workspace and skills whose scripts run in the
// pass-through sandbox with fake runtimes.
type covFixture struct {
	ws    string
	r     *SkillScripts
	reqs  *[]api.ApprovalRequest
	fakes *fakeRuntimes
}

// newCovFixture writes the skills (name -> SKILL.md, plus files under the
// skill's directory) and a workspace, answering approvals with decide.
func newCovFixture(t *testing.T, docs map[string]string, files map[string]string, decide func(api.ApprovalRequest) api.Decision, edit func(*config.SkillPolicy)) covFixture {
	t.Helper()
	passthroughSandbox(t)
	fakes := newFakeRuntimes(t)
	skillsDir := t.TempDir()
	for name, doc := range docs {
		require.NoError(t, os.MkdirAll(filepath.Join(skillsDir, name), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(skillsDir, name, "SKILL.md"), []byte(doc), 0o644))
	}
	for name, body := range files {
		p := filepath.Join(skillsDir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	prov, _ := skills.NewProvider()
	require.NoError(t, prov.DiscoverExternal([]string{skillsDir}))

	wsDir, _ := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(wsDir, "README.md"), []byte("original\n"), 0o644))
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: wsDir, BlockedPaths: config.DefaultBlockedPaths})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	policy := config.DefaultConfig().Skills.Policy
	policy.EnvPassthrough = []string{"COV_PASSED"}
	if edit != nil {
		edit(&policy)
	}
	var reqs []api.ApprovalRequest
	hooks := NewHooks(Policy{})
	hooks.SetApprover(func(_ context.Context, req api.ApprovalRequest) (api.Decision, error) {
		reqs = append(reqs, req)
		return decide(req), nil
	})
	r := NewSkillScripts(prov, policy, ws, hooks, NewPyEnvs(filepath.Join(t.TempDir(), "envs"), policy.Packages),
		ScriptBoxConfig{Mode: "os", Blocked: ws.Blocked(), StateDir: t.TempDir()})
	_, err = r.Box()
	require.NoError(t, err)
	return covFixture{ws: wsDir, r: r, reqs: &reqs, fakes: fakes}
}

func approveAll(api.ApprovalRequest) api.Decision { return api.DecisionOnce }
func denyAll(api.ApprovalRequest) api.Decision    { return api.DecisionDeny }

// newCov is the fixture for the "cov" skill.
func newCov(t *testing.T, decide func(api.ApprovalRequest) api.Decision, edit func(*config.SkillPolicy)) covFixture {
	t.Helper()
	return newCovFixture(t, map[string]string{"cov": covSkill}, map[string]string{
		"cov/scripts/work.py": covWork,
		"cov/scripts/meta.py": "# /// script\n# requires-python = \">=3.11\"\n# ///\nprint('meta')\n",
		"cov/scripts/a.ts":    "export function main(): number { return 3 }\n",
	}, decide, edit)
}

// A tier 2 script runs without asking, sees only the environment it's
// given, and lists what it wrote to its output directory.
func TestRunSkillScriptRunsAndReports(t *testing.T) {
	f := newCov(t, denyAll, nil)
	t.Setenv("COV_PASSED", "passed")
	t.Setenv("COV_WITHHELD", "withheld")
	t.Setenv("COV_SECRET", "secret")
	rt := toolOf(t)(NewRunSkillScriptTool(f.r))
	out := runTool(t, rt, map[string]any{"skill": "cov", "script": "echo", "args": []string{"a b", "c"}})
	require.Equal(t, "", errOf(out), "%v", out)
	assert.EqualValues(t, 0, out["exit_code"])
	assert.Equal(t, "os", out["sandbox"])
	assert.Equal(t, "TIER_2_AUDITED_WRITE", out["tier"])
	stdout, _ := out["stdout"].(string)
	assert.Contains(t, stdout, "args ['a b', 'c']")
	assert.Contains(t, stdout, "env hi passed None None 1", "only passed-through variables reach the script")
	files, _ := out["output_files"].([]any)
	assert.Len(t, files, 3, "%v", out)
	assert.True(t, strings.HasPrefix(out["output_dir"].(string), SkillOutputDir+"/cov/echo-"), "%v", out)
	assert.Empty(t, *f.reqs, "tier 2 doesn't ask")
}

// The run's outcomes: entry points, exit codes, timeouts and refusals.
func TestRunSkillScriptOutcomes(t *testing.T) {
	f := newCov(t, approveAll, nil)
	for _, c := range []struct {
		script   string
		args     []string
		exit     int
		stdout   string
		err      string
		timedOut bool
	}{
		{script: "entry", args: []string{"x"}, exit: 3, stdout: "entry point ['x']"},
		{script: "meta", stdout: "meta"},
		{script: "fail", exit: 2},
		{script: "slow", exit: -1, err: "stopped after 1s (timeout)", timedOut: true},
		{script: "pinned", stdout: "pinned"},
		{script: "unreadable-spec", err: `can't read the Python requirement "~~3"`},
	} {
		t.Run(c.script, func(t *testing.T) {
			out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: c.script, Args: c.args})
			if c.err == "" {
				assert.Equal(t, "", out.Error, "%+v", out)
			} else {
				assert.Contains(t, out.Error, c.err, "%+v", out)
			}
			assert.Equal(t, c.exit, out.ExitCode, "%+v", out)
			assert.Equal(t, c.timedOut, out.TimedOut, "%+v", out)
			assert.Contains(t, out.Stdout, c.stdout, "%+v", out)
			assert.NotContains(t, out.Stdout, "ran as main", "an entry point's module isn't run as __main__")
		})
	}
}

// Tier 3 asks before every run; tier 0 runs without asking only when both
// the skill and the policy allow it.
func TestRunSkillScriptTiers(t *testing.T) {
	doc := func(name, hints string) string {
		return "---\nname: " + name + "\nexecution_hints:\n" + hints + "scripts:\n  - name: go\n    language: python\n    inline_code: \"import os; print('output', bool(os.environ.get('SKILL_OUTPUT')))\"\n---\n"
	}
	docs := map[string]string{
		"strict": doc("strict", "  hitl_tier: TIER_3_MANDATORY_APPROVAL\n"),
		"free":   doc("free", "  hitl_tier: HITL_POLICY_TIER_0_BYPASS_ALL\n  allow_hitl_bypass: true\n"),
	}
	for _, c := range []struct {
		name, skill string
		decide      func(api.ApprovalRequest) api.Decision
		bypass      bool
		asks        int
		err, stdout string
	}{
		{name: "tier 3 approved", skill: "strict", decide: approveAll, asks: 1, stdout: "output True"},
		{name: "tier 3 denied", skill: "strict", decide: denyAll, asks: 1, err: "not approved"},
		{name: "bypass allowed", skill: "free", decide: denyAll, bypass: true, stdout: "output True"},
		{name: "bypass not allowed is tier 3", skill: "free", decide: denyAll, asks: 1, err: "not approved"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newCovFixture(t, docs, nil, c.decide, func(p *config.SkillPolicy) { p.AllowHITLBypass = c.bypass })
			out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: c.skill, Script: "go"})
			if c.err == "" {
				assert.Equal(t, "", out.Error, "%+v", out)
			} else {
				assert.Contains(t, out.Error, c.err, "%+v", out)
			}
			assert.Contains(t, out.Stdout, c.stdout)
			assert.Len(t, *f.reqs, c.asks, "%+v", *f.reqs)
		})
	}
}

// A script's packages are installed once, after their own approval, and
// a failed install says why.
func TestRunSkillScriptPythonPackages(t *testing.T) {
	for _, uv := range []bool{true, false} {
		t.Run(map[bool]string{true: "uv", false: "pip"}[uv], func(t *testing.T) {
			f := newCov(t, approveAll, nil)
			if !uv {
				f.fakes.withoutUV(t)
			}
			out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "deps"})
			require.Equal(t, "", out.Error, "%+v", out)
			assert.Equal(t, "with deps\n", out.Stdout)
			require.Len(t, *f.reqs, 1)
			assert.True(t, strings.HasPrefix((*f.reqs)[0].Key, "pyenv:"), "%+v", (*f.reqs)[0])
			log := map[bool]string{true: "uv.log", false: "pip.log"}[uv]
			b, err := os.ReadFile(filepath.Join(f.fakes.root, log))
			require.NoError(t, err)
			assert.Contains(t, string(b), "six==1.16.0")
			assert.Contains(t, string(b), "--only-binary :all:", "wheels only by default")

			again := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "deps"})
			require.Equal(t, "", again.Error, "%+v", again)
			assert.Len(t, *f.reqs, 1, "a built environment needs no approval")
			envs := f.r.Envs().List()
			require.Len(t, envs, 1)
			assert.Equal(t, []string{"cov"}, envs[0].Skills)

			out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "deps-fail"})
			assert.Contains(t, out.Error, "setting up the environment", "%+v", out)
			assert.Contains(t, out.Error, "fail-me==1.0", "the installer's output says why")
			assert.Len(t, f.r.Envs().List(), 1, "a failed build leaves nothing behind")
		})
	}
}

// A Python requirement the system's doesn't meet uses uv's: one it has,
// else one it installs after an approval; without uv it's refused.
func TestRunSkillScriptManagedPython(t *testing.T) {
	f := newCov(t, approveAll, nil)
	out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "newer"})
	require.Equal(t, "", out.Error, "%+v", out)
	assert.Equal(t, "newer\n", out.Stdout)
	require.Len(t, *f.reqs, 1)
	assert.Equal(t, "uvpython:==3.13.*", (*f.reqs)[0].Key)
	assert.Contains(t, (*f.reqs)[0].Detail, "the system's is 3.12.3")

	again := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "newer"})
	require.Equal(t, "", again.Error, "%+v", again)
	assert.Len(t, *f.reqs, 1, "an installed Python is found without asking")

	for _, c := range []struct{ name, script, mode, err string }{
		{name: "install fails", script: "impossible", err: "uv python install failed (exit 1): error: no download found for ==9.9.*"},
		{name: "installed but lost", script: "newer", mode: "lost", err: "uv installed Python ==3.13.* but can't find it"},
		{name: "no python dir", script: "impossible", mode: "nodir", err: "uv python dir"},
		{name: "no cache dir", script: "impossible", mode: "nocache", err: "uv cache dir"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f.fakes.mode(t, "uv", c.mode)
			defer f.fakes.mode(t, "uv", "")
			out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: c.script})
			assert.Contains(t, out.Error, c.err, "%+v", out)
		})
	}
	// uv may name a path that isn't there: it's used as named.
	f.fakes.mode(t, "uv", "dangling")
	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "newer"})
	assert.NotEqual(t, 0, out.ExitCode, "%+v", out)
	f.fakes.mode(t, "uv", "")

	denied := newCov(t, denyAll, nil)
	out = denied.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "newer"})
	assert.Contains(t, out.Error, "not approved", "%+v", out)

	f.fakes.withoutUV(t)
	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "old"})
	assert.Contains(t, out.Error, "the script needs Python <3 and the system's is 3.12.3: install uv", "%+v", out)
}

// The system Python's version must be readable to check a requirement.
func TestRunSkillScriptUnreadablePythonVersion(t *testing.T) {
	f := newCov(t, approveAll, nil)
	for mode, want := range map[string]string{"broken": "--version: exit status 9", "garbled": `--version said "Python ???"`} {
		t.Run(mode, func(t *testing.T) {
			f.fakes.mode(t, "python", mode)
			defer f.fakes.mode(t, "python", "")
			out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "pinned"})
			assert.Contains(t, out.Error, want, "%+v", out)
		})
	}
}

// TypeScript scripts run on node with their types stripped; an entry point
// is imported and called; packages are installed after an approval.
func TestRunSkillScriptTypeScriptCommands(t *testing.T) {
	f := newCov(t, approveAll, nil)
	out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "ts", Args: []string{"x"}})
	require.Equal(t, "", out.Error, "%+v", out)
	assert.Contains(t, out.Stdout, "node --experimental-strip-types --no-warnings ")
	assert.Contains(t, out.Stdout, "ts.ts x")

	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "ts-entry"})
	require.Equal(t, "", out.Error, "%+v", out)
	assert.Contains(t, out.Stdout, "--input-type=module -e ")
	assert.Contains(t, out.Stdout, "m['main']()")

	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "ts-deps"})
	require.Equal(t, "", out.Error, "%+v", out)
	require.Len(t, *f.reqs, 1)
	assert.True(t, strings.HasPrefix((*f.reqs)[0].Key, "nodeenv:"))
	again := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "ts-deps"})
	require.Equal(t, "", again.Error, "%+v", again)
	assert.Len(t, *f.reqs, 1, "a built environment needs no approval")

	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "ts-deps-fail"})
	assert.Contains(t, out.Error, "setting up the environment: npm install failed (exit 1)", "%+v", out)

	require.NoError(t, os.WriteFile(filepath.Join(f.fakes.root, "node.version"), []byte("v20.11.0\n"), 0o644))
	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "ts"})
	assert.Contains(t, out.Error, "node v20.11.0 can't run TypeScript", "%+v", out)
	require.NoError(t, os.WriteFile(filepath.Join(f.fakes.root, "node.broken"), nil, 0o644))
	out = f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "ts"})
	assert.Contains(t, out.Error, "node --version", "%+v", out)

	denied := newCov(t, denyAll, nil)
	out = denied.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "ts-deps"})
	assert.Contains(t, out.Error, "not approved", "%+v", out)
	out = denied.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "deps"})
	assert.Contains(t, out.Error, "not approved", "%+v", out)
}

// Without a sandbox, nothing runs.
func TestRunSkillScriptNoSandbox(t *testing.T) {
	f := newCov(t, approveAll, nil)
	r := NewSkillScripts(f.r.provider, f.r.policy, f.r.ws, f.r.hooks, f.r.envs, ScriptBoxConfig{Mode: "docker"})
	out := r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "echo"})
	assert.Contains(t, out.Error, "unknown skills.policy.sandbox", "%+v", out)
}

// Unknown skills and scripts, and scripts the policy doesn't allow, are
// refused before anything runs.
func TestRunSkillScriptRefusedEarly(t *testing.T) {
	f := newCov(t, approveAll, func(p *config.SkillPolicy) { p.Languages = []string{"python"} })
	for _, c := range []struct{ skill, script, err string }{
		{skill: "nope", script: "echo", err: `no skill named "nope"`},
		{skill: "cov", script: "nope", err: `skill "cov" has no script "nope"`},
		{skill: "cov", script: "ts", err: `skills.policy doesn't allow this script: language "typescript" isn't in skills.policy.languages`},
	} {
		t.Run(c.skill+"/"+c.script, func(t *testing.T) {
			out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: c.skill, Script: c.script})
			assert.Equal(t, c.err, out.Error)
			assert.Empty(t, out.Sandbox, "nothing ran")
		})
	}
	// An output directory that can't be made stops the run.
	require.NoError(t, os.WriteFile(filepath.Join(f.ws, ".blitz"), nil, 0o644))
	out := f.r.Run(context.Background(), RunSkillScriptInput{Skill: "cov", Script: "echo"})
	assert.Contains(t, out.Error, "not a directory", "%+v", out)
}

// listFiles stops at its limit, and pyQuote escapes what Python needs.
func TestListFilesAndPyQuote(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), nil, 0o644))
	}
	assert.Len(t, listFiles(dir, dir, 2), 2)
	assert.Len(t, listFiles(dir, filepath.Join(dir, "missing"), 2), 0)
	assert.Equal(t, `'a\'b\\c\nd'`, pyQuote("a'b\\c\nd"))
	assert.Equal(t, "", argsNote(nil))
	assert.Equal(t, " with arguments 'a b' 'c'", argsNote([]string{"a b", "c"}))
}
