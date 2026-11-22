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

package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync/atomic"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/observability"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

// repeatWarnAt is how many times the same call may fail the same way in a
// run before the agent is told to stop repeating it.
const repeatWarnAt = 3

// repeatedFailure counts a failed call in ctx's run and returns how often
// this tool, with these arguments, failed with this error in it (0 outside
// a run).
func repeatedFailure(ctx context.Context, tool string, args map[string]any, why string) int {
	st := stateFrom(ctx)
	if st == nil {
		return 0
	}
	b, _ := json.Marshal(args) // map keys sorted: the same arguments, the same key
	n, _ := st.failures.LoadOrStore(tool+"\x00"+string(b)+"\x00"+why, new(atomic.Int32))
	return int(n.(*atomic.Int32).Add(1))
}

// beforeModel enforces the per-run model-call budget (a sub-agent's own
// and every calling run's), tells the read log which read_file results
// the request carries (FS-83), and puts {secrets:key} values into the
// request's messages.
func (e *Engine) beforeModel(ctx agent.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	for st := stateFrom(ctx); st != nil; st = st.parent {
		if st.maxTurns > 0 {
			if n := st.turns.Add(1); n > int64(st.maxTurns) {
				return nil, fmt.Errorf("%w (%d model calls)", api.ErrMaxTurns, st.maxTurns)
			}
		}
	}
	if req != nil {
		if e.toolReg != nil {
			e.toolReg.Workspace().Reads().Shown(ctx, req.Contents)
		}
		req.Contents = e.expandContents(req.Contents)
	}
	return nil, nil
}

// expandSecrets is text with {secrets:key} values put in, and those used
// remembered for the redactors.
func (e *Engine) expandSecrets(text string) string {
	if e.secrets == nil {
		return text
	}
	out, used := e.secrets(text)
	redact.Remember(used...)
	return out
}

// expandContents puts secrets into copies of the messages that name them:
// the session's own stay as they were, so nothing stores a value.
func (e *Engine) expandContents(contents []*genai.Content) []*genai.Content {
	if e.secrets == nil {
		return contents
	}
	var out []*genai.Content
	for i, c := range contents {
		if c == nil || !partsNameSecrets(c.Parts) {
			if out != nil {
				out = append(out, c)
			}
			continue
		}
		if out == nil {
			out = append(make([]*genai.Content, 0, len(contents)), contents[:i]...)
		}
		cp := &genai.Content{Role: c.Role, Parts: make([]*genai.Part, len(c.Parts))}
		for j, p := range c.Parts {
			if p == nil || !secrets.HasPlaceholders(p.Text) {
				cp.Parts[j] = p
				continue
			}
			q := *p
			q.Text = e.expandSecrets(p.Text)
			cp.Parts[j] = &q
		}
		out = append(out, cp)
	}
	if out == nil {
		return contents
	}
	return out
}

func partsNameSecrets(parts []*genai.Part) bool {
	for _, p := range parts {
		if p != nil && secrets.HasPlaceholders(p.Text) {
			return true
		}
	}
	return false
}

// afterModel records token usage for the run's session.
func (e *Engine) afterModel(ctx agent.Context, resp *model.LLMResponse, respErr error) (*model.LLMResponse, error) {
	e.guardOutput(ctx, resp) // secrets it echoes, and guardrails on the answer: secrets, personal data, custom checks
	if resp == nil || resp.Partial {
		return nil, nil
	}
	e.noteFallback(ctx, resp)
	e.noteUnshown(ctx, resp)
	if lines, _ := resp.CustomMetadata[LearnedKey].([]string); len(lines) > 0 {
		// What a refusal taught about the model, told once (spec_056 CA-13).
		if notice := e.noticeTo(ctx); notice != nil {
			for _, l := range lines {
				notice(l)
			}
		}
	}
	if resp.UsageMetadata == nil && resp.Content == nil {
		return nil, nil // not an answer (nothing to count)
	}
	// An answer without usage (a server that doesn't send it) is still a
	// call, of unknown cost: the session's usage stops claiming a price.
	id := "default"
	if st := stateFrom(ctx); st != nil {
		id = st.sessionID
	}
	// Price by the model that actually answered: with server-side fallback
	// it can differ from the configured one.
	served := resp.ModelVersion
	if o, ok := originOf(resp); ok && (served == "" || !e.usage.HasPrice(served)) {
		// The model that answered, priced or not: a fallback with no price
		// is unpriced, never charged at the primary's (spec_056 CA-92).
		served = o.Model
	}
	if served == "" || (!e.usage.HasPrice(served) && !isFallback(resp)) {
		name, _ := e.AgentModel(ctx.AgentName()) // the calling agent's model (it may be pinned)
		if st := stateFrom(ctx); st != nil && st.models[ctx.AgentName()] != "" {
			name = st.models[ctx.AgentName()] // a run with its own agent or model
		}
		served = name
	}
	var writes int64
	switch v := resp.CustomMetadata[CacheWriteTokensKey].(type) {
	case int64:
		writes = v
	case int:
		writes = int64(v)
	case float64:
		writes = int64(v)
	}
	o, _ := originOf(resp)
	c := Charge{Session: id, Purpose: api.PurposeTurn, Model: served, Origin: o, Usage: resp.UsageMetadata, CacheWrites: writes}
	st := stateFrom(ctx)
	if st != nil && st.taskID != "" {
		c.Task, c.Purpose = st.taskID, api.PurposeTask // its cost cap counts its own
	}
	if st == nil {
		e.restoreUsage(id)
		e.usage.Charge(ctx, c) // no session to keep it in
		return nil, nil
	}
	e.charge(ctx, c)
	return nil, nil
}

