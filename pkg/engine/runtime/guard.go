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

	"google.golang.org/adk/v2/model"
)

// Guard checks what a turn reads, says and does, at every step
// (guardrails, spec_gates_041): a tool call before it runs, a tool's
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

// guardOutput lets the guard check each text part of the model's reply,
// replacing what it masks or withholds.
func (e *Engine) guardOutput(ctx interface {
	context.Context
	AgentName() string
}, resp *model.LLMResponse) {
	if e.guard == nil || resp == nil || resp.Content == nil {
		return
	}
	c := e.guardCall(ctx, ctx, "", nil)
	for _, p := range resp.Content.Parts {
		if p == nil || p.Text == "" || p.Thought {
			continue
		}
		v := e.guard.Output(ctx, c, p.Text, !resp.Partial)
		if v.Text != nil {
			p.Text = *v.Text
		}
		e.tell(ctx, v.Notices)
	}
}
