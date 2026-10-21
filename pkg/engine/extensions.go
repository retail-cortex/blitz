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
	"sort"
	"strings"
	"time"

	"google.golang.org/adk/v2/model"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"

	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/redact"
)

// Skills and their scripts' environments, MCP servers, and the tools an
// agent can use.

func (w *Workspace) skillInfo(s *skills.Skill) api.SkillInfo {
	info := api.SkillInfo{
		Name: s.Name, Description: s.Description, Version: s.Version, License: s.License, Category: s.Category,
		Compatibility: s.Compatibility, Tags: s.Tags, NeedsNetwork: s.ExecutionHints.NeedsNetwork(), Dir: s.HostDir(),
		Kind: s.Kind(), Agents: s.Agents, Members: s.Members, References: s.References, PackageFiles: s.PackageFiles, Project: w.ownsSkill(s),
		Triggers: s.TriggerPhrases, Scenarios: len(skills.Scenarios(s)),
	}
	for _, t := range s.ToolRequirements {
		info.Tools = append(info.Tools, api.SkillTool{Name: t.Name, Scopes: t.Scopes, Why: t.Description})
	}
	ev := skills.Evaluate(s, w.cfg.Skills.Policy)
	info.Hash, info.Network, info.Env, info.Withheld, info.Blocked = ev.Hash, ev.Network, ev.Env, ev.Withheld, ev.Blocked
	info.NeedsReview = ev.NeedsReview
	if len(s.Scripts) > 0 {
		info.Tier, info.Bypass = ev.Tier.String(), ev.Bypass
	}
	for i, v := range ev.Scripts {
		src := s.Scripts[i].RelativePath
		if src == "" {
			src = "inline"
		}
		info.Scripts = append(info.Scripts, api.SkillScript{
			Name: v.Name, Language: string(s.Scripts[i].Language), Source: src, Timeout: time.Duration(v.TimeoutSeconds) * time.Second,
			Deps: v.Dependencies, Allowed: v.Allowed, Reasons: v.Reasons, Found: s.Scripts[i].Found,
		})
	}
	return info
}

func (w *Workspace) skillInfos(list []*skills.Skill) []api.SkillInfo {
	out := make([]api.SkillInfo, len(list))
	for i, s := range list {
		out[i] = w.skillInfo(s)
	}
	return out
}

// ListSkills returns every skill found.
func (w *Workspace) ListSkills() []api.SkillInfo { return w.skillInfos(w.skills.List()) }

// SearchSkills returns the skills matching query.
func (w *Workspace) SearchSkills(query string) []api.SkillInfo {
	return w.skillInfos(w.skills.Search(query))
}

// Skill returns the named skill.
func (w *Workspace) Skill(name string) (api.SkillInfo, bool) {
	s, ok := w.skills.Get(name)
	if !ok {
		return api.SkillInfo{}, false
	}
	return w.skillInfo(s), true
}

func (w *Workspace) pyEnvs() (*tools.PyEnvs, error) {
	if w.tools.SkillScripts() == nil {
		return nil, api.ErrScriptsDisabled
	}
	return w.tools.SkillScripts().Envs(), nil
}

// ListEnvs returns the script environments.
func (w *Workspace) ListEnvs() ([]api.Env, error) {
	envs, err := w.pyEnvs()
	if err != nil {
		return nil, err
	}
	var out []api.Env
	for _, e := range envs.List() {
		out = append(out, api.Env{Key: e.Key, Deps: e.Deps, Skills: e.Skills, Size: e.Size, LastUsed: e.LastUsed, Ready: e.Ready})
	}
	return out, nil
}

// RemoveEnv deletes one environment.
func (w *Workspace) RemoveEnv(key string) error {
	envs, err := w.pyEnvs()
	if err != nil {
		return err
	}
	return envs.Remove(key)
}

// PruneEnvs removes environments no script the policy lets run needs, and
// incomplete ones.
func (w *Workspace) PruneEnvs() (api.PruneResult, error) {
	envs, err := w.pyEnvs()
	if err != nil {
		return api.PruneResult{}, err
	}
	needed := w.neededEnvs(envs)
	var res api.PruneResult
	for _, e := range envs.List() {
		if e.Ready && needed[e.Key] {
			continue
		}
		if err := envs.Remove(e.Key); err != nil {
			res.Failed = append(res.Failed, api.EnvError{Key: e.Key, Err: err})
			continue
		}
		res.Removed++
		res.Freed += e.Size
	}
	return res, nil
}

// neededEnvs are the environments of the scripts the skills policy lets run.
func (w *Workspace) neededEnvs(envs *tools.PyEnvs) map[string]bool {
	needed := map[string]bool{}
	python, err := tools.SystemPython()
	if err != nil {
		return needed
	}
	for _, s := range w.skills.List() {
		ev := skills.Evaluate(s, w.cfg.Skills.Policy)
		for i, sc := range s.Scripts {
			if ev.Scripts[i].Allowed && len(sc.Dependencies) > 0 {
				needed[envs.Key(python, sc.Dependencies)] = true
			}
		}
	}
	return needed
}

// ListMCPServers returns the configured MCP servers, the disabled ones
// marked, each with how it's doing (State: disabled, idle, connected or
// failed).
func (w *Workspace) ListMCPServers() []api.MCPServer {
	states := map[string]tools.MCPState{}
	for _, st := range w.tools.MCP().States() {
		states[st.Name] = st
	}
	var out []api.MCPServer
	for _, s := range w.liveMCP() {
		target := s.URL
		if s.InProcess != "" {
			target = "in-process: " + strings.Join(s.Args, ", ")
		} else if target == "" {
			target = strings.Join(append([]string{s.Command}, s.Args...), " ")
		}
		m := api.MCPServer{Name: s.Name, Target: target, AutoApprove: s.AutoApprove, Disabled: s.Disabled, State: "disabled"}
		if st, ok := states[s.Name]; ok && !s.Disabled {
			m.State, m.Error, m.Tools = st.State, st.Error, st.Tools
		}
		out = append(out, m)
	}
	return out
}

