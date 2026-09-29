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

// Package engine is Blitz without a user interface: it opens a workspace
// (registries, tools, sessions, the model and the engine) and exposes what a
// front end needs as an api.Backend. The service and the CLI's --local mode
// run it; none of it prints (spec_workspace_018).
package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/retail-cortex/blitz/pkg/redact"
	"google.golang.org/adk/v2/model"
)

// Options configure Open.
type Options struct {
	// Streaming asks the model for partial responses as they are generated.
	Streaming bool
	// Warn receives problems that don't stop the workspace from opening.
	Warn func(string)
	// Model replaces the model built from the configuration (tests use a
	// mock). Agents with their own model still get theirs.
	Model model.LLM
	// Workers records which workers are enabled; one store serves every
	// workspace in a process (nil: ~/.blitz/workers.json).
	Workers *workers.Store
	// NewModel builds models by name for /model, pins and agents' own
	// models (nil: runtime.NewModel).
	NewModel func(ctx context.Context, cfg *config.Config, name string) (model.LLM, error)
}

// Workspace is one open project: everything a session needs.
type Workspace struct {
	cfg     *config.Config
	agents  *agents.Registry
	skills  *skills.Provider
	tools   *tools.Registry
	engine  *runtime.Engine
	storage *session.Storage
	audit   *audit.Logger
	memory  memory.Loaded
	locales *i18n.Bundle
	// modelErr is set when the configured model failed to initialise;
	// modelMu guards it (the service reloads and retries concurrently).
	modelErr error
	modelMu  sync.Mutex
	// rebuildMu serialises rebuilding the models.
	rebuildMu sync.Mutex
	warn      func(string)
	// reply is the language the model replies in, per workspace.
	reply *i18n.Localizer
	lock  *workspaceLock
	// workerStore records which workers are enabled.
	workerStore *workers.Store
	runLog      *workers.RunLog
	runsMu      sync.Mutex
	running     map[string]bool // workers running now, by name
	newModel    func(ctx context.Context, cfg *config.Config, name string) (model.LLM, error)
	warnedMu    sync.Mutex
	warned      map[string]bool // messages warnOnce has shown
	// sessionContext is session_start hook output waiting for each
	// session's next prompt.
	hookCtxMu      sync.Mutex
	sessionContext map[string]string
	// inTurn counts the turns running in each session, which a rewind
	// mustn't cut under.
	turnsMu sync.Mutex
	inTurn  map[string]int
	// userEdits are the user's changes to files since the agent's last
	// turn (files.go).
	editsMu   sync.Mutex
	userEdits []userEdit
	// index is the workspace's file list, for FindFiles.
	index fileIndex
}

// warnOnce reports a problem the first time it is seen: things re-read on
// every use (command files) would otherwise repeat it.
func (w *Workspace) warnOnce(msg string) {
	w.warnedMu.Lock()
	seen := w.warned[msg]
	if w.warned == nil {
		w.warned = map[string]bool{}
	}
	w.warned[msg] = true
	w.warnedMu.Unlock()
	if !seen {
		w.warn(msg)
	}
}

