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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func addWorker(t *testing.T, w *Workspace, name, content string) {
	t.Helper()
	dir := filepath.Join(w.Dir(), "workers", name)
	os.MkdirAll(dir, 0o755)
	require.NoError(t, os.WriteFile(filepath.Join(dir, workers.FileName), []byte(content), 0o644))
}

func TestWorkersLifecycle(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Workers.Policy.Allow = []string{"shell", "write"} })
	addWorker(t, w, "deps", "---\nschedule: Daily at 6 AM\npermissions: [\"shell:go list -m -u all\", \"web:proxy.golang.org\"]\n---\nReport outdated modules.\n")
	addWorker(t, w, "broken", "---\nschedule: whenever\n---\ndo it\n")

	list, err := w.ListWorkers()
	require.NoError(t, err, "list %v", list)
	require.Len(t, list, 2, "list %v %v", list, err)
	broken, deps := list[0], list[1]
	assert.Equal(t, api.StateInvalid, broken.State, "broken %+v", broken)
	assert.NotEqual(t, 0, len(broken.Problems), "broken %+v", broken)
	assert.Equal(t, api.StateNew, deps.State, "deps %+v", deps)
	assert.Equal(t, "0 6 * * *", deps.Cron, "deps %+v", deps)
	assert.True(t, deps.Next.IsZero(), "deps %+v", deps)
	assert.Len(t, deps.Permissions, 1, "deps %+v", deps)
	assert.NotEqual(t, 0, deps.Limits.MaxTurns, "deps %+v", deps)
	assert.Len(t, deps.Problems, 1, "deps %+v", deps)
	assert.Contains(t, deps.Problems[0], "web", "deps %+v", deps)

	_, staleErr := w.EnableWorker("deps", "sha256:stale")
	assert.ErrorIs(t, staleErr, api.ErrHashMismatch, "enabling a stale hash")
	_, brokenErr := w.EnableWorker("broken", broken.Hash)
	assert.Error(t, brokenErr, "an invalid worker isn't enabled")
	// A name is never a path: nothing outside workers/ is looked up.
	for _, bad := range []string{"../deps", "deps/..", "/etc", "Deps"} {
		t.Run(bad, func(t *testing.T) {
			_, err := w.EnableWorker(bad, "x")
			assert.ErrorIs(t, err, api.ErrUnknownWorker, "enable %q: %v", bad, err)
			_, err = w.WorkerRuns(bad, 1)
			assert.ErrorIs(t, err, api.ErrUnknownWorker, "runs of %q: %v", bad, err)
		})
	}
	_, unknownErr := w.EnableWorker("nope", "x")
	assert.ErrorIs(t, unknownErr, api.ErrUnknownWorker)
	on, err := w.EnableWorker("deps", deps.Hash)
	require.NoError(t, err, "enable %+v", on)
	require.Equal(t, api.StateEnabled, on.State, "enable %+v %v", on, err)
	require.False(t, on.Next.IsZero(), "enable %+v %v", on, err)

	// An edit suspends it.
	addWorker(t, w, "deps", "---\nschedule: Daily at 7 AM\n---\nReport outdated modules.\n")
	list, _ = w.ListWorkers()
	assert.Equal(t, api.StateChanged, list[1].State, "after an edit: %+v", list[1])
	assert.True(t, list[1].Next.IsZero(), "after an edit: %+v", list[1])
	off, err := w.DisableWorker("deps")
	assert.NoError(t, err, "disable %+v", off)
	assert.Equal(t, api.StateDisabled, off.State, "disable %+v %v", off, err)
}

// A policy reloaded from the settings applies from the next listing:
// its default limits fill in, and kinds it no longer allows are dropped.
func TestReloadWorkerPolicy(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Workers.Policy.Allow = []string{"shell"} })
	addWorker(t, w, "deps", "---\nschedule: Daily at 6 AM\npermissions: [\"shell:go list -m -u all\"]\n---\nReport outdated modules.\n")
	before, err := w.ListWorkers()
	require.NoError(t, err)
	require.Len(t, before[0].Permissions, 1)

	cfg := config.DefaultConfig()
	cfg.Workers.Policy.Allow = nil
	cfg.Workers.Policy.DefaultMaxTurns = 7
	w.ReloadWorkerPolicy(cfg)
	after, err := w.ListWorkers()
	require.NoError(t, err)
	assert.Empty(t, after[0].Permissions, "shell isn't allowed any more")
	assert.Equal(t, 7, after[0].Limits.MaxTurns, "the new default")
}

func TestWorkersCanBeTurnedOff(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Workers.Enabled = false })
	_, err := w.ListWorkers()
	assert.ErrorIs(t, err, api.ErrWorkersDisabled, "%v", err)
}

func enable(t *testing.T, w *Workspace, name string) {
	t.Helper()
	list, err := w.ListWorkers()
	require.NoError(t, err)
	for _, info := range list {
		if info.Name == name {
			_, err := w.EnableWorker(name, info.Hash)
			require.NoError(t, err)
			return
		}
	}
	t.Fatalf("no worker %s", name)
}