// fallbackState is a fallback model answering for primary.
type fallbackState struct{ served, primary string }

// noteFallback tells the user when a fallback model starts answering and
// when the primary is back, once per change in the session (a background
// task's apart) rather than on every call.
func (e *Engine) noteFallback(ctx context.Context, resp *model.LLMResponse) {
	primary, _ := resp.CustomMetadata[FallbackFromKey].(string)
	now := fallbackState{primary: primary}
	if primary != "" {
		now.served = resp.ModelVersion
	}
	key := ""
	if st := stateFrom(ctx); st != nil {
		key = st.sessionID + "\x00" + st.taskID
	}
	e.fallbackMu.Lock()
	prev := e.fallbackBy[key]
	if now.served == "" {
		delete(e.fallbackBy, key)
	} else {
		if e.fallbackBy == nil {
			e.fallbackBy = map[string]fallbackState{}
		}
		e.fallbackBy[key] = now
	}
	e.fallbackMu.Unlock()
	notice := e.noticeTo(ctx)
	if now.served == prev.served || notice == nil {
		return
	}
	msg := i18n.T("model.fallback_recovered", "model", prev.primary) // the model that recovered
	if now.served != "" {
		msg = i18n.T("model.fallback", "primary", primary, "model", now.served)
		if reason, _ := resp.CustomMetadata[FallbackReasonKey].(string); reason != "" {
			msg = i18n.T("model.fallback_error", "primary", primary, "model", now.served, "error", reason)
		}
	}
	slog.InfoContext(ctx, msg)
	notice(msg)
}

// beforeTool audits the call, gates MCP tools, and runs pre_tool hooks.
// Returning a result map skips the tool and hands that result to the model.
func (e *Engine) beforeTool(ctx agent.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
	log := e.toolReg.Hooks().Audit()
	name := t.Name()
	if c, ok := args["command"].(string); ok && name == "blitz" {
		name = "blitz:" + strings.Join(strings.Fields(strings.ToLower(c)), " ") // the command, in the audit (spec_060 DRV-73)
	}
	log.LogContext(ctx, audit.Entry{Kind: audit.KindToolCall, Tool: name, Args: args})

	if r := planRefusal(stateFrom(ctx), tools.PlanGateFrom(ctx).Planning(), t.Name(), args); r != nil {
		return r, nil
	}
	if r := allowedRefusal(stateFrom(ctx), t.Name()); r != nil {
		return r, nil
	}
	if rule := e.toolReg.Rules().ToolDenied(t.Name(), args); rule != nil {
		log.LogContext(ctx, audit.Entry{Kind: audit.KindDenial, Tool: t.Name(), Decision: "rule-deny " + rule.String()})
		return map[string]any{"error": fmt.Sprintf("denied by the permission rule deny %s", rule)}, nil
	}
	if r := e.preToolHooks(ctx, t, args); r != nil {
		return r, nil
	}
	if e.guard != nil { // guardrails on the action: limits, custom checks
		v := e.guard.ToolCall(ctx, e.guardCall(ctx, ctx, t.Name(), args))
		e.tell(ctx, v.Notices)
		if v.Refusal != "" {
			log.LogContext(ctx, audit.Entry{Kind: audit.KindDenial, Tool: t.Name(), Decision: "guardrail", Detail: v.Refusal})
			return map[string]any{"error": v.Refusal}, nil
		}
	}
	if err := e.toolReg.ApproveMCP(ctx, t.Name(), args); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return nil, nil
}

