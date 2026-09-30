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
	// Worktree and Branch: it ran in its own git worktree, on this branch,
	// for its changes to be reviewed and merged.
	Worktree string `json:"worktree,omitempty"`
	Branch   string `json:"branch,omitempty"`
}

// Active reports whether it hasn't ended: running, or waiting for an
// answer.
func (t TaskInfo) Active() bool { return t.State == TaskRunning || t.State == TaskWaiting }

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
	TaskWaiting = "waiting" // for an answer to its approval request or question
	TaskDone    = "done"
	TaskFailed  = "failed"
	TaskStopped = "stopped"
)

// ErrUnknownTask: no task with that ID was started in the sessions asking.
var ErrUnknownTask = errors.New("no such task")

// TaskRequest is a background task's approval request or question,
// waiting for one of its session's people to answer
// (spec_background_agents_032 BGA-41).
type TaskRequest struct {
	ID      string // answered with AnswerTaskRequest
	TaskID  string
	Agent   string
	Session string
	// Approval, or else Question and its suggested Options.
	Approval *ApprovalRequest
	Question string
	Options  []string
	// MultiSelect: several options may be chosen (one per line).
	MultiSelect bool
}

// Label names who asks: "qa · task-3".
func (r TaskRequest) Label() string { return r.Agent + " · " + r.TaskID }

// SessionEvent is something that happened to a session's background
// tasks outside its turns: one of Task (started, waiting, running again,
// ended), Request (waiting for an answer), or Resolved (the ID of a
// request that no longer waits: answered, or its task ended).
type SessionEvent struct {
	Task     *TaskInfo
	Request  *TaskRequest
	Resolved string
}

// ErrUnknownRequest: no request with that ID is waiting.
var ErrUnknownRequest = errors.New("no such request is waiting")

// ErrNoNote is returned by ForgetNote when no note has the name.
var ErrNoNote = errors.New("no such note")

// Note is something the agent remembered across sessions of a workspace
// with its remember tool: a fact, a preference or a correction.
type Note struct {
	Name string // to show or forget it
	Kind string
	Text string
	Time time.Time
	// Path is its file, to edit (on the machine that keeps it).
	Path string
}

// HookInfo is a configured hook, for /hooks.
type HookInfo struct {
	Event string
	// Type is command, http or prompt.
	Type  string
	Match string
	If    string
	// Runs is what it runs: the command, POST <url> or prompt: <text>.
	Runs string
	// Source is the project file it came from ("": the user's settings).
	Source     string
	FailClosed bool
	// Failures are its latest failed runs since the workspace opened.
	Failures []HookFailure
}

// HookFailure is one failed run of a hook.
type HookFailure struct {
	Time  time.Time
	Error string
}

// Goal is what a session works toward until a judge says it holds (/goal).
type Goal struct {
	Condition string
	// Continues is how often the agent was sent on; Max the limit.
	Continues, Max int
	// Last is the judge's latest reason.
	Last string
}

// ErrNoGoal is returned when the session has no goal.
var ErrNoGoal = errors.New("no goal set")
