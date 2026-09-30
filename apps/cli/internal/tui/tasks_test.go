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

package tui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// taskBackend is a workspace with background tasks and nothing else.
type taskBackend struct {
	api.Backend
	mu    sync.Mutex
	tasks []api.TaskInfo
}

func (b *taskBackend) ListTasks() []api.TaskInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]api.TaskInfo(nil), b.tasks...)
}

func (b *taskBackend) Task(id string) (api.TaskInfo, []string, error) {
	for _, t := range b.ListTasks() {
		if t.ID == id {
			return t, []string{"→ run_shell_command {\"command\":\"go test ./...\"}"}, nil
		}
	}
	return api.TaskInfo{}, nil, api.ErrUnknownTask
}

func (b *taskBackend) StopTask(id string) (api.TaskInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, t := range b.tasks {
		if t.ID == id {
			b.tasks[i].State, b.tasks[i].Ended = api.TaskStopped, time.Now()
			return b.tasks[i], nil
		}
	}
	return api.TaskInfo{}, api.ErrUnknownTask
}

func (b *taskBackend) end(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == id {
			b.tasks[i].State, b.tasks[i].Result, b.tasks[i].Ended = api.TaskDone, "all green\nnothing to fix", time.Now()
		}
	}
}

func running(id string) api.TaskInfo {
	return api.TaskInfo{ID: id, Agent: "qa", Prompt: "run the tests", State: api.TaskRunning, Started: time.Now().Add(-90 * time.Second)}
}

func TestTaskLine(t *testing.T) {
	task := running("task-3")
	task.Usage.CostUSD = 0.021
	assert.Equal(t, "[task-3] qa · running · 1m30s · $0.02 — run the tests", TaskLine(task))
}

func TestTasksCommandAndNotices(t *testing.T) {
	b := &taskBackend{tasks: []api.TaskInfo{running("task-1")}}
	app := &App{Workspace: b}
	out := captureStdout(t, func() { cmdTasks(nil, app) })
	assert.Contains(t, out, "[task-1] qa · running")
	out = captureStdout(t, func() { cmdTasks([]string{"show", "task-1"}, app) })
	assert.Contains(t, out, "go test ./...")
	out = captureStdout(t, func() { cmdTasks([]string{"show", "task-9"}, app) })
	assert.Contains(t, out, "no such task")
	out = captureStdout(t, func() { cmdTasks([]string{"what"}, app) })
	assert.Contains(t, out, "/tasks show <id>")

	assert.Empty(t, captureStdout(t, func() { announceTasks(app) }), "a running task announced")
	b.end("task-1")
	out = captureStdout(t, func() { announceTasks(app) })
	assert.Contains(t, out, "Background task-1 (qa) done")
	assert.Contains(t, out, "all green")
	assert.Empty(t, captureStdout(t, func() { announceTasks(app) }), "announced twice")

	b.tasks = append(b.tasks, running("task-2"))
	out = captureStdout(t, func() { cmdTasks([]string{"stop", "task-2"}, app) })
	assert.Contains(t, out, "[task-2] qa · stopped")
}

func TestExitAccountsForTasks(t *testing.T) {
	cases := []struct {
		name    string
		answer  string
		finish  bool // the task ends while waited for
		exits   bool
		stopped bool
	}{
		{name: "kill", answer: "k\n", exits: true, stopped: true},
		{name: "wait", answer: "w\n", finish: true, exits: true},
		{name: "cancel", answer: "c\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			old := taskPoll
			taskPoll = 10 * time.Millisecond
			t.Cleanup(func() { taskPoll = old })
			b := &taskBackend{tasks: []api.TaskInfo{running("task-1")}}
			if c.finish {
				go func() { time.Sleep(50 * time.Millisecond); b.end("task-1") }()
			}
			var exits bool
			out := captureStdout(t, func() {
				exits = ConfirmExit(context.Background(), input(c.answer), nil, nil, ExitPrompt{CanPrompt: true, AllowCancel: true, Tasks: b})
			})
			assert.Equal(t, c.exits, exits)
			assert.Contains(t, out, "1 background task still running")
			tasks := b.ListTasks()
			require.Len(t, tasks, 1)
			assert.Equal(t, c.stopped, tasks[0].State == api.TaskStopped)
		})
	}
	assert.True(t, ConfirmExit(context.Background(), input(""), nil, nil, ExitPrompt{Tasks: &taskBackend{}}), "nothing running: exits at once")
}

// requestBackend has background tasks' requests waiting, and records the
// answers.
type requestBackend struct {
	taskBackend
	pending []api.TaskRequest
	answers map[string]string
}

func (b *requestBackend) PendingTaskRequests() []api.TaskRequest { return b.pending }

func (b *requestBackend) AnswerTaskRequest(id string, d api.Decision, answer string) error {
	if answer == "" {
		answer = d.String()
	}
	b.answers[id] = answer
	return nil
}

func TestAnswerTasksAtThePrompt(t *testing.T) {
	b := &requestBackend{answers: map[string]string{}, pending: []api.TaskRequest{
		{ID: "task-1-0001", TaskID: "task-1", Agent: "qa", Approval: &api.ApprovalRequest{Tool: "run_shell_command", Kind: api.ActionCommand, Detail: "go test ./..."}},
		{ID: "task-2-0002", TaskID: "task-2", Agent: "docs", Question: "Tabs or spaces?", Options: []string{"tabs", "spaces"}},
	}}
	app := &App{Workspace: b, Input: input("y\ntabs\n")}
	out := captureStdout(t, func() { answerTasks(context.Background(), app) })
	assert.Contains(t, out, "qa · task-1 asks, from the background")
	assert.Contains(t, out, "docs · task-2 asks")
	assert.Equal(t, "tabs", b.answers["task-2-0002"])
	assert.NotEqual(t, api.DecisionDeny.String(), b.answers["task-1-0001"], "approved: %v", b.answers)
}
