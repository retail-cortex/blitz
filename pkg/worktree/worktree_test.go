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

package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repo is a git repository with one commit, an untracked .env and a
// .worktreeinclude that lists it.
func repo(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	run("init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, IncludeFile), []byte("# local files\n.env\nconfig/*.local\n"), 0o644))
	run("add", ".")
	run("commit", "-q", "-m", "first")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("KEY=1\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "dev.local"), []byte("x"), 0o644))
	return dir
}

func TestWorktrees(t *testing.T) {
	dir := repo(t)
	w, err := Create(filepath.Join(dir), "fix-rounding", "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, Dir, "fix-rounding"), w.Path)
	assert.Equal(t, "blitz/fix-rounding", w.Branch)
	for _, f := range []string{"main.go", ".env", "config/dev.local"} {
		_, err := os.Stat(filepath.Join(w.Path, f))
		assert.NoError(t, err, "%s missing from the worktree", f)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	assert.Contains(t, string(data), "/.blitz/worktrees/")
	status, _ := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	assert.NotContains(t, string(status), ".blitz", "the worktree shows in the repository's status")

	_, err = Create(dir, "fix-rounding", "")
	assert.ErrorContains(t, err, "exists already")
	_, err = Create(dir, "bad name", "")
	assert.ErrorContains(t, err, "can't name a worktree")
	auto, err := Create(dir, "", "HEAD")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(auto.Name, "wt-"))

	list, err := List(w.Path) // from inside a worktree too
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "fix-rounding", list[0].Name)
	assert.Equal(t, "blitz/fix-rounding", list[0].Branch)
	assert.False(t, Dirty(list[0]))
	require.NoError(t, os.WriteFile(filepath.Join(w.Path, "main.go"), []byte("package main // changed\n"), 0o644))
	assert.True(t, Dirty(list[0]))

	_, err = Remove(dir, "fix-rounding", false)
	assert.Error(t, err, "removed with changes not committed")
	_, err = Remove(dir, "fix-rounding", true)
	require.NoError(t, err)
	_, err = Remove(dir, "nope", false)
	assert.ErrorContains(t, err, "no worktree")

	require.NoError(t, os.RemoveAll(auto.Path))
	list, _ = List(dir)
	require.Len(t, list, 1)
	assert.True(t, list[0].Missing)
	require.NoError(t, Prune(dir))
	list, _ = List(dir)
	assert.Empty(t, list)

	_, err = Create(t.TempDir(), "x", "")
	assert.ErrorIs(t, err, ErrNotRepo)
}