func TestRunWorkerEnforcesPermissions(t *testing.T) {
	create := func(path string) *genai.Content {
		return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": path, "content": "x\n"}}}}}
	}
	ask := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "ask_user_question", Args: map[string]any{"question": "ok?"}}}}}
	// auto_approve is on for people; it must not widen what a worker may do.
	w, llm := openTestWith(t, configtest.RunTools,
		create("reports/deps.md"), create("main.go"), ask, text("Wrote the report."))
	addWorker(t, w, "deps", "---\nschedule: daily at 6 AM\npermissions: [\"write:reports/\"]\n---\nWrite reports/deps.md.\n")
	user := newSession(t, w)

	_, err := w.RunWorker(context.Background(), "deps", RunOptions{Manual: true})
	require.ErrorIs(t, err, api.ErrWorkerNotEnabled, "not enabled: %v", err)
	enable(t, w, "deps")
	var results []string
	var started api.Run
	run, err := w.RunWorker(context.Background(), "deps", RunOptions{Manual: true, OnStart: func(r api.Run) { started = r }, OnEvent: func(e api.Event) {
		if e.ToolResult != nil {
			results = append(results, fmt.Sprint(e.ToolResult.Result))
		}
	}})
	assert.Equal(t, run.ID, started.ID, "started %+v", started)
	assert.Equal(t, api.RunRunning, started.Status, "started %+v", started)
	require.NoError(t, err, "run %+v", run)
	require.Equal(t, api.RunSucceeded, run.Status, "run %+v %v", run, err)
	require.True(t, run.Manual, "run %+v %v", run, err)
	require.NotEqual(t, "", run.SessionID, "run %+v %v", run, err)
	require.NotEqual(t, user.ID, run.SessionID, "run %+v %v", run, err)
	assert.FileExists(t, filepath.Join(w.Dir(), "reports", "deps.md"), "the permitted write happened")
	assert.NoFileExists(t, filepath.Join(w.Dir(), "main.go"), "the refused write didn't happen")
	assert.Len(t, run.Refusals, 1, "refusals %+v", run.Refusals)
	assert.Equal(t, api.ActionWrite, run.Refusals[0].Kind, "refusals %+v", run.Refusals)
	assert.Len(t, results, 3, "results %q", results)
	assert.Contains(t, results[2], "unattended", "results %q", results)
	first := llm.Requests[0].Contents
	got := first[len(first)-1].Parts[0].Text
	assert.Contains(t, got, "running unattended", "the agent wasn't told it runs unattended")
	assert.Contains(t, got, "Write reports/deps.md.", "the agent wasn't told it runs unattended")
	// The workspace's own session was left alone.
	active, _ := w.ActiveSession()
	assert.Equal(t, user.ID, active.ID, "active session %s with %d messages", active.ID, len(active.Messages))
	assert.Len(t, active.Messages, 0, "active session %s with %d messages", active.ID, len(active.Messages))
	runs, err := w.WorkerRuns("deps", 0)
	assert.NoError(t, err, "recorded runs %+v", runs)
	assert.Len(t, runs, 1, "recorded runs %+v %v", runs, err)
	assert.Equal(t, run.ID, runs[0].ID, "recorded runs %+v %v", runs, err)
	assert.Equal(t, []string{"reports/deps.md"}, runs[0].Files, "the run's files are recorded")
	list, err := w.ListSessions(false)
	require.NoError(t, err)
	i := slices.IndexFunc(list, func(s api.SessionInfo) bool { return s.ID == run.SessionID })
	require.GreaterOrEqual(t, i, 0, "the run's session is listed")
	assert.Equal(t, session.OriginWorker, list[i].Origin, "listed as a worker's run")

	// Its change is its own: not the sessions' /undo, but the run's undo
	// (BL-WK-01, BL-WK-02).
	_, err = w.Undo("", false)
	assert.ErrorIs(t, err, api.ErrNothingToUndo, "/undo reached the worker's change")
	assert.FileExists(t, filepath.Join(w.Dir(), "reports", "deps.md"))
	undone, err := w.UndoWorkerRun(run.ID, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"reports/deps.md"}, undone.Restored)
	assert.NoFileExists(t, filepath.Join(w.Dir(), "reports", "deps.md"))
	_, err = w.UndoWorkerRun(run.ID, false)
	assert.ErrorIs(t, err, api.ErrNothingToUndo)
}

func TestRunWorkerStopsAtItsTurnLimit(t *testing.T) {
	list := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_files", Args: map[string]any{}}}}}
	w, _ := openTestWith(t, nil, list, list, list, text("done"))
	addWorker(t, w, "loop", "---\nschedule: hourly\nlimits: {max_turns: 2}\n---\nLook around.\n")
	enable(t, w, "loop")
	run, err := w.RunWorker(context.Background(), "loop", RunOptions{})
	assert.NoError(t, err, "run %+v", run)
	assert.Equal(t, api.RunLimited, run.Status, "run %+v %v", run, err)
	assert.NotEqual(t, "", run.Error, "run %+v %v", run, err)
}

