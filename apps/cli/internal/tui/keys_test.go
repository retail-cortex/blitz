package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/ergochat/readline"
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
	if err != nil {
		t.Fatal(err)
	}
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
	if line, err := within(t, func() (string, error) { return f.in.ReadInput(ctx, "> ") }); err != nil || line != "abc" {
		t.Fatalf("single Esc: %q %v", line, err)
	}
	// Esc Esc clears the line.
	f.keys(t, "draft", "\x1b", "\x1b", "new\r")
	if line, err := within(t, func() (string, error) { return f.in.ReadInput(ctx, "> ") }); err != nil || line != "new" {
		t.Fatalf("Esc Esc: %q %v", line, err)
	}
	// An arrow key's escape sequence is still an arrow key: left, then insert.
	f.keys(t, "ac", "\x1b[D", "b\r")
	if line, err := within(t, func() (string, error) { return f.in.ReadInput(ctx, "> ") }); err != nil || line != "abc" {
		t.Fatalf("arrow key: %q %v", line, err)
	}
}

// At an approval Esc is Ctrl+C: the turn's interrupt handler runs.
func TestEscAtAnApprovalInterrupts(t *testing.T) {
	f := newFakeTerminal(t)
	interrupted := make(chan struct{}, 1)
	f.in.SetInterruptHandler(func() { interrupted <- struct{}{} })
	f.keys(t, "\x1b")
	if _, err := within(t, func() (string, error) { return f.in.Ask(context.Background(), "Allow? ") }); !errors.Is(err, context.Canceled) {
		t.Fatalf("Esc at an approval: %v", err)
	}
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
	if line, err := within(t, func() (string, error) { return f.in.AskSteer(context.Background(), "steer> ", "fix") }); err != nil || line != "" {
		t.Fatalf("Esc in the steer prompt: %q %v", line, err)
	}
}

func TestShiftTabCyclesTheMode(t *testing.T) {
	f := newFakeTerminal(t)
	calls := 0
	f.in.SetPromptKeys(PromptKeys{CycleMode: func() string { calls++; return "[plan] > " }})
	f.keys(t, "hi", "\x1b[Z", "!\r")
	if line, err := within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") }); err != nil || line != "hi!" {
		t.Fatalf("line %q %v", line, err)
	}
	if calls != 1 || !strings.Contains(f.output(), "[plan] > ") {
		t.Fatalf("calls %d, output %q", calls, f.output())
	}
	// Not at a question.
	f.keys(t, "\x1b[Z", "y\r")
	if line, _ := within(t, func() (string, error) { return f.in.Ask(context.Background(), "? ") }); line != "y" || calls != 1 {
		t.Fatalf("Shift+Tab at a question: %q, calls %d", line, calls)
	}
}

func TestCtrlGEditsAndSubmits(t *testing.T) {
	f := newFakeTerminal(t)
	var got string
	f.in.edit = func(text string) (string, error) { got = text; return text + "\nsecond line", nil }
	f.keys(t, "draft", "\x07")
	line, err := within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") })
	if err != nil || line != "draft\nsecond line" || got != "draft" {
		t.Fatalf("line %q err %v, editor got %q", line, err, got)
	}
	if !strings.Contains(f.output(), "> draft\nsecond line") {
		t.Errorf("the edited text isn't echoed: %q", f.output())
	}

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
	if err != nil || line != "draft!" || !strings.Contains(f.output(), "$EDITOR") {
		t.Fatalf("without an editor: %q %v\n%s", line, err, f.output())
	}
}

func TestEditText(t *testing.T) {
	script := filepath.Join(t.TempDir(), "ed")
	os.WriteFile(script, []byte("#!/bin/sh\nprintf ' and more\\n\\n' >> \"$1\"\n"), 0o755)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script)
	got, err := editText("start", nil, io.Discard, io.Discard)
	if err != nil || got != "start and more" {
		t.Fatalf("%q %v", got, err)
	}
	t.Setenv("VISUAL", "false")
	if _, err := editText("x", nil, io.Discard, io.Discard); err == nil {
		t.Error("a failing $VISUAL (which wins over $EDITOR) wasn't reported")
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if _, err := editText("x", nil, io.Discard, io.Discard); !errors.Is(err, errNoEditor) {
		t.Errorf("no editor: %v", err)
	}
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
	if len(escs) != 0 {
		t.Error("the arrow key interrupted too")
	}
}

func TestNextMode(t *testing.T) {
	for _, c := range []struct {
		from   api.PermissionMode
		bypass bool
		want   api.PermissionMode
	}{
		{api.ModeDefault, false, api.ModeAcceptEdits},
		{api.ModeAcceptEdits, false, api.ModePlan},
		{api.ModePlan, false, api.ModeDefault},
		{api.ModePlan, true, api.ModeBypass},
		{api.ModeBypass, true, api.ModeDefault},
		{api.ModeDontAsk, false, api.ModeDefault},
	} {
		if got := nextMode(string(c.from), c.bypass); got != string(c.want) {
			t.Errorf("%s (bypass %v) -> %s, want %s", c.from, c.bypass, got, c.want)
		}
	}
}

// Ctrl+G in the steer prompt edits the pre-filled text too.
func TestCtrlGEditsAPrefilledLine(t *testing.T) {
	f := newFakeTerminal(t)
	var got string
	f.in.edit = func(text string) (string, error) { got = text; return "edited", nil }
	f.keys(t, "\x07")
	if line, err := within(t, func() (string, error) { return f.in.AskSteer(context.Background(), "steer> ", "fix it") }); err != nil || line != "edited" || got != "fix it" {
		t.Fatalf("line %q err %v, editor got %q", line, err, got)
	}
}
