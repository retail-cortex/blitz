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

package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// A saved session's turns come back with the graph of the one asked for:
// its prompt, its model calls, the tools they called and what those
// returned (spec_066 RSN-01).
func TestWorkspaceReasoning(t *testing.T) {
	w, _ := openTestWith(t, nil,
		toolCall("list_files", map[string]any{"directory": "."}), text("Nothing much."),
		text("Hello again."))
	s := newSession(t, w)
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "what's here?"}, func(api.Event) {})
	require.NoError(t, err)
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "hi"}, func(api.Event) {})
	require.NoError(t, err)

	r, err := w.Reasoning(s.ID, 0)
	require.NoError(t, err)
	require.Len(t, r.Turns, 2)
	assert.Equal(t, "what's here?", r.Turns[0].Prompt)
	assert.Equal(t, 2, r.Turns[0].Calls)
	assert.Equal(t, 0, r.Turn)
	assert.Equal(t, []string{"turn", "call", "tool", "call", "text"}, rzKinds(r.Nodes))
	assert.Equal(t, "list_files", r.Nodes[2].Tool)
	assert.Equal(t, api.NodeOK, r.Nodes[2].Status, "%v", r.Nodes[2].Result)
	assert.NotNil(t, r.Nodes[2].Result)
	assert.Equal(t, r.Nodes[1].ID, r.Nodes[2].Parent, "the tool hangs from the call that chose it")

	last, err := w.Reasoning(s.ID, -1)
	require.NoError(t, err)
	assert.Equal(t, 1, last.Turn, "a negative turn is the last")
	assert.Equal(t, "Hello again.", last.Nodes[len(last.Nodes)-1].Text)

	none, err := w.Reasoning("session-20990101-000000-none", -1)
	require.NoError(t, err)
	assert.Empty(t, none.Turns)
	assert.Equal(t, -1, none.Turn)
	_, err = w.Reasoning("../x", 0)
	assert.Error(t, err)
}

func rzKinds(nodes []api.ReasoningNode) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Kind
	}
	return out
}

var rzT0 = time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)

func rzAt(s int) time.Time { return rzT0.Add(time.Duration(s) * time.Second) }

func rzPrompt(s int, text string) *adksession.Event {
	ev := &adksession.Event{InvocationID: "inv"}
	ev.Author, ev.Timestamp = "user", rzAt(s)
	ev.Content = genai.NewContentFromText(text, genai.RoleUser)
	return ev
}

func rzAnswer(s int, author string, thoughts int32, parts ...*genai.Part) *adksession.Event {
	ev := &adksession.Event{InvocationID: "inv"}
	ev.Author, ev.Timestamp = author, rzAt(s)
	ev.LLMResponse = model.LLMResponse{
		Content:        &genai.Content{Role: genai.RoleModel, Parts: parts},
		UsageMetadata:  &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 20, ThoughtsTokenCount: thoughts},
		CustomMetadata: map[string]any{runtime.OriginKey: map[string]any{"api": "gemini", "provider": "google", "model": "gemini-3-pro"}},
	}
	return ev
}

func rzThought(s string) *genai.Part { return &genai.Part{Text: s, Thought: true} }

func rzCall(id, name string, args map[string]any) *genai.Part {
	return &genai.Part{FunctionCall: &genai.FunctionCall{ID: id, Name: name, Args: args}}
}

func rzResponse(s int, author, id, name string, res map[string]any) *adksession.Event {
	ev := &adksession.Event{InvocationID: "inv"}
	ev.Author, ev.Timestamp = author, rzAt(s)
	ev.Content = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: id, Name: name, Response: res}}}}
	return ev
}

func rzPriced(_ string, um *genai.GenerateContentResponseUsageMetadata) api.Usage {
	u := tokensOnly(um)
	u.Priced, u.Unpriced, u.CostUSD = true, 0, 0.01
	return u
}

