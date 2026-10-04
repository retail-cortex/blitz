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
