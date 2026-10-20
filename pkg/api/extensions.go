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

package api

import (
	"errors"
	"time"
)

// SkillInfo describes a skill and what the skills policy lets it do.
type SkillInfo struct {
	Name          string
	Description   string
	Version       string
	License       string
	Category      string
	Compatibility string
	Tags          []string
	// Hash identifies the skill's content, for pinning it in the policy
	// ("" if it couldn't be computed).
	Hash string
	// Tools are the tools the skill says it needs, and why.
	Tools []SkillTool
	// Scripts are the skill's scripts and whether each may run.
	Scripts []SkillScript
	// Tier is the approval tier the scripts run at ("" without scripts);
	// Bypass means the skill may skip approvals (tier 0) and the policy
	// allows it.
	Tier   string
	Bypass bool
	// Network means the scripts may use the network. NeedsNetwork means the
	// skill asks for it (granted or not).
	Network      bool
	NeedsNetwork bool
	// Env are the host environment variables passed to the scripts;
	// Withheld are the ones the skill asked for that the policy keeps back.
	Env, Withheld []string
	// Blocked are reasons none of the scripts may run.
	Blocked []string
	// Dir is the skill's folder on disk ("" for a built-in skill).
	Dir string
	// Kind is simple, references, scripts or meta, from its folder
	// (spec_skills_013 SK-97).
	Kind string
	// Agents are the agents it's offered to (none: every one); Members the
	// skills a meta skill combines; References its files under
	// references/.
	Agents, Members, References []string
	// PackageFiles are its package files (requirements.txt,
	// pyproject.toml, package.json), as paths in its folder.
	PackageFiles []string
	// Project means the workspace's own: its folder is in the workspace,
	// so Studio edits it.
	Project bool
	// NeedsReview means its scripts, on disk, weren't reviewed and trusted
	// with this content (new, changed, a registry's): each run asks first.
	NeedsReview bool
	// Triggers are its trigger phrases (spec_skills_013 SK-110);
	// Scenarios how many scenarios/*.md it carries (SK-111).
	Triggers  []string
	Scenarios int
}

// SkillTool is a tool a skill needs.
type SkillTool struct {
	Name   string
	Scopes []string
	Why    string
}

// SkillScript is one of a skill's scripts and the policy's verdict on it.
type SkillScript struct {
	Name     string
	Language string
	Source   string // its path in the skill, or "inline"
	Timeout  time.Duration
	Deps     []string
	Allowed  bool
	Reasons  []string // why it may not run
	// Found means it isn't declared but was found in scripts/ (SK-98).
	Found bool
}

// SkillSpec is what Studio's skill form edits (SK-100): the skill's name
// (its folder's), what it's for, the tools it may use (allowed-tools,
// space-separated), the agents it's offered to, the skills it combines
// and its instructions.
type SkillSpec struct {
	Name, Description, Tools string
	Agents, Members          []string
	Instructions             string
	Triggers                 []string
}

// SkillFile is a skill as Studio edits it: what ListSkills says of it,
// its SKILL.md (path, content hash), the form's fields as written, and
// whether it's the workspace's own, which may be edited.
type SkillFile struct {
	Info     SkillInfo
	Path     string
	Hash     string
	Spec     SkillSpec
	Editable bool
}

// ScriptRun is how a skill's script ran from Studio's Test run.
type ScriptRun struct {
	ExitCode       int
	Stdout, Stderr string
	TimedOut       bool
	Sandbox        string
	Files          []string
	Error          string
}

// Runnable reports whether any of the skill's scripts may run.
func (s SkillInfo) Runnable() bool {
	for _, sc := range s.Scripts {
		if sc.Allowed {
			return true
		}
	}
	return false
}

// ErrScriptsDisabled reports that skill scripts (and so their
// environments) are turned off.
var ErrScriptsDisabled = errors.New("skill scripts are disabled")

// Env is an isolated Python environment built for skill scripts.
type Env struct {
	Key      string
	Deps     []string
	Skills   []string // the skills that used it
	Size     int64    // bytes on disk
	LastUsed time.Time
	Ready    bool // built completely
}

// EnvError is an environment that couldn't be removed.
type EnvError struct {
	Key string
	Err error
}

// PruneResult is what PruneEnvs removed.
type PruneResult struct {
	Removed int
	Freed   int64 // bytes
	Failed  []EnvError
}

// MCPServer is a configured MCP server.
type MCPServer struct {
	Name string
	// Target is its URL, or the command that starts it.
	Target      string
	AutoApprove bool
	// Disabled means it's configured but never started (disabled = true).
	Disabled bool
	// State is "disabled", "idle" (its tools not listed yet), "connected"
	// or "failed" (Error says why); Tools is how many tools it offered.
	State string
	Error string
	Tools int
}

// ToolInfo is a tool an agent can call.
type ToolInfo struct {
	Name        string
	Description string
	// PlanAllowed means the tool stays available in plan mode, which refuses
	// tools that change anything.
	PlanAllowed bool
}

// MCPOffer is an MCP server offered to an agent.
type MCPOffer struct {
	Server string
	Tools  []string // the tools offered; empty means all of them
	Prefix string   // prepended to tool names ("" for none)
}

// AgentTools is what an agent can use.
type AgentTools struct {
	Agent string
	Tools []ToolInfo // by name
	MCP   []MCPOffer
}

// CommandInfo describes a custom slash command: a command file, a bundled
// command, or a skill run by name.
type CommandInfo struct {
	Name         string
	Description  string
	ArgumentHint string
	Source       string // project, user, bundled, skill, or "plugin <name>"
}

// ErrUnknownCommand reports a slash command that doesn't exist.
var ErrUnknownCommand = errors.New("unknown command")
