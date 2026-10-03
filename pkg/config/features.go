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
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UIConfig controls terminal presentation.
type UIConfig struct {
	// Style is the output style sessions start with: default, concise,
	// explanatory, or one in ~/.blitz/styles.
	Style    string `toml:"style"`
	Markdown bool   `toml:"markdown"` // render model output as Markdown (TTY only)
	Spinner  bool   `toml:"spinner"`  // show progress while waiting (TTY only)
	// TerminalTitle shows the session's name in the terminal window title (TTY only).
	TerminalTitle bool   `toml:"terminal_title"`
	HistoryFile   string `toml:"history_file"` // REPL input history
	// HistorySize is how many REPL inputs the history keeps.
	HistorySize int    `toml:"history_size"`
	DiffLines   int    `toml:"diff_lines"`  // max diff lines shown in approval prompts
	Theme       string `toml:"theme"`       // glamour style: auto, dark, light, notty
	Locale      string `toml:"locale"`      // interface language, e.g. en-US, es, fr-CA
	LocalesDir  string `toml:"locales_dir"` // extra translation catalogs (*.json)
	// NotifyAfter makes a turn that runs longer than this many seconds tell
	// you when it ends, or when an approval or question waits (0: never).
	NotifyAfter int `toml:"notify_after"`
	// Notify is how the terminal tells you: both (bell and desktop
	// notification), bell, desktop or off.
	Notify string `toml:"notify"`
	// StatusLine is the line shown above each prompt: "default" (model,
	// mode, context, cost), a shell command that reads the session's state
	// as JSON and prints the line, or "" for none.
	StatusLine string `toml:"status_line"`
	// Editor is how the prompt edits: emacs (the default) or vim.
	Editor string `toml:"editor"`
	// Keybindings is the file that rebinds the REPL's keys.
	Keybindings string `toml:"keybindings"`
}

// ImagesConfig controls pictures sent to the model: @file.png mentions,
// /attach, /paste, --image and the view_image tool.
type ImagesConfig struct {
	// Enabled lets images be sent to the model.
	Enabled      bool   `toml:"enabled"`
	Dir          string `toml:"dir"`           // where prepared images are kept (owner-only)
	MaxDimension int    `toml:"max_dimension"` // longest edge sent to the model, in pixels
	MaxInputMB   int    `toml:"max_input_mb"`  // largest file accepted
	RetainDays   int    `toml:"retain_days"`   // unused images are deleted after this (0 = keep)
}

// PDFConfig shapes the PDFs the export_pdf tool writes.
type PDFConfig struct {
	// PageSize is a4, letter, or auto (the default): Letter where the
	// locale's country uses it (LC_PAPER, LANG), A4 elsewhere.
	PageSize string `toml:"page_size"`
}

// SearchConfig is workspace search: an index of the workspace's files,
// the text of its PDFs and notebooks, its chats and its notes, kept in
// ~/.blitz/workspaces and brought up to date as they change.
type SearchConfig struct {
	// Enabled keeps the index (on by default). Off, search finds nothing.
	Enabled bool `toml:"enabled"`
	// Sources are what a search looks in when it names none: files,
	// documents (PDFs and notebooks), chats and notes. The default is
	// files and documents.
	Sources []string `toml:"sources"`
	// EmbeddingModel turns on semantic search: "provider/model", a Gemini
	// ("gemini/text-embedding-004"), OpenAI ("openai/text-embedding-3-small")
	// or Ollama ("ollama/nomic-embed-text") embedding model. Every indexed
	// chunk is sent to that provider, once, and each query; empty (the
	// default) keeps search local. A project's settings can't set it.
	EmbeddingModel string `toml:"embedding_model"`
	// Enrich has a model write a summary and tags for each file and
	// document, in the background after indexing, so search finds files by
	// what they're about. Off by default: each file it describes is sent
	// to that model, once per change. A project's settings can't set it.
	Enrich bool `toml:"enrich"`
	// EnrichModel is the model that describes files ("provider/model");
	// empty uses suggestions.model, else the auto mode's model, else the
	// default model.
	EnrichModel string `toml:"enrich_model"`
	// EnrichDailyLimit is how many files may be described a day, per
	// workspace (default 200).
	EnrichDailyLimit int `toml:"enrich_daily_limit"`
	// IncludeIgnored indexes the text files git ignores too (data files,
	// local notes), still skipping dependency, build and cache folders
	// (node_modules, vendor, build, dist, target, virtual environments).
	// They're hidden: shown only when hidden files are.
	IncludeIgnored bool `toml:"include_ignored"`
}

