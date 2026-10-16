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

package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// remoteRepos makes a bare remote, the workspace's clone of it (main, one
// commit of f.txt and g.txt, tracking origin/main), and someone else's
// clone, which pushes. Git reads no global or system config, so the
// user's own can't change how pulls go.
func remoteRepos(t *testing.T) (w *Workspace, other string) {
	t.Helper()
	needGit(t)
	empty := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	bare := t.TempDir()
	gitOut(t, bare, "init", "-q", "--bare", "-b", "main")
	w = openTest(t)
	dir := w.Dir()
	gitOut(t, dir, "init", "-q", "-b", "main")
	gitOut(t, dir, "config", "user.email", "me@example.com")
	gitOut(t, dir, "config", "user.name", "Me")
	write(t, dir, "f.txt", "one\ntwo\nthree\n")
	write(t, dir, "g.txt", "g\n")
	gitOut(t, dir, "add", "f.txt", "g.txt")
	gitOut(t, dir, "commit", "-qm", "init")
	gitOut(t, dir, "remote", "add", "origin", bare)
	gitOut(t, dir, "push", "-q", "-u", "origin", "main")

	other = t.TempDir()
	gitOut(t, other, "clone", "-q", bare, ".")
	return w, other
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	return string(b)
}

// commitIn commits name with content in dir, and pushes it when push.
func commitIn(t *testing.T, dir, name, content, msg string, push bool) {
	t.Helper()
	write(t, dir, name, content)
	gitOut(t, dir, "add", name)
	gitOut(t, dir, "commit", "-qm", msg)
	if push {
		gitOut(t, dir, "push", "-q")
	}
}

func TestRemoteStatusAndFetch(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()

	st, err := w.GitStatus(ctx)
	require.NoError(t, err)
	assert.True(t, st.Remote)
	assert.Equal(t, "origin/main", st.Upstream)
	assert.Zero(t, st.Ahead)
	assert.Zero(t, st.Behind)
	assert.Empty(t, st.Operation)

	commitIn(t, other, "h.txt", "h\n", "theirs", true)
	commitIn(t, w.Dir(), "i.txt", "i\n", "mine", false)
	st, err = w.Fetch(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, st.Ahead, "my commit")
	assert.Equal(t, 1, st.Behind, "theirs, once fetched")
}

func TestPull(t *testing.T) {
	tests := []struct {
		name     string
		rebase   bool
		mine     bool // a commit of my own first
		want     PullOutcome
		wantHead int // parents of HEAD after
	}{
		{"fast-forward", false, false, PullFastForward, 1},
		{"merge", false, true, PullMerged, 2},
		{"rebase, as config says", true, true, PullRebased, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, other := remoteRepos(t)
			ctx := context.Background()
			dir := w.Dir()
			if tt.rebase {
				gitOut(t, dir, "config", "pull.rebase", "true")
			}
			commitIn(t, other, "h.txt", "h\n", "theirs", true)
			if tt.mine {
				commitIn(t, dir, "i.txt", "i\n", "mine", false)
			}
			write(t, dir, "g.txt", "g, not yet committed\n") // kept (--autostash)

			res, err := w.Pull(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, res.Outcome)
			assert.Equal(t, 1, res.Commits)
			assert.Equal(t, "h\n", read(t, dir, "h.txt"))
			assert.Equal(t, "g, not yet committed\n", read(t, dir, "g.txt"))
			parents := len(strings.Fields(gitOut(t, dir, "log", "-1", "--format=%P")))
			assert.Equal(t, tt.wantHead, parents)
			assert.Zero(t, res.Status.Behind)

			res, err = w.Pull(ctx)
			require.NoError(t, err)
			assert.Equal(t, PullUpToDate, res.Outcome)
			assert.Zero(t, res.Commits)
		})
	}
}

// conflictCase is a pull whose changes clash, settled one way.
type conflictCase struct {
	name   string
	rebase bool
	choice ConflictChoice
	edit   string // the file as edited by hand, for MarkResolved
	want   string
	ahead  int // mine and the merge commit; a rebase replays mine, or drops it once empty
}

// A pull whose changes clash stops with the conflicts; each file is
// settled (mine or theirs, or edited by hand), and the merge completes.
// (Merges and rebases are separate tests, so they run on separate shards.)
func TestPullConflictsMerge(t *testing.T) {
	pullConflicts(t, []conflictCase{
		{"keep mine", false, KeepMine, "", "one\nmine\nthree\n", 2},
		{"take theirs", false, TakeTheirs, "", "one\ntheirs\nthree\n", 2},
		{"edited", false, MarkResolved, "one\nboth\nthree\n", "one\nboth\nthree\n", 2},
	})
}

