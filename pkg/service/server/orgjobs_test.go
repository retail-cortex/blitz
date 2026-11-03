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
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/enterprise/enterprisetest"
	"github.com/retail-cortex/blitz/pkg/orgsync"
	"github.com/retail-cortex/blitz/pkg/secrets"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A job in Studio the service hasn't got says why, with what to do (the
// owner, 2026-11-02: "doesn't seem to have a matching action"): its
// WORKER.md is where the service doesn't look; it isn't on main, and
// Push to main puts it there and has the service read it; sync is putting
// it there; or main has it and the service doesn't run it.
func TestWhyNotServiceJob(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	idp := enterprisetest.NewProvider(t)
	org := enterprisetest.NewService(t, idp.URL)
	org.AddWorkspace(t, "plans", "Plans", "editor")
	org.Mu.Lock()
	ws := org.Workspaces["plans"]
	ws.Sync = false // nothing pushes it but Push to main
	org.Workspaces["plans"] = ws
	org.Mu.Unlock()
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	_, err := c.ConnectEnterprise(ctx, connect.NewRequest(&pb.ConnectEnterpriseRequest{Url: org.URL}))
	require.NoError(t, err)
	stream, err := c.EnterpriseSignIn(ctx, connect.NewRequest(&pb.EnterpriseSignInRequest{Device: true}))
	require.NoError(t, err)
	for stream.Receive() {
	}
	require.NoError(t, stream.Err())
	_ = stream.Close()
	token, err := s.ent.c.AccessToken(ctx)
	require.NoError(t, err)
	t.Setenv("BLITZ_TEST_TOKEN", token)
	s.ent.gitHelper = `!f() { echo username=blitz; echo "password=$BLITZ_TEST_TOKEN"; }; f`
	opened, err := c.OpenOrgWorkspace(ctx, connect.NewRequest(&pb.OpenOrgWorkspaceRequest{Handle: "plans"}))
	require.NoError(t, err)
	dir := opened.Msg.Dir
	for _, kv := range [][2]string{{"user.name", "Ana"}, {"user.email", "ana@acme.com"}} {
		require.NoError(t, exec.Command("git", "-C", dir, "config", kv[0], kv[1]).Run())
	}
	write := func(name, front string) string {
		p := filepath.Join(dir, ".agents", "workers", name, "WORKER.md")
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("---\n"+front+"\n---\nWrite it.\n"), 0o644))
		return p
	}
	why := func(name, path string) *pb.GetServiceJobResponse {
		t.Helper()
		got, err := c.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: dir, Name: name, Path: path}))
		require.NoError(t, err)
		return got.Msg
	}

	t.Run("where the service doesn't look", func(t *testing.T) {
		got := why("digest", filepath.Join(dir, "jobs", "digest.md"))
		assert.Equal(t, "outside", got.Reason)
		assert.Equal(t, ".agents/workers/digest/WORKER.md", got.Detail)
	})

	t.Run("not on main, then pushed and read", func(t *testing.T) {
		p := write("digest", "runs_on: service\nschedule: Daily at 6 AM")
		got := why("digest", p)
		assert.Equal(t, "not_pushed", got.Reason, "not committed")
		assert.Empty(t, org.Reads, "nothing to read yet")

		pushed, err := c.PushServiceJob(ctx, connect.NewRequest(&pb.PushServiceJobRequest{Workspace: dir, Name: "digest"}))
		require.NoError(t, err)
		require.True(t, pushed.Msg.Job.Found, "%+v", pushed.Msg.Job)
		assert.Equal(t, "job-digest", pushed.Msg.Job.Job.Id)
		assert.Contains(t, org.Reads, "plans", "the service read main at once")
		log, err := exec.Command("git", "--git-dir", org.Repo("plans"), "log", "-1", "--format=%s", "main").Output()
		require.NoError(t, err)
		assert.Contains(t, string(log), "the job digest")
		assert.True(t, why("digest", p).Found)
	})

	t.Run("committed here but not pushed", func(t *testing.T) {
		write("tidy", "runs_on: service")
		require.NoError(t, exec.Command("git", "-C", dir, "add", "-A").Run())
		require.NoError(t, exec.Command("git", "-C", dir, "commit", "-qm", "tidy").Run())
		assert.Equal(t, "not_pushed", why("tidy", "").Reason)
	})

	t.Run("sync is putting it there", func(t *testing.T) {
		// A map of the test's own: runSyncs reads its own map unlocked, as
		// its only writer.
		s.ent.syncs.mu.Lock()
		was := s.ent.syncs.running
		s.ent.syncs.running = map[string]*orgSync{dir: {s: &orgsync.Syncer{Dir: dir}}}
		s.ent.syncs.mu.Unlock()
		defer func() {
			s.ent.syncs.mu.Lock()
			s.ent.syncs.running = was
			s.ent.syncs.mu.Unlock()
		}()
		got := why("tidy", "")
		assert.Equal(t, "saving", got.Reason)
		assert.Equal(t, orgsync.Saving, got.SyncState)
	})

	t.Run("sync refused by the service", func(t *testing.T) {
		// A syncer whose pushes the service turns down, as go-git's thin
		// packs did: the window and the job say why, never offline.
		refused := func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("exit status 1: error: remote unpack failed: reference delta not found")
		}
		sy := &orgsync.Syncer{Dir: dir, Git: refused, SignedIn: func() bool { return true }, Poll: 10 * time.Millisecond}
		rctx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { sy.Run(rctx); close(done) }()
		defer func() { stop(); <-done }()
		require.Eventually(t, func() bool { return sy.State().State == orgsync.Refused }, 5*time.Second, 10*time.Millisecond)
		s.ent.syncs.mu.Lock()
		was := s.ent.syncs.running
		s.ent.syncs.running = map[string]*orgSync{dir: {s: sy}}
		s.ent.syncs.mu.Unlock()
		defer func() {
			s.ent.syncs.mu.Lock()
			s.ent.syncs.running = was
			s.ent.syncs.mu.Unlock()
		}()
		st, err := c.GetOrgSync(ctx, connect.NewRequest(&pb.GetOrgSyncRequest{Workspace: dir}))
		require.NoError(t, err)
		assert.Equal(t, orgsync.Refused, st.Msg.State)
		assert.Contains(t, st.Msg.Error, "reference delta not found")
		got := why("tidy", "")
		assert.Equal(t, "saving", got.Reason)
		assert.Equal(t, orgsync.Refused, got.SyncState)
		assert.Contains(t, got.Detail, "remote unpack failed")
	})

	t.Run("on main, but not the service's", func(t *testing.T) {
		write("mine", "schedule: hourly")
		_, err := c.PushServiceJob(ctx, connect.NewRequest(&pb.PushServiceJobRequest{Workspace: dir, Name: "mine"}))
		require.NoError(t, err)
		got := why("mine", "")
		assert.Equal(t, "not_service", got.Reason)
		assert.Contains(t, got.Detail, "runs_on: service")
		write("odd", "runs_on: moon")
		pushed, err := c.PushServiceJob(ctx, connect.NewRequest(&pb.PushServiceJobRequest{Workspace: dir, Name: "odd"}))
		require.NoError(t, err)
		assert.Equal(t, "not_service", pushed.Msg.Job.Reason)
		assert.Contains(t, pushed.Msg.Job.Detail, "doesn't read")
	})

	t.Run("a service that can't say", func(t *testing.T) {
		org.Mu.Lock()
		org.JobsRefused = true
		org.Mu.Unlock()
		defer func() {
			org.Mu.Lock()
			org.JobsRefused = false
			org.Mu.Unlock()
		}()
		_, err := c.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: dir, Name: "ghost"}))
		assert.Error(t, err, "its jobs refused")
	})

	t.Run("on main nowhere", func(t *testing.T) {
		got := why("ghost", "")
		assert.Equal(t, "not_read", got.Reason)
		assert.Empty(t, got.Detail)

		// The service can't say: it doesn't know the workspace.
		org.Mu.Lock()
		w := org.Workspaces["plans"]
		delete(org.Workspaces, "plans")
		org.Mu.Unlock()
		defer func() {
			org.Mu.Lock()
			org.Workspaces["plans"] = w
			org.Mu.Unlock()
		}()
		got = why("ghost", "")
		assert.Equal(t, "not_read", got.Reason)
		assert.NotEmpty(t, got.Detail)
	})

	t.Run("refusals", func(t *testing.T) {
		_, err := c.PushServiceJob(ctx, connect.NewRequest(&pb.PushServiceJobRequest{Workspace: dir, Name: "nowhere"}))
		_, info := errorReason(t, err)
		assert.Equal(t, "PUSH_FAILED", info.Reason, "no such folder to commit")

		for _, name := range []string{"", "..", "a/b"} {
			_, err = c.PushServiceJob(ctx, connect.NewRequest(&pb.PushServiceJobRequest{Workspace: dir, Name: name}))
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), name)
		}
		_, err = c.PushServiceJob(ctx, connect.NewRequest(&pb.PushServiceJobRequest{Workspace: t.TempDir(), Name: "x"}))
		_, info = errorReason(t, err)
		assert.Equal(t, "NOT_ORG_WORKSPACE", info.Reason)

		// A push the service refuses says so.
		write("late", "runs_on: service")
		t.Setenv("BLITZ_TEST_TOKEN", "not a token")
		_, err = c.PushServiceJob(ctx, connect.NewRequest(&pb.PushServiceJobRequest{Workspace: dir, Name: "late"}))
		_, info = errorReason(t, err)
		assert.Equal(t, "PUSH_FAILED", info.Reason)
	})
}

