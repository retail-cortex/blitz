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
	"google.golang.org/adk/v2/model"
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
	return serveSchedulerWith(t, rescan, start, runtime.NewMockLLM("m", replies...))
}

// serveSchedulerWith is serveScheduler with llm as every workspace's model.
func serveSchedulerWith(t *testing.T, rescan time.Duration, start bool, llm model.LLM) (pb.WorkerServiceClient, *Server) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	store, err := workers.OpenStore(filepath.Join(t.TempDir(), "workers.json"))
	require.NoError(t, err)
	s := New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{Model: llm, Workers: store})
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

// A run's undo that restores some files reports why the rest weren't, so
// `blitz workers undo` fails through the service as it does locally.
func TestPartialWorkerRunUndoReportsTheError(t *testing.T) {
	createA := call("create_file", map[string]any{"path": "reports/a/r.md", "content": "a\n"})
	createB := call("create_file", map[string]any{"path": "reports/b/r.md", "content": "b\n"})
	c, _ := serveWorkers(t, time.Hour, createA, createB, text("Wrote them."))
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reports", "a"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "reports", "b"), 0o755))
	addWorker(t, dir, "report", "---\nschedule: Daily at 6 AM\npermissions: [\"write:reports/\"]\n---\nWrite the reports.\n")
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

	// reports/b/r.md can't be removed.
	locked := filepath.Join(dir, "reports", "b")
	require.NoError(t, os.Chmod(locked, 0o500))
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	undone, err := c.UndoWorkerRun(ctx, connect.NewRequest(&pb.UndoWorkerRunRequest{Workspace: dir, RunId: id}))
	require.NoError(t, err)
	assert.Equal(t, []string{"reports/a/r.md"}, undone.Msg.Restored)
	require.NotNil(t, undone.Msg.Error, "the file that wasn't restored")
	assert.Contains(t, undone.Msg.Error.Message, "reports/b/r.md")
}

// thinking is a model that never answers: it waits until its call is
// cancelled, as a model working on a long reply would.
type thinking struct{ calls chan struct{} }

func (m thinking) Name() string { return "thinking" }

func (m thinking) GenerateContent(ctx context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		select {
		case m.calls <- struct{}{}:
		default:
		}
		<-ctx.Done()
		yield(nil, ctx.Err())
	}
}

