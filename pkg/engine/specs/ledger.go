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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

// LedgerDir holds what the specification's work is traced to
// (spec_057_spec_driven SDD-10): specs/<name>.json, a spec's approval and
// each requirement's trace; baselines/<file>, the text approved; diffs/,
// a diff for each change to an approved spec.
const LedgerDir = ".agents/.ledger"

// The states a requirement's trace can be in: the check without a model
// says traced (its ID is in the work) or untraced; the model's judgement
// says implemented, partial, missing or contradicted.
const (
	Traced       = "traced"
	Untraced     = "untraced"
	Implemented  = "implemented"
	Partial      = "partial"
	Missing      = "missing"
	Contradicted = "contradicted"
)

// Ledger is one spec's entry: its approval and its requirements' traces.
type Ledger struct {
	Spec     string            `json:"spec"`
	Approved *Approval         `json:"approved,omitempty"`
	Items    map[string]*Trace `json:"items"`
}

// Approval is when a spec was approved, by whom, and its text's hash.
type Approval struct {
	At   time.Time `json:"at"`
	By   string    `json:"by,omitempty"`
	Hash string    `json:"hash"`
}

// Trace is one requirement: its text and fingerprint as approved, and its
// last verification (the fingerprint it was verified at, its state, why,
// and the evidence).
type Trace struct {
	Text                string     `json:"text"`
	Fingerprint         string     `json:"fingerprint"`
	VerifiedAt          *time.Time `json:"verified_at,omitempty"`
	VerifiedFingerprint string     `json:"verified_fingerprint,omitempty"`
	State               string     `json:"state,omitempty"`
	Judged              bool       `json:"judged,omitempty"`
	Reason              string     `json:"reason,omitempty"`
	Evidence            []Evidence `json:"evidence,omitempty"`
}

// Evidence is where a requirement was found: a file's line (code, test or
// doc) or a commit (Path is its hash).
type Evidence struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}

// Paths in the ledger, for a spec file (002-orders.md).
func LedgerPath(file string) string {
	return path.Join(LedgerDir, "specs", strings.TrimSuffix(file, path.Ext(file))+".json")
}

// BaselinePath is where the approved text of file is kept.
func BaselinePath(file string) string { return path.Join(LedgerDir, "baselines", file) }

// DiffPath is where a change to file approved at at is kept.
func DiffPath(file string, at time.Time) string {
	return path.Join(LedgerDir, "diffs", at.UTC().Format("20060102-150405")+"-"+strings.TrimSuffix(file, path.Ext(file))+".diff")
}

var (
	emphasisRE = regexp.MustCompile("[*_`]+")
	spacesRE   = regexp.MustCompile(`\s+`)
)

// Fingerprint is a requirement's text, normalized (case, spacing and
// emphasis don't count), hashed: what tells that it changed.
func Fingerprint(text string) string {
	t := strings.ToLower(spacesRE.ReplaceAllString(emphasisRE.ReplaceAllString(text, ""), " "))
	sum := sha256.Sum256([]byte(strings.TrimSpace(t)))
	return hex.EncodeToString(sum[:6])
}

// Hash is a whole text's hash.
func Hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// ReadLedger reads a ledger entry; empty data is an empty one for file.
func ReadLedger(file string, data []byte) (Ledger, error) {
	l := Ledger{Spec: file, Items: map[string]*Trace{}}
	if len(data) == 0 {
		return l, nil
	}
	if err := json.Unmarshal(data, &l); err != nil {
		return Ledger{Spec: file, Items: map[string]*Trace{}}, fmt.Errorf("%s: %w", LedgerPath(file), err)
	}
	if l.Items == nil {
		l.Items = map[string]*Trace{}
	}
	return l, nil
}

// Bytes is the entry as it's written: indented, so its diffs read.
func (l Ledger) Bytes() []byte {
	b, _ := json.MarshalIndent(l, "", "  ")
	return append(b, '\n')
}

