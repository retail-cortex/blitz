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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func items(labels ...string) []PickItem {
	out := make([]PickItem, len(labels))
	for i, l := range labels {
		out[i] = PickItem{Label: l}
	}
	return out
}

func TestPickStateFiltersMovesAndChooses(t *testing.T) {
	st := newPickState([]PickItem{{Label: "Blitz", Detail: "general coder"}, {Label: "QA", Detail: "writes tests"}, {Label: "Docs", Detail: "writes docs"}}, 1)
	press := func(keys ...pickKey) (pickDone, int) {
		var done pickDone
		chosen := -1
		for _, k := range keys {
			if done, chosen = st.handle(k); done != pickOpen {
				break
			}
		}
		return done, chosen
	}
	char := func(s string) []pickKey {
		var out []pickKey
		for _, r := range s {
			out = append(out, pickKey{kind: pkChar, r: r})
		}
		return out
	}
	done, i := press(pickKey{kind: pkEnter})
	require.Equal(t, pickChosen, done, "the current item isn't selected first: %v %d", done, i)
	require.Equal(t, 1, i, "the current item isn't selected first: %v %d", done, i)
	// Typing filters on label and detail, word by word, ignoring case.
	st = newPickState(st.items, 0)
	press(char("WRITES do")...)
	require.Len(t, st.visible, 1, "filter: %v", st.visible)
	require.Equal(t, 2, st.visible[0], "filter: %v", st.visible)
	press(pickKey{kind: pkBackspace}, pickKey{kind: pkBackspace})
	require.Len(t, st.visible, 2, "after backspace: %v", st.visible)
	done, i = press(pickKey{kind: pkDown}, pickKey{kind: pkDown}, pickKey{kind: pkEnter})
	require.Equal(t, pickChosen, done, "down past the end stays on the last: %v %d", done, i)
	require.Equal(t, 2, i, "down past the end stays on the last: %v %d", done, i)
	// Esc clears the filter, then cancels.
	st = newPickState(st.items, 0)
	press(char("zzz")...)
	done, _ = press(pickKey{kind: pkEnter})
	require.Equal(t, pickOpen, done, "Enter with nothing matching chose something")
	done, _ = press(pickKey{kind: pkEsc})
	require.Equal(t, pickOpen, done, "first Esc: %v %v", done, st.visible)
	require.Len(t, st.visible, 3, "first Esc: %v %v", done, st.visible)
	done, _ = press(pickKey{kind: pkEsc})
	require.Equal(t, pickCancelled, done, "second Esc: %v", done)
	done, _ = press(pickKey{kind: pkInterrupt})
	require.Equal(t, pickInterrupted, done, "Ctrl+C: %v", done)
}

func TestPickKeysSelectOnlyBeforeFiltering(t *testing.T) {
	its := []PickItem{{Label: "Yes", Key: 'y'}, {Label: "No", Key: 'n'}, {Label: "Maybe not"}}
	st := newPickState(its, 0)
	done, i := st.handle(pickKey{kind: pkChar, r: 'N'})
	require.Equal(t, pickChosen, done, "key: %v %d", done, i)
	require.Equal(t, 1, i, "key: %v %d", done, i)
	st = newPickState(its, 0)
	st.handle(pickKey{kind: pkChar, r: 'm'})
	done, _ = st.handle(pickKey{kind: pkChar, r: 'y'})
	require.Equal(t, pickOpen, done, "a key after filter text is text: %v %q", done, string(st.filter))
	require.Equal(t, "my", string(st.filter), "a key after filter text is text: %v %q", done, string(st.filter))
}

func TestPickStateScrolls(t *testing.T) {
	var labels []string
	for i := range 25 {
		labels = append(labels, string(rune('a'+i)))
	}
	st := newPickState(items(labels...), 0)
	for range 12 {
		st.handle(pickKey{kind: pkDown})
	}
	require.Equal(t, 12, st.cursor, "cursor %d top %d", st.cursor, st.top)
	require.Equal(t, 3, st.top, "cursor %d top %d", st.cursor, st.top)
	st.handle(pickKey{kind: pkPageDown})
	st.handle(pickKey{kind: pkPageDown})
	require.Equal(t, 24, st.cursor, "page down: %d", st.cursor)
	var sb strings.Builder
	n := st.render(&sb, "Pick", 80)
	require.Equal(t, pickRows+2, n, "render: %d lines\n%s", n, sb.String())
	require.Contains(t, sb.String(), "25/25", "render: %d lines\n%s", n, sb.String())
}

