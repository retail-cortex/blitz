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

package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// A client accounts for the background processes its own turns started in
// the service, and no one else's.
func TestRemoteBackgroundProcesses(t *testing.T) {
	start := call("run_shell_command", map[string]any{"command": "sleep 30", "background": true})
	r := attach(t, configtest.RunTools, start, text("started"))
	pm := r.Processes()
	require.NotNil(t, pm)
	assert.Empty(t, pm.Running(), "no turn yet")

	sess, _, err := r.OpenSession("", false)
	require.NoError(t, err)
	_, err = r.Run(context.Background(), sess.ID, api.Turn{Text: "start it"}, func(api.Event) {})
	require.NoError(t, err)
	running := pm.Running()
	require.Len(t, running, 1)
	assert.Equal(t, "sleep 30", running[0].Command)

	// Another client of the same workspace, which ran no turn there.
	other := &Remote{dir: r.dir, sessions: r.sessions, workspaces: r.workspaces, warn: func(string) {}}
	assert.Empty(t, other.Processes().Running(), "another client's process listed")
	other.ranIn("session-20260101-000000-00000000")
	_, err = other.workspaces.KillProcess(context.Background(), killRequest(other, running[0].ID))
	assert.ErrorIs(t, fromAPI(err), api.ErrUnknownProcess, "another client's process killed: %v", err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, pm.WaitAll(ctx), context.Canceled, "waiting stops with the context")

	pm.Shutdown()
	assert.Empty(t, pm.Running(), "still running after Shutdown")
	assert.NoError(t, pm.WaitAll(context.Background()))
}

