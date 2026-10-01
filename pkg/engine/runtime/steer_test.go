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
	"fmt"
	"iter"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// hookedLLM runs onCall before each model call, e.g. to simulate the user
// typing while the model works.
type hookedLLM struct {
	*MockLLM
	onCall func(n int)
}

func (h *hookedLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	if h.onCall != nil {
		h.onCall(h.Calls() + 1)
	}
	return h.MockLLM.GenerateContent(ctx, req, stream)
}

// steerIn returns the message_from_user values in contents' tool results.
func steerIn(cs []*genai.Content) []string {
	var out []string
	for _, c := range cs {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				if v, ok := p.FunctionResponse.Response[SteerKey]; ok {
					out = append(out, fmt.Sprint(v))
				}
			}
		}
	}
	return out
}

func steered(t *testing.T, f engineFixture, onCall func(n int)) {
	t.Helper()
	f.eng.OpenSteers("s") // the workspace opens it as the turn starts
	require.NoError(t, f.eng.SetModel(context.Background(), &hookedLLM{MockLLM: f.llm, onCall: onCall}))
}

func TestSteerRidesOnTheNextToolResult(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{},
		toolCall("list_files", map[string]any{}),
		textContent("done, using tabs"),
		textContent("second turn"))
	steered(t, f, func(n int) {
		if n == 1 { // the user types while the first call is in flight
			f.eng.Steer("s", "use tabs, not spaces")
		}
	})

	_, err := collect(t, f.eng, "s", "reformat the file")
	require.NoError(t, err)
	reqs := f.llm.Requests
	require.Len(t, reqs, 2, "want 2 model calls, got %d", len(reqs))
	require.Equal(t, []string{"use tabs, not spaces"}, steerIn(reqs[1].Contents), "the second call's tool results carry the steer")
	left := f.eng.TakeSteers("s")
	require.Len(t, left, 0, "delivered steer still queued: %v", left)

	// It is part of the history: the next turn sees it too.
	_, err = collect(t, f.eng, "s", "anything else?")
	require.NoError(t, err)
	require.Len(t, steerIn(f.llm.Requests[2].Contents), 1, "steer missing from history on the next turn")
	got, err := f.eng.sessions.Get(context.Background(), &session.GetRequest{AppName: appName, UserID: "user", SessionID: "s"})
	require.NoError(t, err)
	var recorded []*genai.Content
	for ev := range got.Session.Events().All() {
		if ev.Content != nil {
			recorded = append(recorded, ev.Content)
		}
	}
	require.Len(t, steerIn(recorded), 1, "steer not recorded in the session events")
}

func TestSteerKeepsAFailedToolsError(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{},
		toolCall("read_file", map[string]any{"path": "missing.txt"}),
		textContent("ok"))
	steered(t, f, func(n int) {
		if n == 1 {
			f.eng.Steer("s", "try README instead")
		}
	})
	_, err := collect(t, f.eng, "s", "read it")
	require.NoError(t, err)
	for _, c := range f.llm.Requests[1].Contents {
		for _, p := range c.Parts {
			if r := p.FunctionResponse; r != nil {
				require.Equal(t, "try README instead", r.Response[SteerKey], "tool result = %v", r.Response)
				require.NotNil(t, r.Response["error"], "tool result = %v", r.Response)
				require.NotEqual(t, "", r.Response["error"], "tool result = %v", r.Response)
				return
			}
		}
	}
	t.Fatal("no tool result in call 2")
}

func TestSteerWithoutFurtherToolCallsIsLeftForTheCaller(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("all done"))
	steered(t, f, func(int) { f.eng.Steer("s", "one more thing") })
	_, err := collect(t, f.eng, "s", "go")
	require.NoError(t, err)
	left := f.eng.TakeSteers("s")
	require.Len(t, left, 1, "leftover = %v", left)
	require.Equal(t, "one more thing", left[0], "leftover = %v", left)
}

func TestSteerIsScopedToItsSession(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, toolCall("list_files", map[string]any{}), textContent("a"))
	f.eng.OpenSteers("other")
	require.NoError(t, f.eng.Steer("other", "not for you"))
	_, err := collect(t, f.eng, "s", "go")
	require.NoError(t, err)
	got := steerIn(f.llm.Requests[1].Contents)
	require.Len(t, got, 0, "steer delivered to another session: %v", got)
	left := f.eng.TakeSteers("other")
	require.Len(t, left, 1, "other session's steer lost: %v", left)
}

func TestSubagentToolsDoNotTakeTheParentsSteer(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{})
	f.eng.OpenSteers("s")
	require.NoError(t, f.eng.Steer("s", "for the main agent"))
	// Run a sub-agent inside a run of session "s"; its tool call must not
	// consume the message.
	sub := NewMockLLM("sub", toolCall("list_files", map[string]any{}), textContent("sub done"))
	require.NoError(t, f.eng.SetModel(context.Background(), sub))
	ctx := context.WithValue(context.Background(), runStateKey{}, &runState{sessionID: "s"})
	_, err := f.eng.InvokeSubagent(ctx, "qa", "check")
	require.NoError(t, err)
	left := f.eng.TakeSteers("s")
	require.Len(t, left, 1, "sub-agent took the parent's steer: left %v", left)
	require.NotContains(t, fmt.Sprint(steerIn(sub.Requests[len(sub.Requests)-1].Contents)), "main agent", "steer reached the sub-agent")
}

// A session takes steer messages only while its turn runs: before it and
// once it collected the unread ones, they're refused (BL-SVC-10).
func TestSteerOnlyWhileTheTurnTakesThem(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{})
	assert.ErrorIs(t, f.eng.Steer("s", "early"), api.ErrSteerTooLate)
	f.eng.OpenSteers("s")
	require.NoError(t, f.eng.Steer("s", "in time"))
	assert.Equal(t, []string{"in time"}, f.eng.CloseSteers("s"))
	assert.ErrorIs(t, f.eng.Steer("s", "late"), api.ErrSteerTooLate)
	assert.Empty(t, f.eng.CloseSteers("s"))
}

// A message for no session goes to "default", which takes none outside a
// turn.
func TestSteerDefaultSession(t *testing.T) {
	f := newEngine(t)
	assert.ErrorIs(t, f.eng.Steer("", "hi"), api.ErrSteerTooLate)
}
