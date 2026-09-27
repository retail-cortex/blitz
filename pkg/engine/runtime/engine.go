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
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
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
	"github.com/retail-cortex/blitz/pkg/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/platform"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/compaction"
	"google.golang.org/adk/v2/tool"
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
)

// runState is carried in the context of one Execute call.
type runState struct {
	sessionID   string
	maxTurns    int
	turns       atomic.Int64
	attachments []*genai.Part
	planOnly    bool   // refuse tools that could change anything (see WithPlanOnly)
	mode        string // names a read-only mode other than plan in refusals (see WithReadOnly)
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
}

func stateFrom(ctx context.Context) *runState {
	s, _ := ctx.Value(runStateKey{}).(*runState)
	return s
}

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

// WithNotice sets where the engine reports events the user should know
// about, such as a fallback model taking over (default: nowhere).
func WithNotice(f func(string)) Option { return func(e *Engine) { e.notice = f } }

// WithAgentModel runs agent on llm instead of the engine's model (a pin
// from [agent_models] or the agent's own default_model).
func WithAgentModel(agent string, llm model.LLM) Option {
	return func(e *Engine) {
		if e.agentModels == nil {
			e.agentModels = map[string]model.LLM{}
		}
		e.agentModels[agent] = withImages(llm, e.toolReg.Images())
	}
}

// WithInstructions appends text (e.g. project memory) to every agent's instructions.
func WithInstructions(text string) Option { return func(e *Engine) { e.extraInstructions = text } }

// Engine orchestrates the Google ADK execution lifecycle for Blitz.
// It is safe for concurrent use.
type Engine struct {
	cfg       *config.Config
	agentReg  *agents.Registry
	skillProv *skills.Provider
	toolReg   *tools.Registry
	usage     *UsageTracker

	// Shared across runner rebuilds so switching agent or model keeps history.
	sessions  session.Service
	artifacts artifact.Service
	memories  memory.Service

	subagentSeq atomic.Int64
	turns       TurnStore // nil: turns are not chained

	steerMu sync.Mutex
	steers  map[string][]string // session ID -> messages sent mid-turn

	notice     func(string)
	settingsMu sync.RWMutex
	settings   map[string]config.ModelSettings // model name -> [model_settings]
	effort     string                          // session-wide reasoning effort (/effort)

	scoped scopedRules // path-scoped project rules

	fallbackMu sync.Mutex
	fallbackBy string // fallback model answering now; "" when the primary is

	mu                sync.RWMutex
	llm               model.LLM
	agentModels       map[string]model.LLM // pinned agents; others use llm
	runner            *runner.Runner
	rootAgent         agent.Agent        // the runner's agent tree, for Aside
	compactionCfg     *compaction.Config // the runner's, for Aside
	active            string
	extraInstructions string
	streaming         bool
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
	e := &Engine{
		cfg:       cfg,
		agentReg:  agentReg,
		skillProv: skillProv,
		toolReg:   toolReg,
		usage:     NewUsageTracker(cfg.Pricing),
		sessions:  session.InMemoryService(),
		artifacts: artifact.InMemoryService(),
		memories:  memory.InMemoryService(),
		llm:       withImages(llm, toolReg.Images()),
		active:    cfg.Blitz.DefaultAgent,
		settings:  map[string]config.ModelSettings{},
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

	e.mu.Lock()
	err := e.rebuildLocked()
	e.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ADK runner: %w", err)
	}

	toolReg.Hooks().SetSubagentInvoker(e.InvokeSubagent)
	return e, nil
}

