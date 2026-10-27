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
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registered reports whether the scheduler has dir's worker name in cron,
// and at which hash.
func registered(sc *scheduler, dir, name string) (string, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	e, ok := sc.entries[key(dir, name)]
	return e.hash, ok
}

// A worker is registered once per version: the same one again changes
// nothing, a new hash replaces it, a schedule that doesn't parse removes
// it; a rescan skips workspaces that can't open and workers not enabled,
// and forgets the rest.
func TestSchedulerRegistration(t *testing.T) {
	c, s := serveScheduler(t, time.Hour, false)
	sc := s.sched
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	info := api.WorkerInfo{Name: "a", Hash: "h1", Cron: "0 6 * * *", State: api.StateEnabled}
	sc.register(dir, info)
	sc.mu.Lock()
	first := sc.entries[key(dir, "a")].id
	sc.mu.Unlock()
	sc.register(dir, info)
	sc.mu.Lock()
	assert.Equal(t, first, sc.entries[key(dir, "a")].id, "registered again unchanged")
	sc.mu.Unlock()
	info.Hash = "h2"
	sc.register(dir, info)
	hash, ok := registered(sc, dir, "a")
	require.True(t, ok)
	assert.Equal(t, "h2", hash, "the new version replaces the old")
	info.Cron = "whenever"
	sc.register(dir, info)
	_, ok = registered(sc, dir, "a")
	assert.False(t, ok, "a schedule that doesn't parse is dropped")

	// One enabled worker, one new one, and a workspace that's gone.
	addWorker(t, dir, "on", "---\nschedule: Daily at 6 AM\n---\nGo.\n")
	addWorker(t, dir, "new", "---\nschedule: Daily at 6 AM\n---\nGo.\n")
	list, err := c.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{Workspace: dir}))
	require.NoError(t, err)
	for _, wk := range list.Msg.Workers {
		if wk.Name == "on" {
			_, err = c.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: dir, Name: "on", Hash: wk.Hash}))
			require.NoError(t, err)
		}
	}
	require.NoError(t, sc.cfg.Store.Enable(filepath.Join(dir, "gone"), &workers.Worker{Name: "x", Hash: "h"}, "h"))
	sc.register(dir, api.WorkerInfo{Name: "stale", Hash: "h", Cron: "0 6 * * *"})
	sc.rescan(ctx)
	_, ok = registered(sc, dir, "on")
	assert.True(t, ok, "the enabled worker")
	_, ok = registered(sc, dir, "new")
	assert.False(t, ok, "a worker never enabled")
	_, ok = registered(sc, dir, "stale")
	assert.False(t, ok, "a worker no longer enabled")
}

// A worker that asks to catch up runs once when its last run was before
// its latest scheduled time.
func TestSchedulerCatchesUp(t *testing.T) {
	_, s := serveScheduler(t, time.Hour, false)
	sc := s.sched
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	addWorker(t, dir, "late", "---\nschedule: Daily at 6 AM\ncatch_up: once\n---\nGo.\n")
	w, err := s.workspace(context.Background(), dir)
	require.NoError(t, err)
	list, err := w.ListWorkers()
	require.NoError(t, err)
	require.Len(t, list, 1)
	_, err = w.EnableWorker("late", list[0].Hash)
	require.NoError(t, err)
	require.NoError(t, sc.cfg.Runs.Append(api.Run{ID: "old", Workspace: dir, Worker: "late", Status: api.RunSucceeded, Started: time.Now().Add(-72 * time.Hour)}))
	info, err := w.ListWorkers()
	require.NoError(t, err)
	require.Equal(t, "once", info[0].CatchUp)

	sc.register(dir, info[0])
	require.Eventually(t, func() bool {
		runs, err := w.WorkerRuns("late", 5)
		return err == nil && len(runs) == 2
	}, 10*time.Second, 20*time.Millisecond, "no catch-up run")
}

// Starting a run fails for a workspace that can't open, with every slot
// busy, once stopped, and for a worker that can't start; StartScheduler
// does nothing without a scheduler, or once stopped.
func TestSchedulerStartFailures(t *testing.T) {
	_, s := serveWorkers(t, time.Hour)
	sc := s.sched
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	_, err := sc.start(ctx, "relative/dir", "x", true)
	assert.Equal(t, "INVALID_WORKSPACE", errorInfo(err).GetReason())
	_, err = sc.start(ctx, dir, "nope", false)
	assert.ErrorIs(t, err, api.ErrUnknownWorker, "a worker that never started")

	sc.slots <- struct{}{}
	sc.slots <- struct{}{} // both slots busy
	_, err = sc.start(ctx, dir, "nope", true)
	assert.Equal(t, "TOO_MANY_RUNS", errorInfo(err).GetReason())
	<-sc.slots
	<-sc.slots

	New(nil).StartScheduler(ctx) // no scheduler: nothing to start
	sc.stop()
	s.StartScheduler(ctx) // stopped: nothing starts
	_, err = sc.start(ctx, dir, "nope", true)
	assert.ErrorIs(t, err, errStopped)
	assert.Empty(t, sc.slots, "the slot taken was given back")
}

// A run that hits its limit is recorded as limited, with the reason.
func TestLimitedWorkerRun(t *testing.T) {
	loop := call("list_files", map[string]any{})
	c, _ := serveWorkers(t, time.Hour, loop, loop, loop, loop)
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	addWorker(t, dir, "busy", "---\nschedule: Daily at 6 AM\nlimits: {max_turns: 1}\n---\nList the files, forever.\n")
	list, err := c.ListWorkers(ctx, connect.NewRequest(&pb.ListWorkersRequest{Workspace: dir}))
	require.NoError(t, err)
	_, err = c.EnableWorker(ctx, connect.NewRequest(&pb.EnableWorkerRequest{Workspace: dir, Name: "busy", Hash: list.Msg.Workers[0].Hash}))
	require.NoError(t, err)
	started, err := c.RunWorker(ctx, connect.NewRequest(&pb.RunWorkerRequest{Workspace: dir, Name: "busy"}))
	require.NoError(t, err)
	var run *pb.WorkerRun
	require.Eventually(t, func() bool {
		got, err := c.GetWorkerRun(ctx, connect.NewRequest(&pb.GetWorkerRunRequest{RunId: started.Msg.Run.Id}))
		if err == nil {
			run = got.Msg.Run
		}
		return err == nil && run.Status != pb.RunStatus_RUN_STATUS_RUNNING
	}, 10*time.Second, 20*time.Millisecond)
	assert.Equal(t, pb.RunStatus_RUN_STATUS_LIMITED, run.Status, "run %v", run)
	assert.Equal(t, "RUN_LIMITED", run.Error.GetReason())
}
