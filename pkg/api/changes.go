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

// Checkpoint is the files one turn changed, which Undo restores.
type Checkpoint struct {
	ID    int
	Label string // the prompt that started the turn, shortened
	Time  time.Time
	Files []string
}

// GitStatus is the workspace's git state for the status bar: its branch
// (or short commit, detached) and how many files differ from HEAD,
// untracked ones included. Git is false when git isn't on the service's
// PATH, Repo outside a repository (a repository with no commit yet is
// one). Git runs read-only, with no filters or fsmonitor.
type GitStatus struct {
	Git     bool
	Repo    bool
	Branch  string
	Changed int
}

// UndoResult is what Undo restored.
type UndoResult struct {
	Label    string // the undone turn's checkpoint label
	Restored []string
}

// Approval is a standing permission: actions it covers run without asking.
type Approval struct {
	// Key identifies the approval, for RevokeApprovals.
	Key string
	// Kind is what it allows: "cmd" (a shell command), "write", "delete",
	// "web" (a host), "mcp" (an MCP tool), "uc-run" (a forged tool), or
	// another kind a newer version added.
	Kind string
	// Subject is the command, path, host or tool.
	Subject string
	// Dir is the workspace a command approval is limited to ("" if any).
	Dir string
	// Always: saved for future sessions (Added says when); otherwise it
	// lasts until this process exits.
	Always bool
	Added  time.Time
}

// ErrUndoConflict is returned when files changed after the checkpointed edit.
var ErrUndoConflict = errors.New("files changed since the edit")

// RewindPoint is a prompt the session can be rewound to.
type RewindPoint struct {
	// Index is the prompt's position in the transcript.
	Index int
	Text  string
	Time  time.Time
	// Files are what the agent changed from this prompt until the next
	// (what rewinding the code to it restores, with later prompts' files).
	Files []string
	// Conversation reports whether the conversation can be rewound to it
	// (prompts recorded by older versions can't be).
	Conversation bool
}

// RewindMode is what a rewind restores.
type RewindMode string

const (
	RewindBoth          RewindMode = "both"            // files and conversation
	RewindConversation  RewindMode = "conversation"    // conversation only; files stay
	RewindCode          RewindMode = "code"            // files only; conversation stays
	RewindSummarizeFrom RewindMode = "summarize_from"  // summarize the prompt and everything after
	RewindSummarizeUpTo RewindMode = "summarize_up_to" // summarize everything before the prompt
)

// RewindModes are the modes, in the order offered.
var RewindModes = []RewindMode{RewindBoth, RewindConversation, RewindCode, RewindSummarizeFrom, RewindSummarizeUpTo}

// RewindResult is what a rewind did.
type RewindResult struct {
	Mode RewindMode
	// Restored are the files put back (code).
	Restored []string
	// Prompt is the rewound prompt's text, to edit and send again
	// (conversation).
	Prompt string
	// Compacted is the summary's result (summarize modes).
	Compacted CompactResult
}

var (
	// ErrSessionBusy reports a rewind while a turn runs in the session.
	ErrSessionBusy = errors.New("a turn is running in this session")
	// ErrNotRewindPoint reports an index that isn't one of the session's
	// prompts.
	ErrNotRewindPoint = errors.New("not a prompt of this session")
	// ErrCantRewindConversation reports a prompt recorded without its place
	// in the conversation (by an older version).
	ErrCantRewindConversation = errors.New("this prompt's place in the conversation isn't known")
	// ErrUnknownRewindMode reports a mode that isn't one of RewindModes.
	ErrUnknownRewindMode = errors.New("unknown rewind mode")
)

// ErrNothingToUndo: there are no changes left to restore.
var ErrNothingToUndo = errors.New("nothing to undo")
