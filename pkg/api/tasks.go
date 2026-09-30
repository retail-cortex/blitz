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

package api

import (
	"errors"
	"time"
)

// TaskInfo is a background task: a sub-agent that invoke_agent started
// with background: true, running beside the turn that started it
// (spec_background_agents_032).
type TaskInfo struct {
	ID    string `json:"id"` // task-1, task-2, … in the workspace
	Agent string `json:"agent"`
	// Prompt is the first line of what it was asked.
	Prompt string `json:"prompt"`
	// Session is the session whose turn started it.
	Session string `json:"session"`
	// State is running, done, failed or stopped.
	State   string    `json:"state"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitzero"` // zero while it runs
	// Result is what it answered, when done.
	Result string `json:"result,omitempty"`
	// Error is why it failed or stopped.
	Error string `json:"error,omitempty"`
	// Usage is its own tokens and cost (also counted in its session's).
	Usage Usage `json:"usage"`
}

// Runtime is how long it has run, or ran.
func (t TaskInfo) Runtime() time.Duration {
	end := t.Ended
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(t.Started)
}

// The states of a task.
const (
	TaskRunning = "running"
	TaskDone    = "done"
	TaskFailed  = "failed"
	TaskStopped = "stopped"
)

// ErrUnknownTask: no task with that ID was started in the sessions asking.
var ErrUnknownTask = errors.New("no such task")
