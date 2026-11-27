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
	"math"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/images"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session/compaction"
	"google.golang.org/adk/v2/tool"
)

// SetActiveAgent switches the primary active agent persona and rebuilds the agent tree.
func (e *Engine) SetActiveAgent(ctx context.Context, agentName string) error {
	if _, ok := e.agentReg.Get(agentName); !ok {
		return &api.UnknownAgentError{Name: agentName}
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

// SetPersonality replaces the root agent's personality (WithPersonality)
// from the next turn; "" removes it.
func (e *Engine) SetPersonality(ctx context.Context, text string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.personality
	e.personality = text
	if err := e.rebuildLocked(); err != nil {
		e.personality = prev
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
	e.llm = withImages(llm, e.media)
	if err := e.rebuildLocked(); err != nil {
		e.llm = prev
		return err
	}
	return nil
}

// SetUploader gives the models a way to send files too large to go
// inline (Gemini's Files API); nil for none.
func (e *Engine) SetUploader(up images.Uploader) {
	e.media.mu.Lock()
	defer e.media.mu.Unlock()
	e.media.up = up
}

// AcceptedMedia is what agent's model takes as attachments ("": the
// active agent's), for refusing others at upload (spec_011_images).
func (e *Engine) AcceptedMedia(agent string) []Accept {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return acceptedFor(e.modelForLocked(cmp.Or(agent, e.active)), e.media.uploader() != nil)
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
		return &api.UnknownAgentError{Name: agent}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	prev, had := e.agentModels[agent]
	if e.agentModels == nil {
		e.agentModels = map[string]model.LLM{}
	}
	e.agentModels[agent] = withImages(llm, e.media)
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

// newLLMAgent builds spec's agent running on llm.
func (e *Engine) newLLMAgent(spec *agents.AgentSpec, llm model.LLM, instruction string, subAgents []agent.Agent, toolsets []tool.Toolset, extra ...tool.Tool) (agent.Agent, error) {
	return e.newLLMAgentWith(e.toolReg, spec, llm, instruction, subAgents, toolsets, extra...)
}

// newLLMAgentWith is newLLMAgent with the tools of reg: a task isolated in
// a worktree works on its files.
func (e *Engine) newLLMAgentWith(reg *tools.Registry, spec *agents.AgentSpec, llm model.LLM, instruction string, subAgents []agent.Agent, toolsets []tool.Toolset, extra ...tool.Tool) (agent.Agent, error) {
	return llmagent.New(e.agentConfig(reg, spec, llm, instruction, subAgents, toolsets, extra...))
}

// newTreeSubAgent builds spec's agent as a sub-agent of a tree's root,
// for transfer_to_agent. It never transfers back, so the ADK runner,
// which hands the next turn to the agent that answered last if it can,
// gives each turn to the root: the agent the run asked for (the active
// one, or a command's), whoever answered before.
func (e *Engine) newTreeSubAgent(spec *agents.AgentSpec, llm model.LLM, toolsets []tool.Toolset) (agent.Agent, error) {
	cfg := e.agentConfig(e.toolReg, spec, llm, e.subInstruction(spec), nil, toolsets)
	cfg.DisallowTransferToParent = true
	return llmagent.New(cfg)
}

// agentConfig is the configuration of spec's agent on llm, with the tools
// of reg.
func (e *Engine) agentConfig(reg *tools.Registry, spec *agents.AgentSpec, llm model.LLM, instruction string, subAgents []agent.Agent, toolsets []tool.Toolset, extra ...tool.Tool) llmagent.Config {
	list := reg.GetToolsForAgent(spec.Tools)
	for _, t := range extra {
		if !slices.ContainsFunc(list, func(x tool.Tool) bool { return x.Name() == t.Name() }) {
			list = append(list, t)
		}
	}
	// InstructionProvider returns the text verbatim: Config.Instruction
	// treats every {ident} in prompts, AGENTS.md or notes as an ADK
	// session-state template key and fails the turn when it is unset.
	fullInstruction := instruction + e.imageInstruction(spec) + diagramInstruction + e.extraInstructions
	return llmagent.Config{
		Name:                  spec.Name,
		Description:           spec.Description,
		InstructionProvider:   func(agent.ReadonlyContext) (string, error) { return e.expandSecrets(fullInstruction), nil },
		Model:                 withAgentSettings(llm, spec),
		Tools:                 list,
		Toolsets:              toolsets,
		SubAgents:             subAgents,
		GenerateContentConfig: e.generateConfig(),
		BeforeModelCallbacks:  []llmagent.BeforeModelCallback{e.beforeModel},
		AfterModelCallbacks:   []llmagent.AfterModelCallback{e.afterModel},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{e.beforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{e.afterTool},
	}
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
	if slices.Contains(spec.Tools, "view_media") {
		text += " If your model takes audio or video, view_media gives you a recording or a video in the workspace."
	}
	if slices.Contains(spec.Tools, "view_document") {
		text += " To read a PDF in the workspace (a paper, slides), call view_document with its path; the document follows the tool result."
	}
	return text
}

// diagramInstruction asks for diagrams as Mermaid, which the desktop app
// and the docs render, instead of ASCII boxes and arrows, which they can't.
const diagramInstruction = "\n\n## Diagrams\nWhen Markdown you write (files, plans, docs, replies) needs a diagram, draw it as a fenced ```mermaid block in valid Mermaid syntax (flowchart, sequenceDiagram, classDiagram, stateDiagram-v2, erDiagram, gantt). Never draw boxes and arrows in ASCII. Quote node labels that hold punctuation, such as A[\"parse (v2)\"]. Directory trees and tables stay as plain text and Markdown tables."

// subagentSummaryInstruction asks delegated sub-agents to end with a concise
// summary so the calling agent's context stays lean (Implements: AG-31b).
const subagentSummaryInstruction = "\n\nEnd with a concise summary: what changed or was found, how it was verified, and any open issues."

func (e *Engine) subInstruction(spec *agents.AgentSpec) string {
	text := spec.InterpolatePrompt(spec.AgencyLevel) + subagentSummaryInstruction
	if slices.Contains(spec.Tools, "activate_skill") { // a specialist that loads skills sees them
		text += e.skillIndex(spec.Name)
	}
	return text
}

// rootPrompt is spec's prompt as the active agent: at the session's
// agency level, with its skills listed (newLLMAgent adds the image,
// diagram and project instructions).
func (e *Engine) rootPrompt(spec *agents.AgentSpec) string {
	return spec.InterpolatePrompt(e.agencyLevel()) + e.skillIndex(spec.Name)
}

// skillIndex lists the skills offered to the named agent, for its
// instruction; empty with skills off or none offered.
func (e *Engine) skillIndex(agent string) string {
	if !e.cfg.Skills.Enabled || e.skillProv == nil {
		return ""
	}
	var sb strings.Builder
	for _, s := range e.skillProv.ListFor(agent) { // its own versions (EN-82)
		if s.OfferedTo(agent) { // agents: offers it to others only
			fmt.Fprintf(&sb, "- **%s**: %s%s\n", s.Name, s.Description, skills.TriggerHint(s.TriggerPhrases))
		}
	}
	if sb.Len() == 0 {
		return ""
	}
	return "\n\n## Available Agent Skills:\n" + sb.String() +
		"\nUse `activate_skill` to load full skill instructions whenever relevant. A skill's \"for requests like\" are examples of what it's for: match a request to them by meaning, not by wording.\n"
}

// rebuildLocked requires e.mu held for writing.
func (e *Engine) rebuildLocked() error {
	if _, ok := e.agentReg.Get(e.active); !ok {
		if _, ok := e.agentReg.Get("blitz"); !ok {
			return fmt.Errorf("default agent 'blitz' not found in registry")
		}
		e.active = "blitz"
	}
	t, err := e.buildTreeLocked(e.active, runModel{}, true)
	if err != nil {
		return err
	}
	e.runner, e.rootAgent, e.compactionCfg, e.compactAt = t.runner, t.root, t.compaction, t.point
	return nil
}

// tree is a runner over an agent tree, and the model each agent runs on.
type tree struct {
	runner     *runner.Runner
	root       agent.Agent
	compaction *compaction.Config
	models     map[string]model.LLM
	point      int // the root model's compaction point, automatic or not
}

// modelInTreeLocked is the model agent runs on in a tree rooted at active
// whose configured model is replaced by override (nil: none): the override
// runs the root agent even when it is pinned, and every unpinned agent.
// e.mu must be held.
func (e *Engine) modelInTreeLocked(agentName, active string, o runModel) model.LLM {
	if o.llm != nil {
		_, pinned := e.agentModels[agentName]
		if !pinned || agentName == active && !o.pins {
			return o.llm
		}
	}
	return e.modelForLocked(agentName)
}

// runModel is the model a run uses in place of the configured one: a
// run's own (WithModel), which its root agent runs on even if pinned, or
// its conversation's (WithConversationModel), which every pinned agent
// keeps its pin over (spec_056 CA-100). The zero value is none.
type runModel struct {
	llm  model.LLM
	pins bool
}

// buildTreeLocked builds a runner whose root is active, with every other
// agent as a sub-agent. override (the zero value: none) replaces the
// configured model (see modelInTreeLocked). The root has the personality
// when personal. It changes nothing in e, so e.mu may be held for reading
// only.
func (e *Engine) buildTreeLocked(active string, override runModel, personal bool) (tree, error) {
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
		sub, err := e.newTreeSubAgent(spec, llm, e.toolReg.MCP().ToolsetsFor(spec.Name, false))
		if err != nil {
			return tree{}, fmt.Errorf("failed to build sub-agent %s: %w", spec.Name, err)
		}
		subAgents = append(subAgents, sub)
	}

	rootInstruction := e.rootPrompt(rootSpec)

	// MCP servers choose their agents; by default only the primary agent.
	llm := e.modelInTreeLocked(active, active, override)
	t.models[active] = llm
	cfg := e.agentConfig(e.toolReg, rootSpec, llm, rootInstruction, subAgents, e.toolReg.MCP().ToolsetsFor(rootSpec.Name, true), e.toolReg.WorkflowTools()...)
	if p := e.personality; personal && p != "" {
		base := cfg.InstructionProvider
		cfg.InstructionProvider = func(c agent.ReadonlyContext) (string, error) {
			s, err := base(c)
			return s + p, err
		}
	}
	rootAgent, err := llmagent.New(cfg)
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
	if info, ok := infoOf(llm); ok {
		threshold = info.CompactionPoint(threshold) // the model's own (spec_056 CA-110)
	}
	point := threshold
	if !c.Compaction || threshold <= 0 {
		threshold = math.MaxInt32
	}
	rc.Compaction = &compaction.Config{TokenThreshold: threshold, EventRetentionSize: retain}
	r, err := runner.New(rc)
	if err != nil {
		return tree{}, fmt.Errorf("failed to instantiate ADK runner: %w", err)
	}
	t.runner, t.root, t.compaction, t.point = r, rootAgent, rc.Compaction, point
	return t, nil
}

// CompactionThreshold is where the active model's conversations are
// compacted (spec_056 CA-110), for the context meter.
func (e *Engine) CompactionThreshold() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.compactAt
}

// runModelOf is the model st's run uses in place of the configured one.
func (e *Engine) runModelOf(st *runState) runModel {
	switch {
	case st.model != nil:
		return runModel{llm: withImages(st.model, e.media)}
	case st.conversationModel != nil:
		return runModel{llm: withImages(st.conversationModel, e.media), pins: true}
	}
	return runModel{}
}
