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
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// Guardrails (spec_041_gates GT-20 on): safety and limits at every step of
// a turn, read from .agents/guardrails (then the global folder) as the
// turn starts. Unlike rules, a guardrail that can't be checked refuses
// (block) rather than lets things through.

// guardSubjectLimit bounds what a custom guardrail's judge is shown at
// once: longer content is judged in parts of it (overlapping by
// guardPartOverlap, so a sentence cut between two is whole in one), up to
// guardMaxParts; content longer than that can't be judged (a guardrail
// that blocks refuses it). The injection patterns read all of a tool's
// result.
const (
	guardSubjectLimit = 12 << 10
	guardPartOverlap  = 1 << 10
	guardMaxParts     = 8
)

// writeTools change files: the limits guardrail counts them.
var writeTools = []string{"create_file", "replace_in_file", "delete_file", "notebook_edit"}

// ProjectGuardrailsDir is where Studio writes the workspace's guardrails:
// .agents/guardrails.
func (w *Workspace) ProjectGuardrailsDir() string {
	return filepath.Join(w.Dir(), ".agents", "guardrails")
}

// guardrailDirs are the folders guardrails are read from: the
// organization's locked ones, the workspace's, the user's
// (~/.blitz/guardrails), then the organization's defaults; a name in an
// earlier folder wins, so a locked guardrail can't be replaced or switched
// off by a file of its name (spec_053_enterprise EN-31).
func (w *Workspace) guardrailDirs() []string { return GuardrailDirs(w.cfg, w.Dir()) }

// GuardrailDirs are the folders a workspace at dir reads its guardrails
// from, with cfg's organization (Workspace.Guardrails; blitz doctor checks
// the same files).
func GuardrailDirs(cfg *config.Config, dir string) []string {
	return cfg.Org.LockedFirst([]string{filepath.Join(dir, ".agents", "guardrails"), filepath.Join(config.ConfigDir(""), "guardrails")}, "guardrails")
}

// Guardrails are the workspace's guardrails, read afresh: the built-in
// ones (with their defaults when they have no file) but those the
// organization's policy switches off (spec_053 EN-31), then custom ones.
func (w *Workspace) Guardrails() []gates.Guardrail {
	all := gates.LoadGuardrails(w.guardrailDirs()...)
	if off := w.cfg.Org.Policy.Builtins.Off; len(off) > 0 {
		all = slices.DeleteFunc(all, func(g gates.Guardrail) bool { return g.For() != "" && slices.Contains(off, g.For()) })
	}
	return all
}

// guardState is what the guardrails hold during a turn: those read as it
// started, and the notices already given (once per turn and guardrail).
type guardState struct {
	all  []gates.Guardrail
	said sync.Map
	// turns is how many turns in the session hold it (a workflow's
	// parallel steps share one); guarding.mu guards it.
	turns int
}

// guarding holds the current turns' guardrails, by session.
type guarding struct {
	mu    sync.Mutex
	turns sync.Map // session ID → *guardState
}

// begin is the guardrails of a turn starting in the session: those of a
// turn already running there (a workflow's parallel steps: one set, its
// notices once), else read reads them.
func (g *guarding) begin(sessionID string, read func() []gates.Guardrail) *guardState {
	g.mu.Lock()
	defer g.mu.Unlock()
	if v, ok := g.turns.Load(sessionID); ok {
		st := v.(*guardState)
		st.turns++
		return st
	}
	st := &guardState{all: read(), turns: 1}
	g.turns.Store(sessionID, st)
	return st
}

// end lets go of the session's guardrails once its last turn ends.
func (g *guarding) end(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if v, ok := g.turns.Load(sessionID); ok {
		if st := v.(*guardState); st.turns > 1 {
			st.turns--
			return
		}
		g.turns.Delete(sessionID)
	}
}

// turnLimits are the limits guardrails that set a turn's model-call and
// cost limits (nil: the turn's own limit, or none), and the limits file
// with a problem whose defaults run in its place (nil: none).
type turnLimits struct{ turns, cost, broken *gates.Guardrail }

