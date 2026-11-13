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
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/runtime/wirefake"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

// wait is how long a test waits for the fake or the voice (short: the
// fake is local).
const wait = 5 * time.Second

// voiceEvents collects a conversation's events for a test.
type voiceEvents struct {
	mu  sync.Mutex
	evs []VoiceEvent
	in  chan VoiceEvent
}

func newVoiceEvents() *voiceEvents { return &voiceEvents{in: make(chan VoiceEvent, 256)} }

func (e *voiceEvents) on(ev VoiceEvent) {
	e.mu.Lock()
	e.evs = append(e.evs, ev)
	e.mu.Unlock()
	e.in <- ev
}

// next waits for the next event of kind.
func (e *voiceEvents) next(t *testing.T, kind string) VoiceEvent {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case ev := <-e.in:
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %s event in %v", kind, wait)
			return VoiceEvent{}
		}
	}
}

// said waits for the next piece of what the voice says.
func (e *voiceEvents) said(t *testing.T) string {
	t.Helper()
	for {
		if ev := e.next(t, "said"); ev.Text != "" {
			return ev.Text
		}
	}
}

// openVoice opens a workspace whose voice is on and speaks to a fake Live
// API, with mutate's settings.
func openVoice(t *testing.T, mutate func(*config.Config)) (*Workspace, *wirefake.Live) {
	t.Helper()
	w, _ := openTestWith(t, mutate)
	_, err := config.SetValue(w.cfg.Dir, "", "voice.enabled", "true")
	require.NoError(t, err)
	live := wirefake.NewLive(t, "test-key")
	w.newVoiceModel = func(ctx context.Context, _ *config.Config) (*runtime.VoiceModel, error) {
		m, err := gemini.NewModel(ctx, "gemini-live-test", &genai.ClientConfig{APIKey: "test-key", Backend: genai.BackendGeminiAPI, HTTPOptions: genai.HTTPOptions{BaseURL: live.URL, APIVersion: "v1beta"}})
		return &runtime.VoiceModel{LLM: m, ID: "gemini-live-test", Backend: genai.BackendGeminiAPI}, err
	}
	return w, live
}

// Off means no conversation, and nothing asked of Google (VOX-02).
func TestVoiceOff(t *testing.T) {
	w, _ := openTestWith(t, nil)
	_, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s"})
	assert.ErrorIs(t, err, ErrVoiceOff)

	_, err = config.SetValue(w.cfg.Dir, "", "voice.enabled", "true")
	require.NoError(t, err)
	w.newVoiceModel = func(context.Context, *config.Config) (*runtime.VoiceModel, error) {
		return nil, runtime.ErrVoiceNotSetUp
	}
	_, err = w.StartVoice(context.Background(), VoiceOptions{SessionID: "s"})
	assert.ErrorIs(t, err, runtime.ErrVoiceNotSetUp, "on, but no Google provider")
}

