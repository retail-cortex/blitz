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
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// serveWorkers starts a server that runs workers, on mock models answering
// with replies, and returns a worker client for it.
func serveWorkers(t *testing.T, rescan time.Duration, replies ...*genai.Content) (pb.WorkerServiceClient, *Server) {
	t.Helper()
	return serveScheduler(t, rescan, true, replies...)
}

// serveScheduler is serveWorkers, starting the scheduler only with start
// (a test that drives it by hand doesn't want its rescans).
func serveScheduler(t *testing.T, rescan time.Duration, start bool, replies ...*genai.Content) (pb.WorkerServiceClient, *Server) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	store, err := workers.OpenStore(filepath.Join(t.TempDir(), "workers.json"))
	require.NoError(t, err)
	s := New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{Model: runtime.NewMockLLM("m", replies...), Workers: store})
	}, WithScheduler(SchedulerConfig{Store: store, Runs: workers.OpenRunLog(filepath.Join(os.Getenv("HOME"), ".blitz", "worker-runs")), MaxConcurrent: 2, Rescan: rescan}))
	srv := httptest.NewServer(s.Handler())
	ctx, cancel := context.WithCancel(context.Background())
	if start {
		s.StartScheduler(ctx)
	}
	t.Cleanup(func() { cancel(); srv.Close(); s.Close() })
	return pb.NewWorkerServiceClient(http.DefaultClient, srv.URL), s
}

func addWorker(t *testing.T, dir, name, content string) {
	t.Helper()
	wd := filepath.Join(dir, "workers", name)
	os.MkdirAll(wd, 0o755)
	require.NoError(t, os.WriteFile(filepath.Join(wd, workers.FileName), []byte(content), 0o644))
}

func TestWorkersOverTheAPI(t *testing.T) {
	c, _ := serveWorkers(t, time.Hour, text("Report written."))
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	addWorker(t, dir, "deps", "---\nschedule: Daily at 6 AM\n---\nWrite the report.\n")

	list, err := c.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{Workspace: dir}))
	require.NoError(t, err, "list %v", list)
	require.Len(t, list.Msg.Workers, 1, "list %v %v", list, err)
	wk := list.Msg.Workers[0]
	assert.Equal(t, pb.WorkerState_WORKER_STATE_NEW, wk.State, "worker %v", wk)
	assert.Equal(t, "0 6 * * *", wk.Cron, "worker %v", wk)
	assert.NotEqual(t, int32(0), wk.Limits.GetMaxTurns(), "worker %v", wk)
	_, err = c.RunWorker(ctx, connect.NewRequest(&pb.RunWorkerRequest{Workspace: dir, Name: "deps"}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, code, "run before enabling: %v %v", code, info)
	assert.Equal(t, "WORKER_DISABLED", info.Reason, "run before enabling: %v %v", code, info)
	_, err = c.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: dir, Name: "deps", Hash: "sha256:stale"}))
	code, info = errorReason(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, code, "stale hash: %v %v", code, info)
	assert.Equal(t, "HASH_MISMATCH", info.Reason, "stale hash: %v %v", code, info)
	_, err = c.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: dir, Name: "nope", Hash: "x"}))
	code, info = errorReason(t, err)
	assert.Equal(t, connect.CodeNotFound, code, "unknown worker: %v %v", code, info)
	assert.Equal(t, "UNKNOWN_WORKER", info.Reason, "unknown worker: %v %v", code, info)
	on, err := c.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: dir, Name: "deps", Hash: wk.Hash}))
	require.NoError(t, err, "enable %v", on)
	require.Equal(t, pb.WorkerState_WORKER_STATE_ENABLED, on.Msg.Worker.State, "enable %v %v", on, err)
	require.NotNil(t, on.Msg.Worker.NextRun, "enable %v %v", on, err)
	all, _ := c.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{}))
	assert.Len(t, all.Msg.Workers, 1, "every registered workspace: %v", all.Msg.Workers)

	started, err := c.RunWorker(ctx, connect.NewRequest(&pb.RunWorkerRequest{Workspace: dir, Name: "deps"}))
	require.NoError(t, err, "run %v", started)
	require.Equal(t, pb.RunStatus_RUN_STATUS_RUNNING, started.Msg.Run.Status, "run %v %v", started, err)
	require.True(t, started.Msg.Run.Manual, "run %v %v", started, err)
	id := started.Msg.Run.Id
	// Watching may begin after the run has; the events come from its start.
	var text string
	var finished *pb.TurnFinished
	if stream, err := c.WatchWorkerRun(ctx, connect.NewRequest(&pb.WatchWorkerRunRequest{RunId: id})); err == nil {
		for stream.Receive() {
			ev := stream.Msg().Event
			if t := ev.GetText(); t != nil {
				text += t.Text
			}
			if f := ev.GetFinished(); f != nil {
				finished = f
			}
		}
	}
	// A run too quick to watch is already recorded; either way it succeeded.
	assert.False(t, finished != nil && (finished.Error != nil || text != "Report written."), "watched %q, finished %v", text, finished)
	var run *pb.WorkerRun
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if got, err := c.GetWorkerRun(ctx, connect.NewRequest(&pb.GetWorkerRunRequest{RunId: id})); err == nil && got.Msg.Run.Status != pb.RunStatus_RUN_STATUS_RUNNING {
			run = got.Msg.Run
			break
		}
	}
	require.NotNil(t, run, "run record %v", run)
	require.Equal(t, pb.RunStatus_RUN_STATUS_SUCCEEDED, run.Status, "run record %v", run)
	require.NotEqual(t, "", run.SessionId, "run record %v", run)
	runs, err := c.ListWorkerRuns(ctx, connect.NewRequest(&pb.ListWorkerRunsRequest{Workspace: dir, Name: "deps"}))
	assert.NoError(t, err, "runs %v", runs)
	assert.Len(t, runs.Msg.Runs, 1, "runs %v %v", runs, err)
	assert.Equal(t, id, runs.Msg.Runs[0].Id, "runs %v %v", runs, err)
	_, err = c.GetWorkerRun(ctx, connect.NewRequest(&pb.GetWorkerRunRequest{RunId: "nope"}))
	code, info = errorReason(t, err)
	assert.Equal(t, connect.CodeNotFound, code, "unknown run: %v %v", code, info)
	assert.Equal(t, "UNKNOWN_RUN", info.Reason, "unknown run: %v %v", code, info)
	off, err := c.DisableWorker(ctx, connect.NewRequest(&pb.DisableWorkerRequest{Workspace: dir, Name: "deps"}))
	assert.NoError(t, err, "disable %v", off)
	assert.Equal(t, pb.WorkerState_WORKER_STATE_DISABLED, off.Msg.Worker.State, "disable %v %v", off, err)
}