// As TestPullConflictsMerge, for a rebase, which swaps mine and theirs in
// git's terms.
func TestPullConflictsRebase(t *testing.T) {
	pullConflicts(t, []conflictCase{
		{"keep mine", true, KeepMine, "", "one\nmine\nthree\n", 1},
		{"take theirs", true, TakeTheirs, "", "one\ntheirs\nthree\n", 0},
	})
}

func pullConflicts(t *testing.T, tests []conflictCase) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, other := remoteRepos(t)
			ctx := context.Background()
			dir := w.Dir()
			if tt.rebase {
				gitOut(t, dir, "config", "pull.rebase", "true")
			}
			commitIn(t, other, "f.txt", "one\ntheirs\nthree\n", "theirs", true)
			commitIn(t, dir, "f.txt", "one\nmine\nthree\n", "mine", false)

			res, err := w.Pull(ctx)
			require.NoError(t, err)
			require.Equal(t, PullConflicts, res.Outcome)
			assert.Equal(t, 1, res.Commits)
			op := api.GitMerging
			if tt.rebase {
				op = api.GitRebasing
			}
			assert.Equal(t, op, res.Status.Operation)
			assert.Equal(t, []string{"f.txt"}, res.Status.Conflicts)

			_, err = w.CompleteMerge(ctx)
			require.ErrorIs(t, err, ErrConflictsRemain)
			_, err = w.ResolveConflict(ctx, "f.txt", MarkResolved)
			require.ErrorIs(t, err, ErrMarkersRemain, "git's markers are still in it")
			_, err = w.Pull(ctx)
			require.ErrorIs(t, err, ErrInProgress, "a pull waits for the merge to end")
			_, _, err = w.Push(ctx)
			require.ErrorIs(t, err, ErrInProgress)

			if tt.edit != "" {
				write(t, dir, "f.txt", tt.edit)
			}
			st, err := w.ResolveConflict(ctx, "f.txt", tt.choice)
			require.NoError(t, err)
			assert.Empty(t, st.Conflicts)
			assert.Equal(t, tt.want, read(t, dir, "f.txt"))

			st, err = w.CompleteMerge(ctx)
			require.NoError(t, err)
			assert.Empty(t, st.Operation)
			assert.Equal(t, tt.want, read(t, dir, "f.txt"))
			assert.Equal(t, tt.ahead, st.Ahead)
			assert.Zero(t, st.Behind)
		})
	}
}

// Aborting puts the branch, and the changes not yet committed, back.
func TestAbortMerge(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	dir := w.Dir()
	commitIn(t, other, "f.txt", "one\ntheirs\nthree\n", "theirs", true)
	commitIn(t, dir, "f.txt", "one\nmine\nthree\n", "mine", false)
	write(t, dir, "g.txt", "g, not yet committed\n")
	head := w.head(ctx)

	res, err := w.Pull(ctx)
	require.NoError(t, err)
	require.Equal(t, PullConflicts, res.Outcome)
	st, err := w.AbortMerge(ctx)
	require.NoError(t, err)
	assert.Empty(t, st.Operation)
	assert.Equal(t, head, w.head(ctx))
	assert.Equal(t, "one\nmine\nthree\n", read(t, dir, "f.txt"))
	assert.Equal(t, "g, not yet committed\n", read(t, dir, "g.txt"))

	_, err = w.AbortMerge(ctx)
	require.ErrorIs(t, err, ErrNoOperation)
	_, err = w.CompleteMerge(ctx)
	require.ErrorIs(t, err, ErrNoOperation)
	_, err = w.ResolveConflict(ctx, "f.txt", KeepMine)
	require.ErrorIs(t, err, ErrNoOperation)
}

func TestPush(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	dir := w.Dir()

	commitIn(t, dir, "i.txt", "i\n", "mine", false)
	to, st, err := w.Push(ctx)
	require.NoError(t, err)
	assert.Equal(t, "origin/main", to)
	assert.Zero(t, st.Ahead)
	gitOut(t, other, "pull", "-q")
	assert.Equal(t, "i\n", read(t, other, "i.txt"))

	// The remote moved on: pull first.
	commitIn(t, other, "h.txt", "h\n", "theirs", true)
	commitIn(t, dir, "j.txt", "j\n", "mine again", false)
	_, _, err = w.Push(ctx)
	require.ErrorIs(t, err, ErrPushRejected)

	// A new branch is published, and tracks the remote's from then on.
	gitOut(t, dir, "switch", "-q", "-c", "feature")
	to, st, err = w.Push(ctx)
	require.NoError(t, err)
	assert.Equal(t, "origin/feature", to)
	assert.Equal(t, "origin/feature", st.Upstream)
}

