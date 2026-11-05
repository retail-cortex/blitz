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
	"strings"
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

	t.Run("git failing isn't read as changes", func(t *testing.T) {
		// The organization's remote isn't there: diff fails, which isn't
		// "ahead" (EN-42a).
		require.NoError(t, exec.Command("git", "-C", dir, "remote", "rename", "origin", "elsewhere").Run())
		defer func() {
			require.NoError(t, exec.Command("git", "-C", dir, "remote", "rename", "elsewhere", "origin").Run())
		}()
		got := why("ghost", "")
		assert.Equal(t, "not_read", got.Reason)
		assert.Contains(t, got.Detail, "origin/main", "git's own words")
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

	t.Run("a run's changes added to main", func(t *testing.T) {
		// A run's branch on the service, as the runner leaves it.
		bare := org.Repo("plans")
		run := func(id, file, body string) string {
			t.Helper()
			work := t.TempDir()
			for _, args := range [][]string{{"clone", "-q", bare, "."}, {"checkout", "-q", "-b", "run"}} {
				out, err := exec.Command("git", append([]string{"-C", work}, args...)...).CombinedOutput()
				require.NoError(t, err, "%s", out)
			}
			require.NoError(t, os.WriteFile(filepath.Join(work, file), []byte(body), 0o644))
			branch := "blitz/jobs/digest/" + id
			for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=Blitz job digest", "-c", "user.email=jobs@blitz.invalid", "commit", "-qm", "digest: run " + id},
				{"push", "-q", "origin", "HEAD:refs/heads/" + branch}} {
				out, err := exec.Command("git", append([]string{"-C", work}, args...)...).CombinedOutput()
				require.NoError(t, err, "%s", out)
			}
			return branch
		}
		org.Mu.Lock()
		org.ServiceRuns = map[string][]enterprisetest.ServiceRun{"job-digest": {
			{ID: "r1", Status: "succeeded", Branch: run("r1", "recipe.md", "Soup.\n")},
			{ID: "r2", Status: "succeeded", Branch: run("r2", "recipe.md", "Stew.\n")},
			{ID: "r3", Status: "succeeded"},
		}}
		org.Mu.Unlock()
		take := func(id string) (*pb.ServiceRun, error) {
			got, err := c.TakeServiceRun(ctx, connect.NewRequest(&pb.TakeServiceRunRequest{Workspace: dir, JobId: "job-digest", RunId: id}))
			if err != nil {
				return nil, err
			}
			return got.Msg.Run, nil
		}
		got, err := take("r1")
		require.NoError(t, err)
		assert.True(t, got.Merged)
		out, err := exec.Command("git", "--git-dir", bare, "show", "main:recipe.md").Output()
		require.NoError(t, err)
		assert.Equal(t, "Soup.\n", string(out), "on main")
		_, err = take("r1")
		require.NoError(t, err, "again: it's on main already")

		_, err = take("r2")
		_, info := errorReason(t, err)
		assert.Equal(t, "CONFLICTS", info.Reason, "both changed recipe.md")
		_, err = take("r3")
		_, info = errorReason(t, err)
		assert.Equal(t, "NO_CHANGES", info.Reason)
		_, err = take("r9")
		_, info = errorReason(t, err)
		assert.Equal(t, "UNKNOWN_RUN", info.Reason)
		_, err = c.TakeServiceRun(ctx, connect.NewRequest(&pb.TakeServiceRunRequest{Workspace: t.TempDir(), JobId: "job-digest", RunId: "r1"}))
		_, info = errorReason(t, err)
		assert.Equal(t, "NOT_ORG_WORKSPACE", info.Reason)

		// Main refused by the service: it says so, and the run stays as it was.
		org.Mu.Lock()
		org.ServiceRuns["job-digest"] = append(org.ServiceRuns["job-digest"], enterprisetest.ServiceRun{ID: "r4", Status: "succeeded", Branch: run("r4", "notes.md", "Notes.\n")})
		org.Mu.Unlock()
		hook := filepath.Join(bare, "hooks", "pre-receive")
		require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\nwhile read old new ref; do [ \"$ref\" = refs/heads/main ] && exit 1; done; exit 0\n"), 0o755))
		_, err = take("r4")
		require.NoError(t, os.Remove(hook))
		_, info = errorReason(t, err)
		assert.Equal(t, "PUSH_FAILED", info.Reason)

		_, err = c.TakeServiceRun(ctx, connect.NewRequest(&pb.TakeServiceRunRequest{Workspace: dir, JobId: "job-nope", RunId: "r4"}))
		assert.Error(t, err, "no such job")

		// A folder sync keeps in step fetches main at once.
		sy := &orgsync.Syncer{Dir: dir}
		s.ent.syncs.mu.Lock()
		was := s.ent.syncs.running
		s.ent.syncs.running = map[string]*orgSync{dir: {s: sy}}
		s.ent.syncs.mu.Unlock()
		defer func() {
			s.ent.syncs.mu.Lock()
			s.ent.syncs.running = was
			s.ent.syncs.mu.Unlock()
		}()
		_, err = take("r4")
		require.NoError(t, err, "main as the service has it now")

		// The service out of reach: the clone the merge is made in fails.
		url, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
		require.NoError(t, err)
		require.NoError(t, exec.Command("git", "-C", dir, "remote", "set-url", "origin", "http://127.0.0.1:1/plans.git").Run())
		defer exec.Command("git", "-C", dir, "remote", "set-url", "origin", strings.TrimSpace(string(url))).Run()
		org.Mu.Lock()
		org.ServiceRuns["job-digest"] = append(org.ServiceRuns["job-digest"], enterprisetest.ServiceRun{ID: "r5", Status: "succeeded", Branch: run("r5", "more.md", "More.\n")})
		org.Mu.Unlock()
		// A folder without the organization's remote.
		require.NoError(t, exec.Command("git", "-C", dir, "remote", "rename", "origin", "elsewhere").Run())
		_, err = take("r5")
		require.NoError(t, exec.Command("git", "-C", dir, "remote", "rename", "elsewhere", "origin").Run())
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

		_, err = take("r5")
		_, info = errorReason(t, err)
		assert.Equal(t, "CLONE_FAILED", info.Reason)
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

// Save now refuses while a turn runs, and when the workspace's sync isn't
// running (EN-64).
func TestSaveOrgSyncRefusals(t *testing.T) {
	t.Cleanup(http.DefaultClient.CloseIdleConnections)
	_, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()
	idle := &orgsync.Syncer{Dir: dir}
	busy := &orgsync.Syncer{Dir: dir, Git: func(context.Context, string, ...string) ([]byte, error) { return nil, nil },
		Busy: func() bool { return true }, Poll: time.Hour, Fetch: time.Hour}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { busy.Run(runCtx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	for _, tt := range []struct {
		name   string
		sy     *orgsync.Syncer
		reason string
	}{
		{"not running", idle, "NOT_SYNCED"},
		{"a turn running", busy, "GIT_BUSY"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s.ent.syncs.mu.Lock()
			was := s.ent.syncs.running
			s.ent.syncs.running = map[string]*orgSync{dir: {s: tt.sy}}
			s.ent.syncs.mu.Unlock()
			defer func() {
				s.ent.syncs.mu.Lock()
				s.ent.syncs.running = was
				s.ent.syncs.mu.Unlock()
			}()
			require.Eventually(t, func() bool {
				_, err := c.SaveOrgSync(ctx, connect.NewRequest(&pb.SaveOrgSyncRequest{Workspace: dir}))
				_, info := errorReason(t, err)
				return info.Reason == tt.reason
			}, 5*time.Second, 20*time.Millisecond)
		})
	}

	// The service away: the state says so, with Git's words.
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"remote", "add", "origin", filepath.Join(t.TempDir(), "gone.git")}} {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	away := &orgsync.Syncer{Dir: repo, Who: "Ana", Poll: time.Hour, Fetch: time.Hour, Quiet: time.Hour,
		Git: func(ctx context.Context, dir string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "user.name=Ana", "-c", "user.email=ana@acme.com"}, args...)...).Output()
		}}
	go func() { away.Run(runCtx) }()
	s.ent.syncs.mu.Lock()
	was := s.ent.syncs.running
	s.ent.syncs.running = map[string]*orgSync{repo: {s: away}}
	s.ent.syncs.mu.Unlock()
	t.Cleanup(func() {
		s.ent.syncs.mu.Lock()
		s.ent.syncs.running = was
		s.ent.syncs.mu.Unlock()
	})
	require.NoError(t, os.WriteFile(filepath.Join(repo, "plan.md"), []byte("Plan."), 0o644))
	var saved *connect.Response[pb.SaveOrgSyncResponse]
	require.Eventually(t, func() bool {
		var err error
		saved, err = c.SaveOrgSync(ctx, connect.NewRequest(&pb.SaveOrgSyncRequest{Workspace: repo}))
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, "offline", saved.Msg.State)
	assert.True(t, saved.Msg.Failed, "the service says it hasn't got the saves")
	assert.NotEmpty(t, saved.Msg.Error, "Git's words")
	st, err := c.GetOrgSync(ctx, connect.NewRequest(&pb.GetOrgSyncRequest{Workspace: repo}))
	require.NoError(t, err)
	assert.Equal(t, "offline", st.Msg.State)
	assert.True(t, st.Msg.Failed)

	_, err = c.SaveOrgSync(ctx, connect.NewRequest(&pb.SaveOrgSyncRequest{Workspace: "relative/nowhere\x00"}))
	_, info := errorReason(t, err)
	assert.Equal(t, "NOT_SYNCED", info.Reason)
}

