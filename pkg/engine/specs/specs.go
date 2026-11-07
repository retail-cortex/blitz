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

// Package specs reads a project's specification as the project-spec agent
// (/spec) writes it, .agents/specs/NNN-<name>.md with README.md as the
// index, and checks it without a model (spec_057_spec_driven SDD-01..05):
// the title, the status, what each spec depends on, the requirements'
// IDs, "Done when" and the open questions.
package specs

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Dir is where a workspace's specs live.
const Dir = ".agents/specs"

// Index is the specs' index file.
const Index = "README.md"

// Statuses are a spec's states, in the order a spec goes through them.
var Statuses = []string{"draft", "approved", "in progress", "implemented", "changed", "retired"}

// Spec is one spec file, as read.
type Spec struct {
	// File is its name in Dir (001-overview.md).
	File string
	// Number is its place in the order (0 for the index or an unnumbered file).
	Number int
	// Title is its heading's text, without the number ("Overview").
	Title string
	// TitleLine is the heading's line (1-based; 0 when there's none).
	TitleLine int
	// Status is its Status: line, lowercase ("" when there's none).
	Status     string
	StatusLine int
	// DependsOn are the numbers its Depends on: line names.
	DependsOn     []int
	DependsOnLine int
	// Kind is 001-overview's Kind: line (software, research, …).
	Kind []string
	// Items are its numbered requirements (or objectives, content).
	Items []Item
	// DoneWhen reports whether it says how to tell it's finished.
	DoneWhen bool
	// OpenQuestions are the lines under its Open questions heading.
	OpenQuestions []string
}

// Item is one requirement with its ID (AUTH-01).
type Item struct {
	ID   string
	Text string
	Line int
}

// Finding is something a check found in a spec.
type Finding struct {
	// Line is where (1-based; 0 for the whole spec).
	Line int
	// ID is the requirement it's about, if one.
	ID string
	// Severity is "error" (the spec can't be traced or ordered as written),
	// "warning" (it can, but it's weaker for it) or "info".
	Severity string
	// Code names the check (no-title, duplicate-id, vague, …).
	Code    string
	Message string
}

