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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The picker's state: moving in an empty list, scrolling back up, paging,
// and clearing the filter.
func TestPickStateEdges(t *testing.T) {
	labels := make([]string, 25)
	for i := range labels {
		labels[i] = fmt.Sprintf("item %02d", i)
	}
	st := newPickState(items(labels...), 24)
	assert.Equal(t, 15, st.top, "the current item is scrolled into view")
	st.handle(pickKey{kind: pkPageUp})
	assert.Equal(t, 14, st.cursor)
	assert.Equal(t, 14, st.top, "moving above the view scrolls up")
	st.handle(pickKey{kind: pkChar, r: 'z'})
	require.Empty(t, st.visible)
	st.handle(pickKey{kind: pkDown}) // nothing to move to
	done, _ := st.handle(pickKey{kind: pkEnter})
	assert.Equal(t, pickOpen, done, "Enter with nothing shown")
	var sb strings.Builder
	st.render(&sb, "Pick", 5) // too narrow: drawn at 80
	assert.Contains(t, sb.String(), "(nothing matches)")
	st.handle(pickKey{kind: pkClear})
	assert.Len(t, st.visible, 25)
}

// parsePickKeys reads page down, a lone Esc inside other input, and an
// escape sequence cut short.
func TestParsePickKeysEdges(t *testing.T) {
	cases := map[string]struct {
		in   string
		want []pickKeyKind
	}{
		"page down":       {"\x1b[6~", []pickKeyKind{pkPageDown}},
		"alt key":         {"\x1bxa", []pickKeyKind{pkEsc, pkChar, pkChar}},
		"cut short":       {"a\x1b[", []pickKeyKind{pkChar}},
		"unknown tilde":   {"\x1b[3~", nil},
		"unknown control": {"\x01", nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var got []pickKeyKind
			for _, k := range parsePickKeys([]byte(c.in)) {
				got = append(got, k.kind)
			}
			assert.Equal(t, c.want, got)
		})
	}
}

// pickByNumber takes a number, an item's key, or text matching one item;
// anything else cancels.
func TestPickByNumber(t *testing.T) {
	list := []PickItem{{Label: "Postgres", Detail: "server", Key: 'p'}, {Label: "SQLite", Key: 's'}, {Label: "SQL Server"}}
	cases := map[string]struct {
		answer string
		want   int
		err    error
	}{
		"number":         {"2", 1, nil},
		"key":            {"P", 0, nil},
		"unique text":    {"lite", 1, nil},
		"ambiguous text": {"sql", -1, ErrPickCancelled},
		"empty":          {"  ", -1, ErrPickCancelled},
		"out of range":   {"9", -1, ErrPickCancelled},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var shown string
			i, err := pickByNumber(context.Background(), func(_ context.Context, s string) (string, error) {
				shown = s
				return c.answer, nil
			}, "Database", list, 1)
			assert.Equal(t, c.want, i)
			assert.ErrorIs(t, err, c.err)
			assert.Contains(t, shown, "› "+" 2. SQLite")
			assert.Contains(t, shown, "server")
		})
	}
	_, err := pickByNumber(context.Background(), func(context.Context, string) (string, error) { return "", errBoom }, "x", list, 0)
	assert.ErrorIs(t, err, errBoom)
}

// failingKeys is a keyTerm whose ready or read fails.
type failingKeys struct {
	readyErr, readErr error
}

func (failingKeys) enter() error { return nil }
func (failingKeys) leave() error { return nil }
func (f failingKeys) ready(time.Duration) (bool, error) {
	return f.readErr != nil, f.readyErr
}
func (f failingKeys) read([]byte) (int, error) { return 0, f.readErr }

// Pick fails with the terminal, stops when its context ends, and offers
// nothing for no items.
func TestTerminalPickFailures(t *testing.T) {
	f := newFakeTerminal(t)
	_, err := f.in.Pick(context.Background(), "Choose", nil, 0)
	assert.ErrorIs(t, err, ErrPickCancelled, "no items")

	for name, k := range map[string]failingKeys{"ready": {readyErr: errBoom}, "read": {readErr: errBoom}} {
		t.Run(name, func(t *testing.T) {
			f.in.pickTerm = func() keyTerm { return k }
			_, err := f.in.Pick(context.Background(), "Choose", items("one"), 0)
			assert.ErrorIs(t, err, errBoom)
		})
	}

	f.in.pickTerm = func() keyTerm { return newChanKeys() }
	ctx, cancel := context.WithTimeout(context.Background(), 2*keyPollTimeout)
	defer cancel()
	_, err = f.in.Pick(ctx, "Choose", items("one"), 0)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Another prompt holds the terminal.
	f.in.turn <- struct{}{}
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = f.in.Pick(ctx, "Choose", items("one"), 0)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	<-f.in.turn
}

