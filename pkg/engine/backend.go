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

package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

var _ api.Backend = (*Workspace)(nil)

// SetUI routes approval requests and questions to the front end.
func (w *Workspace) SetUI(approve api.Approver, ask api.UserPromptFunc) {
	if approve != nil {
		w.tools.Hooks().SetApprover(approve)
	}
	if ask != nil {
		w.tools.Hooks().SetUserPrompter(ask)
	}
}

// Processes are the workspace's background processes.
func (w *Workspace) Processes() api.Processes { return w.tools.Processes() }

// ListProcesses are the background processes started in any of sessions'
// turns, ordered by ID: a client's, which it accounts for when it exits.
func (w *Workspace) ListProcesses(sessions []string) []api.ProcessInfo {
	return w.tools.Processes().ListIn(sessions)
}

// ProcessOutput is the captured output and status of background process
// id, when one of sessions started it (else api.ErrUnknownProcess).
func (w *Workspace) ProcessOutput(sessions []string, id int) (string, api.ProcessInfo, error) {
	if err := w.processIn(sessions, id); err != nil {
		return "", api.ProcessInfo{}, err
	}
	return w.tools.Processes().Output(id)
}

// KillProcess stops background process id and its descendants, when one
// of sessions started it (else api.ErrUnknownProcess).
func (w *Workspace) KillProcess(sessions []string, id int) (api.ProcessInfo, error) {
	if err := w.processIn(sessions, id); err != nil {
		return api.ProcessInfo{}, err
	}
	return w.tools.Processes().Kill(id)
}

// RunningProcesses are the background processes still running, ordered
// by ID, whichever session started them (the tray's Processes menu).
func (w *Workspace) RunningProcesses() []api.ProcessInfo {
	return w.tools.Processes().Running()
}

// AnyProcessOutput is background process id's captured output and status,
// whichever session started it: the user's own look (spec_processes_052).
func (w *Workspace) AnyProcessOutput(id int) (string, api.ProcessInfo, error) {
	if _, err := w.tools.Processes().SessionOf(id); err != nil {
		return "", api.ProcessInfo{}, fmt.Errorf("%w: %d", api.ErrUnknownProcess, id)
	}
	return w.tools.Processes().Output(id)
}

// StopProcess stops background process id and its descendants, whichever
// session started it: the user's own stop.
func (w *Workspace) StopProcess(id int) (api.ProcessInfo, error) {
	if _, err := w.tools.Processes().SessionOf(id); err != nil {
		return api.ProcessInfo{}, fmt.Errorf("%w: %d", api.ErrUnknownProcess, id)
	}
	return w.tools.Processes().Kill(id)
}

// processIn checks that one of sessions started background process id.
func (w *Workspace) processIn(sessions []string, id int) error {
	s, err := w.tools.Processes().SessionOf(id)
	if err != nil || !slices.Contains(sessions, s) {
		return fmt.Errorf("%w: %d", api.ErrUnknownProcess, id)
	}
	return nil
}

// AuditShell records a command the user ran directly.
func (w *Workspace) AuditShell(command string, exitCode int, startErr error) {
	entry := audit.Entry{Kind: audit.KindUserShell, Detail: command, Decision: "exit " + strconv.Itoa(exitCode)}
	if startErr != nil {
		entry.Error = startErr.Error()
	}
	w.tools.Hooks().Audit().Log(entry)
}

// MaxUserShellOutput bounds what RunUserShell passes on of a command's
// output; the rest is dropped, and said to be.
const MaxUserShellOutput = 1 << 20

// RunUserShell runs a command the user typed after "!" in a client that
// has no terminal of its own (the desktop app's chat), in the workspace:
// as the user, outside the agent's sandbox and approvals, unseen by the
// agent. Its output (stdout and stderr together, at most
// MaxUserShellOutput) goes to out as it comes; the audit log records it
// as the terminal's ! is (AuditShell). It returns the exit code (-1 when
// stopped: ctx's end kills its process group), whether output was cut,
// and why it didn't start.
func (w *Workspace) RunUserShell(ctx context.Context, command string, out func([]byte)) (exitCode int, truncated bool, startErr error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return 0, false, errors.New("no command after the exclamation mark")
	}
	cmd := tools.UserCommand(ctx, w.Dir(), command)
	sink := &cappedWriter{out: out, left: MaxUserShellOutput}
	cmd.Stdout, cmd.Stderr = sink, sink
	err := cmd.Run()
	code := 0
	switch {
	case cmd.ProcessState == nil:
		startErr = err
	case ctx.Err() != nil:
		code = -1
	default:
		code = cmd.ProcessState.ExitCode()
	}
	w.AuditShell(command, code, startErr)
	return code, sink.cut, startErr
}

// cappedWriter passes writes on until left runs out, then drops them.
type cappedWriter struct {
	mu   sync.Mutex
	out  func([]byte)
	left int
	cut  bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(p)
	if c.left <= 0 {
		c.cut = c.cut || n > 0
		return n, nil
	}
	if len(p) > c.left {
		p, c.cut = p[:c.left], true
	}
	c.left -= len(p)
	c.out(slices.Clone(p))
	return n, nil
}

// ImagesEnabled reports whether images can be attached.
func (w *Workspace) ImagesEnabled() bool { return w.cfg.Images.Enabled && w.tools.Images() != nil }
