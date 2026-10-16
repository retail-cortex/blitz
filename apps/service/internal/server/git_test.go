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

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A folder that isn't a repository says so, with git installed, and
// InitGitRepo makes it one; without git, NO_GIT.
func TestInitGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	_, s := serve(t, nil, text("hi"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ws := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	t.Run("a folder", func(t *testing.T) {
		dir := t.TempDir()
		st, err := ws.GetGitStatus(ctx, connect.NewRequest(&pb.GetGitStatusRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.True(t, st.Msg.Git)
		assert.False(t, st.Msg.Repo)
		res, err := ws.InitGitRepo(ctx, connect.NewRequest(&pb.InitGitRepoRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.True(t, res.Msg.Status.Repo)
		assert.NotEmpty(t, res.Msg.Status.Branch)
		d, err := ws.GetDiff(ctx, connect.NewRequest(&pb.GetDiffRequest{Workspace: dir, Git: true}))
		require.NoError(t, err, "git diff in the new repository")
		assert.Empty(t, d.Msg.Diff)
	})
	t.Run("no git", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("PATH", t.TempDir())
		_, err := ws.InitGitRepo(ctx, connect.NewRequest(&pb.InitGitRepoRequest{Workspace: dir}))
		code, info := errorReason(t, err)
		assert.Equal(t, connect.CodeFailedPrecondition, code)
		assert.Equal(t, "NO_GIT", info.Reason)
	})
}

// The commit dialog's calls: DraftCommit stages everything when asked and
// returns the files and the model's message; Commit commits them, and
// refuses an empty index.
func TestCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	_, s := serve(t, nil, text("hi"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ws := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()

	for _, call := range []func() error{
		func() error {
			_, err := ws.DraftCommit(ctx, connect.NewRequest(&pb.DraftCommitRequest{Workspace: "relative/dir"}))
			return err
		},
		func() error {
			_, err := ws.Commit(ctx, connect.NewRequest(&pb.CommitRequest{Workspace: "relative/dir", Message: "x"}))
			return err
		},
	} {
		assert.Error(t, call(), "a workspace must be an absolute folder")
	}

	_, err := ws.DraftCommit(ctx, connect.NewRequest(&pb.DraftCommitRequest{Workspace: dir}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, code)
	assert.Equal(t, "NOT_A_REPOSITORY", info.Reason)

	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cart.go"), []byte("package cart\n"), 0o644))

	d, err := ws.DraftCommit(ctx, connect.NewRequest(&pb.DraftCommitRequest{Workspace: dir, StageAll: true}))
	require.NoError(t, err)
	require.Len(t, d.Msg.Files, 1)
	assert.Equal(t, "cart.go", d.Msg.Files[0].Path)
	assert.Equal(t, "A", d.Msg.Files[0].Status)
	assert.NotEmpty(t, d.Msg.Message, "the test model's draft")

	c, err := ws.Commit(ctx, connect.NewRequest(&pb.CommitRequest{Workspace: dir, Message: "Add the cart"}))
	require.NoError(t, err)
	assert.NotEmpty(t, c.Msg.Hash)
	assert.Equal(t, "Add the cart", c.Msg.Subject)

	_, err = ws.Commit(ctx, connect.NewRequest(&pb.CommitRequest{Workspace: dir, Message: "again"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NOTHING_STAGED", info.Reason)
}

// What the desktop's Files tree and Changes view see as files are staged:
// each file's flag says how much of it is staged, and the git diff holds
// every change since the last commit, new files too.
func TestStagedFlagsAndDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	_, s := serve(t, nil, text("hi"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ws := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
	files := pb.NewFileServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	write := func(name, data string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644))
	}
	git("init", "-q")
	write("a.txt", "one\n")
	git("add", ".")
	git("commit", "-qm", "init")
	write("a.txt", "two\n")
	write("n.txt", "new\n")
	flags := func() map[string]string {
		t.Helper()
		l, err := files.ListDir(ctx, connect.NewRequest(&pb.ListDirRequest{Workspace: dir}))
		require.NoError(t, err)
		out := map[string]string{}
		for _, e := range l.Msg.Entries {
			out[e.Path] = e.Git + "/" + e.Staged
		}
		return out
	}
	assert.Equal(t, map[string]string{"a.txt": "modified/", "n.txt": "untracked/"}, flags())

	_, err := files.GitFileAction(ctx, connect.NewRequest(&pb.GitFileActionRequest{Workspace: dir, Path: "a.txt", Action: pb.GitAction_GIT_ACTION_STAGE}))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a.txt": "modified/all", "n.txt": "untracked/"}, flags())

	d, err := ws.GetDiff(ctx, connect.NewRequest(&pb.GetDiffRequest{Workspace: dir, Git: true}))
	require.NoError(t, err)
	assert.Contains(t, d.Msg.Diff, "+two", "a staged change")
	assert.Contains(t, d.Msg.Diff, "+new", "a new file")

	// View revisions, and a restore.
	h, err := files.GetFileHistory(ctx, connect.NewRequest(&pb.GetFileHistoryRequest{Workspace: dir, Path: "a.txt"}))
	require.NoError(t, err)
	require.Len(t, h.Msg.Revisions, 1)
	rev := h.Msg.Revisions[0]
	assert.Equal(t, "init", rev.Subject)
	assert.Equal(t, "a.txt", rev.Path)
	assert.NotNil(t, rev.When)
	rd, err := files.GetFileRevisionDiff(ctx, connect.NewRequest(&pb.GetFileRevisionDiffRequest{Workspace: dir, Path: "a.txt", Commit: rev.Commit}))
	require.NoError(t, err)
	assert.Contains(t, rd.Msg.Diff, "-one")
	_, err = files.GetFileRevisionDiff(ctx, connect.NewRequest(&pb.GetFileRevisionDiffRequest{Workspace: dir, Path: "a.txt", Commit: "HEAD"}))
	assert.Equal(t, "BAD_REVISION", reasonOf(err))
	_, err = files.RestoreFileRevision(ctx, connect.NewRequest(&pb.RestoreFileRevisionRequest{Workspace: dir, Path: "a.txt", Commit: rev.Commit, Version: "stale"}))
	assert.Equal(t, "FILE_CHANGED", reasonOf(err))
	rr, err := files.RestoreFileRevision(ctx, connect.NewRequest(&pb.RestoreFileRevisionRequest{Workspace: dir, Path: "a.txt", Commit: rev.Commit, Version: engine.FileVersion([]byte("two\n"))}))
	require.NoError(t, err)
	assert.Equal(t, engine.FileVersion([]byte("one\n")), rr.Msg.Version)

	// A folder's (here the workspace's) history: parents, what a commit
	// changed, two compared, and a restore's preview.
	all, err := files.GetFileHistory(ctx, connect.NewRequest(&pb.GetFileHistoryRequest{Workspace: dir, Path: "", Limit: 1}))
	require.NoError(t, err)
	require.Len(t, all.Msg.Revisions, 1)
	assert.Equal(t, 1, int(all.Msg.Revisions[0].Files))
	changes, err := files.GetFileRevisionDiff(ctx, connect.NewRequest(&pb.GetFileRevisionDiffRequest{Workspace: dir, Path: "", Commit: rev.Commit, Against: pb.RevisionDiff_REVISION_DIFF_PARENT}))
	require.NoError(t, err)
	assert.Contains(t, changes.Msg.Diff, "+one", "what the first commit changed")
	same, err := files.GetFileRevisionDiff(ctx, connect.NewRequest(&pb.GetFileRevisionDiffRequest{Workspace: dir, Path: "", Commit: rev.Commit, Against: pb.RevisionDiff_REVISION_DIFF_OTHER, OtherCommit: rev.Commit}))
	require.NoError(t, err)
	assert.Empty(t, same.Msg.Diff, "a commit against itself")
	preview, err := files.RestoreFileRevision(ctx, connect.NewRequest(&pb.RestoreFileRevisionRequest{Workspace: dir, Path: "", Commit: rev.Commit, Preview: true}))
	require.NoError(t, err)
	assert.Empty(t, preview.Msg.Files, "a.txt is as it was; n.txt, added since, is kept")

	// Remove from .gitignore, and a rule it can't remove.
	write(".gitignore", "*.log\n")
	write("x.log", "x")
	l, err := files.ListDir(ctx, connect.NewRequest(&pb.ListDirRequest{Workspace: dir, ShowHidden: true}))
	require.NoError(t, err)
	for _, e := range l.Msg.Entries {
		assert.Equal(t, e.Path == "x.log", e.Ignored, e.Path)
	}
	_, err = files.GitFileAction(ctx, connect.NewRequest(&pb.GitFileActionRequest{Workspace: dir, Path: "x.log", Action: pb.GitAction_GIT_ACTION_UNIGNORE}))
	assert.Equal(t, "IGNORED_ELSEWHERE", reasonOf(err))
	_, err = files.GitFileAction(ctx, connect.NewRequest(&pb.GitFileActionRequest{Workspace: dir, Path: "n.txt", Action: pb.GitAction_GIT_ACTION_IGNORE}))
	require.NoError(t, err)
	_, err = files.GitFileAction(ctx, connect.NewRequest(&pb.GitFileActionRequest{Workspace: dir, Path: "n.txt", Action: pb.GitAction_GIT_ACTION_UNIGNORE}))
	require.NoError(t, err)
}

func TestCommitError(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{engine.ErrNotARepository, "NOT_A_REPOSITORY"},
		{engine.ErrNothingStaged, "NOTHING_STAGED"},
		{fmt.Errorf("wrapped: %w", engine.ErrEmptyMessage), "EMPTY_MESSAGE"},
		{errors.New("hook failed"), "GIT_FAILED"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, reasonOf(commitError(tt.err)))
		})
	}
}

// The Changes page's remote actions over the API: a pull that stops on a
// conflict, settled and completed, then a push; and errors with codes.
func TestPullResolvePush(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	empty := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	commit := func(dir, content string, push bool) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644))
		git(dir, "commit", "-qam", content)
		if push {
			git(dir, "push", "-q")
		}
	}
	bare, dir, other := t.TempDir(), t.TempDir(), t.TempDir()
	git(bare, "init", "-q", "--bare", "-b", "main")
	git(dir, "init", "-q", "-b", "main")
	git(dir, "config", "user.email", "t@t")
	git(dir, "config", "user.name", "t")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\n"), 0o644))
	git(dir, "add", "f.txt")
	git(dir, "commit", "-qm", "init")
	git(dir, "remote", "add", "origin", bare)
	git(dir, "push", "-q", "-u", "origin", "main")
	git(other, "clone", "-q", bare, ".")
	commit(other, "theirs\n", true)
	commit(dir, "mine\n", false)

	_, s := serve(t, nil, text("hi"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ws := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	f, err := ws.FetchRemote(ctx, connect.NewRequest(&pb.FetchRemoteRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.True(t, f.Msg.Status.Remote)
	assert.Equal(t, "origin/main", f.Msg.Status.Upstream)
	assert.Equal(t, int32(1), f.Msg.Status.Ahead)
	assert.Equal(t, int32(1), f.Msg.Status.Behind)

	_, err = ws.Push(ctx, connect.NewRequest(&pb.PushRequest{Workspace: dir}))
	_, info := errorReason(t, err)
	assert.Equal(t, "PUSH_REJECTED", info.Reason)
	_, err = ws.ResolveConflict(ctx, connect.NewRequest(&pb.ResolveConflictRequest{Workspace: dir, Path: "f.txt", Choice: pb.ConflictChoice_CONFLICT_CHOICE_MINE}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NO_MERGE", info.Reason, "no pull has stopped yet")

	// Not a repository: each says so.
	plain := t.TempDir()
	_, err = ws.FetchRemote(ctx, connect.NewRequest(&pb.FetchRemoteRequest{Workspace: plain}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_A_REPOSITORY", info.Reason)
	_, err = ws.Pull(ctx, connect.NewRequest(&pb.PullRequest{Workspace: plain}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NOT_A_REPOSITORY", info.Reason)

	p, err := ws.Pull(ctx, connect.NewRequest(&pb.PullRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Equal(t, pb.PullOutcome_PULL_OUTCOME_CONFLICTS, p.Msg.Outcome)
	assert.Equal(t, int32(1), p.Msg.Commits)
	assert.Equal(t, pb.GitOperation_GIT_OPERATION_MERGE, p.Msg.Status.Operation)
	assert.Equal(t, []string{"f.txt"}, p.Msg.Status.Conflicts)

	_, err = ws.ResolveConflict(ctx, connect.NewRequest(&pb.ResolveConflictRequest{Workspace: dir, Path: "f.txt"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "INVALID_CHOICE", info.Reason)
	_, err = ws.CompleteMerge(ctx, connect.NewRequest(&pb.CompleteMergeRequest{Workspace: dir}))
	_, info = errorReason(t, err)
	assert.Equal(t, "CONFLICTS_REMAIN", info.Reason)

	r, err := ws.ResolveConflict(ctx, connect.NewRequest(&pb.ResolveConflictRequest{Workspace: dir, Path: "f.txt", Choice: pb.ConflictChoice_CONFLICT_CHOICE_THEIRS}))
	require.NoError(t, err)
	assert.Empty(t, r.Msg.Status.Conflicts)
	c, err := ws.CompleteMerge(ctx, connect.NewRequest(&pb.CompleteMergeRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Equal(t, pb.GitOperation_GIT_OPERATION_UNSPECIFIED, c.Msg.Status.Operation)
	got, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	require.NoError(t, err)
	assert.Equal(t, "theirs\n", string(got))

	pushed, err := ws.Push(ctx, connect.NewRequest(&pb.PushRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Equal(t, "origin/main", pushed.Msg.Target)
	assert.Zero(t, pushed.Msg.Status.Ahead)

	_, err = ws.AbortMerge(ctx, connect.NewRequest(&pb.AbortMergeRequest{Workspace: dir}))
	_, info = errorReason(t, err)
	assert.Equal(t, "NO_MERGE", info.Reason)

	// A merge or rebase, as the status reports it.
	git(other, "pull", "-q", "--no-rebase")
	commit(other, "theirs again\n", true)
	commit(dir, "mine again\n", false)
	git(dir, "config", "pull.rebase", "true")
	p, err = ws.Pull(ctx, connect.NewRequest(&pb.PullRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Equal(t, pb.GitOperation_GIT_OPERATION_REBASE, p.Msg.Status.Operation)
	a, err := ws.AbortMerge(ctx, connect.NewRequest(&pb.AbortMergeRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Equal(t, pb.GitOperation_GIT_OPERATION_UNSPECIFIED, a.Msg.Status.Operation)

	for _, call := range []func() error{
		func() error {
			_, err := ws.FetchRemote(ctx, connect.NewRequest(&pb.FetchRemoteRequest{Workspace: "relative"}))
			return err
		},
		func() error {
			_, err := ws.Pull(ctx, connect.NewRequest(&pb.PullRequest{Workspace: "relative"}))
			return err
		},
		func() error {
			_, err := ws.Push(ctx, connect.NewRequest(&pb.PushRequest{Workspace: "relative"}))
			return err
		},
		func() error {
			_, err := ws.ResolveConflict(ctx, connect.NewRequest(&pb.ResolveConflictRequest{Workspace: "relative", Choice: pb.ConflictChoice_CONFLICT_CHOICE_MINE}))
			return err
		},
		func() error {
			_, err := ws.CompleteMerge(ctx, connect.NewRequest(&pb.CompleteMergeRequest{Workspace: "relative"}))
			return err
		},
		func() error {
			_, err := ws.AbortMerge(ctx, connect.NewRequest(&pb.AbortMergeRequest{Workspace: "relative"}))
			return err
		},
	} {
		assert.Error(t, call(), "a workspace must be an absolute folder")
	}
}

func TestRemoteError(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{&engine.RepoConfigError{Key: "core.sshcommand"}, "REPO_CONFIG_COMMAND"},
		{engine.ErrNotARepository, "NOT_A_REPOSITORY"},
		{engine.ErrNoRemote, "NO_REMOTE"},
		{engine.ErrNoUpstream, "NO_UPSTREAM"},
		{engine.ErrInProgress, "GIT_BUSY"},
		{engine.ErrPushRejected, "PUSH_REJECTED"},
		{fmt.Errorf("%w (denied)", engine.ErrRemoteAuth), "REMOTE_AUTH"},
		{engine.ErrNoOperation, "NO_MERGE"},
		{engine.ErrConflictsRemain, "CONFLICTS_REMAIN"},
		{engine.ErrMarkersRemain, "MARKERS_REMAIN"},
		{engine.ErrNotConflicted, "NOT_CONFLICTED"},
		{engine.ErrBadPath, "BAD_PATH"},
		{engine.ErrNoBranch, "NO_BRANCH"},
		{engine.ErrBranchExists, "BRANCH_EXISTS"},
		{engine.ErrBadBranchName, "BAD_BRANCH_NAME"},
		{fmt.Errorf("%w (f.txt)", engine.ErrLocalChanges), "LOCAL_CHANGES"},
		{engine.ErrMergeSelf, "MERGE_SELF"},
		{errors.New("network down"), "GIT_FAILED"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, reasonOf(remoteError(tt.err)))
		})
	}
}

func TestBranches(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	empty := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	git("init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\n"), 0o644))
	git("add", "f.txt")
	git("commit", "-qm", "init")

	_, s := serve(t, nil, text("hi"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ws := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	c, err := ws.CreateBranch(ctx, connect.NewRequest(&pb.CreateBranchRequest{Workspace: dir, Branch: "feature"}))
	require.NoError(t, err)
	assert.Equal(t, "feature", c.Msg.Status.Branch)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feature\n"), 0o644))
	git("commit", "-qam", "feature work")

	l, err := ws.ListBranches(ctx, connect.NewRequest(&pb.ListBranchesRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, l.Msg.Branches, 2)
	assert.Equal(t, "feature", l.Msg.Branches[0].Name, "newest commit first")
	assert.True(t, l.Msg.Branches[0].Current)
	assert.Equal(t, "feature work", l.Msg.Branches[0].Subject)
	assert.NotNil(t, l.Msg.Branches[0].Time)

	sw, err := ws.SwitchBranch(ctx, connect.NewRequest(&pb.SwitchBranchRequest{Workspace: dir, Branch: "main"}))
	require.NoError(t, err)
	assert.Equal(t, "main", sw.Msg.Status.Branch)

	m, err := ws.MergeBranch(ctx, connect.NewRequest(&pb.MergeBranchRequest{Workspace: dir, Branch: "feature"}))
	require.NoError(t, err)
	assert.Equal(t, pb.PullOutcome_PULL_OUTCOME_FAST_FORWARD, m.Msg.Outcome)
	assert.Equal(t, int32(1), m.Msg.Commits)

	tests := []struct {
		name string
		call func() error
		want string
	}{
		{"switch to a missing branch", func() error {
			_, err := ws.SwitchBranch(ctx, connect.NewRequest(&pb.SwitchBranchRequest{Workspace: dir, Branch: "nope"}))
			return err
		}, "NO_BRANCH"},
		{"create a taken name", func() error {
			_, err := ws.CreateBranch(ctx, connect.NewRequest(&pb.CreateBranchRequest{Workspace: dir, Branch: "feature"}))
			return err
		}, "BRANCH_EXISTS"},
		{"create a bad name", func() error {
			_, err := ws.CreateBranch(ctx, connect.NewRequest(&pb.CreateBranchRequest{Workspace: dir, Branch: "a..b"}))
			return err
		}, "BAD_BRANCH_NAME"},
		{"merge the current branch", func() error {
			_, err := ws.MergeBranch(ctx, connect.NewRequest(&pb.MergeBranchRequest{Workspace: dir, Branch: "main"}))
			return err
		}, "MERGE_SELF"},
		{"list outside a repository", func() error {
			_, err := ws.ListBranches(ctx, connect.NewRequest(&pb.ListBranchesRequest{Workspace: t.TempDir()}))
			return err
		}, "NOT_A_REPOSITORY"},
		{"a relative workspace", func() error {
			_, err := ws.ListBranches(ctx, connect.NewRequest(&pb.ListBranchesRequest{Workspace: "relative"}))
			return err
		}, "INVALID_WORKSPACE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, info := errorReason(t, tt.call())
			assert.Equal(t, tt.want, info.Reason)
		})
	}
}
