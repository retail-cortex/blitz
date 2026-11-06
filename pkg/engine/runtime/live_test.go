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
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime/wirefake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// The paid check (spec_056_conversation_adapters CA-151): the conformance
// grid with a real API in place of each fake whose key is in the
// environment, so the fakes' rules are confirmed by the APIs they stand
// for. It runs only with BLITZ_LIVE=1, and costs money:
//
//	bazel test //pkg/engine/runtime:runtime_test --test_filter=TestLive \
//	  --test_env=BLITZ_LIVE=1 --test_env=GEMINI_API_KEY --test_env=ANTHROPIC_API_KEY \
//	  --test_sharding_strategy=disabled --nocache_test_results
//
// Keys come from the environment only. BLITZ_LIVE_<API>_MODEL picks a
// model; BLITZ_LIVE_OPENAI_BASE_URL points the OpenAI slot at a server
// that speaks its API (Ollama, vLLM), which checks shape, not OpenAI's
// own rules.

// liveAPIs are the APIs to run for real, with their models.
func liveAPIs(t *testing.T) map[string]string {
	t.Helper()
	if os.Getenv("BLITZ_LIVE") != "1" {
		t.Skip("the paid check runs with BLITZ_LIVE=1 (spec_056 CA-151)")
	}
	pick := func(env, def string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		return def
	}
	live := map[string]string{}
	if os.Getenv("GEMINI_API_KEY") != "" {
		live["gemini"] = "gemini/" + pick("BLITZ_LIVE_GEMINI_MODEL", "gemini-4-argon")
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		live["anthropic"] = "anthropic/" + pick("BLITZ_LIVE_ANTHROPIC_MODEL", "claude-sonnet-5-5")
	}
	if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("BLITZ_LIVE_OPENAI_BASE_URL") != "" {
		live["openai"] = "openai/" + pick("BLITZ_LIVE_OPENAI_MODEL", "gpt-5-mini")
	}
	if len(live) == 0 {
		t.Skip("no API key in the environment")
	}
	t.Cleanup(closeConnections)
	return live
}

// closeConnections closes the idle connections to the real APIs, so none
// is left reading when the tests end.
func closeConnections() {
	modelClientsMu.Lock()
	for _, c := range modelClients {
		if st, ok := c.Transport.(*stallTransport); ok {
			if tr, ok := st.base.(*http.Transport); ok {
				tr.CloseIdleConnections()
			}
		}
	}
	modelClientsMu.Unlock()
	http.DefaultClient.CloseIdleConnections()
}

// liveWires is wires with the live APIs real: their wire has no fake.
func liveWires(t *testing.T, cfg *config.Config, live map[string]string) map[string]*wire {
	t.Helper()
	ws := map[string]*wire{}
	if ref, ok := live["gemini"]; ok {
		cfg.LLM.Gemini.APIKey = os.Getenv("GEMINI_API_KEY")
		ws["gemini"] = &wire{name: "gemini", ref: ref}
	} else {
		g := wirefake.Gemini(t)
		t.Setenv("GOOGLE_GEMINI_BASE_URL", g.URL())
		t.Setenv("GEMINI_API_KEY", "")
		t.Setenv("GOOGLE_API_KEY", "")
		cfg.LLM.Gemini.APIKey = "test-key"
		ws["gemini"] = &wire{name: "gemini", ref: "gemini/gemini-3-pro", fake: g}
	}
	if ref, ok := live["anthropic"]; ok {
		cfg.LLM.Anthropic.APIKey = os.Getenv("ANTHROPIC_API_KEY")
		ws["anthropic"] = &wire{name: "anthropic", ref: ref}
	} else {
		a := wirefake.Anthropic(t)
		cfg.LLM.Anthropic.APIKey, cfg.LLM.Anthropic.BaseURL = "test-key", a.URL()
		ws["anthropic"] = &wire{name: "anthropic", ref: "anthropic/claude-opus-5-5", fake: a}
	}
	if ref, ok := live["openai"]; ok {
		cfg.LLM.OpenAI.APIKey = cmpOr(os.Getenv("OPENAI_API_KEY"), "local")
		cfg.LLM.OpenAI.BaseURL = os.Getenv("BLITZ_LIVE_OPENAI_BASE_URL")
		ws["openai"] = &wire{name: "openai", ref: ref}
	} else {
		o := wirefake.OpenAI(t)
		cfg.LLM.OpenAI.APIKey, cfg.LLM.OpenAI.BaseURL = "test-key", o.URL()+"/v1"
		ws["openai"] = &wire{name: "openai", ref: "openai/gpt-5", fake: o}
	}
	return ws
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// liveTurn runs a prompt and reports whether a tool ran and the answer.
func liveTurn(t *testing.T, eng *Engine, prompt string) (toolRan bool, answer string, err error) {
	t.Helper()
	events, err := collect(t, eng, "s", prompt)
	for _, ev := range events {
		if ev.Content == nil || ev.Partial {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p.FunctionResponse != nil {
				toolRan = true
			}
			if p.Text != "" && !p.Thought && ev.Author != "user" {
				answer = p.Text
			}
		}
	}
	return toolRan, answer, err
}

// checkSide checks one side's part in a turn: a fake refused nothing and
// was asked everything scripted; a real API answered.
func checkSide(t *testing.T, w *wire, answer string, err error) {
	t.Helper()
	require.NoError(t, err, "%s", w.ref)
	if w.fake != nil {
		assert.Empty(t, w.fake.Refusals(), "the %s fake refused", w.name)
		assert.Zero(t, w.fake.Remaining(), "the %s fake wasn't asked everything", w.name)
		return
	}
	assert.NotEmpty(t, strings.TrimSpace(answer), "%s answered nothing", w.ref)
}

const (
	liveAsk   = "Use the read_file tool to read notes.txt, then tell me in one short sentence what it says."
	liveAgain = "Read notes.txt again with read_file and tell me in one short sentence what it says."
)

// CA-01–CA-03, CA-20–CA-23 against the real APIs: each direction with a
// real API on at least one side.
func TestLiveSwitchAtTurnEnd(t *testing.T) {
	live := liveAPIs(t)
	names := []string{"gemini", "anthropic", "openai"}
	for _, from := range names {
		for _, to := range names {
			_, a := live[from]
			_, b := live[to]
			if from == to || !a && !b {
				continue
			}
			t.Run(from+" to "+to, func(t *testing.T) {
				cfg := conformanceConfig(t)
				ws := liveWires(t, cfg, live)
				first, next := ws[from], ws[to]
				eng := conformanceEngine(t, cfg, modelFor(t, cfg, first))
				if first.fake != nil {
					first.fake.Script(toolTurn()...)
				}
				ran, answer, err := liveTurn(t, eng, liveAsk)
				checkSide(t, first, answer, err)
				t.Logf("%s: tool ran %v, answered %q", first.ref, ran, answer)

				require.NoError(t, eng.SetModel(context.Background(), modelFor(t, cfg, next)))
				if next.fake != nil {
					next.fake.Script(toolTurn()...)
				}
				ran, answer, err = liveTurn(t, eng, liveAgain)
				checkSide(t, next, answer, err)
				t.Logf("%s: tool ran %v, answered %q", next.ref, ran, answer)
			})
		}
	}
}

// failsAfter answers its first call and fails every later one, as a
// provider that goes down mid-turn.
type failsAfter struct {
	model.LLM
	mu    sync.Mutex
	calls int
}

func (f *failsAfter) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	if n > 1 {
		return func(yield func(*model.LLMResponse, error) bool) {
			yield(nil, errors.New("503 Service Unavailable (the live check's outage)"))
		}
	}
	return f.LLM.GenerateContent(ctx, req, stream)
}

