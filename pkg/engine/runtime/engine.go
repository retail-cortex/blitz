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

// Package runtime runs the agent: it wraps Google's ADK runner, builds the
// agent tree with its tools and sub-agents, runs a prompt as a turn within
// its limits, delivers steering mid-turn, answers side questions without
// touching history, compacts history, and records usage and traces. It
// also makes the models (Gemini, Anthropic, OpenAI, Ollama) and falls back
// between them (spec_016_engine, spec_015_models).
package runtime

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/modelfacts"
	"github.com/retail-cortex/blitz/pkg/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/platform"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/compaction"
	"google.golang.org/genai"
)

const appName = "blitz"

// MaxSubagentDepth bounds nested invoke_agent calls so agents cannot recurse forever.
const MaxSubagentDepth = 3

var (
	// ErrSubagentDepth is returned when invoke_agent nesting exceeds MaxSubagentDepth.
	ErrSubagentDepth = errors.New("maximum sub-agent nesting depth exceeded")
)

type (
	subagentDepthKey struct{}
	runStateKey      struct{}
	turnNoticeKey    struct{}
	turnStatusKey    struct{}
)

// runState is carried in the context of one Execute call.
type runState struct {
	// conversationModel is the conversation's own model, if it has one
	// (WithConversationModel).
	conversationModel model.LLM
	sessionID         string
	maxTurns          int
	turns             atomic.Int64
	attachments       []*genai.Part
	planOnly          bool   // refuse tools that could change anything (see WithPlanOnly)
	mode              string // names a read-only mode other than plan in refusals (see WithReadOnly)
	// allowed limits the run to these tools (see WithAllowedTools); nil
	// means no limit.
	allowed map[string]bool
	// agent and model replace the active agent and the configured model for
	// this run only (see WithAgent, WithModel).
	agent string
	model model.LLM
	// models names the model each agent of an overridden tree runs on, for
	// pricing; nil when the run uses the engine's own tree.
	models map[string]string
	// taskID is the background task this run is ("" for a turn): its
	// usage is counted for the task too.
	taskID string
	// failures counts the run's failed calls by tool, arguments and error
	// (*atomic.Int32), to tell the agent when it repeats one.
	failures sync.Map
	// holds are the answers streaming in the run, held back while their
	// end may be a secret still arriving (see guardOutput).
	holds sync.Map
	// parent is the run a sub-agent with its own budget was called from,
	// or a budget several runs share (see Budget): its model calls count
	// toward each run up the chain too.
	parent *runState
}

// child is the state of a run started from st (nil: none): the same
// session, the same restrictions (plan-only or read-only, allowed tools)
// and overrides (agent, model, task), with its own model-call budget.
// The caller sets parent when the calls should still count toward st's.
func (st *runState) child(maxTurns int) *runState {
	c := &runState{maxTurns: maxTurns}
	if st != nil {
		c.sessionID, c.planOnly, c.mode, c.allowed = st.sessionID, st.planOnly, st.mode, st.allowed
		c.agent, c.model, c.models, c.taskID = st.agent, st.model, st.models, st.taskID
	}
	return c
}

// Budget is a model-call budget several Execute calls share: the runs of
// one turn (its first, an approved plan's, a stop hook's, a goal's or a
// rule's continuation), or a job's workflow steps.
type Budget struct{ st *runState }

// NewBudget is a budget of max model calls (0: no limit of its own) whose
// calls count toward within too (nil: none).
func NewBudget(max int, within *Budget) *Budget {
	b := &Budget{st: &runState{maxTurns: max}}
	if within != nil {
		b.st.parent = within.st
	}
	return b
}

// Exhausted reports whether a run was stopped at this budget's own limit
// (not one it counts toward).
func (b *Budget) Exhausted() bool {
	return b != nil && b.st.maxTurns > 0 && b.st.turns.Load() > int64(b.st.maxTurns)
}

func stateFrom(ctx context.Context) *runState {
	s, _ := ctx.Value(runStateKey{}).(*runState)
	return s
}