// A worker's agent and model apply to its runs only.
func TestRunWorkerUsesItsAgentAndModel(t *testing.T) {
	w, main := openTestWith(t, nil, text("from the workspace model"))
	addWorker(t, w, "review", "---\nschedule: hourly\nagent: qa\nmodel: anthropic/claude-sonnet-5\n---\nReview the tests.\n")
	addWorker(t, w, "badmodel", "---\nschedule: hourly\nmodel: broken\n---\nDo it.\n")
	addWorker(t, w, "nobody", "---\nschedule: hourly\nagent: nobody\n---\nDo it.\n")
	for _, name := range []string{"review", "badmodel", "nobody"} {
		enable(t, w, name)
	}

	run, err := w.RunWorker(context.Background(), "review", RunOptions{})
	require.NoError(t, err, "run %+v", run)
	require.Equal(t, api.RunSucceeded, run.Status, "run %+v %v", run, err)
	assert.Equal(t, 0, main.Calls(), "the workspace model answered %d times; the worker's model should have", main.Calls())
	sessions, _ := w.ListSessions(false)
	found := false
	for _, s := range sessions {
		if s.ID == run.SessionID {
			found = true
			assert.Equal(t, "qa", s.Agent, "run session agent %q, want qa", s.Agent)
		}
	}
	assert.True(t, found, "run session %s not listed", run.SessionID)
	assert.Equal(t, "blitz", w.ActiveAgent().Name, "the run changed the workspace: %s on %s", w.ActiveAgent().Name, w.Model().Name)
	assert.Equal(t, "gemini-3.8-flash", w.Model().Name, "the run changed the workspace: %s on %s", w.ActiveAgent().Name, w.Model().Name)

	run, _ = w.RunWorker(context.Background(), "badmodel", RunOptions{})
	assert.Equal(t, api.RunFailed, run.Status, "unbuildable model: %+v", run)
	assert.Contains(t, run.Error, "broken", "unbuildable model: %+v", run)
	list, _ := w.ListWorkers()
	for _, info := range list {
		t.Run(info.Name, func(t *testing.T) {
			assert.False(t, info.Name == "nobody" && (len(info.Problems) == 0 || !strings.Contains(info.Problems[0], "nobody")), "unknown agent not reported: %+v", info.Problems)
		})
	}
	run, _ = w.RunWorker(context.Background(), "nobody", RunOptions{})
	assert.Equal(t, api.RunFailed, run.Status, "unknown agent: %+v", run)
	assert.Contains(t, run.Error, "nobody", "unknown agent: %+v", run)
}

// A run's timeout and cost limit stop it as "limited", with the limit as
// the reason (they share the turn's limits since the refactor).
func TestRunWorkerStopsAtItsTimeAndCostLimits(t *testing.T) {
	sleep := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "run_shell_command", Args: map[string]any{"command": "sleep 5"}}}}}
	w, _ := openTestWith(t, func(c *config.Config) { c.Workers.Policy.Allow = []string{"shell"} }, sleep, sleep)
	addWorker(t, w, "slow", "---\nschedule: hourly\npermissions: [\"shell:sleep 5\"]\nlimits: {timeout: 300ms}\n---\nWait.\n")
	enable(t, w, "slow")
	run, err := w.RunWorker(context.Background(), "slow", RunOptions{})
	assert.NoError(t, err, "timeout run %+v", run)
	assert.Equal(t, api.RunLimited, run.Status, "timeout run %+v %v", run, err)
	assert.Contains(t, run.Error, "time limit", "timeout run %+v %v", run, err)
	assert.LessOrEqual(t, run.Duration, time.Duration(4e9), "timeout run %+v %v", run, err)

	list := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_files", Args: map[string]any{}}}}}
	w2, llm := openTestWith(t, nil, list, list, list, list, list, list)
	llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 10}
	addWorker(t, w2, "spend", "---\nschedule: hourly\nlimits: {max_cost_usd: 0.0002}\n---\nLook around.\n")
	enable(t, w2, "spend")
	run, err = w2.RunWorker(context.Background(), "spend", RunOptions{})
	assert.NoError(t, err, "cost run %+v", run)
	assert.Equal(t, api.RunLimited, run.Status, "cost run %+v %v", run, err)
	assert.Contains(t, run.Error, "cost limit", "cost run %+v %v", run, err)
}