func TestRemoteActionsNeedARemote(t *testing.T) {
	needGit(t)
	w := openTest(t)
	ctx := context.Background()
	_, err := w.Pull(ctx)
	require.ErrorIs(t, err, ErrNotARepository)

	gitOut(t, w.Dir(), "init", "-q")
	_, _, err = w.Push(ctx)
	require.ErrorIs(t, err, ErrNoRemote)
	_, err = w.Fetch(ctx)
	require.ErrorIs(t, err, ErrNoRemote)
	_, err = w.Pull(ctx)
	require.ErrorIs(t, err, ErrNoUpstream)
}

// Settings that run commands are taken from the user's config, never the
// repository's (which the agent can write), including what it includes.
func TestRepoConfigThatRunsCommandsIsRefused(t *testing.T) {
	tests := []struct {
		name string
		set  func(t *testing.T, dir string)
		key  string
	}{
		{"ssh command", func(t *testing.T, dir string) {
			gitOut(t, dir, "config", "core.sshCommand", "touch pwned; ssh")
		}, "core.sshcommand"},
		{"credential helper", func(t *testing.T, dir string) {
			gitOut(t, dir, "config", "credential.https://example.com.helper", "!touch pwned")
		}, "credential.https://example.com.helper"},
		{"filter", func(t *testing.T, dir string) {
			gitOut(t, dir, "config", "filter.x.smudge", "touch pwned")
		}, "filter.x.smudge"},
		{"merge driver", func(t *testing.T, dir string) {
			gitOut(t, dir, "config", "merge.x.driver", "touch pwned")
		}, "merge.x.driver"},
		{"included", func(t *testing.T, dir string) {
			inc := filepath.Join(t.TempDir(), "inc")
			require.NoError(t, os.WriteFile(inc, []byte("[core]\n\tsshCommand = touch pwned\n"), 0o644))
			gitOut(t, dir, "config", "include.path", inc)
		}, "core.sshcommand"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := remoteRepos(t)
			ctx := context.Background()
			tt.set(t, w.Dir())
			_, err := w.Pull(ctx)
			var rc *RepoConfigError
			require.True(t, errors.As(err, &rc), "got %v", err)
			assert.Equal(t, tt.key, rc.Key)
			assert.Contains(t, rc.Error(), tt.key)
			_, _, err = w.Push(ctx)
			require.True(t, errors.As(err, &rc), "got %v", err)
			_, err = w.Fetch(ctx)
			require.True(t, errors.As(err, &rc), "got %v", err)
			_, err = os.Stat(filepath.Join(w.Dir(), "pwned"))
			assert.True(t, os.IsNotExist(err), "nothing ran")
		})
	}
}

func TestRemoteError(t *testing.T) {
	base := errors.New("exit status 128")
	tests := []struct {
		name string
		out  string
		want error
	}{
		{"https", "fatal: could not read Username for 'https://github.com': terminal prompts disabled", ErrRemoteAuth},
		{"ssh", "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.", ErrRemoteAuth},
		{"bad password", "remote: Invalid username or token.\nfatal: Authentication failed for 'https://github.com/a/b.git/'", ErrRemoteAuth},
		{"rejected", " ! [rejected]        main -> main (fetch first)\nerror: failed to push some refs", ErrPushRejected},
		{"other", "fatal: unable to access 'https://x/': Could not resolve host: x", base},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := remoteError(base, tt.out)
			require.ErrorIs(t, err, tt.want)
		})
	}
	require.Equal(t, base, remoteError(base, ""))
}

// A branch without an upstream is published to origin, or to the only
// remote; with several and no origin, Blitz doesn't guess.
func TestPublishChoosesTheRemote(t *testing.T) {
	tests := []struct {
		name    string
		remotes []string // added besides origin, which is renamed away when absent
		origin  bool
		want    string
		wantErr bool
	}{
		{"the only remote", nil, false, "upstream/feature", false},
		{"origin among several", []string{"fork"}, true, "origin/feature", false},
		{"several, none origin", []string{"fork"}, false, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := remoteRepos(t)
			ctx := context.Background()
			dir := w.Dir()
			bare := gitOut(t, dir, "remote", "get-url", "origin")
			if !tt.origin {
				gitOut(t, dir, "remote", "rename", "origin", "upstream")
			}
			for _, r := range tt.remotes {
				gitOut(t, dir, "remote", "add", r, bare)
			}
			gitOut(t, dir, "switch", "-q", "-c", "feature")
			to, _, err := w.Push(ctx)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, to)
		})
	}
}

