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

package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// A run's receipt: each step's run in order with its times, tries,
// agent, models, tokens and cost (its turns' and its rule's judge's),
// what it streamed, and the run's events, total and output.
func TestFlowReceipt(t *testing.T) {
	w, llm := openTestWith(t, configtest.RunTools, text("A long brief."), text("Short."))
	llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 2}
	judge := withJudge(t, w, "fail: too long", "pass")
	judge.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 5, CandidatesTokenCount: 1}
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "notes.md"), []byte("Notes."), 0o644))
	writeRule(t, w, "short", "---\nwhen: after_agent\naction: revise\nagents: [editor]\n---\nOne line.\n")
	writeFlow(t, w, "brief", `nodes:
  - {id: notes, type: read_file, title: Read the notes, path: notes.md}
  - {id: draft, type: agent, title: Draft, instructions: Write., rules: [short]}
edges: [{from: notes, to: draft}]
`)
	s := newSession(t, w)
	var run string
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/brief for the team", Command: true}, func(e api.Event) {
		if e.Step != nil {
			run = e.Step.Run
		}
	})
	require.NoError(t, err)
	require.NotEmpty(t, run, "steps say their run")

	list := FlowReceipts(w.StorageDir(), w.Dir(), "brief")
	require.Len(t, list, 1)
	r, ver, err := FlowReceiptOf(w.StorageDir(), run)
	require.NoError(t, err)
	assert.Zero(t, ver, "an ended run isn't live")
	assert.Equal(t, run, r.Run)
	assert.Equal(t, RunDone, r.Status)
	assert.Equal(t, "brief", r.Workflow)
	assert.Equal(t, "chat", r.Origin)
	assert.Equal(t, s.ID, r.Session)
	assert.Equal(t, "for the team", r.Input)
	assert.Equal(t, "Short.", r.Output)
	assert.False(t, r.Ended.Before(r.Started))
	require.Len(t, r.Steps, 2)
	notes, draft := r.Steps[0], r.Steps[1]
	assert.Equal(t, "notes", notes.ID)
	assert.Equal(t, api.StepDone, notes.Status)
	assert.Equal(t, 1, notes.Attempts)
	assert.Zero(t, notes.Usage.Calls)
	assert.Empty(t, notes.Agent, "not an agent step")
	assert.Equal(t, "Notes.", notes.Output)
	assert.Equal(t, "draft", draft.ID)
	assert.Equal(t, "Draft", draft.Title)
	assert.Equal(t, "agent", draft.Type)
	assert.Equal(t, w.engine.ActiveAgent(), draft.Agent)
	assert.Equal(t, 4, draft.Usage.Calls, "two turns and two judgements")
	assert.Equal(t, int64(30), draft.Usage.Input)
	assert.Equal(t, int64(6), draft.Usage.Output)
	assert.Contains(t, draft.Models, "judge")
	assert.Len(t, draft.Models, 2)
	assert.Contains(t, draft.Stream, "A long brief.")
	assert.Contains(t, draft.Stream, "Short.")
	assert.False(t, draft.Started.IsZero())
	assert.False(t, draft.Ended.Before(draft.Started))
	assert.Equal(t, draft.Usage.Calls, r.Usage.Calls, "the run's total is its steps'")
	assert.ElementsMatch(t, draft.Models, r.Models)
	var kinds []string
	for _, e := range r.Events {
		kinds = append(kinds, e.Kind+":"+e.Text)
	}
	assert.Equal(t, "run:started", kinds[0])
	assert.Equal(t, "run:done", kinds[len(kinds)-1])
	assert.Contains(t, kinds, "step:running")

	sum := r.Summary()
	assert.Empty(t, sum.Events)
	assert.Empty(t, sum.Steps[1].Stream)
	assert.Empty(t, sum.Steps[0].Output)
	assert.NotEmpty(t, r.Steps[1].Stream, "the summary is a copy")

	assert.Empty(t, FlowReceipts(w.StorageDir(), w.Dir(), "other"))
	assert.Empty(t, FlowReceipts(w.StorageDir(), "/elsewhere", ""))
	for _, bad := range []string{"", "..", "a/b", run + "x"} {
		_, _, err := FlowReceiptOf(w.StorageDir(), bad)
		assert.ErrorIs(t, err, ErrNoRun, bad)
	}
}