// A workflow's job may do what the workflow's steps do as they are when
// it runs (WF-20, N26 desktop-4#19), within the policy, without the job
// listing it.
func TestRunWorkerWorkflowGrants(t *testing.T) {
	for _, tc := range []struct {
		name   string
		allow  []string
		status api.RunStatus
		wrote  bool
	}{
		{name: "its save step writes", allow: []string{"write"}, status: api.RunSucceeded, wrote: true},
		{name: "the policy still applies", allow: []string{"shell"}, status: api.RunFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := openTestWith(t, func(c *config.Config) { c.Workers.Policy.Allow = tc.allow })
			writeFlow(t, w, "save", "nodes:\n  - {id: s, type: save_file, path: out/report.md}\n")
			addWorker(t, w, "flow", "---\nschedule: hourly\nworkflow: save\n---\nThe report.\n")
			enable(t, w, "flow")
			run, err := w.RunWorker(context.Background(), "flow", RunOptions{})
			require.NoError(t, err)
			assert.Equal(t, tc.status, run.Status, "run %+v", run)
			_, statErr := os.Stat(filepath.Join(w.Dir(), "out", "report.md"))
			assert.Equal(t, tc.wrote, statErr == nil, "the file written: %v", statErr)
		})
	}
}

// A job's hash covers the workflow it runs, whose steps add to what it
// may do: editing, creating or removing the workflow suspends an enabled
// job as changed, an edit read before it is refused, and the job runs
// again once enabled at the new hash (scan N26 desktop-4#19 follow-up).
func TestWorkflowChangeSuspendsJob(t *testing.T) {
	const save = "nodes:\n  - {id: s, type: save_file, path: out/report.md}\n"
	const wider = "nodes:\n  - {id: s, type: save_file, path: out/report.md}\n  - {id: b, type: save_file, path: notes/b.md}\nedges: [{from: s, to: b}]\n"
	for _, tc := range []struct {
		name          string
		before, after string // "" for no workflow file
	}{
		{name: "edited", before: save, after: wider},
		{name: "created", after: save},
		{name: "removed", before: save},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := openTestWith(t, func(c *config.Config) { c.Workers.Policy.Allow = []string{"write"} })
			if tc.before != "" {
				writeFlow(t, w, "save", tc.before)
			}
			addWorker(t, w, "flow", "---\nschedule: hourly\nworkflow: save\n---\nThe report.\n")
			enable(t, w, "flow")
			_, read, err := w.GetWorkerSpec("flow")
			require.NoError(t, err)

			if tc.after != "" {
				writeFlow(t, w, "save", tc.after)
			} else {
				require.NoError(t, w.DeleteWorkflow("save"))
			}
			list, err := w.ListWorkers()
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, api.StateChanged, list[0].State, "the job ran on with its workflow changed")
			assert.NotEqual(t, read, list[0].Hash)
			_, err = w.RunWorker(context.Background(), "flow", RunOptions{})
			assert.ErrorIs(t, err, api.ErrWorkerNotEnabled)
			spec, now, err := w.GetWorkerSpec("flow")
			require.NoError(t, err)
			assert.Equal(t, list[0].Hash, now, "GetWorkerSpec and the listing disagree")
			_, _, err = w.UpdateWorker(spec, read)
			assert.ErrorIs(t, err, api.ErrHashMismatch, "an edit read before the workflow changed")
			_, err = w.EnableWorker("flow", read)
			assert.ErrorIs(t, err, api.ErrHashMismatch, "enabled at the hash before the change")

			enable(t, w, "flow")
			if tc.after == "" {
				return
			}
			run, err := w.RunWorker(context.Background(), "flow", RunOptions{})
			require.NoError(t, err)
			assert.Equal(t, api.RunSucceeded, run.Status, "run %+v", run)
			assert.FileExists(t, filepath.Join(w.Dir(), "out", "report.md"))
		})
	}
}

// A run reads its job's workflow once: the steps it runs, and what they
// may write, are those of the bytes its enabled hash covers. An edit made
// after the hash was checked (afterJobCheck) can't give that run steps
// or permissions nobody reviewed (spec_023 WK-05b, N26 engine-decisions).
func TestJobRunUsesTheWorkflowItsHashCovers(t *testing.T) {
	const save = "nodes:\n  - {id: s, type: save_file, path: out/report.md}\n"
	const swapped = "nodes:\n  - {id: s, type: save_file, path: notes/b.md}\n"
	w, _ := openTestWith(t, func(c *config.Config) { c.Workers.Policy.Allow = []string{"write"} })
	writeFlow(t, w, "save", save)
	addWorker(t, w, "flow", "---\nschedule: hourly\nworkflow: save\n---\nThe report.\n")
	enable(t, w, "flow")
	swap := func() { writeFlow(t, w, "save", swapped) }
	afterJobCheck = swap
	t.Cleanup(func() { afterJobCheck = func() {} })

	run, err := w.RunWorker(context.Background(), "flow", RunOptions{})
	require.NoError(t, err)
	assert.Equal(t, api.RunSucceeded, run.Status, "run %+v", run)
	assert.FileExists(t, filepath.Join(w.Dir(), "out", "report.md"), "the reviewed step didn't run")
	assert.NoFileExists(t, filepath.Join(w.Dir(), "notes", "b.md"), "a step edited in after the check ran")

	// The edit made the job changed: the next run doesn't start.
	_, err = w.RunWorker(context.Background(), "flow", RunOptions{})
	assert.ErrorIs(t, err, api.ErrWorkerNotEnabled)
}