// Approve records s (text) as approved at at by by: each requirement's
// text and fingerprint. A requirement whose fingerprint is unchanged keeps
// its verification; a changed one starts over; one gone is dropped.
func (l *Ledger) Approve(s Spec, text string, at time.Time, by string) {
	l.Approved = &Approval{At: at, By: by, Hash: Hash(text)}
	items := map[string]*Trace{}
	for _, it := range s.Items {
		fp := Fingerprint(it.Text)
		if old, ok := l.Items[it.ID]; ok && old.Fingerprint == fp {
			old.Text = it.Text
			items[it.ID] = old
			continue
		}
		items[it.ID] = &Trace{Text: it.Text, Fingerprint: fp}
	}
	l.Items = items
}

// Change is a requirement that differs from what was approved.
type Change struct {
	ID string
	// Kind is "added", "changed" or "removed".
	Kind string
}

// Changes are s's requirements against its approval: added, changed and
// removed, in the spec's order (the removed last). Nothing when it was
// never approved.
func (l Ledger) Changes(s Spec) []Change {
	if l.Approved == nil {
		return nil
	}
	var out []Change
	seen := map[string]bool{}
	for _, it := range s.Items {
		seen[it.ID] = true
		switch tr, ok := l.Items[it.ID]; {
		case !ok:
			out = append(out, Change{ID: it.ID, Kind: "added"})
		case tr.Fingerprint != Fingerprint(it.Text):
			out = append(out, Change{ID: it.ID, Kind: "changed"})
		}
	}
	for _, id := range sortedKeys(l.Items) {
		if !seen[id] {
			out = append(out, Change{ID: id, Kind: "removed"})
		}
	}
	return out
}

func sortedKeys(m map[string]*Trace) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// SetStatus is text with its Status: line saying status (added after the
// title when there's none).
func SetStatus(text, status string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if m := fieldRE.FindStringSubmatch(line); m != nil && m[1] == "Status" {
			lead := line[:strings.Index(line, "Status")]
			lines[i] = strings.TrimRight(lead, "*_") + "Status: " + status
			return strings.Join(lines, "\n")
		}
	}
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "# ") {
			return strings.Join(append(lines[:i+1:i+1], append([]string{"", "Status: " + status}, lines[i+1:]...)...), "\n")
		}
	}
	return "Status: " + status + "\n\n" + text
}

// UnifiedDiff is a diff of a and b, every changed line with three lines
// around it, under name's headers.
func UnifiedDiff(name, a, b string) string {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	type op struct {
		kind byte // ' ', '-', '+'
		text string
	}
	n, m := len(x), len(y)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var ops []op
	for i, j := 0, 0; i < n || j < m; {
		switch {
		case i < n && j < m && x[i] == y[j]:
			ops = append(ops, op{' ', x[i]})
			i++
			j++
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, op{'-', x[i]}) // removed lines first, as diff writes them
			i++
		default:
			ops = append(ops, op{'+', y[j]})
			j++
		}
	}
	var b2 strings.Builder
	fmt.Fprintf(&b2, "--- a/%s\n+++ b/%s\n", name, name)
	const around = 3
	changed := func(k int) bool { return k >= 0 && k < len(ops) && ops[k].kind != ' ' }
	for k := 0; k < len(ops); {
		if !changed(k) {
			k++
			continue
		}
		start := max(0, k-around)
		end := k
		for end < len(ops) {
			if changed(end) {
				end++
				continue
			}
			next := end
			for next < len(ops) && !changed(next) && next-end < 2*around {
				next++
			}
			if next < len(ops) && changed(next) {
				end = next
				continue
			}
			end = min(len(ops), end+around)
			break
		}
		// Line numbers of the hunk's start in a and b.
		ai, bi := 1, 1
		for _, o := range ops[:start] {
			if o.kind != '+' {
				ai++
			}
			if o.kind != '-' {
				bi++
			}
		}
		al, bl := 0, 0
		for _, o := range ops[start:end] {
			if o.kind != '+' {
				al++
			}
			if o.kind != '-' {
				bl++
			}
		}
		fmt.Fprintf(&b2, "@@ -%d,%d +%d,%d @@\n", ai, al, bi, bl)
		for _, o := range ops[start:end] {
			b2.WriteByte(o.kind)
			b2.WriteString(o.text)
			b2.WriteByte('\n')
		}
		k = end
	}
	return b2.String()
}