// A conversation (VOX-07, VOX-10, VOX-11, VOX-16): the setup names the
// personality's voice and transcripts both ways; the voice's instructions
// have its frame and the personality; the person's words and the voice's
// are heard, said and kept in the session as spoken lines; the voice's
// audio reaches the window; run_task hands the work to the window, refused
// while a task runs (then steer); end_conversation ends it after its
// goodbye.
func TestVoiceConversation(t *testing.T) {
	w, live := openVoice(t, func(c *config.Config) { c.UI.Personality = "mentor" })
	s, err := w.NewSession()
	require.NoError(t, err)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: s.ID, OnEvent: evs.on})
	require.NoError(t, err)
	t.Cleanup(v.Close)

	setup := live.Opened(wait)
	gc := setup["generationConfig"].(map[string]any)
	assert.Equal(t, "Sadaltager", gc["speechConfig"].(map[string]any)["voiceConfig"].(map[string]any)["prebuiltVoiceConfig"].(map[string]any)["voiceName"], "the personality's voice")
	assert.Contains(t, setup, "inputAudioTranscription")
	assert.Contains(t, setup, "outputAudioTranscription")
	instruction := fmt.Sprint(setup["systemInstruction"])
	assert.Contains(t, instruction, "Blitz's voice")
	assert.Contains(t, instruction, "patient mentor")
	var names []string
	for _, tl := range setup["tools"].([]any) {
		for _, d := range tl.(map[string]any)["functionDeclarations"].([]any) {
			names = append(names, d.(map[string]any)["name"].(string))
		}
	}
	assert.ElementsMatch(t, []string{"run_task", "steer", "stop", "status", "answer", "recent", "end_conversation"}, names, "only the voice's tools")

	require.NoError(t, v.Audio(make([]byte, 640)))
	live.Next("realtimeInput", wait)

	live.Heard("Run the tests ")
	live.Heard("please")
	assert.Equal(t, "Run the tests ", evs.next(t, "heard").Text)
	res := live.Call("c1", "run_task", map[string]any{"prompt": "Run the test suite and fix what fails."}, wait)
	assert.Equal(t, true, res["ok"])
	assert.Equal(t, "Run the test suite and fix what fails.", evs.next(t, "run_task").Text)

	live.Say("On it.", []byte{1, 2, 3, 4})
	assert.Equal(t, "speaking", evs.next(t, "state").State)
	assert.Equal(t, []byte{1, 2, 3, 4}, evs.next(t, "audio").Audio)
	assert.Equal(t, "listening", evs.next(t, "state").State)
	rec, err := w.storage.Load(s.ID)
	require.NoError(t, err)
	var spoken []string
	for _, m := range rec.Messages {
		if m.Kind == session.KindSpoken {
			spoken = append(spoken, m.Role+": "+m.Content)
		}
	}
	assert.Equal(t, []string{"user: Run the tests please", "model: On it."}, spoken, "kept in writing")

	on := w.factsStart(s.ID, "Run the tests", nil)
	res = live.Call("c2", "run_task", map[string]any{"prompt": "more"}, wait)
	assert.Contains(t, res["error"], "use steer")
	res = live.Call("c3", "steer", map[string]any{"prompt": "Skip the slow ones."}, wait)
	assert.Equal(t, true, res["ok"])
	assert.Equal(t, "Skip the slow ones.", evs.next(t, "steer").Text)
	on(api.Event{ToolCall: &api.ToolCall{ID: "t", Name: "run_shell_command", Args: map[string]any{"command": "go test ./..."}}})
	res = live.Call("c4", "status", map[string]any{}, wait)
	assert.Contains(t, res["status"], "A task is running")
	assert.Contains(t, res["status"], "run_shell_command")
	res = live.Call("c5", "stop", map[string]any{}, wait)
	assert.Equal(t, true, res["ok"])
	evs.next(t, "stop")
	w.factsEnd(s.ID, api.TurnResult{}, context.Canceled)
	assert.Contains(t, fmt.Sprint(live.Next("clientContent", wait)), "It was stopped.", "the end is said")
	res = live.Call("c6", "steer", map[string]any{"prompt": "x"}, wait)
	assert.Contains(t, res["error"], "no task is running")

	res = live.Call("c7", "recent", map[string]any{"n": 2}, wait)
	assert.Contains(t, res["status"], "The person (spoken): Run the tests please")

	live.Call("c8", "end_conversation", map[string]any{}, wait)
	live.Say("Bye for now.", nil)
	assert.Equal(t, "goodbye", evs.next(t, "end").Text)
	select {
	case <-v.Done():
	case <-time.After(wait):
		t.Fatal("not ended")
	}
}

