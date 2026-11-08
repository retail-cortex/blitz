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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/evals"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

// Blitz's own commands, as the blitz tool runs them (spec_060_blitz_tools):
// each on the functions Studio, Settings and the CLI use, a change asked
// of the person first with what it will do.

// runBlitzCommand is the workspace's tools.BlitzRunner.
func (w *Workspace) runBlitzCommand(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	h, ok := blitzHandlers[c.Name]
	if !ok {
		return nil, fmt.Errorf("%s isn't available yet", c.Name)
	}
	return h(w, ctx, c, args)
}

// blitzHandler runs one command.
type blitzHandler func(w *Workspace, ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error)

// blitzHandlers are the commands the workspace runs; help, job create and
// job list are the tool's own.
var blitzHandlers map[string]blitzHandler

func init() {
	blitzHandlers = map[string]blitzHandler{
		"connector list":    (*Workspace).cmdConnectorList,
		"connector save":    (*Workspace).cmdConnectorSave,
		"connector enable":  (*Workspace).cmdConnectorOn,
		"connector disable": (*Workspace).cmdConnectorOn,
		"settings show":     (*Workspace).cmdSettingsShow,
		"settings set":      (*Workspace).cmdSettingsSet,
		"doctor":            (*Workspace).cmdDoctor,
		"org policy":        (*Workspace).cmdOrgPolicy,
		"org status":        (*Workspace).cmdOrgStatus,
		"open url":          (*Workspace).cmdOpen,
		"open file":         (*Workspace).cmdOpen,
		"open studio":       (*Workspace).cmdOpen,
		"open settings":     (*Workspace).cmdOpen,
		"open view":         (*Workspace).cmdOpen,
		"picture":           (*Workspace).cmdPicture,
		"eval list":         (*Workspace).cmdEvalList,
		"eval run":          (*Workspace).cmdEvalRun,
		"eval results":      (*Workspace).cmdEvalResults,
		"spec list":         (*Workspace).cmdSpecList,
		"spec review":       (*Workspace).cmdSpecReview,
		"spec verify":       (*Workspace).cmdSpecVerify,
		"spec implement":    (*Workspace).cmdSpecImplement,
		"memory list":       (*Workspace).cmdMemoryList,
		"memory forget":     (*Workspace).cmdMemoryForget,
		"chat new":          (*Workspace).cmdChatNew,
		"chat export":       (*Workspace).cmdChatExport,
		"chat search":       (*Workspace).cmdChatSearch,
		"process list":      (*Workspace).cmdProcessList,
		"process stop":      (*Workspace).cmdProcessStop,
		"approval list":     (*Workspace).cmdApprovalList,
		"approval forget":   (*Workspace).cmdApprovalForget,
		"secret list":       (*Workspace).cmdSecretList,
		"secret set":        (*Workspace).cmdSecretSet,
		"workflow list":     (*Workspace).cmdWorkflowList,
		"workflow save":     (*Workspace).cmdWorkflowSave,
		"agent list":        (*Workspace).cmdAgentList,
		"agent save":        (*Workspace).cmdAgentSave,
		"skill list":        (*Workspace).cmdSkillList,
		"skill save":        (*Workspace).cmdSkillSave,
		"mcp list":          (*Workspace).cmdMCPList,
		"mcp add":           (*Workspace).cmdMCPAdd,
		"mcp enable":        (*Workspace).cmdMCPOn,
		"mcp disable":       (*Workspace).cmdMCPOn,
		"rule list":         (*Workspace).cmdRuleList,
		"rule save":         (*Workspace).cmdRuleSave,
		"guardrail list":    (*Workspace).cmdGuardrailList,
		"guardrail save":    (*Workspace).cmdGuardrailSave,
		"command list":      (*Workspace).cmdCommandList,
		"command save":      (*Workspace).cmdCommandSave,
		"hook list":         (*Workspace).cmdHookList,
		"plugin list":       (*Workspace).cmdPluginList,
	}
}