// Without an upstream, a fetch brings every remote's branches.
func TestFetchWithoutUpstream(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	gitOut(t, w.Dir(), "switch", "-q", "-c", "local-only")
	gitOut(t, other, "switch", "-q", "-c", "theirs")
	commitIn(t, other, "h.txt", "h\n", "theirs", false)
	gitOut(t, other, "push", "-q", "-u", "origin", "theirs")
	st, err := w.Fetch(ctx)
	require.NoError(t, err)
	assert.Empty(t, st.Upstream)
	assert.Contains(t, gitOut(t, w.Dir(), "branch", "-r"), "origin/theirs")
}

func TestResolveConflictRefuses(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	commitIn(t, other, "f.txt", "one\ntheirs\nthree\n", "theirs", true)
	commitIn(t, w.Dir(), "f.txt", "one\nmine\nthree\n", "mine", false)
	res, err := w.Pull(ctx)
	require.NoError(t, err)
	require.Equal(t, PullConflicts, res.Outcome)

	_, err = w.ResolveConflict(ctx, "", KeepMine)
	require.ErrorIs(t, err, ErrBadPath)
	_, err = w.ResolveConflict(ctx, "../outside", KeepMine)
	require.Error(t, err)
	_, err = w.ResolveConflict(ctx, "g.txt", KeepMine)
	require.ErrorIs(t, err, ErrNotConflicted)
	_, err = w.ResolveConflict(ctx, "f.txt", ConflictChoice("both"))
	require.Error(t, err)

	gitOut(t, w.Dir(), "config", "core.sshCommand", "touch pwned; ssh")
	var rc *RepoConfigError
	_, err = w.ResolveConflict(ctx, "f.txt", KeepMine)
	require.True(t, errors.As(err, &rc), "got %v", err)
	_, err = w.CompleteMerge(ctx)
	require.True(t, errors.As(err, &rc), "got %v", err)
	_, err = w.AbortMerge(ctx)
	require.True(t, errors.As(err, &rc), "got %v", err)
}

// A file one side deleted and the other changed: taking the deleting
// side deletes it.
func TestResolveADeletedFile(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	dir := w.Dir()
	gitOut(t, other, "rm", "-q", "g.txt")
	gitOut(t, other, "commit", "-qm", "drop g")
	gitOut(t, other, "push", "-q")
	commitIn(t, dir, "g.txt", "g, changed\n", "change g", false)

	res, err := w.Pull(ctx)
	require.NoError(t, err)
	require.Equal(t, PullConflicts, res.Outcome)
	assert.Equal(t, []string{"g.txt"}, res.Status.Conflicts)
	st, err := w.ResolveConflict(ctx, "g.txt", TakeTheirs)
	require.NoError(t, err)
	assert.Empty(t, st.Conflicts)
	_, err = os.Stat(filepath.Join(dir, "g.txt"))
	assert.True(t, os.IsNotExist(err))
	st, err = w.CompleteMerge(ctx)
	require.NoError(t, err)
	assert.Empty(t, st.Operation)
}

// A rebase of two commits that both clash stops again on the second once
// the first is settled: continuing isn't an error, the status says so.
func TestRebaseStopsAgain(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	dir := w.Dir()
	gitOut(t, dir, "config", "pull.rebase", "true")
	commitIn(t, other, "f.txt", "one\ntheirs\nthree\n", "theirs f", false)
	commitIn(t, other, "g.txt", "g, theirs\n", "theirs g", true)
	commitIn(t, dir, "f.txt", "one\nmine\nthree\n", "mine f", false)
	commitIn(t, dir, "g.txt", "g, mine\n", "mine g", false)

	res, err := w.Pull(ctx)
	require.NoError(t, err)
	require.Equal(t, PullConflicts, res.Outcome)
	assert.Equal(t, []string{"f.txt"}, res.Status.Conflicts)
	_, err = w.ResolveConflict(ctx, "f.txt", KeepMine)
	require.NoError(t, err)
	st, err := w.CompleteMerge(ctx)
	require.NoError(t, err)
	assert.Equal(t, api.GitRebasing, st.Operation)
	assert.Equal(t, []string{"g.txt"}, st.Conflicts)
	_, err = w.ResolveConflict(ctx, "g.txt", KeepMine)
	require.NoError(t, err)
	st, err = w.CompleteMerge(ctx)
	require.NoError(t, err)
	assert.Empty(t, st.Operation)
	assert.Equal(t, "g, mine\n", read(t, dir, "g.txt"))
	assert.Equal(t, 2, st.Ahead)
}

