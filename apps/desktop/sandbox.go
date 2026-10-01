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

package main

import (
	"context"

	"github.com/retail-cortex/blitz/pkg/sandboxsetup"
)

// SandboxFix is what FixSandbox did: the sandbox's state after it, and if
// it didn't work, why and the commands to run by hand.
type SandboxFix struct {
	Status   sandboxsetup.Status `json:"status"`
	Error    string              `json:"error,omitempty"`
	Commands string              `json:"commands,omitempty"`
}

// SandboxStatus is whether the OS sandbox for the agent's commands works
// here (Linux: bubblewrap; "unsupported" elsewhere, where the page hides
// the setting).
func (a *App) SandboxStatus() sandboxsetup.Status { return sandboxsetup.Check(a.context()) }

// FixSandbox lets bubblewrap past AppArmor's restriction with a profile
// for bwrap alone, asking for the password with the system's dialog
// (pkexec), then checks again.
func (a *App) FixSandbox() SandboxFix {
	st, err := sandboxsetup.Fix(a.context(), sandboxsetup.Elevation{Graphical: true})
	if err != nil {
		return SandboxFix{Status: st, Error: err.Error(), Commands: sandboxsetup.Commands(st)}
	}
	return SandboxFix{Status: st}
}

// context is the app's, or a background one before Wails starts it (tests).
func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}
