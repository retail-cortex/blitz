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
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// TaskRunner runs background tasks: sub-agents invoke_agent starts with
// background: true (spec_background_agents_032). The engine supplies it;
// every call names the sessions whose tasks it may see.
type TaskRunner interface {
	StartTask(ctx context.Context, agent, prompt string) (api.TaskInfo, error)
	ListTasks(sessions []string) []api.TaskInfo
	// WaitTask returns the task with its latest events, once it has
	// finished or wait has passed (0: at once).
	WaitTask(ctx context.Context, sessions []string, id string, wait time.Duration) (api.TaskInfo, []string, error)
	StopTask(sessions []string, id string) (api.TaskInfo, error)
}

// maxTaskWait bounds task_output's wait_seconds.
const maxTaskWait = 300 * time.Second

// TaskSummary is a task as the agent's task tools report it.
type TaskSummary struct {
	ID             string  `json:"id"`
	Agent          string  `json:"agent"`
	State          string  `json:"state"`
	Prompt         string  `json:"prompt"`
	RuntimeSeconds int64   `json:"runtime_seconds"`
	CostUSD        float64 `json:"cost_usd"`
	Result         string  `json:"result,omitempty"`
	Error          string  `json:"error,omitempty"`
}

func taskSummary(t api.TaskInfo, result bool) TaskSummary {
	s := TaskSummary{ID: t.ID, Agent: t.Agent, State: t.State, Prompt: t.Prompt, RuntimeSeconds: int64(t.Runtime().Seconds()), CostUSD: t.Usage.CostUSD, Error: t.Error}
	if result {
		s.Result = t.Result
	} else if first, _, _ := strings.Cut(t.Result, "\n"); first != "" {
		s.Result = first
	}
	return s
}

// ListTasksOutput lists this session's tasks.
type ListTasksOutput struct {
	Tasks []TaskSummary `json:"tasks"`
	Error string        `json:"error,omitempty"`
}

// TaskOutputInput names a task, and how long to wait for it.
type TaskOutputInput struct {
	TaskID      string `json:"task_id" jsonschema:"The task's ID, from invoke_agent or list_tasks"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"Wait up to this many seconds (at most 300) for the task to finish; 0 returns at once"`
}

// TaskOutputOutput is a task, its result once finished, and its latest
// events while it runs.
type TaskOutputOutput struct {
	TaskSummary
	RecentEvents []string `json:"recent_events,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// StopTaskInput names the task to stop.
type StopTaskInput struct {
	TaskID string `json:"task_id" jsonschema:"The task's ID"`
}

// StopTaskOutput is the task once stopped.
type StopTaskOutput struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

var errNoTasks = errors.New("background tasks are not available in this session")

// NewTaskTools creates list_tasks, task_output and stop_task: the calling
// session's background tasks.
func NewTaskTools(hooks *Hooks) ([]tool.Tool, error) {
	list, err := functiontool.New(
		functiontool.Config{Name: "list_tasks", Description: "List this session's background tasks (invoke_agent with background: true): state, runtime, cost, the first line of each result"},
		func(ctx agent.Context, _ struct{}) (ListTasksOutput, error) {
			r := hooks.taskRunner()
			if r == nil {
				return ListTasksOutput{Error: errNoTasks.Error()}, nil
			}
			out := ListTasksOutput{Tasks: []TaskSummary{}}
			for _, t := range r.ListTasks([]string{sessionOf(ctx)}) {
				out.Tasks = append(out.Tasks, taskSummary(t, false))
			}
			return out, nil
		})
	if err != nil {
		return nil, err
	}
	output, err := functiontool.New(
		functiontool.Config{Name: "task_output", Description: "Read a background task: its result once finished, else its latest events; optionally wait for it to finish"},
		func(ctx agent.Context, in TaskOutputInput) (TaskOutputOutput, error) {
			r := hooks.taskRunner()
			if r == nil {
				return TaskOutputOutput{Error: errNoTasks.Error()}, nil
			}
			wait := min(time.Duration(max(in.WaitSeconds, 0))*time.Second, maxTaskWait)
			t, events, err := r.WaitTask(ctx, []string{sessionOf(ctx)}, in.TaskID, wait)
			if err != nil {
				return TaskOutputOutput{Error: err.Error()}, nil
			}
			out := TaskOutputOutput{TaskSummary: taskSummary(t, true)}
			if t.State == api.TaskRunning {
				out.RecentEvents = events[max(0, len(events)-20):]
			}
			return out, nil
		})
	if err != nil {
		return nil, err
	}
	stop, err := functiontool.New(
		functiontool.Config{Name: "stop_task", Description: "Stop a background task"},
		func(ctx agent.Context, in StopTaskInput) (StopTaskOutput, error) {
			r := hooks.taskRunner()
			if r == nil {
				return StopTaskOutput{Error: errNoTasks.Error()}, nil
			}
			t, err := r.StopTask([]string{sessionOf(ctx)}, in.TaskID)
			if err != nil {
				return StopTaskOutput{ID: in.TaskID, Error: err.Error()}, nil
			}
			return StopTaskOutput{ID: t.ID, State: t.State}, nil
		})
	if err != nil {
		return nil, err
	}
	return []tool.Tool{list, output, stop}, nil
}
