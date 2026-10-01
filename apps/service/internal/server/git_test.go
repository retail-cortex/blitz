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
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"connectrpc.com/connect"
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
