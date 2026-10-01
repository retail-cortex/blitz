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
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptStep is one answer of a scriptInput: a line or an error, after
// running do. block waits for the read to be cancelled instead.
type scriptStep struct {
	line  string
	err   error
	do    func()
	block bool
}

// scriptInput answers the REPL's reads from a script, then EOF.
type scriptInput struct {
	mu    sync.Mutex
	steps []scriptStep
	asks  []string // answers to Ask, then EOF
}

func (s *scriptInput) ReadInput(ctx context.Context, _ string) (string, error) {
	s.mu.Lock()
	if len(s.steps) == 0 {
		s.mu.Unlock()
		return "", io.EOF
	}
	st := s.steps[0]
	s.steps = s.steps[1:]
	s.mu.Unlock()
	if st.do != nil {
		st.do()
	}
	if st.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return st.line, st.err
}

func (s *scriptInput) Ask(context.Context, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.asks) == 0 {
		return "", io.EOF
	}
	a := s.asks[0]
	s.asks = s.asks[1:]
	return a, nil
}

// lines is a script of plain lines.
func lines(ls ...string) []scriptStep {
	out := make([]scriptStep, len(ls))
	for i, l := range ls {
		out[i] = scriptStep{line: l}
	}
	return out
}

// recordTurns makes the stub's turns record what they were sent.
func recordTurns(s *stubBackend) *[]api.Turn {
	var mu sync.Mutex
	turns := &[]api.Turn{}
	s.run = func(_ context.Context, t api.Turn, _ func(api.Event)) (api.TurnResult, error) {
		mu.Lock()
		*turns = append(*turns, t)
		mu.Unlock()
		if t.OnAccepted != nil {
			t.OnAccepted()
		}
		if t.OnFinished != nil {
			t.OnFinished()
		}
		return api.TurnResult{}, nil
	}
	return turns
}

// runScript runs the REPL over steps and returns what it printed and its
// error.
func runScript(t *testing.T, app *App, steps []scriptStep) (string, error) {
	t.Helper()
	if app.Interrupts == nil {
		app.Interrupts = make(chan os.Signal, 1)
	}
	app.Input = &scriptInput{steps: steps}
	var err error
	out := captureStdout(t, func() { err = RunREPL(context.Background(), app) })
	return out, err
}

// Without an active session, the lines that need one say so instead of
// running a turn.
func TestREPLWithoutASession(t *testing.T) {
	app, s := stubApp(t, "")
	s.activeSession = noSession
	s.newSession = func() (api.SessionInfo, error) { return api.SessionInfo{ID: "ghost"}, nil }
	s.rewindPoints = func() ([]api.RewindPoint, error) { return nil, nil }
	turns := recordTurns(s)
	steps := append(lines("   ", "/btw why", "/init", "/search web go", "/goal tests pass", "hello"),
		scriptStep{err: ErrRewindKey})
	out, err := runScript(t, app, steps)
	require.NoError(t, err)
	assert.Empty(t, *turns, "a turn ran without a session")
	assert.Equal(t, 5, bytes.Count([]byte(out), []byte("No active session.")), "output:\n%s", out)
	assert.Contains(t, out, "No prompts to rewind to yet.", "Esc Esc opens /rewind")
}

// The REPL stops when it can't start a session, or the input fails.
func TestREPLErrors(t *testing.T) {
	app, s := stubApp(t, "")
	s.activeSession = noSession
	s.newSession = func() (api.SessionInfo, error) { return api.SessionInfo{}, errBoom }
	_, err := runScript(t, app, nil)
	assert.ErrorIs(t, err, errBoom)

	app, _ = stubApp(t, "")
	gone := errors.New("terminal gone")
	_, err = runScript(t, app, []scriptStep{{err: gone}})
	assert.ErrorIs(t, err, gone)
}