// AudioConfig is spoken audio: the generate_audio tool reads text aloud
// with a speech model and writes a sound file.
type AudioConfig struct {
	// Model is the speech model as "provider/model": a Gemini text-to-speech
	// model ("gemini/gemini-2.5-flash-preview-tts") or an OpenAI one
	// ("openai/gpt-4o-mini-tts"). Empty turns generate_audio off.
	Model string `toml:"model"`
	// Voice is the voice for a single speaker, by the provider's name for
	// it ("Kore" for Gemini, "coral" for OpenAI); empty is the provider's
	// default.
	Voice string `toml:"voice"`
	// Speakers are two named voices for a conversation, such as a
	// two-host overview, whose lines start "Name: " (Gemini only).
	Speakers []SpeakerConfig `toml:"speakers"`
	// MaxChars caps the text spoken in one call (default 9000, about ten
	// minutes; Gemini's audio is uncompressed, about 3 MB a minute).
	MaxChars int `toml:"max_chars"`
}

// SpeakerConfig is one voice in a conversation.
type SpeakerConfig struct {
	Name  string `toml:"name" json:"name"`   // as the text's lines name them
	Voice string `toml:"voice" json:"voice"` // the provider's voice name
}

// MemoryConfig controls project instruction files loaded into the prompt.
type MemoryConfig struct {
	// Enabled loads project instruction files (BLITZ.md, AGENTS.md) into the
	// prompt.
	Enabled  bool     `toml:"enabled"`
	Files    []string `toml:"files"`     // names searched from the workspace up to the repo root
	Global   string   `toml:"global"`    // user-wide instructions file
	MaxBytes int      `toml:"max_bytes"` // per file
	// LocalFiles are personal instruction files (not to be committed),
	// loaded after Files in each directory.
	LocalFiles []string `toml:"local_files"`
	// RuleDirs hold rule files (*.md), relative to each directory from the
	// repo root to the workspace; a rule with frontmatter "paths" applies
	// only to matching files. GlobalRules is the user's own rule directory.
	RuleDirs    []string `toml:"rule_dirs"`
	GlobalRules string   `toml:"global_rules"`
	// Auto lets the agent save notes across sessions with its remember
	// tool, loaded into its instructions (nil: on).
	Auto *bool `toml:"auto"`
}

// AutoOn reports whether the agent keeps notes (memory.auto).
func (m MemoryConfig) AutoOn() bool { return m.Auto == nil || *m.Auto }

// ContextConfig controls conversation compaction.
type ContextConfig struct {
	// Compaction summarises older history once a prompt reaches
	// TokenThreshold tokens, keeping the RetainEvents most recent events raw.
	Compaction     bool `toml:"compaction"`
	TokenThreshold int  `toml:"token_threshold"`
	RetainEvents   int  `toml:"retain_events"`
}

// ModelPrice is the cost per million tokens, in USD.
type ModelPrice struct {
	// InputPerMTok is the price of a million input tokens.
	InputPerMTok float64 `toml:"input_per_mtok"`
	// OutputPerMTok is the price of a million output tokens.
	OutputPerMTok float64 `toml:"output_per_mtok"`
	// CachedInputPerMTok is the price of a million input tokens read from the
	// prompt cache.
	CachedInputPerMTok float64 `toml:"cached_input_per_mtok"`
	// CacheWritePerMTok prices tokens written to the prompt cache (Anthropic
	// bills these above the input rate); 0 means the input rate.
	CacheWritePerMTok float64 `toml:"cache_write_per_mtok"`
}

