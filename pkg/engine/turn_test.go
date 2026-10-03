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
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func text(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleModel) }

func ignore(api.Event) {}

// transcript returns the active session's messages as "role: text".
func transcript(t *testing.T, w *Workspace) []string {
	t.Helper()
	s, ok := w.ActiveSession()
	require.True(t, ok, "no active session")
	var out []string
	for _, m := range s.Messages {
		out = append(out, m.Role+": "+m.Text)
	}
	return out
}

func newSession(t *testing.T, w *Workspace) api.SessionInfo {
	t.Helper()
	s, _, err := w.OpenSession("", false)
	require.NoError(t, err)
	return s
}

func TestRunRecordsBothSidesAndOmitsThoughts(t *testing.T) {
	reply := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "pondering", Thought: true}, {Text: "hello there"}}}
	w, llm := openTestWith(t, nil, reply)
	sid := newSession(t, w).ID
	accepted := 0
	var seen int
	res, err := w.Run(context.Background(), sid, api.Turn{Text: "hi", OnAccepted: func() { accepted++ }},
		func(api.Event) { seen++ })
	require.NoError(t, err)
	assert.Equal(t, "hello there", res.Output, "output %q, accepted %d, events %d, calls %d", res.Output, accepted, seen, llm.Calls())
	assert.Equal(t, 1, accepted, "output %q, accepted %d, events %d, calls %d", res.Output, accepted, seen, llm.Calls())
	assert.NotEqual(t, 0, seen, "output %q, accepted %d, events %d, calls %d", res.Output, accepted, seen, llm.Calls())
	assert.Equal(t, 1, llm.Calls(), "output %q, accepted %d, events %d, calls %d", res.Output, accepted, seen, llm.Calls())
	got := transcript(t, w)
	assert.Equal(t, []string{"user: hi", "model: hello there"}, got, "transcript %q", got)
}

func TestRunPlanPromptOverrideAndAside(t *testing.T) {
	w, llm := openTestWith(t, nil, text("a plan"), text("searched"), text("an aside"))
	sid := newSession(t, w).ID
	ctx := context.Background()
	_, err := w.Run(ctx, sid, api.Turn{Text: "add a flag", Plan: true}, ignore)
	require.NoError(t, err)
	_, err = w.Run(ctx, sid, api.Turn{Text: "/search web go", Prompt: "results: …", ReadOnly: "search"}, ignore)
	require.NoError(t, err)
	assert.Contains(t, lastUserText(llm), "results: …", "Prompt not sent: %q", lastUserText(llm))
	_, err = w.Run(ctx, sid, api.Turn{Text: "what's a flag?", Aside: true}, ignore)
	require.NoError(t, err)
	want := []string{"user: /plan add a flag", "model: a plan", "user: /search web go", "model: searched"} // asides are recorded nowhere
	got := transcript(t, w)
	assert.Equal(t, want, got, "transcript %q, want %q", got, want)
}

func TestRunAcceptedSkipsHooksAndRecording(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Hooks.PromptSubmit = []config.HookConfig{{Command: `echo "nope" >&2; exit 2`}}
	}, text("ok"))
	sid := newSession(t, w).ID
	_, err := w.Run(context.Background(), sid, api.Turn{Text: "late steer", Accepted: true}, ignore)
	require.NoError(t, err)
	got := transcript(t, w)
	assert.Equal(t, []string{"model: ok"}, got, "transcript %q", got)
}

func TestRunAndSteerBlockedByHook(t *testing.T) {
	w, llm := openTestWith(t, func(c *config.Config) {
		c.Hooks.PromptSubmit = []config.HookConfig{{Command: `echo "no secrets" >&2; exit 2`}}
	}, text("should not run"))
	sid := newSession(t, w).ID
	accepted := false
	_, err := w.Run(context.Background(), sid, api.Turn{Text: "my password", OnAccepted: func() { accepted = true }}, ignore)
	var blocked *api.BlockedError
	assert.ErrorAs(t, err, &blocked, "err %v, accepted %v, calls %d", err, accepted, llm.Calls())
	assert.Contains(t, blocked.Reason, "no secrets", "err %v, accepted %v, calls %d", err, accepted, llm.Calls())
	assert.False(t, accepted, "err %v, accepted %v, calls %d", err, accepted, llm.Calls())
	assert.Equal(t, 0, llm.Calls(), "err %v, accepted %v, calls %d", err, accepted, llm.Calls())
	err = w.Steer(context.Background(), sid, "my password")
	assert.ErrorAs(t, err, &blocked, "steer: %v", err)
	got := transcript(t, w)
	assert.Len(t, got, 0, "a blocked prompt was recorded: %q", got)
}

func TestSteerRecordsAndUnreadSteersAreLeftOver(t *testing.T) {
	w, _ := openTestWith(t, nil, text("done"))
	sid := newSession(t, w).ID
	ctx := context.Background()
	// A message sent as the agent stops (the front end was still taking it)
	// must still come back as unread.
	res, err := w.Run(ctx, sid, api.Turn{Text: "go", OnFinished: func() {
		assert.NoError(t, w.Steer(ctx, sid, "also do this"))
	}}, ignore)
	require.NoError(t, err)
	assert.Equal(t, []string{"also do this"}, res.Leftover, "leftover %q", res.Leftover)
	got := transcript(t, w)
	assert.Equal(t, []string{"user: go", "user: also do this", "model: done"}, got, "transcript %q", got)
}

// lastUserText is the text of the last user message the model was sent.
func lastUserText(m *runtime.MockLLM) string {
	req := m.Requests[len(m.Requests)-1]
	for i := len(req.Contents) - 1; i >= 0; i-- {
		if c := req.Contents[i]; c.Role == genai.RoleUser {
			var sb strings.Builder
			for _, p := range c.Parts {
				sb.WriteString(p.Text)
			}
			return sb.String()
		}
	}
	return ""
}

