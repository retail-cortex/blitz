package tui

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
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
	if done, i := press(pickKey{kind: pkEnter}); done != pickChosen || i != 1 {
		t.Fatalf("the current item isn't selected first: %v %d", done, i)
	}
	// Typing filters on label and detail, word by word, ignoring case.
	st = newPickState(st.items, 0)
	press(char("WRITES do")...)
	if len(st.visible) != 1 || st.visible[0] != 2 {
		t.Fatalf("filter: %v", st.visible)
	}
	press(pickKey{kind: pkBackspace}, pickKey{kind: pkBackspace})
	if len(st.visible) != 2 {
		t.Fatalf("after backspace: %v", st.visible)
	}
	if done, i := press(pickKey{kind: pkDown}, pickKey{kind: pkDown}, pickKey{kind: pkEnter}); done != pickChosen || i != 2 {
		t.Fatalf("down past the end stays on the last: %v %d", done, i)
	}
	// Esc clears the filter, then cancels.
	st = newPickState(st.items, 0)
	press(char("zzz")...)
	if done, _ := press(pickKey{kind: pkEnter}); done != pickOpen {
		t.Fatal("Enter with nothing matching chose something")
	}
	if done, _ := press(pickKey{kind: pkEsc}); done != pickOpen || len(st.visible) != 3 {
		t.Fatalf("first Esc: %v %v", done, st.visible)
	}
	if done, _ := press(pickKey{kind: pkEsc}); done != pickCancelled {
		t.Fatalf("second Esc: %v", done)
	}
	if done, _ := press(pickKey{kind: pkInterrupt}); done != pickInterrupted {
		t.Fatalf("Ctrl+C: %v", done)
	}
}

func TestPickKeysSelectOnlyBeforeFiltering(t *testing.T) {
	its := []PickItem{{Label: "Yes", Key: 'y'}, {Label: "No", Key: 'n'}, {Label: "Maybe not"}}
	st := newPickState(its, 0)
	if done, i := st.handle(pickKey{kind: pkChar, r: 'N'}); done != pickChosen || i != 1 {
		t.Fatalf("key: %v %d", done, i)
	}
	st = newPickState(its, 0)
	st.handle(pickKey{kind: pkChar, r: 'm'})
	if done, _ := st.handle(pickKey{kind: pkChar, r: 'y'}); done != pickOpen || string(st.filter) != "my" {
		t.Fatalf("a key after filter text is text: %v %q", done, string(st.filter))
	}
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
	if st.cursor != 12 || st.top != 3 {
		t.Fatalf("cursor %d top %d", st.cursor, st.top)
	}
	st.handle(pickKey{kind: pkPageDown})
	st.handle(pickKey{kind: pkPageDown})
	if st.cursor != 24 {
		t.Fatalf("page down: %d", st.cursor)
	}
	var sb strings.Builder
	if n := st.render(&sb, "Pick", 80); n != pickRows+2 || !strings.Contains(sb.String(), "25/25") {
		t.Fatalf("render: %d lines\n%s", n, sb.String())
	}
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
		if got := kinds(in); len(got) != len(want) || (len(want) > 0 && strings.Join(kindNames(got), ",") != strings.Join(kindNames(want), ",")) {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
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
	if err != nil || i != 1 {
		t.Fatalf("%d %v", i, err)
	}
	out := f.output()
	if strings.Count(out, "Intro") != 1 || !strings.Contains(out, "❯ ") || !strings.Contains(out, "Choose"+Reset+" two") {
		t.Errorf("output:\n%q", out)
	}
	if keys.inMode.Load() {
		t.Error("the terminal was left in key mode")
	}

	interrupted := false
	f.in.SetInterruptHandler(func() { interrupted = true })
	keys.in <- []byte("\x03")
	if _, err := f.in.Pick(context.Background(), "Choose", items("one"), 0); !errors.Is(err, context.Canceled) || !interrupted {
		t.Fatalf("Ctrl+C: %v, interrupted %v", err, interrupted)
	}
	keys.in <- []byte("\x1b")
	if _, err := f.in.Pick(context.Background(), "Choose", items("one"), 0); !errors.Is(err, ErrPickCancelled) {
		t.Fatalf("Esc: %v", err)
	}
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
	if err != nil || i != "1" || !strings.Contains(f.output(), " 2. two") {
		t.Fatalf("%q %v\n%s", i, err, f.output())
	}
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
		if d, err := approve(context.Background(), req); err != nil || d != want {
			t.Errorf("%s: %v %v", key, d, err)
		}
	}
	out := f.output()
	for _, want := range []string{"Approval required", "Yes, and allow edits to notes.txt this session", "Show the whole diff", "Allow?"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "[y/s/a/d/N]") {
		t.Error("the line prompt's answer keys are shown with the picker")
	}

	// d shows the whole diff and asks again, without the diff item.
	keys.in <- []byte("d")
	keys.in <- []byte("y")
	if d, err := approve(context.Background(), req); err != nil || d != api.DecisionOnce {
		t.Fatalf("after the diff: %v %v", d, err)
	}
	if !strings.Contains(f.output(), "+new") {
		t.Error("the whole diff wasn't shown")
	}

	// Esc denies and stops the turn.
	interrupted := false
	f.in.SetInterruptHandler(func() { interrupted = true })
	keys.in <- []byte("\x1b")
	if d, err := approve(context.Background(), req); d != api.DecisionDeny || !errors.Is(err, context.Canceled) || !interrupted {
		t.Fatalf("Esc: %v %v, interrupted %v", d, err, interrupted)
	}
}

