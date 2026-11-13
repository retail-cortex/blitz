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

// Package terminal runs the person's shell in a pseudo-terminal for the
// window's terminal tab (spec_062_terminal): one Manager a workspace, each
// Terminal keeping its recent output as scrollback that a watcher replays
// from an offset, so a window that reloads picks a terminal up as it was.
//
// A terminal is the person's, not the agent's: it runs outside the agent's
// sandbox and nothing of its text reaches a model or the logs (TRM-02).
package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/creack/pty"
)

// Limits (TRM-05).
const (
	// DefaultMax is how many terminals a workspace keeps at once.
	DefaultMax = 10
	// DefaultScrollback is how many bytes of output a terminal keeps.
	DefaultScrollback = 2 << 20
	// MaxWrite is the most input one write takes.
	MaxWrite = 64 << 10
	// maxSize is the most columns or rows a terminal takes.
	maxSize = 1000
)

// Errors a caller tells apart.
var (
	// ErrUnknown is a terminal that isn't there (or was closed).
	ErrUnknown = errors.New("no such terminal")
	// ErrTooMany is a terminal past the workspace's limit.
	ErrTooMany = errors.New("too many terminals")
	// ErrUnsupported is a platform without pseudo-terminals.
	ErrUnsupported = errors.New("terminals aren't supported on this system yet")
	// ErrClosed is a manager whose workspace has closed.
	ErrClosed = errors.New("the workspace's terminals are closed")
	// ErrNotReading is input the shell hasn't taken for WriteTimeout: what
	// runs in it isn't reading (a hung program, an ssh whose network went),
	// so its input is full (TRM-13a).
	ErrNotReading = errors.New("the terminal isn't taking input: what runs in it isn't reading")
	// ErrExited is input for a shell that has exited.
	ErrExited = errors.New("the terminal's shell has exited")
)

// WriteTimeout is how long input waits for room in the shell's input
// before the write fails with ErrNotReading (TRM-13a); a variable for
// tests.
var WriteTimeout = 5 * time.Second

// inputQueue is how many writes wait behind one the PTY is taking.
const inputQueue = 64

// killAfter is how long a closed terminal's shell has to end after its
// hangup before it's killed (TRM-03); a variable for tests.
var killAfter = 2 * time.Second

// drainFor is how long the output is read after the shell exits, when the
// PTY stays open (a background process holds it).
var drainFor = 200 * time.Millisecond

// Options are how a terminal starts.
type Options struct {
	// Dir is the folder it starts in (absolute).
	Dir string
	// Shell is the program to run; "" for the person's login shell.
	Shell string
	// Args are the shell's arguments; nil for a login shell's ("-l").
	Args []string
	// Env is its environment; nil for the service's (TRM-01's additions
	// are made either way).
	Env []string
	// Cols and Rows are its size; 0 for 80×24.
	Cols, Rows uint16
	// Version is Blitz's, as TERM_PROGRAM_VERSION.
	Version string
}

// Info is a terminal as the window lists it.
type Info struct {
	ID       string
	Dir      string
	Shell    string
	Started  time.Time
	Exited   bool
	ExitCode int
}

// Manager keeps a workspace's terminals.
type Manager struct {
	max        int
	scrollback int

	mu     sync.Mutex
	terms  map[string]*Terminal
	order  []string
	closed bool
}

// NewManager is a manager keeping at most max terminals, each with
// scrollback bytes of output (0 for each's default).
func NewManager(max, scrollback int) *Manager {
	if max <= 0 {
		max = DefaultMax
	}
	if scrollback <= 0 {
		scrollback = DefaultScrollback
	}
	return &Manager{max: max, scrollback: scrollback, terms: map[string]*Terminal{}}
}