var (
	fileRE   = regexp.MustCompile(`^(\d{3})-(.+)\.md$`)
	titleRE  = regexp.MustCompile(`^#\s+(?:(\d{1,3})[.)]?\s+)?(.+?)\s*#*\s*$`)
	fieldRE  = regexp.MustCompile(`^\s*(?:\*\*|__)?(Status|Depends on|Kind)(?:\*\*|__)?\s*:\s*(?:\*\*|__)?\s*(.*?)\s*$`)
	itemRE   = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])?\s*(?:\*\*|__|` + "`" + `)?([A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*-\d{2,4}[a-z]?)(?:\*\*|__|` + "`" + `)?\s*[:.)\-–—]?\s*(.*)$`)
	headRE   = regexp.MustCompile(`^\s*(?:#{2,6}\s*|\*\*|__)\s*([^*_#:]+)(?::\s*(?:\*\*|__)?|(?:\*\*|__)\s*:?)?\s*(.*)$`)
	numberRE = regexp.MustCompile(`\b(\d{3})\b`)
	listRE   = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+(.+)$`)
)

// notIDs are prefixes of names that look like IDs but aren't (UTF-16,
// ISO-8601, RFC-9110).
var notIDs = map[string]bool{"UTF": true, "ISO": true, "RFC": true, "SHA": true, "MD": true, "AES": true, "RSA": true, "HTTP": true, "TLS": true, "SSL": true, "CVE": true, "ECMA": true, "IEEE": true, "PEP": true}

// Parse reads the spec file named file.
func Parse(file string, text string) Spec {
	s := Spec{File: file}
	if m := fileRE.FindStringSubmatch(file); m != nil {
		s.Number, _ = strconv.Atoi(m[1])
	}
	section := ""
	inFence := false
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		n := i + 1
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || trimmed == "" {
			continue
		}
		if s.TitleLine == 0 && strings.HasPrefix(trimmed, "# ") {
			if m := titleRE.FindStringSubmatch(trimmed); m != nil {
				s.Title, s.TitleLine = m[2], n
			}
			continue
		}
		if m := fieldRE.FindStringSubmatch(line); m != nil {
			switch m[1] {
			case "Status":
				s.Status, s.StatusLine = strings.ToLower(strings.Trim(m[2], "*_` ")), n
			case "Depends on":
				s.DependsOnLine = n
				for _, d := range numberRE.FindAllString(m[2], -1) {
					v, _ := strconv.Atoi(d)
					s.DependsOn = append(s.DependsOn, v)
				}
			case "Kind":
				for k := range strings.SplitSeq(m[2], ",") {
					if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
						s.Kind = append(s.Kind, k)
					}
				}
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "**") || strings.HasPrefix(trimmed, "__") {
			if m := headRE.FindStringSubmatch(line); m != nil && !itemRE.MatchString(line) {
				section = strings.ToLower(strings.TrimSpace(m[1]))
				if strings.HasPrefix(section, "done when") {
					s.DoneWhen = true
				}
				if rest := strings.TrimSpace(m[2]); rest != "" && strings.HasPrefix(section, "open question") {
					s.OpenQuestions = append(s.OpenQuestions, rest)
				}
				continue
			}
		}
		// A plain "Done when: …" or "Open questions:" line starts its section too.
		if low := strings.ToLower(trimmed); strings.HasPrefix(low, "done when") {
			s.DoneWhen, section = true, "done when"
			continue
		} else if strings.HasPrefix(low, "open questions:") {
			section = "open questions"
			if rest := strings.TrimSpace(trimmed[len("open questions:"):]); rest != "" {
				s.OpenQuestions = append(s.OpenQuestions, rest)
			}
			continue
		}
		if m := itemRE.FindStringSubmatch(line); m != nil && !notIDs[strings.SplitN(m[1], "-", 2)[0]] && (listRE.MatchString(line) || strings.HasPrefix(trimmed, m[1]) || strings.HasPrefix(trimmed, "**"+m[1])) {
			s.Items = append(s.Items, Item{ID: m[1], Text: strings.TrimSpace(m[2]), Line: n})
			continue
		}
		if strings.HasPrefix(section, "open question") {
			if m := listRE.FindStringSubmatch(line); m != nil {
				s.OpenQuestions = append(s.OpenQuestions, strings.TrimSpace(m[1]))
			}
		}
	}
	return s
}

// vagueWords make a requirement hard to check: each is a judgement, not a
// test (English: the specs /spec writes in other languages are judged by
// the model's review instead).
var vagueWords = []string{"fast", "quickly", "quick", "secure", "user-friendly", "user friendly", "easy", "easily", "intuitive", "robust", "scalable", "efficient", "efficiently", "appropriate", "as needed", "as appropriate", "etc", "and/or", "flexible", "simple", "optimal", "minimal", "reasonable", "seamless", "modern", "high quality", "good"}

var vagueRE = regexp.MustCompile(`(?i)\b(` + strings.Join(func() []string {
	q := make([]string, len(vagueWords))
	for i, w := range vagueWords {
		q[i] = regexp.QuoteMeta(w)
	}
	return q
}(), "|") + `)\b`)