func TestParsePickKeys(t *testing.T) {
	kinds := func(b string) []pickKeyKind {
		var out []pickKeyKind
		for _, k := range parsePickKeys([]byte(b)) {
			out = append(out, k.kind)
		}
		return out
	}
	for in, want := range map[string][]pickKeyKind{
		"\x1b":         {pkEsc},
		"\x1b[A\x1b[B": {pkUp, pkDown},
		"\x1bOA":       {pkUp},
		"\x1b[5~":      {pkPageUp},
		"\x1b[1;5C":    nil, // Ctrl+Right: ignored
		"ab\r":         {pkChar, pkChar, pkEnter},
		"\x7f\x15\x03": {pkBackspace, pkClear, pkInterrupt},
		"\x10\x0e":     {pkUp, pkDown},
	} {
		got := kinds(in)
		assert.Len(t, got, len(want), "%q: %v, want %v", in, got, want)
		assert.False(t, len(want) > 0 && strings.Join(kindNames(got), ",") != strings.Join(kindNames(want), ","), "%q: %v, want %v", in, got, want)
	}
}

func kindNames(ks []pickKeyKind) []string {
	var out []string
	for _, k := range ks {
		out = append(out, string(rune('0'+k)))
	}
	return out
}

// pickTerminal is a fake terminal whose pickers read keys from a channel.
func pickTerminal(t *testing.T) (*fakeTerminal, *chanKeys) {
	f := newFakeTerminal(t)
	keys := newChanKeys()
	f.in.pickTerm = func() keyTerm { return keys }
	return f, keys
}

func TestTerminalPick(t *testing.T) {
	f, keys := pickTerminal(t)
	keys.in <- []byte("\x1b[B")
	keys.in <- []byte("\r")
	i, err := f.in.Pick(context.Background(), "Intro\nChoose", items("one", "two"), 0)
	require.NoError(t, err, "%d", i)
	require.Equal(t, 1, i, "%d %v", i, err)
	out := f.output()
	assert.Equal(t, 1, strings.Count(out, "Intro"), "output:\n%q", out)
	assert.Contains(t, out, "❯ ", "output:\n%q", out)
	assert.Contains(t, out, "Choose"+Reset+" two", "output:\n%q", out)
	assert.False(t, keys.inMode.Load(), "the terminal was left in key mode")

	interrupted := false
	f.in.SetInterruptHandler(func() { interrupted = true })
	keys.in <- []byte("\x03")
	_, err = f.in.Pick(context.Background(), "Choose", items("one"), 0)
	require.ErrorIs(t, err, context.Canceled, "Ctrl+C: %v, interrupted %v", err, interrupted)
	require.True(t, interrupted, "Ctrl+C: %v, interrupted %v", err, interrupted)
	keys.in <- []byte("\x1b")
	_, err = f.in.Pick(context.Background(), "Choose", items("one"), 0)
	require.ErrorIs(t, err, ErrPickCancelled, "Esc: %v", err)
}

// Without raw keys the picker asks for a number on the line editor.
func TestTerminalPickFallsBackToANumber(t *testing.T) {
	f := newFakeTerminal(t)
	f.in.pickTerm = func() keyTerm { return ttyKeysUnavailable{} }
	f.keys(t, "2\r")
	i, err := within(t, func() (string, error) {
		i, err := f.in.Pick(context.Background(), "Choose", items("one", "two"), 0)
		return string(rune('0' + i)), err
	})
	require.NoError(t, err, "%q %v\n%s", i, err, f.output())
	require.Equal(t, "1", i, "%q %v\n%s", i, err, f.output())
	require.Contains(t, f.output(), " 2. two", "%q %v\n%s", i, err, f.output())
}

type ttyKeysUnavailable struct{}

func (ttyKeysUnavailable) enter() error                      { return errors.New("no terminal") }
func (ttyKeysUnavailable) leave() error                      { return nil }
func (ttyKeysUnavailable) ready(time.Duration) (bool, error) { return false, nil }
func (ttyKeysUnavailable) read([]byte) (int, error)          { return 0, nil }

func TestApprovalPicker(t *testing.T) {
	f, keys := pickTerminal(t)
	approve := NewApprover(f.in, 2)
	req := api.ApprovalRequest{Kind: "edit", Tool: "edit", Detail: "notes.txt", Key: "k", KeyLabel: "edits to notes.txt",
		Diff: "--- a/notes.txt\n+++ b/notes.txt\n@@ -1 +1 @@\n-old\n+new\n"}
	for key, want := range map[string]api.Decision{"y": api.DecisionOnce, "s": api.DecisionSession, "a": api.DecisionAlways, "n": api.DecisionDeny} {
		keys.in <- []byte(key)
		d, err := approve(context.Background(), req)
		assert.NoError(t, err, "%s: %v", key, d)
		assert.Equal(t, want, d, "%s: %v %v", key, d, err)
	}
	out := f.output()
	for _, want := range []string{"Approval required", "Yes, and allow edits to notes.txt this session", "Show the whole diff", "Allow?"} {
		assert.Contains(t, out, want, "missing %q in:\n%s", want, out)
	}
	assert.NotContains(t, out, "[y/s/a/d/N]", "the line prompt's answer keys are shown with the picker")

	// d shows the whole diff and asks again, without the diff item.
	keys.in <- []byte("d")
	keys.in <- []byte("y")
	d, err := approve(context.Background(), req)
	require.NoError(t, err, "after the diff: %v", d)
	require.Equal(t, api.DecisionOnce, d, "after the diff: %v %v", d, err)
	assert.Contains(t, f.output(), "+new", "the whole diff wasn't shown")

	// Esc denies and stops the turn.
	interrupted := false
	f.in.SetInterruptHandler(func() { interrupted = true })
	keys.in <- []byte("\x1b")
	d, err = approve(context.Background(), req)
	require.Equal(t, api.DecisionDeny, d, "Esc: %v %v, interrupted %v", d, err, interrupted)
	require.ErrorIs(t, err, context.Canceled, "Esc: %v %v, interrupted %v", d, err, interrupted)
	require.True(t, interrupted, "Esc: %v %v, interrupted %v", d, err, interrupted)
}