// An enabled worker runs on its schedule without anyone asking.
func TestWorkersRunOnSchedule(t *testing.T) {
	c, s := serveWorkers(t, 50*time.Millisecond, text("tick"), text("tick"), text("tick"), text("tick"))
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	addWorker(t, dir, "ticker", "---\nschedule: \"@every 1s\"\n---\nSay tick.\n")
	list, err := c.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{Workspace: dir}))
	require.NoError(t, err)
	_, err = c.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: dir, Name: "ticker", Hash: list.Msg.Workers[0].Hash}))
	require.NoError(t, err)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		runs, err := c.ListWorkerRuns(ctx, connect.NewRequest(&pb.ListWorkerRunsRequest{Workspace: dir, Name: "ticker"}))
		if err == nil {
			for _, r := range runs.Msg.Runs {
				if r.Status == pb.RunStatus_RUN_STATUS_SUCCEEDED && !r.Manual {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			s.sched.mu.Lock()
			n := len(s.sched.entries)
			s.sched.mu.Unlock()
			t.Fatalf("no scheduled run happened; %d workers scheduled", n)
		}
	}
}

// A worker behind schedule waits for a slot once, not once per missed
// tick, and stopping the scheduler releases the wait.
func TestSchedulerWaitsOncePerWorkerAndStops(t *testing.T) {
	s := New(nil, WithScheduler(SchedulerConfig{MaxConcurrent: 1}))
	sc := s.sched
	sc.slots <- struct{}{} // every slot busy

	waited := make(chan error, 1)
	go func() { waited <- sc.acquire("/w", "deps", false) }()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		sc.mu.Lock()
		w := sc.waiting[key("/w", "deps")]
		sc.mu.Unlock()
		if w {
			break
		}
		require.False(t, time.Now().After(deadline), "the scheduled run never waited")
	}
	err := sc.acquire("/w", "deps", false)
	assert.ErrorIs(t, err, api.ErrRunInProgress, "second wait for the same worker: %v", err)
	code, info := errorReason(t, sc.acquire("/w", "other", true))
	assert.Equal(t, connect.CodeResourceExhausted, code, "manual run with no slot: %v %v", code, info)
	assert.Equal(t, "TOO_MANY_RUNS", info.Reason, "manual run with no slot: %v %v", code, info)

	s.Close()
	err = <-waited
	assert.ErrorIs(t, err, errStopped, "waiting run after Close: %v", err)
	assert.False(t, sc.track(), "a stopped scheduler started a run")
}

