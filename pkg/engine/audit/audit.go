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

// Package audit writes an append-only JSONL record of what tools did and
// what the user approved or denied. Secrets are masked before writing.
package audit

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/redact"
)

// Kinds of audit entries.
const (
	KindSession    = "session_start"
	KindPrompt     = "prompt"
	KindToolCall   = "tool_call"
	KindToolResult = "tool_result"
	KindApproval   = "approval"
	KindDenial     = "denial"
	KindHook       = "hook"
	KindRule       = "rule"      // a rule's verdict on a prompt or an answer (spec_gates_041)
	KindGuardrail  = "guardrail" // a guardrail's decision on a prompt, a tool call, a result or an answer
	KindUndo       = "undo"
	KindAttachment = "attachment"  // an image sent to the model: path and hash only
	KindUserShell  = "user_shell"  // a command the user ran with "!" (not an agent action)
	KindUserSearch = "user_search" // a web search the user ran with /search web
	KindTurnEnd    = "turn_end"    // a turn that didn't finish: stopped, or at a limit (Decision)
)

// Decisions on a KindPrompt and a KindTurnEnd entry.
const (
	DecisionSteer   = "steer"   // a prompt sent while a turn ran (Workspace.Steer)
	DecisionStopped = "stopped" // the user stopped the turn
	DecisionLimit   = "limit"   // the turn reached a limit; Detail: steps, cost or time
)

// Entry is one audit record.
type Entry struct {
	Time      time.Time      `json:"time"`
	Session   string         `json:"session,omitempty"`
	Kind      string         `json:"kind"`
	Tool      string         `json:"tool,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
	Detail    string         `json:"detail,omitempty"`
	Decision  string         `json:"decision,omitempty"`
	Error     string         `json:"error,omitempty"`
	Workspace string         `json:"workspace,omitempty"`
	// Agent is the agent whose action it was: on denials and failed tool
	// results only, which the harness's evidence blames agents with.
	Agent string `json:"agent,omitempty"`
}

// Logger appends entries to <dir>/audit-YYYY-MM-DD.jsonl. A nil *Logger is a
// valid no-op, so callers never need to check whether auditing is enabled.
type Logger struct {
	mu        sync.Mutex
	dir       string
	day       string
	f         *os.File
	redactor  *redact.Redactor
	workspace string
	now       func() time.Time
}

// Open creates the audit directory (owner-only) and returns a logger.
func Open(dir string, r *redact.Redactor) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create audit dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	return &Logger{dir: dir, redactor: r, now: time.Now}, nil
}

// SetWorkspace sets the workspace later entries record when they name
// none. The session is each entry's own (LogContext): a workspace runs
// turns of several sessions at once, so the logger has none.
func (l *Logger) SetWorkspace(workspace string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.workspace = workspace
}

// sessionKey is the context key of the session a turn audits for.
type sessionKey struct{}

// WithSession is ctx for a turn of session: what LogContext audits in it
// records that session.
func WithSession(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, sessionKey{}, session)
}

// SessionFrom is the session WithSession put in ctx ("" none).
func SessionFrom(ctx context.Context) string {
	s, _ := ctx.Value(sessionKey{}).(string)
	return s
}

// LogContext is Log, with the session of ctx's turn when e names none,
// and the agent of ctx on a denial or a failed result.
func (l *Logger) LogContext(ctx context.Context, e Entry) {
	if e.Session == "" && ctx != nil {
		e.Session = SessionFrom(ctx)
	}
	if e.Agent == "" && ctx != nil && blamed(e) {
		e.Agent = agentOf(ctx)
	}
	l.Log(e)
}

// blamed reports whether e is an entry that names its agent: a denial, or
// a tool's result that wasn't ok.
func blamed(e Entry) bool {
	return e.Kind == KindDenial || e.Kind == KindToolResult && e.Decision != "" && e.Decision != "ok"
}

// agentOf is the agent ctx runs, when it's an agent's context ("" if not).
func agentOf(ctx context.Context) (name string) {
	a, ok := ctx.(interface{ AgentName() string })
	if !ok {
		return ""
	}
	defer func() {
		if recover() != nil { // a test's strict mock
			name = ""
		}
	}()
	return a.AgentName()
}

// Log appends an entry. Failures are reported once to stderr and otherwise
// ignored: auditing must never break the session.
func (l *Logger) Log(e Entry) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	e.Time = l.now().UTC()
	if e.Workspace == "" {
		e.Workspace = l.workspace
	}
	e.Detail = l.redactor.String(e.Detail)
	e.Error = l.redactor.String(e.Error)
	if e.Args != nil {
		e.Args, _ = l.redactor.Value(e.Args).(map[string]any)
	}

	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	if err := l.rotateLocked(e.Time); err != nil {
		fmt.Fprintf(os.Stderr, "audit: %v\n", err)
		return
	}
	_, _ = l.f.Write(append(line, '\n'))
}

func (l *Logger) rotateLocked(t time.Time) error {
	day := t.Format("2006-01-02")
	if l.f != nil && day == l.day {
		return nil
	}
	if l.f != nil {
		l.f.Close()
	}
	f, err := os.OpenFile(filepath.Join(l.dir, "audit-"+day+".jsonl"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		l.f = nil
		return err
	}
	l.f, l.day = f, day
	return nil
}

// Read returns the entries in dir's daily files (audit-YYYY-MM-DD.jsonl)
// written since then, about workspace or no workspace in particular, in
// the order they were written. A missing dir or day is no entries.
func Read(dir, workspace string, since time.Time) []Entry {
	if dir == "" {
		return nil
	}
	var out []Entry
	for d := since.UTC(); !d.After(time.Now().UTC().Add(24 * time.Hour)); d = d.AddDate(0, 0, 1) {
		f, err := os.Open(filepath.Join(dir, "audit-"+d.Format(time.DateOnly)+".jsonl"))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for sc.Scan() {
			var e Entry
			if json.Unmarshal(sc.Bytes(), &e) != nil || e.Time.Before(since) {
				continue
			}
			if e.Workspace != "" && e.Workspace != workspace {
				continue
			}
			out = append(out, e)
		}
		f.Close()
	}
	return out
}

// Stamp identifies the log Read would read from dir since then, as its
// files are: an entry written since changes it. It reads no entries.
func Stamp(dir string, since time.Time) string {
	h := sha256.New()
	for d := since.UTC(); !d.After(time.Now().UTC().Add(24 * time.Hour)); d = d.AddDate(0, 0, 1) {
		name := "audit-" + d.Format(time.DateOnly) + ".jsonl"
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil {
			fmt.Fprintf(h, "%s\x00%d\x00%d\n", name, info.Size(), info.ModTime().UnixNano())
		}
	}
	return fmt.Sprintf("%d:%x", since.UnixNano(), h.Sum(nil)[:8])
}

// Close closes the current file.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
