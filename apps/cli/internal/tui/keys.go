package tui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ergochat/readline"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// keyEsc stands for a lone Esc keypress. The line editor reads Esc as the
// start of an escape sequence and waits for the rest, so escReader turns an
// Esc that arrives on its own into this private-use rune first.
const keyEsc = ''

// Line-editor keys the REPL gives meaning to.
const (
	keyCtrlG    = readline.CharBell // Ctrl+G: edit the input in $EDITOR
	keyShiftTab = readline.MetaShiftTab
)

// Bytes injected to steer the line editor from a key handler, which can't
// change the line or end the read itself.
const (
	injectClear     = "\x05\x15" // Ctrl+E, Ctrl+U: to the end, then delete to the start
	injectSubmit    = injectClear + "\r"
	injectInterrupt = "\x03"
)

// doubleEscWindow is how soon a second Esc must follow the first.
const doubleEscWindow = 600 * time.Millisecond

// escReader is the line editor's stdin. A read that returns just an Esc
// byte is a keypress (a terminal sends escape sequences in one write), so it
// becomes keyEsc. Handlers can also inject bytes, which are read before
// the terminal's; the editor reads only when it wants a key, so injected
// bytes are the next input.
type escReader struct {
	in io.Reader

	mu     sync.Mutex
	inject []byte
}

func (r *escReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if len(r.inject) > 0 {
		n := copy(p, r.inject)
		r.inject = r.inject[n:]
		r.mu.Unlock()
		return n, nil
	}
	r.mu.Unlock()
	n, err := r.in.Read(p)
	if n == 1 && p[0] == 0x1b && len(p) >= 3 {
		n = copy(p, string(keyEsc))
	}
	return n, err
}

func (r *escReader) push(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inject = append(r.inject, s...)
}

// PromptKeys are what REPL-only keys do at the main prompt.
type PromptKeys struct {
	// CycleMode switches to the next permission mode (Shift+Tab) and
	// returns the new prompt, or "" to leave it.
	CycleMode func() string
}

// readKind says which keys a read handles.
type readKind int

const (
	readAsk      readKind = iota // approvals, questions: Esc is Ctrl+C
	readEntry                    // the REPL prompt: all the keys
	readSteer                    // a message during a turn: Esc cancels it
	readContinue                 // a continuation line of an entry
)

// action is what a key asked for when it ended a read.
type action int

const (
	actionNone   action = iota
	actionEditor        // Ctrl+G: edit the line in $EDITOR
	actionRewind        // Esc Esc at an empty prompt: /rewind
)

// ErrRewindKey is what ReadInput returns when Esc Esc was pressed at an
// empty prompt: the REPL opens /rewind.
var ErrRewindKey = errors.New("rewind requested")

// keyState is one read's view of the keys.
type keyState struct {
	kind    readKind
	line    string // the line as last seen by the listener
	lastEsc time.Time
	action  action
	saved   string // the line when the action was chosen
}

// filterKey handles the REPL's keys before the line editor sees them. It
// runs on the reading goroutine; false drops the key.
func (t *TerminalInput) filterKey(r rune) (rune, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ks := t.ks
	if ks == nil {
		if r == keyEsc {
			return r, false
		}
		return r, true
	}
	switch r {
	case keyEsc:
		switch ks.kind {
		case readAsk:
			t.stdin.push(injectInterrupt)
		case readSteer:
			t.stdin.push(injectSubmit)
			ks.line = ""
		case readEntry, readContinue:
			now := time.Now()
			if now.Sub(ks.lastEsc) < doubleEscWindow {
				ks.lastEsc = time.Time{}
				switch {
				case ks.line != "": // Esc Esc clears the line
					t.stdin.push(injectClear)
				case ks.kind == readEntry: // or, when empty, opens /rewind
					ks.action = actionRewind
					t.stdin.push(injectSubmit)
				}
				return r, false
			}
			ks.lastEsc = now
		}
		return r, false
	case keyCtrlG:
		if ks.kind == readAsk || ks.kind == readContinue {
			return r, true
		}
		ks.action, ks.saved = actionEditor, ks.line
		t.stdin.push(injectSubmit)
		return r, false
	case keyShiftTab:
		if ks.kind == readEntry && t.promptKeys.CycleMode != nil {
			if p := t.promptKeys.CycleMode(); p != "" {
				t.rl.SetPrompt(p)
			}
		}
		return r, false
	}
	return r, true
}

// listen keeps the line the user has typed so far.
func (t *TerminalInput) listen(line []rune, _ int, key rune) ([]rune, int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ks != nil && key != 0 { // 0: the read is starting, before any prefill
		t.ks.line = string(line)
	}
	return nil, 0, false
}

// SetNextInput puts text in the next REPL prompt's line, to edit or send
// (a rewound prompt).
func (t *TerminalInput) SetNextInput(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextInput = text
}

// SetPromptKeys sets what the REPL's own keys do at the main prompt.
func (t *TerminalInput) SetPromptKeys(k PromptKeys) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.promptKeys = k
}

// errNoEditor means neither $VISUAL nor $EDITOR is set.
var errNoEditor = errors.New("no editor")

// editText opens text in $VISUAL or $EDITOR and returns what was saved,
// without a final newline.
func editText(text string, stdin io.Reader, stdout, stderr io.Writer) (string, error) {
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		return "", errNoEditor
	}
	f, err := os.CreateTemp("", "blitz-prompt-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	// The editor setting may carry arguments ("code --wait"); it is the
	// user's own command, run through the shell like git does.
	cmd := exec.Command("/bin/sh", "-c", editor+` "$1"`, "sh", f.Name())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w", editor, err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		return "", err
	}
	return string(bytes.TrimRight(b, "\r\n")), nil
}

// runEditor edits line for the prompt and returns the text to submit.
// The text is echoed after the prompt, so the transcript shows it. On
// failure (or an empty file) it says so and returns ok false.
func (t *TerminalInput) runEditor(prompt, line string) (string, bool) {
	out := t.rl.Stdout()
	text, err := t.edit(line)
	switch {
	case errors.Is(err, errNoEditor):
		fmt.Fprintf(out, "%s%s%s\n", Yellow, i18n.T("editor.none"), Reset)
		return "", false
	case err != nil:
		fmt.Fprintf(out, "%s✗ %s%s\n", Red, i18n.T("editor.failed", "error", safe(err.Error())), Reset)
		return "", false
	case strings.TrimSpace(text) == "":
		fmt.Fprintf(out, "%s%s%s\n", Dim, i18n.T("editor.empty"), Reset)
		return "", false
	}
	fmt.Fprintf(out, "%s%s\n", prompt, safe(text))
	return text, true
}
