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
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// Auto mode with a local model (spec_005_approvals APR-14c): without a
// reviewer model of its own ([permissions.auto] model, built as the
// workspace opened), the session's model reviews the agent's actions, so
// a small model on this machine would judge its own (one denied a file
// inside the workspace as "outside the workspace", and gave up on its
// tools). While such a model is the workspace's, the mode is default (ask)
// instead, with a note saying why; a reviewer model, or a model that
// doesn't run here, gives auto back.

// localProviders run the model on the person's machine.
var localProviders = map[string]bool{"ollama": true}

// localReview reports whether auto mode's reviewer would be a local model
// reviewing its own actions: there's no reviewer of its own (ownReviewer)
// and cfg's model runs on this machine.
func localReview(cfg *config.Config, ownReviewer bool) bool {
	if ownReviewer {
		return false
	}
	provider, _ := runtime.ParseModelRef(cfg.ModelName(), cfg.LLM.Provider)
	return localProviders[provider]
}

// holdLocalAuto switches auto mode to default while localReview holds,
// and back to auto when it no longer does, if it was switched here.
func (w *Workspace) holdLocalAuto() {
	local := localReview(w.settings(), w.ownReviewer)
	mode := w.tools.Hooks().Mode()
	switch {
	case local && mode == api.ModeAuto:
		if w.tools.SetPermissionMode(api.ModeDefault) == nil {
			w.autoHeld.Store(true)
		}
	case !local && w.autoHeld.Swap(false) && mode == api.ModeDefault:
		_ = w.tools.SetPermissionMode(api.ModeAuto)
	}
}

// modeNote is what the window says beside the mode: why auto isn't used,
// while localReview holds; "" otherwise.
func (w *Workspace) modeNote() string {
	if !localReview(w.settings(), w.ownReviewer) {
		return ""
	}
	return i18n.T("permissions.auto_local")
}
