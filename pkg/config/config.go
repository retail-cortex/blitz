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

// Package config loads Blitz's settings: one TOML file, .env.toml, in
// ~/.blitz (or $MODENV_PREFIX, or --config), with a workspace's own
// settings laid over it from ~/.blitz/workspaces, API keys resolved from
// the OS keychain, and environment variables applied last. Nothing is
// read from the workspace itself, so a cloned repository can't redirect
// a key or turn off approvals. It also edits the file in place, a line at
// a time, for the settings the front ends change (spec_config_002).
package config

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	anthropicconfig "github.com/anthropics/anthropic-sdk-go/config"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/rrmcguinness/modenv/pkg/modenv"
)

// AgencyLevel determines how autonomously the agent operates.
type AgencyLevel string

const (
	AgencyLow     AgencyLevel = "low"     // Stop after each meaningful unit of work and ask
	AgencyMedium  AgencyLevel = "medium"  // Complete routine tasks; pause at major milestones
	AgencyHigh    AgencyLevel = "high"    // Complete tasks autonomously; ask only when blocked
	AgencyExtreme AgencyLevel = "extreme" // Maximally autonomous, polling background tasks
)

// Config represents the complete Blitz configuration.
type Config struct {
	Blitz   BlitzConfig   `toml:"blitz"`
	LLM     LLMConfig     `toml:"llm"`
	Skills  SkillsConfig  `toml:"skills"`
	Workers WorkersConfig `toml:"workers"`
	Tools   ToolsConfig   `toml:"tools"`
	Session SessionConfig `toml:"session"`
	Sandbox SandboxConfig `toml:"sandbox"`
	// Permissions are allow, ask and deny rules for actions, as
	// "kind(pattern)" (see tools.ParsePermissionRule).
	Permissions PermissionsConfig `toml:"permissions"`

	UI          UIConfig         `toml:"ui"`
	Memory      MemoryConfig     `toml:"memory"`
	Context     ContextConfig    `toml:"context"`
	Audit       AuditConfig      `toml:"audit"`
	Checkpoints CheckpointConfig `toml:"checkpoints"`
	Images      ImagesConfig     `toml:"images"`
	Hooks       HooksConfig      `toml:"hooks"`
	MCP         MCPConfig        `toml:"mcp"`
	Web         WebConfig        `toml:"web"`
	Browser     BrowserConfig    `toml:"browser"`
	Plugins     PluginsConfig    `toml:"plugins"`
	Network     NetworkConfig    `toml:"network"`
	// LSP are the language servers the lsp tool uses, by language: the
	// built-in ones (go, typescript, python, rust), changed or turned off,
	// and others.
	LSP map[string]LSPServerConfig `toml:"lsp"`
	// PluginAgentDirs and PluginCommandDirs are what loaded plugins add
	// (package plugins; never read from a file).
	PluginAgentDirs   []string        `toml:"-"`
	PluginCommandDirs []PluginDir     `toml:"-"`
	Log               LogConfig       `toml:"log"`
	Telemetry         TelemetryConfig `toml:"telemetry"`
	// Pricing overrides or adds model prices, keyed by model name, for
	// /cost and cost limits.
	Pricing map[string]ModelPrice `toml:"pricing"`
	// SearchPricing prices web search providers' queries, keyed by
	// provider ("google"), for /cost; DefaultSearchPricing fills the rest.
	SearchPricing map[string]SearchPrice `toml:"search_pricing"`
	// AgentModels pins agents to models: agent name -> "provider/model".
	// A pin wins over the agent's own default_model; unpinned agents use
	// the configured model. Set with /pin_model, removed with /unpin.
	AgentModels map[string]string `toml:"agent_models"`
	// ModelSettings are per-model generation settings, keyed by model name
	// (a "provider/" prefix is ignored). They win over the global
	// temperature and max_tokens. Set with /model_settings.
	ModelSettings map[string]ModelSettings `toml:"model_settings"`

	// Dir is the settings directory this was loaded from ("" for none).
	Dir string `toml:"-"`
	// Project is what the workspace's project files say
	// (.blitz/settings.toml), for LoadWorkspace; nil otherwise. Its tier
	// A settings are applied; tier B only once trusted (ApplyTrusted).
	Project *Project `toml:"-"`
	// ProjectPermissions are the project files' rules, kept apart from
	// the user's so they're listed as the project's.
	ProjectPermissions PermissionsConfig `toml:"-"`
}