// A remote that can't be reached fails each action with git's reason, and
// leaves nothing in progress.
func TestRemoteGone(t *testing.T) {
	w, _ := remoteRepos(t)
	ctx := context.Background()
	dir := w.Dir()
	gitOut(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone"))
	commitIn(t, dir, "i.txt", "i\n", "mine", false)

	_, err := w.Fetch(ctx)
	require.ErrorContains(t, err, "git fetch")
	res, err := w.Pull(ctx)
	require.ErrorContains(t, err, "git pull")
	assert.Empty(t, res.Status.Operation)
	_, _, err = w.Push(ctx)
	require.ErrorContains(t, err, "git push")
	require.NotErrorIs(t, err, ErrPushRejected)
}

// A pre-push hook that refuses stops the push with its words.
func TestPushHookRefuses(t *testing.T) {
	w, _ := remoteRepos(t)
	ctx := context.Background()
	dir := w.Dir()
	hook := filepath.Join(dir, ".git", "hooks", "pre-push")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho 'tests must pass first' >&2\nexit 1\n"), 0o755))
	commitIn(t, dir, "i.txt", "i\n", "mine", false)
	_, st, err := w.Push(ctx)
	require.ErrorContains(t, err, "tests must pass first")
	assert.Equal(t, 1, st.Ahead)
}

// A new repository with nothing committed yet, set to track a remote
// branch: the pull brings everything.
func TestPullIntoAnEmptyBranch(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	commitIn(t, other, "h.txt", "h\n", "theirs", true)
	bare := gitOut(t, w.Dir(), "remote", "get-url", "origin")

	fresh := openTest(t)
	dir := fresh.Dir()
	gitOut(t, dir, "init", "-q", "-b", "main")
	gitOut(t, dir, "remote", "add", "origin", bare)
	gitOut(t, dir, "fetch", "-q", "origin")
	gitOut(t, dir, "config", "branch.main.remote", "origin")
	gitOut(t, dir, "config", "branch.main.merge", "refs/heads/main")

	res, err := fresh.Pull(ctx)
	require.NoError(t, err)
	assert.Equal(t, PullFastForward, res.Outcome)
	assert.Equal(t, 2, res.Commits, "init and theirs")
	assert.Equal(t, "h\n", read(t, dir, "h.txt"))
}

// A commit hook that refuses the merge commit leaves the merge waiting,
// and says why.
func TestCompleteMergeHookRefuses(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	dir := w.Dir()
	commitIn(t, other, "f.txt", "one\ntheirs\nthree\n", "theirs", true)
	commitIn(t, dir, "f.txt", "one\nmine\nthree\n", "mine", false)
	res, err := w.Pull(ctx)
	require.NoError(t, err)
	require.Equal(t, PullConflicts, res.Outcome)
	_, err = w.ResolveConflict(ctx, "f.txt", KeepMine)
	require.NoError(t, err)

	hook := filepath.Join(dir, ".git", "hooks", "commit-msg")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho 'needs a ticket number' >&2\nexit 1\n"), 0o755))
	st, err := w.CompleteMerge(ctx)
	require.ErrorContains(t, err, "needs a ticket number")
	assert.Equal(t, api.GitMerging, st.Operation)
}

// A repository with linked worktrees (a background task's) and no
// per-worktree config: its config is still checked, and pulls work.
func TestRemoteActionsWithLinkedWorktrees(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	gitOut(t, w.Dir(), "worktree", "add", "-q", filepath.Join(t.TempDir(), "task"), "-b", "task")
	commitIn(t, other, "h.txt", "h\n", "theirs", true)
	res, err := w.Pull(ctx)
	require.NoError(t, err)
	assert.Equal(t, PullFastForward, res.Outcome)

	gitOut(t, w.Dir(), "config", "core.sshCommand", "touch pwned; ssh")
	_, err = w.Fetch(ctx)
	var rc *RepoConfigError
	require.True(t, errors.As(err, &rc), "got %v", err)
}