// Open wires registries, tools, the model and the engine for the workspace
// cfg.Tools.WorkspaceDir names. The workspace owns cfg from then on and
// changes it (the model, pins, settings), so each workspace needs its own
// copy. Open also activates cfg's interface language for the process.
func Open(ctx context.Context, cfg *config.Config, o Options) (*Workspace, error) {
	if o.Warn == nil {
		o.Warn = func(string) {}
	}
	w := &Workspace{cfg: cfg, locales: SetupLocale(cfg, o.Warn), warn: o.Warn}
	w.reply = i18n.Current()
	if w.newModel = o.NewModel; w.newModel == nil {
		w.newModel = runtime.NewModel
	}
	w.runLog = workers.OpenRunLog(config.ExpandHome("~/.blitz/worker-runs"))
	w.running = map[string]bool{}
	if w.workerStore = o.Workers; w.workerStore == nil {
		var err error
		if w.workerStore, err = defaultWorkerStore(); err != nil {
			return nil, err
		}
	}
	// Everything relative resolves against the workspace, never the
	// process's working directory: one process can hold several workspaces.
	dir, err := filepath.Abs(config.ExpandHome(cfg.Tools.WorkspaceDir))
	if err != nil {
		return nil, fmt.Errorf("invalid workspace directory: %w", err)
	}
	cfg.Tools.WorkspaceDir = dir
	// One owner per workspace, across processes.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace directory: %w", err)
	}
	if w.lock, err = lockWorkspace(real); err != nil {
		return nil, err
	}
	// Anything failing from here on must release the lock.
	opened := false
	defer func() {
		if !opened {
			w.lock.release()
		}
	}()

	if w.agents, err = agents.NewRegistry(); err != nil {
		return nil, fmt.Errorf("failed to load agent registry: %w", err)
	}
	if err := w.agents.LoadExternalAgents(cfg.AgentSearchPaths(dir)...); err != nil {
		o.Warn(err.Error())
	}
	if w.skills, err = skills.NewProvider(); err != nil {
		return nil, fmt.Errorf("failed to load skills provider: %w", err)
	}
	if cfg.Skills.Enabled {
		if err := w.skills.DiscoverExternal(cfg.SkillSearchPaths(dir)); err != nil {
			o.Warn(err.Error())
		}
		for _, p := range cfg.Skills.Policy.Problems() {
			o.Warn(p)
		}
	}

	if w.tools, err = tools.NewRegistry(cfg, w.agents, w.skills); err != nil {
		return nil, fmt.Errorf("failed to initialize tools: %w", err)
	}
	w.tools.SetWarn(o.Warn)
	if note := w.tools.ModeNote(); note != nil {
		o.Warn(i18n.T("mode.bypass_unavailable"))
	}
	if st := w.tools.Images(); st != nil && cfg.Images.RetainDays > 0 {
		if _, err := st.Prune(time.Duration(cfg.Images.RetainDays) * 24 * time.Hour); err != nil {
			o.Warn("image cleanup: " + err.Error())
		}
	}

	if cfg.Audit.Enabled {
		if w.audit, err = audit.Open(config.ExpandHome(cfg.Audit.Dir), SecretRedactor(cfg)); err != nil {
			o.Warn("audit log disabled: " + err.Error())
		}
		w.tools.SetAudit(w.audit)
	}

	if w.storage, err = session.NewStorage(cfg.Session.StorageDir); err != nil {
		w.Close()
		return nil, fmt.Errorf("failed to initialize session storage: %w", err)
	}
	w.storage.SetWorkspace(w.tools.Workspace().Dir())
	events, err := session.NewPersistentService(config.ExpandHome(cfg.Session.StorageDir))
	if err != nil {
		w.Close()
		return nil, err
	}

	llm := o.Model
	if llm == nil {
		if llm, err = w.newModel(ctx, cfg, ""); err != nil {
			w.modelErr = err
			llm = unavailable(cfg, err)
		}
	}

	w.loadMemory()
	for _, d := range w.memory.Docs {
		if d.Local && memory.TrackedByGit(d.Path) {
			o.Warn(i18n.T("memory.local_tracked", "path", d.Path))
		}
	}
	opts := []runtime.Option{runtime.WithSessionService(events), runtime.WithScopedRules(w.memory.Rules)}
	for agent, ref := range agentModelRefs(cfg, w.agents, o.Warn) {
		m, err := w.newModel(ctx, cfg, ref)
		if err != nil {
			o.Warn(i18n.T("pin.load_failed", "agent", agent, "model", ref, "error", ModelErrorSummary(err, cfg)))
			continue
		}
		opts = append(opts, runtime.WithAgentModel(agent, m))
	}
	w.engine, err = runtime.NewEngine(ctx, cfg, w.agents, w.skills, w.tools, llm, append(opts,
		runtime.WithInstructions(w.instructions()),
		runtime.WithStreaming(o.Streaming),
		runtime.WithTurnStore(w.storage),
		runtime.WithNotice(o.Warn),
	)...)
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("failed to initialize engine: %w", err)
	}
	w.tools.ScriptHooks().Info = func(_ context.Context, session string) tools.HookInfo {
		return tools.HookInfo{TranscriptPath: w.storage.TranscriptPath(session), PermissionMode: string(w.tools.Hooks().Mode()), Agent: w.engine.ActiveAgent()}
	}
	opened = true
	return w, nil
}

// Config returns the workspace's configuration.
func (w *Workspace) Config() *config.Config { return w.cfg }

// Agents returns the agent registry.
func (w *Workspace) Agents() *agents.Registry { return w.agents }

// Skills returns the skills provider.
func (w *Workspace) Skills() *skills.Provider { return w.skills }

// Tools returns the tool registry: workspace, approvals, processes, MCP, hooks.
func (w *Workspace) Tools() *tools.Registry { return w.tools }

// Engine returns the agent engine.
func (w *Workspace) Engine() *runtime.Engine { return w.engine }

// Storage returns the saved-session store, scoped to this workspace.
func (w *Workspace) Storage() *session.Storage { return w.storage }

// Audit returns the audit log (nil when disabled; its methods accept nil).
func (w *Workspace) Audit() *audit.Logger { return w.audit }

// Locales returns the loaded translation catalogs.
func (w *Workspace) Locales() *i18n.Bundle { return w.locales }

// ModelErr is why the configured model failed to initialise (nil if it
// didn't). The workspace then runs on a placeholder model.
func (w *Workspace) ModelErr() error {
	w.modelMu.Lock()
	defer w.modelMu.Unlock()
	return w.modelErr
}

