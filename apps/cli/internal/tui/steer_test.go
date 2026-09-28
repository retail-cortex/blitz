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

package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func TestSteerTrigger(t *testing.T) {
	cases := []struct {
		in      string
		prefill string
		trigger bool
	}{
		{"fix", "fix", true},
		{"é", "é", true},
		{"\x14", "", true},      // Ctrl+T
		{"\x14ab", "ab", true},  // Ctrl+T then typing
		{"\r", "", false},       // Enter
		{"\x1b[A", "", false},   // arrow key
		{"\x03\x04", "", false}, // other control keys
		{"a\rb", "ab", true},    // control bytes dropped
	}
	for _, c := range cases {
		p, trig := steerTrigger([]byte(c.in))
		assert.Equal(t, c.prefill, p, "%q: got (%q, %v), want (%q, %v)", c.in, p, trig, c.prefill, c.trigger)
		assert.Equal(t, c.trigger, trig, "%q: got (%q, %v), want (%q, %v)", c.in, p, trig, c.prefill, c.trigger)
	}
}

// chanKeys is a keyTerm fed through a channel.
type chanKeys struct {
	in            chan []byte
	pending       []byte
	entered, left atomic.Int32
	inMode        atomic.Bool
}

func newChanKeys() *chanKeys { return &chanKeys{in: make(chan []byte, 8)} }

func (c *chanKeys) enter() error { c.entered.Add(1); c.inMode.Store(true); return nil }
func (c *chanKeys) leave() error { c.left.Add(1); c.inMode.Store(false); return nil }
func (c *chanKeys) ready(d time.Duration) (bool, error) {
	if c.pending != nil {
		return true, nil
	}
	select {
	case b := <-c.in:
		c.pending = b
		return true, nil
	case <-time.After(d):
		return false, nil
	}
}
func (c *chanKeys) read(p []byte) (int, error) {
	n := copy(p, c.pending)
	c.pending = nil
	return n, nil
}

func TestKeyWatcherTriggersPausesAndRestores(t *testing.T) {
	keys := newChanKeys()
	got := make(chan string, 4)
	w := startKeyWatcher(keys, func(prefill string) {
		assert.False(t, keys.inMode.Load(), "onKey ran with the terminal still in key mode")
		got <- prefill
	}, nil)

	keys.in <- []byte("\r") // ignored
	keys.in <- []byte("fix")
	select {
	case p := <-got:
		require.Equal(t, "fix", p, "prefill %q", p)
	case <-time.After(2 * time.Second):
		t.Fatal("typing did not open the steer prompt")
	}

	resume, err := w.pause(context.Background())
	require.NoError(t, err)
	require.False(t, keys.inMode.Load(), "terminal still in key mode while paused")
	keys.in <- []byte("x")
	select {
	case p := <-got:
		t.Fatalf("key handled while paused: %q", p)
	case <-time.After(200 * time.Millisecond):
	}
	resume()
	select {
	case p := <-got:
		require.Equal(t, "x", p, "after resume: %q", p)
	case <-time.After(2 * time.Second):
		t.Fatal("key typed during the pause was lost")
	}

	w.close()
	require.False(t, keys.inMode.Load(), "terminal not restored: entered %d, left %d", keys.entered.Load(), keys.left.Load())
	require.Equal(t, keys.left.Load(), keys.entered.Load(), "terminal not restored: entered %d, left %d", keys.entered.Load(), keys.left.Load())
	resume, err = w.pause(context.Background())
	require.NoError(t, err, "pause after close must be a no-op")
	require.NotNil(t, resume, "pause after close must be a no-op")
}

// A prompt during the turn (an approval) waits while a steer message is
// being typed, and gives up if its context ends first.
func TestKeyWatcherPauseWaitsForOpenSteerPrompt(t *testing.T) {
	keys := newChanKeys()
	release := make(chan struct{})
	w := startKeyWatcher(keys, func(string) { <-release }, nil)
	defer w.close()
	keys.in <- []byte("a")
	time.Sleep(100 * time.Millisecond) // steer prompt now "open"

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := w.pause(ctx)
	require.Error(t, err, "pause succeeded while a steer prompt was open")
	close(release)
	resume, err := w.pause(context.Background())
	require.NoError(t, err)
	resume()
}

func TestPrinterPauseHoldsOutputAndOnlyRespinsIfSpinning(t *testing.T) {
	var out bytes.Buffer
	p := NewPrinter(PrinterOptions{Out: &out, Spinner: true})
	p.text("before ")
	p.Pause()
	p.text("during ")
	require.NotContains(t, out.String(), "during", "output written while paused")
	p.Resume()
	require.Contains(t, out.String(), "before during ", "held output not flushed in order: %q", out.String())
	require.False(t, p.spin.Running(), "spinner restarted mid-text")

	p.spin.Start("working")
	p.Pause()
	p.Resume()
	require.True(t, p.spin.Running(), "spinner not restored after pause")
	p.End()
}

