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

// WorkerInfo describes a worker and whether it may run.
type WorkerInfo struct {
	Workspace   string
	Name        string
	Description string
	Path        string // WORKER.md
	Hash        string
	State       State
	// Schedule as written, as understood, and when it next runs (zero
	// unless it's enabled and valid).
	Schedule string
	Cron     string
	Timezone string
	Next     time.Time
	Agent    string
	Model    string
	// CatchUp is "once" when a run missed while nothing was running should
	// happen as soon as possible, otherwise "none".
	CatchUp string
	// Permissions and Limits are what the worker gets after the host
	// policy.
	Permissions []string
	Limits      Limits
	// Problems say why the worker is invalid, or what the policy changed.
	Problems []string
}

// ErrUnknownWorker reports a worker name the workspace doesn't define.
var ErrUnknownWorker = errors.New("no such worker")

// ErrWorkersDisabled reports that workers are turned off ([workers] enabled).
var ErrWorkersDisabled = errors.New("workers are disabled")

// ErrWorkerNotEnabled reports running a worker that isn't enabled at its
// current hash.
var ErrWorkerNotEnabled = errors.New("the worker isn't enabled")

// ErrRunInProgress reports a run of a worker that is already running.
var ErrRunInProgress = errors.New("the worker is already running")

// RunStatus is where a run stands.
type RunStatus string

// Where a run stands. Running is while it runs; the others are how it ended.
const (
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	// RunLimited: stopped at a limit (turns, cost or time).
	RunLimited RunStatus = "limited"
	// RunSkipped: not started, because the previous run was still going.
	RunSkipped RunStatus = "skipped"
)

// Refusal is an action a run wasn't permitted.
type Refusal struct {
	Tool   string     `json:"tool"`
	Kind   ActionKind `json:"kind"`
	Detail string     `json:"detail"`
	Time   time.Time  `json:"time"`
}

// Run is one run of a worker.
type Run struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	Worker    string    `json:"worker"`
	Hash      string    `json:"hash"`
	Status    RunStatus `json:"status"`
	// Manual: started on request, not by the schedule.
	Manual    bool          `json:"manual,omitempty"`
	Started   time.Time     `json:"started"`
	Duration  time.Duration `json:"duration"`
	CostUSD   float64       `json:"cost_usd"`
	Calls     int           `json:"calls"`
	SessionID string        `json:"session_id,omitempty"`
	Refusals  []Refusal     `json:"refusals,omitempty"`
	// Files are the files the run changed, which blitz workers undo
	// restores.
	Files []string `json:"files,omitempty"`
	// Error is why the run failed or stopped.
	Error string `json:"error,omitempty"`
}

// State is whether a worker may run.
type State string

const (
	// StateNew: found but never enabled.
	StateNew State = "new"
	// StateEnabled: enabled at its current hash; it runs on schedule.
	StateEnabled State = "enabled"
	// StateDisabled: turned off.
	StateDisabled State = "disabled"
	// StateChanged: enabled at an older hash; the files changed since, so
	// it doesn't run until re-enabled.
	StateChanged State = "changed"
	// StateInvalid: WORKER.md can't be used.
	StateInvalid State = "invalid"
)

// WorkerSpec is a new worker as a form describes it: WORKER.md's
// frontmatter fields and its workflow (Prompt). Empty fields are left out
// of the file.
type WorkerSpec struct {
	Name, Description  string
	Schedule, Timezone string
	Agent, Model       string
	Permissions        []string // "kind:pattern"
	Limits             Limits   // TimeoutRaw, not Timeout
	CatchUp            string   // "", "none" or "once"
	Prompt             string
}

// ErrWorkerExists reports creating a worker with a name the workspace
// already has.
var ErrWorkerExists = errors.New("a worker with that name exists")

// ErrHashMismatch reports enabling a worker at a hash other than its
// current one: the files changed after they were reviewed.
var ErrHashMismatch = errors.New("the worker changed since it was reviewed")

// Limits bound one run. Every run has them: the host policy fills in what
// a worker leaves out.
type Limits struct {
	MaxTurns   int           `yaml:"max_turns"`
	MaxCostUSD float64       `yaml:"max_cost_usd"`
	Timeout    time.Duration `yaml:"-"`
	TimeoutRaw string        `yaml:"timeout"`
}
