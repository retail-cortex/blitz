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