// A job whose body is a workflow keeps the job's limits across its
// steps, runs them on its model, and its steps' changes are the run's to
// undo (spec_023 WK-43, WK-54; N26 engine-top-2#2).
func TestRunWorkerWorkflowIsTheRun(t *testing.T) {
	call := func(name string, args map[string]any) *genai.Content {
		return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: name, Args: args}}}}
	}
	list := call("list_files", map[string]any{})
	sleep := call("run_shell_command", map[string]any{"command": "sleep 5"})
	twoSteps := "nodes:\n  - {id: a, type: agent, instructions: Look.}\n  - {id: b, type: agent, instructions: Look again.}\nedges: [{from: a, to: b}]\n"
	for _, tc := range []struct {
		name    string
		job     string
		replies []*genai.Content
		priced  bool
		status  api.RunStatus
		want    []string // in the run's error
	}{
		{name: "max_turns spans the steps", job: "limits: {max_turns: 3}",
			replies: []*genai.Content{list, text("a done"), list, list, text("b done")},
			status:  api.RunLimited, want: []string{"maximum turns reached (3 model calls)", "the job sets this in " + filepath.Join("workers", "flow", workers.FileName)}},
		{name: "max_cost_usd spans the steps", job: "limits: {max_cost_usd: 0.0002}", priced: true,
			replies: []*genai.Content{list, text("a done"), list, list, list, list, text("b done")},
			status:  api.RunLimited, want: []string{"cost limit", "the job sets this"}},
		{name: "the timeout ends it limited", job: "permissions: [\"shell:sleep 5\"]\nlimits: {timeout: 300ms}",
			replies: []*genai.Content{sleep, sleep},
			status:  api.RunLimited, want: []string{"time limit"}},
		{name: "within its limits", job: "limits: {max_turns: 5}",
			replies: []*genai.Content{list, text("a done"), list, text("b done")},
			status:  api.RunSucceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, llm := openTestWith(t, func(c *config.Config) { c.Workers.Policy.Allow = []string{"shell"} }, tc.replies...)
			if tc.priced {
				llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 10}
			}
			writeFlow(t, w, "two", twoSteps)
			addWorker(t, w, "flow", "---\nschedule: hourly\nworkflow: two\n"+tc.job+"\n---\n")
			enable(t, w, "flow")
			run, err := w.RunWorker(context.Background(), "flow", RunOptions{})
			require.NoError(t, err)
			assert.Equal(t, tc.status, run.Status, "run %+v", run)
			for _, s := range tc.want {
				assert.Contains(t, run.Error, s)
			}
			assert.NotContains(t, run.Error, "continue", "nobody to send continue to")
			assert.Less(t, run.Duration, 4*time.Second)
		})
	}

	t.Run("its model, and its changes are the run's", func(t *testing.T) {
		w, main := openTestWith(t, func(c *config.Config) { c.Workers.Policy.Allow = []string{"write"} })
		writeFlow(t, w, "one", "nodes:\n  - {id: a, type: agent, instructions: Write.}\n")
		addWorker(t, w, "flow", "---\nschedule: hourly\nworkflow: one\nmodel: writer\npermissions: [\"write:out/\"]\n---\n")
		enable(t, w, "flow")
		writer := runtime.NewMockLLM("writer", call("create_file", map[string]any{"path": "out/a.md", "content": "x\n"}), text("wrote it"))
		w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return writer, nil }
		run, err := w.RunWorker(context.Background(), "flow", RunOptions{})
		require.NoError(t, err)
		require.Equal(t, api.RunSucceeded, run.Status, "run %+v", run)
		assert.Zero(t, main.Calls(), "the steps ran on the job's model")
		assert.Equal(t, 2, writer.Calls())
		assert.Equal(t, []string{"out/a.md"}, run.Files)
		_, err = w.UndoWorkerRun(run.ID, false)
		require.NoError(t, err)
		assert.NoFileExists(t, filepath.Join(w.Dir(), "out", "a.md"))
	})
}

// A job's turn gets the guardrails of the agent it runs as, not of the
// window's active agent (N26 engine-top-2#5).
func TestRunWorkerGuardrailsOfItsAgent(t *testing.T) {
	list := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_files", Args: map[string]any{}}}}}
	for _, tc := range []struct {
		name, agent string
		status      api.RunStatus
	}{
		{name: "the guarded agent", agent: "qa", status: api.RunLimited},
		{name: "another agent", agent: "", status: api.RunSucceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := openTestWith(t, nil, list, list, text("done"))
			writeGuardrail(t, w, "limits", "---\nbuiltin: limits\nagents: [qa]\nlimits:\n  max_turns: 1\n---\n")
			job := "---\nschedule: hourly\n"
			if tc.agent != "" {
				job += "agent: " + tc.agent + "\n"
			}
			addWorker(t, w, "look", job+"---\nLook around.\n")
			enable(t, w, "look")
			run, err := w.RunWorker(context.Background(), "look", RunOptions{})
			require.NoError(t, err)
			assert.Equal(t, tc.status, run.Status, "run %+v", run)
		})
	}
}