// Each case is a turn's events and what its graph holds.
func TestBuildReasoning(t *testing.T) {
	for _, tc := range []struct {
		name  string
		evs   []*adksession.Event
		subs  []*adksession.Event
		check func(t *testing.T, r api.Reasoning)
	}{
		{
			name: "reasoning split at its headings, under its call",
			evs: []*adksession.Event{rzPrompt(0, "fix it"), rzAnswer(3, "blitz", 40,
				rzThought("**Reading the code**\nThe uploader has no retry.\n**Choosing a fix**\nWrap Do in a loop."),
				&genai.Part{Text: "Done."})},
			check: func(t *testing.T, r api.Reasoning) {
				assert.Equal(t, []string{"turn", "call", "thought", "thought", "text"}, rzKinds(r.Nodes))
				assert.Equal(t, "Reading the code", r.Nodes[2].Title)
				assert.Equal(t, "The uploader has no retry.", r.Nodes[2].Text)
				assert.Equal(t, "Choosing a fix", r.Nodes[3].Title)
				assert.Equal(t, "gemini-3-pro", r.Nodes[1].Model)
				assert.Equal(t, "google", r.Nodes[1].Provider)
				assert.Equal(t, 3*time.Second, r.Nodes[1].Duration)
				assert.Equal(t, int64(40), r.Nodes[1].Usage.Reasoning)
				assert.False(t, r.Nodes[1].Hidden)
				assert.Equal(t, 2, r.Turns[0].Thoughts)
				assert.InDelta(t, 0.01, r.Nodes[0].Usage.CostUSD, 1e-9, "the turn sums its calls")
			},
		},
		{
			name: "paragraphs without headings are its steps",
			evs:  []*adksession.Event{rzPrompt(0, "q"), rzAnswer(1, "blitz", 0, rzThought("First I look. Then more.\n\nThen I decide."))},
			check: func(t *testing.T, r api.Reasoning) {
				require.Equal(t, []string{"turn", "call", "thought", "thought"}, rzKinds(r.Nodes))
				assert.Equal(t, "First I look.", r.Nodes[2].Title)
				assert.Equal(t, "Then I decide.", r.Nodes[3].Title)
			},
		},
		{
			name: "long reasoning is grouped to a readable number of steps",
			evs:  []*adksession.Event{rzPrompt(0, "q"), rzAnswer(1, "blitz", 0, rzThought(strings.Repeat("A step.\n\n", 30)))},
			check: func(t *testing.T, r api.Reasoning) {
				assert.Len(t, r.Nodes, 2+maxThoughtSteps)
			},
		},
		{
			name: "reasoning the provider keeps to itself is marked hidden",
			evs:  []*adksession.Event{rzPrompt(0, "q"), rzAnswer(1, "blitz", 512, &genai.Part{Text: "42"})},
			check: func(t *testing.T, r api.Reasoning) {
				assert.True(t, r.Nodes[1].Hidden)
				assert.Equal(t, 0, r.Turns[0].Thoughts)
			},
		},
		{
			name: "a failed tool, and one still waiting",
			evs: []*adksession.Event{rzPrompt(0, "q"),
				rzAnswer(1, "blitz", 0, rzCall("a", "read_file", map[string]any{"path": "x"}), rzCall("b", "run_shell", map[string]any{"command": "make"})),
				rzResponse(2, "blitz", "a", "read_file", map[string]any{"error": "no such file"})},
			check: func(t *testing.T, r api.Reasoning) {
				assert.Equal(t, api.NodeFailed, r.Nodes[2].Status)
				assert.Equal(t, time.Second, r.Nodes[2].Duration)
				assert.Equal(t, api.NodeRunning, r.Nodes[3].Status)
				assert.Equal(t, api.NodeRunning, r.Nodes[0].Status, "the turn is still going")
			},
		},
		{
			name: "a sub-agent's calls nest under it, its cost in the turn's",
			evs: []*adksession.Event{rzPrompt(0, "review"),
				rzAnswer(1, "blitz", 0, rzCall("s", "invoke_agent", map[string]any{"agent_name": "reviewer", "prompt": "look"})),
				rzResponse(9, "blitz", "s", "invoke_agent", map[string]any{"response": "fine"}),
				rzAnswer(10, "blitz", 0, &genai.Part{Text: "It's fine."})},
			subs: []*adksession.Event{
				rzAnswer(3, "reviewer", 0, rzThought("Checking the diff."), rzCall("r", "git_diff", nil)),
				rzResponse(4, "reviewer", "r", "git_diff", map[string]any{"diff": ""}),
				rzAnswer(5, "reviewer", 0, &genai.Part{Text: "fine"}),
				rzAnswer(20, "reviewer", 0, &genai.Part{Text: "another run, later"})},
			check: func(t *testing.T, r api.Reasoning) {
				assert.Equal(t, []string{"turn", "call", "agent", "call", "thought", "tool", "call", "text", "call", "text"}, rzKinds(r.Nodes))
				agent := r.Nodes[2]
				assert.Equal(t, "reviewer", agent.Title)
				assert.Equal(t, agent.ID, r.Nodes[3].Parent)
				assert.Equal(t, 2, agent.Usage.Calls)
				assert.Equal(t, 4, r.Nodes[0].Usage.Calls, "two of its own, two of the sub-agent's")
				assert.Equal(t, "reviewer", r.Nodes[3].Author)
			},
		},
		{
			name: "a background agent has no nested calls",
			evs: []*adksession.Event{rzPrompt(0, "q"),
				rzAnswer(1, "blitz", 0, rzCall("s", "invoke_agent", map[string]any{"agent_name": "qa", "background": true})),
				rzResponse(2, "blitz", "s", "invoke_agent", map[string]any{"task_id": "task-1"})},
			check: func(t *testing.T, r api.Reasoning) {
				assert.Equal(t, "qa (background)", r.Nodes[2].Title)
				assert.Len(t, r.Nodes, 3)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := buildReasoning(tc.evs, tc.subs, -1, rzPriced)
			require.NotEmpty(t, r.Nodes)
			tc.check(t, r)
		})
	}
}