// SearchPrice is what a search provider bills for its queries.
type SearchPrice struct {
	// Per1KQueries is the price of a thousand queries.
	Per1KQueries float64 `toml:"per_1k_queries"`
}

// DefaultSearchPricing are the providers' published prices past their
// free tiers: Google's grounding bills each search query Gemini runs.
var DefaultSearchPricing = map[string]SearchPrice{
	"google": {Per1KQueries: 14},
}

// SearchPriceFor is provider's price per query: the settings', else the
// default, else 0 (not billed per query, or unknown).
func (c *Config) SearchPriceFor(provider string) float64 {
	if p, ok := c.SearchPricing[provider]; ok {
		return p.Per1KQueries / 1000
	}
	return DefaultSearchPricing[provider].Per1KQueries / 1000
}

// AuditConfig controls the append-only audit log.
type AuditConfig struct {
	// Enabled records every approval and tool action in the audit log.
	Enabled bool `toml:"enabled"`
	// Dir is where the audit log is kept (owner-only).
	Dir string `toml:"dir"`
}

// LogConfig controls the diagnostic log (<dir>/blitz-YYYY-MM-DD.jsonl).
// Secrets are masked before writing, as in the audit log.
type LogConfig struct {
	Level      string `toml:"level"`       // debug, info, warn, error, or off
	Dir        string `toml:"dir"`         // owner-only
	RetainDays int    `toml:"retain_days"` // older log files are deleted (0 = keep)
}

// TelemetryConfig controls OpenTelemetry trace and log export over OTLP/HTTP.
// It is off by default; BLITZ_TELEMETRY=1 also turns it on. Standard
// OTEL_EXPORTER_OTLP_* variables (headers, timeouts) apply.
type TelemetryConfig struct {
	// Enabled exports traces and logs over OTLP/HTTP.
	Enabled bool `toml:"enabled"`
	// Endpoint is the collector's base URL, e.g. http://localhost:4318.
	// Empty uses OTEL_EXPORTER_OTLP_ENDPOINT, then the OTLP default.
	Endpoint string `toml:"endpoint"`
	// CaptureContent exports prompts, model replies, tool arguments and tool
	// results (secrets masked). Off, spans carry only names, timings, token
	// counts and outcomes.
	CaptureContent bool `toml:"capture_content"`
}

// CheckpointConfig controls file snapshots used by /undo and /rewind.
type CheckpointConfig struct {
	// Enabled keeps file snapshots for /undo and /rewind.
	Enabled  bool  `toml:"enabled"`
	MaxBytes int64 `toml:"max_bytes"` // total snapshot size before the oldest turns are dropped
	// Dir keeps checkpoints between runs, one directory per workspace ("":
	// in memory only).
	Dir string `toml:"dir"`
	// MaxAgeDays drops kept checkpoints older than this.
	MaxAgeDays int `toml:"max_age_days"`
}

// HookConfig runs a command at a lifecycle point. The command receives a JSON
// event on stdin. Exit code 2 blocks the action (stderr is the reason); any
// other non-zero exit is reported and ignored unless FailClosed is set.
type HookConfig struct {
	Match string `toml:"match"` // tool-name glob for tool hooks; empty matches all
	// If is a permission rule, "shell(git push *)" say: a tool hook runs
	// only for calls it matches.
	If string `toml:"if"`
	// Type is how the hook runs: "command" (the default), "http" (the event
	// is POSTed to URL) or "prompt" (a model judges it against Prompt).
	Type string `toml:"type"`
	// Command is run with bash; it gets the event as JSON on stdin.
	Command string `toml:"command"`
	// Args, instead of Command, run a program directly, without a shell.
	Args []string `toml:"args"`
	// URL is where an http hook POSTs the event.
	URL string `toml:"url"`
	// Headers go with an http hook's request; $VAR and ${VAR} are replaced
	// by the variables AllowedEnvVars names (others stay as written).
	Headers        map[string]string `toml:"headers"`
	AllowedEnvVars []string          `toml:"allowed_env_vars"`
	// Prompt is what a prompt hook asks the model to judge the event by.
	Prompt string `toml:"prompt"`
	// Model is the prompt hook's model ("provider/model"; empty: the
	// auto mode's reviewer, else the main model).
	Model string `toml:"model"`
	// TimeoutSeconds stops the hook after this long (0: 30).
	TimeoutSeconds int `toml:"timeout_seconds"`
	// FailClosed blocks the action when the hook fails, not only on exit code
	// 2.
	FailClosed bool `toml:"fail_closed"`

	// Source is the project file a hook came from ("": the user's own
	// settings).
	Source string `toml:"-" json:"-"`
}

