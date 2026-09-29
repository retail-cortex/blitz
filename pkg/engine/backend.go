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
	"fmt"
	"slices"
	"strconv"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/engine/audit"
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

// ImagesEnabled reports whether images can be attached.
func (w *Workspace) ImagesEnabled() bool { return w.cfg.Images.Enabled && w.tools.Images() != nil }