// A worker made from a form goes in .agents/workers once it's valid, and
// lists as new (disabled); an invalid one writes nothing.
func TestCreateWorker(t *testing.T) {
	w := openTest(t)
	addWorker(t, w, "old", "---\nschedule: \"@daily\"\n---\nAn older worker, in workers/.\n")
	good := api.WorkerSpec{Name: "deps", Description: "Outdated modules", Schedule: "Daily at 6 AM", Permissions: []string{"shell:go list -m -u all"}, Prompt: "Report outdated modules."}

	info, problems, err := w.CreateWorker(good)
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, filepath.Join(w.Dir(), ".agents", "workers", "deps", workers.FileName), info.Path)
	assert.Equal(t, api.StateNew, info.State, "a new worker waits to be reviewed")
	assert.Equal(t, "0 6 * * *", info.Cron)
	list, err := w.ListWorkers()
	require.NoError(t, err)
	names := []string{}
	for _, x := range list {
		names = append(names, x.Name)
	}
	assert.Equal(t, []string{"deps", "old"}, names, "both places are read")

	for _, tc := range []struct {
		name    string
		spec    api.WorkerSpec
		problem string
		err     error
	}{
		{"taken", good, "", api.ErrWorkerExists},
		{"taken in workers/", api.WorkerSpec{Name: "old", Schedule: "@daily", Prompt: "x"}, "", api.ErrWorkerExists},
		{"bad name", api.WorkerSpec{Name: "My Worker", Schedule: "@daily", Prompt: "x"}, "lowercase", nil},
		{"bad schedule", api.WorkerSpec{Name: "a", Schedule: "whenever", Prompt: "x"}, "whenever", nil},
		{"no workflow", api.WorkerSpec{Name: "b", Schedule: "@daily"}, "workflow", nil},
		{"bad permission", api.WorkerSpec{Name: "c", Schedule: "@daily", Permissions: []string{"root"}, Prompt: "x"}, "kind:pattern", nil},
		{"bad timeout", api.WorkerSpec{Name: "d", Schedule: "@daily", Limits: api.Limits{TimeoutRaw: "soon"}, Prompt: "x"}, "timeout", nil},
		{"unknown agent", api.WorkerSpec{Name: "e", Schedule: "@daily", Agent: "ghost", Prompt: "x"}, "ghost", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, problems, err := w.CreateWorker(tc.spec)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, problems)
			assert.Contains(t, strings.Join(problems, "; "), tc.problem)
			_, statErr := os.Stat(filepath.Join(w.Dir(), ".agents", "workers", tc.spec.Name))
			assert.True(t, os.IsNotExist(statErr), "an invalid worker was written")
		})
	}
}

// [workers] notify hears of scheduled runs whose status it names, with the
// run record on stdin; manual runs and other statuses don't reach it
// (BL-WK-10).
func TestWorkerRunNotify(t *testing.T) {
	out := filepath.Join(t.TempDir(), "notified")
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Workers.Notify = `cat > "` + out + `"; echo "$BLITZ_WORKER $BLITZ_RUN_STATUS" >> "` + out + `"`
		c.Workers.NotifyOn = []string{"succeeded"}
	}, text("done"), text("again"))
	addWorker(t, w, "deps", "---\nschedule: daily at 6 AM\n---\nCheck.\n")
	enable(t, w, "deps")

	_, err := w.RunWorker(context.Background(), "deps", RunOptions{Manual: true})
	require.NoError(t, err)
	assert.NoFileExists(t, out, "a manual run notified")

	run, err := w.RunWorker(context.Background(), "deps", RunOptions{})
	require.NoError(t, err)
	b, err := os.ReadFile(out)
	require.NoError(t, err)
	record, status, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	var got api.Run
	require.NoError(t, json.Unmarshal([]byte(record), &got), "stdin %q", record)
	assert.Equal(t, run.ID, got.ID)
	assert.Equal(t, "deps succeeded", status)
	assert.NoError(t, w.DeleteSession(context.Background(), run.SessionID), "still held by the run's storage")

	// Not a status it names: nothing.
	require.NoError(t, os.Remove(out))
	w.cfg.Workers.NotifyOn = []string{"failed"}
	w.notifyRun(api.Run{Worker: "deps", Status: api.RunSucceeded})
	assert.NoFileExists(t, out)
}

// With workers turned off, none can be created, disabled or run.
func TestWorkersOffRefuseEverything(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Workers.Enabled = false })
	_, _, err := w.CreateWorker(api.WorkerSpec{Name: "a", Schedule: "@daily", Prompt: "x"})
	assert.ErrorIs(t, err, api.ErrWorkersDisabled)
	_, err = w.DisableWorker("a")
	assert.ErrorIs(t, err, api.ErrWorkersDisabled)
	_, err = w.RunWorker(context.Background(), "a", RunOptions{})
	assert.ErrorIs(t, err, api.ErrWorkersDisabled)
	_, _, _, err = w.DraftWorker(api.WorkerSpec{Name: "a", Schedule: "@daily", Prompt: "x"})
	assert.ErrorIs(t, err, api.ErrWorkersDisabled)
}