// BlitzConfig controls the core behaviour settings.
type BlitzConfig struct {
	// DefaultAgent is the agent a session starts with.
	DefaultAgent string `toml:"default_agent"`
	// DefaultModel, when set, is the model to use instead of the provider's
	// own (llm.<provider>.model); "provider/model" picks another provider.
	DefaultModel string `toml:"default_model"`
	// AgencyLevel is how far the agent goes before checking in: low, medium,
	// high or extreme.
	AgencyLevel string `toml:"agency_level"`
	// Temperature is the sampling temperature, unless [model_settings] sets
	// one for the model.
	Temperature float64 `toml:"temperature"`
	// MaxTokens caps the tokens in one model response, unless [model_settings]
	// sets it for the model.
	MaxTokens int `toml:"max_tokens"`
	// AutoApprove is the older spelling of PermissionMode = "bypass".
	AutoApprove bool `toml:"auto_approve"`
	// PermissionMode is the starting permission mode: default,
	// accept-edits, plan, dont-ask or bypass (which needs the OS sandbox).
	PermissionMode string `toml:"permission_mode"`
	// PlanReview decides when the agent plans for approval before changing
	// anything: always (every prompt), agent-decides (it may choose to, with
	// enter_plan_mode) or never (only in plan mode and /plan).
	PlanReview string `toml:"plan_review"`
	// TrustWorkspace is deprecated: it trusts the workspace's project
	// settings (.blitz/settings.toml, its skills' scripts) as they are, for
	// each run, without recording it. Use blitz trust instead. Only the
	// user's own settings or --trust-workspace can set it.
	TrustWorkspace bool `toml:"trust_workspace"`
	// GoalMaxContinues caps how often a /goal sends the agent on before it
	// stops to ask.
	GoalMaxContinues int `toml:"goal_max_continues"`
}

// LLMConfig holds provider configurations for LLM backends.
type LLMConfig struct {
	// Provider is the model provider: gemini, openai, anthropic or ollama.
	Provider string `toml:"provider"`
	// MaxRetries is how often a failed model request is retried (rate
	// limits, overload, 5xx, connection errors), with exponential backoff
	// that honours Retry-After. 0 disables retries.
	MaxRetries int `toml:"max_retries"`
	// StallTimeoutSeconds fails a model request that sends nothing (no
	// headers, or no streamed data) for this long, so a hung connection
	// can't block a turn forever. It must exceed the longest non-streamed
	// generation.
	StallTimeoutSeconds int `toml:"stall_timeout_seconds"`
	// FallbackModels are tried in order when the model in use fails before
	// answering (after its retries): "provider/model" (e.g.
	// "anthropic/claude-sonnet-5") or just "model" for the same provider.
	// Credentials come from each provider's own section. For names that
	// contain a slash (OpenRouter), write the provider first:
	// "openai/anthropic/claude-sonnet-5".
	FallbackModels []string `toml:"fallback_models"`

	Gemini    GeminiConfig    `toml:"gemini"`
	OpenAI    OpenAIConfig    `toml:"openai"`
	Anthropic AnthropicConfig `toml:"anthropic"`
	Bedrock   BedrockConfig   `toml:"bedrock"`
	Azure     AzureConfig     `toml:"azure"`
}

// BedrockConfig holds settings for Claude on Amazon Bedrock. Credentials
// come from AWS's standard chain: the environment, the shared config and
// credentials files (Profile), SSO, and the instance or task role; or a
// Bedrock API key in AWS_BEARER_TOKEN_BEDROCK.
type BedrockConfig struct {
	// Region is the AWS region (default: AWS_REGION, then the profile's).
	Region string `toml:"region"`
	// Profile is the AWS profile to use (default: AWS_PROFILE, "default").
	Profile string `toml:"profile"`
	// Model is the Bedrock model or inference profile ID, e.g.
	// us.anthropic.claude-sonnet-4-5-20250929-v1:0.
	Model string `toml:"model"`
}

// AzureConfig holds settings for models on Azure: OpenAI models through
// Azure OpenAI's v1 API, and Claude through Microsoft Foundry (a model
// whose name starts with "claude"). Model is the deployment name.
type AzureConfig struct {
	// Resource is the Azure resource name: <resource>.openai.azure.com
	// and <resource>.services.ai.azure.com.
	Resource string `toml:"resource"`
	// BaseURL replaces the address made from Resource for OpenAI models
	// (it ends in /openai/v1/), and AnthropicBaseURL for Claude (it ends in
	// /anthropic/).
	BaseURL          string `toml:"base_url"`
	AnthropicBaseURL string `toml:"anthropic_base_url"`
	// APIKey is the resource's key (default: AZURE_OPENAI_API_KEY, or
	// ANTHROPIC_FOUNDRY_API_KEY for Claude). Auth "entra" uses Entra ID
	// instead: Azure's credential chain (environment, workload and managed
	// identity, the Azure CLI's login).
	APIKey string `toml:"api_key"`
	Auth   string `toml:"auth"`
	// Model is the deployment to use.
	Model string `toml:"model"`
}

// AuthEntra is Azure's Entra ID sign-in.
const AuthEntra = "entra"

// How a provider authenticates: an API key (the default, also for ""),
// Google Cloud's Application Default Credentials (Gemini through Vertex
// AI), or an Anthropic OAuth profile from `ant auth login`.
const (
	AuthAPIKey = "api_key"
	AuthADC    = "adc"
	AuthOAuth  = "oauth"
)

