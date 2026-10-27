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
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// Guardrails (spec_gates_041 GT-20 on): safety and limits at every step of
// a turn, read from .agents/guardrails (then the global folder) as the
// turn starts. Unlike rules, a guardrail that can't be checked refuses
// (block) rather than lets things through.

// guardSubjectLimit bounds what a custom guardrail's judge is shown, and
// how much of a tool's result the injection patterns read.
const (
	guardSubjectLimit = 12 << 10
	guardScanLimit    = 256 << 10
)

// writeTools change files: the limits guardrail counts them.
var writeTools = []string{"create_file", "replace_in_file", "delete_file", "notebook_edit"}

// ProjectGuardrailsDir is where Studio writes the workspace's guardrails:
// .agents/guardrails.
func (w *Workspace) ProjectGuardrailsDir() string {
	return filepath.Join(w.Dir(), ".agents", "guardrails")
}

// guardrailDirs are the folders guardrails are read from: the
// workspace's, then the user's (~/.blitz/guardrails); a name in the
// workspace's wins.
func (w *Workspace) guardrailDirs() []string {
	return []string{w.ProjectGuardrailsDir(), filepath.Join(config.ConfigDir(""), "guardrails")}
}

// Guardrails are the workspace's guardrails, read afresh: the built-in
// ones (with their defaults when they have no file), then custom ones.
func (w *Workspace) Guardrails() []gates.Guardrail { return gates.LoadGuardrails(w.guardrailDirs()...) }

// guardState is what the guardrails hold during a turn: those read as it
// started, and the notices already given (once per turn and guardrail).
type guardState struct {
	all  []gates.Guardrail
	said sync.Map
}

// guarding holds the current turns' guardrails, by session.
type guarding struct {
	turns sync.Map // session ID → *guardState
}

// startGuardrails reads the guardrails for a turn in the session, and
// applies the limits guardrail's turn and cost limits to t.
func (w *Workspace) startGuardrails(sessionID string, t *api.Turn) {
	st := &guardState{all: w.Guardrails()}
	w.guards.turns.Store(sessionID, st)
	for _, g := range gates.Active(st.all, gates.StageAction, w.engine.ActiveAgent()) {
		if g.Builtin != gates.BuiltinLimits {
			continue
		}
		if l := g.Limits.MaxTurns; l > 0 && (t.MaxTurns == 0 || l < t.MaxTurns) {
			t.MaxTurns = l
		}
		if l := g.Limits.MaxCostUSD; l > 0 && (t.MaxCostUSD == 0 || l < t.MaxCostUSD) {
			t.MaxCostUSD = l
		}
	}
}

// guardsFor are the guardrails of the session's turn. A call outside one
// (a background task's after its turn ended) reads them afresh: never
// another session's turn's, whose notices it would share.
func (w *Workspace) guardsFor(sessionID string) *guardState {
	if v, ok := w.guards.turns.Load(sessionID); ok {
		return v.(*guardState)
	}
	return &guardState{all: w.Guardrails()}
}

// once reports whether a notice for key hasn't been given this turn.
func (st *guardState) once(key string) bool {
	_, said := st.said.LoadOrStore(key, true)
	return !said
}

// checked records, once per turn, that a built-in guardrail checked
// something and let it through: its catches are recorded each time, so
// without this a quiet one would look as if it never ran (the
// dashboard's "decided n times").
func (w *Workspace) checked(ctx context.Context, st *guardState, g gates.Guardrail) {
	if st.once("checked:" + g.Name) {
		w.auditGuardrail(ctx, g, "pass", "checked: nothing found")
	}
}

// auditGuardrail records a guardrail's decision in the audit log.
func (w *Workspace) auditGuardrail(ctx context.Context, g gates.Guardrail, decision, detail string) {
	w.tools.Hooks().Audit().LogContext(ctx, audit.Entry{Kind: audit.KindGuardrail, Tool: "guardrail:" + g.Name, Detail: detail, Decision: decision})
}

