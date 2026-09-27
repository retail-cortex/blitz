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
	"sort"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

// Skills and their scripts' environments, MCP servers, and the tools an
// agent can use.

func (w *Workspace) skillInfo(s *skills.Skill) api.SkillInfo {
	info := api.SkillInfo{
		Name: s.Name, Description: s.Description, Version: s.Version, License: s.License, Category: s.Category,
		Compatibility: s.Compatibility, Tags: s.Tags, NeedsNetwork: s.ExecutionHints.NeedsNetwork(),
	}
	for _, t := range s.ToolRequirements {
		info.Tools = append(info.Tools, api.SkillTool{Name: t.Name, Scopes: t.Scopes, Why: t.Description})
	}
	ev := skills.Evaluate(s, w.cfg.Skills.Policy)
	info.Hash, info.Network, info.Env, info.Withheld, info.Blocked = ev.Hash, ev.Network, ev.Env, ev.Withheld, ev.Blocked
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
			Deps: v.Dependencies, Allowed: v.Allowed, Reasons: v.Reasons,
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

// ListMCPServers returns the configured MCP servers, or none when none
// could be started.
func (w *Workspace) ListMCPServers() []api.MCPServer {
	if len(w.tools.MCP().Servers()) == 0 {
		return nil
	}
	var out []api.MCPServer
	for _, s := range w.cfg.MCP.Servers {
		target := s.URL
		if target == "" {
			target = strings.Join(append([]string{s.Command}, s.Args...), " ")
		}
		out = append(out, api.MCPServer{Name: s.Name, Target: target, AutoApprove: s.AutoApprove})
	}
	return out
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
	for _, s := range w.cfg.MCP.Servers {
		if mcpOfferedTo(s.Agents, active) {
			out.MCP = append(out.MCP, api.MCPOffer{Server: s.Name, Tools: s.Tools, Prefix: s.Prefix})
		}
	}
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