// Close kills background processes and MCP servers, flushes the audit log
// and releases the workspace.
func (w *Workspace) Close() error {
	var err error
	if w.storage != nil && w.tools != nil {
		if a := w.storage.Active(); a != nil {
			w.sessionEnded(a.ID, "exit") // queued before the hooks drain
		}
	}
	if w.tools != nil {
		err = w.tools.Close()
	}
	w.audit.Close()
	w.lock.release()
	return err
}

// LoadAttachments loads image files (failures are errors naming the path)
// and @image mentions in prompt (failures are warnings: the prompt may be
// piped text that merely contains an @path) through the workspace sandbox.
// Identical images are attached once.
func (w *Workspace) LoadAttachments(paths []string, prompt string, warn func(string)) ([]*images.Image, error) {
	var out []*images.Image
	seen := map[string]bool{}
	add := func(img *images.Image) {
		if !seen[img.SHA256] {
			seen[img.SHA256] = true
			out = append(out, img)
		}
	}
	for _, p := range paths {
		img, err := w.tools.LoadImage(strings.TrimPrefix(p, "@"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		add(img)
	}
	for _, p := range images.Mentions(prompt) {
		img, err := w.tools.LoadImage(p)
		if err != nil {
			warn(i18n.T("attach.failed", "path", p, "error", err.Error()))
			continue
		}
		add(img)
	}
	return out, nil
}

// instructions are the extra system instructions: project memory plus, for
// non-English locales, which language to reply in.
func (w *Workspace) instructions() string {
	return memory.Render(w.memory.Docs, len(w.memory.Rules) > 0) + i18n.ReplyInstruction(w.reply)
}

// loadMemory reads instruction files and rules; imports of blocked paths
// (.env, keys) are never read into the prompt.
func (w *Workspace) loadMemory() {
	blocked := w.tools.Workspace().Blocked()
	w.memory = memory.LoadAll(w.Dir(), w.cfg.Memory, memory.Options{Blocked: func(p string) bool {
		_, b := blocked.Match(p)
		return b
	}})
}

// unavailable stands in for the configured model when it can't be built:
// turns fail with why (secrets removed) until it can.
func unavailable(cfg *config.Config, err error) model.LLM {
	return runtime.NewUnavailableModel(cfg.ModelName(), ModelErrorSummary(err, cfg))
}

// agentModelRefs returns the model each agent should run on when it isn't
// the configured one: a pin in [agent_models] wins over the agent's own
// default_model. Pins naming unknown agents are reported and skipped.
func agentModelRefs(cfg *config.Config, reg *agents.Registry, warn func(string)) map[string]string {
	refs := map[string]string{}
	for _, spec := range reg.List() {
		if spec.DefaultModel != "" {
			refs[spec.Name] = spec.DefaultModel
		}
	}
	for agent, ref := range cfg.AgentModels {
		if ref == "" {
			continue // unpinned in the workspace: its own default_model, or the configured model
		}
		if _, ok := reg.Get(agent); !ok {
			warn(i18n.T("pin.unknown_agent", "agent", agent))
			continue
		}
		refs[agent] = ref
	}
	return refs
}

// SetupLocale loads translation catalogs (built in, then ui.locales_dir) and
// activates ui.locale for the process.
func SetupLocale(cfg *config.Config, warn func(string)) *i18n.Bundle {
	b, errs := i18n.NewBundle(config.ExpandHome(cfg.UI.LocalesDir))
	for _, err := range errs {
		warn(err.Error())
	}
	locale := cfg.UI.Locale
	if locale == "" {
		locale = i18n.DefaultLocale
	}
	tag, err := b.Resolve(locale)
	if err != nil {
		warn(fmt.Sprintf("ui.locale %q is not a language code; using %s", cfg.UI.Locale, i18n.DefaultLocale))
		tag, _ = b.Resolve(i18n.DefaultLocale)
	}
	i18n.SetCurrent(b.Localizer(tag))
	return b
}

// SecretRedactor masks configured credentials and the values of scrubbed
// environment variables in audit entries, logs and telemetry.
func SecretRedactor(cfg *config.Config) *redact.Redactor {
	secrets := []string{cfg.LLM.Gemini.APIKey, cfg.LLM.OpenAI.APIKey, cfg.LLM.Anthropic.APIKey, cfg.Web.SearchAPIKey}
	for _, s := range cfg.MCP.Servers {
		for _, v := range s.Env {
			secrets = append(secrets, v)
		}
	}
	return redact.FromEnv(cfg.Sandbox.ScrubEnv, secrets...)
}

// ModelErrorSummary makes a model initialisation error safe and readable:
// some SDK errors embed their whole client config, including the API key.
func ModelErrorSummary(err error, cfg *config.Config) string {
	msg := err.Error()
	if i := strings.Index(msg, "ClientConfig:"); i >= 0 {
		msg = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(msg[:i]), "."))
	}
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	r := redact.FromEnv(cfg.Sandbox.ScrubEnv, cfg.LLM.Gemini.APIKey, cfg.LLM.OpenAI.APIKey, cfg.LLM.Anthropic.APIKey)
	return r.String(msg)
}
