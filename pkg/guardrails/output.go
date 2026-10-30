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
	held     string
	withheld bool
	findings []Finding
	seen     map[string]bool
}

// Output is a check of one stream of the model's text.
func (s Set) Output() *Output { return &Output{set: s, seen: map[string]bool{}} }

// Findings are what the guardrails did, once each.
func (o *Output) Findings() []Finding { return o.findings }

func (o *Output) found(f Finding) {
	if !o.seen[f.Guardrail] {
		o.seen[f.Guardrail] = true
		o.findings = append(o.findings, f)
	}
}

// Write takes the next piece of text and returns what can be sent now.
func (o *Output) Write(piece string) string {
	if o.withheld {
		return ""
	}
	o.held += piece
	o.held = o.check(o.held)
	if o.withheld {
		return o.held
	}
	cut := len(o.held) - Window
	if i := keyStart(o.held); i >= 0 {
		cut = min(cut, i) // a key, not masked yet: held until it's whole
	}
	if cut <= 0 {
		return ""
	}
	for cut > 0 && !utf8.RuneStart(o.held[cut]) {
		cut--
	}
	out := o.held[:cut]
	o.held = o.held[cut:]
	return out
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

// Flush is the rest, at the end of the text.
func (o *Output) Flush() string {
	if o.withheld {
		return ""
	}
	out := o.check(o.held)
	o.held = ""
	return out
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
