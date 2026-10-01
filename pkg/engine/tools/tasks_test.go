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

package tools

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTasks is a TaskRunner over fixed tasks that records what it's asked.
type fakeTasks struct {
	tasks    map[string]api.TaskInfo
	events   []string
	sessions [][]string
	waited   time.Duration
	started  []string // agent, prompt, isolation
	startErr error
}

func (f *fakeTasks) StartTask(_ context.Context, agent, prompt, isolation string) (api.TaskInfo, error) {
	f.started = []string{agent, prompt, isolation}
	if f.startErr != nil {
		return api.TaskInfo{}, f.startErr
	}
	t := api.TaskInfo{ID: "task-9", Agent: agent, State: api.TaskRunning}
	if isolation == "worktree" {
		t.Worktree, t.Branch = "/tmp/wt", "blitz/task-9"
	}
	return t, nil
}

func (f *fakeTasks) ListTasks(sessions []string) []api.TaskInfo {
	f.sessions = append(f.sessions, sessions)
	var out []api.TaskInfo
	for _, id := range []string{"task-1", "task-2"} {
		out = append(out, f.tasks[id])
	}
	return out
}

func (f *fakeTasks) WaitTask(_ context.Context, sessions []string, id string, wait time.Duration) (api.TaskInfo, []string, error) {
	f.sessions = append(f.sessions, sessions)
	f.waited = wait
	t, ok := f.tasks[id]
	if !ok {
		return api.TaskInfo{}, nil, fmt.Errorf("no task %s", id)
	}
	return t, f.events, nil
}

func (f *fakeTasks) StopTask(sessions []string, id string) (api.TaskInfo, error) {
	f.sessions = append(f.sessions, sessions)
	t, ok := f.tasks[id]
	if !ok {
		return api.TaskInfo{}, errors.New("no such task")
	}
	t.State = api.TaskStopped
	return t, nil
}

// taskTools returns list_tasks, task_output and stop_task over hooks.
func taskTools(t *testing.T, hooks *Hooks) (list, output, stop runnerTool) {
	t.Helper()
	tl, err := NewTaskTools(hooks)
	require.NoError(t, err)
	require.Len(t, tl, 3)
	return tl[0].(runnerTool), tl[1].(runnerTool), tl[2].(runnerTool)
}

// Without a task runner each task tool says tasks aren't available.
func TestTaskToolsUnavailable(t *testing.T) {
	list, output, stop := taskTools(t, NewHooks(Policy{}))
	for name, call := range map[string]func() map[string]any{
		"list_tasks":  func() map[string]any { return runTool(t, list, map[string]any{}) },
		"task_output": func() map[string]any { return runTool(t, output, map[string]any{"task_id": "task-1"}) },
		"stop_task":   func() map[string]any { return runTool(t, stop, map[string]any{"task_id": "task-1"}) },
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, errNoTasks.Error(), errOf(call()))
		})
	}
}

// list_tasks gives each task's first result line; task_output the whole
// result, or a running task's latest 20 events, with the wait bounded;
// stop_task stops one. Each asks for the calling session's tasks only.
func TestTaskTools(t *testing.T) {
	started := time.Now().Add(-90 * time.Second)
	runner := &fakeTasks{tasks: map[string]api.TaskInfo{
		"task-1": {ID: "task-1", Agent: "qa", State: api.TaskDone, Prompt: "review", Started: started, Ended: started.Add(time.Minute), Result: "All good.\nDetails follow.", Usage: api.Usage{CostUSD: 0.25}},
		"task-2": {ID: "task-2", Agent: "helios", State: api.TaskRunning, Started: started},
	}}
	for i := range 25 {
		runner.events = append(runner.events, fmt.Sprintf("event %d", i))
	}
	hooks := NewHooks(Policy{})
	hooks.SetTaskRunner(runner)
	list, output, stop := taskTools(t, hooks)
	ctx := createTestToolContextWith(WithOwnerSession(context.Background(), "s1"))

	out, err := list.Run(ctx, map[string]any{})
	require.NoError(t, err)
	tasks := out["tasks"].([]any)
	require.Len(t, tasks, 2)
	first := tasks[0].(map[string]any)
	assert.Equal(t, "All good.", first["result"], "the list shows the first line")
	assert.EqualValues(t, 60, first["runtime_seconds"])
	assert.EqualValues(t, 0.25, first["cost_usd"])

	tests := []struct {
		name       string
		args       map[string]any
		wantWait   time.Duration
		wantResult string
		wantEvents int
		wantErr    string
	}{
		{"finished", map[string]any{"task_id": "task-1"}, 0, "All good.\nDetails follow.", 0, ""},
		{"running", map[string]any{"task_id": "task-2", "wait_seconds": 5}, 5 * time.Second, "", 20, ""},
		{"wait bounded", map[string]any{"task_id": "task-2", "wait_seconds": 9999}, maxTaskWait, "", 20, ""},
		{"negative wait", map[string]any{"task_id": "task-2", "wait_seconds": -3}, 0, "", 20, ""},
		{"unknown", map[string]any{"task_id": "task-7"}, 0, "", 0, "no task task-7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := output.Run(ctx, tt.args)
			require.NoError(t, err)
			assert.Equal(t, tt.wantWait, runner.waited)
			if tt.wantErr != "" {
				assert.Equal(t, tt.wantErr, errOf(out))
				return
			}
			result, _ := out["result"].(string)
			assert.Equal(t, tt.wantResult, result)
			events, _ := out["recent_events"].([]any)
			assert.Len(t, events, tt.wantEvents)
			if tt.wantEvents > 0 {
				assert.Equal(t, "event 24", events[len(events)-1], "the latest events")
			}
		})
	}

	out, err = stop.Run(ctx, map[string]any{"task_id": "task-2"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"id": "task-2", "state": api.TaskStopped}, out)
	out, err = stop.Run(ctx, map[string]any{"task_id": "task-7"})
	require.NoError(t, err)
	assert.Equal(t, "task-7", out["id"])
	assert.Equal(t, "no such task", errOf(out))

	for _, s := range runner.sessions {
		assert.Equal(t, []string{"s1"}, s, "a call asked for another session's tasks")
	}
}
