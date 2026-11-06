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
	"errors"
	"iter"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/runtime/wirefake"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// The conformance suite (spec_056_conversation_adapters CA-150): one
// conversation, run through the real engine and the real adapters against
// fakes as strict as each API (wirefake), switching between them.

// wire is one API in the suite: its fake, and the model that speaks it.
type wire struct {
	name string
	ref  string
	fake *wirefake.Fake
}

// wires stands up a fake for each API and points cfg at them.
func wires(t *testing.T, cfg *config.Config) map[string]*wire {
	t.Helper()
	g, a, o := wirefake.Gemini(t), wirefake.Anthropic(t), wirefake.OpenAI(t)
	t.Setenv("GOOGLE_GEMINI_BASE_URL", g.URL())
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	cfg.LLM.Gemini.APIKey = "test-key"
	cfg.LLM.Anthropic.APIKey = "test-key"
	cfg.LLM.Anthropic.BaseURL = a.URL()
	cfg.LLM.OpenAI.APIKey = "test-key"
	cfg.LLM.OpenAI.BaseURL = o.URL() + "/v1"
	return map[string]*wire{
		"gemini":    {name: "gemini", ref: "gemini/gemini-3-pro", fake: g},
		"anthropic": {name: "anthropic", ref: "anthropic/claude-opus-5-5", fake: a},
		"openai":    {name: "openai", ref: "openai/gpt-5", fake: o},
	}
}

func conformanceConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.UCToolsDir = t.TempDir()
	cfg.LLM.MaxRetries = 0
	require.NoError(t, os.WriteFile(filepath.Join(cfg.Tools.WorkspaceDir, "notes.txt"), []byte("hello\n"), 0o644))
	return cfg
}

func conformanceEngine(t *testing.T, cfg *config.Config, llm model.LLM) *Engine {
	t.Helper()
	agentReg, err := agents.NewRegistry()
	require.NoError(t, err)
	skillProv, err := skills.NewProvider()
	require.NoError(t, err)
	toolReg, err := tools.NewRegistry(cfg, agentReg, skillProv)
	require.NoError(t, err)
	t.Cleanup(func() { toolReg.Close() })
	eng, err := NewEngine(context.Background(), cfg, agentReg, skillProv, toolReg, llm)
	require.NoError(t, err)
	return eng
}

func modelFor(t *testing.T, cfg *config.Config, w *wire) model.LLM {
	t.Helper()
	m, err := NewModelRef(context.Background(), cfg, w.ref)
	require.NoError(t, err)
	return m
}

// A turn that reads a file (a tool call, with reasoning), then answers.
func toolTurn() []wirefake.Reply {
	return []wirefake.Reply{
		{Thought: "I should read the notes.", Calls: []wirefake.Call{{Name: "read_file", Args: map[string]any{"path": "notes.txt"}}}},
		{Thought: "It says hello.", Text: "The notes say hello."},
	}
}

// A conversation goes on after its model changes, in every direction: the
// next model gets a history it accepts (CA-01–CA-03, CA-20–CA-23).
func TestConformanceSwitchAtTurnEnd(t *testing.T) {
	names := []string{"gemini", "anthropic", "openai"}
	for _, from := range names {
		for _, to := range names {
			if from == to {
				continue
			}
			t.Run(from+" to "+to, func(t *testing.T) {
				cfg := conformanceConfig(t)
				ws := wires(t, cfg)
				first, next := ws[from], ws[to]
				eng := conformanceEngine(t, cfg, modelFor(t, cfg, first))

				first.fake.Script(toolTurn()...)
				_, err := collect(t, eng, "s", "What do my notes say?")
				require.NoError(t, err, "the first model's turn: %v", first.fake.Refusals())
				require.Empty(t, first.fake.Refusals())
				require.Zero(t, first.fake.Remaining(), "%s didn't get as far as its tool call and answer", from)

				require.NoError(t, eng.SetModel(context.Background(), modelFor(t, cfg, next)))
				next.fake.Script(toolTurn()...)
				_, err = collect(t, eng, "s", "Read them again, please.")
				assert.NoError(t, err)
				assert.Empty(t, next.fake.Refusals(), "%s refused the history %s left", to, from)
				assert.Zero(t, next.fake.Remaining(), "%s didn't get as far as its tool call and answer", to)
			})
		}
	}
}

// A compacted conversation goes on with Claude: the summary is context,
// not a turn of the model's that would open the history (CA-04, CA-21).
func TestConformanceCompactedConversationOnClaude(t *testing.T) {
	cfg := conformanceConfig(t)
	ws := wires(t, cfg)
	a := ws["anthropic"]
	eng := conformanceEngine(t, cfg, modelFor(t, cfg, a))
	for _, prompt := range []string{"What do my notes say?", "And now?", "Once more."} {
		a.fake.Script(toolTurn()...)
		_, err := collect(t, eng, "s", prompt)
		require.NoError(t, err, "%v", a.fake.Refusals())
	}
	a.fake.Script(wirefake.Reply{Text: "The user read their notes three times; they say hello."})
	_, err := eng.Compact(context.Background(), "s", "", 1)
	require.NoError(t, err, "%v", a.fake.Refusals())

	a.fake.Script(wirefake.Reply{Text: "Still hello."})
	_, err = collect(t, eng, "s", "What did we find?")
	assert.NoError(t, err)
	assert.Empty(t, a.fake.Refusals())
}

// OpenAI's reasoning tokens are inside its output tokens and are counted
// once (CA-122).
func TestConformanceOpenAIReasoningCountedOnce(t *testing.T) {
	cfg := conformanceConfig(t)
	ws := wires(t, cfg)
	o := ws["openai"]
	eng := conformanceEngine(t, cfg, modelFor(t, cfg, o))
	o.fake.Script(wirefake.Reply{Thought: "think", Text: "hi", Usage: wirefake.Usage{Input: 1000, Output: 300, Reasoning: 100}})
	_, err := collect(t, eng, "s", "hello")
	require.NoError(t, err)
	assert.EqualValues(t, 300, eng.Usage("s").Output, "reasoning counted twice")
}

// servedBy answers as the model it's named.
type servedBy struct{ name string }

func (m servedBy) Name() string { return m.name }

func (m servedBy) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{
			Content:       genai.NewContentFromText("hi", genai.RoleModel),
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1000, CandidatesTokenCount: 100},
			TurnComplete:  true,
		}, nil)
	}
}

// failing refuses every call.
type failing struct{ name string }

func (m failing) Name() string { return m.name }

func (m failing) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) { yield(nil, errors.New("overloaded")) }
}

// A call answered by a fallback with no price is unpriced, not charged at
// the primary's price (CA-92).
func TestConformanceUnpricedFallbackIsUnpriced(t *testing.T) {
	cfg := conformanceConfig(t)
	chain := newFallbackModel([]model.LLM{
		&settingsModel{inner: failing{name: "claude-opus-5-5"}, provider: "anthropic"},
		&settingsModel{inner: servedBy{name: "llama-local-7b"}, provider: "openai", configured: "ollama"},
	})
	eng := conformanceEngine(t, cfg, chain)
	_, err := collect(t, eng, "s", "hello")
	require.NoError(t, err)
	u := eng.Usage("s")
	assert.False(t, u.Priced, "a call by an unpriced model was priced")
	assert.Zero(t, u.CostUSD, "a call by an unpriced model was charged %v", u.CostUSD)
}