// judgeGuardrail judges subject against a custom guardrail, secrets
// redacted first.
func (w *Workspace) judgeGuardrail(ctx context.Context, g gates.Guardrail, subject string) (gates.Verdict, error) {
	return gates.JudgeGuardrail(ctx, w.ruleAsker(), g, w.secretRedactor().String(textutil.Ellipsize(subject, guardSubjectLimit)))
}

// TryGuardrail judges a sample against a guardrail as written (Studio's
// Try it): a custom one by the judge; the built-in injection and secrets
// ones by their patterns.
func (w *Workspace) TryGuardrail(ctx context.Context, g gates.Guardrail, sample string) (gates.Verdict, error) {
	switch g.Builtin {
	case gates.BuiltinInjection:
		if phrase := gates.Injection(sample); phrase != "" {
			return gates.Verdict{Reason: i18n.T("guardrail.injection_found", "phrase", phrase)}, nil
		}
		return gates.Verdict{Pass: true, Reason: i18n.T("guardrail.injection_none")}, nil
	case gates.BuiltinSecrets:
		if masked, found := w.maskOutput(g, sample); found {
			return gates.Verdict{Reason: i18n.T("guardrail.secrets_found", "text", masked)}, nil
		}
		return gates.Verdict{Pass: true, Reason: i18n.T("guardrail.secrets_none")}, nil
	case gates.BuiltinLimits:
		return gates.Verdict{}, fmt.Errorf("the limits guardrail is checked on actions, not samples")
	}
	return w.judgeGuardrail(ctx, g, sample)
}

// maskOutput is text with the secrets guardrail's masks, and whether it
// masked anything.
func (w *Workspace) maskOutput(g gates.Guardrail, text string) (string, bool) {
	masked := w.secretRedactor().String(text)
	if g.PII {
		masked, _ = redact.PII(masked)
	}
	return masked, masked != text
}

// guard is the workspace's runtime.Guard: its guardrails at every step.
type guard struct{ w *Workspace }

// external reports whether a tool brings in outside content: the web,
// the browser, an MCP server.
func (g guard) external(tool string) bool {
	if slices.Contains([]string{"web_fetch", "web_search", "browser"}, tool) {
		return true
	}
	_, _, ok := g.w.tools.MCP().Lookup(tool)
	return ok
}

// ToolCall applies the action guardrails: the limits guardrail's domains,
// commands and files changed, and custom checks (judged; one that can't
// be judged refuses when it blocks).
func (g guard) ToolCall(ctx context.Context, c runtime.GuardCall) runtime.GuardVerdict {
	st := g.w.guardsFor(c.Session)
	var v runtime.GuardVerdict
	for _, gr := range gates.Active(st.all, gates.StageAction, c.Agent) {
		if !gr.Checks(c.Tool, g.external(c.Tool)) {
			continue
		}
		if gr.Builtin == gates.BuiltinLimits {
			if why := g.overLimits(gr, c); why != "" {
				g.w.auditGuardrail(ctx, gr, "block", why)
				v.Refusal = i18n.T("guardrail.refused", "name", gr.Name, "reason", why)
				v.Notices = append(v.Notices, v.Refusal)
				return v
			}
			g.w.checked(ctx, st, gr)
			continue
		}
		args, _ := json.Marshal(c.Args)
		verdict, err := g.w.judgeGuardrail(ctx, gr, fmt.Sprintf("Tool %s with arguments %s", c.Tool, args))
		switch {
		case err != nil && gr.Action == gates.Block:
			g.w.auditGuardrail(ctx, gr, "unjudged-block", err.Error())
			v.Refusal = i18n.T("guardrail.unjudged_refused", "name", gr.Name, "error", err.Error())
			return v
		case err != nil:
			g.w.auditGuardrail(ctx, gr, "unjudged", err.Error())
			v.Notices = append(v.Notices, i18n.T("guardrail.unjudged", "name", gr.Name, "error", err.Error()))
		case verdict.Pass:
			g.w.auditGuardrail(ctx, gr, "pass", verdict.Reason)
		case gr.Action == gates.Block:
			g.w.auditGuardrail(ctx, gr, "block", verdict.Reason)
			v.Refusal = i18n.T("guardrail.refused", "name", gr.Name, "reason", verdict.Reason)
			v.Notices = append(v.Notices, v.Refusal)
			return v
		default:
			g.w.auditGuardrail(ctx, gr, "warn", verdict.Reason)
			v.Notices = append(v.Notices, i18n.T("guardrail.warned", "name", gr.Name, "reason", verdict.Reason))
		}
	}
	return v
}

