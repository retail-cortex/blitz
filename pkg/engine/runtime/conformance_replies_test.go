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
	"sync"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/runtime/wirefake"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
)

// Replies, errors and streaming in the conformance suite
// (spec_056_conversation_adapters §6, §7).

// streamedEngine is conformanceEngine with streaming on, as the service
// runs it, and the engine's notices kept.
func streamedEngine(t *testing.T, cfg *config.Config, llm model.LLM, streaming bool) (*Engine, *notices) {
	t.Helper()
	agentReg, err := agents.NewRegistry()
	require.NoError(t, err)
	skillProv, err := skills.NewProvider()
	require.NoError(t, err)
	toolReg, err := tools.NewRegistry(cfg, agentReg, skillProv)
	require.NoError(t, err)
	t.Cleanup(func() { toolReg.Close() })
	n := &notices{}
	eng, err := NewEngine(context.Background(), cfg, agentReg, skillProv, toolReg, llm, WithStreaming(streaming), WithNotice(n.add))
	require.NoError(t, err)
	return eng, n
}

type notices struct {
	mu   sync.Mutex
	said []string
}

func (n *notices) add(s string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.said = append(n.said, s)
}

func (n *notices) all() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return strings.Join(n.said, "\n")
}

var wireNames = []string{"gemini", "anthropic", "openai"}

// The switch and the mid-loop fallback hold with the reply streamed, as
// the service runs them (CA-50, CA-54).
func TestConformanceStreamed(t *testing.T) {
	// The streaming path itself: a call offering OpenAI's own web search
	// isn't streamed (CA-50), so provider tools are off here; their own
	// grid is providertools_conformance_test.go.
	off := false
	for _, from := range wireNames {
		for _, to := range wireNames {
			if from == to {
				continue
			}
			t.Run("switch "+from+" to "+to, func(t *testing.T) {
				cfg := conformanceConfig(t)
				cfg.Tools.Provider = &off
				ws := wires(t, cfg)
				first, next := ws[from], ws[to]
				eng, _ := streamedEngine(t, cfg, modelFor(t, cfg, first), true)
				first.fake.Script(toolTurn()...)
				_, err := collect(t, eng, "s", "What do my notes say?")
				require.NoError(t, err, "%v", first.fake.Refusals())
				require.NoError(t, eng.SetModel(context.Background(), modelFor(t, cfg, next)))
				next.fake.Script(toolTurn()...)
				evs, err := collect(t, eng, "s", "Read them again, please.")
				assert.NoError(t, err)
				assert.Empty(t, next.fake.Refusals())
				assert.Zero(t, first.fake.Remaining()+next.fake.Remaining())
				assert.True(t, streamedAt(next.fake), "%s wasn't asked to stream", to)
				assert.Contains(t, finalText(evs), "The notes say hello.")
			})
			t.Run("fallback "+from+" to "+to, func(t *testing.T) {
				cfg := conformanceConfig(t)
				ws := wires(t, cfg)
				primary, backup := ws[from], ws[to]
				eng, _ := streamedEngine(t, cfg, newFallbackModel([]model.LLM{modelFor(t, cfg, primary), modelFor(t, cfg, backup)}), true)
				primary.fake.Script(toolTurn()[0], wirefake.Reply{Status: 503})
				backup.fake.Script(wirefake.Reply{Thought: "The tool said hello.", Text: "The notes say hello."})
				evs, err := collect(t, eng, "s", "What do my notes say?")
				assert.NoError(t, err)
				assert.Empty(t, backup.fake.Refusals())
				assert.Zero(t, primary.fake.Remaining()+backup.fake.Remaining())
				assert.Contains(t, finalText(evs), "The notes say hello.")
			})
		}
	}
}

// streamedAt reports whether f answered as a stream.
func streamedAt(f *wirefake.Fake) bool { return f.Streamed() > 0 }

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// finalText is the text of a turn's whole (not partial) answers.
func finalText(evs []*session.Event) string {
	var b strings.Builder
	for _, ev := range evs {
		if ev == nil || ev.Partial || ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p != nil && !p.Thought {
				b.WriteString(p.Text)
			}
		}
	}
	return b.String()
}