// SetActiveAgent switches the primary active agent persona and rebuilds the agent tree.
func (e *Engine) SetActiveAgent(ctx context.Context, agentName string) error {
	if _, ok := e.agentReg.Get(agentName); !ok {
		return fmt.Errorf("agent '%s' not found", agentName)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.active
	e.active = agentName
	if err := e.rebuildLocked(); err != nil {
		e.active = prev
		return err
	}
	return nil
}

// Rebuild regenerates agent instructions, e.g. after settings change.
func (e *Engine) Rebuild(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rebuildLocked()
}

// SetInstructions replaces the extra instructions (project memory) and rebuilds.
func (e *Engine) SetInstructions(ctx context.Context, text string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.extraInstructions
	e.extraInstructions = text
	if err := e.rebuildLocked(); err != nil {
		e.extraInstructions = prev
		return err
	}
	return nil
}

// SetModel swaps the LLM used by all agents.
func (e *Engine) SetModel(ctx context.Context, llm model.LLM) error {
	if llm == nil {
		return errors.New("model must not be nil")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.llm
	e.llm = withImages(llm, e.toolReg.Images())
	if err := e.rebuildLocked(); err != nil {
		e.llm = prev
		return err
	}
	return nil
}

// ActiveAgent returns the name of the currently active agent persona.
func (e *Engine) ActiveAgent() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.active
}

// ModelName returns the name of the model the active agent runs on.
func (e *Engine) ModelName() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.modelForLocked(e.active).Name()
}

// AgentModel returns the model agent runs on and whether it is pinned.
func (e *Engine) AgentModel(agent string) (name string, pinned bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, pinned = e.agentModels[agent]
	return e.modelForLocked(agent).Name(), pinned
}

