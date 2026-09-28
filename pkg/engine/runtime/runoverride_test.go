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
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// A run with its own model answers on it and is priced by it; the engine's
// model is untouched for the next run.
func TestWithModelRunsOnlyThatRunOnTheModel(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("from main"))
	sonnet := NewMockLLM("claude-sonnet-5", textContent("from sonnet"))
	sonnet.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1_000_000}

	_, err := functionResponses(t, f.eng, "s", "hi", WithModel(sonnet))
	require.NoError(t, err)
	require.Equal(t, 1, sonnet.Calls(), "override %d calls, main %d", sonnet.Calls(), f.llm.Calls())
	require.Equal(t, 0, f.llm.Calls(), "override %d calls, main %d", sonnet.Calls(), f.llm.Calls())
	want := config.DefaultPricingAt(time.Now())["claude-sonnet-5"].InputPerMTok
	u := f.eng.Usage("s")
	require.LessOrEqual(t, abs(u.CostUSD-want), 1e-9, "cost $%v, want $%v (sonnet input for 1M tokens)", u.CostUSD, want)
	require.NotEqual(t, "claude-sonnet-5", f.eng.ModelName(), "the override changed the engine's model")
	_, err = collect(t, f.eng, "s", "again")
	require.NoError(t, err, "next run: main %d, override %d, err", f.llm.Calls(), sonnet.Calls())
	require.Equal(t, 1, f.llm.Calls(), "next run: main %d, override %d, err %v", f.llm.Calls(), sonnet.Calls(), err)
	require.Equal(t, 1, sonnet.Calls(), "next run: main %d, override %d, err %v", f.llm.Calls(), sonnet.Calls(), err)
}

// A run with its own agent answers as that agent; the active agent stays.
func TestWithAgentRunsAsThatAgent(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("qa here"))
	var authors []string
	err := f.eng.Execute(context.Background(), "s", "check", func(ev *session.Event) error {
		if ev.Content != nil && ev.Author != "user" {
			authors = append(authors, ev.Author)
		}
		return nil
	}, WithAgent("qa"))
	require.NoError(t, err)
	require.NotEqual(t, 0, len(authors), "authors %v, want qa", authors)
	require.Equal(t, "qa", authors[len(authors)-1], "authors %v, want qa", authors)
	require.Equal(t, "blitz", f.eng.ActiveAgent(), "active agent changed to %s", f.eng.ActiveAgent())
	require.Error(t, f.eng.Execute(context.Background(), "s", "x", nil, WithAgent("nobody")), "ran as an unknown agent")
}

// The override runs the root agent even when it is pinned, and unpinned
// sub-agents; a pinned sub-agent keeps its own model.
func TestWithModelKeepsOtherAgentsPins(t *testing.T) {
	haiku := NewMockLLM("claude-haiku-4-5", textContent("qa review"))
	pinnedRoot := NewMockLLM("pinned-root")
	f := newEngineWith(t, fixtureOpts{opts: []Option{WithAgentModel("qa", haiku), WithAgentModel("blitz", pinnedRoot)}})
	sonnet := NewMockLLM("claude-sonnet-5",
		toolCall("invoke_agent", map[string]any{"agent_name": "qa", "prompt": "review"}),
		toolCall("invoke_agent", map[string]any{"agent_name": "web-retriever", "prompt": "look"}),
		textContent("retriever answer"),
		textContent("done"))

	_, err := functionResponses(t, f.eng, "s", "go", WithModel(sonnet))
	require.NoError(t, err)
	require.Equal(t, 0, pinnedRoot.Calls(), "pinned root agent ran on its pin (%d calls), not the override", pinnedRoot.Calls())
	require.Equal(t, 1, haiku.Calls(), "pinned sub-agent got %d calls, want 1", haiku.Calls())
	// Root: invoke qa, invoke web-retriever, final answer; web-retriever: one answer.
	require.Equal(t, 4, sonnet.Calls(), "override got %d calls, want 4", sonnet.Calls())
}