// !cmd run by an attached CLI lands in the service workspace's audit log.
func TestRemoteAuditShell(t *testing.T) {
	dir := t.TempDir()
	r := attach(t, func(c *config.Config) { c.Audit.Enabled, c.Audit.Dir = true, dir })
	r.AuditShell("ls -la", 0, nil)
	r.AuditShell("nosuchcmd", -1, errors.New("not found"))

	files, err := filepath.Glob(filepath.Join(dir, "audit-*.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	data, err := os.ReadFile(files[0])
	require.NoError(t, err)
	var shell []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"user_shell"`) {
			shell = append(shell, line)
		}
	}
	require.Len(t, shell, 2, "audit log:\n%s", data)
	assert.Contains(t, shell[0], "ls -la")
	assert.Contains(t, shell[1], "not found")
}

func killRequest(r *Remote, id int) *connect.Request[pb.KillProcessRequest] {
	return connect.NewRequest(&pb.KillProcessRequest{Workspace: r.dir, SessionIds: r.sessionsRan(), Id: int32(id)})
}

// A client sees and stops the background tasks its own turns started in
// the service, and no one else's; an ended task is in the transcript.
func TestRemoteBackgroundTasks(t *testing.T) {
	start := call("invoke_agent", map[string]any{"agent_name": "qa", "prompt": "review the cart", "background": true})
	r := attach(t, func(c *config.Config) {
		configtest.RunTools(c)
		c.AgentModels = map[string]string{"qa": "gemini/qa-model"} // answers "Done." at once
	}, start, text("started"))
	assert.Empty(t, r.ListTasks(), "no turn yet")

	sess, _, err := r.OpenSession("", false)
	require.NoError(t, err)
	_, err = r.Run(context.Background(), sess.ID, api.Turn{Text: "review in the background"}, func(api.Event) {})
	require.NoError(t, err)
	tasks := r.ListTasks()
	require.Len(t, tasks, 1)
	assert.Equal(t, "qa", tasks[0].Agent)

	var got api.TaskInfo
	require.Eventually(t, func() bool {
		got, _, err = r.Task(tasks[0].ID)
		return err == nil && got.State == api.TaskDone
	}, 10*time.Second, 50*time.Millisecond, "the task didn't finish: %+v %v", got, err)
	assert.Equal(t, "Done.", got.Result)

	other := &Remote{dir: r.dir, sessions: r.sessions, workspaces: r.workspaces, warn: func(string) {}}
	assert.Empty(t, other.ListTasks(), "another client's task listed")
	_, _, err = other.Task(got.ID)
	assert.ErrorIs(t, err, api.ErrUnknownTask)
	_, err = other.StopTask(got.ID)
	assert.ErrorIs(t, err, api.ErrUnknownTask)

	info, _, err := r.LoadSession(sess.ID)
	require.NoError(t, err)
	i := slices.IndexFunc(info.Messages, func(m api.Message) bool { return m.Kind == "task" })
	require.GreaterOrEqual(t, i, 0, "no task in the transcript: %+v", info.Messages)
	assert.Contains(t, info.Messages[i].Text, got.ID+" (qa) finished")
}

// A background task's approval request reaches its client through
// WatchTasks and ListTaskRequests, and the ordinary Approve answers it.
func TestRemoteTaskRequests(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	start := call("invoke_agent", map[string]any{"agent_name": "qa", "prompt": "make a file", "background": true})
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	s := servicetest.New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		cfg.AgentModels = map[string]string{"qa": "gemini/qa-model"}
		return engine.Open(ctx, cfg, engine.Options{
			Model: runtime.NewMockLLM("gemini-3.8-flash", start, text("started")),
			NewModel: func(_ context.Context, _ *config.Config, ref string) (model.LLM, error) {
				return runtime.NewMockLLM("qa-model", create, text("made it")), nil
			},
		})
	})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); s.Close() })
	r, err := AttachHTTP(context.Background(), http.DefaultClient, srv.URL, t.TempDir(), nil)
	require.NoError(t, err)
	sess, _, err := r.OpenSession("", false)
	require.NoError(t, err)

	watchCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	stream, err := r.workspaces.WatchTasks(watchCtx, connect.NewRequest(&pb.WatchTasksRequest{Workspace: r.dir, SessionIds: []string{sess.ID}}))
	require.NoError(t, err)
	defer stream.Close()
	require.True(t, stream.Receive(), "no ready: %v", stream.Err())
	require.True(t, stream.Msg().Event.GetReady())

	_, err = r.Run(context.Background(), sess.ID, api.Turn{Text: "go"}, func(api.Event) {})
	require.NoError(t, err)
	var asked *pb.ApprovalRequest
	for asked == nil && stream.Receive() {
		if a := stream.Msg().Event.GetApprovalRequest(); a != nil {
			asked = a
		}
	}
	require.NotNil(t, asked, "no request streamed: %v", stream.Err())
	assert.Equal(t, "qa", asked.Agent)
	assert.Equal(t, "task-1", asked.TaskId)
	pending := r.PendingTaskRequests()
	require.Len(t, pending, 1)
	assert.Equal(t, asked.RequestId, pending[0].ID)

	require.NoError(t, r.AnswerTaskRequest(asked.RequestId, api.DecisionOnce, ""))
	require.Eventually(t, func() bool {
		got, _, err := r.Task("task-1")
		return err == nil && got.State == api.TaskDone
	}, 10*time.Second, 50*time.Millisecond)
	assert.Empty(t, r.PendingTaskRequests())
	assert.ErrorIs(t, r.AnswerTaskRequest(asked.RequestId, api.DecisionOnce, ""), api.ErrUnknownRequest, "answered twice")
}

// Attached, /cd moves the session to the target workspace in the service;
// the old one stays open there for other clients (PAR-SES-43).
func TestRemoteMoveSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	sessions := t.TempDir() // one store for every workspace, as ~/.blitz/sessions is
	s := servicetest.New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = sessions
		return engine.Open(ctx, cfg, engine.Options{Model: runtime.NewMockLLM("gemini-3.8-flash")})
	})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); s.Close() })
	a, err := AttachHTTP(context.Background(), http.DefaultClient, srv.URL, t.TempDir(), nil)
	require.NoError(t, err)
	sess, _, err := a.OpenSession("", false)
	require.NoError(t, err)
	_, err = a.Run(context.Background(), sess.ID, api.Turn{Text: "hello"}, func(api.Event) {})
	require.NoError(t, err)

	b := &Remote{dir: t.TempDir(), sessions: a.sessions, workspaces: a.workspaces, warn: func(string) {}}
	moved, err := b.MoveSession(sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, moved.ID)
	real, _ := filepath.EvalSymlinks(a.dir)
	assert.Equal(t, real, moved.MovedFrom)
	assert.Equal(t, 2, moved.MovedAt)

	here, err := b.ListSessions(false)
	require.NoError(t, err)
	assert.True(t, slices.ContainsFunc(here, func(s api.SessionInfo) bool { return s.ID == sess.ID }), "not listed in the new workspace")
	there, err := a.ListSessions(false)
	require.NoError(t, err)
	assert.False(t, slices.ContainsFunc(there, func(s api.SessionInfo) bool { return s.ID == sess.ID }), "still listed in the old one")
	_, err = a.Run(context.Background(), sess.ID, api.Turn{Text: "the old workspace still answers"}, func(api.Event) {})
	assert.NoError(t, err, "the old workspace was closed in the service")
}
