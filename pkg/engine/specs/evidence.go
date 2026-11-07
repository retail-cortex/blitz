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
	"path"
	"regexp"
	"strings"
)

// MaxEvidence is the most evidence kept for one requirement.
const MaxEvidence = 8

// Finder finds requirements' IDs in the work (SDD-12's pass without a
// model): a file's lines and commit messages that name one.
type Finder struct {
	re *regexp.Regexp
	// Found is the evidence by ID, in the order it was found.
	Found map[string][]Evidence
}

// NewFinder looks for ids (nil when there are none).
func NewFinder(ids []string) *Finder {
	if len(ids) == 0 {
		return nil
	}
	q := make([]string, len(ids))
	for i, id := range ids {
		q[i] = regexp.QuoteMeta(id)
	}
	// An ID ends at a non-word character: ORD-01 isn't in ORD-010.
	return &Finder{re: regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])(` + strings.Join(q, "|") + `)(?:$|[^A-Za-z0-9_])`), Found: map[string][]Evidence{}}
}

// File looks for the IDs in a file's text (p, relative to the workspace).
func (f *Finder) File(p, text string) {
	if !strings.ContainsAny(text, "-") {
		return
	}
	kind := EvidenceKind(p)
	for i, line := range strings.Split(text, "\n") {
		for _, m := range f.re.FindAllStringSubmatch(line, -1) {
			f.add(m[1], Evidence{Path: p, Line: i + 1, Kind: kind, Text: clip(strings.TrimSpace(line), 160)})
		}
	}
}

// Commit looks for the IDs in a commit's message (its hash and subject
// shown).
func (f *Finder) Commit(hash, subject, message string) {
	for _, m := range f.re.FindAllStringSubmatch(message, -1) {
		f.add(m[1], Evidence{Path: hash, Kind: "commit", Text: clip(subject, 160)})
	}
}

func (f *Finder) add(id string, e Evidence) {
	list := f.Found[id]
	if len(list) >= MaxEvidence {
		return
	}
	for _, o := range list {
		if o.Path == e.Path && o.Line == e.Line {
			return
		}
	}
	f.Found[id] = append(list, e)
}

var testRE = regexp.MustCompile(`(?i)(^|/)(tests?|__tests__|spec|specs)/|[._-](test|spec)s?\.[a-z0-9]+$|(^|/)test_[^/]*$`)

// EvidenceKind says what a file is to a requirement: a test, a document,
// or code (anything else: scripts, configuration, notebooks).
func EvidenceKind(p string) string {
	switch {
	case testRE.MatchString(p):
		return "test"
	case strings.EqualFold(path.Ext(p), ".md"), strings.EqualFold(path.Ext(p), ".txt"), strings.EqualFold(path.Ext(p), ".rst"), strings.EqualFold(path.Ext(p), ".adoc"):
		return "doc"
	}
	return "code"
}

// StateOf is what the evidence says without a model: traced when code, a
// test or a commit names the requirement, untraced otherwise (documents
// that only mention it don't implement it).
func StateOf(ev []Evidence) string {
	for _, e := range ev {
		if e.Kind != "doc" {
			return Traced
		}
	}
	return Untraced
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
