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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPyEnvKey(t *testing.T) {
	m := NewPyEnvs(t.TempDir(), config.PackagePolicy{Index: "https://pypi.org/simple", WheelsOnly: true})
	a := m.Key("/usr/bin/python3.12", []string{"requests>=2", "rich==13.7.1"})
	b := m.Key("/usr/bin/python3.12", []string{"rich==13.7.1", "requests>=2", "requests>=2"})
	assert.Equal(t, b, a, "key depends on order or duplicates")
	assert.NotEqual(t, a, m.Key("/usr/bin/python3.13", []string{"requests>=2", "rich==13.7.1"}), "key ignores the interpreter")
	assert.NotEqual(t, a, NewPyEnvs(t.TempDir(), config.PackagePolicy{WheelsOnly: false}).Key("/usr/bin/python3.12", []string{"requests>=2", "rich==13.7.1"}), "key ignores wheels_only")
	e, ok := m.Lookup("/usr/bin/python3.12", []string{"x"})
	assert.False(t, ok, "lookup of a missing env: %+v %v", e, ok)
	assert.NotEqual(t, "", e.Key, "lookup of a missing env: %+v %v", e, ok)
	assert.True(t, strings.HasPrefix(e.Dir, m.dir), "lookup of a missing env: %+v %v", e, ok)
	assert.Error(t, m.Remove("../escape"), "remove accepted a path")
}

func TestMountsFor(t *testing.T) {
	for py, want := range map[string]string{
		"/usr/bin/python3.12":                                        "",
		"/usr/local/bin/python3.13":                                  "",
		"/opt/python3.13/bin/python3.13":                             "/opt/python3.13",
		"/home/u/.local/share/uv/python/cpython-3.13/bin/python3.13": "/home/u/.local/share/uv/python/cpython-3.13",
	} {
		t.Run(py, func(t *testing.T) {
			got := strings.Join(MountsFor(py), ",")
			assert.Equal(t, want, got, "%s: %q, want %q", py, got, want)
		})
	}
}

// Builds a real environment from PyPI inside the script sandbox, then uses
// it with the network off. Needs the network: BLITZ_PYENV_TESTS=1.
func TestPyEnvBuildAndUse(t *testing.T) {
	if os.Getenv("BLITZ_PYENV_TESTS") != "1" {
		t.Skip("set BLITZ_PYENV_TESTS=1 to build a real environment (needs the network)")
	}
	box, _, err := NewScriptBox(ScriptBoxConfig{Mode: os.Getenv("BLITZ_PYENV_SANDBOX"), StateDir: t.TempDir()})
	if err != nil {
		t.Skipf("no script sandbox: %v", err)
	}
	python, err := SystemPython()
	if err != nil {
		t.Skip(err)
	}
	m := NewPyEnvs(filepath.Join(t.TempDir(), "envs"), config.PackagePolicy{Index: "https://pypi.org/simple", WheelsOnly: true})
	deps := []string{"six==1.16.0"}
	e, err := m.Ensure(context.Background(), box, python, "demo", deps)
	require.NoError(t, err, "build with %s", box.Name())
	again, ok := m.Lookup(python, deps)
	require.True(t, ok, "lookup after build: %+v %v", again, ok)
	require.Equal(t, e.Key, again.Key, "lookup after build: %+v %v", again, ok)
	require.Equal(t, "demo", again.Skills[0], "lookup after build: %+v %v", again, ok)
	var out bytes.Buffer
	res, err := box.Run(context.Background(), ScriptRequest{
		Argv: []string{e.Interpreter(), "-c", "import six; print('six', six.__version__)"},
		Dir:  e.Dir, ReadOnly: append([]string{e.Dir}, MountsFor(python)...), Stdout: &out, Stderr: &out,
	})
	require.NoError(t, err, "using the env (network off): %+v %v\n%s", res, err, out.String())
	require.Equal(t, 0, res.ExitCode, "using the env (network off): %+v %v\n%s", res, err, out.String())
	require.Contains(t, out.String(), "six 1.16.0", "using the env (network off): %+v %v\n%s", res, err, out.String())
	// A build that fails leaves nothing behind.
	_, err = m.Ensure(context.Background(), box, python, "demo", []string{"no-such-package-blitz-test==9.9.9"})
	require.Error(t, err, "impossible requirement installed")
	n := len(m.List())
	require.Equal(t, 1, n, "%d environments after a failed build, want 1", n)
	t.Logf("sandbox %s, env %s, %d KB", box.Name(), e.Key, m.List()[0].Size/1024)
}