// str, flag, num and list are an argument as its type ("" or zero when
// it wasn't given; the tool checked the types).
func str(args map[string]any, k string) string { s, _ := args[k].(string); return strings.TrimSpace(s) }
func flag(args map[string]any, k string) bool  { b, _ := args[k].(bool); return b }
func num(args map[string]any, k string) int    { n, _ := args[k].(int); return n }
func list(args map[string]any, k string) []string {
	var out []string
	for _, v := range asSlice(args[k]) {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func asSlice(v any) []any { l, _ := v.([]any); return l }

// askChange asks the person about what c will change (DRV-04): what, and
// the file it writes as a diff when there is one; asks are the secrets
// it'll need after. A command that brings code is always asked (DRV-75).
// From the CLI (blitz do), the person confirms in their terminal: the
// first run answers with what to confirm (errConfirm).
func (w *Workspace) askChange(ctx context.Context, c api.BlitzCommand, detail, diff string, asks ...string) error {
	if run, ok := ctx.Value(cliRunKey{}).(*cliRun); ok {
		if !run.confirmed {
			return &errConfirm{detail: detail, diff: diff, asks: asks}
		}
		return nil
	}
	return w.tools.Hooks().Approve(ctx, api.ApprovalRequest{
		Tool: "blitz:" + c.Name, Kind: api.ActionBlitz, Detail: detail, Diff: diff,
		Key: "blitz:" + c.Name, KeyLabel: "blitz " + c.Name, Targets: []string{c.Name},
		MustAsk: c.BringsCode,
	})
}

// fileDiff is a new or changed file as a unified diff for an approval:
// the old text's lines removed, the new one's added.
func fileDiff(path, old, text string) string {
	var b strings.Builder
	from := "a/" + path
	if old == "" {
		from = "/dev/null"
	}
	fmt.Fprintf(&b, "--- %s\n+++ b/%s\n", from, path)
	oldLines, newLines := lines(old), lines(text)
	fmt.Fprintf(&b, "@@ -%d,%d +1,%d @@\n", min(1, len(oldLines)), len(oldLines), len(newLines))
	for _, l := range oldLines {
		b.WriteString("-" + l + "\n")
	}
	for _, l := range newLines {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// --- connections (DRV-10..15)

// connectorKindInfo is a kind as connector list gives it.
type connectorKindInfo struct {
	Kind, Category, Name, Hint string
	Fields                     []connectorFieldInfo
	CanBeReadOnly              bool
	ReadyTools                 []string
}

type connectorFieldInfo struct {
	Name, Label, Help, Type string
	Required, Secret, Path  bool
	Default                 string
}

type connectionInfo struct {
	Name, Kind, Category, Scope, Description, Problem string
	On, Trusted, ReadOnly                             bool
	Queries                                           []string
}

func (w *Workspace) cmdConnectorList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	cat := w.ConnectorCatalog()
	lang := w.replyIn().Tag().String()
	var kinds []connectorKindInfo
	for _, k := range cat.Kinds {
		if k.Problem != "" {
			continue
		}
		ki := connectorKindInfo{Kind: k.ID, Category: k.Category, Name: k.Name.In(lang), Hint: k.Hint.In(lang), CanBeReadOnly: k.ReadOnly != nil}
		for _, f := range k.Fields {
			ki.Fields = append(ki.Fields, connectorFieldInfo{Name: f.Key, Label: f.Label.In(lang), Help: f.Help.In(lang), Type: f.Type, Required: f.Required, Secret: f.Secret(), Path: f.Type == connectors.FieldPath, Default: f.Default})
		}
		for _, t := range k.Tools {
			ki.ReadyTools = append(ki.ReadyTools, t.ID)
		}
		kinds = append(kinds, ki)
	}
	var conns []connectionInfo
	for _, c := range w.Connectors() {
		ci := connectionInfo{Name: c.Name, Kind: c.Kind, Category: c.Category, Scope: c.Scope, Description: c.Description, Problem: c.Problem, On: c.On, Trusted: c.Trusted, ReadOnly: c.ReadOnly}
		for _, q := range c.Queries {
			ci.Queries = append(ci.Queries, q.Name)
		}
		conns = append(conns, ci)
	}
	return map[string]any{"kinds": kinds, "connections": conns}, nil
}

// cmdConnectorSave saves a connection of this workspace (DRV-11): checked
// as Studio's form checks it, its file shown in the approval, then each
// required secret it doesn't have asked of the person, hidden; saved as
// Studio saves it (trusted), on here, and tested.
func (w *Workspace) cmdConnectorSave(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	cat := w.ConnectorCatalog()
	k, ok := cat.Kind(str(args, "kind"))
	if !ok {
		var ids []string
		for _, k := range cat.Kinds {
			ids = append(ids, k.ID)
		}
		return nil, fmt.Errorf("there's no kind %q: one of %s (connector list says what each takes)", str(args, "kind"), strings.Join(ids, ", "))
	}
	category, _ := cat.Category(k.Category)
	name := str(args, "name")
	conn := connectors.Connection{Name: name, Category: k.Category, Scope: connectors.ScopeWorkspace, Kind: k.ID, Description: str(args, "description"), ReadOnly: flag(args, "read_only"), Settings: map[string]string{}}
	var dropped []string
	settings, _ := args["settings"].(map[string]any)
	for key, v := range settings {
		f, known := k.Field(key)
		if !known {
			return nil, fmt.Errorf("%s has no field %q (connector list says what it takes)", k.ID, key)
		}
		if f.Secret() { // never from the model: asked of the person
			dropped = append(dropped, key)
			continue
		}
		conn.Settings[key] = fmt.Sprint(v)
	}
	for _, q := range asSlice(args["queries"]) {
		var query connectors.Query
		b, _ := json.Marshal(q)
		if err := json.Unmarshal(b, &query); err != nil {
			return nil, fmt.Errorf("a query: %w", err)
		}
		conn.Queries = append(conn.Queries, query)
	}
	previous := ""
	for _, ex := range w.Connectors() {
		if ex.Name == name && ex.Category == k.Category {
			if ex.Scope != connectors.ScopeWorkspace {
				return nil, fmt.Errorf("%s is a %s connection: Settings changes it", name, ex.Scope)
			}
			previous = name
		}
	}
	d := w.connectorDirs()
	// The secrets it needs and hasn't got: asked of the person once the
	// file is approved. To show the file, a stand-in fills them.
	var missing []string
	stand := map[string]string{}
	for _, f := range k.SecretFields() {
		if f.Required && !connectors.HasSecret(d, category, conn, f.Key) {
			missing = append(missing, f.Key)
			stand[f.Key] = "-"
		}
	}
	path, text, problems, err := connectors.Preview(cat, d, conn, stand, previous)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	old, _ := os.ReadFile(path)
	rel, _ := filepath.Rel(w.Dir(), path)
	detail := fmt.Sprintf("Save the %s connection %s (%s)", k.ID, name, map[bool]string{true: "read-only", false: "may write"}[conn.ReadOnly])
	if len(missing) > 0 {
		detail += "; then you're asked for " + strings.Join(missing, ", ")
	}
	if err := w.askChange(ctx, c, detail, fileDiff(filepath.ToSlash(rel), string(old), text), missing...); err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, key := range missing {
		f, _ := k.Field(key)
		v, err := w.askSecret(ctx, key, fmt.Sprintf("%s for the %s connection %s", f.Label.In(w.replyIn().Tag().String()), k.ID, name))
		if err != nil {
			return nil, err
		}
		values[key] = v
	}
	saved, problems, err := w.SaveConnector(ctx, conn, values, previous)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	if !saved.On {
		if err := w.SetConnectorOn(ctx, saved.Category, saved.Scope, saved.Name, true); err != nil {
			return nil, err
		}
	}
	out := map[string]any{"saved": rel, "trusted": true, "on": true,
		"next": fmt.Sprintf("The agents have its tools from the next turn, and @db:%s works in the next message; Studio › Connectors shows it.", name)}
	if len(dropped) > 0 {
		out["note"] = "secret fields aren't taken as arguments: " + strings.Join(dropped, ", ") + " were asked of the person or left as they were"
	}
	if tools, err := w.TestConnector(ctx, saved, nil); err != nil {
		out["test"] = "it couldn't connect: " + err.Error()
	} else {
		var names []string
		for _, t := range tools {
			names = append(names, t.Name)
		}
		out["tools"] = names
	}
	return out, nil
}

// askSecret asks the person for a secret's value (key, for what), hidden,
// and never shown, recorded or given to the model (DRV-05). From the CLI
// it's what the person typed in their terminal.
func (w *Workspace) askSecret(ctx context.Context, key, what string) (string, error) {
	if run, ok := ctx.Value(cliRunKey{}).(*cliRun); ok {
		if v := run.secrets[key]; v != "" {
			return v, nil
		}
		return "", fmt.Errorf("%s is needed", key)
	}
	if !tools.Attended(ctx) {
		return "", errors.New("a secret is needed, and nobody is here to type it: set it in Studio › Secrets")
	}
	v, err := w.tools.Hooks().AskUser(api.WithSecretAnswer(ctx), "Type the "+what+" (it's kept as a Blitz secret and never shown to the model)", nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(v) == "" {
		return "", errors.New("no value was typed: nothing was saved")
	}
	return v, nil
}

func (w *Workspace) cmdConnectorOn(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name, on := str(args, "name"), c.Name == "connector enable"
	for _, conn := range w.Connectors() {
		if conn.Name != name {
			continue
		}
		if err := w.askChange(ctx, c, fmt.Sprintf("Turn the connection %s %s in this workspace", name, map[bool]string{true: "on", false: "off"}[on]), ""); err != nil {
			return nil, err
		}
		if err := w.SetConnectorOn(ctx, conn.Category, conn.Scope, conn.Name, on); err != nil {
			return nil, err
		}
		return map[string]any{"name": name, "on": on}, nil
	}
	return nil, fmt.Errorf("there's no connection %q (connector list)", name)
}

// --- settings, doctor, the organization (DRV-80..82)

func (w *Workspace) cmdSettingsShow(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	s := w.Settings()
	out := map[string]any{
		"workspace_model": refOf(s.Model), "agent": s.Agent, "effort": s.Effort, "mode": s.PermissionMode,
		"image_model": s.ImageModel, "speech_model": s.SpeechModel,
	}
	if s.ModeNote != "" {
		out["mode_note"] = s.ModeNote
	}
	if cur, ok := w.ActiveSession(); ok && cur.Model != "" {
		out["conversation_model"] = cur.Model
	}
	return out, nil
}

func refOf(m api.ModelInfo) string {
	if m.Provider == "" || strings.Contains(m.Name, "/") {
		return m.Name
	}
	return m.Provider + "/" + m.Name
}

// cmdSettingsSet changes one setting (DRV-80), the mode only to a
// stricter one (DRV-71).
func (w *Workspace) cmdSettingsSet(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	key, value := str(args, "key"), str(args, "value")
	s := w.Settings()
	from := map[string]string{"model": refOf(s.Model), "image_model": s.ImageModel, "speech_model": s.SpeechModel, "effort": s.Effort, "mode": s.PermissionMode}[key]
	if key == "mode" {
		m, err := api.ParsePermissionMode(value)
		if err != nil {
			return nil, err
		}
		if api.Looser(m, api.PermissionMode(s.PermissionMode)) {
			return nil, fmt.Errorf("the mode only changes to a stricter one here: %s is looser than %s, so the person changes it (the status bar's mode, or /mode)", m, s.PermissionMode)
		}
	}
	if err := w.askChange(ctx, c, fmt.Sprintf("Change %s from %q to %q", key, from, value), ""); err != nil {
		return nil, err
	}
	switch key {
	case "model":
		if cur, ok := w.ActiveSession(); ok && cur.MessageCount > 0 {
			p, err := w.SetSessionModel(ctx, cur.ID, value)
			if err != nil {
				return nil, err
			}
			return map[string]any{"conversation_model": value, "switch": p}, nil
		}
		if _, err := w.SetModel(ctx, value); err != nil {
			return nil, err
		}
	case "effort":
		if _, err := w.Set(ctx, "effort", value); err != nil {
			return nil, err
		}
	case "mode":
		got, err := w.SetPermissionMode(value)
		if err != nil {
			return nil, err
		}
		value = got
	case "image_model", "speech_model":
		k := map[string]string{"image_model": "images.model", "speech_model": "audio.model"}[key]
		if _, err := config.SetValue(w.cfg.Dir, w.Dir(), k, value); err != nil {
			return nil, err
		}
		cfg, err := config.LoadWorkspace(w.cfg.Dir, w.Dir())
		if err != nil {
			return nil, err
		}
		if err := w.ReloadProviders(ctx, cfg); err != nil {
			return nil, err
		}
	}
	return map[string]any{key: value}, nil
}

// doctorCheck is one of doctor's answers.
type doctorCheck struct {
	Check, State, Detail string // State: ok, warning or problem
}

// cmdDoctor checks Blitz here (DRV-81).
func (w *Workspace) cmdDoctor(ctx context.Context, _ api.BlitzCommand, _ map[string]any) (any, error) {
	var out []doctorCheck
	add := func(check, state, detail string) { out = append(out, doctorCheck{check, state, detail}) }
	cfg := w.settings()
	s := w.Settings()
	if err := w.ModelErr(); err != nil {
		add("model", "problem", fmt.Sprintf("%s can't be used: %v", refOf(s.Model), err))
	} else {
		add("model", "ok", refOf(s.Model))
	}
	providers := runtime.ConfiguredProviders(cfg)
	add("providers", map[bool]string{true: "ok", false: "problem"}[len(providers) > 0], strings.Join(providers, ", "))
	provider, _ := runtime.ParseModelRef(cfg.ModelName(), cfg.LLM.Provider)
	if provider == "ollama" || slices.Contains(providers, "ollama") || runtime.OllamaAnswers(ctx) {
		if runtime.OllamaAnswers(ctx) {
			add("ollama", "ok", "answers on this computer")
		} else {
			add("ollama", "problem", "doesn't answer at localhost:11434: start it with `ollama serve`")
		}
	}
	mode := s.PermissionMode
	if s.ModeNote != "" {
		add("mode", "warning", mode+": "+s.ModeNote)
	} else {
		add("mode", "ok", mode)
	}
	add("sandbox", "ok", strings.Join(w.SandboxSummary(), "; "))
	if err := w.PermissionsProblem(); err != nil {
		add("permissions", "problem", err.Error())
	}
	for _, m := range w.ListMCPServers() {
		if m.Error != "" {
			add("mcp "+m.Name, "problem", m.Error)
		}
	}
	for _, c := range w.Connectors() {
		if c.Problem != "" {
			add("connection "+c.Name, "problem", c.Problem)
		}
	}
	for _, l := range w.LanguageStatus() {
		if l.Err != "" {
			add("language server "+l.Language, "warning", l.Err)
		}
	}
	if o := cfg.Org; o.Active() && o.Problem != "" {
		add("organization", "problem", o.Problem)
	}
	return out, nil
}

func (w *Workspace) cmdOrgPolicy(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	o := w.settings().Org
	if !o.Active() {
		return map[string]any{"organization": false, "note": "This workspace isn't under an organization: its own settings decide."}, nil
	}
	p := o.Policy
	out := map[string]any{
		"own_providers": p.Models.OwnProviders, "own_mcp_servers": p.MCP.Own, "own_connections": p.Connectors.Own,
		"connection_kinds": p.Connectors.Kinds, "shell_commands": p.Tools.ShellCommands, "tools_off": p.Tools.Off, "builtins_off": p.Builtins.Off,
	}
	if p.Plugins.Own != nil {
		out["own_plugins"] = *p.Plugins.Own
	}
	if !p.Connectors.Own {
		out["note"] = "Your own connections aren't allowed: an Admin sets [connectors] own = true in the organization's policy.toml."
	}
	return out, nil
}

func (w *Workspace) cmdOrgStatus(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	cfg := w.settings()
	if cfg.Enterprise.URL == "" {
		return map[string]any{"organization": false}, nil
	}
	o := cfg.Org
	out := map[string]any{"organization": true, "service": cfg.Enterprise.URL, "catalog": o.Active()}
	if !o.Expires.IsZero() {
		out["catalog_expires"] = o.Expires
	}
	if o.Problem != "" {
		out["problem"] = o.Problem
	}
	return out, nil
}

// --- opening things for the person (DRV-30, DRV-31)

// cmdOpen opens a page, a file, a Studio or Settings section, or a view
// in the newest window.
func (w *Workspace) cmdOpen(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	var cmd api.AgentBrowserCommand
	switch c.Name {
	case "open url":
		return nil, errors.New("use the open_page tool for a web page: it asks where the person wants it")
	case "open file":
		rel, err := w.tools.EditorPath(str(args, "path"))
		if err != nil {
			return nil, err
		}
		cmd = api.AgentBrowserCommand{Kind: "file", Path: rel, Line: num(args, "line")}
	case "open studio":
		cmd = api.AgentBrowserCommand{Kind: "view", Path: "studio:" + str(args, "section")}
	case "open settings":
		cmd = api.AgentBrowserCommand{Kind: "view", Path: "settings:" + str(args, "section")}
	case "open view":
		cmd = api.AgentBrowserCommand{Kind: "view", Path: str(args, "view")}
	}
	if err := w.toWindow(ctx, cmd); err != nil {
		return nil, fmt.Errorf("%w: tell the person where it is instead", err)
	}
	return map[string]any{"opened": true}, nil
}

// toWindow hands cmd to the newest window watching the workspace.
func (w *Workspace) toWindow(ctx context.Context, cmd api.AgentBrowserCommand) error {
	h := &w.browsers
	h.mu.Lock()
	if len(h.windows) == 0 {
		h.mu.Unlock()
		return errors.New("no Blitz window is open")
	}
	win := h.windows[len(h.windows)-1]
	h.mu.Unlock()
	select {
	case win.send <- cmd:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// cmdPicture opens Pictures with the prompt filled in, for the person to
// make it there, its cost shown first (DRV-30).
func (w *Workspace) cmdPicture(ctx context.Context, _ api.BlitzCommand, args map[string]any) (any, error) {
	b, _ := json.Marshal(map[string]any{"prompt": str(args, "prompt"), "model": str(args, "model"), "references": list(args, "references")})
	if err := w.toWindow(ctx, api.AgentBrowserCommand{Kind: "picture", Message: string(b)}); err != nil {
		return nil, fmt.Errorf("%w: use generate_image to make it here", err)
	}
	return map[string]any{"opened": "Pictures", "note": "The person makes it there, seeing its cost first; it isn't made yet."}, nil
}

// --- evals, specs (DRV-40, DRV-41)

func (w *Workspace) cmdEvalList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	var out []map[string]any
	for _, e := range w.Evals() {
		out = append(out, map[string]any{"name": e.Name, "cases": len(e.Cases)})
	}
	return out, nil
}

// cmdEvalRun runs an eval set in the background, after the person agrees
// (it calls models), so the turn goes on.
func (w *Workspace) cmdEvalRun(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	if !slices.ContainsFunc(w.Evals(), func(e evals.Eval) bool { return e.Name == name }) {
		return nil, fmt.Errorf("there's no eval set %q (eval list)", name)
	}
	if err := w.askChange(ctx, c, fmt.Sprintf("Run the evals %s: each case calls models, which costs", name), ""); err != nil {
		return nil, err
	}
	go func() { _, _ = w.RunEval(context.WithoutCancel(ctx), name, api.EvalOptions{}) }()
	return map[string]any{"started": name, "note": "It runs in the background; eval results reads it once done (Studio › Evals shows it as it goes)."}, nil
}

func (w *Workspace) cmdEvalResults(_ context.Context, _ api.BlitzCommand, args map[string]any) (any, error) {
	return w.EvalRuns(str(args, "name"), 3)
}

func (w *Workspace) cmdSpecList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.ListSpecs()
}

func (w *Workspace) cmdSpecReview(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	p, judge := str(args, "path"), flag(args, "judge")
	file, err := w.ReadFile(p)
	if err != nil {
		return nil, err
	}
	if judge {
		if err := w.askChange(ctx, c, "Have a model review the spec "+p+" (it costs)", ""); err != nil {
			return nil, err
		}
	}
	return w.ReviewSpec(ctx, p, file.Text, judge)
}

func (w *Workspace) cmdSpecVerify(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	p, judge := str(args, "path"), flag(args, "judge")
	if judge {
		est, err := w.VerifySpec(ctx, p, true, true)
		if err != nil {
			return nil, err
		}
		if err := w.askChange(ctx, c, fmt.Sprintf("Have a model check the requirements of %s (about $%.2f)", p, est.EstimatedUSD), ""); err != nil {
			return nil, err
		}
	}
	return w.VerifySpec(ctx, p, judge, false)
}

func (w *Workspace) cmdSpecImplement(_ context.Context, _ api.BlitzCommand, args map[string]any) (any, error) {
	return w.PlanSpec(str(args, "path"))
}

// --- memory, chats, processes, approvals, secrets (phase 2)

func (w *Workspace) cmdMemoryList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.ListNotes()
}

func (w *Workspace) cmdMemoryForget(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	if err := w.askChange(ctx, c, "Forget the note "+name, ""); err != nil {
		return nil, err
	}
	return map[string]any{"forgot": name}, w.ForgetNote(name)
}

func (w *Workspace) cmdChatNew(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	s, err := w.NewSession()
	if err != nil {
		return nil, err
	}
	return map[string]any{"session": s.ID, "note": "The next message starts it."}, nil
}

func (w *Workspace) cmdChatExport(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	cur, ok := w.ActiveSession()
	if !ok {
		return nil, errors.New("there's no conversation to export")
	}
	md, err := w.ExportSession(cur.ID)
	if err != nil {
		return nil, err
	}
	p := str(args, "path")
	rel, err := w.tools.Workspace().WritablePath(p)
	if err != nil {
		return nil, err
	}
	if err := w.askChange(ctx, c, "Write this conversation to "+rel, fileDiff(filepath.ToSlash(rel), "", md)); err != nil {
		return nil, err
	}
	if _, err := w.WriteFile(ctx, rel, md, ""); err != nil {
		return nil, err
	}
	return map[string]any{"written": rel}, nil
}

func (w *Workspace) cmdChatSearch(_ context.Context, _ api.BlitzCommand, args map[string]any) (any, error) {
	found, text := w.SearchSession(str(args, "terms"))
	return map[string]any{"found": found, "matches": text}, nil
}

func (w *Workspace) cmdProcessList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.RunningProcesses(), nil
}

func (w *Workspace) cmdProcessStop(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	id := num(args, "id")
	i := slices.IndexFunc(w.RunningProcesses(), func(p api.ProcessInfo) bool { return p.ID == id })
	if i < 0 {
		return nil, fmt.Errorf("no process %d is running (process list)", id)
	}
	if err := w.askChange(ctx, c, fmt.Sprintf("Stop process %d (%s)", id, w.RunningProcesses()[i].Command), ""); err != nil {
		return nil, err
	}
	return w.StopProcess(id)
}

func (w *Workspace) cmdApprovalList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.ListApprovals(), nil
}

// cmdApprovalForget only ever makes Blitz ask more, so it isn't asked.
func (w *Workspace) cmdApprovalForget(_ context.Context, _ api.BlitzCommand, args map[string]any) (any, error) {
	n, err := w.RevokeApprovals(str(args, "key"))
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("no saved approval %q (approval list)", str(args, "key"))
	}
	return map[string]any{"forgot": n}, nil
}