// Lint checks spec s against the rest of the set (all, s among them),
// without a model.
func Lint(s Spec, all []Spec) []Finding {
	var out []Finding
	add := func(line int, id, severity, code, format string, args ...any) {
		out = append(out, Finding{Line: line, ID: id, Severity: severity, Code: code, Message: fmt.Sprintf(format, args...)})
	}
	if s.File == Index {
		return nil // the index: Unlisted checks it
	}
	if s.TitleLine == 0 {
		add(0, "", "error", "no-title", `no "# " title: start with "# %s. <Title>"`, numberText(s.Number))
	}
	if s.Number == 0 {
		add(0, "", "warning", "unnumbered", "the file name has no number: name it NNN-<name>.md so it has a place in the order")
	}
	switch {
	case s.StatusLine == 0:
		add(0, "", "warning", "no-status", `no "Status:" line (%s)`, strings.Join(Statuses, ", "))
	case !slices.Contains(Statuses, s.Status):
		add(s.StatusLine, "", "warning", "unknown-status", "status %q isn't one of %s", s.Status, strings.Join(Statuses, ", "))
	}
	numbers := map[int]Spec{}
	for _, o := range all {
		if o.Number > 0 {
			numbers[o.Number] = o
		}
	}
	for _, d := range s.DependsOn {
		switch o, ok := numbers[d]; {
		case d == s.Number:
			add(s.DependsOnLine, "", "error", "depends-on-itself", "it depends on itself (%s)", numberText(d))
		case !ok:
			add(s.DependsOnLine, "", "error", "unknown-dependency", "it depends on %s, which isn't a spec", numberText(d))
		case s.Number > 0 && d > s.Number:
			add(s.DependsOnLine, "", "warning", "later-dependency", "it depends on %s (%s), which comes after it in the order", numberText(d), o.File)
		}
	}
	if cycle := cycleFrom(s, numbers); len(cycle) > 0 {
		add(s.DependsOnLine, "", "error", "dependency-cycle", "its dependencies go round: %s", strings.Join(cycle, " → "))
	}
	if len(s.Items) == 0 {
		add(0, "", "warning", "no-items", "no requirements with IDs (AUTH-01): without them nothing can be traced to it")
	}
	seen := map[string]int{}
	for _, it := range s.Items {
		if line, ok := seen[it.ID]; ok {
			add(it.Line, it.ID, "error", "duplicate-id", "%s is also on line %d", it.ID, line)
			continue
		}
		seen[it.ID] = it.Line
		for _, o := range all {
			if o.File == s.File {
				continue
			}
			if j := slices.IndexFunc(o.Items, func(x Item) bool { return x.ID == it.ID }); j >= 0 {
				add(it.Line, it.ID, "error", "duplicate-id", "%s is also in %s (line %d)", it.ID, o.File, o.Items[j].Line)
			}
		}
		if strings.TrimSpace(it.Text) == "" {
			add(it.Line, it.ID, "warning", "empty-item", "%s says nothing", it.ID)
		} else if m := vagueRE.FindString(it.Text); m != "" {
			add(it.Line, it.ID, "warning", "vague", "%s: %q is a judgement, not a check: say how much, or how it's measured", it.ID, m)
		}
	}
	if !s.DoneWhen {
		add(0, "", "warning", "no-done-when", `no "Done when": say how anyone can tell it's finished`)
	}
	if n := len(s.OpenQuestions); n > 0 {
		add(0, "", "info", "open-questions", "%d open question(s) to answer before it's approved", n)
	}
	return out
}

// Unlisted are the numbered specs the index (its text) doesn't name.
func Unlisted(indexText string, all []Spec) []string {
	var out []string
	for _, s := range all {
		if s.Number > 0 && !strings.Contains(indexText, s.File) && !strings.Contains(indexText, strings.TrimSuffix(s.File, ".md")) {
			out = append(out, s.File)
		}
	}
	return out
}

// cycleFrom is a dependency loop back to s, as files, or nil.
func cycleFrom(s Spec, numbers map[int]Spec) []string {
	if s.Number == 0 {
		return nil
	}
	var path []string
	seen := map[int]bool{}
	var walk func(n int) bool
	walk = func(n int) bool {
		o, ok := numbers[n]
		if !ok || seen[n] {
			return false
		}
		seen[n] = true
		path = append(path, o.File)
		for _, d := range o.DependsOn {
			if d == s.Number && d != n {
				path = append(path, s.File)
				return true
			}
			if walk(d) {
				return true
			}
		}
		path = path[:len(path)-1]
		return false
	}
	for _, d := range s.DependsOn {
		if d == s.Number {
			continue
		}
		path = []string{s.File}
		clear(seen)
		seen[s.Number] = true
		if walk(d) {
			return path
		}
	}
	return nil
}

func numberText(n int) string {
	if n == 0 {
		return "NNN"
	}
	return fmt.Sprintf("%03d", n)
}

// Sort puts specs in their order: the index first, then by number, then
// the unnumbered by name.
func Sort(all []Spec) {
	rank := func(s Spec) int {
		switch {
		case strings.EqualFold(s.File, Index):
			return -1
		case s.Number == 0:
			return 1000
		}
		return s.Number
	}
	slices.SortFunc(all, func(a, b Spec) int { return cmp.Or(cmp.Compare(rank(a), rank(b)), strings.Compare(a.File, b.File)) })
}