// Engine orchestrates the Google ADK execution lifecycle for Blitz.
// It is safe for concurrent use.
type Engine struct {
	// guard checks each step (guardrails; nil: none).
	guard Guard
	// secrets expands {secrets:key} as the model is called (nil: none).
	secrets func(text string) (string, []string)
	// hookContext is pre_tool hooks' additional_context, by tool call ID,
	// until the result goes back.
	hookContext sync.Map
	// hookModels judge prompt hooks naming a model, by its reference.
	hookModels map[string]model.LLM

	cfg   *config.Config
	tasks *taskManager
	// compactAt is the active model's compaction point (spec_056 CA-110),
	// set as the agents are built (under mu).
	compactAt int
	// projectTrusted means the project's agents may loosen the mode.
	projectTrusted bool
	// reviewModel reviews actions in the auto mode (nil: e.llm).
	reviewModel model.LLM
	agentReg    *agents.Registry
	skillProv   *skills.Provider
	toolReg     *tools.Registry
	usage       *UsageTracker
	media       *mediaSource // stored attachments, and the uploader

	// Shared across runner rebuilds so switching agent or model keeps history.
	sessions  session.Service
	artifacts artifact.Service
	memories  memory.Service

	subagentSeq atomic.Int64
	turns       TurnStore  // nil: turns are not chained
	usageStore  UsageStore // nil: usage lasts as long as the process

	steerMu sync.Mutex
	steers  map[string][]string // session ID -> messages sent mid-turn
	// steering are the sessions whose turn takes steer messages: from its
	// start until it collects the unread ones (CloseSteers).
	steering map[string]bool

	notice     func(string)
	settingsMu sync.RWMutex
	settings   map[string]config.ModelSettings // model name -> [model_settings]
	effort     string                          // session-wide reasoning effort (/effort)
	agency     string                          // the agency level the root agent's prompt names (/set agency)

	scoped scopedRules // path-scoped project rules

	fallbackMu sync.Mutex
	// fallbackBy is, by run (session and task), the fallback answering
	// now and the primary it stands in for; absent while the primary is.
	fallbackBy map[string]fallbackState

	mu                sync.RWMutex
	llm               model.LLM
	agentModels       map[string]model.LLM // pinned agents; others use llm
	runner            *runner.Runner
	rootAgent         agent.Agent        // the runner's agent tree, for Aside
	compactionCfg     *compaction.Config // the runner's, for Aside
	active            string
	extraInstructions string
	// personality is the root agent's in attended runs, after every other
	// instruction ("": none).
	personality string
	streaming   bool
}

// EventHandler receives events (text tokens, function calls, function responses) from the runner.
type EventHandler func(ev *session.Event) error

// NewEngine creates and wires the ADK runtime engine, registering itself as
// the sub-agent invoker for the invoke_agent tool.
func NewEngine(
	ctx context.Context,
	cfg *config.Config,
	agentReg *agents.Registry,
	skillProv *skills.Provider,
	toolReg *tools.Registry,
	llm model.LLM,
	opts ...Option,
) (*Engine, error) {
	// The models' records read the settings' prices and what was learned
	// of them, kept in Blitz's home (spec_056 CA-11, CA-14).
	setPricing(cfg.Pricing)
	if learnedStore() == nil {
		SetLearnedStore(modelfacts.Open(modelfacts.Path(config.Dir())))
	}
	e := &Engine{
		agency:    cfg.Blitz.AgencyLevel,
		cfg:       cfg,
		agentReg:  agentReg,
		skillProv: skillProv,
		toolReg:   toolReg,
		usage:     NewUsageTracker(cfg.Pricing),
		sessions:  session.InMemoryService(),
		artifacts: artifact.InMemoryService(),
		memories:  memory.InMemoryService(),
		media:     &mediaSource{store: toolReg.Images()},
		active:    cfg.Blitz.DefaultAgent,
		settings:  map[string]config.ModelSettings{},
		tasks:     newTaskManager(),
	}
	if cfg.Enterprise.URL != "" {
		e.usage.SetCatalogPrices(catalogPrices(cfg))
	}
	// Sorted, so that when "gpt-5" and "openai/gpt-5" are both written the
	// bare name wins every time.
	for _, key := range slices.Sorted(maps.Keys(cfg.ModelSettings)) {
		name := settingsName(key)
		if _, bare := cfg.ModelSettings[name]; bare && key != name {
			continue
		}
		if s := cfg.ModelSettings[key]; !s.IsZero() {
			e.settings[name] = s
		}
	}
	for _, o := range opts {
		o(e)
	}

	e.llm = withImages(llm, e.media)
	e.mu.Lock()
	err := e.rebuildLocked()
	e.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ADK runner: %w", err)
	}

	toolReg.Hooks().SetSubagentInvoker(e.InvokeSubagent)
	toolReg.Hooks().SetTaskRunner(e)
	toolReg.Hooks().SetReviewer(e.review)
	if s := toolReg.ScriptHooks(); s != nil { // http and prompt hooks, and their messages
		s.Judge = e.judgeHook
		s.Notify = func(ctx context.Context, msg string) {
			if n := e.noticeTo(ctx); n != nil {
				n(msg)
			}
		}
	}
	return e, nil
}