func (w *Workspace) cmdSecretList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	entries, err := config.Vault(w.cfg.Dir).List(config.SecretScope(w.cfg.Dir, w.Dir()))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Key)
	}
	return map[string]any{"names": names, "note": "Values are never shown."}, nil
}

func (w *Workspace) cmdSecretSet(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	if err := w.askChange(ctx, c, "Set this workspace's secret "+name+" (you type its value next)", "", name); err != nil {
		return nil, err
	}
	v, err := w.askSecret(ctx, name, "value of the secret "+name)
	if err != nil {
		return nil, err
	}
	if err := config.Vault(w.cfg.Dir).Set(config.SecretScope(w.cfg.Dir, w.Dir()), name, v); err != nil {
		return nil, err
	}
	return map[string]any{"set": name, "use": "${" + name + "}"}, nil
}

// --- workflows, agents, skills (DRV-50, DRV-51)

func (w *Workspace) cmdWorkflowList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.Workflows(), nil
}

func (w *Workspace) cmdWorkflowSave(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	var f flows.Flow
	b, _ := json.Marshal(args["workflow"])
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("the workflow: %w", err)
	}
	if probs := flows.Check(f); len(probs) > 0 {
		return nil, errors.New(strings.Join(probs, "; "))
	}
	var steps []string
	for _, n := range f.Nodes {
		steps = append(steps, fmt.Sprintf("%s (%s)", firstOf(n.Title, n.ID), n.Type))
	}
	if err := w.askChange(ctx, c, fmt.Sprintf("Save the workflow %s: %s", f.Name, strings.Join(steps, ", ")), ""); err != nil {
		return nil, err
	}
	saved, problems, err := w.SaveWorkflow(f, str(args, "previous"))
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return map[string]any{"saved": saved.Name, "note": "Studio › Workflows shows it; it runs when the person runs or schedules it."}, nil
}