// Cancelling the REPL's context at the prompt ends it quietly.
func TestREPLContextCancelled(t *testing.T) {
	app, _ := stubApp(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	app.Interrupts = make(chan os.Signal, 1)
	app.Input = &scriptInput{steps: []scriptStep{{do: cancel, err: context.Canceled}}}
	var err error
	captureStdout(t, func() { err = RunREPL(ctx, app) })
	assert.NoError(t, err)
}

// Ctrl+C at the prompt with a task running asks first; cancelling stays,
// and EOF stops the task on the way out.
func TestREPLExitWithARunningTask(t *testing.T) {
	app, s := stubApp(t, "")
	running := true
	s.listTasks = func() []api.TaskInfo {
		if !running {
			return nil
		}
		return []api.TaskInfo{{ID: "task-1", Agent: "qa", Prompt: "check", State: api.TaskRunning, Started: time.Now()}}
	}
	var stopped []string
	s.stopTask = func(id string) (api.TaskInfo, error) {
		stopped = append(stopped, id)
		running = false
		return api.TaskInfo{ID: id}, nil
	}
	app.Interrupts = make(chan os.Signal, 1)
	in := &scriptInput{steps: []scriptStep{{err: context.Canceled}}, asks: []string{"c"}}
	app.Input = in
	var err error
	captureStdout(t, func() { err = RunREPL(context.Background(), app) })
	require.NoError(t, err)
	assert.Equal(t, []string{"task-1"}, stopped)
}

// The turn's outcomes: a blocked prompt, an error, and an interrupted turn
// that drops the steer messages it never read.
func TestREPLTurnOutcomes(t *testing.T) {
	cases := map[string]struct {
		run  func(sig chan os.Signal) func(context.Context, api.Turn, func(api.Event)) (api.TurnResult, error)
		want []string
	}{
		"blocked": {
			run: func(chan os.Signal) func(context.Context, api.Turn, func(api.Event)) (api.TurnResult, error) {
				return func(context.Context, api.Turn, func(api.Event)) (api.TurnResult, error) {
					return api.TurnResult{}, &api.BlockedError{Reason: "no secrets"}
				}
			},
			want: []string{"Prompt blocked by hook: no secrets"},
		},
		"failed": {
			run: func(chan os.Signal) func(context.Context, api.Turn, func(api.Event)) (api.TurnResult, error) {
				return func(context.Context, api.Turn, func(api.Event)) (api.TurnResult, error) {
					return api.TurnResult{}, errBoom
				}
			},
			want: []string{"Error: boom"},
		},
		"interrupted": {
			run: func(sig chan os.Signal) func(context.Context, api.Turn, func(api.Event)) (api.TurnResult, error) {
				return func(ctx context.Context, _ api.Turn, _ func(api.Event)) (api.TurnResult, error) {
					sig <- os.Interrupt
					<-ctx.Done()
					return api.TurnResult{Leftover: []string{"also this"}}, ctx.Err()
				}
			},
			want: []string{"Interrupted", "your message was not sent: also this"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			app, s := stubApp(t, "")
			ch := make(chan os.Signal, 1)
			app.Interrupts = ch
			s.run = c.run(ch)
			out, err := runScript(t, app, lines("do it"))
			require.NoError(t, err)
			for _, w := range c.want {
				assert.Contains(t, out, w)
			}
		})
	}
}

// A long turn rings the bell when it ends.
func TestREPLNotifiesAfterALongTurn(t *testing.T) {
	app, s := stubApp(t, "")
	s.run = func(context.Context, api.Turn, func(api.Event)) (api.TurnResult, error) {
		time.Sleep(2 * time.Millisecond)
		return api.TurnResult{}, nil
	}
	app.Notify = Notifier{After: time.Millisecond, Mode: "bell"}
	out, err := runScript(t, app, lines("do it"))
	require.NoError(t, err)
	assert.Contains(t, out, "\a")
}

// /goal with a condition sets the goal and starts a turn toward it; a goal
// that can't be set is reported.
func TestREPLStartsAGoal(t *testing.T) {
	app, s := stubApp(t, "")
	turns := recordTurns(s)
	s.setGoal = func(cond string) (api.Goal, error) {
		if cond == "bad" {
			return api.Goal{}, errBoom
		}
		return api.Goal{Condition: cond, Max: 7}, nil
	}
	out, err := runScript(t, app, lines("/goal bad", "/goal tests pass"))
	require.NoError(t, err)
	assert.Contains(t, out, "✗ boom")
	assert.Contains(t, out, "at most 7 more turns")
	require.Len(t, *turns, 1)
	assert.Contains(t, (*turns)[0].Prompt, "Work toward this goal, and keep going until it holds: tests pass")
}

// A loop that is due runs its prompt before the next read.
func TestREPLRunsADueLoop(t *testing.T) {
	app, s := stubApp(t, "")
	turns := recordTurns(s)
	app.loops.add(time.Minute, "/review")
	later := time.Now().Add(time.Hour)
	app.loops.now = func() time.Time { return later }
	out, err := runScript(t, app, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "Loop 1: /review")
	require.Len(t, *turns, 1)
	assert.Equal(t, "/review", (*turns)[0].Text)
	assert.True(t, (*turns)[0].Command, "a slash prompt runs as a custom command")
}

// A background process's report interrupts the prompt and runs a turn.
func TestREPLRunsNotices(t *testing.T) {
	old := noticePoll
	noticePoll = 5 * time.Millisecond
	t.Cleanup(func() { noticePoll = old })
	app, s := stubApp(t, "")
	turns := recordTurns(s)
	var once sync.Once
	s.takeNotices = func() []string {
		var n []string
		once.Do(func() { n = []string{"make exited with code 2"} })
		return n
	}
	out, err := runScript(t, app, []scriptStep{{block: true}})
	require.NoError(t, err)
	assert.Contains(t, out, "A background process reported")
	require.Len(t, *turns, 1)
	assert.Equal(t, "(background) make exited with code 2", (*turns)[0].Text)
}

// runNotices does nothing without notices or a session.
func TestRunNoticesWithNothingToDo(t *testing.T) {
	app, s := stubApp(t, "")
	turns := recordTurns(s)
	captureStdout(t, func() { runNotices(context.Background(), app, nil) })
	s.activeSession = noSession
	app.notices = []string{"x"}
	captureStdout(t, func() { runNotices(context.Background(), app, nil) })
	captureStdout(t, func() { runLoop(context.Background(), app, &sessionLoop{n: 1, prompt: "p"}, nil) })
	assert.Empty(t, *turns)
}

// promptKeysInput is a scriptInput that takes the prompt's keys, as the
// terminal does.
type promptKeysInput struct {
	*scriptInput
	keys PromptKeys
}

func (p *promptKeysInput) SetPromptKeys(k PromptKeys) { p.keys = k }

// Shift+Tab at the prompt cycles the mode and redraws the prompt; a mode
// that can't be set leaves it.
func TestREPLPromptKeysCycleTheMode(t *testing.T) {
	app, s := stubApp(t, "")
	in := &promptKeysInput{}
	var prompts []string
	failing := false
	s.setPermission = func(mode string) (string, error) {
		if failing {
			return "", errBoom
		}
		return s.Backend.SetPermissionMode(mode)
	}
	in.scriptInput = &scriptInput{steps: []scriptStep{
		{do: func() { prompts = append(prompts, in.keys.CycleMode()) }},
		{do: func() { failing = true; prompts = append(prompts, in.keys.CycleMode()) }},
	}}
	app.Input = in
	app.Interrupts = make(chan os.Signal, 1)
	captureStdout(t, func() { require.NoError(t, RunREPL(context.Background(), app)) })
	require.Len(t, prompts, 2)
	assert.Contains(t, prompts[0], "accept-edits")
	assert.Empty(t, prompts[1], "a failed switch keeps the prompt")
	assert.Nil(t, in.keys.CycleMode, "the keys are released at exit")
}

// A switch to bypass that fails falls back to default, and a failure there
// leaves the mode.
func TestCycleModeOutOfBypass(t *testing.T) {
	app, s := stubApp(t, "")
	s.settings = func() api.Settings { return api.Settings{PermissionMode: string(api.ModePlan)} }
	var tried []string
	s.setPermission = func(mode string) (string, error) {
		tried = append(tried, mode)
		return "", errBoom
	}
	assert.False(t, cycleMode(app, true))
	assert.Equal(t, []string{string(api.ModeBypass), string(api.ModeDefault)}, tried)
}

// steerScript is an Input that types one steer message during each turn
// it watches, answered as AskSteer says.
type steerScript struct {
	*scriptInput
	answer func() (string, error)
	sent   chan struct{}
}

func (s *steerScript) WatchKeys(onKey func(string)) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		onKey("x")
	}()
	return func() { <-done }
}

