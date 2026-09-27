package api

import (
	"context"

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
	AddPermissionRule(effect, rule string, save bool) (PermissionChange, error)
	RemovePermissionRule(rule string, save bool) (PermissionChange, error)

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

// ProcessInfo is a snapshot of a background process's status.
type ProcessInfo struct {
	ID        int    `json:"id"`
	Command   string `json:"command"`
	Running   bool   `json:"running"`
	ExitCode  int    `json:"exit_code"`
	RuntimeMs int64  `json:"runtime_ms"`
}