// Open starts a terminal (TRM-01, TRM-10).
func (m *Manager) Open(o Options) (*Terminal, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	if len(m.terms) >= m.max {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: at most %d", ErrTooMany, m.max)
	}
	m.mu.Unlock()

	shell := o.Shell
	if shell == "" {
		shell = LoginShell()
	}
	args := o.Args
	if args == nil {
		args = []string{"-l"}
	}
	cmd := exec.Command(shell, args...)
	cmd.Dir = o.Dir
	cmd.Env = Environment(o.Env, o.Version)
	cols, rows := o.Cols, o.Rows
	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if errors.Is(err, pty.ErrUnsupported) {
		return nil, ErrUnsupported
	}
	if err != nil {
		return nil, err
	}
	t := &Terminal{
		info: Info{ID: newID(), Dir: o.Dir, Shell: shell, Started: time.Now()},
		pty:  f, cmd: cmd, limit: m.scrollback, done: make(chan struct{}), changed: make(chan struct{}),
		in: make(chan []byte, inputQueue),
	}
	m.mu.Lock()
	if m.closed { // closed while it started
		m.mu.Unlock()
		t.close()
		return nil, ErrClosed
	}
	m.terms[t.info.ID] = t
	m.order = append(m.order, t.info.ID)
	m.mu.Unlock()
	go t.read()
	go t.wait()
	go t.write()
	return t, nil
}

// Get is terminal id.
func (m *Manager) Get(id string) (*Terminal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.terms[id]
	if !ok {
		return nil, ErrUnknown
	}
	return t, nil
}

// List is the terminals, oldest first.
func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.terms[id].Info())
	}
	return out
}

// Close ends terminal id (TRM-03).
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	t, ok := m.terms[id]
	if ok {
		delete(m.terms, id)
		m.order = slices.DeleteFunc(m.order, func(x string) bool { return x == id })
	}
	m.mu.Unlock()
	if !ok {
		return ErrUnknown
	}
	t.close()
	return nil
}

// CloseAll ends every terminal; none opens after it (the workspace closed).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	m.closed = true
	terms := make([]*Terminal, 0, len(m.terms))
	for _, id := range m.order {
		terms = append(terms, m.terms[id])
	}
	m.terms, m.order = map[string]*Terminal{}, nil
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, t := range terms {
		wg.Go(t.close)
	}
	wg.Wait()
}

// Terminal is one shell in a pseudo-terminal.
type Terminal struct {
	pty   *os.File
	cmd   *exec.Cmd
	limit int

	mu      sync.Mutex
	info    Info
	buf     []byte // the last output, at most limit bytes
	end     int64  // the offset after buf's last byte, from the start
	changed chan struct{}
	readEOF bool
	done    chan struct{} // closed when the shell has exited and its output is read
	once    sync.Once
	in      chan []byte // input for the writer, in order (TRM-13a)
}

// Info is the terminal as the window lists it.
func (t *Terminal) Info() Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.info
}

// Write writes to the shell's input (TRM-13), through the terminal's
// writer: a PTY whose input is full blocks its writer, never the caller,
// who gets ErrNotReading once the queue has had no room for WriteTimeout
// (TRM-13a).
func (t *Terminal) Write(p []byte) error {
	if len(p) > MaxWrite {
		return fmt.Errorf("at most %d bytes a write", MaxWrite)
	}
	// Done first: the queue may have room once the writer has stopped,
	// and select picks at random.
	select {
	case <-t.done:
		return ErrExited
	default:
	}
	timer := time.NewTimer(WriteTimeout)
	defer timer.Stop()
	select {
	case t.in <- slices.Clone(p):
		return nil
	case <-t.done:
		return ErrExited
	case <-timer.C:
		return ErrNotReading
	}
}

// write gives the queued input to the PTY until the shell is done; a
// write the shell isn't reading blocks here, and ends when the PTY closes.
func (t *Terminal) write() {
	for {
		select {
		case p := <-t.in:
			if _, err := t.pty.Write(p); err != nil {
				return
			}
		case <-t.done:
			return
		}
	}
}