// A run going now is listed among every workspace's running runs, and can
// be stopped: it ends at once, recorded as stopped by the user, and the
// worker can run again.
func TestStopWorkerRun(t *testing.T) {
	llm := thinking{calls: make(chan struct{}, 1)}
	c, _ := serveSchedulerWith(t, time.Hour, true, llm)
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	addWorker(t, dir, "slow", "---\nschedule: Daily at 6 AM\n---\nThink about it.\n")
	list, err := c.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{Workspace: dir}))
	require.NoError(t, err)
	_, err = c.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: dir, Name: "slow", Hash: list.Msg.Workers[0].Hash}))
	require.NoError(t, err)

	_, err = c.StopWorkerRun(ctx, connect.NewRequest(&pb.StopWorkerRunRequest{RunId: "nope"}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeNotFound, code)
	assert.Equal(t, "RUN_NOT_RUNNING", info.Reason)

	started, err := c.RunWorker(ctx, connect.NewRequest(&pb.RunWorkerRequest{Workspace: dir, Name: "slow"}))
	require.NoError(t, err)
	id := started.Msg.Run.Id
	select {
	case <-llm.calls:
	case <-time.After(10 * time.Second):
		t.Fatal("the run never asked its model")
	}
	running, err := c.ListRunningWorkerRuns(ctx, connect.NewRequest(&pb.ListRunningWorkerRunsRequest{}))
	require.NoError(t, err)
	require.Len(t, running.Msg.Runs, 1)
	assert.Equal(t, id, running.Msg.Runs[0].Id)
	assert.Equal(t, "slow", running.Msg.Runs[0].Worker)
	assert.Equal(t, dir, running.Msg.Runs[0].Workspace)

	stopped, err := c.StopWorkerRun(ctx, connect.NewRequest(&pb.StopWorkerRunRequest{RunId: id}))
	require.NoError(t, err)
	assert.Equal(t, id, stopped.Msg.Run.Id)
	var run *pb.WorkerRun
	require.Eventually(t, func() bool {
		got, err := c.GetWorkerRun(ctx, connect.NewRequest(&pb.GetWorkerRunRequest{RunId: id}))
		if err == nil {
			run = got.Msg.Run
		}
		return err == nil && run.Status != pb.RunStatus_RUN_STATUS_RUNNING
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, pb.RunStatus_RUN_STATUS_STOPPED, run.Status, "run %v", run)
	assert.Equal(t, "RUN_STOPPED", run.Error.GetReason())
	assert.Equal(t, api.ErrRunStopped.Error(), run.Error.GetMessage())

	running, err = c.ListRunningWorkerRuns(ctx, connect.NewRequest(&pb.ListRunningWorkerRunsRequest{}))
	require.NoError(t, err)
	assert.Empty(t, running.Msg.Runs)
	again, err := c.RunWorker(ctx, connect.NewRequest(&pb.RunWorkerRequest{Workspace: dir, Name: "slow"}))
	require.NoError(t, err, "the worker runs again")
	_, err = c.StopWorkerRun(ctx, connect.NewRequest(&pb.StopWorkerRunRequest{RunId: again.Msg.Run.Id}))
	require.NoError(t, err)
}

// Rewriting needs instructions; without a model it can reach, it says
// why rather than failing.
func TestRewriteWorkerOverTheAPI(t *testing.T) {
	c, _ := serveWorkers(t, time.Hour)
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	for _, w := range []*pb.WorkerDefinition{nil, {Name: "x", Prompt: " "}} {
		_, err := c.RewriteWorker(ctx, connect.NewRequest(&pb.RewriteWorkerRequest{Workspace: dir, Worker: w}))
		code, info := errorReason(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, code)
		assert.Equal(t, "NO_INSTRUCTIONS", info.Reason)
	}
	res, err := c.RewriteWorker(ctx, connect.NewRequest(&pb.RewriteWorkerRequest{Workspace: dir, Worker: &pb.WorkerDefinition{Name: "x", Prompt: "Find the news."}}))
	require.NoError(t, err)
	assert.Empty(t, res.Msg.Prompt)
	assert.NotEmpty(t, res.Msg.Problem, "no suggestions model can be built here")
	_, err = c.RewriteWorker(ctx, connect.NewRequest(&pb.RewriteWorkerRequest{Workspace: "relative", Worker: &pb.WorkerDefinition{Prompt: "x"}}))
	assert.Error(t, err)
}

// ListAllWorkers lists the home workspace (the global workers, in
// ~/.blitz/workers) first, then every workspace with workers on record,
// a gone folder as missing with its records; forgetting one drops it.
func TestAllWorkersOverTheAPI(t *testing.T) {
	c, s := serveScheduler(t, time.Hour, false)
	ctx := context.Background()
	global := filepath.Join(os.Getenv("HOME"), ".blitz", "workers")
	addWorkerIn(t, global, "news", "---\nschedule: Daily at 7 AM\n---\nSummarise the news.\n")
	project, _ := filepath.EvalSymlinks(t.TempDir())
	addWorker(t, project, "deps", "---\nschedule: Daily at 6 AM\n---\nWrite the report.\n")
	gone, _ := filepath.EvalSymlinks(t.TempDir())
	addWorker(t, gone, "old", "---\nschedule: Daily at 5 AM\n---\nDo the old thing.\n")
	for _, dir := range []string{project, gone} {
		list, err := c.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{Workspace: dir}))
		require.NoError(t, err)
		wk := list.Msg.Workers[0]
		_, err = c.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: dir, Name: wk.Name, Hash: wk.Hash}))
		require.NoError(t, err)
	}
	require.NoError(t, os.RemoveAll(gone))
	s.sched.rescan(ctx) // a gone folder is skipped, said once
	s.sched.rescan(ctx)
	assert.True(t, s.sched.missing[gone], "the gone folder is noted")
	// Every workspace's workers (the tray's list): the gone folder is left out.
	enabled, err := c.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{}))
	require.NoError(t, err, "a gone folder doesn't fail the list")
	require.Len(t, enabled.Msg.Workers, 1)
	assert.Equal(t, "deps", enabled.Msg.Workers[0].Name)
	_, err = c.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{Workspace: gone}))
	code, info := errorReason(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, code, "asked for by name, it still fails: %v", info)

	all, err := c.ListAllWorkers(ctx, connect.NewRequest(&pb.ListAllWorkersRequest{}))
	require.NoError(t, err)
	home := all.Msg.Home
	require.Len(t, all.Msg.Workspaces, 3, "%v", all.Msg.Workspaces)
	byDir := map[string]*pb.WorkspaceWorkers{}
	for _, g := range all.Msg.Workspaces {
		byDir[g.Workspace] = g
	}
	tests := []struct {
		name          string
		dir           string
		home, missing bool
		worker        string
		state         pb.WorkerState
	}{
		{"the global workers", home, true, false, "news", pb.WorkerState_WORKER_STATE_NEW},
		{"a project", project, false, false, "deps", pb.WorkerState_WORKER_STATE_ENABLED},
		{"a gone folder", gone, false, true, "old", pb.WorkerState_WORKER_STATE_ENABLED},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := byDir[tt.dir]
			require.NotNil(t, g, "%v", all.Msg.Workspaces)
			assert.Equal(t, tt.home, g.Home)
			assert.Equal(t, tt.missing, g.Missing)
			assert.Empty(t, g.Problem)
			require.Len(t, g.Workers, 1)
			assert.Equal(t, tt.worker, g.Workers[0].Name)
			assert.Equal(t, tt.state, g.Workers[0].State)
		})
	}
	assert.Equal(t, home, all.Msg.Workspaces[0].Workspace, "home first")
	if p := all.Msg.Policy; assert.NotNil(t, p, "the global limits") {
		assert.Equal(t, int32(2), p.RunningMaxConcurrent, "as the scheduler runs")
		assert.Equal(t, config.WorkerPermissionKinds, p.Allow)
		assert.Equal(t, int32(50), p.DefaultMaxTurns)
		assert.Zero(t, p.MaxTurns, "no cap by default (WK-73)")
		assert.NotEmpty(t, p.Path)
	}

	// A global worker made in the home workspace is written to ~/.blitz/workers.
	made, err := c.CreateWorker(ctx, connect.NewRequest(&pb.CreateWorkerRequest{Workspace: home, Name: "digest", Schedule: "Daily at 8 AM", Prompt: "Write the digest."}))
	require.NoError(t, err, "%v", made)
	assert.FileExists(t, filepath.Join(global, "digest", workers.FileName))

	_, err = c.ForgetWorkspaceWorkers(ctx, connect.NewRequest(&pb.ForgetWorkspaceWorkersRequest{Workspace: gone}))
	require.NoError(t, err)
	all, err = c.ListAllWorkers(ctx, connect.NewRequest(&pb.ListAllWorkersRequest{}))
	require.NoError(t, err)
	assert.Len(t, all.Msg.Workspaces, 2, "the gone folder is forgotten: %v", all.Msg.Workspaces)
	_, err = c.ForgetWorkspaceWorkers(ctx, connect.NewRequest(&pb.ForgetWorkspaceWorkersRequest{Workspace: "relative"}))
	code, info = errorReason(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, code, "%v", info)
}

