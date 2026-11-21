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
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/browser"
	"github.com/retail-cortex/blitz/pkg/engine/memory"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/retail-cortex/blitz/pkg/observability"
	"github.com/retail-cortex/blitz/pkg/plugins"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

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

// openRegistries loads the agent and skill registries, hides and locks
// per-project and organization entries, and reports configuration warnings.
func (w *Workspace) openRegistries(cfg *config.Config, dir string, warn func(string)) error {
	var err error
	if w.agents, err = agents.NewRegistry(); err != nil {
		return fmt.Errorf("failed to load agent registry: %w", err)
	}
	// The global agents switched off here aren't loaded (STU-15).
	userAgents := filepath.Clean(config.ExpandHome(config.UserAgentsDir))
	w.agents.Hide(func(from, name string) bool {
		return filepath.Clean(from) == userAgents && agentScope(cfg).Off(dir)[name]
	})
	// The organization's required agents and skills: nothing of their
	// names replaces them (spec_053 EN-81).
	if d := cfg.Org.ItemDir(config.OrgRequired, "agents"); d != "" {
		w.agents.Lock(d)
	}
	if err := w.agents.LoadExternalAgents(cfg.AgentSearchPaths(dir)...); err != nil {
		warn(err.Error())
	}
	if w.skills, err = skills.NewProvider(); err != nil {
		return fmt.Errorf("failed to load skills provider: %w", err)
	}
	// What the settings files hold that loading didn't use (CFG-48), and
	// an organization policy that can't be read (EN-33).
	for _, msg := range cfg.Warnings {
		warn(msg)
	}
	// The built-in agents and skills the organization switches off
	// (spec_053 EN-31), but the agent sessions start with.
	if off := cfg.Org.Policy.Builtins.Off; len(off) > 0 {
		if slices.Contains(off, cfg.Blitz.DefaultAgent) {
			warn(i18n.T("org.builtin_kept", "name", cfg.Blitz.DefaultAgent))
			off = slices.DeleteFunc(slices.Clone(off), func(n string) bool { return n == cfg.Blitz.DefaultAgent })
		}
		w.agents.SwitchOff(off)
		w.skills.SwitchOff(off)
	}
	w.skills.Lock(cfg.Org.ItemDir(config.OrgRequired, "skills"))
	w.skills.SetVersions(w.skillVersions)
	if cfg.Skills.Enabled {
		if err := w.skills.DiscoverExternal(cfg.SkillSearchPaths(dir)); err != nil {
			warn(err.Error())
		}
		for _, p := range cfg.Skills.Policy.Problems() {
			warn(p)
		}
	}
	for _, p := range cfg.Workers.Policy.Problems() {
		warn(p)
	}
	return nil
}

// setupSpeaker gives generate_audio the [audio] model's speaker (none
// without one), warning when it can't be built; its calls go in the
// session's /cost.
func (w *Workspace) setupSpeaker(ctx context.Context, warn func(string)) {
	cfg := w.cfg
	sp, err := runtime.NewSpeaker(ctx, cfg)
	if err != nil {
		warn(i18n.T("audio.model_failed", "model", cfg.Audio.Model, "error", ModelErrorSummary(err, cfg)))
	}
	w.tools.SetSpeaker(sp, err, cfg.Audio, func(ctx context.Context, session, model string, usage *genai.GenerateContentResponseUsageMetadata) {
		if session != "" && w.engine != nil {
			w.engine.RecordSpeech(ctx, session, model, usage)
		}
	})
}

// setupPainter gives generate_image the [images] model's painter (none
// without one), warning when it can't be built; its calls go in the
// session's /cost.
func (w *Workspace) setupPainter(ctx context.Context, warn func(string)) {
	cfg := w.cfg
	p, err := runtime.NewPainter(ctx, cfg)
	if err != nil {
		warn(i18n.T("images.model_failed", "model", cfg.Images.Model, "error", ModelErrorSummary(err, cfg)))
	}
	w.tools.SetPainter(p, err, func(ctx context.Context, session, model string, usage *genai.GenerateContentResponseUsageMetadata) {
		if session != "" && w.engine != nil {
			w.engine.RecordImage(ctx, session, model, usage)
		}
	})
	w.tools.SetPictureKeeper(w.keepToolPictures) // spec_058 PIC-81
}

// LoadAttachments loads image and PDF files (failures are errors naming
// the path) and @image or @paper.pdf mentions in prompt (failures are
// warnings: the prompt may be piped text that merely contains an @path)
// through the workspace sandbox. Identical files are attached once.
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

// loadPlugins adds the enabled plugins (after the project settings, which
// may enable or disable some): their agents and skills now, their hooks,
// MCP servers and commands through cfg.
func (w *Workspace) loadPlugins(cfg *config.Config, warn func(string)) {
	skillPaths := len(cfg.Skills.Paths)
	var problems []string
	w.plugins, problems = plugins.Apply(cfg, plugins.Default())
	for _, p := range problems {
		warn(p)
	}
	if err := w.agents.LoadExternalAgents(cfg.PluginAgentDirs...); err != nil {
		warn(err.Error())
	}
	if cfg.Skills.Enabled && len(cfg.Skills.Paths) > skillPaths {
		if err := w.skills.DiscoverExternal(cfg.Skills.Paths[skillPaths:]); err != nil {
			warn(err.Error())
		}
	}
}

// instructions are the extra system instructions: project memory plus, for
// non-English locales, which language to reply in.
func (w *Workspace) instructions() string {
	s := memory.Render(w.memory.Docs, len(w.memory.Rules) > 0)
	if w.cfg.Memory.AutoOn() {
		if notes, err := memory.Notes(memory.NotesDir(w.Dir())); err == nil {
			s += memory.RenderNotes(notes)
		}
	}
	s += w.styleText()
	s += harnessFolders(w.Dir())
	if p := strings.TrimSpace(w.appendPrompt); p != "" {
		s += "\n\n" + p
	}
	return s + i18n.ReplyInstruction(w.replyIn())
}