// GeminiConfig holds settings for Google Gemini / Vertex AI.
type GeminiConfig struct {
	// APIKey is the Gemini API key (default: GEMINI_API_KEY). Keep it in the
	// keychain: set it in the form or with blitz config set-key.
	APIKey string `toml:"api_key"`
	// APIKeyCommand, when APIKey is empty, is a command whose output is the
	// key (a password manager, a gateway's rotating token), run when needed
	// and kept for APIKeyTTL (a Go duration; default 5m).
	APIKeyCommand string `toml:"api_key_command"`
	APIKeyTTL     string `toml:"api_key_ttl"`
	// Model is the Gemini model to use.
	Model string `toml:"model"`
	// Auth is "api_key" ("" too: the Gemini API) or "adc": Vertex AI with
	// Application Default Credentials (`gcloud auth application-default
	// login`, or a service account), in ProjectID and Location.
	Auth string `toml:"auth"`
	// ProjectID and Location are Vertex AI's; empty, they come from
	// GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION (Location: "global").
	ProjectID string `toml:"project_id"`
	Location  string `toml:"location"`
}

// UsesADC reports whether Gemini authenticates with Application Default
// Credentials, through Vertex AI.
func (g GeminiConfig) UsesADC() bool { return g.Auth == AuthADC }

// OpenAIConfig holds settings for OpenAI, OpenRouter, or Ollama.
type OpenAIConfig struct {
	// APIKey is the OpenAI or OpenRouter API key (default: OPENAI_API_KEY;
	// Ollama needs none). Keep it in the keychain: set it in the form or with
	// blitz config set-key.
	APIKey string `toml:"api_key"`
	// APIKeyCommand, when APIKey is empty, is a command whose output is the
	// key (a password manager, a gateway's rotating token), run when needed
	// and kept for APIKeyTTL (a Go duration; default 5m).
	APIKeyCommand string `toml:"api_key_command"`
	APIKeyTTL     string `toml:"api_key_ttl"`
	// BaseURL is the API's address: OpenAI's, OpenRouter's or a local
	// Ollama's.
	BaseURL string `toml:"base_url"`
	// Model is the model to use.
	Model string `toml:"model"`
}

// AnthropicConfig holds settings for Anthropic Claude.
type AnthropicConfig struct {
	// APIKey is optional: without it the SDK also accepts ANTHROPIC_AUTH_TOKEN
	// and `ant auth login` profiles.
	APIKey string `toml:"api_key"`
	// APIKeyCommand, when APIKey is empty, is a command whose output is the
	// key (a password manager, a gateway's rotating token), run when needed
	// and kept for APIKeyTTL (a Go duration; default 5m).
	APIKeyCommand string `toml:"api_key_command"`
	APIKeyTTL     string `toml:"api_key_ttl"`
	// Auth is "api_key" ("" too); "oauth": the `ant auth login` profile
	// named by Profile (empty: ant's active profile); or "adc": Claude on
	// Vertex AI with Application Default Credentials, in ProjectID and
	// Location. With either, any API key in the settings or the environment
	// is ignored.
	Auth    string `toml:"auth"`
	Profile string `toml:"profile"`
	// ProjectID and Location are Vertex AI's (auth = "adc"); empty, they
	// come from GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION ("global").
	ProjectID string `toml:"project_id"`
	Location  string `toml:"location"`
	// Model is the Claude model to use.
	Model   string `toml:"model"`
	BaseURL string `toml:"base_url"` // for gateways/proxies; empty uses the API default
	// Fallbacks controls refusal fallback: server-side on models that
	// support it (claude-opus-5, claude-fable-5*), where "default" routes
	// by refusal category; client-side on Vertex AI, where "default" is
	// claude-opus-4-8. A model ID pins one fallback model; "off" disables it.
	Fallbacks string `toml:"fallbacks"`
}

// UsesOAuth reports whether Claude authenticates with an `ant auth login`
// OAuth profile rather than an API key.
func (a AnthropicConfig) UsesOAuth() bool { return a.Auth == AuthOAuth }

// UsesADC reports whether Claude runs on Vertex AI with Application
// Default Credentials.
func (a AnthropicConfig) UsesADC() bool { return a.Auth == AuthADC }

// OAuthProfile is the `ant auth login` profile Claude signs in with, and
// the directory ant keeps it in: Profile, else the one ant would use
// (ANTHROPIC_PROFILE, its active profile, "default").
func (a AnthropicConfig) OAuthProfile() (dir, name string) {
	dir = anthropicconfig.DefaultDir()
	if a.Profile != "" {
		return dir, a.Profile
	}
	active, _ := os.ReadFile(anthropicconfig.ActiveConfigPath(dir))
	return dir, cmp.Or(os.Getenv("ANTHROPIC_PROFILE"), strings.TrimSpace(string(active)), "default")
}