// preToolHooks runs pre_tool hooks (spec_027_parity PAR-HK-10): a block or
// deny stops the call; updated_args replace its arguments (checked against
// the deny rules again; the tool still checks paths and the sandbox);
// allow lets the call through its approvals, and ask puts it to the user
// first; additional_context goes to the agent with the result.
func (e *Engine) preToolHooks(ctx agent.Context, t tool.Tool, args map[string]any) map[string]any {
	scripts := e.toolReg.ScriptHooks()
	if !scripts.Has("pre_tool") {
		return nil
	}
	out := scripts.PreTool(ctx, e.sessionOf(ctx), t.Name(), args)
	if out.Blocked {
		return map[string]any{"error": "blocked by pre_tool hook: " + out.Reason}
	}
	if out.UpdatedArgs != nil {
		clear(args) // the same map the tool gets
		maps.Copy(args, out.UpdatedArgs)
		if rule := e.toolReg.Rules().ToolDenied(t.Name(), args); rule != nil {
			return map[string]any{"error": fmt.Sprintf("denied by the permission rule deny %s", rule)}
		}
	}
	hooks := e.toolReg.Hooks()
	id := tools.CallID(ctx)
	switch out.Decision {
	case "ask":
		why := out.Reason
		if why == "" {
			why = "a pre_tool hook asks"
		}
		if err := hooks.Approve(ctx, api.ApprovalRequest{
			Tool: t.Name(), Kind: api.ActionCommand, Detail: fmt.Sprintf("%s %s (%s)", t.Name(), argsSummary(args), why), MustAsk: true,
		}); err != nil {
			return map[string]any{"error": err.Error()}
		}
		hooks.GrantCall(ctx)
	case "allow":
		hooks.GrantCall(ctx)
	}
	if out.Context != "" && id != "" {
		e.hookContext.Store(id, out.Context)
	}
	return nil
}

// argsSummary is a call's arguments in brief, for a question.
func argsSummary(args map[string]any) string {
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return textutil.Ellipsize(string(b), 300)
}

// exitCode is a command's exit code in a tool's result (run_shell_command,
// run_skill_script); 0 when there's none.
func exitCode(result map[string]any) int {
	switch c := result["exit_code"].(type) {
	case int:
		return c
	case int64:
		return int(c)
	case float64:
		return int(c)
	}
	return 0
}

// afterTool runs post_tool hooks, audits the outcome, and attaches messages
// the user sent while the turn was running (see Steer).
func (e *Engine) afterTool(ctx agent.Context, t tool.Tool, args, result map[string]any, toolErr error) (map[string]any, error) {
	id := tools.CallID(ctx)
	e.toolReg.Hooks().EndCall(ctx)
	e.toolReg.ScriptHooks().PostTool(ctx, e.sessionOf(ctx), t.Name(), args, result, toolErr)
	entry := audit.Entry{Kind: audit.KindToolResult, Tool: t.Name(), Decision: "ok"}
	if toolErr != nil {
		entry.Decision, entry.Error = "error", toolErr.Error()
	} else if msg, _ := result["error"].(string); msg != "" {
		entry.Decision, entry.Error = "error", msg
	}
	observability.RecordToolCall(ctx, t.Name(), entry.Decision)
	// What failed, for what agents learn from their runs
	// (spec_048_harness_learning HE-02): the call's arguments, and for a
	// command that exited nonzero its code and the end of its output.
	if code := exitCode(result); entry.Decision == "ok" && code != 0 {
		out, _ := result["output"].(string)
		entry.Decision, entry.Error, entry.Detail = "exit", fmt.Sprintf("exit code %d", code), textutil.Tail(strings.TrimSpace(out), 300)
	}
	if entry.Decision != "ok" {
		entry.Args = args
	}
	e.toolReg.Hooks().Audit().LogContext(ctx, entry)
	out := result
	changed := false
	if e.guard != nil && toolErr == nil { // guardrails on what the tool brought back: injection, custom checks
		v := e.guard.ToolResult(ctx, e.guardCall(ctx, ctx, t.Name(), args), out)
		e.tell(ctx, v.Notices)
		if v.Result != nil {
			out, changed = v.Result, true
		}
	}
	if r := e.attachRules(ctx, t.Name(), args, out, toolErr); r != nil {
		out, changed = r, true
	}
	if r := e.attachSteers(ctx, out, toolErr); r != nil {
		out, changed = r, true
	}
	if d := e.toolReg.EditDiagnostics(ctx, t.Name(), args, out); len(d) > 0 {
		out = maps.Clone(out)
		if out == nil {
			out = map[string]any{}
		}
		out["diagnostics"] = d
		changed = true
	}
	// The same call failing the same way again (spec_048_harness_learning
	// HE-07): say so, so the agent changes course instead of retrying.
	if entry.Decision != "ok" {
		if n := repeatedFailure(ctx, t.Name(), args, entry.Error); n >= repeatWarnAt {
			out = maps.Clone(out)
			if out == nil {
				out = map[string]any{}
			}
			out["repeated_failure"] = fmt.Sprintf("This exact call has now failed %d times in this turn with the same error. Don't make it again: read the error, then change the call, try another way, or ask the user.", n)
			changed = true
		}
	}
	if text, ok := e.hookContext.LoadAndDelete(id); ok && id != "" {
		out = maps.Clone(out)
		if out == nil {
			out = map[string]any{}
		}
		out["hook_context"] = text
		changed = true
	}
	if changed {
		if toolErr != nil { // returning a result replaces the error; keep it visible
			out["error"] = toolErr.Error()
		}
		return out, nil
	}
	return nil, nil
}

func (e *Engine) sessionOf(ctx context.Context) string {
	if st := stateFrom(ctx); st != nil {
		return st.sessionID
	}
	return ""
}
