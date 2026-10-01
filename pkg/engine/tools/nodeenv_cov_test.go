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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeNodeInstall is a node beside npm's CLI, as an installation lays them
// out; it returns node's path.
func fakeNodeInstall(t *testing.T) string {
	t.Helper()
	prefix := t.TempDir()
	node := filepath.Join(prefix, "bin", "node")
	cli := filepath.Join(prefix, "lib", "node_modules", "npm", "bin", "npm-cli.js")
	require.NoError(t, os.MkdirAll(filepath.Dir(cli), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(node), 0o755))
	require.NoError(t, os.WriteFile(cli, nil, 0o644))
	require.NoError(t, os.WriteFile(node, []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", t.TempDir()) // no npm on PATH
	return node
}

// Ensure installs packages with npm in the box, without their install
// scripts, marks the environment ready and reuses it; a failed install
// leaves nothing behind.
func TestNodeEnvEnsure(t *testing.T) {
	node := fakeNodeInstall(t)
	for _, c := range []struct {
		name string
		box  recordingBox
		err  string
	}{
		{name: "built"},
		{name: "box error", box: recordingBox{err: errors.New("no box")}, err: "npm: no box"},
		{name: "timed out", box: recordingBox{result: ScriptResult{TimedOut: true}}, err: "took longer than"},
		{name: "failed", box: recordingBox{result: ScriptResult{ExitCode: 1}, output: "npm error 404"}, err: "npm install failed (exit 1): npm error 404"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := NewNodeEnvs(filepath.Join(t.TempDir(), "node-envs"), config.NPMPolicy{Registry: "https://npm.example"})
			e, err := m.Ensure(context.Background(), &c.box, node, "demo", []string{"zod@3"})
			require.Len(t, c.box.reqs, 1)
			argv := strings.Join(c.box.reqs[0].Argv, " ")
			assert.Contains(t, argv, "npm-cli.js install --ignore-scripts")
			assert.Contains(t, argv, "--registry https://npm.example")
			assert.True(t, strings.HasSuffix(argv, "-- zod@3"), argv)
			if c.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), c.err)
				assert.NoDirExists(t, e.Dir)
				return
			}
			require.NoError(t, err)
			assert.FileExists(t, filepath.Join(e.Dir, "package.json"), "npm installs here and nowhere above")
			assert.Equal(t, []string{"demo"}, e.Skills)
			again, err := m.Ensure(context.Background(), &c.box, node, "other", []string{"zod@3"})
			require.NoError(t, err)
			assert.Len(t, c.box.reqs, 1, "built once")
			assert.Equal(t, []string{"demo", "other"}, again.Skills)
		})
	}
}

// Without npm beside node, or a directory to build in, nothing is built.
func TestNodeEnvEnsureFails(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := NewNodeEnvs(filepath.Join(t.TempDir(), "node-envs"), config.NPMPolicy{})
	box := &recordingBox{}
	_, err := m.Ensure(context.Background(), box, filepath.Join(t.TempDir(), "bin", "node"), "demo", []string{"zod"})
	assert.ErrorContains(t, err, "npm not found")
	assert.Empty(t, box.reqs)

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	m = NewNodeEnvs(filepath.Join(file, "node-envs"), config.NPMPolicy{})
	_, err = m.Ensure(context.Background(), box, fakeNodeInstall(t), "demo", []string{"zod"})
	assert.Error(t, err)

	dir := t.TempDir()
	m = NewNodeEnvs(dir, config.NPMPolicy{})
	e, _ := m.Lookup("/n", []string{"zod"})
	require.NoError(t, os.MkdirAll(e.Dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(e.Dir, pyEnvMarker), []byte("{"), 0o600))
	_, ok := m.Lookup("/n", []string{"zod"})
	assert.False(t, ok, "an unreadable marker isn't a ready environment")
}

// SystemNode wants a node that strips types (22.6 or later); npm's CLI is
// found through an npm on PATH that links to it.
func TestSystemNodeVersions(t *testing.T) {
	for _, c := range []struct{ name, script, err string }{
		{name: "new enough", script: "echo v22.6.0"},
		{name: "too old", script: "echo v22.5.9", err: "node v22.5.9 can't run TypeScript"},
		{name: "unreadable", script: "echo nightly", err: "node nightly can't run TypeScript"},
		{name: "broken", script: "exit 3", err: "node --version: exit status 3"},
	} {
		t.Run(c.name, func(t *testing.T) {
			bin := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(bin, "node"), []byte("#!/bin/sh\n"+c.script+"\n"), 0o755))
			t.Setenv("PATH", bin)
			got, err := SystemNode()
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
				return
			}
			require.NoError(t, err)
			want, _ := filepath.EvalSymlinks(filepath.Join(bin, "node")) // macOS: /var is a link
			got, _ = filepath.EvalSymlinks(got)
			assert.Equal(t, want, got)
		})
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	assert.Equal(t, filepath.Join(home, ".blitz", "node-envs"), NewNodeEnvs("", config.NPMPolicy{}).dir)

	cli := filepath.Join(t.TempDir(), "npm-cli.js")
	require.NoError(t, os.WriteFile(cli, nil, 0o755))
	bin := t.TempDir()
	require.NoError(t, os.Symlink(cli, filepath.Join(bin, "npm")))
	t.Setenv("PATH", bin)
	got, err := npmCLI(filepath.Join(t.TempDir(), "bin", "node"))
	require.NoError(t, err)
	assert.Equal(t, cli, got)
}