// Resize sets the terminal's size (TRM-11).
func (t *Terminal) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 || cols > maxSize || rows > maxSize {
		return fmt.Errorf("a size of %d×%d: 1 to %d each", cols, rows, maxSize)
	}
	return pty.Setsize(t.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

// Chunk is output a watcher is sent: Data ends at Offset (counted from the
// terminal's start); Skipped is output before Data that was no longer in
// the scrollback (older than it, or come and gone while the watcher was
// behind); Exited ends the stream with the shell's exit code.
type Chunk struct {
	Data     []byte
	Offset   int64
	Skipped  int64
	Exited   bool
	ExitCode int
}

// Watch sends the output after from, then each new output as it comes,
// until the shell exits (a last Chunk with Exited) or ctx ends (TRM-12).
func (t *Terminal) Watch(ctx context.Context, from int64, send func(Chunk) error) error {
	from = max(from, 0)
	for {
		t.mu.Lock()
		start := t.end - int64(len(t.buf))
		var c Chunk
		if from < start { // older than the scrollback, or a watcher that fell behind
			c.Skipped = start - from
			from = start
		}
		if from < t.end {
			c.Data = slices.Clone(t.buf[from-start:])
			from = t.end
		}
		c.Offset = from
		changed, exited := t.changed, t.readEOF && t.info.Exited
		code := t.info.ExitCode
		t.mu.Unlock()
		if len(c.Data) > 0 || c.Skipped > 0 {
			if err := send(c); err != nil {
				return err
			}
		}
		if exited {
			return send(Chunk{Offset: from, Exited: true, ExitCode: code})
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// Done is closed when the shell has exited and its output is read.
func (t *Terminal) Done() <-chan struct{} { return t.done }

// read keeps the shell's output as scrollback and wakes the watchers.
func (t *Terminal) read() {
	b := make([]byte, 32<<10)
	for {
		n, err := t.pty.Read(b)
		if n > 0 {
			t.mu.Lock()
			t.buf = append(t.buf, b[:n]...)
			if over := len(t.buf) - t.limit; over > 0 {
				t.buf = slices.Clone(t.buf[over:]) // a fresh array: the old one goes
			}
			t.end += int64(n)
			t.wake()
			t.mu.Unlock()
		}
		if err != nil {
			t.mu.Lock()
			t.readEOF = true
			t.wake()
			t.mu.Unlock()
			t.finish()
			return
		}
	}
}

// wait records the shell's exit code when it exits.
func (t *Terminal) wait() {
	err := t.cmd.Wait()
	code := 0
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		code = -1
	}
	t.mu.Lock()
	t.info.Exited, t.info.ExitCode = true, code
	t.wake()
	t.mu.Unlock()
	t.finish()
}

// finish closes done once the shell has exited and its output is read. A
// process the shell left running in the background can hold the PTY open,
// so the read never ends by itself: a moment after the shell exits, the
// PTY is closed, which ends it with what was read.
func (t *Terminal) finish() {
	t.mu.Lock()
	exited, eof := t.info.Exited, t.readEOF
	t.mu.Unlock()
	if exited && !eof {
		time.AfterFunc(drainFor, func() {
			t.mu.Lock()
			eof := t.readEOF
			t.mu.Unlock()
			if !eof {
				_ = t.pty.Close()
			}
		})
		return
	}
	if exited && eof {
		t.once.Do(func() { close(t.done) })
	}
}

// wake tells the watchers there's news; t.mu is held.
func (t *Terminal) wake() {
	close(t.changed)
	t.changed = make(chan struct{})
}

// close hangs the shell up, kills it if it hasn't ended in killAfter, and
// waits for it.
func (t *Terminal) close() {
	hangUp(t.cmd)
	select {
	case <-t.done:
	case <-time.After(killAfter):
		kill(t.cmd)
		<-t.done
	}
	_ = t.pty.Close()
}

// LoginShell is the person's shell: $SHELL, else zsh on macOS and bash
// elsewhere (TRM-01).
func LoginShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/bash"
}

// Environment is env (the service's when nil) with what a terminal adds
// (TRM-01): its type and colours, Blitz as its program, and a UTF-8
// locale when none is set.
func Environment(env []string, version string) []string {
	if env == nil {
		env = os.Environ()
	}
	out := make([]string, 0, len(env)+5)
	hasLang := false
	for _, kv := range env {
		switch key(kv) {
		case "TERM", "COLORTERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION":
			continue
		case "LANG":
			hasLang = kv != "LANG="
		}
		out = append(out, kv)
	}
	out = append(out, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=Blitz", "TERM_PROGRAM_VERSION="+version)
	if !hasLang {
		out = append(out, "LANG=en_US.UTF-8")
	}
	return out
}

func key(kv string) string {
	for i := range len(kv) {
		if kv[i] == '=' {
			return kv[:i]
		}
	}
	return kv
}

func newID() string {
	b := make([]byte, 6)
	_, _ = io.ReadFull(rand.Reader, b)
	return "term-" + hex.EncodeToString(b)
}