func (s *steerScript) AskSteer(context.Context, string, string) (string, error) {
	return s.answer()
}

// Steer messages: cancelled, empty, blocked by a hook, or too late (sent
// as the next turn).
func TestREPLSteerOutcomes(t *testing.T) {
	cases := map[string]struct {
		text      string
		err       error
		steerErr  error
		want      []string
		wantTurns int
	}{
		"ctrl+c":   {err: context.Canceled, wantTurns: 1},
		"empty":    {text: "  ", want: []string{"Nothing sent."}, wantTurns: 1},
		"blocked":  {text: "rm it", steerErr: &api.BlockedError{Reason: "nope"}, want: []string{"Prompt blocked by hook: nope"}, wantTurns: 1},
		"too late": {text: "and docs", steerErr: api.ErrSteerTooLate, want: []string{"Sent.", "sending it now"}, wantTurns: 2},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			app, s := stubApp(t, "")
			s.sandboxSummary = func() []string { return []string{"sandbox: on"} }
			turns := recordTurns(s)
			s.steer = func(string) error { return c.steerErr }
			in := &steerScript{scriptInput: &scriptInput{steps: lines("work")}}
			first := true
			in.answer = func() (string, error) {
				if !first { // the late message's own turn
					return "", context.Canceled
				}
				first = false
				return c.text, c.err
			}
			app.Input = in
			app.Interrupts = make(chan os.Signal, 1)
			out := captureStdout(t, func() { require.NoError(t, RunREPL(context.Background(), app)) })
			assert.Contains(t, out, "just start typing", "the steering hint is shown")
			for _, w := range c.want {
				assert.Contains(t, out, w)
			}
			assert.Len(t, *turns, c.wantTurns)
		})
	}
}