// fakeSteerInput types one message during the first turn it watches.
type fakeSteerInput struct {
	*LineReader
	message string
	once    sync.Once
	typed   chan struct{} // closed once the message has been handled
}

func (f *fakeSteerInput) WatchKeys(onKey func(string)) func() {
	started := false
	f.once.Do(func() {
		started = true
		go func() {
			onKey(f.message[:1])
			close(f.typed)
		}()
	})
	if !started {
		return func() {}
	}
	return func() { <-f.typed }
}

func (f *fakeSteerInput) AskSteer(_ context.Context, _ string, prefill string) (string, error) {
	return prefill + f.message[1:], nil
}

// gatedLLM holds its first call until the user's message is handled, so
// the test doesn't depend on timing.
type gatedLLM struct {
	*runtime.MockLLM
	gate <-chan struct{}
}

func (g *gatedLLM) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	if g.Calls() == 0 {
		select {
		case <-g.gate:
		case <-time.After(5 * time.Second):
		}
	}
	return g.MockLLM.GenerateContent(ctx, req, stream)
}

func newSteerApp(t *testing.T, message string, cfgFn func(*config.Config), replies ...*genai.Content) (*App, *runtime.MockLLM) {
	t.Helper()
	isolateHome(t)
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Images.Dir = t.TempDir()
	cfg.Audit.Enabled = false
	cfg.Blitz.AutoApprove = true
	if cfgFn != nil {
		cfgFn(cfg)
	}
	in := &fakeSteerInput{LineReader: NewLineReader(strings.NewReader(""), io.Discard), message: message, typed: make(chan struct{})}
	llm := runtime.NewMockLLM("gemini-3.8-flash", replies...)
	app := openApp(t, cfg, &gatedLLM{MockLLM: llm, gate: in.typed})
	local(app).Storage().CreateSession("", "t", "blitz")
	app.Input = in
	return app, llm
}

func listFilesCall() *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_files", Args: map[string]any{}}}}}
}

func userMessages(st *session.Storage) []string {
	var out []string
	for _, m := range st.Active().Messages {
		if m.Role == "user" {
			out = append(out, m.Content)
		}
	}
	return out
}

func TestREPLSteerReachesTheAgentMidTurn(t *testing.T) {
	app, llm := newSteerApp(t, "use tabs", nil, listFilesCall(), genai.NewContentFromText("done", genai.RoleModel))
	runTurn(context.Background(), app, local(app).Storage().Active().ID, "reformat", nil, turnOptions{})

	n := llm.Calls()
	require.Equal(t, 2, n, "want 2 model calls, got %d", n)
	found := false
	for _, c := range llm.Requests[1].Contents {
		for _, p := range c.Parts {
			if r := p.FunctionResponse; r != nil && r.Response[runtime.SteerKey] == "use tabs" {
				found = true
			}
		}
	}
	require.True(t, found, "steer message not in the tool result the model read")
	got := fmt.Sprint(userMessages(local(app).Storage()))
	require.Equal(t, "[reformat use tabs]", got, "transcript user messages = %s", got)
}

func TestREPLLateSteerIsSentAsTheNextPrompt(t *testing.T) {
	app, llm := newSteerApp(t, "and add a test", nil,
		genai.NewContentFromText("done", genai.RoleModel),       // no tool call: the message can't ride along
		genai.NewContentFromText("test added", genai.RoleModel)) // the follow-up turn
	runTurn(context.Background(), app, local(app).Storage().Active().ID, "fix the bug", nil, turnOptions{})

	n := llm.Calls()
	require.Equal(t, 2, n, "want a follow-up turn (2 model calls), got %d", n)
	last := llm.Requests[1].Contents[len(llm.Requests[1].Contents)-1]
	require.Equal(t, genai.RoleUser, last.Role, "follow-up prompt = %+v", last)
	require.Contains(t, last.Parts[0].Text, "and add a test", "follow-up prompt = %+v", last)
	got := fmt.Sprint(userMessages(local(app).Storage()))
	require.Equal(t, "[fix the bug and add a test]", got, "message recorded twice or not at all: %s", got)
}

func TestREPLSteerGoesThroughPromptHooks(t *testing.T) {
	app, llm := newSteerApp(t, "my password is hunter2", func(c *config.Config) {
		c.Hooks.PromptSubmit = []config.HookConfig{{Command: `grep -q password && { echo "no secrets" >&2; exit 2; }; exit 0`}}
	}, listFilesCall(), genai.NewContentFromText("done", genai.RoleModel))
	runTurn(context.Background(), app, local(app).Storage().Active().ID, "reformat", nil, turnOptions{})

	for _, c := range llm.Requests[len(llm.Requests)-1].Contents {
		for _, p := range c.Parts {
			r := p.FunctionResponse
			require.False(t, r != nil && r.Response[runtime.SteerKey] != nil, "blocked message reached the agent")
		}
	}
	got := fmt.Sprint(userMessages(local(app).Storage()))
	require.NotContains(t, got, "hunter2", "blocked message recorded: %s", got)
}