// firstOf is a unless it's empty, else b.
func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (w *Workspace) cmdAgentList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.ListAgents(), nil
}

func (w *Workspace) cmdAgentSave(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	spec := &agents.AgentSpec{SystemPrompt: str(args, "instructions")}
	spec.Name, spec.Description, spec.Tools, spec.DefaultModel = name, str(args, "description"), list(args, "tools"), str(args, "model")
	dir := w.ProjectAgentsDir()
	previous := ""
	if _, err := os.Stat(filepath.Join(dir, name+".md")); err == nil {
		previous = name
	}
	old, _ := os.ReadFile(filepath.Join(dir, name+".md"))
	rel, _ := filepath.Rel(w.Dir(), filepath.Join(dir, name+".md"))
	if err := w.askChange(ctx, c, "Save the agent "+name, fileDiff(filepath.ToSlash(rel), string(old), "# "+name+"\n"+spec.SystemPrompt)); err != nil {
		return nil, err
	}
	path, problems, err := agents.Save(dir, spec, previous, w.agents.IsBuiltin)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	w.RefreshAgents(ctx)
	rel, _ = filepath.Rel(w.Dir(), path)
	return map[string]any{"saved": rel, "note": "Its evals run when the person runs them (Studio › Evals): never in this turn."}, nil
}