// A draft with a name that can't be a folder's is a problem to fix, not
// an error.
func TestDraftWorkerBadName(t *testing.T) {
	w := openTest(t)
	_, data, problems, err := w.DraftWorker(api.WorkerSpec{Name: " Not A Name ", Schedule: "@daily", Prompt: "x"})
	require.NoError(t, err)
	assert.Nil(t, data)
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], `name "Not A Name"`)
}

// With no worker folders configured, a new worker has nowhere to go.
// Editing a worker: its file as written comes back with its hash; a save
// rewrites it only at that hash and only valid, keeps the name, and
// suspends an enabled worker until it's enabled again.
func TestUpdateWorker(t *testing.T) {
	w := openTest(t)
	created, _, err := w.CreateWorker(api.WorkerSpec{Name: "deps", Schedule: "Daily at 6 AM", Permissions: []string{"shell:go list -m -u all"}, Limits: api.Limits{TimeoutRaw: "20m"}, Prompt: "Report outdated modules."})
	require.NoError(t, err)
	_, err = w.EnableWorker("deps", created.Hash)
	require.NoError(t, err)

	spec, hash, err := w.GetWorkerSpec("deps")
	require.NoError(t, err)
	assert.Equal(t, created.Hash, hash)
	assert.Equal(t, "Daily at 6 AM", spec.Schedule, "as written, not as understood")
	assert.Equal(t, "20m", spec.Limits.TimeoutRaw)
	assert.Equal(t, "Report outdated modules.", spec.Prompt)

	spec.Schedule, spec.Prompt = "Weekdays at 9:30", "Report outdated modules, grouped by severity."
	info, problems, err := w.UpdateWorker(spec, hash)
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, "30 9 * * 1-5", info.Cron)
	assert.Equal(t, api.StateChanged, info.State, "an edit suspends the worker until it's enabled again")
	assert.NotEqual(t, hash, info.Hash)

	_, _, err = w.UpdateWorker(spec, hash)
	assert.ErrorIs(t, err, api.ErrHashMismatch, "a stale hash: the file changed since it was read")

	bad := spec
	bad.Schedule = "whenever"
	_, problems, err = w.UpdateWorker(bad, info.Hash)
	require.NoError(t, err)
	assert.NotEmpty(t, problems)
	again, _, err := w.GetWorkerSpec("deps")
	require.NoError(t, err)
	assert.Equal(t, "Weekdays at 9:30", again.Schedule, "nothing written")

	_, _, err = w.UpdateWorker(api.WorkerSpec{Name: "ghost", Schedule: "@daily", Prompt: "x"}, "h")
	assert.ErrorIs(t, err, api.ErrUnknownWorker)

	// A file that doesn't even parse can be read into the form and fixed.
	addWorker(t, w, "broken", "no frontmatter at all\n")
	broken, brokenHash, err := w.GetWorkerSpec("broken")
	require.NoError(t, err)
	assert.Equal(t, "no frontmatter at all", broken.Prompt)
	broken.Schedule = "@daily"
	fixed, problems, err := w.UpdateWorker(broken, brokenHash)
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, api.StateNew, fixed.State)
	assert.Equal(t, filepath.Join(w.Dir(), "workers", "broken", workers.FileName), fixed.Path, "edited where it is")
	entries, err := os.ReadDir(filepath.Join(w.Dir(), "workers"))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file left behind")
}

func TestCreateWorkerNeedsAFolder(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Workers.Paths = nil })
	_, _, err := w.CreateWorker(api.WorkerSpec{Name: "a", Schedule: "@daily", Prompt: "x"})
	assert.ErrorIs(t, err, api.ErrWorkersDisabled)
	assert.ErrorContains(t, err, "workers.paths is empty")
}

// A worker folder that can't be read is a warning; the others are still
// listed.
func TestUnreadableWorkerFolderWarns(t *testing.T) {
	w := openTest(t)
	var warnings []string
	w.warn = func(s string) { warnings = append(warnings, s) }
	write(t, w.Dir(), "workers", "not a folder")
	list, err := w.ListWorkers()
	require.NoError(t, err)
	assert.Empty(t, list)
	assert.NotEmpty(t, warnings)
}

// A scheduled run while the worker is still running is skipped and
// recorded; a manual one is refused without a record.
func TestRunWorkerSkipsWhileRunning(t *testing.T) {
	w := openTest(t)
	addWorker(t, w, "deps", "---\nschedule: daily at 6 AM\n---\nCheck.\n")
	enable(t, w, "deps")
	w.runsMu.Lock()
	w.running["deps"] = true
	w.runsMu.Unlock()

	run, err := w.RunWorker(context.Background(), "deps", RunOptions{})
	assert.ErrorIs(t, err, api.ErrRunInProgress)
	assert.Equal(t, api.RunSkipped, run.Status)
	_, err = w.RunWorker(context.Background(), "deps", RunOptions{Manual: true})
	assert.ErrorIs(t, err, api.ErrRunInProgress)
	runs, err := w.WorkerRuns("deps", 10)
	require.NoError(t, err)
	require.Len(t, runs, 1, "only the scheduled run is recorded")
	assert.Equal(t, run.ID, runs[0].ID)
}