// Remembered secrets are masked in the reasoning, its titles and the
// prompt (spec_056 CA-29a).
func TestReasoningMasksSecrets(t *testing.T) {
	const secret = "s3cr3t-reasoning-value-77"
	redact.Remember(secret)
	r := buildReasoning([]*adksession.Event{rzPrompt(0, "use "+secret),
		rzAnswer(1, "blitz", 0, rzThought("**Key "+secret+"**\nIt is "+secret), &genai.Part{Text: "ok " + secret})}, nil, 0, rzPriced)
	for _, n := range r.Nodes {
		assert.NotContains(t, n.Title+n.Text, secret, n.Kind)
	}
	assert.NotContains(t, r.Turns[0].Prompt, secret)
}

// A turn too big to draw is cut, and says so.
func TestReasoningCut(t *testing.T) {
	evs := []*adksession.Event{rzPrompt(0, "q")}
	for i := range api.MaxReasoningNodes {
		evs = append(evs, rzAnswer(i+1, "blitz", 0, rzCall(fmt.Sprint(i), "read_file", nil)))
	}
	r := buildReasoning(evs, nil, 0, rzPriced)
	assert.True(t, r.Cut)
	assert.Len(t, r.Nodes, api.MaxReasoningNodes)
	assert.Equal(t, api.MaxReasoningNodes, r.Turns[0].Calls, "the summary counts them all")
}

func TestThoughtSteps(t *testing.T) {
	for _, tc := range []struct {
		in     string
		titles []string
	}{
		{"", nil},
		{"  \n ", nil},
		{"One line only", []string{"One line only"}},
		{"**Only a heading**", []string{"Only a heading"}},
		{"Intro.\n**Heading**\nBody", []string{"Intro.", "Heading"}},
		{"**not** a heading line", []string{"not** a heading line"}},
		{strings.Repeat("x", 200), []string{strings.Repeat("x", 79) + "…"}},
	} {
		t.Run(tc.in, func(t *testing.T) {
			var got []string
			for _, s := range thoughtSteps(tc.in) {
				got = append(got, s.title)
			}
			assert.Equal(t, tc.titles, got)
		})
	}
}