// A worker made from a form over the API: created when valid, its
// problems when not, WORKER_EXISTS for a name taken.
func TestCreateWorkerOverTheAPI(t *testing.T) {
	c, _ := serveWorkers(t, time.Hour)
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	req := &pb.CreateWorkerRequest{Workspace: dir, Name: "deps", Schedule: "Daily at 6 AM", MaxTurns: 10, Timeout: "20m", Prompt: "Write the report."}

	res, err := c.CreateWorker(ctx, connect.NewRequest(req))
	require.NoError(t, err)
	require.Empty(t, res.Msg.Problems)
	assert.Equal(t, filepath.Join(dir, ".agents", "workers", "deps", "WORKER.md"), res.Msg.Worker.GetPath())
	assert.Equal(t, pb.WorkerState_WORKER_STATE_NEW, res.Msg.Worker.GetState())
	assert.Equal(t, int32(10), res.Msg.Worker.GetLimits().GetMaxTurns())

	_, err = c.CreateWorker(ctx, connect.NewRequest(req))
	code, _ := errorReason(t, err)
	assert.Equal(t, connect.CodeAlreadyExists, code)

	bad, err := c.CreateWorker(ctx, connect.NewRequest(&pb.CreateWorkerRequest{Workspace: dir, Name: "x", Schedule: "whenever", Prompt: "p"}))
	require.NoError(t, err)
	assert.Nil(t, bad.Msg.Worker)
	assert.NotEmpty(t, bad.Msg.Problems)
}

// A worker edited over the API: read as written with its hash, saved at
// that hash (suspending it if enabled), refused at a stale one.
func TestUpdateWorkerOverTheAPI(t *testing.T) {
	c, _ := serveWorkers(t, time.Hour)
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	created, err := c.CreateWorker(ctx, connect.NewRequest(&pb.CreateWorkerRequest{Workspace: dir, Name: "deps", Schedule: "Daily at 6 AM", Timeout: "20m", Prompt: "Write the report."}))
	require.NoError(t, err)
	_, err = c.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: dir, Name: "deps", Hash: created.Msg.Worker.Hash}))
	require.NoError(t, err)

	got, err := c.GetWorkerSpec(ctx, connect.NewRequest(&pb.GetWorkerSpecRequest{Workspace: dir, Name: "deps"}))
	require.NoError(t, err)
	assert.Equal(t, created.Msg.Worker.Hash, got.Msg.Hash)
	assert.Equal(t, "20m", got.Msg.Worker.Timeout)
	def := got.Msg.Worker
	def.Prompt = "Write the report, shorter."

	saved, err := c.UpdateWorker(ctx, connect.NewRequest(&pb.UpdateWorkerRequest{Workspace: dir, Hash: got.Msg.Hash, Worker: def}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	assert.Equal(t, pb.WorkerState_WORKER_STATE_CHANGED, saved.Msg.Worker.GetState())

	_, err = c.UpdateWorker(ctx, connect.NewRequest(&pb.UpdateWorkerRequest{Workspace: dir, Hash: got.Msg.Hash, Worker: def}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, code)
	assert.Equal(t, "HASH_MISMATCH", info.Reason)

	def.Schedule = "whenever"
	bad, err := c.UpdateWorker(ctx, connect.NewRequest(&pb.UpdateWorkerRequest{Workspace: dir, Hash: saved.Msg.Worker.Hash, Worker: def}))
	require.NoError(t, err)
	assert.Nil(t, bad.Msg.Worker)
	assert.NotEmpty(t, bad.Msg.Problems)

	_, err = c.GetWorkerSpec(ctx, connect.NewRequest(&pb.GetWorkerSpecRequest{Workspace: dir, Name: "ghost"}))
	code, info = errorReason(t, err)
	assert.Equal(t, connect.CodeNotFound, code)
	assert.Equal(t, "UNKNOWN_WORKER", info.Reason)
	assert.Error(t, unary(c.UpdateWorker, &pb.UpdateWorkerRequest{Workspace: dir}), "a worker is required")
}

// A worker is deleted over the API: its folder goes and it's no longer
// listed; deleting it again is UNKNOWN_WORKER.
func TestDeleteWorkerOverTheAPI(t *testing.T) {
	c, _ := serveWorkers(t, time.Hour)
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	created, err := c.CreateWorker(ctx, connect.NewRequest(&pb.CreateWorkerRequest{Workspace: dir, Name: "deps", Schedule: "Daily at 6 AM", Prompt: "Write the report."}))
	require.NoError(t, err)
	_, err = c.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: dir, Name: "deps", Hash: created.Msg.Worker.Hash}))
	require.NoError(t, err)

	require.NoError(t, unary(c.DeleteWorker, &pb.DeleteWorkerRequest{Workspace: dir, Name: "deps"}))
	assert.NoDirExists(t, filepath.Join(dir, ".agents", "workers", "deps"))
	list, err := c.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Empty(t, list.Msg.Workers)

	code, info := errorReason(t, unary(c.DeleteWorker, &pb.DeleteWorkerRequest{Workspace: dir, Name: "deps"}))
	assert.Equal(t, connect.CodeNotFound, code)
	assert.Equal(t, "UNKNOWN_WORKER", info.Reason)
}

