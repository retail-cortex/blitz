package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// A run with its own model answers on it and is priced by it; the engine's
// model is untouched for the next run.
func TestWithModelRunsOnlyThatRunOnTheModel(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("from main"))
	sonnet := NewMockLLM("claude-sonnet-5", textContent("from sonnet"))
	sonnet.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 1_000_000}

	if _, err := functionResponses(t, f.eng, "s", "hi", WithModel(sonnet)); err != nil {
		t.Fatal(err)
	}
	if sonnet.Calls() != 1 || f.llm.Calls() != 0 {
		t.Fatalf("override %d calls, main %d", sonnet.Calls(), f.llm.Calls())
	}
	want := config.DefaultPricingAt(time.Now())["claude-sonnet-5"].InputPerMTok
	if u := f.eng.Usage("s"); abs(u.CostUSD-want) > 1e-9 {
		t.Fatalf("cost $%v, want $%v (sonnet input for 1M tokens)", u.CostUSD, want)
	}
	if f.eng.ModelName() == "claude-sonnet-5" {
		t.Fatal("the override changed the engine's model")
	}
	if _, err := collect(t, f.eng, "s", "again"); err != nil || f.llm.Calls() != 1 || sonnet.Calls() != 1 {
		t.Fatalf("next run: main %d, override %d, err %v", f.llm.Calls(), sonnet.Calls(), err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if len(authors) == 0 || authors[len(authors)-1] != "qa" {
		t.Fatalf("authors %v, want qa", authors)
	}
	if f.eng.ActiveAgent() != "blitz" {
		t.Fatalf("active agent changed to %s", f.eng.ActiveAgent())
	}
	if err := f.eng.Execute(context.Background(), "s", "x", nil, WithAgent("nobody")); err == nil {
		t.Fatal("ran as an unknown agent")
	}
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

	if _, err := functionResponses(t, f.eng, "s", "go", WithModel(sonnet)); err != nil {
		t.Fatal(err)
	}
	if pinnedRoot.Calls() != 0 {
		t.Fatalf("pinned root agent ran on its pin (%d calls), not the override", pinnedRoot.Calls())
	}
	if haiku.Calls() != 1 {
		t.Fatalf("pinned sub-agent got %d calls, want 1", haiku.Calls())
	}
	// Root: invoke qa, invoke web-retriever, final answer; web-retriever: one answer.
	if sonnet.Calls() != 4 {
		t.Fatalf("override got %d calls, want 4", sonnet.Calls())
	}
}