func (w *Workspace) cmdSkillList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.ListSkills(), nil
}

func (w *Workspace) cmdSkillSave(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	spec := api.SkillSpec{Name: str(args, "name"), Description: str(args, "description"), Instructions: str(args, "instructions")}
	if err := w.askChange(ctx, c, "Save the skill "+spec.Name+": "+spec.Description, fileDiff(".agents/skills/"+spec.Name+"/SKILL.md", "", spec.Instructions)); err != nil {
		return nil, err
	}
	previous, hash := "", ""
	if cur, err := w.GetSkillFile(spec.Name); err == nil && cur.Editable {
		previous, hash = spec.Name, cur.Hash
	}
	f, problems, err := w.SaveSkill(spec, previous, hash)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return map[string]any{"saved": f.Path}, nil
}

// --- what brings code, and Studio's other items (phase 4)

func (w *Workspace) cmdMCPList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.ListMCPServers(), nil
}

// cmdMCPAdd adds an MCP server to the workspace: asked with exactly what
// will run, always (DRV-75), and refused where the organization's policy
// keeps people's own out (DRV-72).
func (w *Workspace) cmdMCPAdd(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	if o := w.settings().Org; o.Active() && !o.Policy.MCP.Own {
		return nil, errors.New("your organization doesn't allow MCP servers of your own")
	}
	s := config.MCPServerConfig{Name: str(args, "name"), Command: str(args, "command"), Args: list(args, "args"), URL: str(args, "url")}
	if (s.Command == "") == (s.URL == "") {
		return nil, errors.New("give either command (with args) or url")
	}
	what := "connects to " + s.URL
	if s.Command != "" {
		what = "runs: " + strings.Join(append([]string{s.Command}, s.Args...), " ")
	}
	if err := w.askChange(ctx, c, fmt.Sprintf("Add the MCP server %s, which %s", s.Name, what), ""); err != nil {
		return nil, err
	}
	path, err := config.AddMCPServer(w.cfg.Dir, w.Dir(), s)
	if err != nil {
		return nil, err
	}
	return map[string]any{"added": s.Name, "file": path}, w.reloadMCPFromSettings(ctx)
}

