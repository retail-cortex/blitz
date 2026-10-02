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
	"path/filepath"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// ProjectAgentsDir is the folder of the workspace's own agents.
func (w *Workspace) ProjectAgentsDir() string {
	return filepath.Join(w.Dir(), config.ProjectAgentsDir)
}

// RefreshAgents loads the agents again if a file in their folders was
// added, changed or removed since they were loaded: a new agent is offered,
// an edited one's prompt, tools and model apply from the next turn, and a
// removed one is gone (the active agent falls back to blitz). Files that
// don't load are warnings.
func (w *Workspace) RefreshAgents(ctx context.Context) {
	w.rebuildMu.Lock()
	defer w.rebuildMu.Unlock()
	changed, err := w.agents.Refresh()
	if err != nil {
		w.warn(err.Error())
	}
	if !changed {
		return
	}
	refs := agentModelRefs(w.cfg, w.agents, func(string) {})
	for agent, ref := range refs {
		if w.agentRefs[agent] == ref {
			continue
		}
		m, err := w.newModel(ctx, w.cfg, ref)
		if err == nil {
			err = w.engine.PinModel(ctx, agent, m)
		}
		if err != nil {
			w.warn(i18n.T("pin.load_failed", "agent", agent, "model", ref, "error", ModelErrorSummary(err, w.cfg)))
			delete(refs, agent) // tried again on the next change
		}
	}
	for agent := range w.agentRefs {
		if _, ok := refs[agent]; !ok {
			_ = w.engine.Unpin(ctx, agent) // back to the configured model
		}
	}
	w.agentRefs = refs
	if err := w.engine.Rebuild(ctx); err != nil {
		w.warn(err.Error())
	}
}