// A picker shown during a turn pauses the key watcher and hands it back.
func TestTerminalPickPausesTheKeyWatcher(t *testing.T) {
	f, keys := pickTerminal(t)
	watched := newChanKeys()
	w := startKeyWatcher(watched, func(string) {}, nil)
	defer w.close()
	f.in.keys = w
	keys.in <- []byte("\r")
	i, err := f.in.Pick(context.Background(), "Choose", items("one", "two"), 1)
	require.NoError(t, err)
	assert.Equal(t, 1, i)
	assert.GreaterOrEqual(t, watched.left.Load(), int32(1), "the watcher let go of the terminal")

	// A cancelled context stops both waiting for the watcher and reading.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = f.in.Pick(ctx, "Choose", items("one"), 0)
	assert.ErrorIs(t, err, context.Canceled)
}

// An approval without a label for its key says "matching", and fails with
// the picker.
func TestApprovalPickerEdges(t *testing.T) {
	f, keys := pickTerminal(t)
	approve := NewApprover(f.in, 0)
	req := api.ApprovalRequest{Tool: "web_fetch", Kind: api.ActionNetwork, Detail: "https://go.dev", Key: "web:go.dev"}
	keys.in <- []byte("s")
	d, err := approve(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, api.DecisionSession, d)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, err = approve(ctx, req)
	assert.Equal(t, api.DecisionDeny, d)
	assert.ErrorIs(t, err, context.Canceled)

	ask := NewUserPrompter(f.in)
	_, err = ask(ctx, "Which?", []string{"a", "b"})
	assert.ErrorIs(t, err, context.Canceled)
}

// On a pipe, /agent and /model show the current choice without a picker.
func TestAgentAndModelOnAPipe(t *testing.T) {
	app, _ := stubApp(t, "")
	assert.Contains(t, runCmd(t, app, "/agent"), "Current agent:")
	assert.Contains(t, runCmd(t, app, "/model"), "Current model:")
	ref, picked, ok := resumeWithPicker(context.Background(), app)
	assert.Empty(t, ref)
	assert.False(t, picked)
	assert.False(t, ok)
	_, picked = pickSession(context.Background(), app)
	assert.False(t, picked)
}

// The model picker offers pinned models and those with settings, and
// switches to the one chosen.
func TestModelPickerOffersPinsAndSettings(t *testing.T) {
	app, s := stubApp(t, "")
	f, keys := pickTerminal(t)
	app.Input = f.in
	s.listAgents = func() []api.AgentInfo {
		return []api.AgentInfo{{Name: "blitz", Active: true}, {Name: "qa", PinnedModel: "openai/gpt-5"}}
	}
	s.allModelSettings = func() map[string]config.ModelSettings {
		return map[string]config.ModelSettings{"anthropic/claude": {}, "openai/gpt-5": {}}
	}
	var set string
	s.setModel = func(ref string) (string, error) { set = ref; return "", nil }
	keys.in <- []byte("gpt\r")
	out := runCmd(t, app, "/model")
	assert.Equal(t, "openai/gpt-5", set)
	assert.Contains(t, out, "Model set to:")
	screen := f.output()
	assert.Contains(t, screen, "pinned for qa")
	assert.Contains(t, screen, "anthropic/claude")
	assert.Contains(t, screen, "has settings")
}