func addWorkerIn(t *testing.T, root, name, content string) {
	t.Helper()
	wd := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(wd, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wd, workers.FileName), []byte(content), 0o644))
}

// A run that has finished but is still going for the scheduler is listed
// once, as it goes; the others in order.
func TestWithLive(t *testing.T) {
	for _, tc := range []struct {
		name           string
		live, recorded []api.Run
		want           []string
	}{
		{name: "none", want: nil},
		{name: "recorded only", recorded: []api.Run{{ID: "b"}, {ID: "a"}}, want: []string{"b", "a"}},
		{name: "live then recorded", live: []api.Run{{ID: "c"}}, recorded: []api.Run{{ID: "b"}}, want: []string{"c", "b"}},
		{name: "in both: once", live: []api.Run{{ID: "c", Status: api.RunRunning}}, recorded: []api.Run{{ID: "c", Status: api.RunSucceeded}, {ID: "b"}}, want: []string{"c", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ids []string
			for _, r := range withLive(tc.live, tc.recorded) {
				ids = append(ids, r.ID)
			}
			assert.Equal(t, tc.want, ids)
		})
	}
}

// The schedule picker's preview: a schedule as understood, its next runs,
// or why it can't be read, as a problem rather than an error.
func TestPreviewScheduleOverTheAPI(t *testing.T) {
	c, _ := serveWorkers(t, time.Hour)
	ctx := context.Background()
	tests := []struct {
		name, schedule, tz string
		count              int32
		cron, problem      string
		runs               int
	}{
		{name: "plain text", schedule: "every day at 8 am", tz: "UTC", cron: "0 8 * * *", runs: 5},
		{name: "a count", schedule: "@hourly", tz: "UTC", count: 3, cron: "@hourly", runs: 3},
		{name: "unreadable", schedule: "whenever", tz: "UTC", problem: "whenever"},
		{name: "unknown time zone", schedule: "@daily", tz: "Mars/Olympus", problem: "Mars/Olympus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := c.PreviewSchedule(ctx, connect.NewRequest(&pb.PreviewScheduleRequest{Schedule: tt.schedule, Timezone: tt.tz, Count: tt.count}))
			require.NoError(t, err)
			if tt.problem != "" {
				assert.Contains(t, r.Msg.Problem, tt.problem)
				assert.Empty(t, r.Msg.NextRuns)
				return
			}
			assert.Empty(t, r.Msg.Problem)
			assert.Equal(t, tt.cron, r.Msg.Cron)
			assert.Equal(t, "UTC", r.Msg.Timezone)
			require.Len(t, r.Msg.NextRuns, tt.runs)
			assert.True(t, r.Msg.NextRuns[0].AsTime().After(time.Now()))
		})
	}
}