// Hook types.
const (
	HookCommand = "command"
	HookHTTP    = "http"
	HookPrompt  = "prompt"
)

// Kind is the hook's type, "command" when unset.
func (h HookConfig) Kind() string {
	if h.Type == "" {
		return HookCommand
	}
	return h.Type
}

// Describe is what the hook runs, for people.
func (h HookConfig) Describe() string {
	switch h.Kind() {
	case HookHTTP:
		return "POST " + h.URL
	case HookPrompt:
		return "prompt: " + h.Prompt
	}
	if len(h.Args) > 0 {
		return strings.Join(h.Args, " ")
	}
	return h.Command
}

// HooksConfig lists hooks per event.
type HooksConfig struct {
	PreTool      []HookConfig `toml:"pre_tool"`
	PostTool     []HookConfig `toml:"post_tool"`
	PromptSubmit []HookConfig `toml:"prompt_submit"`
	// Lifecycle events. session_start and prompt_submit output (plain
	// text, or additional_context) is given to the agent; stop may ask the
	// agent to continue; permission_request may answer an approval. The
	// rest only observe and run in the background.
	SessionStart      []HookConfig `toml:"session_start"`
	SessionEnd        []HookConfig `toml:"session_end"`
	Stop              []HookConfig `toml:"stop"`
	PostToolFailure   []HookConfig `toml:"post_tool_failure"`
	SubagentStart     []HookConfig `toml:"subagent_start"`
	SubagentStop      []HookConfig `toml:"subagent_stop"`
	PreCompact        []HookConfig `toml:"pre_compact"`
	PostCompact       []HookConfig `toml:"post_compact"`
	Notification      []HookConfig `toml:"notification"`
	PermissionRequest []HookConfig `toml:"permission_request"`
}

// ByEvent returns the hooks for each event name.
func (h HooksConfig) ByEvent() map[string][]HookConfig {
	return map[string][]HookConfig{
		"pre_tool": h.PreTool, "post_tool": h.PostTool, "prompt_submit": h.PromptSubmit,
		"session_start": h.SessionStart, "session_end": h.SessionEnd, "stop": h.Stop,
		"post_tool_failure": h.PostToolFailure, "subagent_start": h.SubagentStart, "subagent_stop": h.SubagentStop,
		"pre_compact": h.PreCompact, "post_compact": h.PostCompact, "notification": h.Notification,
		"permission_request": h.PermissionRequest,
	}
}

// All returns every configured hook.
func (h HooksConfig) All() []HookConfig {
	var out []HookConfig
	for _, list := range h.ByEvent() {
		out = append(out, list...)
	}
	return out
}