// ModelName returns the model to use: blitz.default_model when set,
// otherwise the active provider's model.
func (c *Config) ModelName() string {
	if c.Blitz.DefaultModel != "" {
		return c.Blitz.DefaultModel
	}
	switch strings.ToLower(c.LLM.Provider) {
	case "openai", "ollama":
		return c.LLM.OpenAI.Model
	case "anthropic", "vertex-anthropic":
		return c.LLM.Anthropic.Model
	case "bedrock":
		return c.LLM.Bedrock.Model
	case "azure":
		return c.LLM.Azure.Model
	default:
		return c.LLM.Gemini.Model
	}
}

// SkillsConfig controls Agent Skills discovery and paths.
type SkillsConfig struct {
	// Enabled loads Agent Skills.
	Enabled bool `toml:"enabled"`
	// Paths are the directories searched for skills (<name>/SKILL.md).
	Paths []string `toml:"paths"`
	// Policy caps what skills may ask for; a skill can make its own
	// settings stricter but never looser.
	Policy SkillPolicy `toml:"policy"`
}

// WorkersConfig controls workers: scheduled workflows a workspace defines
// in workers/<name>/WORKER.md (see pkg/engine/workers).
type WorkersConfig struct {
	// Enabled lets workers run.
	Enabled bool `toml:"enabled"`
	// Paths are the directories holding worker directories, relative to
	// the workspace; a name in an earlier one wins. New workers go in the
	// first.
	Paths []string `toml:"paths"`
	// Policy caps what any worker may do and spend.
	Policy WorkerPolicy `toml:"policy"`
}

// WorkerPolicy is the host's limit on workers. A worker asks for
// permissions and limits; the policy removes what it doesn't allow and
// caps the rest.
type WorkerPolicy struct {
	// Allow are the permission kinds workers may be given: shell, write,
	// delete, web, mcp.
	Allow []string `toml:"allow"`
	// Default limits fill in what a worker leaves out; Max caps them.
	DefaultMaxTurns   int     `toml:"default_max_turns"`
	DefaultMaxCostUSD float64 `toml:"default_max_cost_usd"`
	DefaultTimeout    string  `toml:"default_timeout"`
	MaxTurns          int     `toml:"max_turns"`
	MaxCostUSD        float64 `toml:"max_cost_usd"`
	MaxTimeout        string  `toml:"max_timeout"`
	// MaxConcurrent is how many worker runs may go at once, across
	// workspaces.
	MaxConcurrent int `toml:"max_concurrent"`
}

// SkillPolicy is the host's limit on what skills' scripts may do. For each
// setting the stricter of the skill's request and this policy applies.
type SkillPolicy struct {
	// MinHITLTier is the lowest approval tier a skill gets (0-3): 1 runs
	// automatically with an audit entry, 2 also takes a checkpoint, 3 asks
	// every time. A skill may ask for a higher tier, never a lower one.
	MinHITLTier int `toml:"min_hitl_tier"`
	// AllowHITLBypass lets a skill that declares tier 0 and
	// allow_hitl_bypass skip approval (e.g. in CI). Off: tier 0 is tier 3.
	AllowHITLBypass bool `toml:"allow_hitl_bypass"`
	// Languages are the script languages that may run: python, typescript.
	Languages []string `toml:"languages"`
	// Sandbox runs scripts under gvisor, the OS sandbox (os), or whichever
	// is available (auto).
	Sandbox string `toml:"sandbox"`
	// Network is "none" (no script gets the network) or "allowlist" (only
	// skills in NetworkAllow that ask for it).
	Network      string   `toml:"network"`
	NetworkAllow []string `toml:"network_allow"`
	// EnvPassthrough lists the environment variables (globs allowed) a
	// skill may receive from the host when it asks for them. Nothing else
	// is passed.
	EnvPassthrough []string `toml:"env_passthrough"`
	// MaxTimeoutSeconds caps every script's run time.
	MaxTimeoutSeconds int `toml:"max_timeout_seconds"`
	// TrustedHashes, when set, are the only skill contents (ContentHash,
	// "sha256:…") whose scripts may run.
	TrustedHashes []string `toml:"trusted_hashes"`
	// DenyTools refuses tool requirements by tool or "tool:scope" (globs,
	// e.g. "Bash:sudo*"); scripts of a skill that needs one don't run.
	DenyTools []string `toml:"deny_tools"`
	// Packages limits script dependencies.
	Packages PackagePolicy `toml:"packages"`
	// UntrustedRoots are directories whose skills' scripts don't run: the
	// workspace, while its project settings aren't trusted.
	UntrustedRoots []string `toml:"-"`
}

