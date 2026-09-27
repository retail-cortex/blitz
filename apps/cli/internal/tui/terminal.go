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
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ergochat/readline"
)

// TerminalInput is an Input backed by a line editor: arrow-key editing,
// persistent history, reverse search (Ctrl+R) and tab completion. Every
// terminal read goes through it so prompts never compete for stdin.
type TerminalInput struct {
	rl   *readline.Instance
	turn chan struct{}

	stdin    *escReader
	edit     func(text string) (string, error) // $EDITOR (Ctrl+G); replaced in tests
	pickTerm func() keyTerm                    // raw keys for pickers; replaced in tests

	mu          sync.Mutex
	onInterrupt func()
	keys        *keyWatcher // set while a turn is running (steering)
	ks          *keyState   // the read in progress
	promptKeys  PromptKeys
	nextInput   string // the next entry's starting text
}

// WatchKeys lets the user steer the running turn: typing (or Ctrl+T) calls
// onKey with the typed text, from a watcher goroutine. Prompts during the
// turn pause the watcher automatically. Call stop when the turn ends; it
// waits for an open steer prompt to be finished.
func (t *TerminalInput) WatchKeys(onKey func(prefill string)) (stop func()) {
	w := startKeyWatcher(newTTYKeys(int(os.Stdin.Fd())), onKey, t.interrupt)
	t.mu.Lock()
	t.keys = w
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		t.keys = nil
		t.mu.Unlock()
		w.close()
	}
}

// AskSteer reads a steer message pre-filled with what the user already
// typed. It is called from the key watcher, which has handed over the
// terminal, so it doesn't pause the watcher itself.
func (t *TerminalInput) AskSteer(ctx context.Context, prompt, prefill string) (string, error) {
	return t.readLine(ctx, prompt, readSteer, prefill)
}

// interrupt does what Ctrl+C does during a turn (Esc while the agent works).
func (t *TerminalInput) interrupt() {
	t.mu.Lock()
	f := t.onInterrupt
	t.mu.Unlock()
	if f != nil {
		f()
	}
}

// SetInterruptHandler sets what Ctrl+C does while a prompt is shown during a
// turn (e.g. an approval). The terminal is in raw mode then, so Ctrl+C is a
// keypress rather than SIGINT; the REPL uses this to cancel the turn.
func (t *TerminalInput) SetInterruptHandler(f func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onInterrupt = f
}

// TerminalOptions configure a TerminalInput.
type TerminalOptions struct {
	HistoryFile string
	HistorySize int
	Completer   readline.AutoCompleter
}

// NewTerminalInput creates the line editor on stdin/stdout.
func NewTerminalInput(o TerminalOptions) (*TerminalInput, error) {
	return newTerminalInput(o, &readline.Config{})
}

// newTerminalInput creates the line editor with base's terminal settings
// (tests supply a pipe and a fake terminal).
func newTerminalInput(o TerminalOptions, base *readline.Config) (*TerminalInput, error) {
	if o.HistoryFile != "" {
		if err := os.MkdirAll(filepath.Dir(o.HistoryFile), 0o700); err != nil {
			return nil, err
		}
		// Create owner-only before the editor opens it: prompts can contain secrets.
		if f, err := os.OpenFile(o.HistoryFile, os.O_CREATE|os.O_RDONLY, 0o600); err == nil {
			f.Close()
		}
	}
	in := base.Stdin
	if in == nil {
		in = os.Stdin
	}
	t := &TerminalInput{turn: make(chan struct{}, 1), stdin: &escReader{in: in}}
	t.edit = func(text string) (string, error) { return editText(text, os.Stdin, os.Stdout, os.Stderr) }
	t.pickTerm = func() keyTerm { return newPickerKeys(int(os.Stdin.Fd())) }
	cfg := *base
	cfg.Stdin = t.stdin
	cfg.HistoryFile = o.HistoryFile
	cfg.HistoryLimit = o.HistorySize
	cfg.DisableAutoSaveHistory = true // multi-line entries are saved whole
	cfg.HistorySearchFold = true
	cfg.AutoComplete = o.Completer
	cfg.InterruptPrompt = "^C"
	cfg.EOFPrompt = ""
	cfg.FuncFilterInputRune = t.filterKey
	cfg.Listener = t.listen
	rl, err := readline.NewFromConfig(&cfg)
	if err != nil {
		return nil, err
	}
	t.rl = rl
	return t, nil
}

// Close restores the terminal.
func (t *TerminalInput) Close() error { return t.rl.Close() }

// Ask prints text (which may span lines) and reads one answer without
// recording it in history.
func (t *TerminalInput) Ask(ctx context.Context, text string) (string, error) {
	return t.read(ctx, text, readAsk)
}

// read pauses the key watcher (if a turn is running) so the prompt gets the
// keyboard, then reads a line.
func (t *TerminalInput) read(ctx context.Context, text string, kind readKind) (string, error) {
	t.mu.Lock()
	keys := t.keys
	t.mu.Unlock()
	if keys != nil {
		resume, err := keys.pause(ctx)
		if err != nil {
			return "", err
		}
		defer resume()
	}
	return t.readLine(ctx, text, kind, "")
}

// ReadInput reads a REPL entry (multi-line aware) and saves it to history.
func (t *TerminalInput) ReadInput(ctx context.Context, prompt string) (string, error) {
	entry, err := readMultiline(ctx, prompt, func(ctx context.Context, p string) (string, error) {
		kind := readEntry
		if p == continuationPrompt {
			kind = readContinue
		}
		return t.read(ctx, p, kind)
	})
	if err == nil && strings.TrimSpace(entry) != "" {
		_ = t.rl.SaveToHistory(entry)
	}
	return entry, err
}