// MCPServerConfig describes a Model Context Protocol server. Exactly one of
// Command (stdio) or URL (streamable HTTP) is set.
type MCPServerConfig struct {
	// Name identifies the server, in mcp(server:tool) rules and /mcp.
	Name string `toml:"name"`
	// Command starts a stdio server.
	Command string `toml:"command"`
	// Args are the command's arguments.
	Args []string `toml:"args"`
	// Env are environment variables for the command.
	Env map[string]string `toml:"env"`
	// URL is a streamable HTTP server's address.
	URL string `toml:"url"`
	// Headers are sent with every request to an HTTP server.
	Headers map[string]string `toml:"headers"`
	// Disabled keeps the server configured without starting it
	// (blitz mcp disable).
	Disabled    bool     `toml:"disabled"`
	Tools       []string `toml:"tools"`        // optional allow-list of tool names
	AutoApprove bool     `toml:"auto_approve"` // skip approval for this server's tools
	Sandbox     *bool    `toml:"sandbox"`      // run stdio servers in the OS sandbox (default true)
	// Prefix namespaces the server's tools: prefix "gh" exposes create_issue
	// as gh__create_issue, avoiding collisions with other servers and built-ins.
	Prefix string `toml:"prefix"`
	// Agents lists the agents offered this server's tools: empty means the
	// active primary agent only, "*" means every agent (including ones run
	// through invoke_agent).
	Agents []string `toml:"agents"`
	// TimeoutSeconds bounds one tool call (default 300). A server that fails
	// or times out twice in a row is paused, with growing back-off.
	TimeoutSeconds int `toml:"timeout_seconds"`
}

// BrowserConfig controls the browser tool (spec_parity_027 §5.2): a
// Chromium-family browser the agent drives, in its own profile, reaching
// what the web rules allow.
type BrowserConfig struct {
	// Enabled offers the browser tool (with web.enabled and network
	// access).
	Enabled bool `toml:"enabled"`
	// Path is the browser to run; empty finds Chrome, Chromium, Edge or
	// Brave.
	Path string `toml:"path"`
	// Visible shows the browser's window instead of running headless.
	Visible bool `toml:"visible"`
	// AllowLocal lets it reach localhost and private addresses, to test
	// the user's own app.
	AllowLocal bool `toml:"allow_local"`
	// AllowScripts runs the agent's page scripts without asking.
	AllowScripts bool `toml:"allow_scripts"`
	// Width and Height are the viewport (1280×800).
	Width  int `toml:"width"`
	Height int `toml:"height"`
}

// LSPServerConfig is a language server: the command that runs it (over
// stdio), and the file extensions it's for.
type LSPServerConfig struct {
	// Command and its arguments, e.g. ["gopls"] or
	// ["typescript-language-server", "--stdio"].
	Command []string `toml:"command"`
	// Extensions are the files it serves, with the dot: [".go"].
	Extensions []string `toml:"extensions"`
	// Disabled turns a built-in server off.
	Disabled bool `toml:"disabled"`
}

// DefaultLSPServers are the language servers used when installed.
var DefaultLSPServers = map[string]LSPServerConfig{
	"go":         {Command: []string{"gopls"}, Extensions: []string{".go"}},
	"typescript": {Command: []string{"typescript-language-server", "--stdio"}, Extensions: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}},
	"python":     {Command: []string{"pyright-langserver", "--stdio"}, Extensions: []string{".py"}},
	"rust":       {Command: []string{"rust-analyzer"}, Extensions: []string{".rs"}},
}

// LSPServers are the language servers in force: the built-in ones with
// the settings' changes, then those the settings add (by language name).
func (c *Config) LSPServers() map[string]LSPServerConfig {
	out := map[string]LSPServerConfig{}
	for lang, d := range DefaultLSPServers {
		out[lang] = d
	}
	for lang, s := range c.LSP {
		d := out[lang]
		if len(s.Command) > 0 {
			d.Command = s.Command
		}
		if len(s.Extensions) > 0 {
			d.Extensions = s.Extensions
		}
		d.Disabled = s.Disabled
		if d.Disabled || len(d.Command) == 0 || len(d.Extensions) == 0 {
			delete(out, lang)
			continue
		}
		out[lang] = d
	}
	return out
}

// NetworkConfig is how Blitz reaches the network. Proxies come from the
// environment (HTTPS_PROXY, HTTP_PROXY, NO_PROXY).
type NetworkConfig struct {
	// CAFile is a PEM file of certificate authorities to trust besides the
	// system's (a company's TLS-inspecting proxy).
	CAFile string `toml:"ca_file"`
}