// A failed run's receipt says where and why; a stopped one says it was
// stopped; a paused one waits, and carries on when it resumes.
func TestFlowReceiptEnds(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	s := newSession(t, w)
	runOf := func(text string, ctx context.Context) (FlowReceipt, error) {
		var run string
		_, err := w.Run(ctx, s.ID, api.Turn{Text: text, Command: true}, func(e api.Event) {
			if e.Step != nil {
				run = e.Step.Run
			}
		})
		if run == "" {
			list := FlowReceipts(w.StorageDir(), w.Dir(), strings.Fields(strings.TrimPrefix(text, "/"))[0])
			require.NotEmpty(t, list)
			return list[0], err
		}
		r, _, rerr := FlowReceiptOf(w.StorageDir(), run)
		require.NoError(t, rerr)
		return r, err
	}

	writeFlow(t, w, "reads", "nodes: [{id: a, type: read_file, path: missing.md}]\n")
	r, err := runOf("/reads", context.Background())
	require.Error(t, err)
	assert.Equal(t, RunFailed, r.Status)
	assert.Contains(t, r.Error, "missing.md")
	require.Len(t, r.Steps, 1)
	assert.Equal(t, api.StepFailed, r.Steps[0].Status)
	assert.Contains(t, r.Steps[0].Error, "missing.md")
	assert.True(t, r.Events[len(r.Events)-1].Error)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writeFlow(t, w, "copy", "nodes: [{id: a, type: accumulate}]\n")
	r, err = runOf("/copy", ctx)
	require.Error(t, err)
	assert.Equal(t, RunStopped, r.Status)

	writeFlow(t, w, "later", "nodes:\n  - {id: wait, type: timer, wait: 1h}\n  - {id: b, type: accumulate}\nedges: [{from: wait, to: b}]\n")
	r, err = runOf("/later", context.Background())
	require.NoError(t, err)
	assert.Equal(t, RunWaiting, r.Status)
	assert.True(t, r.Ended.IsZero(), "a paused run hasn't ended")
	require.Len(t, r.Steps, 1)
	assert.Equal(t, api.StepWaiting, r.Steps[0].Status)
	recent := RecentFlowReceipts(w.StorageDir(), w.Dir(), time.Now().Add(time.Hour))
	require.Len(t, recent, 1, "a paused run is listed however old")
	assert.Equal(t, r.Run, recent[0].Run)

	tasks := w.FlowTasks()
	require.Len(t, tasks, 1)
	_, err = w.ResumeFlow(context.Background(), tasks[0].Tasks[0].ID, map[string]any{"fired_at": "now"}, func(api.Event) {})
	require.NoError(t, err)
	r, _, err = FlowReceiptOf(w.StorageDir(), r.Run)
	require.NoError(t, err)
	assert.Equal(t, RunDone, r.Status)
	require.Len(t, r.Steps, 2, "the timer's run carried on, then b ran")
	assert.Equal(t, api.StepDone, r.Steps[0].Status)
	assert.False(t, r.Steps[0].Ended.IsZero())
	var resumed bool
	for _, e := range r.Events {
		resumed = resumed || e.Kind == "run" && e.Text == "resumed"
	}
	assert.True(t, resumed)

	assert.False(t, StopFlowRun("nothing"))
}

// A run is stopped through its receipt from anywhere: a step that's
// running ends with it.
func TestStopFlowRun(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	s := newSession(t, w)
	writeFlow(t, w, "slow", "nodes: [{id: a, type: timer, wait: 1h}]\n")
	r := &flowRun{f: w.Workflows()[0], runID: "flow-stop", session: s.ID, on: ignore}
	ctx, cancel := context.WithCancel(context.Background())
	lr := w.beginReceipt(r, "worker", cancel)
	r.receipt = lr
	r.emit(api.Event{Step: &api.FlowStep{Workflow: "slow", ID: "a", Status: api.StepRunning}})
	m := &stepMeter{}
	lr.meter("a", m)
	m.add("m1", api.Usage{Calls: 1, Input: 3, CostUSD: 0.5, Priced: true})
	live, ver := lr.snapshot()
	assert.Positive(t, ver)
	assert.Equal(t, 1, live.Steps[0].Usage.Calls, "a running step's spending so far")
	assert.Equal(t, 0.5, live.Usage.CostUSD)
	assert.Equal(t, "worker", live.Origin)

	got, gotVer, err := FlowReceiptOf(w.StorageDir(), "flow-stop")
	require.NoError(t, err)
	assert.Equal(t, RunRunning, got.Status)
	assert.Equal(t, ver, gotVer)
	assert.Len(t, RecentFlowReceipts(w.StorageDir(), "", time.Now().Add(time.Hour)), 1, "a live run is listed")

	require.True(t, StopFlowRun("flow-stop"))
	assert.Error(t, ctx.Err())
	lr.end(RunStopped, "", context.Canceled)
	got, _, err = FlowReceiptOf(w.StorageDir(), "flow-stop")
	require.NoError(t, err)
	assert.Equal(t, RunStopped, got.Status)
	assert.Equal(t, api.StepFailed, got.Steps[0].Status, "it ended with the run")
	assert.Equal(t, 0.5, got.Steps[0].Usage.CostUSD)
	assert.False(t, StopFlowRun("flow-stop"), "it's not running any more")
}