func (t *TerminalInput) readLine(ctx context.Context, text string, kind readKind, prefill string) (string, error) {
	select {
	case t.turn <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-t.turn }()

	// The editor redraws only the last prompt line; print the rest first.
	prompt := text
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		io.WriteString(t.rl.Stdout(), text[:i+1])
		prompt = text[i+1:]
	}
	if kind != readEntry && kind != readContinue {
		t.rl.DisableHistory()
		defer t.rl.EnableHistory()
	}
	// The editor can't abandon a read; closing it is the only way to unblock,
	// which is fine because cancellation here means the process is exiting.
	stop := context.AfterFunc(ctx, func() { t.rl.Close() })
	defer stop()
	defer func() {
		t.mu.Lock()
		t.ks = nil
		t.mu.Unlock()
	}()

	t.rl.SetPrompt(prompt)
	if kind == readEntry && prefill == "" {
		t.mu.Lock()
		prefill, t.nextInput = t.nextInput, ""
		t.mu.Unlock()
	}
	for {
		ks := &keyState{kind: kind, line: prefill}
		t.mu.Lock()
		t.ks = ks
		t.mu.Unlock()
		var line string
		var err error
		if prefill != "" {
			line, err = t.rl.ReadLineWithDefault(prefill)
		} else {
			line, err = t.rl.ReadLine()
		}
		switch {
		case errors.Is(err, readline.ErrInterrupt):
			t.interrupt()
			return "", context.Canceled // Ctrl+C: same meaning as SIGINT at the prompt
		case err != nil && ctx.Err() != nil:
			return "", ctx.Err()
		}
		t.mu.Lock()
		act, saved := ks.action, ks.saved
		t.mu.Unlock()
		if err == nil && act == actionRewind {
			return "", ErrRewindKey
		}
		if err != nil || act != actionEditor {
			return line, err
		}
		// Ctrl+G: submit what the editor saves, or go back to the line.
		if edited, ok := t.runEditor(t.rl.GetConfig().Prompt, saved); ok {
			return edited, nil
		}
		prefill = saved
	}
}

// Completer completes slash commands, their arguments, and @path references
// to files in the workspace.
type Completer struct {
	mu        sync.RWMutex
	commands  map[string][]string        // command -> static argument choices
	dynamic   map[string]func() []string // command -> argument choices computed on demand
	workspace string
}

// NewCompleter creates a completer rooted at workspace.
func NewCompleter(workspace string) *Completer {
	return &Completer{commands: map[string][]string{}, dynamic: map[string]func() []string{}, workspace: workspace}
}

// Command registers a slash command (without "/") and static argument choices.
func (c *Completer) Command(name string, args ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commands[name] = args
}

// Dynamic registers a function supplying argument choices for a command.
func (c *Completer) Dynamic(name string, f func() []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.commands[name]; !ok {
		c.commands[name] = nil
	}
	c.dynamic[name] = f
}

// Do implements readline.AutoCompleter: it returns candidate suffixes for the
// word before the cursor and the length of that word.
func (c *Completer) Do(line []rune, pos int) ([][]rune, int) {
	before := string(line[:pos])
	words := strings.Fields(before)
	endsWithSpace := strings.HasSuffix(before, " ")
	current := ""
	if !endsWithSpace && len(words) > 0 {
		current = words[len(words)-1]
	}

	var candidates []string
	switch {
	case strings.HasPrefix(before, "/") && len(words) <= 1 && !endsWithSpace:
		c.mu.RLock()
		for name := range c.commands {
			candidates = append(candidates, "/"+name+" ")
		}
		c.mu.RUnlock()
	case strings.HasPrefix(current, "@"):
		candidates = c.paths(current[1:])
		for i := range candidates {
			candidates[i] = "@" + candidates[i]
		}
	case strings.HasPrefix(before, "/") && len(words) >= 1 && (len(words) == 1 && endsWithSpace || len(words) == 2 && !endsWithSpace):
		cmd := strings.TrimPrefix(words[0], "/")
		c.mu.RLock()
		candidates = append(candidates, c.commands[cmd]...)
		if f := c.dynamic[cmd]; f != nil {
			c.mu.RUnlock()
			candidates = append(candidates, f()...)
		} else {
			c.mu.RUnlock()
		}
	}
	return suffixes(candidates, current), len([]rune(current))
}

func suffixes(candidates []string, prefix string) [][]rune {
	sort.Strings(candidates)
	var out [][]rune
	seen := map[string]bool{}
	for _, cand := range candidates {
		if strings.HasPrefix(cand, prefix) && cand != prefix && !seen[cand] {
			seen[cand] = true
			out = append(out, []rune(cand[len(prefix):]))
		}
	}
	return out
}

// paths lists workspace entries matching partial ("src/ma" -> "src/main.go").
// Hidden entries are offered only when the partial name starts with ".".
func (c *Completer) paths(partial string) []string {
	if strings.Contains(partial, "..") || filepath.IsAbs(partial) {
		return nil
	}
	dir, base := filepath.Split(partial)
	entries, err := os.ReadDir(filepath.Join(c.workspace, dir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		if !strings.HasPrefix(name, base) {
			continue
		}
		p := dir + name
		if e.IsDir() {
			p += "/"
		}
		out = append(out, p)
		if len(out) >= 200 {
			break
		}
	}
	return out
}
