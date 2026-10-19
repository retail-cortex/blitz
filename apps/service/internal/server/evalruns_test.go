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
	"iter"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// hangingLLM answers only with its context's error: a judge that never
// decides, so an eval run stays going on until it's stopped.
type hangingLLM struct{}

func (hangingLLM) Name() string { return "hanging" }

func (hangingLLM) GenerateContent(ctx context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		<-ctx.Done()
		yield(nil, ctx.Err())
	}
}

// serveHangingJudge serves workspaces whose agent answers and whose
// judge (Near cases) never does.
func serveHangingJudge(t *testing.T) (pb.WorkspaceServiceClient, *Server, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	s := New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{
			Model:    runtime.NewMockLLM("gemini-3.8-flash", genai.NewContentFromText("Paris", genai.RoleModel), genai.NewContentFromText("Paris", genai.RoleModel), genai.NewContentFromText("Paris", genai.RoleModel)),
			NewModel: func(context.Context, *config.Config, string) (model.LLM, error) { return hangingLLM{}, nil },
		})
	})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() { srv.Close(); s.Close() })
	w := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	saved, err := w.SaveEval(context.Background(), connect.NewRequest(&pb.SaveEvalRequest{Workspace: dir, Eval: &pb.EvalSpec{Name: "capitals", Cases: []*pb.EvalCase{{Id: "fr", Prompt: "France's capital?", Output: "Paris", Type: "near"}}}}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	return w, s, dir
}

// startRun starts the eval, reads its started event, and leaves: the
// stream ends as the page does when it's closed.
func startRun(t *testing.T, w pb.WorkspaceServiceClient, dir string, background bool) string {
	t.Helper()
	ctx, leave := context.WithCancel(context.Background())
	defer leave()
	stream, err := w.RunEval(ctx, connect.NewRequest(&pb.RunEvalRequest{Workspace: dir, Name: "capitals", Background: background}))
	require.NoError(t, err)
	require.True(t, stream.Receive(), "started: %v", stream.Err())
	started := stream.Msg().GetStarted()
	require.NotNil(t, started)
	assert.Equal(t, "running", started.Status)
	return started.Id
}

// latest is the eval's newest run, as ListEvalRuns has it.
func latest(t *testing.T, w pb.WorkspaceServiceClient, dir string) *pb.EvalRun {
	t.Helper()
	runs, err := w.ListEvalRuns(context.Background(), connect.NewRequest(&pb.ListEvalRunsRequest{Workspace: dir, Name: "capitals"}))
	require.NoError(t, err)
	require.NotEmpty(t, runs.Msg.Runs)
	return runs.Msg.Runs[0]
}

// A background run goes on when its stream ends: it's listed as running,
// followed again, stopped (and recorded as stopped); closing the service
// stops the rest. A run started without background stops with its
// stream, as blitz eval's does on Ctrl+C.
func TestEvalRunsGoOnInTheBackground(t *testing.T) {
	w, s, dir := serveHangingJudge(t)
	ctx := context.Background()

	id := startRun(t, w, dir, true)
	time.Sleep(50 * time.Millisecond) // the stream has gone
	run := latest(t, w, dir)
	assert.Equal(t, id, run.Id)
	assert.Equal(t, "running", run.Status, "it went on after its stream ended")
	list, err := w.ListEvals(ctx, connect.NewRequest(&pb.ListEvalsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.NotNil(t, list.Msg.Evals[0].LastRun)
	assert.Equal(t, "running", list.Msg.Evals[0].LastRun.Status, "the list marks it")

	followed := make(chan []string, 1)
	watching := make(chan struct{})
	go func() {
		stream, err := w.WatchEvalRun(ctx, connect.NewRequest(&pb.WatchEvalRunRequest{Workspace: dir, Id: id}))
		if !assert.NoError(t, err) {
			close(watching)
			followed <- nil
			return
		}
		var kinds []string
		for stream.Receive() {
			switch ev := stream.Msg().Event.(type) {
			case *pb.RunEvalResponse_Started:
				kinds = append(kinds, "started")
				close(watching)
			case *pb.RunEvalResponse_Finished:
				kinds = append(kinds, "finished:"+ev.Finished.Status)
			}
		}
		followed <- kinds
	}()
	<-watching
	_, err = w.StopEvalRun(ctx, connect.NewRequest(&pb.StopEvalRunRequest{Workspace: dir, Id: id}))
	require.NoError(t, err)
	assert.Equal(t, []string{"started", "finished:stopped"}, <-followed)
	assert.Equal(t, "stopped", latest(t, w, dir).Status, "recorded as stopped")

	assert.Equal(t, "EVAL_RUN_NOT_FOUND", reason(t, unary(w.StopEvalRun, &pb.StopEvalRunRequest{Workspace: dir, Id: id})), "no longer going on")
	ended, err := w.WatchEvalRun(ctx, connect.NewRequest(&pb.WatchEvalRunRequest{Workspace: dir, Id: id}))
	require.NoError(t, err)
	require.True(t, ended.Receive())
	assert.Equal(t, "stopped", ended.Msg().GetFinished().GetStatus(), "an ended run: finished alone")
	assert.False(t, ended.Receive())
	unknown, err := w.WatchEvalRun(ctx, connect.NewRequest(&pb.WatchEvalRunRequest{Workspace: dir, Id: "nope"}))
	require.NoError(t, err)
	assert.False(t, unknown.Receive())
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(unknown.Err()))

	// Without background, leaving stops it.
	id = startRun(t, w, dir, false)
	require.Eventually(t, func() bool { r := latest(t, w, dir); return r.Id == id && r.Status == "stopped" }, 5*time.Second, 20*time.Millisecond)

	// Closing the service stops what's going on, and nothing starts after.
	startRun(t, w, dir, true)
	require.NoError(t, s.Close())
	assert.Empty(t, s.liveEvals(dir, "capitals"))
	stream, err := w.RunEval(ctx, connect.NewRequest(&pb.RunEvalRequest{Workspace: dir, Name: "capitals", Background: true}))
	require.NoError(t, err)
	assert.False(t, stream.Receive())
	assert.Error(t, stream.Err(), "started after closing")
}

func TestWithLiveEvals(t *testing.T) {
	run := func(id, status string) api.EvalRun { return api.EvalRun{ID: id, Status: status} }
	for _, tc := range []struct {
		name           string
		live, recorded []api.EvalRun
		want           []api.EvalRun
	}{
		{"none going on", nil, []api.EvalRun{run("b", "passed")}, []api.EvalRun{run("b", "passed")}},
		{"one going on, first", []api.EvalRun{run("c", "running")}, []api.EvalRun{run("b", "passed")}, []api.EvalRun{run("c", "running"), run("b", "passed")}},
		{"recorded as it ends: listed once", []api.EvalRun{run("c", "running")}, []api.EvalRun{run("c", "failed"), run("b", "passed")}, []api.EvalRun{run("c", "running"), run("b", "passed")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, withLiveEvals(tc.live, tc.recorded))
		})
	}
}