// A shared folder is the same folder however it's spelled, links followed
// (macOS's /var is /private/var), as the folder's sync finds it (EN-66a).
func TestSamePath(t *testing.T) {
	dir := t.TempDir()
	real, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(real, link))
	cases := []struct {
		name string
		a, b string
		same bool
	}{
		{"as given", dir, dir, true},
		{"through a link", link, real, true},
		{"unclean", dir + "/./", dir, true},
		{"another", dir, t.TempDir(), false},
		{"gone, compared as given", "/no/such/dir", "/no/such/dir/", true},
		{"gone and another", "/no/such/dir", "/no/such/other", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.same, samePath(tc.a, tc.b))
		})
	}
}

// gitOut reads a refused sign-in from git's own words, in English
// whatever the computer's language, and not from any "401" in a message
// (EN-64).
func TestGitOutSignedOut(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="blitz"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(refusing.Close)
	t.Setenv("LANGUAGE", "de")
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	s := &Server{}
	s.ent.gitHelper = `!f() { echo username=blitz; echo password=wrong; }; f`
	repo := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", repo, "init", "-q").Run())
	cases := []struct {
		name      string
		args      []string
		signedOut bool
	}{
		{"refused by the service", []string{"ls-remote", refusing.URL + "/x.git"}, true},
		{"a 401 in another failure", []string{"rev-parse", "--verify", "x401"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.gitOut(context.Background(), repo, tc.args...)
			require.Error(t, err)
			assert.Equal(t, tc.signedOut, errors.Is(err, orgsync.ErrSignedOut), "%v", err)
		})
	}
}
