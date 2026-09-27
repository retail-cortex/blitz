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
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"golang.org/x/term"
)

// PickItem is one choice in a picker.
type PickItem struct {
	Label  string // shown, and matched by typing
	Detail string // shown dim after the label, and matched too
	// Key selects the item at once when typed before any filter text (the
	// approval answers y, s, a, n). 0 for none.
	Key rune
}

// ErrPickCancelled is returned when the user leaves a picker without
// choosing (Esc, or an empty answer).
var ErrPickCancelled = errors.New("cancelled")

// Picker is an Input that can show a menu: an arrow-key, type-to-filter
// list inside the terminal's normal flow (decision §12.4 of the parity
// spec: no full-screen views).
type Picker interface {
	// Pick shows items under title with current selected, and returns the
	// index chosen.
	Pick(ctx context.Context, title string, items []PickItem, current int) (int, error)
}

// pickRows is how many items a picker shows at once.
const pickRows = 10

// pickState is a picker's state, apart from the terminal.
type pickState struct {
	items   []PickItem
	filter  []rune
	visible []int // indices of items matching the filter
	cursor  int   // position in visible
	top     int   // first visible row shown
}

func newPickState(items []PickItem, current int) *pickState {
	p := &pickState{items: items}
	p.refilter()
	if current >= 0 && current < len(items) {
		p.cursor = current
		p.scroll()
	}
	return p
}

// refilter keeps the items whose label or detail contains every word of
// the filter, ignoring case.
func (p *pickState) refilter() {
	words := strings.Fields(strings.ToLower(string(p.filter)))
	p.visible = p.visible[:0]
	for i, it := range p.items {
		text := strings.ToLower(it.Label + " " + it.Detail)
		match := true
		for _, w := range words {
			if !strings.Contains(text, w) {
				match = false
				break
			}
		}
		if match {
			p.visible = append(p.visible, i)
		}
	}
	p.cursor, p.top = 0, 0
}

func (p *pickState) move(n int) {
	if len(p.visible) == 0 {
		return
	}
	p.cursor = min(max(p.cursor+n, 0), len(p.visible)-1)
	p.scroll()
}

func (p *pickState) scroll() {
	if p.cursor < p.top {
		p.top = p.cursor
	}
	if p.cursor >= p.top+pickRows {
		p.top = p.cursor - pickRows + 1
	}
}

// pickKey is a key a picker understands.
type pickKey struct {
	kind pickKeyKind
	r    rune
}

type pickKeyKind int

const (
	pkChar pickKeyKind = iota
	pkUp
	pkDown
	pkPageUp
	pkPageDown
	pkEnter
	pkEsc
	pkBackspace
	pkClear
	pkInterrupt
)

// parsePickKeys splits a terminal read into keys. A read of just ESC is the
// Esc key; other escape sequences are arrow and paging keys, or ignored.
func parsePickKeys(b []byte) []pickKey {
	if len(b) == 1 && b[0] == 0x1b {
		return []pickKey{{kind: pkEsc}}
	}
	var out []pickKey
	for len(b) > 0 {
		if b[0] == 0x1b {
			n, k := escapeKey(b)
			if k != nil {
				out = append(out, *k)
			}
			b = b[n:]
			continue
		}
		r, size := utf8.DecodeRune(b)
		b = b[size:]
		switch {
		case r == '\r' || r == '\n':
			out = append(out, pickKey{kind: pkEnter})
		case r == 0x7f || r == 0x08:
			out = append(out, pickKey{kind: pkBackspace})
		case r == 0x15: // Ctrl+U
			out = append(out, pickKey{kind: pkClear})
		case r == 0x03: // Ctrl+C
			out = append(out, pickKey{kind: pkInterrupt})
		case r == 0x10: // Ctrl+P
			out = append(out, pickKey{kind: pkUp})
		case r == 0x0e: // Ctrl+N
			out = append(out, pickKey{kind: pkDown})
		case r != utf8.RuneError && unicode.IsPrint(r):
			out = append(out, pickKey{kind: pkChar, r: r})
		}
	}
	return out
}

