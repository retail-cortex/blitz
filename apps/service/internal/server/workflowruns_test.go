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
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A workflow run in the background is listed while it runs, with the
// background run following it; its receipt streams as it changes; it's
// stopped through its receipt; it's then listed as stopped, with its
// steps, and its whole receipt has its events.
func TestWorkflowRunsOverTheAPI(t *testing.T) {
	watchTasksEvery, watchRunEvery = 20*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { watchTasksEvery, watchRunEvery = time.Second, 300*time.Millisecond })
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-hold:
		}
	}))
	defer srv.Close()
	defer close(hold)
	c, _ := serve(t, configtest.RunTools)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFlowFile(t, dir, "slow", "nodes:\n  - {id: copy, type: accumulate}\n  - {id: call, type: api_call, url: \""+srv.URL+"/wait\"}\nedges: [{from: copy, to: call}]\n")

	runs, err := c.workspaces.WatchWorkflowRuns(ctx, connect.NewRequest(&pb.WatchWorkflowRunsRequest{}))
	require.NoError(t, err)
	defer runs.Close()
	require.True(t, runs.Receive(), "%v", runs.Err())
	assert.Empty(t, runs.Msg().Runs)

	started, err := c.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: dir, Turn: &pb.Turn{Text: "/slow now", Command: true}}))
	require.NoError(t, err)
	var run *pb.WorkflowRun
	for run == nil || len(run.Steps) < 2 {
		require.True(t, runs.Receive(), "%v", runs.Err())
		if len(runs.Msg().Runs) > 0 {
			run = runs.Msg().Runs[0]
		}
	}
	assert.Equal(t, "running", run.Status)
	assert.Equal(t, "slow", run.Workflow)
	assert.Equal(t, dir, run.Workspace)
	assert.Equal(t, "now", run.Input)
	assert.Equal(t, started.Msg.Run.Id, run.BackgroundId, "the background run following it")
	assert.Empty(t, run.Events, "a summary")
	assert.Equal(t, "running", run.Steps[1].Status)

	bg, err := c.sessions.ListBackground(ctx, connect.NewRequest(&pb.ListBackgroundRequest{}))
	require.NoError(t, err)
	require.NotEmpty(t, bg.Msg.Runs)
	assert.Equal(t, run.Id, bg.Msg.Runs[len(bg.Msg.Runs)-1].WorkflowRun)

	watch, err := c.workspaces.WatchWorkflowRun(ctx, connect.NewRequest(&pb.WatchWorkflowRunRequest{Workspace: dir, Id: run.Id}))
	require.NoError(t, err)
	defer watch.Close()
	require.True(t, watch.Receive(), "%v", watch.Err())
	assert.Equal(t, "running", watch.Msg().Run.Status)
	assert.NotEmpty(t, watch.Msg().Run.Events, "the whole receipt")

	_, err = c.workspaces.StopWorkflowRun(ctx, connect.NewRequest(&pb.StopWorkflowRunRequest{Workspace: dir, Id: run.Id}))
	require.NoError(t, err)
	var last *pb.WorkflowRun
	for watch.Receive() {
		last = watch.Msg().Run
	}
	require.NoError(t, watch.Err())
	require.NotNil(t, last)
	assert.Equal(t, "stopped", last.Status, "the watch ends with the run")
	assert.NotNil(t, last.Ended)
	assert.Equal(t, "failed", last.Steps[1].Status)

	_, err = c.workspaces.StopWorkflowRun(ctx, connect.NewRequest(&pb.StopWorkflowRunRequest{Workspace: dir, Id: run.Id}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "it isn't running")
	for {
		require.True(t, runs.Receive(), "%v", runs.Err())
		if m := runs.Msg().Runs; len(m) == 1 && m[0].Status == "stopped" {
			assert.Empty(t, m[0].BackgroundId)
			break
		}
	}

	list, err := c.workspaces.ListWorkflowRuns(ctx, connect.NewRequest(&pb.ListWorkflowRunsRequest{Workspace: dir, Workflow: "slow"}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Runs, 1)
	assert.Len(t, list.Msg.Runs[0].Steps, 2)
	assert.Empty(t, list.Msg.Runs[0].Events)
	none, err := c.workspaces.ListWorkflowRuns(ctx, connect.NewRequest(&pb.ListWorkflowRunsRequest{Workflow: "other", Limit: 1}))
	require.NoError(t, err)
	assert.Empty(t, none.Msg.Runs)

	got, err := c.workspaces.GetWorkflowRun(ctx, connect.NewRequest(&pb.GetWorkflowRunRequest{Workspace: dir, Id: run.Id}))
	require.NoError(t, err)
	assert.Equal(t, "stopped", got.Msg.Run.Status)
	assert.NotEmpty(t, got.Msg.Run.Events)
	assert.NotNil(t, got.Msg.Run.Usage)
	assert.NotNil(t, got.Msg.Run.Steps[0].Started)

	for _, tc := range []struct{ name, workspace, id string }{
		{name: "unknown", workspace: dir, id: "flow-nothing"},
		{name: "another workspace's", workspace: "/elsewhere", id: run.Id},
		{name: "a path", workspace: dir, id: "../x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.workspaces.GetWorkflowRun(ctx, connect.NewRequest(&pb.GetWorkflowRunRequest{Workspace: tc.workspace, Id: tc.id}))
			assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
			_, err = c.workspaces.StopWorkflowRun(ctx, connect.NewRequest(&pb.StopWorkflowRunRequest{Workspace: tc.workspace, Id: tc.id}))
			assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
			w, err := c.workspaces.WatchWorkflowRun(ctx, connect.NewRequest(&pb.WatchWorkflowRunRequest{Workspace: tc.workspace, Id: tc.id}))
			require.NoError(t, err)
			assert.False(t, w.Receive())
			assert.Equal(t, connect.CodeNotFound, connect.CodeOf(w.Err()))
			w.Close()
		})
	}
}