func (w *Workspace) cmdMCPOn(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name, on := str(args, "name"), c.Name == "mcp enable"
	i := slices.IndexFunc(w.ListMCPServers(), func(m api.MCPServer) bool { return m.Name == name })
	if i < 0 {
		return nil, fmt.Errorf("there's no MCP server %q (mcp list)", name)
	}
	what := "Turn the MCP server " + name + " off here"
	if on {
		what = "Turn the MCP server " + name + " on here: it runs " + w.ListMCPServers()[i].Target
	}
	if err := w.askChange(ctx, c, what, ""); err != nil {
		return nil, err
	}
	if _, err := config.SetMCPServerEnabled(w.cfg.Dir, w.Dir(), name, on); err != nil {
		return nil, err
	}
	return map[string]any{"name": name, "on": on}, w.reloadMCPFromSettings(ctx)
}

// reloadMCPFromSettings serves the MCP servers the settings now name.
func (w *Workspace) reloadMCPFromSettings(ctx context.Context) error {
	cfg, err := config.LoadWorkspace(w.cfg.Dir, w.Dir())
	if err != nil {
		return err
	}
	return w.ReloadMCP(ctx, cfg)
}

func (w *Workspace) cmdRuleList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.Rules(), nil
}

func (w *Workspace) cmdRuleSave(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	spec := RuleSpec{Name: str(args, "name"), Description: str(args, "description"), Text: str(args, "text")}
	if err := w.askChange(ctx, c, "Save the rule "+spec.Name, fileDiff(".agents/rules/"+spec.Name+".md", "", spec.Text)); err != nil {
		return nil, err
	}
	r, problems, err := w.SaveRule(spec, w.existingRule(spec.Name))
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return map[string]any{"saved": r.Name}, nil
}