func adkText(text string, partial, thought bool) *adksession.Event {
	ev := &adksession.Event{Author: "blitz"}
	ev.LLMResponse = model.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: text, Thought: thought}}}, Partial: partial}
	return ev
}

// Streamed chunks are delivered as they come; the final event that repeats
// them is marked, and only final answer text reaches the transcript.
func TestRelayMarksRepeatedText(t *testing.T) {
	var got []api.Event
	r := &relay{on: func(e api.Event) { got = append(got, e) }}
	r.handle(adkText("thinking…", true, true))
	r.handle(adkText("Hel", true, false))
	r.handle(adkText("lo", true, false))
	r.handle(adkText("Hello", false, false))  // repeats the chunks
	r.handle(adkText(" again", false, false)) // not streamed
	r.handle(&adksession.Event{})             // no content
	require.Len(t, got, 5, "got %d events", len(got))
	var shown strings.Builder
	for _, e := range got {
		require.Equal(t, "blitz", e.Author, "event %+v", e)
		require.NotNil(t, e.Text, "event %+v", e)
		if !e.Text.Thought && (e.Text.Partial || !e.Text.Repeat) {
			shown.WriteString(e.Text.Text)
		}
	}
	assert.Equal(t, "Hello again", shown.String(), "shown %q, events %+v", shown.String(), got)
	assert.True(t, got[3].Text.Repeat, "shown %q, events %+v", shown.String(), got)
	assert.False(t, got[4].Text.Repeat, "shown %q, events %+v", shown.String(), got)
	assert.Equal(t, "Hello again", r.output.String(), "transcript text %q", r.output.String())
}

// A front end may take steer messages as soon as the prompt is accepted;
// the prompt must be in the transcript before any of them.
func TestPromptIsRecordedBeforeSteering(t *testing.T) {
	w, _ := openTestWith(t, nil, text("done"))
	sid := newSession(t, w).ID
	ctx := context.Background()
	_, err := w.Run(ctx, sid, api.Turn{Text: "reformat", OnAccepted: func() {
		assert.NoError(t, w.Steer(ctx, sid, "use tabs"))
	}}, ignore)
	require.NoError(t, err)
	got := transcript(t, w)
	assert.GreaterOrEqual(t, len(got), 2, "transcript %q", got)
	assert.Equal(t, "user: reformat", got[0], "transcript %q", got)
	assert.Equal(t, "user: use tabs", got[1], "transcript %q", got)
}

// A detached turn runs in a session of its own, which doesn't become the
// workspace's active one.
func TestRunDetached(t *testing.T) {
	w, llm := openTestWith(t, nil, text("checked"))
	llm.Usage = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 1}
	var started string
	res, err := w.RunDetached(context.Background(), api.Turn{Text: "check"}, func(id string) { started = id }, ignore)
	require.NoError(t, err)
	assert.Equal(t, "checked", res.Output)
	require.NotEmpty(t, started)
	_, active := w.ActiveSession()
	assert.False(t, active, "the detached session became active")
	rec, err := w.storage.Get(started)
	require.NoError(t, err)
	assert.Len(t, rec.Messages, 2)
	assert.Equal(t, 1, w.UsageOf(started).Calls)
	assert.NoError(t, w.DeleteSession(context.Background(), started), "still held by the run's storage")
}

// How a turn ended, for metrics.
func TestTurnOutcome(t *testing.T) {
	for want, err := range map[string]error{
		"ok":        nil,
		"blocked":   &api.BlockedError{Reason: "no"},
		"limit":     api.ErrMaxTurns,
		"cancelled": context.Canceled,
		"error":     errors.New("boom"),
	} {
		t.Run(want, func(t *testing.T) { assert.Equal(t, want, turnOutcome(err)) })
	}
}

// Images go to the model with the prompt, and the transcript names them.
func TestRunWithImagesAndFetchGrants(t *testing.T) {
	w, llm := openTestWith(t, nil, text("a red square"))
	writePNG(t, filepath.Join(w.Dir(), "shot.png"))
	img, err := w.LoadImage("shot.png")
	require.NoError(t, err)
	_, err = w.Run(context.Background(), newSession(t, w).ID, api.Turn{Text: "what is it?", Images: []*images.Image{img}, FetchGrants: []string{"https://go.dev/"}}, ignore)
	require.NoError(t, err)
	var inline int
	for _, c := range llm.Requests[0].Contents {
		for _, p := range c.Parts {
			if p.InlineData != nil {
				inline++
			}
		}
	}
	assert.Equal(t, 1, inline, "the image was sent")
	assert.Equal(t, "\n[images: shot.png]", AttachmentNote([]*images.Image{img}))
	assert.Equal(t, "", AttachmentNote(nil))
}

// A stop hook that asks to go on without saying why gets a default
// reason.
func TestStopHookWithoutAReason(t *testing.T) {
	w, llm := openTestWith(t, func(c *config.Config) {
		c.Hooks.Stop = []config.HookConfig{{Command: `input=$(cat); case "$input" in *'"stop_hook_active":true'*) ;; *) echo '{"continue": true}';; esac`}}
	}, text("done"), text("really done"))
	_, err := w.Run(context.Background(), newSession(t, w).ID, api.Turn{Text: "go"}, ignore)
	require.NoError(t, err)
	require.Equal(t, 2, llm.Calls())
	assert.Equal(t, "A stop hook asked you to continue.", userTextAt(llm.Requests[1].Contents))
}