// A reply that ends badly ends the turn with its reason: a refusal, a
// safety block, a recitation; a tool call that can't be read is asked for
// again once, then ends it (CA-31).
func TestConformanceFinishReasons(t *testing.T) {
	tests := []struct {
		name    string
		wire    string
		replies []wirefake.Reply
		want    error
		retried bool
	}{
		{name: "Claude refuses", wire: "anthropic", replies: []wirefake.Reply{{Text: "I can't help with that.", Finish: "refusal"}}, want: api.ErrRefused},
		{name: "Gemini's safety filters", wire: "gemini", replies: []wirefake.Reply{{Text: "x", Finish: "SAFETY"}}, want: api.ErrBlocked},
		{name: "Gemini recites", wire: "gemini", replies: []wirefake.Reply{{Text: "x", Finish: "RECITATION"}}, want: api.ErrRecitation},
		{name: "OpenAI's content filter", wire: "openai", replies: []wirefake.Reply{{Text: "x", Finish: "content_filter"}}, want: api.ErrBlocked},
		{name: "Claude stops at max_tokens", wire: "anthropic", replies: []wirefake.Reply{{Text: "cut", Finish: "max_tokens"}}, want: api.ErrOutputLimit},
		{name: "Gemini's malformed call, then a good answer", wire: "gemini", replies: []wirefake.Reply{{Finish: "MALFORMED_FUNCTION_CALL"}, {Text: "done"}}, retried: true},
		{name: "Gemini's malformed call twice", wire: "gemini", replies: []wirefake.Reply{{Finish: "MALFORMED_FUNCTION_CALL"}, {Finish: "MALFORMED_FUNCTION_CALL"}}, want: api.ErrMalformedToolCall, retried: true},
	}
	for _, tt := range tests {
		for _, streaming := range []bool{false, true} {
			t.Run(tt.name+map[bool]string{false: "", true: ", streamed"}[streaming], func(t *testing.T) {
				cfg := conformanceConfig(t)
				w := wires(t, cfg)[tt.wire]
				eng, _ := streamedEngine(t, cfg, modelFor(t, cfg, w), streaming)
				w.fake.Script(tt.replies...)
				_, err := collect(t, eng, "s", "hello")
				if tt.want == nil {
					assert.NoError(t, err)
				} else {
					assert.ErrorIs(t, err, tt.want)
				}
				assert.Empty(t, w.fake.Refusals())
				assert.Zero(t, w.fake.Remaining())
				if tt.retried {
					reqs := w.fake.Requests()
					require.Len(t, reqs, 2)
					assert.Contains(t, mustJSON(t, reqs[1]), "couldn’t be read", "the second call says why")
				}
			})
		}
	}
}

// Parts of a reply Blitz has no form for (a server tool's blocks, an
// output item it doesn't know) are kept with the answer and noted, not an
// error (CA-30).
func TestConformanceUnshownParts(t *testing.T) {
	tests := []struct {
		wire  string
		extra map[string]any
	}{
		// Parts Blitz has no form for (a provider's own search is one it has:
		// CA-82, providertools_conformance_test.go).
		{"anthropic", map[string]any{"type": "container_upload", "file_id": "file_1"}},
		{"openai", map[string]any{"type": "computer_call", "id": "cu_1", "call_id": "call_9", "status": "completed", "action": map[string]any{"type": "screenshot"}}},
	}
	for _, tt := range tests {
		for _, streaming := range []bool{false, true} {
			t.Run(tt.wire+map[bool]string{false: "", true: ", streamed"}[streaming], func(t *testing.T) {
				cfg := conformanceConfig(t)
				w := wires(t, cfg)[tt.wire]
				eng, said := streamedEngine(t, cfg, modelFor(t, cfg, w), streaming)
				w.fake.Script(wirefake.Reply{Text: "Found it.", Extra: []map[string]any{tt.extra}})
				evs, err := collect(t, eng, "s", "search")
				require.NoError(t, err)
				assert.Contains(t, finalText(evs), "Found it.")
				kept := false
				for _, ev := range evs {
					if ev != nil && !ev.Partial && len(unshownTypes(&ev.LLMResponse)) > 0 {
						kept = true
						assert.Equal(t, []string{tt.extra["type"].(string)}, unshownTypes(&ev.LLMResponse))
					}
				}
				assert.True(t, kept, "the part wasn't kept with the answer")
				assert.Contains(t, said.all(), tt.extra["type"].(string), "the person isn't told")
			})
		}
	}
}

// A conversation longer than the model reads is summarised to fit, without
// asking, and the same model tried again (CA-111); still too long, the
// turn ends with CONTEXT_LENGTH.
func TestConformanceContextTooLong(t *testing.T) {
	for _, wire := range wireNames {
		t.Run(wire, func(t *testing.T) {
			cfg := conformanceConfig(t)
			w := wires(t, cfg)[wire]
			eng, said := streamedEngine(t, cfg, modelFor(t, cfg, w), false)
			for _, prompt := range []string{"What do my notes say?", "And now?"} {
				w.fake.Script(toolTurn()...)
				_, err := collect(t, eng, "s", prompt)
				require.NoError(t, err, "%v", w.fake.Refusals())
			}
			w.fake.Script(wirefake.Reply{TooLong: true}, wirefake.Reply{Text: "The notes were read twice; they say hello."}, wirefake.Reply{Text: "Still hello."})
			evs, err := collect(t, eng, "s", "Once more?")
			require.NoError(t, err)
			assert.Contains(t, finalText(evs), "Still hello.")
			assert.Contains(t, said.all(), "summarised")
			assert.Zero(t, w.fake.Remaining())

			w.fake.Script(wirefake.Reply{TooLong: true}, wirefake.Reply{Text: "A summary."}, wirefake.Reply{TooLong: true})
			_, err = collect(t, eng, "s", "And again?")
			assert.ErrorIs(t, err, api.ErrContextLength)
		})
	}
}
