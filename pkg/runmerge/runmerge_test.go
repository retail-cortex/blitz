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

package runmerge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitIn(dir string) Git {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "user.name=Ana", "-c", "user.email=ana@acme.com", "-c", "init.defaultBranch=main"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
		return cmd.CombinedOutput()
	}
}

func must(t *testing.T, git Git, args ...string) string {
	t.Helper()
	out, err := git(context.Background(), args...)
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// write commits a file in dir.
func write(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	must(t, gitIn(dir), "add", "-A")
	must(t, gitIn(dir), "commit", "-q", "-m", name)
}

// repo is a workspace's repository with a first commit, a clone of it,
// and a run's branch there adding a recipe.
func repo(t *testing.T) (bare, clone string) {
	root := t.TempDir()
	bare, clone = filepath.Join(root, "ws.git"), filepath.Join(root, "clone")
	must(t, gitIn(root), "init", "-q", "--bare", bare)
	must(t, gitIn(root), "clone", "-q", bare, clone)
	write(t, clone, "plan.md", "# Plan\n")
	must(t, gitIn(clone), "push", "-q", "origin", "HEAD:main")
	must(t, gitIn(clone), "checkout", "-q", "-b", "run")
	write(t, clone, "recipe.md", "Soup.\n")
	must(t, gitIn(clone), "push", "-q", "origin", "HEAD:refs/heads/blitz/jobs/daily/r1")
	return bare, clone
}

// A run's changes reach main, however main moved meanwhile; ones that
// change the same lines as main stay on the run's branch.
func TestIntoMain(t *testing.T) {
	ctx := context.Background()
	t.Run("added", func(t *testing.T) {
		bare, clone := repo(t)
		require.NoError(t, IntoMain(ctx, gitIn(clone), "origin", "blitz/jobs/daily/r1", "Blitz job daily"))
		assert.Equal(t, "Soup.", must(t, gitIn(bare), "show", "main:recipe.md"))
		require.NoError(t, IntoMain(ctx, gitIn(clone), "origin", "blitz/jobs/daily/r1", "Blitz job daily"), "again: no change")
	})
	t.Run("main moved since the run began", func(t *testing.T) {
		bare, clone := repo(t)
		other := filepath.Join(t.TempDir(), "other")
		must(t, gitIn(filepath.Dir(other)), "clone", "-q", bare, other)
		write(t, other, "notes.md", "Notes.\n")
		must(t, gitIn(other), "push", "-q", "origin", "main")
		require.NoError(t, IntoMain(ctx, gitIn(clone), "origin", "blitz/jobs/daily/r1", "Blitz job daily"))
		assert.Equal(t, "Soup.", must(t, gitIn(bare), "show", "main:recipe.md"))
		assert.Equal(t, "Notes.", must(t, gitIn(bare), "show", "main:notes.md"), "nothing of main's lost")
		assert.Contains(t, must(t, gitIn(bare), "log", "-1", "--format=%an %s", "main"), "Blitz job daily Add blitz/jobs/daily/r1 to main")
	})
	t.Run("main pushed between the merge and its push", func(t *testing.T) {
		bare, clone := repo(t)
		other := filepath.Join(t.TempDir(), "other")
		must(t, gitIn(filepath.Dir(other)), "clone", "-q", bare, other)
		var once atomic.Bool
		racing := func(ctx context.Context, args ...string) ([]byte, error) {
			if args[0] == "push" && once.CompareAndSwap(false, true) {
				write(t, other, "late.md", "Late.\n")
				must(t, gitIn(other), "push", "-q", "origin", "main")
			}
			return gitIn(clone)(ctx, args...)
		}
		require.NoError(t, IntoMain(ctx, racing, "origin", "blitz/jobs/daily/r1", "Blitz job daily"))
		assert.Equal(t, "Late.", must(t, gitIn(bare), "show", "main:late.md"))
		assert.Equal(t, "Soup.", must(t, gitIn(bare), "show", "main:recipe.md"))
	})
	t.Run("conflicts", func(t *testing.T) {
		bare, clone := repo(t)
		must(t, gitIn(clone), "checkout", "-q", "-b", "clash", "origin/main")
		write(t, clone, "recipe.md", "Stew.\n")
		must(t, gitIn(clone), "push", "-q", "origin", "HEAD:main")
		err := IntoMain(ctx, gitIn(clone), "origin", "blitz/jobs/daily/r1", "Blitz job daily")
		require.ErrorIs(t, err, ErrConflicts)
		assert.Equal(t, "Stew.", must(t, gitIn(bare), "show", "main:recipe.md"), "main as it was")
		_, err = gitIn(clone)(ctx, "rev-parse", "-q", "--verify", "MERGE_HEAD")
		assert.Error(t, err, "no merge left half-done")
	})
	t.Run("git failing", func(t *testing.T) {
		for _, step := range []string{"checkout", "merge"} {
			_, clone := repo(t)
			failing := func(ctx context.Context, args ...string) ([]byte, error) {
				if args[0] == step || (len(args) > 4 && args[4] == step) {
					return nil, errors.New(step + " broke")
				}
				return gitIn(clone)(ctx, args...)
			}
			assert.ErrorContains(t, IntoMain(ctx, failing, "origin", "blitz/jobs/daily/r1", "Blitz job daily"), step+" broke", step)
		}
	})
	t.Run("no such branch", func(t *testing.T) {
		_, clone := repo(t)
		assert.ErrorContains(t, IntoMain(ctx, gitIn(clone), "origin", "blitz/jobs/daily/gone", "Blitz job daily"), "fetching main and blitz/jobs/daily/gone")
	})
	t.Run("never pushed", func(t *testing.T) {
		bare, clone := repo(t)
		hook := filepath.Join(bare, "hooks", "pre-receive")
		require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755))
		assert.ErrorContains(t, IntoMain(ctx, gitIn(clone), "origin", "blitz/jobs/daily/r1", "Blitz job daily"), "pushing main")
	})
}