// Problems reports settings the policy can't honour as written.
func (p SkillPolicy) Problems() []string {
	var probs []string
	if p.MinHITLTier < 0 || p.MinHITLTier > 3 {
		probs = append(probs, fmt.Sprintf("skills.policy.min_hitl_tier = %d; use 0-3", p.MinHITLTier))
	}
	switch strings.ToLower(p.Sandbox) {
	case "", "auto", "gvisor", "os":
	default:
		probs = append(probs, fmt.Sprintf("skills.policy.sandbox = %q; use auto, gvisor or os", p.Sandbox))
	}
	switch strings.ToLower(p.Network) {
	case "", "none", "allowlist":
	default:
		probs = append(probs, fmt.Sprintf("skills.policy.network = %q; use none or allowlist", p.Network))
	}
	if len(p.NetworkAllow) > 0 && !strings.EqualFold(p.Network, "allowlist") {
		probs = append(probs, "skills.policy.network_allow is ignored unless network = \"allowlist\"")
	}
	for _, l := range p.Languages {
		if !strings.EqualFold(l, "python") && !strings.EqualFold(l, "typescript") {
			probs = append(probs, fmt.Sprintf("skills.policy.languages: unknown language %q (python, typescript)", l))
		}
	}
	if p.MaxTimeoutSeconds < 0 {
		probs = append(probs, "skills.policy.max_timeout_seconds can't be negative")
	}
	return probs
}

// PackagePolicy limits the packages skill scripts may install.
type PackagePolicy struct {
	Index string `toml:"index"` // package index URL
	// WheelsOnly installs pre-built packages only, so no package code runs
	// during installation.
	WheelsOnly bool `toml:"wheels_only"`
	// RequireHashes requires every dependency to be pinned with "==", so
	// installs can be checked against hashes.
	RequireHashes bool     `toml:"require_hashes"`
	Allow         []string `toml:"allow"` // package names (globs); empty allows any not denied
	Deny          []string `toml:"deny"`  // package names (globs)
}

// ToolsConfig configures shell execution, file operations, and permissions.
type ToolsConfig struct {
	// ShellTimeoutSeconds stops a shell command that runs longer than this.
	ShellTimeoutSeconds int `toml:"shell_timeout_seconds"`
	// MaxFileSizeBytes is the largest file the file tools read or write.
	MaxFileSizeBytes int64 `toml:"max_file_size_bytes"`
	// WorkspaceDir is the workspace: the directory the tools work in (the
	// --dir flag, else the current directory).
	WorkspaceDir string `toml:"workspace_dir"`
	// AutoApproveCommands runs shell commands without asking (the sandbox and
	// deny rules still apply).
	AutoApproveCommands bool `toml:"auto_approve_commands"`
	// UCToolsDir is where the Universal Constructor (helios) keeps the tools
	// it creates.
	UCToolsDir string `toml:"uc_tools_dir"`
	// ApprovalsFile stores "always allow" decisions.
	ApprovalsFile string `toml:"approvals_file"`
	// MaxParallel caps how many tool calls from one model response run at
	// once (0 = unlimited). Nested batches (sub-agents) get their own cap.
	MaxParallel int `toml:"max_parallel"`
	// MaxBackgroundAgents caps how many background tasks
	// (invoke_agent with background: true) run at once in a workspace.
	MaxBackgroundAgents int `toml:"max_background_agents"`
	// BackgroundAgentTimeout stops a background task that runs longer
	// (a Go duration such as "30m").
	BackgroundAgentTimeout string `toml:"background_agent_timeout"`
	// BackgroundAgentMaxTurns caps a background task's model calls, unless
	// its agent sets max_turns.
	BackgroundAgentMaxTurns int `toml:"background_agent_max_turns"`
	// BackgroundAgentMaxCostUSD stops a background task that has cost more
	// (0: no cap).
	BackgroundAgentMaxCostUSD float64 `toml:"background_agent_max_cost_usd"`
}

// PermissionsConfig lists permission rules. Deny wins over ask, ask over
// allow: an allow rule lets a matching action run without asking; an ask
// rule makes it ask even when a mode or a saved approval would let it
// through; a deny rule refuses it in every mode. (Unlike
// sandbox.commands.allow, which is an allow-list of the only commands
// that may run, permissions.allow only skips the question.)
//
// A workspace's rules add to the global ones (Merge).
type PermissionsConfig struct {
	// Allow rules let matching actions run without asking.
	Allow []string `toml:"allow"`
	// Ask rules make matching actions always ask.
	Ask []string `toml:"ask"`
	// Deny rules refuse matching actions in every mode.
	Deny []string `toml:"deny"`
	// ReadOnlyDefaults allows ReadOnlyCommands without asking (with
	// ReadOnlyGuards asking even so); nil means on. A workspace may set it
	// either way.
	ReadOnlyDefaults *bool `toml:"read_only_defaults"`
	// Auto configures the reviewer of the auto permission mode.
	Auto AutoReviewConfig `toml:"auto"`
}

// AutoReviewConfig is the reviewer model of the auto permission mode: it
// decides what would otherwise ask the user.
type AutoReviewConfig struct {
	// Model reviews the actions ("provider/model"); empty: the session's
	// model. A small, fast model is enough.
	Model string `toml:"model"`
	// Environment tells the reviewer what to trust: repositories, hosts
	// and paths the agent may use freely, and anything else it should
	// know.
	Environment string `toml:"environment"`
}