// A background run's agent step that asks for approval: its receipt names
// the background run and counts the request, which a watcher of that run
// sees and answers, and the run goes on.
func TestWorkflowRunApproval(t *testing.T) {
	watchTasksEvery, watchRunEvery = 20*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { watchTasksEvery, watchRunEvery = time.Second, 300*time.Millisecond })
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	c, _ := serve(t, func(cfg *config.Config) { cfg.Blitz.AutoApprove = false }, create, text("made it"))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFlowFile(t, dir, "make", "nodes:\n  - {id: a, type: agent, instructions: Make the file.}\n")

	started, err := c.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: dir, Turn: &pb.Turn{Text: "/make", Command: true}}))
	require.NoError(t, err)
	runs, err := c.workspaces.WatchWorkflowRuns(ctx, connect.NewRequest(&pb.WatchWorkflowRunsRequest{Workspace: dir}))
	require.NoError(t, err)
	defer runs.Close()
	var run *pb.WorkflowRun
	for run == nil || run.Approvals == 0 {
		require.True(t, runs.Receive(), "%v", runs.Err())
		if len(runs.Msg().Runs) > 0 {
			run = runs.Msg().Runs[0]
		}
	}
	assert.Equal(t, started.Msg.Run.Id, run.BackgroundId)

	var request string
	for _, ev := range watchAll(t, c, run.BackgroundId, false) {
		if ar := ev.GetApprovalRequest(); ar != nil {
			request = ar.RequestId
		}
	}
	require.NotEmpty(t, request, "the waiting request isn't shown")
	_, err = c.sessions.Approve(ctx, connect.NewRequest(&pb.ApproveRequest{Workspace: dir, RequestId: request, Decision: pb.Decision_DECISION_ONCE}))
	require.NoError(t, err)
	for run.Status == "running" {
		require.True(t, runs.Receive(), "%v", runs.Err())
		run = runs.Msg().Runs[0]
	}
	assert.Equal(t, "done", run.Status)
}
