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
	"context"
	"errors"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/images"
)

// Backend is what a front end drives: a *Workspace in this process, or a
// workspace held by the Blitz service (through the client). Front ends
// use nothing else, so they work the same either way.
type Backend interface {
	// Dir is the workspace directory.
	Dir() string
	// ModelErr is why the configured model couldn't be built (nil if it
	// could); the workspace then runs on a placeholder.
	ModelErr() error
	// SetUI routes the agent's approval requests and questions to the
	// front end.
	SetUI(approve Approver, ask UserPromptFunc)
	// Processes are the background processes to account for when the front
	// end exits; nil when they don't belong to it (a remote workspace).
	Processes() Processes
	// AuditShell records a command the user ran directly (!cmd), with why
	// it didn't start if it didn't.
	AuditShell(command string, exitCode int, startErr error)
	// ImagesEnabled reports whether images can be attached.
	ImagesEnabled() bool
	Close() error

	// Background tasks (invoke_agent with background: true) of the
	// sessions this front end ran turns in, oldest first; Task adds its
	// latest events, in words.
	ListTasks() []TaskInfo
	Task(id string) (TaskInfo, []string, error)
	StopTask(id string) (TaskInfo, error)
	// PendingTaskRequests are their approval requests and questions
	// waiting for an answer; AnswerTaskRequest answers one (a decision
	// for an approval, text for a question).
	PendingTaskRequests() []TaskRequest
	AnswerTaskRequest(id string, decision Decision, answer string) error

	// The configured hooks, by event, with recent failures (/hooks).
	ListHooks() []HookInfo

	// Notes the agent saved across sessions (remember), newest first;
	// ForgetNote deletes one.
	ListNotes() ([]Note, error)
	ForgetNote(name string) error

	// Project settings.
	ProjectSettings() ProjectSettings
	// TrustProject records trusting (or declining) the project settings
	// whose hash is given (ErrProjectChanged if they have changed since).
	// They load when the workspace next opens.
	TrustProject(hash string, trusted bool) error

	// Turns.
	Run(ctx context.Context, sessionID string, t Turn, on func(Event)) (TurnResult, error)
	Steer(ctx context.Context, sessionID, text string) error

	// Sessions.
	OpenSession(resume string, cont bool) (SessionInfo, bool, error)
	ActiveSession() (SessionInfo, bool)
	ListSessions(all bool) ([]SessionInfo, error)
	NewSession() (SessionInfo, error)
	LoadSession(ref string) (SessionInfo, bool, error)
	SaveSnapshot(name string, force bool) (SessionInfo, error)
	RenameSession(title string) (SessionInfo, error)
	// MoveSession carries session id into this workspace from the one it
	// was in, and makes it active (/cd).
	MoveSession(id string) (SessionInfo, error)

	// Agents, models and settings.
	ListAgents() []AgentInfo
	ActiveAgent() AgentInfo
	SetAgent(ctx context.Context, name string) (AgentInfo, error)
	Model() ModelInfo
	SetModel(ctx context.Context, ref string) (string, error)
	PinModel(ctx context.Context, agent, ref string) (PinResult, error)
	Unpin(ctx context.Context, agent string) (PinResult, error)
	AllModelSettings() map[string]config.ModelSettings
	ModelSettings(ref string) (ModelSettingsInfo, error)
	UpdateModelSettings(ref string, reset bool, changes []Setting) (ModelSettingsChange, error)
	Settings() Settings
	Set(ctx context.Context, key, value string) (string, error)
	SetPermissionMode(mode string) (string, error)
	ListPermissionRules() []PermissionRule
	AddPermissionRule(effect, rule string, save Scope) (PermissionChange, error)
	RemovePermissionRule(rule string, save Scope) (PermissionChange, error)

	// Skills, environments, MCP and tools.
	ListSkills() []SkillInfo
	SearchSkills(query string) []SkillInfo
	Skill(name string) (SkillInfo, bool)
	ListEnvs() ([]Env, error)
	RemoveEnv(key string) error
	PruneEnvs() (PruneResult, error)
	ListMCPServers() []MCPServer
	ListCommands() []CommandInfo
	ActiveAgentTools() AgentTools

	// Context, memory and language.
	SessionUsage() (Usage, error)
	Context() (ContextInfo, error)
	Compact(ctx context.Context, focus string) (CompactResult, error)
	MemoryFiles() []string
	ReloadMemory(ctx context.Context) ([]string, error)
	AddMemory(ctx context.Context, text string) (string, error)
	AvailableLocales() ([]LocaleInfo, string)
	SetLocale(ctx context.Context, input string) (LocaleChange, error)
	SandboxSummary() []string

	// Checkpoints and approvals.
	ListCheckpoints() []Checkpoint
	Undo(force bool) (UndoResult, error)
	RewindPoints() ([]RewindPoint, error)
	Rewind(ctx context.Context, index int, mode RewindMode, force bool) (RewindResult, error)
	SessionDiff() string
	GitDiff(ctx context.Context, color bool) (string, error)
	ListApprovals() []Approval
	RevokeApprovals(keys ...string) int
	ClearApprovals() int

	// Images and search.
	LoadImage(path string) (*images.Image, error)
	AddImage(name string, data []byte) (*images.Image, error)
	LoadAttachments(paths []string, prompt string, warn func(string)) ([]*images.Image, error)
	SearchProvider() (string, error)
	SearchWeb(ctx context.Context, terms string) (WebSearch, error)
	SearchSession(terms string) (int, string)
}

