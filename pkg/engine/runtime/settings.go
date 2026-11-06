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
	"fmt"
	"iter"
	"log/slog"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/modelfacts"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// modelSettingsKey carries the engine's per-model settings lookup in a
// run's context, so each model (including each member of a fallback chain)
// applies its own settings, and changes apply to the next call.
type modelSettingsKey struct{}

type settingsLookup func(model string) (config.ModelSettings, bool)

func withSettingsLookup(ctx context.Context, f settingsLookup) context.Context {
	return context.WithValue(ctx, modelSettingsKey{}, f)
}

// settingsModel applies [model_settings] for the model it wraps. Settings
// the provider can't take are dropped rather than sent: the OpenAI adapter
// fails the request on a seed, and current Anthropic models reject sampling
// parameters.
type settingsModel struct {
	inner    model.LLM
	provider string
	// configured is the provider as configured (bedrock, vertex, …),
	// for the origin of what the model writes (spec_056 CA-02); provider
	// when unset.
	configured string
}

// info is the model's record now (spec_056 CA-10).
func (m *settingsModel) info() ModelInfo {
	return Info(m.provider, cmp.Or(m.configured, m.provider), m.inner.Name())
}

// origin is where this model's answers come from.
func (m *settingsModel) origin() Origin {
	return Origin{API: m.provider, Provider: cmp.Or(m.configured, m.provider), Model: m.inner.Name()}
}

func withModelSettings(m model.LLM, provider string) model.LLM {
	if m == nil {
		return nil
	}
	return &settingsModel{inner: m, provider: provider}
}

func (m *settingsModel) Name() string { return m.inner.Name() }

func (m *settingsModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	o := m.origin()
	back := namesBack(req)
	req = adaptRequest(o, req)
	if m.provider == "gemini" {
		req = attachLoneSignatures(req)
	}
	var s config.ModelSettings
	var ok bool
	if lookup, _ := ctx.Value(modelSettingsKey{}).(settingsLookup); lookup != nil {
		s, ok = lookup(m.inner.Name())
	}
	if own, has := ctx.Value(agentSettingsKey{}).(config.ModelSettings); has { // the agent's own win
		s, ok = overlay(s, own), true
	}
	if ok && req != nil {
		req = m.apply(ctx, req, s)
		if s.ReasoningEffort != nil && SettingSupported(m.provider, m.inner.Name(), "reasoning_effort") {
			// The exact level, for adapters with more levels than genai's
			// (Anthropic's max).
			ctx = context.WithValue(ctx, effortKey{}, *s.ReasoningEffort)
		}
	}
	info := m.info()
	req, stream = applyFacts(info, req, stream)
	return func(yield func(*model.LLMResponse, error) bool) {
		// A refusal for something the record let through, before any
		// output, teaches the record; the call goes again without it, once
		// (spec_056 CA-13).
		var tell []string
		send := req
		for attempt := 0; ; attempt++ {
			var learned *modelfacts.Fact
			answered := false
			for resp, err := range m.inner.GenerateContent(ctx, send, stream) {
				if err != nil && !answered && attempt == 0 {
					if f, ok := recogniseRefusal(err); ok {
						if line := learn(o.Provider, info.Name, f); line != "" {
							tell = append(tell, line)
						}
						if retryable(f) {
							learned = &f
							break
						}
					}
				}
				answered = true
				stampResponse(o, resp)
				nameCallsBack(resp, back)
				if resp != nil && !resp.Partial {
					tellLearned(resp, tell)
					tell = nil
				}
				if err != nil && len(tell) > 0 {
					err = fmt.Errorf("%w (%s)", err, strings.Join(tell, " "))
					tell = nil
				}
				if !yield(resp, asQuota(err)) {
					return
				}
			}
			if learned == nil {
				return
			}
			slog.InfoContext(ctx, "model refused part of a request: learned and sent again", "model", info.Name, "fact", learned.Kind, "value", learned.Value)
			info = withFact(info, *learned)
			send, stream = applyFacts(info, req, stream)
		}
	}
}

