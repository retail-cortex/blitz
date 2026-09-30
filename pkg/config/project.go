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

package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// A workspace may carry settings for everyone who works in it, in
// .blitz/settings.toml (committed) and .blitz/settings.local.toml
// (personal). A repository is not trusted, so a project file may set only
// the keys below, in three tiers (spec_project_config_031):
//
//   - tier A only informs or tightens (deny and ask rules, blocked paths,
//     lower limits, stricter skill and worker policy) and applies at once;
//   - tier B runs code or loosens policy (hooks, MCP servers, allow rules,
//     writable paths, models, more worker permissions) and applies only
//     after the person trusts that exact content (Project.Hash);
//   - everything else, credentials and endpoints above all, is ignored
//     with a warning.

// ProjectFiles are a workspace's settings files, relative to it, in the
// order they apply.
var ProjectFiles = []string{".blitz/settings.toml", ".blitz/settings.local.toml"}

// maxProjectFileSize bounds a project settings file.
const maxProjectFileSize = 256 << 10

// The kinds of project settings, in ProjectItem.Kind.
const (
	ProjectDeny        = "deny"         // a deny rule (A)
	ProjectAsk         = "ask"          // an ask rule (A)
	ProjectBlockedPath = "blocked_path" // a blocked path (A)
	ProjectLimit       = "limit"        // a lower limit (A)
	ProjectSkillPolicy = "skill_policy" // a stricter skill policy value (A)
	ProjectWorkerLimit = "worker_limit" // a stricter worker policy value (A)
	ProjectHook        = "hook"         // a hook (B)
	ProjectMCP         = "mcp"          // an MCP server (B)
	ProjectAllow       = "allow"        // an allow rule (B)
	ProjectWritable    = "writable"     // a shell-writable path (B)
	ProjectModel       = "model"        // the default model (B)
	ProjectAgentModel  = "agent_model"  // an agent's model (B)
	ProjectWorkerAllow = "worker_allow" // a permission kind workers may get (B)
	ProjectSetting     = "setting"      // any other key (ignored)
)

// Why a project setting was ignored, in ProjectItem.Reason.
const (
	ReasonNever       = "never"        // never taken from a project (credentials, endpoints, loosening the sandbox)
	ReasonUnknown     = "unknown"      // not a project setting
	ReasonNotStricter = "not_stricter" // no stricter than the user's own
	ReasonOutside     = "outside"      // a path outside the workspace
	ReasonProvider    = "provider"     // a model of a provider the user hasn't set up
	ReasonUserSet     = "user_set"     // the user's own settings set it
)

// ProjectItem is one setting from a project file.
type ProjectItem struct {
	File string // relative to the workspace
	Kind string // Project*
	// Key names it: the event of a hook, the MCP server, the agent, the
	// setting's key.
	Key string
	// Value is what it says: a rule, a command, a path, a model, a number.
	Value string
	// Reason is why it was ignored (Reason*), for ignored items.
	Reason string
}

// Project is what a workspace's project files say, and what came of it.
type Project struct {
	// Files are the project files found, relative to the workspace.
	Files []string
	// Applied are the tier A settings in force.
	Applied []ProjectItem
	// Pending are the tier B settings, in force only once trusted
	// (ApplyTrusted).
	Pending []ProjectItem
	// Ignored are settings a project may not set, or that change nothing.
	Ignored []ProjectItem
	// Problems are files that couldn't be read.
	Problems []string

	dir     string
	trusted []projectFile // tier B content, one per file
}