// Processes are a workspace's background processes, as a front end sees
// them when it exits: which still run, waiting for them, or stopping them.
type Processes interface {
	Running() []ProcessInfo
	WaitAll(ctx context.Context) error
	Shutdown()
}

// ProjectSettings are what a workspace's project files
// (.blitz/settings.toml, .blitz/settings.local.toml) say, and whether the
// person trusts the part that needs it (spec_project_config_031).
type ProjectSettings struct {
	// Files are the project files found, relative to the workspace.
	Files []string
	// State is the trust decision for the content as it is now: none
	// (nothing needs trust), new, changed, trusted or declined.
	State string
	// Hash identifies the content that needs trust; TrustProject takes it
	// back.
	Hash string
	// Loaded: the settings that need trust are in force (trusted when the
	// workspace opened, or for this run only).
	Loaded bool
	// Applied are the settings in force without trust: they only tighten.
	Applied []ProjectItem
	// Pending need trust: they run code or loosen a policy.
	Pending []ProjectItem
	// Ignored may not come from a project, or change nothing.
	Ignored []ProjectItem
	// Problems are files that couldn't be read.
	Problems []string
}

// ProjectItem is one project setting. Kind is deny, ask, blocked_path,
// limit, skill_policy, worker_limit (applied); hook, mcp, allow, writable,
// model, agent_model, worker_allow, skill_scripts (needing trust); or
// setting (another key).
type ProjectItem struct {
	File  string
	Kind  string
	Key   string // the hook's event, the server's, agent's or skill's name, or the setting
	Value string // the rule, command, path, model or number
	// Reason, for an ignored item: never, unknown, not_stricter, outside,
	// provider or user_set.
	Reason string
}

// The trust states of ProjectSettings.State.
const (
	TrustNone     = "none"
	TrustNew      = "new"
	TrustChanged  = "changed"
	TrustTrusted  = "trusted"
	TrustDeclined = "declined"
)

// ErrProjectChanged: the project settings changed since they were shown;
// review them again.
var ErrProjectChanged = errors.New("the project settings changed since they were shown")

// ErrUnknownProcess: no background process with that ID was started for
// the sessions asking.
var ErrUnknownProcess = errors.New("no such background process")

// ProcessInfo is a snapshot of a background process's status.
type ProcessInfo struct {
	ID        int    `json:"id"`
	Command   string `json:"command"`
	Running   bool   `json:"running"`
	ExitCode  int    `json:"exit_code"`
	RuntimeMs int64  `json:"runtime_ms"`
}