// Execute runs a prompt within a session and streams ADK events to the handler.
func (e *Engine) Execute(ctx context.Context, sessionID, prompt string, handler EventHandler, opts ...ExecOption) error {
	if sessionID == "" {
		sessionID = "default"
	}
	st := &runState{sessionID: sessionID}
	for _, o := range opts {
		o(st)
	}
	r, agentName, modelName, err := e.runnerFor(st, tools.Attended(ctx))
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, runStateKey{}, st)
	ctx = audit.WithSession(ctx, sessionID)
	// MCP servers without an agents list are the turn's own agent's.
	ctx = tools.WithPrimaryAgent(ctx, agentName)
	ctx = withSettingsLookup(ctx, e.lookupSettings)
	ctx = WithProviderToolPolicy(ctx, e.providerToolPolicy())
	if n := e.cfg.Tools.MaxParallel; n > 0 {
		ctx = platform.WithTaskRunner(ctx, boundedRunner(n))
	}
	rc := agent.RunConfig{}
	if e.streaming {
		rc.StreamingMode = agent.StreamingModeSSE
	}

	ctx, span, index := e.startTurn(ctx, sessionID,
		attribute.String("agent", agentName),
		attribute.String("model", modelName),
		attribute.Int("prompt.chars", len(prompt)),
		attribute.Int("attachments", len(st.attachments)),
		attribute.Int("max_turns", st.maxTurns),
		attribute.Bool("plan_only", st.planOnly),
	)
	defer e.recordTurn(ctx, sessionID, span, index)
	if p, ok := e.sessions.(pinner); ok {
		defer p.Pin(sessionID)() // its events go to the session the runner loaded
	}
	before := e.Usage(sessionID)
	answered := false
	seen := func(ev *session.Event) error {
		if ev != nil && ev.Author != "user" && ev.Content != nil {
			answered = true
		}
		if handler == nil {
			return nil
		}
		return handler(ev)
	}
	err = drain(r.Run(ctx, "user", sessionID, userContent(prompt, st.attachments), rc), seen)
	if errors.Is(err, api.ErrContextLength) && !answered && ctx.Err() == nil {
		// Longer than the model reads: summarised to fit, without asking
		// (the owner's decision), and the same model tried once more
		// (spec_056 CA-111). The turn's prompt is in the session already.
		if _, cerr := e.Compact(ctx, sessionID, "", 1); cerr == nil {
			if notice := e.noticeTo(ctx); notice != nil {
				notice(i18n.T("model.context_summarised", "model", modelName))
			}
			err = drain(r.Run(withCompactedRetry(ctx), "user", sessionID, nil, rc), seen)
		}
	}
	after := e.usage.Session(sessionID)
	span.SetAttributes(
		attribute.Int64("model_calls", int64(after.Calls-before.Calls)),
		attribute.Int64("tokens.input", after.Input-before.Input),
		attribute.Int64("tokens.output", after.Output-before.Output),
	)
	if after.Priced {
		span.SetAttributes(attribute.Float64("cost_usd", after.CostUSD-before.CostUSD))
	}
	observability.End(span, err)
	if err != nil && ctx.Err() == nil {
		slog.ErrorContext(ctx, "turn failed", "session", sessionID, "error", err)
	}
	return err
}