func (w *Workspace) existingRule(name string) string {
	if slices.ContainsFunc(w.Rules(), func(r gates.Rule) bool { return r.Name == name }) {
		return name
	}
	return ""
}

func (w *Workspace) cmdGuardrailList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.Guardrails(), nil
}

func (w *Workspace) cmdGuardrailSave(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	spec := GuardrailSpec{Name: str(args, "name"), Description: str(args, "description"), Stage: str(args, "stage"), Action: str(args, "action"), Text: str(args, "check")}
	if err := w.askChange(ctx, c, fmt.Sprintf("Save the guardrail %s (%s, %s)", spec.Name, spec.Stage, firstOf(spec.Action, "block")), fileDiff(".agents/guardrails/"+spec.Name+".md", "", spec.Text)); err != nil {
		return nil, err
	}
	previous := ""
	if slices.ContainsFunc(w.Guardrails(), func(g gates.Guardrail) bool { return g.Name == spec.Name && g.Builtin == "" }) {
		previous = spec.Name
	}
	g, problems, err := w.SaveGuardrail(spec, previous)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return map[string]any{"saved": g.Name}, nil
}

func (w *Workspace) cmdCommandList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.ListCommands(), nil
}

// cmdCommandSave writes .blitz/commands/<name>.md, the workspace's own
// slash command.
func (w *Workspace) cmdCommandSave(ctx context.Context, c api.BlitzCommand, args map[string]any) (any, error) {
	name := str(args, "name")
	if !gates.ValidName(name) {
		return nil, fmt.Errorf("name %q: lowercase letters, digits, - and _", name)
	}
	rel := filepath.ToSlash(filepath.Join(".blitz", "commands", name+".md"))
	text := "---\ndescription: " + strconv.Quote(str(args, "description")) + "\n---\n" + str(args, "prompt") + "\n"
	// An existing command is replaced: written over the version read here.
	var old, version string
	if cur, err := w.ReadFile(rel); err == nil {
		old, version = cur.Text, cur.Version
	}
	if err := w.askChange(ctx, c, "Save the command /"+name, fileDiff(rel, old, text)); err != nil {
		return nil, err
	}
	if _, err := w.WriteFile(ctx, rel, text, version); err != nil {
		return nil, err
	}
	return map[string]any{"saved": rel, "use": "/" + name}, nil
}

