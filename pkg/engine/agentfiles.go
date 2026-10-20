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
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/memory"
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
	if err := w.refreshAgents(ctx); err != nil {
		w.warn(err.Error())
	}
}

// refreshAgents is RefreshAgents returning the files that didn't load.
func (w *Workspace) refreshAgents(ctx context.Context) error {
	w.rebuildMu.Lock()
	defer w.rebuildMu.Unlock()
	changed, loadErr := w.agents.Refresh()
	if !changed {
		return loadErr
	}
	refs := agentModelRefs(w.settings(), w.agents, func(string) {})
	for agent, ref := range refs {
		if w.agentRefs[agent] == ref {
			continue
		}
		m, err := w.newModel(ctx, w.settings(), ref)
		if err == nil {
			err = w.engine.PinModel(ctx, agent, m)
		}
		if err != nil {
			w.warn(i18n.T("pin.load_failed", "agent", agent, "model", ref, "error", ModelErrorSummary(err, w.settings())))
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
	return loadErr
}

// refreshAgentFiles brings the agents, skills, instruction files and rules
// in step with their files at the start of each turn, so an edit (by the
// user, an editor or the agent) applies from the next message, in this
// conversation or a new one. A look reads no file: it compares sizes and
// times with what was loaded. Nothing is rebuilt when nothing changed, so
// the prompt the model caches stays as it was.
func (w *Workspace) refreshAgentFiles(ctx context.Context) {
	w.RefreshAgents(ctx)
	skills := w.cfg.Skills.Enabled && w.skills.Refresh()
	w.memoryMu.Lock()
	defer w.memoryMu.Unlock()
	if memory.Stamp(w.Dir(), w.cfg.Memory, w.memory) != w.memoryStamp {
		if _, err := w.reloadMemory(ctx); err != nil { // its rebuild takes the skills too
			w.warn(err.Error())
		}
		return
	}
	if skills {
		w.rebuildMu.Lock()
		defer w.rebuildMu.Unlock()
		if err := w.engine.Rebuild(ctx); err != nil { // the skills listed in the prompt
			w.warn(err.Error())
		}
	}
}

// Reload reads the agents, skills, instruction files and rules again
// whether or not they look changed (/reload), rebuilds the agent with them,
// and says what it loaded and what didn't load. MCP servers and the
// project's settings aren't reloaded: opening the workspace again does.
func (w *Workspace) Reload(ctx context.Context) (api.ReloadResult, error) {
	var out api.ReloadResult
	w.agents.Invalidate()
	if err := w.refreshAgents(ctx); err != nil {
		out.Problems = append(out.Problems, problemLines(err)...)
	}
	if w.cfg.Skills.Enabled {
		if err := w.skills.Reload(); err != nil {
			out.Problems = append(out.Problems, problemLines(err)...)
		}
	}
	w.memoryMu.Lock()
	files, err := w.reloadMemory(ctx)
	for _, r := range w.memory.Rules {
		files = append(files, r.Path)
	}
	w.memoryMu.Unlock()
	if err != nil {
		return out, err
	}
	out.Agents, out.Files = w.agents.External(), files
	if w.cfg.Skills.Enabled {
		out.Skills = w.skills.External()
	}
	return out, nil
}

// problemLines are a joined error's errors, one a line.
func problemLines(err error) []string {
	var out []string
	for _, line := range strings.Split(err.Error(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