func TestQuestionPicker(t *testing.T) {
	f, keys := pickTerminal(t)
	ask := NewUserPrompter(f.in)
	keys.in <- []byte("\x1b[B\r")
	if got, err := ask(context.Background(), "Which database?", []string{"Postgres", "SQLite"}); err != nil || got != "SQLite" {
		t.Fatalf("%q %v", got, err)
	}
	// The last item asks for another answer on the line.
	keys.in <- []byte("another\r")
	f.keys(t, "DuckDB\r")
	got, err := within(t, func() (string, error) {
		return ask(context.Background(), "Which database?", []string{"Postgres", "SQLite"})
	})
	if err != nil || got != "DuckDB" {
		t.Fatalf("other: %q %v", got, err)
	}
	if strings.Contains(f.output(), "[1] Postgres") {
		t.Error("the numbered options are shown with the picker")
	}
}

func TestAgentAndModelPickers(t *testing.T) {
	app, _ := newCommandApp(t, "")
	f, keys := pickTerminal(t)
	app.Input = f.in
	keys.in <- []byte("qa\r")
	out := captureStdout(t, func() { HandleCommand(context.Background(), "/agent", app) })
	if got := app.Workspace.ActiveAgent().Name; got != "qa" {
		t.Fatalf("agent %q\n%s", got, out)
	}
	// Choosing the active agent just shows it.
	keys.in <- []byte("\r")
	out = captureStdout(t, func() { HandleCommand(context.Background(), "/agent", app) })
	if !strings.Contains(out, "Current agent:") {
		t.Errorf("choosing the active agent:\n%s", out)
	}

	keys.in <- []byte("\x1b")
	out = captureStdout(t, func() { HandleCommand(context.Background(), "/model", app) })
	if !strings.Contains(out, "Current model:") || !strings.Contains(f.output(), "current") {
		t.Errorf("cancelled model picker:\n%s\n%s", out, f.output())
	}
}

func TestResumePicker(t *testing.T) {
	app, _ := newCommandApp(t, "")
	first, err := app.Workspace.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Workspace.Run(context.Background(), first.ID, api.Turn{Text: "remember the milk"}, func(api.Event) {}); err != nil {
		t.Fatal(err)
	}
	app.Workspace.NewSession()
	f, keys := pickTerminal(t)
	app.Input = f.in
	keys.in <- []byte("milk\r")
	out := captureStdout(t, func() { HandleCommand(context.Background(), "/resume", app) })
	if a, _ := app.Workspace.ActiveSession(); a.ID != first.ID {
		t.Fatalf("resumed %s, want %s\n%s\n%s", a.ID, first.ID, out, f.output())
	}
	// Cancelling doesn't print the usage.
	keys.in <- []byte("\x1b")
	if out := captureStdout(t, func() { HandleCommand(context.Background(), "/resume", app) }); strings.Contains(out, "Usage") {
		t.Errorf("cancel printed:\n%s", out)
	}
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
	if len(lines) != n {
		t.Fatalf("%d lines drawn, %d counted", len(lines), n)
	}
	for _, l := range lines {
		plain := ansiPattern.ReplaceAllString(strings.TrimPrefix(l, "\r\x1b[2K"), "")
		if w := len([]rune(plain)); w > 40 {
			t.Errorf("line of %d columns: %q", w, plain)
		}
	}
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