func TestQuestionPicker(t *testing.T) {
	f, keys := pickTerminal(t)
	ask := NewUserPrompter(f.in)
	keys.in <- []byte("\x1b[B\r")
	got, err := ask(context.Background(), "Which database?", []string{"Postgres", "SQLite"})
	require.NoError(t, err, "%q", got)
	require.Equal(t, "SQLite", got, "%q %v", got, err)
	// The last item asks for another answer on the line.
	keys.in <- []byte("another\r")
	f.keys(t, "DuckDB\r")
	got, err = within(t, func() (string, error) {
		return ask(context.Background(), "Which database?", []string{"Postgres", "SQLite"})
	})
	require.NoError(t, err, "other: %q", got)
	require.Equal(t, "DuckDB", got, "other: %q %v", got, err)
	assert.NotContains(t, f.output(), "[1] Postgres", "the numbered options are shown with the picker")
}

func TestAgentAndModelPickers(t *testing.T) {
	app, _ := newCommandApp(t, "")
	f, keys := pickTerminal(t)
	app.Input = f.in
	keys.in <- []byte("qa\r")
	out := captureStdout(t, func() { HandleCommand(context.Background(), "/agent", app) })
	got := app.Workspace.ActiveAgent().Name
	require.Equal(t, "qa", got, "agent %q\n%s", got, out)
	// Choosing the active agent just shows it.
	keys.in <- []byte("\r")
	out = captureStdout(t, func() { HandleCommand(context.Background(), "/agent", app) })
	assert.Contains(t, out, "Current agent:", "choosing the active agent:\n%s", out)

	keys.in <- []byte("\x1b")
	out = captureStdout(t, func() { HandleCommand(context.Background(), "/model", app) })
	assert.Contains(t, out, "Current model:", "cancelled model picker:\n%s\n%s", out, f.output())
	assert.Contains(t, f.output(), "current", "cancelled model picker:\n%s\n%s", out, f.output())
}

func TestResumePicker(t *testing.T) {
	app, _ := newCommandApp(t, "")
	first, err := app.Workspace.NewSession()
	require.NoError(t, err)
	_, err = app.Workspace.Run(context.Background(), first.ID, api.Turn{Text: "remember the milk"}, func(api.Event) {})
	require.NoError(t, err)
	app.Workspace.NewSession()
	f, keys := pickTerminal(t)
	app.Input = f.in
	keys.in <- []byte("milk\r")
	out := captureStdout(t, func() { HandleCommand(context.Background(), "/resume", app) })
	a, _ := app.Workspace.ActiveSession()
	require.Equal(t, first.ID, a.ID, "resumed %s, want %s\n%s\n%s", a.ID, first.ID, out, f.output())
	// Cancelling doesn't print the usage.
	keys.in <- []byte("\x1b")
	out = captureStdout(t, func() { HandleCommand(context.Background(), "/resume", app) })
	assert.NotContains(t, out, "Usage", "cancel printed:\n%s", out)
}

// Every drawn line fits the width, so erasing the picker counts right.
func TestPickRenderFitsTheWidth(t *testing.T) {
	st := newPickState([]PickItem{{Label: "a label that is\nlong " + strings.Repeat("x", 100), Detail: strings.Repeat("d", 100)}}, 0)
	st.filter = []rune(strings.Repeat("f", 100))
	st.refilter()
	st.filter = []rune(strings.Repeat("x", 50)) // keep the item visible
	st.refilter()
	var sb strings.Builder
	n := st.render(&sb, strings.Repeat("T", 100), 40)
	lines := strings.Split(strings.TrimSuffix(sb.String(), "\n"), "\n")
	require.Len(t, lines, n, "%d lines drawn, %d counted", len(lines), n)
	for _, l := range lines {
		plain := ansiPattern.ReplaceAllString(strings.TrimPrefix(l, "\r\x1b[2K"), "")
		w := len([]rune(plain))
		assert.LessOrEqual(t, w, 40, "line of %d columns: %q", w, plain)
	}
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
