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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ergochat/readline"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTerminal runs the real line editor on a pipe. Each write reaches the
// editor as one read, as a keypress does from a terminal.
type fakeTerminal struct {
	in  *TerminalInput
	w   *io.PipeWriter
	out *cprWriter
}

// cprWriter is the fake terminal's screen. It answers the editor's cursor
// position queries, as a terminal does.
type cprWriter struct {
	syncBuffer
	w        *io.PipeWriter
	answered chan struct{} // one per query answered
}

func (c *cprWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("\x1b[6n")) {
		go func() {
			c.w.Write([]byte("\x1b[1;1R"))
			c.answered <- struct{}{}
		}()
	}
	return c.syncBuffer.Write(p)
}

func newFakeTerminal(t *testing.T) *fakeTerminal {
	t.Helper()
	r, w := io.Pipe()
	out := &cprWriter{w: w, answered: make(chan struct{}, 16)}
	in, err := newTerminalInput(TerminalOptions{}, &readline.Config{
		Stdin:              r,
		Stdout:             out,
		Stderr:             out,
		FuncIsTerminal:     func() bool { return true },
		FuncMakeRaw:        func() error { return nil },
		FuncExitRaw:        func() error { return nil },
		FuncGetSize:        func() (int, int) { return 80, 24 },
		FuncOnWidthChanged: func(func()) {},
	})
	require.NoError(t, err)
	t.Cleanup(func() { w.Close(); in.Close() })
	return &fakeTerminal{in: in, w: w, out: out}
}

// keys types each string as one keypress (or paste), in order, once the
// next read has started.
func (f *fakeTerminal) keys(t *testing.T, keys ...string) {
	t.Helper()
	go func() {
		<-f.out.answered
		for _, k := range keys {
			if _, err := f.w.Write([]byte(k)); err != nil {
				return
			}
		}
	}()
}

func (f *fakeTerminal) output() string {
	f.out.mu.Lock()
	defer f.out.mu.Unlock()
	return f.out.b.String()
}

type readResult struct {
	line string
	err  error
}

func within(t *testing.T, read func() (string, error)) (string, error) {
	t.Helper()
	ch := make(chan readResult, 1)
	go func() {
		line, err := read()
		ch <- readResult{line, err}
	}()
	select {
	case r := <-ch:
		return r.line, r.err
	case <-time.After(5 * time.Second):
		t.Fatal("the read did not finish")
		return "", nil
	}
}

func TestEscAtThePrompt(t *testing.T) {
	f := newFakeTerminal(t)
	ctx := context.Background()

	// One Esc does nothing; typing goes on.
	f.keys(t, "ab", "\x1b", "c\r")
	line, err := within(t, func() (string, error) { return f.in.ReadInput(ctx, "> ") })
	require.NoError(t, err, "single Esc: %q", line)
	require.Equal(t, "abc", line, "single Esc: %q %v", line, err)
	// Esc Esc clears the line.
	f.keys(t, "draft", "\x1b", "\x1b", "new\r")
	line, err = within(t, func() (string, error) { return f.in.ReadInput(ctx, "> ") })
	require.NoError(t, err, "Esc Esc: %q", line)
	require.Equal(t, "new", line, "Esc Esc: %q %v", line, err)
	// An arrow key's escape sequence is still an arrow key: left, then insert.
	f.keys(t, "ac", "\x1b[D", "b\r")
	line, err = within(t, func() (string, error) { return f.in.ReadInput(ctx, "> ") })
	require.NoError(t, err, "arrow key: %q", line)
	require.Equal(t, "abc", line, "arrow key: %q %v", line, err)
}

// At an approval Esc is Ctrl+C: the turn's interrupt handler runs.
func TestEscAtAnApprovalInterrupts(t *testing.T) {
	f := newFakeTerminal(t)
	interrupted := make(chan struct{}, 1)
	f.in.SetInterruptHandler(func() { interrupted <- struct{}{} })
	f.keys(t, "\x1b")
	_, err := within(t, func() (string, error) { return f.in.Ask(context.Background(), "Allow? ") })
	require.ErrorIs(t, err, context.Canceled, "Esc at an approval: %v", err)
	select {
	case <-interrupted:
	default:
		t.Fatal("the interrupt handler didn't run")
	}
}

// In the steer prompt Esc drops the message, not the turn.
func TestEscCancelsASteerMessage(t *testing.T) {
	f := newFakeTerminal(t)
	f.in.SetInterruptHandler(func() { t.Error("Esc in the steer prompt interrupted the turn") })
	f.keys(t, " more", "\x1b")
	line, err := within(t, func() (string, error) { return f.in.AskSteer(context.Background(), "steer> ", "fix") })
	require.NoError(t, err, "Esc in the steer prompt: %q", line)
	require.Equal(t, "", line, "Esc in the steer prompt: %q %v", line, err)
}