// The session picker leaves out the active session, empty ones and
// snapshots (offered by name), and says when there's nothing to resume.
func TestSessionPickerChoices(t *testing.T) {
	app, s := stubApp(t, "")
	f, keys := pickTerminal(t)
	app.Input = f.in
	s.activeSession = func() (api.SessionInfo, bool) { return api.SessionInfo{ID: "now"}, true }
	s.listSessions = func(all bool) ([]api.SessionInfo, error) {
		if all {
			return []api.SessionInfo{{ID: "snap1", Snapshot: "base", Title: "Base"}}, nil
		}
		return []api.SessionInfo{{ID: "now", MessageCount: 3}, {ID: "empty"}, {ID: "snap1", Snapshot: "base", MessageCount: 2},
			{ID: "older", Title: "Older work", MessageCount: 4, Updated: time.Now()}}, nil
	}
	var loaded string
	s.loadSession = func(ref string) (api.SessionInfo, bool, error) {
		loaded = ref
		return api.SessionInfo{ID: ref}, false, nil
	}
	keys.in <- []byte("snapshot\r")
	runCmd(t, app, "/resume")
	assert.Equal(t, "base", loaded)
	screen := f.output()
	assert.Contains(t, screen, "Older work")
	assert.NotContains(t, screen, "empty")

	s.listSessions = func(bool) ([]api.SessionInfo, error) { return nil, nil }
	assert.Contains(t, runCmd(t, app, "/resume"), "No other sessions in this workspace.")
	s.listSessions = func(bool) ([]api.SessionInfo, error) { return nil, errBoom }
	_, picked := pickSession(context.Background(), app)
	assert.False(t, picked)
}

// The session picker shows at most fifty sessions.
func TestSessionPickerCapsTheList(t *testing.T) {
	app, s := stubApp(t, "")
	f, keys := pickTerminal(t)
	app.Input = f.in
	var list []api.SessionInfo
	for i := range 60 {
		list = append(list, api.SessionInfo{ID: fmt.Sprintf("s%02d", i), MessageCount: 1})
	}
	s.listSessions = func(all bool) ([]api.SessionInfo, error) {
		if all {
			return nil, nil
		}
		return list, nil
	}
	keys.in <- []byte("\x1b[F") // ignored
	keys.in <- []byte("\x1b")
	_, picked := pickSession(context.Background(), app)
	assert.False(t, picked)
	assert.Contains(t, f.output(), "1/50")
}

// The terminal's history file is created owner-only, and one that can't
// be made is an error.
func TestTerminalHistoryFile(t *testing.T) {
	dir := t.TempDir()
	history := filepath.Join(dir, "sub", "history")
	f := newFakeTerminalWith(t, TerminalOptions{HistoryFile: history})
	require.NotNil(t, f)
	info, err := os.Stat(history)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	notDir := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(notDir, nil, 0o600))
	_, err = newTerminalInput(TerminalOptions{HistoryFile: filepath.Join(notDir, "history")}, nil)
	assert.Error(t, err)
}

// A line ending in a backslash goes on with a continuation line.
func TestTerminalContinuationLine(t *testing.T) {
	f := newFakeTerminal(t)
	go func() { // each line is typed once the editor has asked where the cursor is
		<-f.out.answered
		f.w.Write([]byte("first \\\r"))
		<-f.out.answered
		f.w.Write([]byte("second\r"))
	}()
	line, err := within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") })
	require.NoError(t, err)
	assert.Equal(t, "first \nsecond", line)
}

// Paths complete after /cd from the new workspace, and listing a missing
// directory offers nothing.
func TestCompleterAfterCd(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(b, "main.go"), nil, 0o600))
	for i := range 205 {
		require.NoError(t, os.WriteFile(filepath.Join(b, fmt.Sprintf("f%03d", i)), nil, 0o600))
	}
	c := NewCompleter(a)
	c.SetWorkspace(b)
	got, _ := c.Do([]rune("@ma"), 3)
	assert.Equal(t, [][]rune{[]rune("in.go")}, got)
	assert.Len(t, c.paths("f"), 200, "the list is capped")
	assert.Nil(t, c.paths("nowhere/x"))
}

// WatchKeys on a stdin that isn't a terminal watches nothing, and stops.
func TestWatchKeysWithoutATerminal(t *testing.T) {
	f := newFakeTerminal(t)
	stop := f.in.WatchKeys(func(string) { t.Error("a key without a terminal") })
	f.in.mu.Lock()
	watching := f.in.keys != nil
	f.in.mu.Unlock()
	assert.True(t, watching)
	stop()
	assert.Nil(t, f.in.keys)
}