// ReadOnlyCommands are the commands that read and don't change anything,
// with any arguments: allowed without asking unless read_only_defaults is
// false. Commands that can write through an option (find -delete, sort -o,
// tree -o, git branch -D) aren't among them; a redirection to a file
// always asks.
var ReadOnlyCommands = []string{
	"ls", "pwd", "cat", "head", "tail", "wc", "stat", "du", "df", "which", "grep", "rg", "diff",
	"git status", "git log", "git show", "git diff", "git blame", "git rev-parse",
}

// ReadOnlyGuards are the options with which ReadOnlyCommands write a file
// or run another program: with read_only_defaults on, they ask even so.
var ReadOnlyGuards = []string{"re:git .*--(output|ext-diff).*", "re:rg .*--pre.*"}

// ReadOnlyGuardLabels say what ReadOnlyGuards cover, for people.
var ReadOnlyGuardLabels = []string{"git … --output", "git … --ext-diff", "rg … --pre"}

// DefaultsOn reports whether the built-in read-only rules apply.
func (p PermissionsConfig) DefaultsOn() bool { return p.ReadOnlyDefaults == nil || *p.ReadOnlyDefaults }

// Merge adds a workspace's rules to these, without repeats; the
// workspace's read_only_defaults, if set, wins.
func (p PermissionsConfig) Merge(ws PermissionsConfig) PermissionsConfig {
	union := func(a, b []string) []string {
		out := slices.Clone(a)
		for _, r := range b {
			if !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
		return out
	}
	out := PermissionsConfig{Allow: union(p.Allow, ws.Allow), Ask: union(p.Ask, ws.Ask), Deny: union(p.Deny, ws.Deny), ReadOnlyDefaults: p.ReadOnlyDefaults, Auto: p.Auto}
	if ws.ReadOnlyDefaults != nil {
		out.ReadOnlyDefaults = ws.ReadOnlyDefaults
	}
	if ws.Auto.Model != "" {
		out.Auto.Model = ws.Auto.Model
	}
	if ws.Auto.Environment != "" {
		out.Auto.Environment = ws.Auto.Environment
	}
	return out
}

// SandboxConfig bounds what tools may touch.
//
// File tools are confined to the workspace plus AllowedPaths (read-write) and
// ReadOnlyPaths, minus BlockedPaths. Shell commands are checked against the
// command policy and, when Shell is "auto" or "required", run under the OS
// sandbox (macOS Seatbelt): writes are limited to the writable roots,
// ShellWritablePaths and temp/cache dirs, blocked paths can't be read, and
// network access follows AllowNetwork.
type SandboxConfig struct {
	// AllowedPaths are directories outside the workspace the file tools may
	// read and write.
	AllowedPaths []string `toml:"allowed_paths"`
	// ReadOnlyPaths are directories outside the workspace the file tools may
	// read.
	ReadOnlyPaths []string `toml:"read_only_paths"`
	// BlockedPaths (globs) are never read or written, by file tools or, in the
	// OS sandbox, by commands: secrets by default.
	BlockedPaths []string `toml:"blocked_paths"`
	// ShellWritablePaths are directories sandboxed commands may also write
	// (caches).
	ShellWritablePaths []string `toml:"shell_writable_paths"`
	Shell              string   `toml:"shell"` // auto | required | off
	// AllowNetwork lets sandboxed commands use the network.
	AllowNetwork bool `toml:"allow_network"`
	// ScrubEnv are environment variables (globs) withheld from commands the
	// model runs, so they can't read credentials.
	ScrubEnv []string       `toml:"scrub_env"`
	Commands CommandsConfig `toml:"commands"`
}

// CommandsConfig holds shell command patterns. Patterns match a whole simple
// command ("git status"); "*" matches anything and a trailing " *" also
// matches the bare command. Deny wins over Allow; if Allow is non-empty only
// matching commands may run; AutoApprove skips the approval prompt.
type CommandsConfig struct {
	// Allow, when set, is the only commands that may run.
	Allow []string `toml:"allow"`
	// Deny are commands that never run, approved or not.
	Deny []string `toml:"deny"`
	// AutoApprove are commands that run without asking.
	AutoApprove []string `toml:"auto_approve"`
}

// DefaultBlockedPaths are secrets that tools never read or write.
var DefaultBlockedPaths = []string{
	".env", ".env.local", ".env.*.local", ".env.toml", ".env.*.toml",
	"*.pem", "*.key", "*.p12", "id_rsa*", "id_ecdsa*", "id_ed25519*",
	"~/.ssh", "~/.aws", "~/.gnupg", "~/.config/gcloud", "~/.azure", "~/.kube",
	"~/.docker/config.json", "~/.netrc",
}

// DefaultScrubEnv are environment variables withheld from commands the model
// runs, so they can't read Blitz's own credentials.
var DefaultScrubEnv = []string{
	"*_API_KEY", "*_API_TOKEN", "*_SECRET", "*_SECRET_KEY", "*_ACCESS_KEY",
	"AWS_SESSION_TOKEN", "GOOGLE_APPLICATION_CREDENTIALS", "MODENV_*",
}

// DefaultDeniedCommands are refused regardless of approval.
var DefaultDeniedCommands = []string{
	"sudo *", "su *", "doas *",
	"shutdown *", "reboot *", "halt *", "mkfs *", "mkfs.*", "diskutil erase*",
}

// SessionConfig configures persistence and session storage.
type SessionConfig struct {
	// StorageDir is where sessions are saved.
	StorageDir string `toml:"storage_dir"`
	// AutoSave saves the session after every turn.
	AutoSave bool `toml:"auto_save"`
}

// DefaultConfig returns a fully initialized Config with sensible production defaults.
func DefaultConfig() *Config {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}

	cfg := &Config{
		Blitz: BlitzConfig{
			DefaultAgent: "blitz",
			DefaultModel: "", // empty: use llm.<provider>.model
			AgencyLevel:  string(AgencyHigh),
			Temperature:  0.2,
			MaxTokens:    8192,
			AutoApprove:  false,
			PlanReview:   PlanReviewAgent,

			GoalMaxContinues: 20,
		},
		LLM: LLMConfig{
			Provider:            "gemini",
			MaxRetries:          3,
			StallTimeoutSeconds: 600,
			Gemini: GeminiConfig{
				APIKey: os.Getenv("GEMINI_API_KEY"),
				Model:  "gemini-3.8-flash",
			},
			OpenAI: OpenAIConfig{
				APIKey:  os.Getenv("OPENAI_API_KEY"),
				BaseURL: "https://api.openai.com/v1", // runtime.defaultOpenAIBaseURL
				Model:   "gpt-4o",
			},
			Anthropic: AnthropicConfig{
				APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
				Model:     "claude-opus-5",
				Fallbacks: "default",
			},
		},
		Workers: WorkersConfig{
			Enabled: true,
			Paths:   []string{".agents/workers", "workers"},
			Policy: WorkerPolicy{
				Allow:             []string{"shell", "write", "delete", "web", "mcp"},
				DefaultMaxTurns:   50,
				DefaultMaxCostUSD: 1,
				DefaultTimeout:    "30m",
				MaxTurns:          200,
				MaxCostUSD:        10,
				MaxTimeout:        "2h",
				MaxConcurrent:     2,
			},
		},
		Skills: SkillsConfig{
			Enabled: true,
			Paths: []string{
				filepath.Join(homeDir, ".blitz", "skills"),
				"./skills",
				".agents/skills",
			},
			Policy: SkillPolicy{
				MinHITLTier:       2,
				Languages:         []string{"python"},
				Sandbox:           "auto",
				Network:           "none",
				MaxTimeoutSeconds: 300,
				Packages:          PackagePolicy{Index: "https://pypi.org/simple", WheelsOnly: true},
			},
		},
		Tools: ToolsConfig{
			ShellTimeoutSeconds: 120,
			MaxFileSizeBytes:    10 * 1024 * 1024, // 10MB
			WorkspaceDir:        ".",
			AutoApproveCommands: false,
			MaxParallel:         8,

			MaxBackgroundAgents:     4,
			BackgroundAgentTimeout:  "30m",
			BackgroundAgentMaxTurns: 50,
		},
		Session: SessionConfig{
			StorageDir: filepath.Join(homeDir, ".blitz", "sessions"),
			AutoSave:   true,
		},
		Sandbox: SandboxConfig{
			BlockedPaths:       append([]string(nil), DefaultBlockedPaths...),
			ShellWritablePaths: []string{"~/.cache", "~/go/pkg/mod", "~/.npm"},
			Shell:              "auto",
			AllowNetwork:       true,
			ScrubEnv:           append([]string(nil), DefaultScrubEnv...),
			Commands: CommandsConfig{
				Deny: append([]string(nil), DefaultDeniedCommands...),
			},
		},
	}
	applyFeatureDefaults(cfg)
	return cfg
}

