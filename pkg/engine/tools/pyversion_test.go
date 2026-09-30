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

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSatisfies(t *testing.T) {
	tests := []struct {
		version, spec string
		want          bool
	}{
		{"3.12.1", ">=3.10", true},
		{"3.9.18", ">=3.10", false},
		{"3.12.1", ">=3.10,<3.13", true},
		{"3.13.0", ">=3.10, <3.13", false},
		{"3.11.4", "~=3.11", true},
		{"4.0", "~=3.11", false},
		{"3.11.9", "~=3.11.2", true},
		{"3.12.0", "~=3.11.2", false},
		{"3.12.7", "==3.12.*", true},
		{"3.13.0", "==3.12.*", false},
		{"3.12.0", "==3.12", true},
		{"3.12.1", "!=3.12.0", true},
		{"3.12.0", "!=3.12.*", false},
		{"3.14.3", ">3.14", true},
		{"3.14.0", "<=3.14", true},
		{"3.14.3", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.version+" "+tt.spec, func(t *testing.T) {
			got, err := satisfies(parseRelease(tt.version), tt.spec)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
	_, err := satisfies([]int{3, 12}, "python 3")
	assert.ErrorContains(t, err, "can't read")
}

func TestScriptRequiresPython(t *testing.T) {
	src := "#!/usr/bin/env python3\n# /// script\n# requires-python = \">=3.11\"\n# dependencies = [\n#   \"requests\",\n# ]\n# ///\nprint('hi')\n"
	assert.Equal(t, ">=3.11", scriptRequiresPython(src))
	assert.Empty(t, scriptRequiresPython("print('no metadata')\n"))
	assert.Empty(t, scriptRequiresPython("# /// script\n# requires-python = \n# ///\n"), "unreadable metadata says nothing")
	v, ok := parseVersion("Python 3.14.3\n")
	assert.True(t, ok)
	assert.Equal(t, "3.14.3", versionString(v))
}

// pinnedSkill is a skill whose scripts need a given Python.
func pinnedFixture(t *testing.T, spec string) *SkillScripts {
	t.Helper()
	skillsDir := t.TempDir()
	os.MkdirAll(filepath.Join(skillsDir, "pinned", "scripts"), 0o755)
	os.WriteFile(filepath.Join(skillsDir, "pinned", "SKILL.md"), []byte("---\nname: pinned\nscripts:\n  - name: meta\n    language: python\n    relative_path: scripts/meta.py\n  - name: field\n    language: python\n    requires_python: \""+spec+"\"\n    inline_code: \"import sys; print(sys.version_info[:2])\"\n---\n"), 0o644)
	os.WriteFile(filepath.Join(skillsDir, "pinned", "scripts", "meta.py"), []byte("# /// script\n# requires-python = \""+spec+"\"\n# ///\nimport sys\nprint(sys.version_info[:2])\n"), 0o644)
	prov, _ := skills.NewProvider()
	require.NoError(t, prov.DiscoverExternal([]string{skillsDir}))
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	hooks, _ := approverHooks(true)
	policy := config.DefaultConfig().Skills.Policy
	r := NewSkillScripts(prov, policy, ws, hooks, NewPyEnvs(filepath.Join(t.TempDir(), "envs"), policy.Packages),
		ScriptBoxConfig{Mode: os.Getenv("BLITZ_PYENV_SANDBOX"), Blocked: ws.Blocked(), StateDir: t.TempDir()})
	if _, err := r.Box(); err != nil {
		t.Skipf("no script sandbox: %v", err)
	}
	if _, err := SystemPython(); err != nil {
		t.Skip(err)
	}
	return r
}

// A script's Python requirement is met by the system's Python, else by a
// uv-managed one; without uv it's refused with the reason (BL-SK-03).
func TestRequiresPython(t *testing.T) {
	r := pinnedFixture(t, ">=3")
	for _, script := range []string{"meta", "field"} {
		out := r.Run(context.Background(), RunSkillScriptInput{Skill: "pinned", Script: script})
		require.Equal(t, "", out.Error, "%s: %+v", script, out)
		assert.Equal(t, 0, out.ExitCode)
	}

	python, _ := SystemPython()
	t.Setenv("PATH", filepath.Dir(python)+":/usr/bin:/bin") // no uv
	t.Setenv("HOME", t.TempDir())
	if _, err := findUV(); err == nil {
		t.Skip("uv is beside python3")
	}
	r = pinnedFixture(t, "<3")
	for _, script := range []string{"meta", "field"} {
		out := r.Run(context.Background(), RunSkillScriptInput{Skill: "pinned", Script: script})
		assert.Contains(t, out.Error, "the script needs Python <3", "%s: %+v", script, out)
		assert.Contains(t, out.Error, "install uv")
	}
}

// With uv, a Python the system doesn't have. Set BLITZ_UV_PYTHON to a
// version uv has or can install (it may download it), e.g. 3.12.
func TestRequiresPythonWithUV(t *testing.T) {
	want := os.Getenv("BLITZ_UV_PYTHON")
	if want == "" {
		t.Skip("set BLITZ_UV_PYTHON=3.12 (a version the system's python3 isn't) to use uv")
	}
	r := pinnedFixture(t, "=="+want+".*")
	out := r.Run(context.Background(), RunSkillScriptInput{Skill: "pinned", Script: "field"})
	require.Equal(t, "", out.Error, "%+v", out)
	assert.Equal(t, "("+strings.ReplaceAll(want, ".", ", ")+")\n", out.Stdout)
}