// projectFile is what a project file may hold. Keys it lacks are
// reported as ignored.
type projectFile struct {
	file        string
	Permissions struct {
		Allow []string `toml:"allow" json:"allow,omitempty"`
		Ask   []string `toml:"ask" json:"-"`
		Deny  []string `toml:"deny" json:"-"`
	} `toml:"permissions" json:"permissions"`
	Sandbox struct {
		BlockedPaths       []string `toml:"blocked_paths" json:"-"`
		ShellWritablePaths []string `toml:"shell_writable_paths" json:"shell_writable_paths,omitempty"`
	} `toml:"sandbox" json:"sandbox"`
	Tools struct {
		ShellTimeoutSeconds int   `toml:"shell_timeout_seconds" json:"-"`
		MaxFileSizeBytes    int64 `toml:"max_file_size_bytes" json:"-"`
		MaxParallel         int   `toml:"max_parallel" json:"-"`
	} `toml:"tools" json:"-"`
	Skills struct {
		Policy struct {
			MinHITLTier       int      `toml:"min_hitl_tier"`
			MaxTimeoutSeconds int      `toml:"max_timeout_seconds"`
			DenyTools         []string `toml:"deny_tools"`
		} `toml:"policy"`
	} `toml:"skills" json:"-"`
	Workers struct {
		Policy struct {
			Allow      []string `toml:"allow" json:"allow,omitempty"`
			MaxTurns   int      `toml:"max_turns" json:"-"`
			MaxCostUSD float64  `toml:"max_cost_usd" json:"-"`
		} `toml:"policy" json:"policy"`
	} `toml:"workers" json:"workers"`
	Blitz struct {
		DefaultModel string `toml:"default_model" json:"default_model,omitempty"`
	} `toml:"blitz" json:"blitz"`
	AgentModels map[string]string `toml:"agent_models" json:"agent_models,omitempty"`
	Hooks       HooksConfig       `toml:"hooks" json:"hooks"`
	MCP         struct {
		Servers []projectMCPServer `toml:"servers" json:"servers,omitempty"`
	} `toml:"mcp" json:"mcp"`
	// Referenced are the workspace files the hooks and MCP servers name
	// (their scripts), by path, with a hash of their content: editing
	// one asks for trust again.
	Referenced map[string]string `toml:"-" json:"referenced,omitempty"`
}

// projectMCPServer is an MCP server a project may define: without
// auto_approve, and always sandboxed.
type projectMCPServer struct {
	Name           string            `toml:"name" json:"name"`
	Command        string            `toml:"command" json:"command,omitempty"`
	Args           []string          `toml:"args" json:"args,omitempty"`
	Env            map[string]string `toml:"env" json:"env,omitempty"`
	URL            string            `toml:"url" json:"url,omitempty"`
	Tools          []string          `toml:"tools" json:"tools,omitempty"`
	Prefix         string            `toml:"prefix" json:"prefix,omitempty"`
	Agents         []string          `toml:"agents" json:"agents,omitempty"`
	TimeoutSeconds int               `toml:"timeout_seconds" json:"timeout_seconds,omitempty"`
}

// neverFromProject are the keys (and their subkeys) a project may never
// set: credentials, endpoints, where logs go, and anything that loosens
// the sandbox or skips approvals.
var neverFromProject = []string{
	"llm", "web", "telemetry", "log", "audit", "checkpoints", "session", "images.dir", "pricing",
	"blitz.auto_approve", "blitz.permission_mode", "blitz.trust_workspace", "permissions.auto",
	"sandbox.shell", "sandbox.allow_network", "sandbox.allowed_paths", "sandbox.read_only_paths",
	"sandbox.scrub_env", "sandbox.commands",
	"tools.auto_approve_commands", "tools.approvals_file", "tools.uc_tools_dir", "tools.workspace_dir",
	"mcp.servers.auto_approve", "mcp.servers.sandbox",
	"skills.policy.trusted_hashes", "skills.policy.allow_hitl_bypass", "skills.policy.network",
	"skills.policy.network_allow", "skills.policy.env_passthrough", "skills.policy.sandbox",
	"workers.policy.max_concurrent", "workers.paths", "skills.paths",
}

