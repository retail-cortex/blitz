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
	// Network: the scripts may use the network. NeedsNetwork: the skill
	// asks for it (granted or not).
	Network      bool
	NeedsNetwork bool
	// Env are the host environment variables passed to the scripts;
	// Withheld are the ones the skill asked for that the policy keeps back.
	Env, Withheld []string
	// Blocked are reasons none of the scripts may run.
	Blocked []string
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
}

// ToolInfo is a tool an agent can call.
type ToolInfo struct {
	Name        string
	Description string
	// PlanAllowed: the tool stays available in plan mode, which refuses
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
	Source       string // project, user, bundled, skill
}

// ErrUnknownCommand reports a slash command that doesn't exist.
var ErrUnknownCommand = errors.New("unknown command")
