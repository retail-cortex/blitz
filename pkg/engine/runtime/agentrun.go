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

package runtime

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

// modeRank orders the permission modes from the strictest.
var modeRank = map[api.PermissionMode]int{api.ModePlan: 0, api.ModeDontAsk: 1, api.ModeDefault: 2, api.ModeAcceptEdits: 3, api.ModeAuto: 3, api.ModeBypass: 4}

// agentRun gives a sub-agent's run its agent's own permission mode and
// model-call budget, from its frontmatter (spec_background_agents_032
// BGA-50). A mode looser than the workspace's applies only to built-in
// agents, the user's own, and a trusted project's; bypass also needs the
// OS sandbox. Otherwise the workspace's mode stays, and the log says why.
func (e *Engine) agentRun(ctx context.Context, spec *agents.AgentSpec) context.Context {
	if m := api.PermissionMode(spec.PermissionMode); m != "" {
		current := e.toolReg.Hooks().Mode()
		switch {
		case modeRank[m] > modeRank[current] && !e.mayLoosen(spec):
			slog.WarnContext(ctx, "agent permission_mode not applied: a project's agent can't loosen the mode until the project is trusted", "agent", spec.Name, "mode", m)
		case m == api.ModeBypass && !e.toolReg.ShellSandbox().Active():
			slog.WarnContext(ctx, "agent permission_mode not applied: bypass needs the OS sandbox", "agent", spec.Name)
		case m == api.ModePlan:
			ctx = tools.WithPlanGate(ctx, tools.NewPlanGate(true))
		default:
			ctx = tools.WithMode(ctx, m)
		}
	}
	if spec.MaxTurns > 0 {
		own := &runState{maxTurns: spec.MaxTurns}
		if st := stateFrom(ctx); st != nil {
			own.sessionID, own.planOnly, own.mode, own.allowed = st.sessionID, st.planOnly, st.mode, st.allowed
			own.agent, own.model, own.models, own.taskID = st.agent, st.model, st.models, st.taskID
		}
		ctx = context.WithValue(ctx, runStateKey{}, own)
	}
	return ctx
}

// mayLoosen reports whether spec may run in a looser mode than the
// workspace's: it isn't the project's, or the project is trusted.
func (e *Engine) mayLoosen(spec *agents.AgentSpec) bool {
	if spec.Path == "" || e.projectTrusted {
		return true
	}
	ws := e.cfg.Tools.WorkspaceDir
	if ws == "" {
		return true
	}
	rel, err := filepath.Rel(ws, spec.Path)
	return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
