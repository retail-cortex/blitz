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

package guardrails

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Window is how much of the model's text Output holds back: a secret
// shorter than this is caught even when it comes in pieces.
const Window = 160

// pemStart begins a PEM block. A private key is longer than the window:
// from its start, text is held until the key is whole and masked.
const pemStart = "-----BEGIN"

// Output checks the model's text as it streams, with the secrets
// guardrail: each piece goes in, and what's safe to send comes out.
type Output struct {
	set      Set
	hold     Hold
	withheld bool
	findings []Finding
	seen     map[string]bool
}

// Output is a check of one stream of the model's text.
func (s Set) Output() *Output {
	o := &Output{set: s, seen: map[string]bool{}}
	o.hold = Hold{Check: func(text string) (string, bool) { out := o.check(text); return out, o.withheld }, Window: o.window()}
	return o
}

// Findings are what the guardrails did, once each.
func (o *Output) Findings() []Finding { return o.findings }

func (o *Output) found(f Finding) {
	if !o.seen[f.Guardrail] {
		o.seen[f.Guardrail] = true
		o.findings = append(o.findings, f)
	}
}

// Write takes the next piece of text and returns what can be sent now
// (Hold.Write).
func (o *Output) Write(piece string) string { return o.hold.Write(piece) }

// Flush is the rest, at the end of the text.
func (o *Output) Flush() string { return o.hold.Flush() }

// Hold holds back the end of a stream of text that a check can't judge
// yet, so a secret is checked whole however the stream splits it: the
// engine's guardrails use it for the model's answer as the gateway's do.
type Hold struct {
	// Check is text as it may be sent (masked, or replaced); stop ends
	// the stream: what it returned is the last of it.
	Check func(text string) (out string, stop bool)
	// Window is how much is held before the word still arriving (Window
	// when less).
	Window  int
	held    string
	stopped bool
}

// Write takes the next piece of text and returns what can be sent now.
// The word still arriving at the end (a run of the characters a key or
// a password's value is made of) isn't checked yet: a key masked when
// only its start had come would send the rest of it unmasked. It's
// held, with the window before it, until something ends it or the
// text does.
func (h *Hold) Write(piece string) string {
	if h.stopped {
		return ""
	}
	h.held += piece
	open := openStart(h.held)
	head, stop := h.Check(h.held[:open])
	if stop {
		h.stopped = true
		h.held = ""
		return head
	}
	h.held = head + h.held[open:]
	open = len(head)
	cut := open - max(h.Window, Window)
	if i := keyStart(h.held); i >= 0 {
		cut = min(cut, i) // a key, not masked yet: held until it's whole
	}
	if cut <= 0 {
		return ""
	}
	for cut > 0 && !utf8.RuneStart(h.held[cut]) {
		cut--
	}
	out := h.held[:cut]
	h.held = h.held[cut:]
	return out
}

// Flush is the rest, checked, at the end of the text.
func (h *Hold) Flush() string {
	if h.stopped {
		return ""
	}
	out, stop := h.Check(h.held)
	h.held, h.stopped = "", stop
	return out
}

// window is how much is held before what's still arriving: Window, or
// the longest secret the redactor knows by value when that's longer, so
// a known value with spaces or new lines in it is whole before any of it
// is sent.
func (o *Output) window() int {
	return max(Window, o.set.Redactor.Longest())
}

// openStart is where the run of characters at the end of s starts:
// those a key or an assigned value may hold (anything but space and
// "',;), so one that may still be growing.
func openStart(s string) int {
	i := strings.LastIndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`"',;`, r) })
	if i < 0 {
		return 0
	}
	_, n := utf8.DecodeRuneInString(s[i:])
	return i + n
}

// keyStart is where a private key starts in s, -1 for none: masked
// whole once its end comes, so one still there is still arriving. A
// certificate or a public key isn't held.
func keyStart(s string) int {
	i := strings.LastIndex(s, pemStart)
	if i < 0 {
		return -1
	}
	head := s[i+len(pemStart):]
	if j := strings.Index(head, "-----"); j >= 0 && !strings.Contains(head[:j], "PRIVATE KEY") {
		return -1
	}
	return i
}

// check is text masked as the secrets guardrails say; one that blocks
// replaces the rest of the answer.
func (o *Output) check(text string) string {
	for _, g := range o.set.active(StageOutput) {
		if g.Builtin != Secrets {
			continue
		}
		masked, found := o.set.mask(g, text)
		if !found {
			continue
		}
		switch g.Action {
		case Redact:
			text = masked
			o.found(Finding{g.Name, Redact, "a secret in the answer"})
		case Block:
			o.withheld = true
			o.found(Finding{g.Name, Block, "a secret in the answer"})
			return "[the rest of this answer was withheld by the " + g.Name + " guardrail]"
		default:
			o.found(Finding{g.Name, Warn, "a secret in the answer"})
		}
	}
	return text
}