// PluginsConfig chooses plugins beyond those enabled in the store
// (blitz plugin enable): Enable turns installed ones on (from the user, or
// a trusted project), Disable off (the user, or any project).
type PluginsConfig struct {
	// Enable turns installed plugins on, by name.
	Enable []string `toml:"enable"`
	// Disable turns installed plugins off, by name; it wins over Enable.
	Disable []string `toml:"disable"`
	// Dirs are plugins loaded from directories for a run (--plugin-dir).
	Dirs []string `toml:"-"`
}

// PluginDir is a plugin's command directory.
type PluginDir struct {
	Dir, Plugin string
}

// MCPConfig lists MCP servers.
type MCPConfig struct {
	Servers []MCPServerConfig `toml:"servers"`
}

// WebConfig controls the web_fetch tool.
type WebConfig struct {
	// Enabled offers the web_fetch tool.
	Enabled      bool     `toml:"enabled"`
	AllowDomains []string `toml:"allow_domains"` // fetched without approval (globs like *.go.dev)
	// DenyDomains are never fetched (globs).
	DenyDomains  []string `toml:"deny_domains"`
	AllowPrivate bool     `toml:"allow_private"` // permit localhost/private network targets
	// MaxBytes is the most of a page web_fetch reads.
	MaxBytes int64 `toml:"max_bytes"`
	// TimeoutSeconds stops a fetch or search that takes longer.
	TimeoutSeconds int `toml:"timeout_seconds"`
	// SearchProvider enables web_search and /search web: "brave",
	// "tavily", "searxng" (self-hosted, needs SearchURL), or "google"
	// (Gemini's grounding with Google Search; uses the Gemini API key).
	// Empty disables it.
	SearchProvider string `toml:"search_provider"`
	SearchAPIKey   string `toml:"search_api_key"` // or BRAVE_API_KEY / TAVILY_API_KEY / GEMINI_API_KEY
	// SearchURL is a SearXNG instance's address, for search_provider
	// "searxng".
	SearchURL string `toml:"search_url"`
	// SearchModel is the Gemini model that runs "google" searches
	// (default: llm.gemini.model, else gemini-3.8-flash).
	SearchModel string `toml:"search_model"`
	// SearchMaxResults is how many results a web search returns (0: 5).
	SearchMaxResults int `toml:"search_max_results"`
}

// DefaultPricing holds estimated list prices for the default models, as of
// the start of PriceChanges. They change over time; override them under
// [pricing."model-name"]. Use DefaultPricingAt for the prices in effect.
var DefaultPricing = map[string]ModelPrice{
	// Gemini 3.8 Flash at its introductory price, valid through 2026-12-31
	// (PriceChanges has the price from 2027-01-01).
	"gemini-3.8-flash": {InputPerMTok: 0.75, OutputPerMTok: 3.75, CachedInputPerMTok: 0.075},
	"gemini-2.5-pro":   {InputPerMTok: 1.25, OutputPerMTok: 10.00, CachedInputPerMTok: 0.31},
	"gpt-4o":           {InputPerMTok: 2.50, OutputPerMTok: 10.00, CachedInputPerMTok: 1.25},
	// Claude list prices; cache reads bill at 10% of input, 5-minute cache
	// writes at 125%.
	"claude-opus-5":    {InputPerMTok: 5.00, OutputPerMTok: 25.00, CachedInputPerMTok: 0.50, CacheWritePerMTok: 6.25},
	"claude-sonnet-5":  {InputPerMTok: 2.00, OutputPerMTok: 10.00, CachedInputPerMTok: 0.20, CacheWritePerMTok: 2.50},
	"claude-haiku-4-5": {InputPerMTok: 1.00, OutputPerMTok: 5.00, CachedInputPerMTok: 0.10, CacheWritePerMTok: 1.25},
	// Gemini's speech models ([audio] model): text in, audio tokens out.
	"gemini-2.5-flash-preview-tts": {InputPerMTok: 0.50, OutputPerMTok: 10.00},
	"gemini-2.5-pro-preview-tts":   {InputPerMTok: 1.00, OutputPerMTok: 20.00},
}