// escapeKey reads one escape sequence at the start of b, returning its
// length and key (nil if it isn't one a picker uses).
func escapeKey(b []byte) (int, *pickKey) {
	if len(b) < 2 || (b[1] != '[' && b[1] != 'O') {
		return 1, &pickKey{kind: pkEsc}
	}
	i := 2
	for i < len(b) && (b[i] == ';' || (b[i] >= '0' && b[i] <= '9')) {
		i++
	}
	if i >= len(b) {
		return len(b), nil
	}
	params := string(b[2:i])
	var k *pickKey
	switch b[i] {
	case 'A':
		k = &pickKey{kind: pkUp}
	case 'B':
		k = &pickKey{kind: pkDown}
	case '~':
		switch params {
		case "5":
			k = &pickKey{kind: pkPageUp}
		case "6":
			k = &pickKey{kind: pkPageDown}
		}
	}
	return i + 1, k
}

// pickDone is how handling a key left the picker.
type pickDone int

const (
	pickOpen pickDone = iota
	pickChosen
	pickCancelled
	pickInterrupted
)

// handle applies a key; when the picker is done, chosen is the item's index.
func (p *pickState) handle(k pickKey) (done pickDone, chosen int) {
	switch k.kind {
	case pkUp:
		p.move(-1)
	case pkDown:
		p.move(1)
	case pkPageUp:
		p.move(-pickRows)
	case pkPageDown:
		p.move(pickRows)
	case pkEnter:
		if len(p.visible) > 0 {
			return pickChosen, p.visible[p.cursor]
		}
	case pkEsc:
		if len(p.filter) > 0 { // Esc clears the filter first
			p.filter = p.filter[:0]
			p.refilter()
			break
		}
		return pickCancelled, -1
	case pkInterrupt:
		return pickInterrupted, -1
	case pkBackspace:
		if len(p.filter) > 0 {
			p.filter = p.filter[:len(p.filter)-1]
			p.refilter()
		}
	case pkClear:
		p.filter = p.filter[:0]
		p.refilter()
	case pkChar:
		if len(p.filter) == 0 {
			for i, it := range p.items {
				if it.Key != 0 && unicode.ToLower(it.Key) == unicode.ToLower(k.r) {
					return pickChosen, i
				}
			}
		}
		p.filter = append(p.filter, k.r)
		p.refilter()
	}
	return pickOpen, -1
}

// render draws the picker and returns how many lines it used.
func (p *pickState) render(w io.Writer, title string, width int) int {
	if width <= 10 {
		width = 80
	}
	lines := 0
	line := func(s string) {
		fmt.Fprintf(w, "\r\x1b[2K%s\n", s)
		lines++
	}
	// Every line must fit the width: erasing counts lines.
	head := Bold + clip(oneLine(title), width-2) + Reset
	if len(p.filter) > 0 {
		room := width - 4 - utf8.RuneCountInString(oneLine(title))
		head = Bold + clip(oneLine(title), width/2) + Reset + " " + Cyan + "› " + clip(oneLine(string(p.filter)), max(room, width/2-4)) + Reset
	}
	line(head)
	if len(p.visible) == 0 {
		line("  " + Dim + i18n.T("picker.no_match") + Reset)
	}
	end := min(p.top+pickRows, len(p.visible))
	for row := p.top; row < end; row++ {
		it := p.items[p.visible[row]]
		text := clip(oneLine(it.Label), width-4)
		if it.Detail != "" && utf8.RuneCountInString(text) < width-8 {
			text += "  " + Dim + clip(oneLine(it.Detail), width-8-utf8.RuneCountInString(text)) + Reset
		}
		if row == p.cursor {
			line(Cyan + Bold + "❯ " + Reset + Cyan + text + Reset)
		} else {
			line("  " + text)
		}
	}
	hint := i18n.T("picker.hint")
	if len(p.visible) > pickRows {
		hint = fmt.Sprintf("%d/%d · %s", p.cursor+1, len(p.visible), hint)
	}
	line("  " + Dim + clip(hint, width-2) + Reset)
	return lines
}

