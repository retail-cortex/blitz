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
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingBox is a ScriptBox that runs nothing: it records each request
// and answers with result and err, writing output to the request's Stdout.
type recordingBox struct {
	reqs   []ScriptRequest
	result ScriptResult
	err    error
	output string
}

func (b *recordingBox) Name() string { return "fake" }

func (b *recordingBox) Run(_ context.Context, req ScriptRequest) (ScriptResult, error) {
	b.reqs = append(b.reqs, req)
	if req.Stdout != nil {
		io.WriteString(req.Stdout, b.output)
	}
	return b.result, b.err
}

// noUV leaves uv off PATH and out of ~/.blitz/bin.
func noUV(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

// Ensure builds an environment in the box with pip (or uv), marks it
// ready, reuses it, and leaves nothing behind when the build fails.
func TestPyEnvEnsure(t *testing.T) {
	noUV(t)
	for _, c := range []struct {
		name    string
		box     recordingBox
		wheels  bool
		err     string
		steps   int
		argvHas string
	}{
		{name: "built with pip", wheels: true, steps: 2, argvHas: "-m pip install --quiet --index-url https://pypi.org/simple --only-binary :all: six==1.16.0"},
		{name: "built from sources", steps: 2, argvHas: "-m pip install --quiet --index-url https://pypi.org/simple six==1.16.0"},
		{name: "box error", box: recordingBox{err: errors.New("no box")}, err: "python3: no box", steps: 1},
		{name: "timed out", box: recordingBox{result: ScriptResult{TimedOut: true}}, err: "took longer than 10m0s", steps: 1},
		{name: "failed", box: recordingBox{result: ScriptResult{ExitCode: 2}, output: strings.Repeat("noise\n", 20) + "the reason"}, err: "python3 failed (exit 2): noise", steps: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := NewPyEnvs(filepath.Join(t.TempDir(), "envs"), config.PackagePolicy{WheelsOnly: c.wheels})
			e, err := m.Ensure(context.Background(), &c.box, "/usr/bin/python3", "demo", []string{"six==1.16.0"})
			require.Len(t, c.box.reqs, c.steps)
			if c.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), c.err)
				assert.NoDirExists(t, e.Dir, "a failed build leaves nothing behind")
				assert.Empty(t, m.List())
				return
			}
			require.NoError(t, err)
			assert.True(t, e.Ready)
			assert.Equal(t, []string{"demo"}, e.Skills)
			assert.Equal(t, []string{"/usr/bin/python3", "-m", "venv", e.Dir}, c.box.reqs[0].Argv)
			assert.Contains(t, strings.Join(c.box.reqs[1].Argv, " "), c.argvHas)
			assert.True(t, c.box.reqs[1].Network, "installing reaches the network")
			assert.Equal(t, []string{e.Dir, filepath.Join(m.dir, pyEnvCacheDir)}, c.box.reqs[1].Writable)

			again, err := m.Ensure(context.Background(), &c.box, "/usr/bin/python3", "other", []string{"six==1.16.0"})
			require.NoError(t, err)
			assert.Len(t, c.box.reqs, c.steps, "a built environment isn't built again")
			assert.Equal(t, []string{"demo", "other"}, again.Skills)
			got, ok := m.Lookup("/usr/bin/python3", []string{"six==1.16.0"})
			assert.True(t, ok)
			assert.Equal(t, filepath.Join(e.Dir, "bin", "python"), got.Interpreter())
		})
	}
}

// With uv, environments are built with it, and its cache shares the
// environments' cache.
func TestPyEnvEnsureWithUV(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", home)
	uv := filepath.Join(home, ".blitz", "bin", "uv")
	require.NoError(t, os.MkdirAll(filepath.Dir(uv), 0o755))
	require.NoError(t, os.WriteFile(uv, []byte("#!/bin/sh\n"), 0o755))

	box := &recordingBox{}
	m := NewPyEnvs(filepath.Join(t.TempDir(), "envs"), config.PackagePolicy{Index: "https://mirror.example/simple", WheelsOnly: true})
	e, err := m.Ensure(context.Background(), box, "/opt/py/bin/python3", "demo", []string{"rich"})
	require.NoError(t, err)
	require.Len(t, box.reqs, 2)
	assert.Equal(t, []string{uv, "venv", "--quiet", "--python", "/opt/py/bin/python3", e.Dir}, box.reqs[0].Argv)
	assert.Equal(t, []string{uv, "pip", "install", "--quiet", "--python", e.Interpreter(), "--index-url", "https://mirror.example/simple", "--only-binary", ":all:", "--", "rich"}, box.reqs[1].Argv)
	assert.Contains(t, box.reqs[1].ReadOnly, filepath.Dir(uv), "uv is mounted")
	assert.Contains(t, box.reqs[1].ReadOnly, "/opt/py", "so is the interpreter")
	assert.Contains(t, box.reqs[1].Env, "UV_PYTHON_DOWNLOADS=never")
	assert.Contains(t, m.InstallCommands([]string{"rich"}), "pip install --index-url https://mirror.example/simple --only-binary :all: -- rich")
}

// List shows complete and interrupted environments, newest use first;
// Remove deletes one; an unreadable marker means not ready.
func TestPyEnvListAndRemove(t *testing.T) {
	noUV(t)
	m := NewPyEnvs(filepath.Join(t.TempDir(), "envs"), config.PackagePolicy{})
	box := &recordingBox{}
	older, err := m.Ensure(context.Background(), box, "/usr/bin/python3", "a", []string{"one"})
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	newer, err := m.Ensure(context.Background(), box, "/usr/bin/python3", "b", []string{"two"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(newer.Dir, "big"), make([]byte, 1000), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(m.dir, "interrupted"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(m.dir, "stray-file"), nil, 0o600))

	list := m.List()
	require.Len(t, list, 3)
	assert.Equal(t, newer.Key, list[0].Key)
	assert.Equal(t, older.Key, list[1].Key)
	assert.Equal(t, "interrupted", list[2].Key)
	assert.False(t, list[2].Ready)
	assert.Greater(t, list[0].Size, int64(1000))

	require.NoError(t, m.Remove(older.Key))
	_, ok := m.Lookup("/usr/bin/python3", []string{"one"})
	assert.False(t, ok)
	for _, bad := range []string{"", "a/b", `a\b`, "a.b"} {
		assert.Error(t, m.Remove(bad), "%q", bad)
	}

	require.NoError(t, os.WriteFile(filepath.Join(newer.Dir, pyEnvMarker), []byte("{not json"), 0o600))
	_, ok = m.Lookup("/usr/bin/python3", []string{"two"})
	assert.False(t, ok, "an unreadable marker isn't a ready environment")
}

// An environment directory that can't be made is an error.
func TestPyEnvEnsureUnwritable(t *testing.T) {
	noUV(t)
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	m := NewPyEnvs(filepath.Join(file, "envs"), config.PackagePolicy{})
	_, err := m.Ensure(context.Background(), &recordingBox{}, "/usr/bin/python3", "demo", []string{"six"})
	assert.Error(t, err)
}

// The defaults: ~/.blitz/envs and PyPI.
func TestNewPyEnvsDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := NewPyEnvs("", config.PackagePolicy{})
	assert.Equal(t, filepath.Join(home, ".blitz", "envs"), m.dir)
	assert.Equal(t, "https://pypi.org/simple", m.index)
	assert.Equal(t, "pip install --index-url https://pypi.org/simple -- a b", m.InstallCommands([]string{"a", "b"}))
	assert.Equal(t, "a\nb", lastLines("x\na\nb\n", 2))
	assert.Equal(t, "x\na", lastLines("x\na", 5))
}
