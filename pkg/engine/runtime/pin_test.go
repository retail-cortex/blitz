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
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestPinnedSubagentRunsAndIsPricedOnItsModel(t *testing.T) {
	haiku := NewMockLLM("claude-haiku-4-5", textContent("tests look fine"))
	haiku.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1_000_000}
	f := newEngineWith(t, fixtureOpts{opts: []Option{WithAgentModel("qa", haiku)}},
		toolCall("invoke_agent", map[string]any{"agent_name": "qa", "prompt": "review"}),
		textContent("done"))

	_, err := functionResponses(t, f.eng, "s", "get a review")
	require.NoError(t, err)
	require.Equal(t, 1, haiku.Calls(), "pinned model got %d calls, want 1", haiku.Calls())
	require.Equal(t, 2, f.llm.Calls(), "main model got %d calls, want 2 (the sub-agent's call went elsewhere)", f.llm.Calls())
	// The mock reports no model version: the sub-agent's tokens must be
	// priced as its own (pinned) model, not the active agent's.
	want := config.DefaultPricing["claude-haiku-4-5"].InputPerMTok
	u := f.eng.Usage("s")
	require.LessOrEqual(t, abs(u.CostUSD-want), 1e-9, "cost $%v, want $%v (haiku input for 1M tokens)", u.CostUSD, want)
	name, pinned := f.eng.AgentModel("qa")
	require.Equal(t, "claude-haiku-4-5", name, "AgentModel = %s %v", name, pinned)
	require.True(t, pinned, "AgentModel = %s %v", name, pinned)
	name, pinned = f.eng.AgentModel("blitz")
	require.Equal(t, "gemini-3.8-flash", name, "unpinned agent: %s %v", name, pinned)
	require.False(t, pinned, "unpinned agent: %s %v", name, pinned)
}

func TestPinAndUnpinTheActiveAgent(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("from main"))
	pinned := NewMockLLM("claude-sonnet-5", textContent("from pinned"))
	ctx := context.Background()
	require.Error(t, f.eng.PinModel(ctx, "nope", pinned), "pinned an unknown agent")
	require.NoError(t, f.eng.PinModel(ctx, "blitz", pinned))
	require.Equal(t, "claude-sonnet-5", f.eng.ModelName(), "ModelName = %s", f.eng.ModelName())
	_, err := collect(t, f.eng, "s", "hi")
	require.NoError(t, err, "pinned %d, main %d, err", pinned.Calls(), f.llm.Calls())
	require.Equal(t, 1, pinned.Calls(), "pinned %d, main %d, err %v", pinned.Calls(), f.llm.Calls(), err)
	require.Equal(t, 0, f.llm.Calls(), "pinned %d, main %d, err %v", pinned.Calls(), f.llm.Calls(), err)
	require.NoError(t, f.eng.Unpin(ctx, "blitz"))
	_, err = collect(t, f.eng, "s", "hi")
	require.NoError(t, err, "after unpin: main %d calls, model %s, err", f.llm.Calls(), f.eng.ModelName())
	require.Equal(t, 1, f.llm.Calls(), "after unpin: main %d calls, model %s, err %v", f.llm.Calls(), f.eng.ModelName(), err)
	require.Equal(t, "gemini-3.8-flash", f.eng.ModelName(), "after unpin: main %d calls, model %s, err %v", f.llm.Calls(), f.eng.ModelName(), err)
}

func TestNewModelAcceptsAProviderQualifiedName(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LLM.Provider = "gemini"
	cfg.LLM.Anthropic.APIKey = "sk-ant-test"
	m, err := NewModel(context.Background(), cfg, "anthropic/claude-sonnet-5")
	require.NoError(t, err)
	sm, ok := m.(*settingsModel)
	require.True(t, ok, "got %T", m)
	require.Equal(t, "claude-sonnet-5", m.Name())
	capped, ok := sm.inner.(*outputCapModel)
	require.True(t, ok, "the settings wrap the output cap: got %T", sm.inner)
	require.IsType(t, &anthropicModel{}, capped.inner, "which wraps the Anthropic model")
	// The configured default model can name its provider too.
	cfg.Blitz.DefaultModel = "anthropic/claude-haiku-4-5"
	m, err = NewModel(context.Background(), cfg, "")
	require.NoError(t, err, "default_model with provider: %v", m)
	require.Equal(t, "claude-haiku-4-5", m.Name(), "default_model with provider: %v %v", m, err)
}
