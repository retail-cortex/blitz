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

package tools

import (
	"github.com/retail-cortex/blitz/pkg/api"
)

// Mode returns the permission mode.
func (h *Hooks) Mode() api.PermissionMode {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.policy.Mode == "" {
		return api.ModeDefault
	}
	return h.policy.Mode
}

// setMode changes the mode; Registry.SetPermissionMode checks bypass's
// requirement first.
func (h *Hooks) setMode(m api.PermissionMode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.policy.Mode = m
}

// SetPermissionMode changes the permission mode, refusing bypass unless
// the OS sandbox is active.
func (r *Registry) SetPermissionMode(m api.PermissionMode) error {
	if _, err := api.ParsePermissionMode(string(m)); err != nil {
		return err
	}
	if m == api.ModeBypass && !r.ShellSandbox().Active() {
		return api.ErrBypassNeedsSandbox
	}
	r.hooks.setMode(m)
	return nil
}