// The printer renders notices, and Markdown when asked.
func TestPrinterNoticesAndMarkdown(t *testing.T) {
	var out syncBuffer
	p := NewPrinter(PrinterOptions{Out: &out, Markdown: true, Theme: "dark", Width: 60})
	require.NotNil(t, p.md)
	p.Begin()
	p.Handle(api.Event{Text: &api.Text{Text: "# Title\n\nSome **bold** text.\n\n"}})
	p.Handle(api.Event{Notice: &api.Notice{Text: "retrying"}})
	p.Handle(api.Event{Notice: &api.Notice{Text: "quota hit", Error: true}})
	p.Handle(api.Event{Text: &api.Text{Text: "tail"}})
	p.End()
	got := out.b.String()
	assert.Contains(t, got, "Title")
	assert.Contains(t, got, Dim+"retrying"+Reset)
	assert.Contains(t, got, Red+"quota hit"+Reset)
	assert.Contains(t, got, "tail")
	assert.NotContains(t, got, "**bold**", "Markdown is rendered")

	// Blank blocks print nothing.
	out.b.Reset()
	p.Handle(api.Event{Text: &api.Text{Text: "\n\n"}})
	p.End()
	assert.Empty(t, out.b.String())

	// The theme comes from the terminal's colours unless set.
	t.Setenv("COLORFGBG", "0;15")
	_, err := newMarkdownStream(&out, "", 80)
	assert.NoError(t, err)
	_, err = newMarkdownStream(&out, "no-such-theme", 80)
	assert.Error(t, err)
}

// The App lets the exit prompt stop its workspace's tasks.
func TestAppStopTask(t *testing.T) {
	app, s := stubApp(t, "")
	s.stopTask = func(id string) (api.TaskInfo, error) { return api.TaskInfo{ID: id, State: "stopped"}, nil }
	ti, err := app.StopTask("task-9")
	require.NoError(t, err)
	assert.Equal(t, "task-9", ti.ID)
}
