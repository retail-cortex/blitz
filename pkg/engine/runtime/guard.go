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
	"maps"

	"github.com/retail-cortex/blitz/pkg/guardrails"
	"github.com/retail-cortex/blitz/pkg/redact"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
)

// Guard checks what a turn reads, says and does, at every step
// (guardrails, spec_041_gates): a tool call before it runs, a tool's
// result before the model reads it, and the model's answers.
type Guard interface {
	// ToolCall decides a call before it runs.
	ToolCall(ctx context.Context, c GuardCall) GuardVerdict
	// ToolResult checks a tool's result.
	ToolResult(ctx context.Context, c GuardCall, result map[string]any) GuardVerdict
	// Output checks a piece of the model's answer; final is the whole
	// answer, after the pieces streamed.
	Output(ctx context.Context, c GuardCall, text string, final bool) GuardVerdict
}

// GuardCall is what a guard is asked about: the session and agent, and
// for a tool call or result, the tool and its arguments.
type GuardCall struct {
	Session, Agent, Tool string
	Args                 map[string]any
}

// GuardVerdict is a guard's answer; the zero value lets things be.
type GuardVerdict struct {
	// Refusal stops a tool call: the model gets it as the call's error.
	Refusal string
	// Result replaces a tool's result (withheld, or with a warning).
	Result map[string]any
	// Text replaces a piece of the answer (masked, or withheld).
	Text *string
	// Notices are said in the conversation.
	Notices []string
	// Withheld ends a streamed answer: the rest of it isn't sent (Text is
	// the last of it).
	Withheld bool
}

// WithGuard has the engine ask g at every step.
func WithGuard(g Guard) Option { return func(e *Engine) { e.guard = g } }

// guardCall is the call a guard is asked about, from the tool context.
func (e *Engine) guardCall(ctx interface{ AgentName() string }, base context.Context, tool string, args map[string]any) GuardCall {
	return GuardCall{Session: e.sessionOf(base), Agent: agentNameOf(ctx), Tool: tool, Args: args}
}

// agentNameOf is the calling agent's name, "" when the context can't say.
func agentNameOf(ctx interface{ AgentName() string }) (name string) {
	defer func() {
		if recover() != nil {
			name = ""
		}
	}()
	return ctx.AgentName()
}

// tell says a guard's notices in the conversation.
func (e *Engine) tell(ctx context.Context, notices []string) {
	if notice := e.noticeTo(ctx); notice != nil {
		for _, n := range notices {
			notice(n)
		}
	}
}

// HeldTextKey is the CustomMetadata key of the end of a streamed answer
// that was held back (see guardOutput) and is sent with its final
// response: front ends showing the streamed pieces get it as the last
// piece, before the final text that repeats them.
const HeldTextKey = "blitz_held_text"

// guardOutput checks the model's answer, masking the secrets it echoes
// and letting the guard replace what it masks or withholds. A streamed
// answer is checked as it grows, not piece by piece: the word still
// arriving at the end, and a window before it, is held
// (guardrails.Hold) until it's settled or the answer ends, so a key
// split across pieces is checked whole. The final response, the whole
// answer, is checked whole, and carries the held end (HeldTextKey).
func (e *Engine) guardOutput(ctx agent.Context, resp *model.LLMResponse) {
	if resp == nil || e.guard == nil && e.secrets == nil {
		return
	}
	var c GuardCall
	if e.guard != nil && ctx != nil {
		c = e.guardCall(ctx, ctx, "", nil)
	}
	check := func(text string, final bool) (string, bool) {
		if e.secrets != nil {
			text = redact.Mask(text)
		}
		if e.guard == nil {
			return text, false
		}
		v := e.guard.Output(ctx, c, text, final)
		if ctx != nil {
			e.tell(ctx, v.Notices)
		}
		if v.Text != nil {
			text = *v.Text
		}
		return text, v.Withheld
	}
	hold := e.outputHold(ctx, resp.Partial, check)
	if !resp.Partial && hold != nil {
		if rest := hold.Flush(); rest != "" {
			resp.CustomMetadata = maps.Clone(resp.CustomMetadata) // a model may share one map across responses
			if resp.CustomMetadata == nil {
				resp.CustomMetadata = map[string]any{}
			}
			resp.CustomMetadata[HeldTextKey] = rest
		}
	}
	if resp.Content == nil {
		return
	}
	for _, p := range resp.Content.Parts {
		switch {
		case p == nil || p.Text == "":
		case p.Thought:
			if e.secrets != nil {
				p.Text = redact.Mask(p.Text)
			}
		case resp.Partial && hold != nil:
			p.Text = hold.Write(p.Text)
		default:
			p.Text, _ = check(p.Text, !resp.Partial)
		}
	}
}

// outputHold is the hold of the answer streaming from the agent of ctx
// (by invocation, branch and agent, so parallel agents' are apart), in
// the run's state: made by a partial response, ended by the final one,
// which gets it back to flush. Nil without a run (each piece is then
// checked alone).
func (e *Engine) outputHold(ctx agent.Context, partial bool, check func(string, bool) (string, bool)) *guardrails.Hold {
	if ctx == nil {
		return nil
	}
	st := stateFrom(ctx)
	if st == nil {
		return nil
	}
	key := ctx.InvocationID() + "\x00" + ctx.Branch() + "\x00" + ctx.AgentName()
	if !partial {
		h, _ := st.holds.LoadAndDelete(key)
		hold, _ := h.(*guardrails.Hold)
		return hold
	}
	window := 0
	if w, ok := e.guard.(interface{ OutputWindow() int }); ok {
		window = w.OutputWindow()
	}
	h, _ := st.holds.LoadOrStore(key, &guardrails.Hold{Check: func(text string) (string, bool) { return check(text, false) }, Window: window})
	return h.(*guardrails.Hold)
}
