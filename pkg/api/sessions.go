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

// SessionInfo describes a saved session.
type SessionInfo struct {
	ID    string
	Title string // "" until the first prompt names it
	Agent string
	// Workspace is the directory the session belongs to ("" for sessions
	// saved before sessions were scoped to one).
	Workspace string
	// Snapshot is the name of a snapshot saved with SaveSnapshot ("" for an
	// ordinary session).
	Snapshot string
	// From is the session this one was copied from: the saved session for a
	// snapshot, the snapshot for a session started from one.
	From string
	// MovedFrom is the workspace /cd moved the session from, and MovedAt
	// the number of messages it had then ("" and 0 if it never moved).
	MovedFrom    string
	MovedAt      int
	MessageCount int
	Created      time.Time
	Updated      time.Time
	// Messages are filled in for the active session and for sessions
	// opened by OpenSession or LoadSession; lists leave them out.
	Messages []Message
}

// Message is one message in a session's transcript.
type Message struct {
	Role string // "user" or "model"
	Text string
	Time time.Time
	// Kind tells user messages apart: "" for a prompt (a rewind point),
	// "steer", "hook" (a stop hook's request) or "plan" (an approved
	// plan's go-ahead); and "task" for a background task that ended.
	Kind string
}

// ErrNoActiveSession reports an operation on the active session when there
// is none.
var ErrNoActiveSession = errors.New("no active session")

// ErrSnapshotNameTaken is returned by Snapshot when another snapshot has the name.
var ErrSnapshotNameTaken = errors.New("a snapshot with this name already exists")

// ErrSessionOpen is returned when deleting a session that is open: the
// active one, or one a background run uses.
var ErrSessionOpen = errors.New("the session is open: switch to another first")

// ErrSessionNotFound reports a session ID with no saved session.
var ErrSessionNotFound = errors.New("no such session")

// ResumeError reports a session that can't be resumed: none is saved for
// this workspace, or no session has the given ID or snapshot name.
type ResumeError struct{ Err error }

// Error is why the session couldn't be resumed.
func (e *ResumeError) Error() string { return e.Err.Error() }

// Unwrap returns that reason, for errors.Is and errors.As.
func (e *ResumeError) Unwrap() error { return e.Err }

// ErrWorkspaceBusy reports a workspace another process (or another
// Workspace in this one) has open. Only one may own a workspace at a time:
// they would otherwise write the same sessions and checkpoints. The lock
// goes with the process, so a crash never leaves a workspace locked.
var ErrWorkspaceBusy = errors.New("the workspace is open elsewhere")

// BackgroundRun is a turn the service runs with no client attached (blitz
// --bg).
type BackgroundRun struct {
	ID        string // bg-1, bg-2, …
	Workspace string
	SessionID string
	Prompt    string // its first line
	// State is running, waiting (for an answer), done, failed or stopped.
	State   string
	Started time.Time
	Ended   time.Time // zero while it runs
	CostUSD float64
	Waiting int // requests waiting for an answer
	Error   string
}

// Active reports whether the run hasn't ended.
func (r BackgroundRun) Active() bool { return r.State == "running" || r.State == "waiting" }

// ErrUnknownRun means the service has no background run with that ID.
var ErrUnknownRun = errors.New("no such background run")