// Load loads configuration using modenv from a trusted directory, falling back
// to DefaultConfig with environment variable overrides.
//
// The config directory is, in order: prefixDir (the --config flag), the
// MODENV_PREFIX environment variable, then ~/.blitz. The current working
// directory is deliberately NOT consulted: a cloned repository could otherwise
// ship a .env.toml that redirects llm.openai.base_url to an attacker's server
// and receive the user's API key, or enable auto-approval. Pass --config .
// to opt in to a workspace config explicitly.
func Load(prefixDir string) (*Config, error) { return load(prefixDir, "") }

// load is Load with, for a workspace, its own settings over the global ones
// (LoadWorkspace), and keychain references resolved.
func load(prefixDir, workspace string) (*Config, error) {
	cfg := DefaultConfig()

	dir := ConfigDir(prefixDir)
	if dir == "" {
		applyEnvOverrides(cfg)
		return cfg, nil
	}
	if err := os.Setenv("MODENV_PREFIX", dir); err != nil {
		return nil, fmt.Errorf("failed to set MODENV_PREFIX: %w", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".env.toml")); err == nil {
		// Load hierarchical configuration and decrypt secrets via modenv
		_, loadErr := modenv.Load(cfg)
		if loadErr != nil {
			return nil, fmt.Errorf("failed to load configuration via modenv: %w", loadErr)
		}
	}
	if err := overlay(cfg, prefixDir, workspace); err != nil {
		return nil, err
	}
	resolveSecrets(cfg, secrets.Default(dir))

	// Apply direct environment variable fallbacks if not populated
	applyEnvOverrides(cfg)

	cfg.Dir = dir
	if workspace != "" {
		if real, err := CanonicalWorkspace(workspace); err == nil {
			cfg.Project = LoadProject(real)
			cfg.Project.ApplyTightening(cfg)
		}
	}
	return cfg, nil
}