// PinModel runs agent on llm from now on (other agents are unaffected).
func (e *Engine) PinModel(ctx context.Context, agent string, llm model.LLM) error {
	if llm == nil {
		return errors.New("model must not be nil")
	}
	if _, ok := e.agentReg.Get(agent); !ok {
		return fmt.Errorf("agent '%s' not found", agent)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	prev, had := e.agentModels[agent]
	if e.agentModels == nil {
		e.agentModels = map[string]model.LLM{}
	}
	e.agentModels[agent] = withImages(llm, e.toolReg.Images())
	if err := e.rebuildLocked(); err != nil {
		if had {
			e.agentModels[agent] = prev
		} else {
			delete(e.agentModels, agent)
		}
		return err
	}
	return nil
}

// Unpin returns agent to the engine's model.
func (e *Engine) Unpin(ctx context.Context, agent string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev, had := e.agentModels[agent]
	if !had {
		return nil
	}
	delete(e.agentModels, agent)
	if err := e.rebuildLocked(); err != nil {
		e.agentModels[agent] = prev
		return err
	}
	return nil
}

// modelForLocked returns the model agent runs on; e.mu must be held.
func (e *Engine) modelForLocked(agent string) model.LLM {
	if m, ok := e.agentModels[agent]; ok {
		return m
	}
	return e.llm
}

// Usage returns the token usage and estimated cost recorded for a session.
func (e *Engine) Usage(sessionID string) api.Usage { return e.usage.Session(sessionID) }

// settingsName is the key a model's settings are kept under: its name as
// the provider reports it, without a "provider/" prefix.
func settingsName(ref string) string {
	_, name := ParseModelRef(ref, "")
	return name
}

// ModelSettings returns the generation settings for model ("provider/"
// prefix optional); the zero value when it has none.
func (e *Engine) ModelSettings(model string) config.ModelSettings {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	return e.settings[settingsName(model)]
}

// AllModelSettings returns every model's settings, by model name.
func (e *Engine) AllModelSettings() map[string]config.ModelSettings {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	out := make(map[string]config.ModelSettings, len(e.settings))
	for k, v := range e.settings {
		out[k] = v
	}
	return out
}

// SetModelSettings replaces model's settings (the zero value removes
// them). They apply from the next model call, also to calls already running.
func (e *Engine) SetModelSettings(model string, s config.ModelSettings) {
	name := settingsName(model)
	e.settingsMu.Lock()
	defer e.settingsMu.Unlock()
	if s.IsZero() {
		delete(e.settings, name)
	} else {
		e.settings[name] = s
	}
}

func (e *Engine) lookupSettings(name string) (config.ModelSettings, bool) {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	s, ok := e.settings[name]
	if e.effort != "" { // the session's effort wins over the model's own
		effort := e.effort
		s.ReasoningEffort, ok = &effort, true
	}
	return s, ok
}

// SetEffort sets the reasoning effort for every model call from now on
// ("" returns each model to its own reasoning_effort, or the default).
func (e *Engine) SetEffort(effort string) {
	e.settingsMu.Lock()
	defer e.settingsMu.Unlock()
	e.effort = effort
}

// Effort returns the session's reasoning effort ("" when unset).
func (e *Engine) Effort() string {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	return e.effort
}

func (e *Engine) generateConfig() *genai.GenerateContentConfig {
	gc := &genai.GenerateContentConfig{}
	if e.cfg.Blitz.Temperature > 0 {
		gc.Temperature = genai.Ptr(float32(e.cfg.Blitz.Temperature))
	}
	if e.cfg.Blitz.MaxTokens > 0 {
		gc.MaxOutputTokens = int32(e.cfg.Blitz.MaxTokens)
	}
	return gc
}

// newLLMAgent builds spec's agent running on llm.
func (e *Engine) newLLMAgent(spec *agents.AgentSpec, llm model.LLM, instruction string, subAgents []agent.Agent, toolsets []tool.Toolset, extra ...tool.Tool) (agent.Agent, error) {
	list := e.toolReg.GetToolsForAgent(spec.Tools)
	for _, t := range extra {
		if !slices.ContainsFunc(list, func(x tool.Tool) bool { return x.Name() == t.Name() }) {
			list = append(list, t)
		}
	}
	return llmagent.New(llmagent.Config{
		Name:                  spec.Name,
		Description:           spec.Description,
		Instruction:           instruction + e.imageInstruction(spec) + e.extraInstructions,
		Model:                 llm,
		Tools:                 list,
		Toolsets:              toolsets,
		SubAgents:             subAgents,
		GenerateContentConfig: e.generateConfig(),
		BeforeModelCallbacks:  []llmagent.BeforeModelCallback{e.beforeModel},
		AfterModelCallbacks:   []llmagent.AfterModelCallback{e.afterModel},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{e.beforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{e.afterTool},
	})
}

// beforeModel enforces the per-run model-call budget.
func (e *Engine) beforeModel(ctx agent.Context, _ *model.LLMRequest) (*model.LLMResponse, error) {
	if st := stateFrom(ctx); st != nil && st.maxTurns > 0 {
		if n := st.turns.Add(1); n > int64(st.maxTurns) {
			return nil, fmt.Errorf("%w (%d model calls)", api.ErrMaxTurns, st.maxTurns)
		}
	}
	return nil, nil
}

// afterModel records token usage for the run's session.
func (e *Engine) afterModel(ctx agent.Context, resp *model.LLMResponse, respErr error) (*model.LLMResponse, error) {
	if resp == nil || resp.Partial {
		return nil, nil
	}
	e.noteFallback(resp)
	if resp.UsageMetadata == nil {
		return nil, nil
	}
	id := "default"
	if st := stateFrom(ctx); st != nil {
		id = st.sessionID
	}
	// Price by the model that actually answered: with server-side fallback
	// it can differ from the configured one.
	served := resp.ModelVersion
	if served == "" || !e.usage.HasPrice(served) {
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
	e.usage.RecordWrites(id, served, resp.UsageMetadata, writes)
	return nil, nil
}

// noteFallback tells the user when a fallback model starts answering and
// when the primary is back, once per change rather than on every call.
func (e *Engine) noteFallback(resp *model.LLMResponse) {
	primary, _ := resp.CustomMetadata[FallbackFromKey].(string)
	served := ""
	if primary != "" {
		served = resp.ModelVersion
	}
	e.fallbackMu.Lock()
	prev := e.fallbackBy
	e.fallbackBy = served
	e.fallbackMu.Unlock()
	if served == prev || e.notice == nil {
		return
	}
	if served != "" {
		e.notice(i18n.T("model.fallback", "primary", primary, "model", served))
	} else {
		e.notice(i18n.T("model.fallback_recovered", "model", e.ModelName()))
	}
}

// beforeTool audits the call, gates MCP tools, and runs pre_tool hooks.
// Returning a result map skips the tool and hands that result to the model.
func (e *Engine) beforeTool(ctx agent.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
	log := e.toolReg.Hooks().Audit()
	log.Log(audit.Entry{Kind: audit.KindToolCall, Tool: t.Name(), Args: args})

	if r := planRefusal(stateFrom(ctx), tools.PlanGateFrom(ctx).Planning(), t.Name()); r != nil {
		return r, nil
	}
	if r := allowedRefusal(stateFrom(ctx), t.Name()); r != nil {
		return r, nil
	}
	if rule := e.toolReg.Rules().ToolDenied(t.Name(), args); rule != nil {
		log.Log(audit.Entry{Kind: audit.KindDenial, Tool: t.Name(), Decision: "rule-deny " + rule.String()})
		return map[string]any{"error": fmt.Sprintf("denied by the permission rule deny %s", rule)}, nil
	}
	if err := e.toolReg.ApproveMCP(ctx, t.Name(), args); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	if reason := e.toolReg.ScriptHooks().PreTool(ctx, e.sessionOf(ctx), t.Name(), args); reason != "" {
		return map[string]any{"error": "blocked by pre_tool hook: " + reason}, nil
	}
	return nil, nil
}

// afterTool runs post_tool hooks, audits the outcome, and attaches messages
// the user sent while the turn was running (see Steer).
func (e *Engine) afterTool(ctx agent.Context, t tool.Tool, args, result map[string]any, toolErr error) (map[string]any, error) {
	e.toolReg.ScriptHooks().PostTool(ctx, e.sessionOf(ctx), t.Name(), args, result, toolErr)
	entry := audit.Entry{Kind: audit.KindToolResult, Tool: t.Name(), Decision: "ok"}
	if toolErr != nil {
		entry.Decision, entry.Error = "error", toolErr.Error()
	} else if msg, _ := result["error"].(string); msg != "" {
		entry.Decision, entry.Error = "error", msg
	}
	e.toolReg.Hooks().Audit().Log(entry)
	out := result
	changed := false
	if r := e.attachRules(ctx, t.Name(), args, out, toolErr); r != nil {
		out, changed = r, true
	}
	if r := e.attachSteers(ctx, out, toolErr); r != nil {
		out, changed = r, true
	}
	if changed {
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

// imageInstruction tells agents they can see images. Without it, personas
// that describe themselves as code assistants tend to claim they can't,
// even when the picture is in the request.
func (e *Engine) imageInstruction(spec *agents.AgentSpec) string {
	if e.toolReg.Images() == nil {
		return ""
	}
	text := "\n\n## Images\nYou can see images. When the user attaches an image or screenshot, it is included in their message: look at it and answer about what it actually shows (text, UI, diagrams, errors)."
	if slices.Contains(spec.Tools, "view_image") {
		text += " To look at an image file in the workspace, call view_image with its path; the picture follows the tool result."
	}
	return text
}

func (e *Engine) subInstruction(spec *agents.AgentSpec) string {
	return spec.InterpolatePrompt(spec.AgencyLevel)
}

// rebuildLocked requires e.mu held for writing.
func (e *Engine) rebuildLocked() error {
	if _, ok := e.agentReg.Get(e.active); !ok {
		if _, ok := e.agentReg.Get("blitz"); !ok {
			return fmt.Errorf("default agent 'blitz' not found in registry")
		}
		e.active = "blitz"
	}
	t, err := e.buildTreeLocked(e.active, nil)
	if err != nil {
		return err
	}
	e.runner, e.rootAgent, e.compactionCfg = t.runner, t.root, t.compaction
	return nil
}

// tree is a runner over an agent tree, and the model each agent runs on.
type tree struct {
	runner     *runner.Runner
	root       agent.Agent
	compaction *compaction.Config
	models     map[string]model.LLM
}

// modelInTreeLocked is the model agent runs on in a tree rooted at active
// whose configured model is replaced by override (nil: none): the override
// runs the root agent even when it is pinned, and every unpinned agent.
// e.mu must be held.
func (e *Engine) modelInTreeLocked(agentName, active string, override model.LLM) model.LLM {
	if override != nil {
		if _, pinned := e.agentModels[agentName]; agentName == active || !pinned {
			return override
		}
	}
	return e.modelForLocked(agentName)
}

// buildTreeLocked builds a runner whose root is active, with every other
// agent as a sub-agent. override (nil: none) replaces the configured model
// (see modelInTreeLocked). It changes nothing in e, so e.mu may be held
// for reading only.
func (e *Engine) buildTreeLocked(active string, override model.LLM) (tree, error) {
	rootSpec, ok := e.agentReg.Get(active)
	if !ok {
		return tree{}, fmt.Errorf("agent '%s' not found", active)
	}
	t := tree{models: map[string]model.LLM{}}

	var subAgents []agent.Agent
	for _, spec := range e.agentReg.List() {
		if spec.Name == active {
			continue
		}
		llm := e.modelInTreeLocked(spec.Name, active, override)
		t.models[spec.Name] = llm
		sub, err := e.newLLMAgent(spec, llm, e.subInstruction(spec), nil, e.toolReg.MCP().ToolsetsFor(spec.Name, false))
		if err != nil {
			return tree{}, fmt.Errorf("failed to build sub-agent %s: %w", spec.Name, err)
		}
		subAgents = append(subAgents, sub)
	}

	rootInstruction := rootSpec.InterpolatePrompt(e.cfg.Blitz.AgencyLevel)
	if e.cfg.Skills.Enabled && e.skillProv != nil {
		if allSkills := e.skillProv.List(); len(allSkills) > 0 {
			var sb strings.Builder
			sb.WriteString("\n\n## Available Agent Skills:\n")
			for _, s := range allSkills {
				fmt.Fprintf(&sb, "- **%s**: %s\n", s.Name, s.Description)
			}
			sb.WriteString("\nUse `activate_skill` to load full skill instructions whenever relevant.\n")
			rootInstruction += sb.String()
		}
	}

	// MCP servers choose their agents; by default only the primary agent.
	llm := e.modelInTreeLocked(active, active, override)
	t.models[active] = llm
	rootAgent, err := e.newLLMAgent(rootSpec, llm, rootInstruction, subAgents, e.toolReg.MCP().ToolsetsFor(rootSpec.Name, true), e.toolReg.WorkflowTools()...)
	if err != nil {
		return tree{}, fmt.Errorf("failed to build root agent: %w", err)
	}

	rc := runner.Config{
		AppName:           appName,
		Agent:             rootAgent,
		SessionService:    e.sessions,
		ArtifactService:   e.artifacts,
		MemoryService:     e.memories,
		AutoCreateSession: true,
	}
	// Compaction is always configured, because the runner only honours
	// compaction events (including manual /compact summaries) when it is.
	// With automatic compaction off, the threshold is unreachable.
	c := e.cfg.Context
	retain := c.RetainEvents
	if retain <= 0 {
		retain = 20
	}
	threshold := c.TokenThreshold
	if !c.Compaction || threshold <= 0 {
		threshold = math.MaxInt32
	}
	rc.Compaction = &compaction.Config{TokenThreshold: threshold, EventRetentionSize: retain}
	r, err := runner.New(rc)
	if err != nil {
		return tree{}, fmt.Errorf("failed to instantiate ADK runner: %w", err)
	}
	t.runner, t.root, t.compaction = r, rootAgent, rc.Compaction
	return t, nil
}

// ExecOption configures one Execute call.
type ExecOption func(*runState)

// WithMaxTurns limits the number of model calls in the run (0 = unlimited).
func WithMaxTurns(n int) ExecOption { return func(s *runState) { s.maxTurns = n } }

// WithAgent runs the prompt with name as the root agent instead of the
// active one, for this run only (e.g. a worker's agent).
func WithAgent(name string) ExecOption { return func(s *runState) { s.agent = name } }

// WithModel runs the prompt on llm instead of the configured model, for
// this run only: the root agent runs on it even if pinned, and so does
// every unpinned agent (e.g. a worker's model).
func WithModel(llm model.LLM) ExecOption { return func(s *runState) { s.model = llm } }

// Execute runs a prompt within a session and streams ADK events to the handler.
func (e *Engine) Execute(ctx context.Context, sessionID, prompt string, handler EventHandler, opts ...ExecOption) error {
	if sessionID == "" {
		sessionID = "default"
	}
	st := &runState{sessionID: sessionID}
	for _, o := range opts {
		o(st)
	}
	r, agentName, modelName, err := e.runnerFor(st)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, runStateKey{}, st)
	ctx = withSettingsLookup(ctx, e.lookupSettings)
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
	before := e.usage.Session(sessionID)
	err = drain(r.Run(ctx, "user", sessionID, userContent(prompt, st.attachments), rc), handler)
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

// runnerFor returns the runner for a run and the root agent and model it
// uses: the engine's own, or, when the run names an agent or a model, one
// built for it alone (st.models then names each agent's model for pricing).
func (e *Engine) runnerFor(st *runState) (*runner.Runner, string, string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if st.agent == "" && st.model == nil {
		if e.runner == nil {
			return nil, "", "", fmt.Errorf("runner is not initialized")
		}
		return e.runner, e.active, e.modelForLocked(e.active).Name(), nil
	}
	active := cmp.Or(st.agent, e.active)
	var override model.LLM
	if st.model != nil {
		override = withImages(st.model, e.toolReg.Images())
	}
	t, err := e.buildTreeLocked(active, override)
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

func drain(events func(yield func(*session.Event, error) bool), handler EventHandler) error {
	for ev, err := range events {
		if err != nil {
			if errors.Is(err, api.ErrMaxTurns) {
				return err
			}
			return fmt.Errorf("agent execution error: %w", err)
		}
		if handler != nil && ev != nil {
			if hErr := handler(ev); hErr != nil {
				return hErr
			}
		}
	}
	return nil
}

// InvokeSubagent runs a single registered agent on prompt in a fresh, isolated
// session and returns the text it produced. Nesting is limited to
// MaxSubagentDepth to stop agents delegating to each other indefinitely.
// Usage and turn limits of the calling run still apply.
func (e *Engine) InvokeSubagent(ctx context.Context, agentName, prompt string) (_ string, err error) {
	depth, _ := ctx.Value(subagentDepthKey{}).(int)
	if depth >= MaxSubagentDepth {
		return "", fmt.Errorf("%w (%d)", ErrSubagentDepth, MaxSubagentDepth)
	}
	spec, ok := e.agentReg.Get(agentName)
	if !ok {
		return "", fmt.Errorf("agent '%s' not found", agentName)
	}

	e.mu.RLock()
	llm := e.modelForLocked(spec.Name)
	if st := stateFrom(ctx); st != nil && st.model != nil { // a run with its own model
		llm = e.modelInTreeLocked(spec.Name, cmp.Or(st.agent, e.active), withImages(st.model, e.toolReg.Images()))
	}
	sub, err := e.newLLMAgent(spec, llm, e.subInstruction(spec), nil, e.toolReg.MCP().ToolsetsFor(agentName, false))
	e.mu.RUnlock()
	if err != nil {
		return "", fmt.Errorf("failed to build sub-agent %s: %w", agentName, err)
	}

	r, err := runner.NewInMemory(appName+"-subagent", sub)
	if err != nil {
		return "", fmt.Errorf("failed to create sub-agent runner: %w", err)
	}

	subCtx := withSettingsLookup(context.WithValue(ctx, subagentDepthKey{}, depth+1), e.lookupSettings)
	sessionID := fmt.Sprintf("subagent-%s-%d", agentName, e.subagentSeq.Add(1))
	hooks := e.toolReg.ScriptHooks()
	hooks.Async(ctx, "subagent_start", agentName, tools.HookEvent{SessionID: e.sessionOf(ctx), Subagent: agentName, Prompt: prompt})
	var out strings.Builder
	defer func() {
		ev := tools.HookEvent{SessionID: e.sessionOf(ctx), Subagent: agentName, Output: out.String()}
		if err != nil {
			ev.Error = err.Error()
		}
		hooks.Async(ctx, "subagent_stop", agentName, ev)
	}()
	err = drain(r.Run(subCtx, "user", sessionID, genai.NewContentFromText(prompt, genai.RoleUser), agent.RunConfig{}),
		func(ev *session.Event) error {
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