// A folder shared in place follows the organization's main (Changes'
// Pull and its counts need an upstream), unless it follows another.
func TestTrackShared(t *testing.T) {
	ctx := context.Background()
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Ana", "-c", "user.email=ana@acme.com", "-c", "init.defaultBranch=main"}, args...)...).CombinedOutput()
		require.NoError(t, err, "%s", out)
		return string(out)
	}
	upstream := func(dir string) string {
		out, _ := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}").Output()
		return string(out)
	}
	bare, other, dir := t.TempDir(), t.TempDir(), t.TempDir()
	git(bare, "init", "-q", "--bare")
	git(other, "init", "-q", "--bare")
	git(dir, "init", "-q")
	git(dir, "commit", "-q", "--allow-empty", "-m", "first")

	trackShared(ctx, dir, "organization")
	assert.Empty(t, upstream(dir), "no such remote")
	git(dir, "remote", "add", "organization", bare)
	trackShared(ctx, dir, "organization")
	assert.Empty(t, upstream(dir), "nothing on its main yet")
	git(dir, "push", "-q", "organization", "HEAD:main")
	trackShared(ctx, dir, "origin")
	assert.Empty(t, upstream(dir), "a clone's origin is Git's own business")
	trackShared(ctx, dir, "organization")
	assert.Equal(t, "organization/main\n", upstream(dir))

	// A detached HEAD has no branch to follow anything: left as it is.
	git(dir, "checkout", "-q", "--detach")
	git(dir, "branch", "-q", "--unset-upstream", "main")
	trackShared(ctx, dir, "organization")
	git(dir, "checkout", "-q", "main")
	assert.Empty(t, upstream(dir))
	trackShared(ctx, dir, "organization")
	assert.Equal(t, "organization/main\n", upstream(dir))

	// One that follows another remote keeps it.
	git(dir, "remote", "add", "upstream", other)
	git(dir, "push", "-q", "-u", "upstream", "HEAD:main")
	trackShared(ctx, dir, "organization")
	assert.Equal(t, "upstream/main\n", upstream(dir))
}
