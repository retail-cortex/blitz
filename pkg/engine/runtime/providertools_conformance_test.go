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
	"encoding/json"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime/wirefake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/session"
)

var blitzSearch = &wirefake.Search{Query: "blitz agent", Results: []wirefake.Source{{Title: "Blitz", URL: "https://example.com/blitz"}}}

// offersProviderSearch reports whether a recorded request offered the
// provider's own search, and whether it declared Blitz's web_search.
func offersProviderSearch(api string, req map[string]any) (provider, client bool) {
	for _, tl := range req["tools"].([]any) {
		t, _ := tl.(map[string]any)
		switch {
		case t["googleSearch"] != nil, t["type"] == "web_search_20250305", t["type"] == "web_search":
			provider = true
		case t["name"] == "web_search":
			client = true
		}
		for _, d := range asList(t["functionDeclarations"]) {
			if d.(map[string]any)["name"] == "web_search" {
				client = true
			}
		}
	}
	return provider, client
}

// A model searches with its provider's own tool in place of Blitz's
// (CA-80), the search and its citations are stored with the answer
// (CA-82, CA-84), and every model reads it back: its maker in its own
// form, any other as Blitz's web_search call and result.
func TestConformanceProviderSearch(t *testing.T) {
	names := []string{"gemini", "anthropic", "openai"}
	for _, from := range names {
		for _, to := range names {
			t.Run(from+" to "+to, func(t *testing.T) {
				cfg := conformanceConfig(t)
				ws := wires(t, cfg)
				first, next := ws[from], ws[to]
				eng := conformanceEngine(t, cfg, modelFor(t, cfg, first))

				first.fake.Script(wirefake.Reply{Search: blitzSearch, Text: "Blitz is an agent."})
				evs, err := collect(t, eng, "s", "What is Blitz?")
				require.NoError(t, err, "%v", first.fake.Refusals())
				require.Zero(t, first.fake.Remaining())
				provider, client := offersProviderSearch(from, first.fake.Requests()[0])
				assert.True(t, provider, "%s wasn't offered its provider's search", from)
				assert.False(t, client, "%s was offered Blitz's web_search beside its own", from)

				var stored *session.Event
				for _, ev := range evs {
					if len(Citations(&ev.LLMResponse)) > 0 {
						stored = ev
					}
				}
				require.NotNil(t, stored, "no answer kept its citations")
				assert.Equal(t, "https://example.com/blitz", Citations(&stored.LLMResponse)[0].URL)
				calls := 0
				for _, p := range stored.Content.Parts {
					if name, _, _, call, ok := ProviderToolPart(p); ok && call {
						assert.Equal(t, "web_search", name)
						calls++
					}
				}
				assert.Equal(t, 1, calls, "the search wasn't stored as a call")
				requests, _ := stored.CustomMetadata[ProviderToolRequestsKey].(map[string]int)
				assert.Equal(t, 1, requests[ptWebSearch], "the search wasn't counted")

				if to != from {
					require.NoError(t, eng.SetModel(context.Background(), modelFor(t, cfg, next)))
				}
				next.fake.Script(wirefake.Reply{Text: "It's an agent."})
				_, err = collect(t, eng, "s", "Say it again.")
				assert.NoError(t, err)
				assert.Empty(t, next.fake.Refusals(), "%s refused the history %s's search left", to, from)
				reqs := next.fake.Requests()
				body, _ := json.Marshal(reqs[len(reqs)-1])
				switch {
				case to == from && from == "anthropic":
					assert.Contains(t, string(body), `"web_search_tool_result"`, "Claude didn't get its own search back")
				case to == from && from == "gemini":
					assert.NotContains(t, string(body), `"toolCall"`)
				default:
					assert.Contains(t, string(body), `"web_search"`)
					assert.True(t, strings.Contains(string(body), `https://example.com/blitz`), "%s didn't read the search's results", to)
				}
			})
		}
	}
}

// Blitz's rules switch provider tools off as they would the client tool
// (CA-81): the settings, the agent, a deny rule and plan mode.
func TestConformanceProviderToolsOff(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(cfg *config.Config)
	}{
		{"[tools] provider = false", func(cfg *config.Config) { off := false; cfg.Tools.Provider = &off }},
		{"the organization's tools.off", func(cfg *config.Config) { cfg.Org.Policy.Tools.Off = []string{"provider:web_search"} }},
		{"the organization's client tool off", func(cfg *config.Config) { cfg.Org.Policy.Tools.Off = []string{"web_search"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := conformanceConfig(t)
			ws := wires(t, cfg)
			a := ws["anthropic"]
			tc.set(cfg)
			eng := conformanceEngine(t, cfg, modelFor(t, cfg, a))
			a.fake.Script(wirefake.Reply{Search: blitzSearch, Text: "No search."})
			_, err := collect(t, eng, "s", "What is Blitz?")
			require.NoError(t, err, "%v", a.fake.Refusals())
			provider, _ := offersProviderSearch("anthropic", a.fake.Requests()[0])
			assert.False(t, provider)
		})
	}
	t.Run("plan mode keeps search and drops code execution", func(t *testing.T) {
		cfg := conformanceConfig(t)
		ws := wires(t, cfg)
		a := ws["anthropic"]
		eng := conformanceEngine(t, cfg, modelFor(t, cfg, a))
		a.fake.Script(wirefake.Reply{Text: "A plan."})
		err := eng.Execute(context.Background(), "s", "Plan it.", func(*session.Event) error { return nil }, WithPlanOnly())
		require.NoError(t, err, "%v", a.fake.Refusals())
		body, _ := json.Marshal(a.fake.Requests()[0]["tools"])
		assert.Contains(t, string(body), "web_search_20250305")
		assert.NotContains(t, string(body), "code_execution")
	})
}