// startGuardrails reads the guardrails for a turn in the session, and
// applies the limits guardrail's turn and cost limits to t, as they
// apply to agent, the turn's ("": the active agent).
func (w *Workspace) startGuardrails(sessionID string, t *api.Turn, agent string) turnLimits {
	st := w.guards.begin(sessionID, w.Guardrails)
	var lim turnLimits
	for _, g := range st.all {
		if g.Problem != "" && g.For() == gates.BuiltinLimits {
			lim.broken = &g
			break
		}
	}
	for _, g := range gates.Active(st.all, gates.StageAction, cmp.Or(agent, w.engine.ActiveAgent())) {
		if g.Builtin != gates.BuiltinLimits {
			continue
		}
		if l := g.Limits.MaxTurns; l > 0 && (t.MaxTurns == 0 || l < t.MaxTurns) {
			t.MaxTurns, lim.turns = l, &g
		}
		if l := g.Limits.MaxCostUSD; l > 0 && (t.MaxCostUSD == 0 || l < t.MaxCostUSD) {
			t.MaxCostUSD, lim.cost = l, &g
		}
	}
	return lim
}

// limitHints say what set a turn's own limits when no guardrail did (a
// job's file, the jobs' defaults or cap, an eval's fixed limit, [harness]
// max_cost_usd), and how to change them; "" says nothing.
type limitHints struct{ turns, cost string }

// limitSource adds to a turn stopped at a limit what set it and how to
// change it: the guardrail that set it (or that its file has a problem,
// so its defaults run), else the turn's own hint; and, where someone can
// answer (not a job's run, an eval, a skill's scenario or the
// reflection), that "continue" carries on: a long coding turn reaches 200
// model calls easily, and "maximum turns reached" alone sends people
// looking for a setting.
func (w *Workspace) limitSource(err error, lim turnLimits, attended bool, own limitHints) error {
	var g *gates.Guardrail
	var key, hint string
	switch {
	case errors.Is(err, api.ErrMaxTurns):
		g, key, hint = lim.turns, "max_turns", own.turns
	case errors.Is(err, api.ErrCostLimit):
		g, key, hint = lim.cost, "max_cost_usd", own.cost
	default:
		return err
	}
	rel := w.workspaceRel
	switch {
	case g == nil:
	case g.Path == "" && lim.broken != nil:
		hint = i18n.T("guardrail.limit_broken", "name", g.Name, "key", key, "path", rel(lim.broken.Path),
			"problem", strings.Join(strings.Fields(lim.broken.Problem), " "))
	case g.Path == "":
		hint = i18n.T("guardrail.limit_default", "name", g.Name, "key", key,
			"path", filepath.Join(".agents", "guardrails", gates.BuiltinLimits+".md"))
	default:
		hint = i18n.T("guardrail.limit_file", "name", g.Name, "key", key, "path", rel(g.Path))
	}
	if hint == "" {
		return err
	}
	if attended {
		hint += " " + i18n.T("guardrail.limit_continue")
	}
	return fmt.Errorf("%w: %s", err, hint)
}

// guardrailProblems are the notices for the session's turn's custom
// guardrails whose files have a problem: they check nothing, so the turn
// says so rather than run as if they did (a built-in's runs with its
// defaults).
func (w *Workspace) guardrailProblems(sessionID string) []api.Notice {
	var out []api.Notice
	for _, g := range w.guardsFor(sessionID).all {
		if g.Problem != "" && g.For() == "" {
			out = append(out, api.Notice{Error: true, Text: i18n.T("guardrail.not_running", "name", g.Name, "path", w.workspaceRel(g.Path),
				"problem", strings.Join(strings.Fields(g.Problem), " "))})
		}
	}
	return out
}

// workspaceRel is path relative to the workspace when it's in it.
func (w *Workspace) workspaceRel(path string) string {
	if r, err := filepath.Rel(w.Dir(), path); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return path
}

// limitSetting says which setting of the limits guardrail gr refused a
// call, and where it's set (or would be, for its defaults).
func (w *Workspace) limitSetting(gr gates.Guardrail, key string) string {
	if gr.Path == "" {
		return i18n.T("guardrail.limit_setting_default", "key", key, "path", filepath.Join(".agents", "guardrails", gates.BuiltinLimits+".md"))
	}
	return i18n.T("guardrail.limit_setting", "key", key, "path", w.workspaceRel(gr.Path))
}