// FindBrowser is the system's Chrome, Chromium, Edge or Brave (path when
// set), for opening a browser profile to sign in by hand (BP-05).
func FindBrowser(path string) (string, error) { return browser.Find(path) }

// browserProfiles are the person's browser profiles as the workspace
// sees them (spec_049_browser_profiles BP-03, BP-03b): the one it browses
// with, as the person chose it (its name, its folder and the sites it
// browses without asking; none chosen, their only profile, BP-03e;
// Anonymous, none), and all of them, which the agent may ask to
// browse with. The workspace's own isn't used while the project's
// settings wait for trust or were declined (BP-03a: a project the person
// hasn't trusted could steer the agent into their sign-ins); it's named
// as withheld instead.
func (w *Workspace) browserProfiles() tools.BrowserProfiles {
	store := config.OpenBrowserProfiles(settingsDir(w.cfg))
	var out tools.BrowserProfiles
	for _, p := range store.List() {
		out.All = append(out.All, tools.BrowserProfile{Name: p.Name, Dir: store.Dir(p.Name), Allow: p.AllowDomains})
	}
	name := store.ForWorkspace(w.Dir())
	if name == "" && len(out.All) == 1 {
		// None chosen: the person's only profile is the default (BP-03e).
		name = out.All[0].Name
	}
	out.Anonymous = name == config.AnonymousBrowserProfile
	i := slices.IndexFunc(out.All, func(p tools.BrowserProfile) bool { return p.Name == name })
	switch {
	case name == "" || i < 0:
	case !w.projectTrusted():
		out.Withheld = name
	default:
		out.Current = out.All[i]
	}
	return out
}

// harnessFolders tells the agents about the harness's scratch folder and
// support scripts, when the workspace has them (spec_048_harness_learning
// HE-50): where working files go, and what's there to run.
func harnessFolders(dir string) string {
	var b strings.Builder
	if info, err := os.Stat(filepath.Join(dir, config.ProjectScratchDir)); err == nil && info.IsDir() {
		fmt.Fprintf(&b, "\n\nPut working files (downloads, drafts, test data, notes) in `%s/`, which git ignores, not in /tmp or the workspace root.", config.ProjectScratchDir)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, config.ProjectScriptsDir)); err == nil {
		var names []string
		for _, e := range entries {
			if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				names = append(names, e.Name())
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(&b, "\n\nThe project's support scripts are in `%s/` (%s): run them rather than repeating their steps; .agents/AGENTS.md says what each does.", config.ProjectScriptsDir, strings.Join(names, ", "))
		}
	}
	return b.String()
}

// loadMemory reads instruction files and rules; imports of blocked or
// hidden paths (.env, keys) are never read into the prompt.
func (w *Workspace) loadMemory() {
	ws := w.tools.Workspace()
	w.memory = memory.LoadAll(w.Dir(), w.cfg.Memory, memory.Options{Blocked: func(p string) bool {
		_, b := ws.KeptFromModel(p)
		return b
	}})
	w.memoryStamp = memory.Stamp(w.Dir(), w.cfg.Memory, w.memory)
	for _, p := range w.memory.Problems {
		w.warnOnce(p) // read again at each change: said once
	}
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
	return i18n.Setup(config.ExpandHome(cfg.UI.LocalesDir), cfg.UI.Locale, warn)
}

// SecretRedactor masks configured credentials, the values of scrubbed
// environment variables and MCP servers' values in audit entries, logs and
// telemetry (observability.SecretRedactor).
func SecretRedactor(cfg *config.Config) *redact.Redactor { return observability.SecretRedactor(cfg) }

// ModelErrorSummary makes a model error safe and readable: some SDK errors
// embed their whole client config, including the API key, and most need
// only their first line. A quota error is kept whole, on one line: Google's
// message names the quota to raise.
func ModelErrorSummary(err error, cfg *config.Config) string {
	msg := err.Error()
	if i := strings.Index(msg, "ClientConfig:"); i >= 0 {
		msg = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(msg[:i]), "."))
	}
	if q, ok := errors.AsType[*runtime.QuotaError](err); ok {
		msg = strings.Join(strings.Fields(q.Error()), " ")
	} else if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return SecretRedactor(cfg).String(msg)
}

// mcpWithSecrets is servers with {secrets:key} in their env and header
// values put in, for the workspace in dir (the values are masked in logs:
// SecretRedactor masks every MCP env and header value).
func mcpWithSecrets(servers []config.MCPServerConfig, dir string) []config.MCPServerConfig {
	lookup := config.Vault("").Resolve(config.SecretScope("", dir))
	expand := func(m map[string]string) map[string]string {
		var out map[string]string
		for k, v := range m {
			if !secrets.HasPlaceholders(v) {
				continue
			}
			if out == nil {
				out = maps.Clone(m)
			}
			x, used := secrets.Expand(v, lookup)
			redact.Remember(used...)
			out[k] = x
		}
		if out == nil {
			return m
		}
		return out
	}
	out := slices.Clone(servers)
	for i := range out {
		out[i].Env = expand(out[i].Env)
		out[i].Headers = expand(out[i].Headers)
	}
	return out
}

// expandSecrets puts secrets' values into text (spec_039_secrets):
// {secrets:key} the workspace's own, else the global one;
// {secrets:global:key} and {secrets:workspace:key} only that scope's.
func (w *Workspace) expandSecrets(text string) (string, []string) {
	return secrets.Expand(text, config.Vault("").Resolve(config.SecretScope("", w.Dir())))
}