// pinner is a session service that keeps only some sessions in memory
// (session.PersistentService): a session a turn runs in must stay.
type pinner interface {
	Pin(id string) (unpin func())
}

// runnerFor returns the runner for a run and the root agent and model it
// uses: the engine's own, or, when the run names an agent or a model, one
// built for it alone (st.models then names each agent's model for pricing).
// An unattended run (attended false) never gets the personality
// (spec_064_personality PER-02), so it gets a tree of its own when there's
// one.
func (e *Engine) runnerFor(st *runState, attended bool) (*runner.Runner, string, string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if st.agent == "" && st.model == nil && st.conversationModel == nil && (attended || e.personality == "") {
		if e.runner == nil {
			return nil, "", "", fmt.Errorf("runner is not initialized")
		}
		return e.runner, e.active, e.modelForLocked(e.active).Name(), nil
	}
	active := cmp.Or(st.agent, e.active)
	t, err := e.buildTreeLocked(active, e.runModelOf(st), attended)
	if err != nil {
		return nil, "", "", err
	}
	st.models = make(map[string]string, len(t.models))
	for name, m := range t.models {
		st.models[name] = m.Name()
	}
	return t.runner, active, t.models[active].Name(), nil
}

// boundedRunner runs the tool calls of one model response with at most
// limit in flight. The limit is per batch, not global: a tool that runs a
// sub-agent holds a slot while the sub-agent's own batch runs, and a shared
// pool could deadlock with every slot held by a waiting parent. Every task
// runs exactly once, as the ADK requires; after cancellation queued tasks
// still start and fail fast on their cancelled context.
func boundedRunner(limit int) platform.TaskRunner {
	return func(ctx context.Context, tasks []func(context.Context)) {
		sem := make(chan struct{}, limit)
		var wg sync.WaitGroup
		for _, task := range tasks {
			sem <- struct{}{}
			wg.Go(func() {
				defer func() { <-sem }()
				task(ctx)
			})
		}
		wg.Wait()
	}
}

// startTurn starts the span for one turn. Each turn is its own trace (a
// session can last days and resume in another process, so it can't be one
// long-lived span); turns of a session share gen_ai.conversation.id and each
// links to the previous turn, so a session reads as a chain.
func (e *Engine) startTurn(ctx context.Context, sessionID string, attrs ...attribute.KeyValue) (context.Context, trace.Span, int) {
	index := 1
	opts := []trace.SpanStartOption{trace.WithNewRoot()}
	if e.turns != nil {
		tp, last := e.turns.LastTurn(sessionID)
		index = last + 1
		if prev, ok := observability.ParseTraceparent(tp); ok {
			opts = append(opts, trace.WithLinks(trace.Link{
				SpanContext: prev,
				Attributes:  []attribute.KeyValue{attribute.String("link.type", "previous_turn"), attribute.Int("turn.index", last)},
			}))
		}
	}
	attrs = append(attrs, observability.ConversationID.String(sessionID), attribute.Int("turn.index", index))
	opts = append(opts, trace.WithAttributes(attrs...))
	ctx, span := observability.StartWith(ctx, "turn", opts...)
	return ctx, span, index
}

// recordTurn saves the turn as the session's latest. With telemetry off the
// span has no trace context and nothing is written.
func (e *Engine) recordTurn(ctx context.Context, sessionID string, span trace.Span, index int) {
	if e.turns == nil {
		return
	}
	if tp := observability.Traceparent(span); tp != "" {
		if err := e.turns.SetLastTurn(sessionID, tp, index); err != nil {
			slog.WarnContext(ctx, "could not save turn trace link", "session", sessionID, "error", err)
		}
	}
}

