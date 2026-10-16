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

	"github.com/retail-cortex/blitz/pkg/pagehost"
	"github.com/retail-cortex/blitz/pkg/sandboxsetup"
)

// SandboxFix is what FixSandbox did (pkg/pagehost).
type SandboxFix = pagehost.SandboxFix

// SandboxStatus is whether the OS sandbox for the agent's commands works
// here (pkg/pagehost).
func (a *App) SandboxStatus() sandboxsetup.Status { return pagehost.SandboxStatus(a.context()) }

// FixSandbox lets bubblewrap past AppArmor's restriction, asking for the
// password with the system's dialog, then checks again (pkg/pagehost).
func (a *App) FixSandbox() SandboxFix { return pagehost.FixSandbox(a.context()) }

// context is the app's, or a background one before Wails starts it (tests).
func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}