// A run's changes are undone over the API, once; a run that isn't going
// can't be watched; and an unknown worker can't be disabled.
func TestWorkerRunsUndoneOverTheAPI(t *testing.T) {
	create := call("create_file", map[string]any{"path": "reports/r.md", "content": "report\n"})
	c, _ := serveWorkers(t, time.Hour, create, text("Wrote it."))
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	addWorker(t, dir, "report", "---\nschedule: Daily at 6 AM\npermissions: [\"write:reports/\"]\n---\nWrite reports/r.md.\n")
	list, err := c.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{Workspace: dir}))
	require.NoError(t, err)
	_, err = c.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: dir, Name: "report", Hash: list.Msg.Workers[0].Hash}))
	require.NoError(t, err)

	started, err := c.RunWorker(ctx, connect.NewRequest(&pb.RunWorkerRequest{Workspace: dir, Name: "report"}))
	require.NoError(t, err)
	id := started.Msg.Run.Id
	var run *pb.WorkerRun
	require.Eventually(t, func() bool {
		got, err := c.GetWorkerRun(ctx, connect.NewRequest(&pb.GetWorkerRunRequest{RunId: id}))
		if err == nil {
			run = got.Msg.Run
		}
		return err == nil && run.Status != pb.RunStatus_RUN_STATUS_RUNNING
	}, 10*time.Second, 20*time.Millisecond)
	require.Equal(t, pb.RunStatus_RUN_STATUS_SUCCEEDED, run.Status, "run %v", run)
	assert.FileExists(t, filepath.Join(dir, "reports", "r.md"))

	undone, err := c.UndoWorkerRun(ctx, connect.NewRequest(&pb.UndoWorkerRunRequest{Workspace: dir, RunId: id}))
	require.NoError(t, err)
	assert.Equal(t, []string{"reports/r.md"}, undone.Msg.Restored)
	assert.NoFileExists(t, filepath.Join(dir, "reports", "r.md"))
	_, err = c.UndoWorkerRun(ctx, connect.NewRequest(&pb.UndoWorkerRunRequest{Workspace: dir, RunId: id}))
	_, info := errorReason(t, err)
	assert.Equal(t, "NOTHING_TO_UNDO", info.Reason, "undone twice")

	_, info = errorReason(t, streamErr(c.WatchWorkerRun(ctx, connect.NewRequest(&pb.WatchWorkerRunRequest{RunId: id}))))
	assert.Equal(t, "RUN_NOT_RUNNING", info.Reason)
	_, err = c.DisableWorker(ctx, connect.NewRequest(&pb.DisableWorkerRequest{Workspace: dir, Name: "nope"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "UNKNOWN_WORKER", info.Reason)
}