// Approvals by voice (VOX-13): said when it shows; a spoken yes allows
// only that approval, only once; a no denies; anything unclear, or another
// approval's ID, leaves it for a click. Questions are said and answered.
func TestVoiceApprovals(t *testing.T) {
	w, live := openVoice(t, nil)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", OnEvent: evs.on})
	require.NoError(t, err)
	t.Cleanup(v.Close)
	live.Opened(wait)

	v.Waiting(VoiceWaiting{ApprovalID: "ap1", Approval: "run make deploy"})
	assert.Contains(t, fmt.Sprint(live.Next("clientContent", wait)), "approval (ID ap1): run make deploy")

	for _, tc := range []struct{ heard, choice, id, want, decision string }{
		{"hmm, what does it do", "yes", "ap1", "didn't clearly say", ""},
		{"yes go ahead", "yes", "ap2", "isn't the one showing", ""},
		{"yes go ahead", "yes", "ap1", "allowed once", "once"},
		{"no don't", "no", "ap1", "denied", "deny"},
	} {
		t.Run(tc.heard+"/"+tc.id, func(t *testing.T) {
			live.Heard(tc.heard)
			evs.next(t, "heard")
			res := live.Call("a-"+tc.id+tc.decision, "answer", map[string]any{"choice": tc.choice, "approval_id": tc.id}, wait)
			assert.Contains(t, fmt.Sprint(res), tc.want)
			if tc.decision != "" {
				ev := evs.next(t, "approve")
				assert.Equal(t, VoiceEvent{Kind: "approve", ID: "ap1", Decision: tc.decision}, ev)
			}
			live.Say("Okay.", nil)
		})
	}

	v.Waiting(VoiceWaiting{Question: "Which database?", Options: []string{"Postgres", "SQLite"}})
	assert.Contains(t, fmt.Sprint(live.Next("clientContent", wait)), "Which database? (options: Postgres; SQLite)")
	res := live.Call("q1", "answer", map[string]any{"choice": "Postgres"}, wait)
	assert.Equal(t, true, res["ok"])
	assert.Equal(t, "Postgres", evs.next(t, "answer").Text)
	v.Waiting(VoiceWaiting{})
	res = live.Call("q2", "answer", map[string]any{"choice": "x"}, wait)
	assert.Contains(t, res["error"], "no question")
}

// A connection the server ends (goAway, its session limit) is resumed with
// the last handle, and the conversation carries on (VOX-07).
func TestVoiceResumes(t *testing.T) {
	w, live := openVoice(t, nil)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", OnEvent: evs.on})
	require.NoError(t, err)
	t.Cleanup(v.Close)
	live.Opened(wait)
	live.Say("Hello.", nil)
	assert.Equal(t, "Hello.", evs.said(t))
	live.Resume("handle-1")
	live.GoAway()
	again := live.Opened(wait)
	assert.Equal(t, "handle-1", again["sessionResumption"].(map[string]any)["handle"])
	live.Say("Still here.", nil)
	assert.Equal(t, "Still here.", evs.said(t))
}