// ConfigDir returns the trusted directory to load .env.toml from, or "" if none.
func ConfigDir(prefixDir string) string {
	if prefixDir != "" {
		return ExpandHome(prefixDir)
	}
	if pfx := os.Getenv("MODENV_PREFIX"); pfx != "" {
		return ExpandHome(pfx)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".blitz")
	}
	return ""
}

// ExpandHome expands a leading "~" or "~/" to the user's home directory.
// Other forms (e.g. "~user") are returned unchanged.
func ExpandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, "~"+string(filepath.Separator)) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}

// IsWorkspaceRelative reports whether a configured search path points into the
// current workspace (a relative path) rather than a user-level location.
func IsWorkspaceRelative(p string) bool {
	return !filepath.IsAbs(ExpandHome(p))
}

// SkillSearchPaths returns skill directories to scan, expanded, with
// workspace-relative entries resolved against workspace (the working
// directory if ""). A project's skills load as prompt text; their scripts
// run only once the project is trusted (spec_project_config_031).
func (c *Config) SkillSearchPaths(workspace string) []string {
	return resolvePaths(c.Skills.Paths, workspace)
}

// AgentSearchPaths returns directories to scan for user-defined agents,
// resolved like SkillSearchPaths.
func (c *Config) AgentSearchPaths(workspace string) []string {
	return resolvePaths([]string{"~/.blitz/agents", "./agents"}, workspace)
}

func resolvePaths(paths []string, workspace string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if IsWorkspaceRelative(p) && workspace != "" {
			out = append(out, filepath.Join(workspace, p))
		} else {
			out = append(out, ExpandHome(p))
		}
	}
	return out
}

func applyEnvOverrides(cfg *Config) {
	if p := os.Getenv("LLM_PROVIDER"); p != "" {
		cfg.LLM.Provider = strings.ToLower(p)
	}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" && cfg.LLM.Gemini.APIKey == "" {
		cfg.LLM.Gemini.APIKey = key
	}
	if key := os.Getenv("GOOGLE_API_KEY"); key != "" && cfg.LLM.Gemini.APIKey == "" {
		cfg.LLM.Gemini.APIKey = key
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" && cfg.LLM.OpenAI.APIKey == "" {
		cfg.LLM.OpenAI.APIKey = key
	}
	if b := os.Getenv("OPENAI_BASE_URL"); b != "" {
		cfg.LLM.OpenAI.BaseURL = b
	}
	if m := os.Getenv("OPENAI_MODEL"); m != "" {
		cfg.LLM.OpenAI.Model = m
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" && cfg.LLM.Anthropic.APIKey == "" {
		cfg.LLM.Anthropic.APIKey = key
	}
	if model := os.Getenv("BLITZ_MODEL"); model != "" {
		cfg.Blitz.DefaultModel = model
	}
	if agent := os.Getenv("BLITZ_AGENT"); agent != "" {
		cfg.Blitz.DefaultAgent = agent
	}
	if agency := os.Getenv("BLITZ_AGENCY"); agency != "" {
		cfg.Blitz.AgencyLevel = strings.ToLower(agency)
	}
	if level := os.Getenv("BLITZ_LOG_LEVEL"); level != "" {
		cfg.Log.Level = strings.ToLower(level)
	}
	switch strings.ToLower(os.Getenv("BLITZ_TELEMETRY")) {
	case "1", "true", "yes", "on":
		cfg.Telemetry.Enabled = true
	case "0", "false", "no", "off":
		cfg.Telemetry.Enabled = false
	}

}

// Plan review policies ([blitz] plan_review).
const (
	PlanReviewAlways = "always"
	PlanReviewAgent  = "agent-decides"
	PlanReviewNever  = "never"
)