// ReloadMCP applies cfg's MCP servers in place of the running ones, after
// a settings change: unchanged servers keep running, changed ones
// restart, disabled and removed ones stop, and the agents are built
// again so the next turn offers what's there now.
func (w *Workspace) ReloadMCP(ctx context.Context, cfg *config.Config) error {
	return w.applyMCP(ctx, cfg.MCP)
}

// applyMCP puts m's servers, with their secrets and the connector servers,
// in place of the running ones (one at a time: the settings watch and the
// service both reload).
func (w *Workspace) applyMCP(ctx context.Context, m config.MCPConfig) error {
	changed, err := w.replaceMCP(m)
	if err != nil || !changed {
		return err
	}
	return w.engine.Rebuild(ctx)
}

// replaceMCP is applyMCP but the rebuild: the settings keep m as written
// (a changed secret is filled in afresh next time), mcpLive what runs.
func (w *Workspace) replaceMCP(m config.MCPConfig) (bool, error) {
	w.mcpMu.Lock()
	defer w.mcpMu.Unlock()
	live := w.withConnectors(mcpWithSecrets(m.Servers, w.Dir()), w.cfg.Dir, w.Dir(), w.warn)
	changed, err := w.tools.MCP().Update(live)
	if err != nil {
		return false, err
	}
	w.cfgMu.Lock()
	defer w.cfgMu.Unlock()
	w.cfg.MCP, w.mcpLive = m, live
	return changed, nil
}

// liveMCP are the MCP servers as they run (mcpLive).
func (w *Workspace) liveMCP() []config.MCPServerConfig {
	w.cfgMu.RLock()
	defer w.cfgMu.RUnlock()
	return w.mcpLive
}

// secretRedactor is SecretRedactor of the workspace's settings now: its
// keys and MCP servers as no reload is halfway through changing them.
func (w *Workspace) secretRedactor() *redact.Redactor {
	return SecretRedactor(w.settings())
}

// settings is a copy of the workspace's settings, taken while nothing
// changes them: what's handed to a model, a redactor or a summary. Its
// maps are shared, so they're replaced, never written in place.
func (w *Workspace) settings() *config.Config {
	w.cfgMu.RLock()
	defer w.cfgMu.RUnlock()
	c := *w.cfg
	if w.mcpLive != nil {
		c.MCP.Servers = w.mcpLive // their secrets, for a redactor
	}
	return &c
}

// suggestionsModel is the small model the drafts and judges use: the
// suggestions model, else the reviewer's (the main one when neither is set).
func (w *Workspace) suggestionsModel(ctx context.Context) (model.LLM, error) {
	return suggestionsLLM(ctx, w.settings(), w.newModel)
}

// replyIn is the language the agents reply in (SetLocale changes it).
func (w *Workspace) replyIn() *i18n.Localizer {
	w.cfgMu.RLock()
	defer w.cfgMu.RUnlock()
	return w.reply
}

// setSettings changes the workspace's settings with f, nothing reading
// them meanwhile.
func (w *Workspace) setSettings(f func(c *config.Config)) {
	w.cfgMu.Lock()
	defer w.cfgMu.Unlock()
	f(w.cfg)
}

// mcpConfig is the MCP settings now, as written (applyMCP replaces them).
func (w *Workspace) mcpConfig() config.MCPConfig {
	w.cfgMu.RLock()
	defer w.cfgMu.RUnlock()
	return w.cfg.MCP
}

// RestartMCPServer stops the MCP server named name and starts it afresh
// when the next turn lists its tools.
func (w *Workspace) RestartMCPServer(ctx context.Context, name string) error {
	if err := w.tools.MCP().Restart(name); err != nil {
		return err
	}
	return w.engine.Rebuild(ctx)
}

// ActiveAgentTools returns what the active agent can use: its built-in
// tools and the MCP servers offered to it.
func (w *Workspace) ActiveAgentTools() api.AgentTools {
	active := w.engine.ActiveAgent()
	out := api.AgentTools{Agent: active}
	spec, ok := w.agents.Get(active)
	if !ok {
		return out
	}
	for _, t := range w.tools.GetToolsForAgent(spec.Tools) {
		out.Tools = append(out.Tools, api.ToolInfo{Name: t.Name(), Description: t.Description(), PlanAllowed: runtime.PlanAllows(t.Name())})
	}
	sort.Slice(out.Tools, func(i, j int) bool { return out.Tools[i].Name < out.Tools[j].Name })
	for _, s := range w.liveMCP() {
		if !s.Disabled && mcpOfferedTo(s.Agents, active) { // a disabled server offers nothing
			out.MCP = append(out.MCP, api.MCPOffer{Server: s.Name, Tools: s.Tools, Prefix: s.Prefix})
		}
	}
	return out
}

// AllTools describes every tool an agent can be given, by name.
func (w *Workspace) AllTools() []api.ToolInfo {
	var out []api.ToolInfo
	for _, t := range w.tools.GetAllTools() {
		out = append(out, api.ToolInfo{Name: t.Name(), Description: t.Description(), PlanAllowed: runtime.PlanAllows(t.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// mcpOfferedTo mirrors the MCP manager's rule for the primary agent: no
// agents list means the primary agent only; "*" means every agent.
func mcpOfferedTo(agents []string, active string) bool {
	if len(agents) == 0 {
		return true
	}
	for _, a := range agents {
		if a == "*" || a == active {
			return true
		}
	}
	return false
}
