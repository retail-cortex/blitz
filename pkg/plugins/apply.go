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

package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
)

// Loaded is a plugin a run uses, and where it came from.
type Loaded struct {
	*Plugin
	// Installed is its store entry; nil for a --plugin-dir one.
	Installed *Installed
}

// Apply loads the plugins cfg enables and adds what they bring to cfg:
// the store's enabled plugins (and those [plugins] enable names, from the
// user or a trusted project), less those [plugins] disable names, then
// cfg.Plugins.Dirs (--plugin-dir). A plugin whose files changed since it
// was installed isn't loaded. Problems are returned, not fatal.
func Apply(cfg *config.Config, store *Store) ([]Loaded, []string) {
	var loaded []Loaded
	var problems []string
	list, err := store.List()
	if err != nil {
		problems = append(problems, err.Error())
	}
	for _, i := range list {
		on := (i.Enabled || slices.Contains(cfg.Plugins.Enable, i.Name)) && !slices.Contains(cfg.Plugins.Disable, i.Name)
		if !on {
			continue
		}
		dir := store.PluginDir(i)
		if h, err := Hash(dir); err != nil || h != i.Hash {
			problems = append(problems, fmt.Sprintf("plugin %s: its files changed since it was installed (reinstall it: blitz plugin update %s)", i.Name, i.Name))
			continue
		}
		p, err := Read(dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("plugin %s: %v", i.Name, err))
			continue
		}
		loaded = append(loaded, Loaded{Plugin: p, Installed: &i})
	}
	for _, name := range cfg.Plugins.Enable {
		if !slices.ContainsFunc(list, func(i Installed) bool { return i.Name == name }) {
			problems = append(problems, fmt.Sprintf("plugin %s is enabled but not installed", name))
		}
	}
	for _, d := range cfg.Plugins.Dirs {
		p, err := Read(config.ExpandHome(d))
		if err != nil {
			problems = append(problems, fmt.Sprintf("--plugin-dir %s: %v", d, err))
			continue
		}
		loaded = append(loaded, Loaded{Plugin: p})
	}
	for _, l := range loaded {
		problems = append(problems, add(cfg, l.Plugin)...)
	}
	return loaded, problems
}

// add puts p's components in cfg.
func add(cfg *config.Config, p *Plugin) []string {
	var problems []string
	source := "plugin " + p.Name
	if len(p.Skills) > 0 {
		cfg.Skills.Paths = append(cfg.Skills.Paths, filepath.Join(p.Dir, SkillsDir))
	}
	if isDir(filepath.Join(p.Dir, AgentsDir)) {
		cfg.PluginAgentDirs = append(cfg.PluginAgentDirs, filepath.Join(p.Dir, AgentsDir))
	}
	if isDir(filepath.Join(p.Dir, CommandsDir)) {
		cfg.PluginCommandDirs = append(cfg.PluginCommandDirs, config.PluginDir{Dir: filepath.Join(p.Dir, CommandsDir), Plugin: p.Name})
	}
	cfg.Hooks = config.AppendHooks(cfg.Hooks, config.WithSource(rooted(p.Hooks, p.Dir), source))
	for _, s := range p.MCP {
		s = rootedServer(s, p.Dir)
		if slices.ContainsFunc(cfg.MCP.Servers, func(u config.MCPServerConfig) bool { return u.Name == s.Name }) {
			problems = append(problems, fmt.Sprintf("plugin %s: an MCP server named %s exists already; the plugin's isn't used", p.Name, s.Name))
			continue
		}
		cfg.MCP.Servers = append(cfg.MCP.Servers, s)
	}
	return problems
}

// RootVar in a plugin's hooks and MCP servers is its directory.
const RootVar = "${BLITZ_PLUGIN_ROOT}"

// rooted puts the plugin's directory in its hooks' commands.
func rooted(h config.HooksConfig, dir string) config.HooksConfig {
	fix := func(list []config.HookConfig) []config.HookConfig {
		out := slices.Clone(list)
		for i := range out {
			out[i].Command = strings.ReplaceAll(out[i].Command, RootVar, dir)
			out[i].Args = replaceAll(out[i].Args, dir)
		}
		return out
	}
	h.PreTool, h.PostTool, h.PromptSubmit = fix(h.PreTool), fix(h.PostTool), fix(h.PromptSubmit)
	h.SessionStart, h.SessionEnd, h.Stop = fix(h.SessionStart), fix(h.SessionEnd), fix(h.Stop)
	h.PostToolFailure, h.SubagentStart, h.SubagentStop = fix(h.PostToolFailure), fix(h.SubagentStart), fix(h.SubagentStop)
	h.PreCompact, h.PostCompact = fix(h.PreCompact), fix(h.PostCompact)
	h.Notification, h.PermissionRequest = fix(h.Notification), fix(h.PermissionRequest)
	return h
}

func rootedServer(s config.MCPServerConfig, dir string) config.MCPServerConfig {
	s.Command = strings.ReplaceAll(s.Command, RootVar, dir)
	s.Args = replaceAll(s.Args, dir)
	if len(s.Env) > 0 {
		env := make(map[string]string, len(s.Env))
		for k, v := range s.Env {
			env[k] = strings.ReplaceAll(v, RootVar, dir)
		}
		s.Env = env
	}
	return s
}

func replaceAll(list []string, dir string) []string {
	if list == nil {
		return nil
	}
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = strings.ReplaceAll(v, RootVar, dir)
	}
	return out
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