// jobLimitHints say where a job's run's limits came from: the job's
// WORKER.md, the jobs' defaults or their cap (Settings › Library › Jobs).
func (w *Workspace) jobLimitHints(wk *workers.Worker, p config.WorkerPolicy) limitHints {
	path := w.workspaceRel(wk.Path)
	hint := func(key string, own, def, cap float64) string {
		set := own
		if set == 0 {
			set = def
		}
		switch {
		case cap > 0 && set > cap:
			return i18n.T("guardrail.limit_job_cap", "key", key)
		case own == 0:
			return i18n.T("guardrail.limit_job_default", "key", key, "path", path)
		}
		return i18n.T("guardrail.limit_job", "key", key, "path", path)
	}
	return limitHints{
		turns: hint("max_turns", float64(wk.Limits.MaxTurns), float64(p.DefaultMaxTurns), float64(p.MaxTurns)),
		cost:  hint("max_cost_usd", wk.Limits.MaxCostUSD, p.DefaultMaxCostUSD, p.MaxCostUSD),
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
// redacted first: all of it, in parts when it's long (the first part that
// fails is the verdict). Past guardMaxParts it can't be judged: an error,
// which a guardrail that blocks treats as a refusal.
func (w *Workspace) judgeGuardrail(ctx context.Context, g gates.Guardrail, subject string) (gates.Verdict, error) {
	parts := guardParts(w.secretRedactor().String(subject))
	if len(parts) > guardMaxParts {
		return gates.Verdict{}, fmt.Errorf("%d KiB is too long to judge (the most is about %d KiB)", len(subject)>>10, guardMaxParts*(guardSubjectLimit-guardPartOverlap)>>10)
	}
	var v gates.Verdict
	for i, part := range parts {
		if len(parts) > 1 {
			part = fmt.Sprintf("(part %d of %d)\n%s", i+1, len(parts), part)
		}
		var err error
		if v, err = gates.JudgeGuardrail(ctx, w.ruleAsker(), g, part); err != nil || !v.Pass {
			return v, err
		}
	}
	return v, nil
}

// guardParts splits s into parts of at most guardSubjectLimit bytes (on
// rune boundaries), each starting guardPartOverlap before the last ended.
func guardParts(s string) []string {
	var parts []string
	for {
		part := textutil.TruncateUTF8(s, guardSubjectLimit)
		parts = append(parts, part)
		if len(part) == len(s) {
			return parts
		}
		next := len(part) - guardPartOverlap
		for next > 0 && !utf8.RuneStart(s[next]) {
			next--
		}
		s = s[max(next, 1):]
	}
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
// the browser, an MCP server, a workflow's call step.
func (g guard) external(tool string) bool {
	if slices.Contains([]string{"web_fetch", "web_search", "browser", "mcp_call", "read_mcp_resource", "api_call", "a2a_call"}, tool) {
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
	case c.Tool == "browser" && str("action") == "script" && len(l.AllowedDomains) > 0:
		// A script can send the page, or a request, anywhere.
		return i18n.T("guardrail.domain_script", "domains", strings.Join(l.AllowedDomains, ", ")) + " " + g.w.limitSetting(gr, "allowed_domains")
	case c.Tool == "web_fetch" || (c.Tool == "browser" && str("action") == "navigate"):
		if u := str("url"); !gates.DomainAllowed(u, l.AllowedDomains) {
			return i18n.T("guardrail.domain", "url", u, "domains", strings.Join(l.AllowedDomains, ", ")) + " " + g.w.limitSetting(gr, "allowed_domains")
		}
	case c.Tool == "run_shell_command":
		if glob := gates.CommandDenied(str("command"), gates.ExpandCommands(l.Commands)); glob != "" {
			return i18n.T("guardrail.command", "glob", glob) + " " + g.w.limitSetting(gr, "commands")
		}
	case slices.Contains(writeTools, c.Tool) && l.MaxFilesChanged > 0:
		changed := g.w.tools.Checkpoints().LatestTurnFiles(c.Session)
		target := str("path")
		if !filepath.IsAbs(target) {
			target = filepath.Join(g.w.Dir(), target)
		}
		if len(changed) >= l.MaxFilesChanged && !slices.Contains(changed, target) {
			return i18n.T("guardrail.files", "n", l.MaxFilesChanged) + " " + g.w.limitSetting(gr, "max_files_changed")
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
	// A click, a form or going back can take the browser off the allowed
	// domains: what it found there is withheld.
	if c.Tool == "browser" {
		page, _ := result["url"].(string)
		if gr, why := g.w.offDomains(st.all, c.Agent, page); why != "" {
			g.w.auditGuardrail(ctx, gr, "block", why)
			v.Result = map[string]any{"error": i18n.T("guardrail.refused", "name", gr.Name, "reason", why)}
			v.Notices = append(v.Notices, v.Result["error"].(string))
			return v
		}
	}
	active := gates.Active(st.all, gates.StageToolResult, c.Agent)
	if len(active) == 0 {
		return v
	}
	data, _ := json.Marshal(result)
	text := string(data)
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
				if verdict, err := gates.ConfirmInjection(ctx, g.w.ruleAsker(), around(text, phrase, guardSubjectLimit)); err == nil && verdict.Pass {
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

// OutputWindow is how much of a streaming answer the engine holds back
// before the word still arriving (runtime.HeldTextKey): the longest
// secret the workspace knows by value, so one with spaces in it is whole
// when it's checked (guardrails.Window when that's longer).
func (g guard) OutputWindow() int { return g.w.secretRedactor().Longest() }

// Output applies the output guardrails to a piece of the answer: the
// secrets guardrail masks (or withholds) on every piece; custom checks
// judge the whole answer. While a custom one that blocks is on, the
// pieces aren't streamed: the answer is shown once it's judged, so one
// it withholds is never seen.
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
				v.Withheld = true
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
			if gr.Action == gates.Block {
				cur = "" // held until the whole answer is judged
			}
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
	for _, gr := range gates.Active(w.guardsFor(sessionID).all, gates.StageInput, w.turnAgent(ctx)) {
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

// around is at most n bytes of s around the first phrase in it (the start
// of s when phrase isn't there), on rune boundaries: what the injection
// judge confirms a match from.
func around(s, phrase string, n int) string {
	i := strings.Index(s, phrase)
	if i < 0 || len(s) <= n {
		return textutil.TruncateUTF8(s, n)
	}
	start := max(0, min(i-(n-len(phrase))/2, len(s)-n))
	for start > 0 && !utf8.RuneStart(s[start]) {
		start--
	}
	return textutil.TruncateUTF8(s[start:], n)
}

// offDomains is the limits guardrail (of all, guarding agent) whose
// allowed_domains an http(s) URL is off, and why ("" when none).
func (w *Workspace) offDomains(all []gates.Guardrail, agent, raw string) (gates.Guardrail, string) {
	if u, err := url.Parse(raw); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return gates.Guardrail{}, ""
	}
	for _, gr := range gates.Active(all, gates.StageAction, agent) {
		if gr.Builtin == gates.BuiltinLimits && !gates.DomainAllowed(raw, gr.Limits.AllowedDomains) {
			return gr, i18n.T("guardrail.domain", "url", raw, "domains", strings.Join(gr.Limits.AllowedDomains, ", ")) + " " + w.limitSetting(gr, "allowed_domains")
		}
	}
	return gates.Guardrail{}, ""
}

// approveNetwork is a workflow call's request to u held to the limits
// guardrail's allowed_domains (as web_fetch is), then approved as a fetch
// of its host is (tools.ApproveNetwork).
func (w *Workspace) approveNetwork(ctx context.Context, tool, detail string, u *url.URL) error {
	if gr, why := w.offDomains(w.Guardrails(), w.engine.ActiveAgent(), u.String()); why != "" {
		w.auditGuardrail(ctx, gr, "block", why)
		return &api.BlockedError{Reason: i18n.T("guardrail.refused", "name", gr.Name, "reason", why)}
	}
	return w.tools.ApproveNetwork(ctx, tool, detail, u)
}