// A receipt that says running that no one's running and that hasn't been
// written lately is a lost run; old receipts are pruned; recent ones are
// those written since.
func TestFlowReceiptsOnDisk(t *testing.T) {
	storage := t.TempDir()
	dir := FlowReceiptsDir(storage)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	write := func(r FlowReceipt, age time.Duration) {
		data, err := json.Marshal(r)
		require.NoError(t, err)
		path := filepath.Join(dir, r.Run+".json")
		require.NoError(t, os.WriteFile(path, data, 0o600))
		at := time.Now().Add(-age)
		require.NoError(t, os.Chtimes(path, at, at))
	}
	now := time.Now()
	write(FlowReceipt{Run: "lost", Workspace: "/w", Workflow: "a", Status: RunRunning, Started: now.Add(-time.Hour)}, time.Hour)
	write(FlowReceipt{Run: "busy", Workspace: "/w", Workflow: "a", Status: RunRunning, Started: now.Add(-time.Minute)}, time.Second)
	write(FlowReceipt{Run: "old", Workspace: "/v", Workflow: "b", Status: RunDone, Started: now.Add(-2 * time.Hour), Ended: now.Add(-2 * time.Hour)}, 2*time.Hour)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "junk.json"), []byte("{"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o600))

	all := FlowReceipts(storage, "", "")
	require.Len(t, all, 3)
	assert.Equal(t, []string{"busy", "lost", "old"}, []string{all[0].Run, all[1].Run, all[2].Run}, "newest first")
	assert.Equal(t, RunRunning, all[0].Status, "written lately: maybe another process's")
	assert.Equal(t, RunStopped, all[1].Status)
	assert.Contains(t, all[1].Error, "stopped while it ran")
	assert.False(t, all[1].Ended.IsZero())
	assert.Len(t, FlowReceipts(storage, "/w", ""), 2)
	assert.Len(t, FlowReceipts(storage, "", "b"), 1)

	recent := RecentFlowReceipts(storage, "", now.Add(-10*time.Minute))
	require.Len(t, recent, 1)
	assert.Equal(t, "busy", recent[0].Run)
	assert.Empty(t, RecentFlowReceipts(storage, "/v", now.Add(-10*time.Minute)))

	old := keptReceipts
	keptReceipts = 3
	t.Cleanup(func() { keptReceipts = old })
	pruneReceipts(dir)
	left, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	assert.Len(t, left, 2, "room for the new one")
	assert.NoFileExists(t, filepath.Join(dir, "old.json"), "the oldest went")
}

// A step's meter adds its calls (and a called workflow's steps' to the
// call step's); streamed text is held to its limit; usage between two of
// a session's is what was spent.
func TestStepMeter(t *testing.T) {
	outer, inner := &stepMeter{}, &stepMeter{}
	ctx := withMeter(withMeter(context.Background(), outer), inner)
	meterUsage(ctx, "a", api.Usage{Calls: 1, Input: 2, CostUSD: 1, Priced: true})
	meterUsage(ctx, "b", api.Usage{Calls: 1, Input: 3, Priced: false})
	meterUsage(context.Background(), "c", api.Usage{Calls: 1})
	u, models, _ := inner.read()
	assert.Equal(t, 2, u.Calls)
	assert.Equal(t, int64(5), u.Input)
	assert.False(t, u.Priced, "b has no price")
	assert.Equal(t, []string{"a", "b"}, models)
	u, _, _ = outer.read()
	assert.Equal(t, 2, u.Calls, "the call step's too")
	empty, _, _ := (&stepMeter{}).read()
	assert.True(t, empty.Priced, "nothing spent is no unknown price")

	got := usageSince(api.Usage{Calls: 1, Input: 10, CostUSD: 0.1, SearchQueries: 1}, api.Usage{Calls: 3, Input: 25, Output: 4, CostUSD: 0.4, Priced: true, LastPrompt: 15, SearchQueries: 2})
	assert.Equal(t, 2, got.Calls)
	assert.Equal(t, int64(15), got.Input)
	assert.Equal(t, int64(4), got.Output)
	assert.InDelta(t, 0.3, got.CostUSD, 1e-9)
	assert.Equal(t, 1, got.SearchQueries)
	assert.Equal(t, int64(15), got.LastPrompt)

	lr := &liveReceipt{path: filepath.Join(t.TempDir(), "r.json"), meters: map[string]*stepMeter{}}
	lr.r.Steps = []StepReceipt{{ID: "a", Status: api.StepRunning}}
	lr.stream("b", "nowhere")
	for range receiptStream/1024 + 2 {
		lr.stream("a", strings.Repeat("x", 1024))
	}
	assert.LessOrEqual(t, len(lr.r.Steps[0].Stream), receiptStream+8)
	lr.r.Events = make([]ReceiptEvent, receiptEvents)
	lr.note("a", "tool", "read_file", false)
	assert.Len(t, lr.r.Events, receiptEvents, "the latest kept")
	assert.Equal(t, "read_file", lr.r.Events[receiptEvents-1].Text)
	assert.FileExists(t, lr.path)
}