// overLimits is why a call breaks the limits guardrail ("" when it
// doesn't): a domain not allowed, a command denied, or one file too many.
func (g guard) overLimits(gr gates.Guardrail, c runtime.GuardCall) string {
	l := gr.Limits
	str := func(k string) string { s, _ := c.Args[k].(string); return s }
	switch {
	case c.Tool == "web_fetch" || (c.Tool == "browser" && str("action") == "navigate"):
		if u := str("url"); !gates.DomainAllowed(u, l.AllowedDomains) {
			return i18n.T("guardrail.domain", "url", u, "domains", strings.Join(l.AllowedDomains, ", "))
		}
	case c.Tool == "run_shell_command":
		if glob := gates.CommandDenied(str("command"), gates.ExpandCommands(l.Commands)); glob != "" {
			return i18n.T("guardrail.command", "glob", glob)
		}
	case slices.Contains(writeTools, c.Tool) && l.MaxFilesChanged > 0:
		changed := g.w.tools.Checkpoints().LatestTurnFiles(c.Session)
		target := str("path")
		if !filepath.IsAbs(target) {
			target = filepath.Join(g.w.Dir(), target)
		}
		if len(changed) >= l.MaxFilesChanged && !slices.Contains(changed, target) {
			return i18n.T("guardrail.files", "n", l.MaxFilesChanged)
		}
	}
	return ""
}

// ToolResult applies the tool-result guardrails: the injection patterns
// (confirmed by the judge when asked) and custom checks, warning the
// agent in the result or withholding it.
func (g guard) ToolResult(ctx context.Context, c runtime.GuardCall, result map[string]any) runtime.GuardVerdict {
	st := g.w.guardsFor(c.Session)
	var v runtime.GuardVerdict
	active := gates.Active(st.all, gates.StageToolResult, c.Agent)
	if len(active) == 0 {
		return v
	}
	data, _ := json.Marshal(result)
	text := textutil.Ellipsize(string(data), guardScanLimit)
	external := g.external(c.Tool)
	out := result
	for _, gr := range active {
		if !gr.Checks(c.Tool, external) {
			continue
		}
		caught, reason := false, ""
		if gr.Builtin == gates.BuiltinInjection {
			phrase := gates.Injection(text)
			if phrase == "" {
				g.w.checked(ctx, st, gr)
				continue
			}
			caught, reason = true, i18n.T("guardrail.injection_found", "phrase", phrase)
			if gr.Judge {
				if verdict, err := gates.ConfirmInjection(ctx, g.w.ruleAsker(), textutil.Ellipsize(text, guardSubjectLimit)); err == nil && verdict.Pass {
					g.w.auditGuardrail(ctx, gr, "pass", verdict.Reason)
					continue // the judge says it's quoted, not aimed at the agent
				}
			}
		} else {
			verdict, err := g.w.judgeGuardrail(ctx, gr, text)
			switch {
			case err != nil:
				caught, reason = gr.Action == gates.Block, i18n.T("guardrail.unjudged", "name", gr.Name, "error", err.Error())
				if !caught {
					v.Notices = append(v.Notices, reason)
					continue
				}
			case verdict.Pass:
				g.w.auditGuardrail(ctx, gr, "pass", verdict.Reason)
				continue
			default:
				caught, reason = true, verdict.Reason
			}
		}
		if !caught {
			continue
		}
		if gr.Action == gates.Redact && gr.Builtin == gates.BuiltinInjection {
			// The instructions out, the rest kept: the agent still has the page.
			g.w.auditGuardrail(ctx, gr, "redact", reason)
			out = redactInjection(out)
			out["guardrail"] = i18n.T("guardrail.agent_redacted", "name", gr.Name, "tool", c.Tool)
			v.Result = out
			v.Notices = append(v.Notices, i18n.T("guardrail.redacted", "name", gr.Name, "reason", reason))
			continue
		}
		if gr.Action == gates.Block {
			g.w.auditGuardrail(ctx, gr, "block", reason)
			v.Result = map[string]any{"error": i18n.T("guardrail.withheld", "name", gr.Name, "tool", c.Tool, "reason", reason)}
			v.Notices = append(v.Notices, i18n.T("guardrail.withheld_notice", "name", gr.Name, "tool", c.Tool, "reason", reason))
			return v
		}
		g.w.auditGuardrail(ctx, gr, "warn", reason)
		out = maps.Clone(out)
		if out == nil {
			out = map[string]any{}
		}
		out["guardrail"] = i18n.T("guardrail.agent_warning", "name", gr.Name, "tool", c.Tool, "reason", reason)
		v.Result = out
		v.Notices = append(v.Notices, i18n.T("guardrail.warned", "name", gr.Name, "reason", reason))
	}
	return v
}

