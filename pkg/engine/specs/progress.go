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

package specs

import (
	"slices"
	"strings"
)

// Done reports that s is done: approved, unchanged since, and every
// requirement found in the work (traced, or judged implemented).
func Done(s Spec, l Ledger) bool {
	if l.Approved == nil || len(s.Items) == 0 || len(l.Changes(s)) > 0 {
		return false
	}
	for _, it := range s.Items {
		tr := l.Items[it.ID]
		if tr == nil || (tr.State != Traced && tr.State != Implemented) {
			return false
		}
	}
	return true
}

// Order is where each numbered spec stands (SDD-32): Next is the first in
// order that isn't done and whose dependencies are; Blocked are, by
// number, the dependencies not done that each other spec waits for.
type Order struct {
	Next    int
	Blocked map[int][]int
}

// Progress is the specification's order, from which specs are done.
func Progress(all []Spec, done map[int]bool) Order {
	o := Order{Blocked: map[int][]int{}}
	for _, s := range all {
		if s.Number == 0 || done[s.Number] {
			continue
		}
		var waits []int
		for _, d := range s.DependsOn {
			if d != s.Number && !done[d] {
				waits = append(waits, d)
			}
		}
		if len(waits) > 0 {
			o.Blocked[s.Number] = waits
			continue
		}
		if o.Next == 0 {
			o.Next = s.Number
		}
	}
	return o
}

// Touched are the approved requirements whose evidence is in one of the
// changed files (relative, slash-separated): what a turn's changes may
// have broken (SDD-31).
func Touched(l Ledger, changed map[string]bool) []string {
	if l.Approved == nil {
		return nil
	}
	var out []string
	for id, tr := range l.Items {
		if slices.ContainsFunc(tr.Evidence, func(e Evidence) bool { return e.Kind != "commit" && e.Kind != "doc" && changed[e.Path] }) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// Drift is why an approved requirement no longer holds, if it doesn't:
// "changed" (since approval), "lost" (no longer found in the work: it was
// before), or "judged" (the model said missing or contradicted).
func Drift(change, was, now string, judged bool) string {
	switch {
	case change != "":
		return "changed"
	case (was == Traced || was == Implemented || was == Partial) && now == Untraced:
		return "lost"
	case judged && (now == Missing || now == Contradicted):
		return "judged"
	}
	return ""
}

// Pending are s's requirements not yet implemented, by l's last
// verification, in order.
func Pending(s Spec, l Ledger) []Item {
	var out []Item
	for _, it := range s.Items {
		tr := l.Items[it.ID]
		if tr == nil || tr.State != Implemented || tr.VerifiedFingerprint != Fingerprint(it.Text) {
			out = append(out, it)
		}
	}
	return out
}

// DoneWhen is s's "Done when" text (its section's lines), for a plan.
func DoneWhen(text string) string {
	var out []string
	in := false
	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		low := strings.ToLower(strings.Trim(t, "#*_ "))
		switch {
		case strings.HasPrefix(low, "done when"):
			in = true
			if _, rest, ok := strings.Cut(t, ":"); ok {
				if rest = strings.TrimSpace(strings.Trim(rest, "*_ ")); rest != "" {
					out = append(out, rest)
				}
			}
			continue
		case in && (strings.HasPrefix(t, "#") || (strings.HasPrefix(t, "**") && strings.HasSuffix(strings.TrimRight(t, ":"), "**"))):
			in = false
		}
		if in && t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n")
}