func (w *Workspace) cmdHookList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	return w.ListHooks(), nil
}

func (w *Workspace) cmdPluginList(context.Context, api.BlitzCommand, map[string]any) (any, error) {
	var out []map[string]any
	for _, p := range w.plugins {
		out = append(out, map[string]any{"name": p.Name, "dir": p.Dir})
	}
	return out, nil
}

// --- the CLI (blitz do, DRV-02, DRV-74)

// cliRunKey marks a run from the CLI: the person confirms in their
// terminal, and types the secrets there.
type cliRunKey struct{}

type cliRun struct {
	confirmed bool
	secrets   map[string]string
}

// errConfirm is a CLI run's first answer to a command that changes
// something: what to confirm, and the secrets it'll need.
type errConfirm struct {
	detail, diff string
	asks         []string
}

func (e *errConfirm) Error() string { return "confirm: " + e.detail }

// RunBlitzCommand runs one of the catalog's commands for the CLI's blitz do
// (spec_060_blitz_tools DRV-02): help, or the command, its arguments
// checked. One that changes something answers with what the person
// confirms (and the secrets it needs) until confirm, with secrets.
func (w *Workspace) RunBlitzCommand(ctx context.Context, name string, args map[string]any, confirm bool, secrets map[string]string) (api.BlitzRun, error) {
	name = strings.Join(strings.Fields(strings.ToLower(name)), " ")
	if name == "" || name == "help" {
		about, _ := args["command"].(string)
		return api.BlitzRun{Help: tools.BlitzHelp(about)}, nil
	}
	c, ok := api.FindBlitzCommand(name)
	if !ok {
		return api.BlitzRun{Help: tools.BlitzHelp(strings.Fields(name)[0])}, fmt.Errorf("there's no command %q", name)
	}
	if name == "job create" || name == "job list" {
		return api.BlitzRun{}, errors.New("jobs have their own commands: blitz jobs")
	}
	checked, err := tools.CheckBlitzArgs(c, args)
	if err != nil {
		return api.BlitzRun{Help: c.Usage()}, err
	}
	out, err := w.runBlitzCommand(context.WithValue(ctx, cliRunKey{}, &cliRun{confirmed: confirm, secrets: secrets}), c, checked)
	var ask *errConfirm
	if errors.As(err, &ask) {
		return api.BlitzRun{Confirm: ask.detail, Diff: ask.diff, Asks: ask.asks}, nil
	}
	if err != nil {
		return api.BlitzRun{}, err
	}
	return api.BlitzRun{Result: out}, nil
}
