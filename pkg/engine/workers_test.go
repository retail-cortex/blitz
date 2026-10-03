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
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// Its change is its own: not the sessions' /undo, but the run's undo
	// (BL-WK-01, BL-WK-02).
	_, err = w.Undo(false)
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