// A run whose session can't be made is a failed run, recorded and told of.
func TestRunWorkerWithoutSession(t *testing.T) {
	out := filepath.Join(t.TempDir(), "notified")
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Workers.Notify = `echo "$BLITZ_RUN_STATUS" > "` + out + `"`
		c.Workers.NotifyOn = []string{"failed"}
	})
	addWorker(t, w, "deps", "---\nschedule: daily at 6 AM\n---\nCheck.\n")
	enable(t, w, "deps")
	notDir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notDir, nil, 0o644))
	w.cfg.Session.StorageDir = filepath.Join(notDir, "sessions")

	run, err := w.RunWorker(context.Background(), "deps", RunOptions{})
	require.Error(t, err)
	assert.Equal(t, api.RunFailed, run.Status)
	assert.Equal(t, err.Error(), run.Error)
	runs, err := w.WorkerRuns("deps", 10)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, api.RunFailed, runs[0].Status)
	b, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "failed\n", string(b))
}

// A notify command that fails is a warning naming the worker.
func TestWorkerNotifyFailureWarns(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Workers.Notify = "echo nope; exit 3"
		c.Workers.NotifyOn = []string{"succeeded"}
	})
	var warnings []string
	w.warn = func(s string) { warnings = append(warnings, s) }
	w.notifyRun(api.Run{Worker: "deps", Status: api.RunSucceeded})
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "workers.notify for deps")
	assert.Contains(t, warnings[0], "nope")
}

// A job asked for in the chat: the agent's create_job is asked about and
// makes a paused job in the workspace, as the New Job dialog does (WK-70).
func TestCreateJobFromTheChat(t *testing.T) {
	call := toolCall("create_job", map[string]any{
		"name":        "ai-news",
		"schedule":    "every day at 8 am",
		"prompt":      "Fetch the latest AI news from news.google.com and write a summary report to reports/ai-news/MM-DD-YYYY.html, glass morphic with a dark theme.",
		"permissions": []any{"web:news.google.com", "write:reports/ai-news/**"},
	})
	w, _ := openTestWith(t, nil, call, text("Created ai-news, paused: review and enable it."))
	var asked []api.ApprovalRequest
	w.SetUI(func(_ context.Context, r api.ApprovalRequest) (api.Decision, error) {
		asked = append(asked, r)
		return api.DecisionOnce, nil
	}, nil)
	s, _ := w.NewSession()
	var result map[string]any
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "Create a job that runs every day at 8 am to fetch the AI news"}, func(e api.Event) {
		if e.ToolResult != nil && e.ToolResult.Name == "create_job" {
			result = e.ToolResult.Result
		}
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result["error"], "result %v", result)
	assert.Equal(t, true, result["paused"])
	require.Len(t, asked, 1)
	assert.Equal(t, "create_job", asked[0].Tool)
	assert.Contains(t, asked[0].Diff, "news.google.com")

	jobs, err := w.ListWorkers()
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, "ai-news", jobs[0].Name)
	assert.Equal(t, api.StateNew, jobs[0].State, "paused until enabled")
	assert.Equal(t, "0 8 * * *", jobs[0].Cron)
	assert.ElementsMatch(t, []string{"web:news.google.com", "write:reports/ai-news/**"}, jobs[0].Permissions)
}

func TestPreviewSchedule(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name, text, tz string
		n              int
		cron           string
		first          string
		count          int
		err            bool
	}{
		{name: "plain text", text: "every day at 8 am", tz: "UTC", n: 3, cron: "0 8 * * *", first: "2026-10-24T08:00:00Z", count: 3},
		{name: "cron", text: "30 9 * * 1-5", tz: "UTC", n: 2, cron: "30 9 * * 1-5", first: "2026-10-23T09:30:00Z", count: 2},
		{name: "in a time zone", text: "daily at 6 AM", tz: "America/Chicago", n: 1, cron: "0 6 * * *", first: "2026-10-23T06:00:00-05:00", count: 1},
		{name: "at most twenty", text: "@hourly", tz: "UTC", n: 99, cron: "@hourly", first: "2026-10-23T10:00:00Z", count: 20},
		{name: "unreadable", text: "someday soon", tz: "UTC", n: 3, err: true},
		{name: "unknown zone", text: "@daily", tz: "Mars/Olympus", n: 3, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := PreviewSchedule(tt.text, tt.tz, tt.n, now)
			if tt.err {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.cron, p.Cron)
			require.Len(t, p.Next, tt.count)
			assert.Equal(t, tt.first, p.Next[0].Format(time.RFC3339))
		})
	}
}