// LoadProject reads workspace's project files. A file that can't be read
// is a problem, not an error: the workspace opens without it.
func LoadProject(workspace string) *Project {
	p := &Project{dir: workspace}
	for _, name := range ProjectFiles {
		pf, ignored, err := readProjectFile(workspace, name)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			p.Problems = append(p.Problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		p.Files = append(p.Files, name)
		p.Ignored = append(p.Ignored, ignored...)
		pf.Referenced = referencedFiles(workspace, pf)
		p.trusted = append(p.trusted, *pf)
		p.Pending = append(p.Pending, pendingItems(workspace, pf)...)
	}
	return p
}

// readProjectFile decodes one project file: a regular file (not a link),
// TOML and nothing else.
func readProjectFile(workspace, name string) (*projectFile, []ProjectItem, error) {
	path := filepath.Join(workspace, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("not a regular file (a link is not followed)")
	}
	if info.Size() > maxProjectFileSize {
		return nil, nil, fmt.Errorf("larger than %d KB", maxProjectFileSize>>10)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	pf := &projectFile{file: name}
	md, err := toml.Decode(string(data), pf)
	if err != nil {
		return nil, nil, err
	}
	var ignored []ProjectItem
	for _, k := range md.Undecoded() {
		key := k.String()
		if md.Type(k...) == "Hash" || md.Type(k...) == "ArrayHash" {
			continue // a table: its keys are reported
		}
		ignored = append(ignored, ProjectItem{File: name, Kind: ProjectSetting, Key: key, Reason: undecodedReason(key)})
	}
	return pf, ignored, nil
}

func undecodedReason(key string) string {
	for _, n := range neverFromProject {
		if key == n || strings.HasPrefix(key, n+".") {
			return ReasonNever
		}
	}
	return ReasonUnknown
}

// pendingItems are pf's tier B settings, one item each.
func pendingItems(workspace string, pf *projectFile) []ProjectItem {
	var out []ProjectItem
	add := func(kind, key, value string) {
		out = append(out, ProjectItem{File: pf.file, Kind: kind, Key: key, Value: value})
	}
	events := pf.Hooks.ByEvent()
	names := make([]string, 0, len(events))
	for e := range events {
		names = append(names, e)
	}
	sort.Strings(names)
	for _, e := range names {
		for _, h := range events[e] {
			add(ProjectHook, e, h.Command)
		}
	}
	for _, s := range pf.MCP.Servers {
		v := s.URL
		if s.Command != "" {
			v = strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
		}
		add(ProjectMCP, s.Name, v)
	}
	for _, r := range pf.Permissions.Allow {
		add(ProjectAllow, "", r)
	}
	for _, d := range pf.Sandbox.ShellWritablePaths {
		add(ProjectWritable, "", d)
	}
	if pf.Blitz.DefaultModel != "" {
		add(ProjectModel, "", pf.Blitz.DefaultModel)
	}
	for _, agent := range sortedKeys(pf.AgentModels) {
		add(ProjectAgentModel, agent, pf.AgentModels[agent])
	}
	for _, k := range pf.Workers.Policy.Allow {
		add(ProjectWorkerAllow, "", k)
	}
	return out
}

// Hash identifies the tier B content: the settings that need trust, each
// tagged with its file, the content of the workspace files they run, and
// extra (the hashes of the project's skills with scripts, which the
// engine adds). "" when there is nothing to trust.
func (p *Project) Hash(extra ...string) string {
	if len(p.Pending) == 0 && len(extra) == 0 {
		return ""
	}
	h := sha256.New()
	for _, pf := range p.trusted {
		data, _ := json.Marshal(struct {
			File string `json:"file"`
			*projectFile
		}{pf.file, &pf})
		h.Write(data)
		h.Write([]byte{0})
	}
	extra = slices.Clone(extra)
	sort.Strings(extra)
	for _, e := range extra {
		io.WriteString(h, e)
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// referencedFiles are the workspace files that pf's hooks and MCP servers
// name (a script they run), with the SHA-256 of each.
func referencedFiles(workspace string, pf *projectFile) map[string]string {
	var words []string
	for _, hooks := range pf.Hooks.ByEvent() {
		for _, h := range hooks {
			words = append(words, strings.Fields(h.Command)...)
		}
	}
	for _, s := range pf.MCP.Servers {
		words = append(append(words, strings.Fields(s.Command)...), s.Args...)
	}
	out := map[string]string{}
	for _, w := range words {
		rel, ok := inWorkspace(workspace, strings.Trim(w, `"'`))
		if !ok {
			continue
		}
		f, err := os.Open(filepath.Join(workspace, rel))
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err == nil && info.Mode().IsRegular() {
			sum := sha256.New()
			if _, err := io.Copy(sum, io.LimitReader(f, 64<<20)); err == nil {
				out[filepath.ToSlash(rel)] = hex.EncodeToString(sum.Sum(nil))
			}
		}
		f.Close()
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// inWorkspace is p (relative to workspace, or absolute) as a path
// relative to workspace, when it lies inside it (symbolic links
// resolved).
func inWorkspace(workspace, p string) (string, bool) {
	if p == "" || strings.HasPrefix(p, "-") {
		return "", false
	}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", false
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workspace, p)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	} else if real, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		abs = filepath.Join(real, filepath.Base(abs))
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// ApplyTightening puts the project's tier A settings in cfg: rules and
// blocked paths add, limits and policy apply only when stricter. Those
// that change nothing are reported as ignored.
func (p *Project) ApplyTightening(cfg *Config) {
	for _, pf := range p.trusted {
		applied := func(kind, key, value string) {
			p.Applied = append(p.Applied, ProjectItem{File: pf.file, Kind: kind, Key: key, Value: value})
		}
		notStricter := func(kind, key, value string) {
			p.Ignored = append(p.Ignored, ProjectItem{File: pf.file, Kind: kind, Key: key, Value: value, Reason: ReasonNotStricter})
		}
		for _, r := range pf.Permissions.Deny {
			cfg.ProjectPermissions.Deny = appendNew(cfg.ProjectPermissions.Deny, r)
			applied(ProjectDeny, "", r)
		}
		for _, r := range pf.Permissions.Ask {
			cfg.ProjectPermissions.Ask = appendNew(cfg.ProjectPermissions.Ask, r)
			applied(ProjectAsk, "", r)
		}
		for _, b := range pf.Sandbox.BlockedPaths {
			cfg.Sandbox.BlockedPaths = appendNew(cfg.Sandbox.BlockedPaths, b)
			applied(ProjectBlockedPath, "", b)
		}
		// Lower limits; 0 in the user's settings means none.
		lower := func(key string, v int64, cur *int64) {
			switch {
			case v <= 0:
			case *cur == 0 || v < *cur:
				*cur = v
				applied(ProjectLimit, key, fmt.Sprint(v))
			default:
				notStricter(ProjectLimit, key, fmt.Sprint(v))
			}
		}
		lowerInt := func(key string, v int, cur *int) {
			c := int64(*cur)
			lower(key, int64(v), &c)
			*cur = int(c)
		}
		lowerInt("tools.shell_timeout_seconds", pf.Tools.ShellTimeoutSeconds, &cfg.Tools.ShellTimeoutSeconds)
		lower("tools.max_file_size_bytes", pf.Tools.MaxFileSizeBytes, &cfg.Tools.MaxFileSizeBytes)
		lowerInt("tools.max_parallel", pf.Tools.MaxParallel, &cfg.Tools.MaxParallel)

		sp := pf.Skills.Policy
		switch {
		case sp.MinHITLTier == 0:
		case sp.MinHITLTier > cfg.Skills.Policy.MinHITLTier && sp.MinHITLTier <= 3:
			cfg.Skills.Policy.MinHITLTier = sp.MinHITLTier
			applied(ProjectSkillPolicy, "skills.policy.min_hitl_tier", fmt.Sprint(sp.MinHITLTier))
		default:
			notStricter(ProjectSkillPolicy, "skills.policy.min_hitl_tier", fmt.Sprint(sp.MinHITLTier))
		}
		lowerInt("skills.policy.max_timeout_seconds", sp.MaxTimeoutSeconds, &cfg.Skills.Policy.MaxTimeoutSeconds)
		for _, d := range sp.DenyTools {
			cfg.Skills.Policy.DenyTools = appendNew(cfg.Skills.Policy.DenyTools, d)
			applied(ProjectSkillPolicy, "skills.policy.deny_tools", d)
		}

		wp := pf.Workers.Policy
		lowerInt("workers.policy.max_turns", wp.MaxTurns, &cfg.Workers.Policy.MaxTurns)
		switch {
		case wp.MaxCostUSD <= 0:
		case cfg.Workers.Policy.MaxCostUSD == 0 || wp.MaxCostUSD < cfg.Workers.Policy.MaxCostUSD:
			cfg.Workers.Policy.MaxCostUSD = wp.MaxCostUSD
			applied(ProjectWorkerLimit, "workers.policy.max_cost_usd", fmt.Sprint(wp.MaxCostUSD))
		default:
			notStricter(ProjectWorkerLimit, "workers.policy.max_cost_usd", fmt.Sprint(wp.MaxCostUSD))
		}
	}
}

// ApplyTrusted puts the project's tier B settings in cfg, once the person
// has trusted them. The user's own settings win: their hooks run first,
// their MCP server of the same name and their model choices stay.
// Writable paths outside the workspace, and models of providers the user
// hasn't set up, are ignored.
func (p *Project) ApplyTrusted(cfg *Config) {
	ignore := func(pf projectFile, kind, key, value, reason string) {
		p.Ignored = append(p.Ignored, ProjectItem{File: pf.file, Kind: kind, Key: key, Value: value, Reason: reason})
	}
	userModel := cfg.Blitz.DefaultModel
	userPins := cfg.AgentModels
	for _, pf := range p.trusted {
		cfg.Hooks = appendHooks(cfg.Hooks, pf.Hooks)
		for _, s := range pf.MCP.Servers {
			if slices.ContainsFunc(cfg.MCP.Servers, func(u MCPServerConfig) bool { return u.Name == s.Name }) {
				ignore(pf, ProjectMCP, s.Name, "", ReasonUserSet)
				continue
			}
			cfg.MCP.Servers = append(cfg.MCP.Servers, MCPServerConfig{
				Name: s.Name, Command: s.Command, Args: s.Args, Env: s.Env, URL: s.URL, Tools: s.Tools,
				Prefix: s.Prefix, Agents: s.Agents, TimeoutSeconds: s.TimeoutSeconds,
			})
		}
		for _, r := range pf.Permissions.Allow {
			cfg.ProjectPermissions.Allow = appendNew(cfg.ProjectPermissions.Allow, r)
		}
		for _, d := range pf.Sandbox.ShellWritablePaths {
			if _, ok := inWorkspace(p.dir, d); !ok {
				ignore(pf, ProjectWritable, "", d, ReasonOutside)
				continue
			}
			cfg.Sandbox.ShellWritablePaths = appendNew(cfg.Sandbox.ShellWritablePaths, d)
		}
		if m := pf.Blitz.DefaultModel; m != "" {
			switch {
			case userModel != "":
				ignore(pf, ProjectModel, "", m, ReasonUserSet)
			case !cfg.providerReady(m):
				ignore(pf, ProjectModel, "", m, ReasonProvider)
			default:
				cfg.Blitz.DefaultModel = m
			}
		}
		for _, agent := range sortedKeys(pf.AgentModels) {
			m := pf.AgentModels[agent]
			switch {
			case userPins[agent] != "":
				ignore(pf, ProjectAgentModel, agent, m, ReasonUserSet)
			case !cfg.providerReady(m):
				ignore(pf, ProjectAgentModel, agent, m, ReasonProvider)
			default:
				if cfg.AgentModels == nil {
					cfg.AgentModels = map[string]string{}
				}
				cfg.AgentModels[agent] = m
			}
		}
		for _, k := range pf.Workers.Policy.Allow {
			cfg.Workers.Policy.Allow = appendNew(cfg.Workers.Policy.Allow, k)
		}
	}
}

// providerReady reports whether ref ("provider/model", or a model of the
// configured provider) names a provider the user has set up: the one
// configured, or one with an API key.
func (c *Config) providerReady(ref string) bool {
	provider, _, ok := strings.Cut(ref, "/")
	if !ok || strings.EqualFold(provider, c.LLM.Provider) {
		return true
	}
	switch strings.ToLower(provider) {
	case "gemini", "google":
		return c.LLM.Gemini.APIKey != ""
	case "anthropic":
		return c.LLM.Anthropic.APIKey != ""
	case "openai":
		return c.LLM.OpenAI.APIKey != ""
	}
	return false
}

func appendHooks(h, add HooksConfig) HooksConfig {
	h.PreTool = append(h.PreTool, add.PreTool...)
	h.PostTool = append(h.PostTool, add.PostTool...)
	h.PromptSubmit = append(h.PromptSubmit, add.PromptSubmit...)
	h.SessionStart = append(h.SessionStart, add.SessionStart...)
	h.SessionEnd = append(h.SessionEnd, add.SessionEnd...)
	h.Stop = append(h.Stop, add.Stop...)
	h.PostToolFailure = append(h.PostToolFailure, add.PostToolFailure...)
	h.SubagentStart = append(h.SubagentStart, add.SubagentStart...)
	h.SubagentStop = append(h.SubagentStop, add.SubagentStop...)
	h.PreCompact = append(h.PreCompact, add.PreCompact...)
	h.PostCompact = append(h.PostCompact, add.PostCompact...)
	h.Notification = append(h.Notification, add.Notification...)
	h.PermissionRequest = append(h.PermissionRequest, add.PermissionRequest...)
	return h
}

func appendNew(list []string, v string) []string {
	if slices.Contains(list, v) {
		return list
	}
	return append(list, v)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