func TestShiftTabCyclesTheMode(t *testing.T) {
	f := newFakeTerminal(t)
	calls := 0
	f.in.SetPromptKeys(PromptKeys{CycleMode: func() string { calls++; return "[plan] > " }})
	f.keys(t, "hi", "\x1b[Z", "!\r")
	line, err := within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") })
	require.NoError(t, err, "line %q", line)
	require.Equal(t, "hi!", line, "line %q %v", line, err)
	require.Equal(t, 1, calls, "calls %d, output %q", calls, f.output())
	require.Contains(t, f.output(), "[plan] > ", "calls %d, output %q", calls, f.output())
	// Not at a question.
	f.keys(t, "\x1b[Z", "y\r")
	line, _ = within(t, func() (string, error) { return f.in.Ask(context.Background(), "? ") })
	require.Equal(t, "y", line, "Shift+Tab at a question: %q, calls %d", line, calls)
	require.Equal(t, 1, calls, "Shift+Tab at a question: %q, calls %d", line, calls)
}

func TestCtrlGEditsAndSubmits(t *testing.T) {
	f := newFakeTerminal(t)
	var got string
	f.in.edit = func(text string) (string, error) { got = text; return text + "\nsecond line", nil }
	f.keys(t, "draft", "\x07")
	line, err := within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") })
	require.NoError(t, err, "line %q err %v, editor got %q", line, err, got)
	require.Equal(t, "draft\nsecond line", line, "line %q err %v, editor got %q", line, err, got)
	require.Equal(t, "draft", got, "line %q err %v, editor got %q", line, err, got)
	assert.Contains(t, f.output(), "> draft\nsecond line", "the edited text isn't echoed: %q", f.output())

	// No editor: a note, and the typed line comes back to finish.
	f.in.edit = func(string) (string, error) { return "", errNoEditor }
	f.keys(t, "draft", "\x07")
	go func() {
		for !strings.Contains(f.output(), "$EDITOR") {
			time.Sleep(5 * time.Millisecond)
		}
		f.w.Write([]byte("!\r"))
	}()
	line, err = within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") })
	require.NoError(t, err, "without an editor: %q %v\n%s", line, err, f.output())
	require.Equal(t, "draft!", line, "without an editor: %q %v\n%s", line, err, f.output())
	require.Contains(t, f.output(), "$EDITOR", "without an editor: %q %v\n%s", line, err, f.output())
}

func TestEditText(t *testing.T) {
	script := filepath.Join(t.TempDir(), "ed")
	os.WriteFile(script, []byte("#!/bin/sh\nprintf ' and more\\n\\n' >> \"$1\"\n"), 0o755)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script)
	got, err := editText("start", nil, io.Discard, io.Discard)
	require.NoError(t, err, "%q", got)
	require.Equal(t, "start and more", got, "%q %v", got, err)
	t.Setenv("VISUAL", "false")
	_, err = editText("x", nil, io.Discard, io.Discard)
	assert.Error(t, err, "a failing $VISUAL (which wins over $EDITOR) wasn't reported")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	_, err = editText("x", nil, io.Discard, io.Discard)
	assert.ErrorIs(t, err, errNoEditor, "no editor: %v", err)
}

func TestKeyWatcherEscInterrupts(t *testing.T) {
	keys := newChanKeys()
	escs := make(chan struct{}, 4)
	w := startKeyWatcher(keys, func(string) { t.Error("Esc opened the steer prompt") }, func() { escs <- struct{}{} })
	defer w.close()
	keys.in <- []byte("\x1b[A") // an arrow key: ignored
	keys.in <- []byte("\x1b")
	select {
	case <-escs:
	case <-time.After(2 * time.Second):
		t.Fatal("Esc did not interrupt")
	}
	assert.Len(t, escs, 0, "the arrow key interrupted too")
}

func TestNextMode(t *testing.T) {
	for _, c := range []struct {
		from   api.PermissionMode
		bypass bool
		want   api.PermissionMode
	}{
		{api.ModeDefault, false, api.ModeAcceptEdits},
		{api.ModeAcceptEdits, false, api.ModeAuto},
		{api.ModeAuto, false, api.ModePlan},
		{api.ModePlan, false, api.ModeDefault},
		{api.ModePlan, true, api.ModeBypass},
		{api.ModeBypass, true, api.ModeDefault},
		{api.ModeDontAsk, false, api.ModeDefault},
	} {
		got := nextMode(string(c.from), c.bypass)
		assert.Equal(t, string(c.want), got, "%s (bypass %v) -> %s, want %s", c.from, c.bypass, got, c.want)
	}
}

// Ctrl+G in the steer prompt edits the pre-filled text too.
func TestCtrlGEditsAPrefilledLine(t *testing.T) {
	f := newFakeTerminal(t)
	var got string
	f.in.edit = func(text string) (string, error) { got = text; return "edited", nil }
	f.keys(t, "\x07")
	line, err := within(t, func() (string, error) { return f.in.AskSteer(context.Background(), "steer> ", "fix it") })
	require.NoError(t, err, "line %q err %v, editor got %q", line, err, got)
	require.Equal(t, "edited", line, "line %q err %v, editor got %q", line, err, got)
	require.Equal(t, "fix it", got, "line %q err %v, editor got %q", line, err, got)
}