// oneLine makes s safe to print on one line.
func oneLine(s string) string { return strings.Join(strings.Fields(safe(s)), " ") }

// clip cuts s to n runes, marking the cut.
func clip(s string, n int) string {
	if n <= 1 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// Pick implements Picker on the terminal: arrow keys (or Ctrl+P/N) move,
// typing filters, Enter chooses, Esc clears the filter or cancels, and
// Ctrl+C cancels (and interrupts a running turn). Without a terminal it
// asks for a number instead.
func (t *TerminalInput) Pick(ctx context.Context, title string, items []PickItem, current int) (int, error) {
	if len(items) == 0 {
		return -1, ErrPickCancelled
	}
	t.mu.Lock()
	keys := t.keys
	t.mu.Unlock()
	if keys != nil {
		resume, err := keys.pause(ctx)
		if err != nil {
			return -1, err
		}
		defer resume()
	}
	select {
	case t.turn <- struct{}{}:
	case <-ctx.Done():
		return -1, ctx.Err()
	}
	kt := t.pickTerm()
	if kt.enter() != nil { // no raw keys here: ask for a number
		<-t.turn
		return pickByNumber(ctx, t.readAsk, title, items, current)
	}
	defer func() { <-t.turn }()
	defer kt.leave()
	out := t.rl.Stdout()
	// Only the title's last line is redrawn with the list.
	if i := strings.LastIndexByte(title, '\n'); i >= 0 {
		io.WriteString(out, title[:i+1])
		title = title[i+1:]
	}
	st := newPickState(items, current)
	width := t.width()
	drawn := st.render(out, title, width)
	erase := func() { fmt.Fprintf(out, "\x1b[%dA\r\x1b[J", drawn) }
	buf := make([]byte, 64)
	for {
		if err := ctx.Err(); err != nil {
			erase()
			return -1, err
		}
		ok, err := kt.ready(keyPollTimeout)
		if err != nil {
			erase()
			return -1, err
		}
		if !ok {
			continue
		}
		n, err := kt.read(buf)
		if err != nil {
			erase()
			return -1, err
		}
		for _, k := range parsePickKeys(buf[:n]) {
			done, chosen := st.handle(k)
			switch done {
			case pickOpen:
				continue
			case pickChosen:
				erase()
				fmt.Fprintf(out, "%s%s%s %s\n", Dim, oneLine(title), Reset, oneLine(items[chosen].Label))
				return chosen, nil
			case pickInterrupted:
				erase()
				t.interrupt()
				return -1, context.Canceled
			default:
				erase()
				return -1, ErrPickCancelled
			}
		}
		erase()
		drawn = st.render(out, title, width)
	}
}

// readAsk reads an answer without pausing the key watcher (Pick already
// has).
func (t *TerminalInput) readAsk(ctx context.Context, text string) (string, error) {
	return t.readLine(ctx, text, readAsk, "")
}

// width is the terminal's width in columns (80 if unknown).
func (t *TerminalInput) width() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 80
}

// pickByNumber is the picker without a terminal: a numbered list, answered
// with a number, an item's key, or text matching one item.
func pickByNumber(ctx context.Context, ask func(context.Context, string) (string, error), title string, items []PickItem, current int) (int, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s%s%s\n", Bold, safe(title), Reset)
	for i, it := range items {
		mark := "  "
		if i == current {
			mark = "› "
		}
		fmt.Fprintf(&sb, "%s%2d. %s", mark, i+1, safe(it.Label))
		if it.Detail != "" {
			fmt.Fprintf(&sb, "  %s%s%s", Dim, safe(it.Detail), Reset)
		}
		sb.WriteString("\n")
	}
	sb.WriteString(i18n.T("picker.number") + " ")
	answer, err := ask(ctx, sb.String())
	if err != nil {
		return -1, err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return -1, ErrPickCancelled
	}
	if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(items) {
		return n - 1, nil
	}
	if r := []rune(answer); len(r) == 1 {
		for i, it := range items {
			if it.Key != 0 && unicode.ToLower(it.Key) == unicode.ToLower(r[0]) {
				return i, nil
			}
		}
	}
	st := newPickState(items, -1)
	st.filter = []rune(answer)
	st.refilter()
	if len(st.visible) == 1 {
		return st.visible[0], nil
	}
	return -1, ErrPickCancelled
}