// CA-91 against the real APIs: a fallback goes on from the tool call
// another model made.
func TestLiveFallbackMidLoop(t *testing.T) {
	live := liveAPIs(t)
	names := []string{"gemini", "anthropic", "openai"}
	for _, from := range names {
		for _, to := range names {
			_, a := live[from]
			_, b := live[to]
			if from == to || !a && !b {
				continue
			}
			t.Run(from+" to "+to, func(t *testing.T) {
				cfg := conformanceConfig(t)
				ws := liveWires(t, cfg, live)
				primary, backup := ws[from], ws[to]
				first := modelFor(t, cfg, primary)
				if primary.fake != nil {
					primary.fake.Script(toolTurn()[0], wirefake.Reply{Status: 503})
				} else {
					first = &failsAfter{LLM: first}
				}
				if backup.fake != nil {
					backup.fake.Script(wirefake.Reply{Thought: "The tool said hello.", Text: "The notes say hello."})
				}
				eng := conformanceEngine(t, cfg, newFallbackModel([]model.LLM{first, modelFor(t, cfg, backup)}))
				ran, answer, err := liveTurn(t, eng, liveAsk)
				require.NoError(t, err)
				if primary.fake != nil {
					assert.Empty(t, primary.fake.Refusals())
				}
				checkSide(t, backup, answer, err)
				t.Logf("%s then %s: tool ran %v, answered %q", primary.ref, backup.ref, ran, answer)
			})
		}
	}
}

// CA-04, CA-21 against Claude: a compacted conversation goes on.
func TestLiveCompactedConversationOnClaude(t *testing.T) {
	live := liveAPIs(t)
	if _, ok := live["anthropic"]; !ok {
		t.Skip("no ANTHROPIC_API_KEY")
	}
	cfg := conformanceConfig(t)
	ws := liveWires(t, cfg, live)
	eng := conformanceEngine(t, cfg, modelFor(t, cfg, ws["anthropic"]))
	_, answer, err := liveTurn(t, eng, liveAsk)
	checkSide(t, ws["anthropic"], answer, err)
	_, answer, err = liveTurn(t, eng, "Thanks. In one word, what was the file's name?")
	checkSide(t, ws["anthropic"], answer, err)
	_, err = eng.Compact(context.Background(), "s", "", 1)
	require.NoError(t, err)
	_, answer, err = liveTurn(t, eng, "And what did it say, in one short sentence?")
	checkSide(t, ws["anthropic"], answer, err)
}

// CA-25: whether Gemini takes $ref in parametersJsonSchema, asked directly.
func TestLiveGeminiSchemaRef(t *testing.T) {
	live := liveAPIs(t)
	ref, ok := live["gemini"]
	if !ok {
		t.Skip("no GEMINI_API_KEY")
	}
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: os.Getenv("GEMINI_API_KEY"), Backend: genai.BackendGeminiAPI})
	require.NoError(t, err)
	schema := map[string]any{
		"type":       "object",
		"$defs":      map[string]any{"city": map[string]any{"type": "string", "description": "A city name"}},
		"properties": map[string]any{"city": map[string]any{"$ref": "#/$defs/city"}},
		"required":   []any{"city"},
	}
	_, err = client.Models.GenerateContent(ctx, strings.TrimPrefix(ref, "gemini/"),
		genai.Text("What's the weather in Paris? Use the tool."),
		&genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name: "weather", Description: "The weather in a city.", ParametersJsonSchema: schema,
		}}}}})
	t.Logf("Gemini %s with $ref: error %v", ref, err)
}
