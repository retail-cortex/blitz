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

// Outside a repository, every operation says so; a ref that doesn't exist
// makes no worktree; a .worktreeinclude that can't be read is reported
// after the worktree is made.
func TestWorktreeErrors(t *testing.T) {
	notRepo := t.TempDir()
	_, err := List(notRepo)
	assert.ErrorIs(t, err, ErrNotRepo)
	_, err = Remove(notRepo, "x", false)
	assert.ErrorIs(t, err, ErrNotRepo)
	assert.ErrorIs(t, Prune(notRepo), ErrNotRepo)

	dir := repo(t)
	_, err = Create(dir, "x", "no-such-ref")
	assert.ErrorContains(t, err, "git worktree add")

	require.NoError(t, os.WriteFile(filepath.Join(dir, IncludeFile), []byte("[\n"), 0o644))
	w, err := Create(dir, "y", "")
	assert.ErrorContains(t, err, "copying .worktreeinclude's files")
	assert.DirExists(t, w.Path, "the worktree is made all the same")
}

// The exclude line goes on a line of its own, once.
func TestExcludeWorktrees(t *testing.T) {
	dir := repo(t)
	exclude := filepath.Join(dir, ".git", "info", "exclude")
	require.NoError(t, os.WriteFile(exclude, []byte("*.log"), 0o644))
	excludeWorktrees(dir)
	excludeWorktrees(dir)
	data, err := os.ReadFile(exclude)
	require.NoError(t, err)
	assert.Equal(t, "*.log\n/.blitz/worktrees/\n", string(data))

	// An exclude file that can't be written is left alone.
	require.NoError(t, os.Remove(exclude))
	require.NoError(t, os.Mkdir(exclude, 0o755))
	excludeWorktrees(dir)
	assert.DirExists(t, exclude)
}

// copyIncluded copies regular files inside the repository only, and
// reports what it can't read or write.
func TestCopyIncluded(t *testing.T) {
	root, to := t.TempDir(), t.TempDir()
	write := func(rel, data string) {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(data), 0o644))
	}
	write("a.txt", "a")
	write("dir/b.txt", "b")
	write(".blitz/worktrees/w/c.txt", "c")
	require.NoError(t, os.Symlink("a.txt", filepath.Join(root, "link.txt")))
	write(IncludeFile, "a.txt\ndir\n.blitz/worktrees/w/*\nlink.txt\n../*\n")

	require.NoError(t, copyIncluded(root, to))
	entries, err := os.ReadDir(to)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Equal(t, []string{"a.txt"}, names, "directories, worktrees, links and files outside are skipped")

	t.Run("no include file", func(t *testing.T) {
		assert.NoError(t, copyIncluded(t.TempDir(), t.TempDir()))
	})
	t.Run("include file is a directory", func(t *testing.T) {
		r := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(r, IncludeFile), 0o755))
		assert.Error(t, copyIncluded(r, t.TempDir()))
	})
	t.Run("destination blocked", func(t *testing.T) {
		dst := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dst, "dir"), nil, 0o644)) // a file where dir/ goes
		require.NoError(t, os.WriteFile(filepath.Join(root, IncludeFile), []byte("dir/b.txt\n"), 0o644))
		assert.Error(t, copyIncluded(root, dst))
	})
	t.Run("destination is a directory", func(t *testing.T) {
		dst := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dst, "a.txt"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, IncludeFile), []byte("a.txt\n"), 0o644))
		assert.Error(t, copyIncluded(root, dst))
	})
	if os.Geteuid() != 0 { // root reads anything
		t.Run("unreadable", func(t *testing.T) {
			require.NoError(t, os.Chmod(filepath.Join(root, "a.txt"), 0))
			t.Cleanup(func() { os.Chmod(filepath.Join(root, "a.txt"), 0o644) })
			assert.Error(t, copyIncluded(root, t.TempDir()), "a file it can't read")
			require.NoError(t, os.Chmod(filepath.Join(root, IncludeFile), 0))
			assert.Error(t, copyIncluded(root, t.TempDir()), "an include file it can't read")
		})
	}
}
