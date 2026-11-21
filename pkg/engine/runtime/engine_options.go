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

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
)

// Option configures an Engine.
type Option func(*Engine)

// WithSessionService replaces the default in-memory session store (e.g. with
// a persistent one so conversations can be resumed).
func WithSessionService(s session.Service) Option { return func(e *Engine) { e.sessions = s } }

// WithStreaming makes Execute deliver partial text events as the model
// generates them (followed by the usual final events).
func WithStreaming(on bool) Option { return func(e *Engine) { e.streaming = on } }

// TurnStore remembers each session's latest turn so the next turn's span
// can link to it, across processes when the store is persistent.
// session.Storage implements it.
type TurnStore interface {
	LastTurn(sessionID string) (traceparent string, index int)
	SetLastTurn(sessionID, traceparent string, index int) error
}

// WithTurnStore chains turn spans per session through s.
func WithTurnStore(s TurnStore) Option { return func(e *Engine) { e.turns = s } }

// UsageStore keeps each session's usage between processes: the engine
// saves it after every model call and restores it the first time a
// session's usage is asked for. session.Storage implements it.
type UsageStore interface {
	Usage(sessionID string) (api.Usage, bool)
	SetUsage(sessionID string, u api.Usage) error
}

// LedgerStore is a UsageStore that also keeps each session's ledger: its
// calls one by one (spec_056_conversation_adapters CA-120).
type LedgerStore interface {
	AppendCall(sessionID string, c api.Call) error
	Calls(sessionID string) ([]api.Call, error)
}

// WithUsageStore keeps sessions' usage in s (and their ledgers, when s is
// a LedgerStore too).
func WithUsageStore(s UsageStore) Option { return func(e *Engine) { e.usageStore = s } }

// WithProjectTrusted says whether the workspace's project settings are
// trusted: until they are, the project's own agents may only tighten the
// permission mode (spec_032_background_agents BGA-50).
func WithProjectTrusted(trusted bool) Option { return func(e *Engine) { e.projectTrusted = trusted } }

// WithNotice sets where the engine reports events the user should know
// about, such as a fallback model taking over (default: nowhere).
func WithNotice(f func(string)) Option { return func(e *Engine) { e.notice = f } }

// WithTurnNotices sends the engine's notices during runs under ctx to f
// (the turn's own events, so the conversation shows them) rather than to
// the engine's WithNotice, which gets them outside a turn.
func WithTurnNotices(ctx context.Context, f func(string)) context.Context {
	return tools.WithNotices(context.WithValue(ctx, turnNoticeKey{}, f), f) // the tools' (an MCP server failing) too
}

// WithTurnStatus sends passing statuses raised during runs under ctx to f:
// a model request whose connection failed and is being retried. Unlike
// notices, they aren't kept in the conversation.
func WithTurnStatus(ctx context.Context, f func(string)) context.Context {
	return context.WithValue(ctx, turnStatusKey{}, f)
}

// statusTo is where a passing status raised under ctx goes, or nil.
func statusTo(ctx context.Context) func(string) {
	f, _ := ctx.Value(turnStatusKey{}).(func(string))
	return f
}

// noticeTo is where a notice raised under ctx goes: the turn's sink, else
// the engine's, else nowhere (nil).
func (e *Engine) noticeTo(ctx context.Context) func(string) {
	if f, _ := ctx.Value(turnNoticeKey{}).(func(string)); f != nil {
		return f
	}
	return e.notice
}

// WithAgentModel runs agent on llm instead of the engine's model (a pin
// from [agent_models] or the agent's own default_model).
func WithAgentModel(agent string, llm model.LLM) Option {
	return func(e *Engine) {
		if e.agentModels == nil {
			e.agentModels = map[string]model.LLM{}
		}
		e.agentModels[agent] = withImages(llm, e.media)
	}
}

// WithSecrets gives the engine {secrets:key} values (spec_039_secrets):
// expand puts them in text's place and returns those used. They're put in
// only as the model is called, never in the session, the transcript or
// the hooks' view.
func WithSecrets(expand func(text string) (string, []string)) Option {
	return func(e *Engine) { e.secrets = expand }
}

// WithInstructions appends text (e.g. project memory) to every agent's instructions.
func WithInstructions(text string) Option { return func(e *Engine) { e.extraInstructions = text } }

// WithPersonality gives the root agent of attended runs text after its
// instructions (spec_064_personality PER-02): how it sounds to the person.
func WithPersonality(text string) Option { return func(e *Engine) { e.personality = text } }

// ExecOption configures one Execute call.
type ExecOption func(*runState)

// WithBudget counts the run's model calls toward b (nil: none).
func WithBudget(b *Budget) ExecOption {
	return func(s *runState) {
		if b != nil {
			s.parent = b.st
		}
	}
}

// WithMaxTurns limits the number of model calls in the run (0 = unlimited).
func WithMaxTurns(n int) ExecOption { return func(s *runState) { s.maxTurns = n } }

// WithAgent runs the prompt with name as the root agent instead of the
// active one, for this run only (e.g. a worker's agent).
func WithAgent(name string) ExecOption { return func(s *runState) { s.agent = name } }

// WithModel runs the prompt on llm instead of the configured model, for
// this run only: the root agent runs on it even if pinned, and so does
// every unpinned agent (e.g. a worker's model).
func WithModel(llm model.LLM) ExecOption { return func(s *runState) { s.model = llm } }

// WithConversationModel runs the prompt on its conversation's own model
// (spec_056 CA-100): every agent without a pin runs on it, a pinned one on
// its pin. A run's own model (WithModel) wins over it.
func WithConversationModel(llm model.LLM) ExecOption {
	return func(s *runState) { s.conversationModel = llm }
}