// PriceChange is a published list price that takes effect on a date.
type PriceChange struct {
	Model string
	From  time.Time // UTC
	Price ModelPrice
}

// PriceChanges are announced changes to DefaultPricing, so a build made
// before a change still prices correctly after it.
var PriceChanges = []PriceChange{
	// The introductory Gemini 3.8 Flash price ends on 2026-12-31.
	{Model: "gemini-3.8-flash", From: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Price: ModelPrice{InputPerMTok: 1.50, OutputPerMTok: 7.50, CachedInputPerMTok: 0.15}},
}

// DefaultPricingAt returns DefaultPricing with every price change in
// effect at t applied (a fresh map the caller may change).
func DefaultPricingAt(t time.Time) map[string]ModelPrice {
	out := make(map[string]ModelPrice, len(DefaultPricing))
	for k, v := range DefaultPricing {
		out[k] = v
	}
	var applied = map[string]time.Time{}
	for _, c := range PriceChanges {
		if !t.Before(c.From) && !c.From.Before(applied[c.Model]) {
			out[c.Model], applied[c.Model] = c.Price, c.From
		}
	}
	return out
}

// Dir returns the Blitz home directory (~/.blitz).
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".blitz"
	}
	return filepath.Join(home, ".blitz")
}

func applyFeatureDefaults(c *Config) {
	dir := Dir()
	c.UI = UIConfig{
		Markdown:      true,
		Spinner:       true,
		TerminalTitle: true,
		HistoryFile:   filepath.Join(dir, "history"),
		HistorySize:   1000,
		DiffLines:     120,
		Theme:         "auto",
		Locale:        "en-US",
		LocalesDir:    filepath.Join(dir, "locales"),
		NotifyAfter:   30,
		Notify:        "both",
		Editor:        "emacs",
		Keybindings:   filepath.Join(dir, "keybindings.toml"),
	}
	c.Memory = MemoryConfig{
		Enabled: true,
		// CLAUDE.md and GEMINI.md too, so a repository set up for another
		// agent works unchanged. BLITZ.md stays last: /memory add appends to
		// the last file.
		Files:       []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md", "BLITZ.md"},
		LocalFiles:  []string{"CLAUDE.local.md", "BLITZ.local.md"},
		RuleDirs:    []string{".blitz/rules", ".agents/rules", ".claude/rules"},
		Global:      filepath.Join(dir, "BLITZ.md"),
		GlobalRules: filepath.Join(dir, "rules"),
		MaxBytes:    32 * 1024,
	}
	c.Context = ContextConfig{Compaction: true, TokenThreshold: 120_000, RetainEvents: 20}
	c.Tools.ApprovalsFile = filepath.Join(dir, "approvals.json")
	c.Audit = AuditConfig{Enabled: true, Dir: filepath.Join(dir, "audit")}
	c.Log = LogConfig{Level: "info", Dir: filepath.Join(dir, "logs"), RetainDays: 14}
	c.Checkpoints = CheckpointConfig{Enabled: true, MaxBytes: 64 * 1024 * 1024, Dir: "~/.blitz/checkpoints", MaxAgeDays: 30}
	c.Images = ImagesConfig{Enabled: true, Dir: filepath.Join(dir, "images"), MaxDimension: 1568, MaxInputMB: 20, RetainDays: 30}
	c.PDF = PDFConfig{PageSize: "auto"}
	c.Search = SearchConfig{Enabled: true, Sources: []string{"files", "documents"}, EnrichDailyLimit: 200}
	c.Audio = AudioConfig{MaxChars: 9_000}
	c.Web = WebConfig{Enabled: true, MaxBytes: 2 * 1024 * 1024, TimeoutSeconds: 20}
	c.Browser = BrowserConfig{Enabled: true, Width: 1280, Height: 800}
	// Prices in effect when the configuration loads: a service running
	// across a price change picks the new price up at its next restart.
	c.Pricing = DefaultPricingAt(time.Now())
}
