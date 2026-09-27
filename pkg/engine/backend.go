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