// redactInjection is a tool result with the injection guardrail's
// phrases taken out of every string in it (a copy: the original is the
// tool's).
func redactInjection(result map[string]any) map[string]any {
	var clean func(v any) any
	clean = func(v any) any {
		switch x := v.(type) {
		case string:
			s, _ := gates.RedactInjection(x)
			return s
		case map[string]any:
			out := make(map[string]any, len(x))
			for k, e := range x {
				out[k] = clean(e)
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, e := range x {
				out[i] = clean(e)
			}
			return out
		}
		return v
	}
	out, _ := clean(result).(map[string]any)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

// Output applies the output guardrails to a piece of the answer: the
// secrets guardrail masks (or withholds) on every piece; custom checks
// judge the whole answer.
func (g guard) Output(ctx context.Context, c runtime.GuardCall, text string, final bool) runtime.GuardVerdict {
	st := g.w.guardsFor(c.Session)
	var v runtime.GuardVerdict
	cur := text
	for _, gr := range gates.Active(st.all, gates.StageOutput, c.Agent) {
		if gr.Builtin == gates.BuiltinSecrets {
			masked, found := g.w.maskOutput(gr, cur)
			if !found {
				g.w.checked(ctx, st, gr)
				continue
			}
			switch gr.Action {
			case gates.Redact:
				cur = masked
				if st.once(gr.Name) {
					g.w.auditGuardrail(ctx, gr, "redact", "")
					v.Notices = append(v.Notices, i18n.T("guardrail.masked", "name", gr.Name))
				}
			case gates.Block:
				cur = i18n.T("guardrail.withheld_text", "name", gr.Name)
				if st.once(gr.Name) {
					g.w.auditGuardrail(ctx, gr, "block", "")
					v.Notices = append(v.Notices, i18n.T("guardrail.withheld_answer", "name", gr.Name))
				}
			default:
				if st.once(gr.Name) {
					g.w.auditGuardrail(ctx, gr, "warn", "")
					v.Notices = append(v.Notices, i18n.T("guardrail.secret_seen", "name", gr.Name))
				}
			}
			continue
		}
		if !final {
			continue // a custom check judges the whole answer
		}
		verdict, err := g.w.judgeGuardrail(ctx, gr, cur)
		switch {
		case err != nil && gr.Action == gates.Block:
			g.w.auditGuardrail(ctx, gr, "unjudged-block", err.Error())
			cur = i18n.T("guardrail.withheld_text", "name", gr.Name)
			v.Notices = append(v.Notices, i18n.T("guardrail.unjudged", "name", gr.Name, "error", err.Error()))
		case err != nil:
			g.w.auditGuardrail(ctx, gr, "unjudged", err.Error())
			v.Notices = append(v.Notices, i18n.T("guardrail.unjudged", "name", gr.Name, "error", err.Error()))
		case verdict.Pass:
			g.w.auditGuardrail(ctx, gr, "pass", verdict.Reason)
		case gr.Action == gates.Block:
			g.w.auditGuardrail(ctx, gr, "block", verdict.Reason)
			cur = i18n.T("guardrail.withheld_text", "name", gr.Name)
			v.Notices = append(v.Notices, i18n.T("guardrail.withheld_answer_why", "name", gr.Name, "reason", verdict.Reason))
		default:
			g.w.auditGuardrail(ctx, gr, "warn", verdict.Reason)
			v.Notices = append(v.Notices, i18n.T("guardrail.warned", "name", gr.Name, "reason", verdict.Reason))
		}
	}
	if cur != text {
		v.Text = &cur
	}
	return v
}

// inputGuardrails judges the prompt against the input guardrails, in
// order: one that fails, or can't be judged, and blocks refuses the
// prompt; one that warns says so.
func (w *Workspace) inputGuardrails(ctx context.Context, sessionID, text string, notice func(string, bool)) error {
	for _, gr := range gates.Active(w.guardsFor(sessionID).all, gates.StageInput, w.engine.ActiveAgent()) {
		verdict, err := w.judgeGuardrail(ctx, gr, text)
		switch {
		case err != nil && gr.Action == gates.Block:
			w.auditGuardrail(ctx, gr, "unjudged-block", err.Error())
			return &api.BlockedError{Reason: i18n.T("guardrail.unjudged_refused", "name", gr.Name, "error", err.Error())}
		case err != nil:
			w.auditGuardrail(ctx, gr, "unjudged", err.Error())
			notice(i18n.T("guardrail.unjudged", "name", gr.Name, "error", err.Error()), true)
		case verdict.Pass:
			w.auditGuardrail(ctx, gr, "pass", verdict.Reason)
		case gr.Action == gates.Block:
			w.auditGuardrail(ctx, gr, "block", verdict.Reason)
			return &api.BlockedError{Reason: i18n.T("guardrail.prompt_blocked", "name", gr.Name, "reason", verdict.Reason)}
		default:
			w.auditGuardrail(ctx, gr, "warn", verdict.Reason)
			notice(i18n.T("guardrail.warned", "name", gr.Name, "reason", verdict.Reason), false)
		}
	}
	return nil
}

// GuardrailSpec is what Studio's guardrail form saves.
type GuardrailSpec = gates.GuardrailSpec

// SaveGuardrail writes a guardrail in .agents/guardrails (a built-in's
// file holds its settings, or switches it off), returning it as read back,
// or the problems.
func (w *Workspace) SaveGuardrail(spec GuardrailSpec, previous string) (gates.Guardrail, []string, error) {
	path, probs, err := gates.SaveGuardrail(w.ProjectGuardrailsDir(), spec, previous)
	if err != nil || len(probs) > 0 {
		return gates.Guardrail{}, probs, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return gates.Guardrail{}, nil, err
	}
	return gates.ParseGuardrail(strings.TrimSuffix(filepath.Base(path), ".md"), path, data), nil, nil
}

// DeleteGuardrail removes a guardrail from .agents/guardrails (a
// built-in's: back to its defaults).
func (w *Workspace) DeleteGuardrail(name string) error {
	return gates.DeleteGuardrail(w.ProjectGuardrailsDir(), name)
}