// Summaries come from the turn's facts (VOX-17): how it ended, the files
// changed, the checks run and their exit codes, the cost over a dollar;
// with summaries off only a failure is said; summary-only speaks the last
// turn's and ends.
func TestVoiceSummaries(t *testing.T) {
	f := &turnFacts{Started: time.Now()}
	for _, e := range []api.Event{
		{ToolCall: &api.ToolCall{Name: "create_file", Args: map[string]any{"path": "a.go"}}},
		{ToolCall: &api.ToolCall{Name: "replace_in_file", Args: map[string]any{"path": "b.go"}}},
		{ToolCall: &api.ToolCall{Name: "replace_in_file", Args: map[string]any{"path": "a.go"}}},
		{ToolCall: &api.ToolCall{Name: "run_shell_command", Args: map[string]any{"command": "go test ./..."}}},
		{ToolResult: &api.ToolResult{Name: "run_shell_command", Result: map[string]any{"exit_code": float64(1)}}},
		{ToolCall: &api.ToolCall{Name: "run_shell_command", Args: map[string]any{"command": "go build ./..."}}},
		{ToolResult: &api.ToolResult{Name: "run_shell_command", Result: map[string]any{"exit_code": 0}}},
		{ToolCall: &api.ToolCall{Name: "run_shell_command", Args: map[string]any{"command": "ls"}}},
		{ToolResult: &api.ToolResult{Name: "run_shell_command", Result: map[string]any{}}},
		{Tasks: []api.Task{{Content: "fix", Status: "done"}, {Content: "test", Status: "pending"}}},
	} {
		f.observe(e)
	}
	assert.Equal(t, []string{"test"}, f.Tasks)
	f.Outcome, f.CostUSD = "finished", 1.25
	assert.Equal(t, `It finished. Files changed: a.go, b.go. Checks run: 1 passed, 1 failed: "go test ./..." (exit 1). It cost $1.25.`, f.describe())
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "It finished."},
		{context.Canceled, "It was stopped."},
		{fmt.Errorf("%w ($0.50)", api.ErrCostLimit), "It stopped at a limit: the turn reached its cost limit ($0.50)."},
		{errors.New("boom"), "It failed: boom."},
	} {
		g := &turnFacts{}
		g.Outcome, g.Error = outcomeOf(tc.err)
		assert.True(t, strings.HasPrefix(g.describe(), tc.want), g.describe())
	}
	w0, _ := openTestWith(t, nil)
	w0.factsEnd("none", api.TurnResult{}, nil) // nothing running: nothing to end
	assert.Nil(t, w0.lastTurnFacts("none"))
	assert.Equal(t, turnFacts{}, w0.currentTurnFacts("none"))
	many := &turnFacts{Files: []string{"a", "b", "c", "d"}}
	assert.Contains(t, many.describe(), "4 files changed, among them a, b, c.")

	w, live := openVoice(t, nil)
	_, err := config.SetValue(w.cfg.Dir, "", "voice.summaries", "off")
	require.NoError(t, err)
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s"})
	require.NoError(t, err)
	t.Cleanup(v.Close)
	live.Opened(wait)
	w.factsStart("s", "x", nil)
	w.factsEnd("s", api.TurnResult{}, nil)
	w.factsStart("s", "y", nil)
	w.factsEnd("s", api.TurnResult{}, errors.New("model down"))
	assert.Contains(t, fmt.Sprint(live.Next("clientContent", wait)), "The task failed: model down", "only the failure, with summaries off")
	v.Close()

	evs := newVoiceEvents()
	sv, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", SummaryOnly: true, OnEvent: evs.on})
	require.NoError(t, err)
	live.Opened(wait)
	assert.Contains(t, fmt.Sprint(live.Next("clientContent", wait)), "It failed: model down")
	live.Say("The last task failed.", []byte{9})
	assert.Equal(t, "summary", evs.next(t, "end").Text)
	<-sv.Done()
}

// A quiet conversation ends by itself, unless a task runs (VOX-14); a
// connection that fails is said and ends it (VOX-24).
func TestVoiceIdleAndErrors(t *testing.T) {
	w, live := openVoice(t, nil)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", OnEvent: evs.on})
	require.NoError(t, err)
	live.Opened(wait)
	v.mu.Lock()
	v.idleFor = 0
	v.mu.Unlock()
	go v.idleWatch(context.Background())
	assert.Equal(t, "idle", evs.next(t, "end").Text)

	assert.Equal(t, "Google refused the voice's sign-in: check Gemini in Settings › Providers.", v.errorText(errors.New("websocket: bad handshake 403")))
	assert.Contains(t, v.errorText(errors.New("RESOURCE_EXHAUSTED")), "quota")
	assert.Contains(t, v.errorText(errors.New("socket closed")), "The voice conversation stopped")
}

// Play's sample says one line in the voice asked for, then ends (VOX-41).
func TestVoiceSample(t *testing.T) {
	w, live := openVoice(t, nil)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "s", SampleVoice: "Puck", OnEvent: evs.on})
	require.NoError(t, err)
	setup := live.Opened(wait)
	assert.Contains(t, fmt.Sprint(setup["generationConfig"]), "Puck")
	assert.Contains(t, fmt.Sprint(live.Next("clientContent", wait)), "Hi, I'm Blitz")
	live.Say("Hi, I'm Blitz.", []byte{1, 2})
	assert.Equal(t, "summary", evs.next(t, "end").Text)
	<-v.Done()
}

