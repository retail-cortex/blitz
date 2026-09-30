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

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	for _, args := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "first"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	return dir
}

func TestWorktreeFlagsAndCommand(t *testing.T) {
	isolate(t)
	repo := gitRepo(t)
	g := &globalFlags{dir: repo, worktree: "review"}
	require.NoError(t, enterWorktree(g))
	assert.Equal(t, filepath.Join(repo, ".blitz", "worktrees", "review"), g.dir, "the run's workspace")
	_, err := os.Stat(g.dir)
	require.NoError(t, err)

	g2 := &globalFlags{dir: t.TempDir(), worktree: "new"}
	assert.ErrorContains(t, enterWorktree(g2), "not in a git repository")

	out, err := runCLI(t, "worktrees", "list", "--dir", repo)
	require.NoError(t, err)
	assert.Contains(t, out, "review\tblitz/review")
	out, err = runCLI(t, "worktrees", "remove", "review", "--dir", repo)
	require.NoError(t, err)
	assert.Contains(t, out, "its branch blitz/review stays")
	out, err = runCLI(t, "worktrees", "list", "--dir", repo)
	require.NoError(t, err)
	assert.Contains(t, out, "No worktrees")
	_, err = runCLI(t, "worktrees", "prune", "--dir", repo)
	require.NoError(t, err)
}