// apply returns a copy of req with s applied; req itself is shared with
// the other models of a fallback chain, so it is never changed.
func (m *settingsModel) apply(ctx context.Context, req *model.LLMRequest, s config.ModelSettings) *model.LLMRequest {
	cp := *req
	gc := genai.GenerateContentConfig{}
	if req.Config != nil {
		gc = *req.Config
	}
	var dropped []string
	use := func(key string) bool {
		if SettingSupported(m.provider, m.inner.Name(), key) {
			return true
		}
		dropped = append(dropped, key)
		return false
	}
	if s.Temperature != nil && use("temperature") {
		gc.Temperature = genai.Ptr(float32(*s.Temperature))
	}
	if s.TopP != nil && use("top_p") {
		gc.TopP = genai.Ptr(float32(*s.TopP))
	}
	if s.MaxTokens != nil && use("max_tokens") {
		gc.MaxOutputTokens = int32(*s.MaxTokens)
	}
	if s.Seed != nil && use("seed") {
		gc.Seed = genai.Ptr(int32(*s.Seed))
	}
	effort := s.ReasoningEffort != nil && use("reasoning_effort")
	budget := s.ThinkingBudget != nil && use("thinking_budget")
	if budget && !SettingApplies(m.provider, m.inner.Name(), "thinking_budget", s) {
		budget = false
		dropped = append(dropped, "thinking_budget")
	}
	if effort && budget && m.provider == "gemini" {
		// Gemini rejects a level and a budget together; the level wins.
		budget = false
		dropped = append(dropped, "thinking_budget")
	}
	if effort || budget {
		tc := genai.ThinkingConfig{}
		if gc.ThinkingConfig != nil {
			tc = *gc.ThinkingConfig
		}
		if effort {
			tc.ThinkingLevel = thinkingLevels[*s.ReasoningEffort]
		}
		if budget {
			tc.ThinkingBudget = genai.Ptr(int32(*s.ThinkingBudget))
		}
		gc.ThinkingConfig = &tc
	}
	if len(dropped) > 0 {
		slog.DebugContext(ctx, "model settings not supported by this model; not sent", "model", m.inner.Name(), "settings", dropped)
	}
	cp.Config = &gc
	return &cp
}

// agentSettingsKey carries, in a model call's context, the model settings
// of the agent making it (its frontmatter's), which win over the model's
// own and the session's effort.
type agentSettingsKey struct{}

// agentSettingsModel calls its model with an agent's own model settings.
type agentSettingsModel struct {
	inner    model.LLM
	settings config.ModelSettings
}

// withAgentSettings runs m with spec's model settings, if it sets any.
func withAgentSettings(m model.LLM, spec *agents.AgentSpec) model.LLM {
	s, ok := spec.ModelSettings()
	if !ok || m == nil {
		return m
	}
	return &agentSettingsModel{inner: m, settings: s}
}

func (m *agentSettingsModel) Name() string { return m.inner.Name() }

func (m *agentSettingsModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return m.inner.GenerateContent(context.WithValue(ctx, agentSettingsKey{}, m.settings), req, stream)
}

// overlay returns base with every setting top sets replaced.
func overlay(base, top config.ModelSettings) config.ModelSettings {
	if top.Temperature != nil {
		base.Temperature = top.Temperature
	}
	if top.TopP != nil {
		base.TopP = top.TopP
	}
	if top.MaxTokens != nil {
		base.MaxTokens = top.MaxTokens
	}
	if top.Seed != nil {
		base.Seed = top.Seed
	}
	if top.ReasoningEffort != nil {
		base.ReasoningEffort = top.ReasoningEffort
	}
	if top.ThinkingBudget != nil {
		base.ThinkingBudget = top.ThinkingBudget
	}
	return base
}

// effortKey carries the exact reasoning effort in a request's context.
type effortKey struct{}

// effortFrom returns the reasoning effort set for this request ("" if none).
func effortFrom(ctx context.Context) string {
	e, _ := ctx.Value(effortKey{}).(string)
	return e
}

// thinkingLevels maps Blitz's efforts to genai's levels, which stop at high.
var thinkingLevels = map[string]genai.ThinkingLevel{
	"minimal": genai.ThinkingLevelMinimal, "low": genai.ThinkingLevelLow, "medium": genai.ThinkingLevelMedium,
	"high": genai.ThinkingLevelHigh, "max": genai.ThinkingLevelHigh,
}

// SettingApplies reports whether a model accepts a setting with the value
// s gives it: SettingSupported, and for thinking_budget 0 (thinking off),
// a model that lets thinking be turned off.
func SettingApplies(provider, modelName, key string, s config.ModelSettings) bool {
	if !SettingSupported(provider, modelName, key) {
		return false
	}
	if key == "thinking_budget" && provider == "anthropic" && s.ThinkingBudget != nil && *s.ThinkingBudget == 0 {
		return !alwaysThinks(modelName)
	}
	return true
}

// SettingSupported reports whether a model of provider accepts a
// [model_settings] key. Unsupported settings are left out of requests.
func SettingSupported(provider, modelName, key string) bool {
	switch provider {
	case "openai", "ollama", "api":
		// The Responses API has no seed; a thinking budget only chooses
		// between no reasoning (0) and medium.
		return key != "seed"
	case "anthropic":
		switch key {
		case "max_tokens":
			return true
		case "temperature":
			return supportsSampling(modelName)
		case "reasoning_effort":
			return supportsEffort(modelName)
		case "thinking_budget":
			return supportsThinking(modelName)
		}
		return false // no seed; top_p isn't mapped (some models reject it with temperature)
	case "gemini":
		// Thinking levels came with Gemini 3; earlier models take a budget.
		return key != "reasoning_effort" || !strings.HasPrefix(strings.TrimPrefix(modelName, "models/"), "gemini-2")
	}
	return true
}