// The tools' edges (VOX-11, VOX-12): an empty request, stop with nothing
// running, status with everything waiting and the last turn, recent with
// nothing kept and long messages cut; hold to talk's activity; an
// interruption; summary-only with nothing run; turned off.
func TestVoiceEdges(t *testing.T) {
	w, live := openVoice(t, nil)
	_, err := config.SetValue(w.cfg.Dir, "", "voice.listen", "hold")
	require.NoError(t, err)
	evs := newVoiceEvents()
	v, err := w.StartVoice(context.Background(), VoiceOptions{SessionID: "edges", OnEvent: evs.on})
	require.NoError(t, err)
	t.Cleanup(v.Close)
	setup := live.Opened(wait)
	assert.Contains(t, fmt.Sprint(setup["realtimeInputConfig"]), "disabled:true", "hold to talk")
	assert.Equal(t, "Kore", v.VoiceName())
	assert.Equal(t, "gemini-live-test", v.ModelID())
	assert.Equal(t, config.VoiceListenHold, w.VoiceSettings().ListenMode())

	require.NoError(t, v.Activity(true))
	assert.Contains(t, live.Next("realtimeInput", wait), "activityStart")
	require.NoError(t, v.Activity(false))
	assert.Contains(t, live.Next("realtimeInput", wait), "activityEnd")

	assert.Contains(t, live.Call("e1", "run_task", map[string]any{"prompt": " "}, wait)["error"], "write the request out")
	assert.Contains(t, live.Call("e2", "stop", map[string]any{}, wait)["error"], "no task is running")
	assert.Equal(t, "No messages yet.", live.Call("e3", "recent", map[string]any{}, wait)["status"])

	on := w.factsStart("edges", "x", nil)
	on(api.Event{Tasks: []api.Task{{Content: "write the test", Status: "in_progress"}}})
	v.Waiting(VoiceWaiting{ApprovalID: "ap9", Approval: "rm -rf build", Question: "Which branch?", Options: []string{"main"}})
	live.Next("clientContent", wait)
	st := fmt.Sprint(live.Call("e4", "status", map[string]any{}, wait)["status"])
	assert.Contains(t, st, "its task list: write the test")
	assert.Contains(t, st, "An approval is waiting (ID ap9): rm -rf build.")
	assert.Contains(t, st, "A question is waiting: Which branch? (options: main).")
	w.factsEnd("edges", api.TurnResult{}, nil)
	live.Next("clientContent", wait)
	st = fmt.Sprint(live.Call("e5", "status", map[string]any{}, wait)["status"])
	assert.Contains(t, st, "No task is running.")
	assert.Contains(t, st, "The last task: It finished.")

	s, err := w.NewSession()
	require.NoError(t, err)
	for i := range 12 {
		w.appendIn(w.storage, s.ID, nil, session.Message{Role: "model", Content: strings.Repeat(fmt.Sprint(i), 1300)})
	}
	v.opts.SessionID = s.ID
	recent := fmt.Sprint(live.Call("e6", "recent", map[string]any{"n": 50}, wait)["status"])
	assert.Equal(t, 10, strings.Count(recent, "The agent:"), "at most 10")
	assert.Contains(t, recent, "…", "long ones cut")

	live.Send(map[string]any{"serverContent": map[string]any{"interrupted": true}})
	evs.next(t, "interrupted")
	assert.Equal(t, "[Blitz] Nothing has run in this conversation yet. Say so in one sentence.", v.summaryRequest(nil))
	assert.Equal(t, "abc…", short("abc  def", 3))
	assert.Equal(t, 7, exitOf(map[string]any{"exit_code": int64(7)}))
	assert.Equal(t, -1, exitOf(map[string]any{"error": "boom"}))
	assert.Equal(t, time.Second, (&turnFacts{Started: time.Unix(0, 0), Ended: time.Unix(1, 0)}).took())

	v.End("off") // voice turned off (VOX-02)
	assert.Equal(t, "off", evs.next(t, "end").Text)
	assert.Error(t, v.Audio([]byte{1}), "nothing to send to")
	v.tellQuietly("[Blitz] too late") // logged, not fatal
}

// With no Google provider, the real model builder says voice isn't set up
// (VOX-03).
func TestVoiceNotSetUp(t *testing.T) {
	w, _ := openTestWith(t, nil)
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())
	_, err := config.SetValue(w.cfg.Dir, "", "voice.enabled", "true")
	require.NoError(t, err)
	_, err = w.StartVoice(context.Background(), VoiceOptions{SessionID: "s"})
	assert.ErrorIs(t, err, runtime.ErrVoiceNotSetUp)
}