// drain runs a turn's events through handler. A turn whose last model
// reply stopped at the output limit ends with ErrOutputLimit: the reply,
// or the tool call it was writing, was cut off, and without a word the
// turn would seem to have done nothing.
func drain(events func(yield func(*session.Event, error) bool), handler EventHandler) error {
	var ended error // why the last model reply ended badly (spec_056 CA-31)
	for ev, err := range events {
		if err != nil {
			if errors.Is(err, api.ErrMaxTurns) {
				return err
			}
			return fmt.Errorf("agent execution error: %w", err)
		}
		if ev != nil && !ev.Partial && ev.Author != "user" {
			// The last model reply decides: a tool's result after a cut-off
			// one (the call was whole) clears it.
			ended = finishError(&ev.LLMResponse)
		}
		if handler != nil && ev != nil {
			if hErr := handler(ev); hErr != nil {
				return hErr
			}
		}
	}
	return ended
}

// InvokeSubagent runs a single registered agent on prompt in a fresh, isolated
// session and returns the text it produced. Nesting is limited to
// MaxSubagentDepth to stop agents delegating to each other indefinitely.
// Usage and turn limits of the calling run still apply.
func (e *Engine) InvokeSubagent(ctx context.Context, agentName, prompt string) (_ string, err error) {
	depth, _ := ctx.Value(subagentDepthKey{}).(int)
	if depth >= MaxSubagentDepth {
		return "", fmt.Errorf("%w (%d): an agent this deep can't delegate; do the work itself, or have a shallower agent hand it out", ErrSubagentDepth, MaxSubagentDepth)
	}
	spec, ok := e.agentReg.Get(agentName)
	if !ok {
		return "", &api.UnknownAgentError{Name: agentName}
	}

	e.mu.RLock()
	llm := e.modelForLocked(spec.Name)
	if st := stateFrom(ctx); st != nil && (st.model != nil || st.conversationModel != nil) { // a run with its own model
		llm = e.modelInTreeLocked(spec.Name, cmp.Or(st.agent, e.active), e.runModelOf(st))
	}
	reg := e.toolReg
	if r, ok := ctx.Value(isolatedToolsKey{}).(*tools.Registry); ok { // a task in its own worktree
		reg = r
	}
	sub, err := e.newLLMAgentWith(reg, spec, llm, e.subInstruction(spec), nil, e.toolReg.MCP().ToolsetsFor(agentName, false))
	e.mu.RUnlock()
	if err != nil {
		return "", fmt.Errorf("failed to build sub-agent %s: %w", agentName, err)
	}

	r, err := runner.NewInMemory(appName+"-subagent", sub)
	if err != nil {
		return "", fmt.Errorf("failed to create sub-agent runner: %w", err)
	}

	subCtx := WithProviderToolPolicy(withSettingsLookup(context.WithValue(ctx, subagentDepthKey{}, depth+1), e.lookupSettings), e.providerToolPolicy())
	subCtx = e.agentRun(subCtx, spec)
	// A sub-agent is never the primary one: only servers naming it are its.
	subCtx = tools.WithPrimaryAgent(subCtx, "")
	// Its shells and tasks count for the session that started it.
	subCtx = tools.WithOwnerSession(subCtx, e.sessionOf(ctx))
	observe, _ := ctx.Value(subagentEventKey{}).(func(*session.Event))
	sessionID := fmt.Sprintf("subagent-%s-%d", agentName, e.subagentSeq.Add(1))
	hooks := e.toolReg.ScriptHooks()
	hooks.Async(ctx, "subagent_start", agentName, tools.HookEvent{SessionID: e.sessionOf(ctx), Subagent: agentName, Prompt: prompt})
	var out strings.Builder
	defer func() {
		e.scoped.forget(sessionID) // its session ends here: what it was handed goes with it
		ev := tools.HookEvent{SessionID: e.sessionOf(ctx), Subagent: agentName, Output: out.String()}
		if err != nil {
			ev.Error = err.Error()
		}
		hooks.Async(ctx, "subagent_stop", agentName, ev)
	}()
	err = drain(r.Run(subCtx, "user", sessionID, genai.NewContentFromText(prompt, genai.RoleUser), agent.RunConfig{}),
		func(ev *session.Event) error {
			if observe != nil {
				observe(ev)
			}
			if ev.Author != agentName || ev.Partial || ev.Content == nil {
				return nil
			}
			for _, part := range ev.Content.Parts {
				if part.Text != "" && !part.Thought {
					out.WriteString(part.Text)
				}
			}
			return nil
		})
	return out.String(), err
}