// terminalPicker returns the REPL's picker when it runs on a terminal.
// Piped input keeps the plain commands, so scripts read the same.
func terminalPicker(app *App) (Picker, bool) {
	p, ok := app.Input.(*TerminalInput)
	return p, ok
}

// pickApproval asks for an approval with a picker. header is the request
// as the line prompt shows it; its last line (the answer keys) is replaced
// by the picker. Esc denies and stops the turn, as Ctrl+C does.
func pickApproval(ctx context.Context, t *TerminalInput, req api.ApprovalRequest, header string, truncated bool) (api.Decision, error) {
	label := req.KeyLabel
	if label == "" {
		label = i18n.T("approve.matching")
	}
	type choice struct {
		item     PickItem
		decision api.Decision
		diff     bool
	}
	choices := []choice{{item: PickItem{Label: i18n.T("approve.pick_yes"), Key: 'y'}, decision: api.DecisionOnce}}
	if req.Key != "" {
		choices = append(choices,
			choice{item: PickItem{Label: i18n.T("approve.pick_session", "label", label), Key: 's'}, decision: api.DecisionSession},
			choice{item: PickItem{Label: i18n.T("approve.pick_always", "label", label), Key: 'a'}, decision: api.DecisionAlways})
	}
	if truncated {
		choices = append(choices, choice{item: PickItem{Label: i18n.T("approve.pick_diff"), Key: 'd'}, diff: true})
	}
	choices = append(choices, choice{item: PickItem{Label: i18n.T("approve.pick_no"), Key: 'n'}, decision: api.DecisionDeny})
	items := make([]PickItem, len(choices))
	for i, c := range choices {
		items[i] = c.item
	}
	title := header + i18n.T("approve.ask")
	for {
		i, err := t.Pick(ctx, title, items, 0)
		if errors.Is(err, ErrPickCancelled) {
			t.interrupt()
			return api.DecisionDeny, fmt.Errorf("no approval input: %w", context.Canceled)
		}
		if err != nil {
			return api.DecisionDeny, fmt.Errorf("no approval input: %w", err)
		}
		if !choices[i].diff {
			return choices[i].decision, nil
		}
		full, _ := RenderDiff(req.Diff, 0)
		title = full + i18n.T("approve.ask")
		choices = append(choices[:i], choices[i+1:]...)
		items = append(items[:i], items[i+1:]...)
	}
}

// pickAnswer asks an agent's multiple-choice question with a picker, plus
// an item for typing another answer. Esc stops the turn, as Ctrl+C does.
func pickAnswer(ctx context.Context, t *TerminalInput, header string, options []string) (string, error) {
	// The numbered options and "Answer:" are the line prompt's; the picker
	// shows the options itself.
	if i := strings.Index(header, "\n   [1] "); i >= 0 {
		header = header[:i+1]
	}
	items := make([]PickItem, 0, len(options)+1)
	for _, o := range options {
		items = append(items, PickItem{Label: o})
	}
	items = append(items, PickItem{Label: i18n.T("question.other")})
	i, err := t.Pick(ctx, header+i18n.T("question.answer"), items, 0)
	switch {
	case errors.Is(err, ErrPickCancelled):
		t.interrupt()
		return "", context.Canceled
	case err != nil:
		return "", err
	case i < len(options):
		return options[i], nil
	}
	answer, err := t.Ask(ctx, i18n.T("question.answer")+" ")
	return strings.TrimSpace(answer), err
}
