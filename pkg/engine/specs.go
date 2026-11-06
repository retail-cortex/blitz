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

package engine

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/skills"
)

// The workspace's specification in .agents/specs/ (spec_012_memory MEM-42,
// MEM-46; spec_040_studio STU-11): numbered stages (NNN-<name>.md) in the
// order the work happens, each with its status, the earlier stages it
// depends on, its numbered requirements or objectives, what done looks
// like, its open questions, and the skills and agents that cite it.

var (
	// ErrUnknownSpec means no stage with that name exists in .agents/specs/.
	ErrUnknownSpec = errors.New("no such spec stage")
	// ErrBadSpecStatus means a stage's status wasn't draft, in-progress or done.
	ErrBadSpecStatus = errors.New("spec status must be draft, in-progress or done")
	// ErrSpecCycle means a stage's dependencies would form a loop.
	ErrSpecCycle = errors.New("spec dependencies cannot form a loop")
	// ErrBadSpecTitle means a new stage was requested without a title.
	ErrBadSpecTitle = errors.New("spec stage title is required")
	// ErrSpecChanged means the stage file on disk changed since it was read.
	ErrSpecChanged = errors.New("the spec stage changed since it was opened")
)

var (
	specFilePattern = regexp.MustCompile(`^(\d{3})-([a-z0-9][a-z0-9-]*)\.md$`)
	specTitlePrefix = regexp.MustCompile(`^\d{3}\s*[.:\-—–]\s*`)
	specItemPattern = regexp.MustCompile(`^(?:[-*]\s+(?:\[([ xX])\]\s+)?)?(?:\*\*([A-Z][A-Z0-9_]*-\d+[a-z]?)\*\*|` + "`" + `([A-Z][A-Z0-9_]*-\d+[a-z]?)` + "`" + `)\s*(?:[—–:\-.]\s*)?(.*)$`)
)

// SpecStatuses are the valid values for a stage's Status line.
var SpecStatuses = []string{"draft", "in-progress", "done"}

// SpecItem is one numbered requirement, objective or content item in a stage spec.
type SpecItem struct {
	ID, Section, Text string
	Done              bool
}

// SpecStage is one numbered stage in .agents/specs/NNN-<name>.md.
type SpecStage struct {
	Name, Path, Number, Title, Status, Kind, Context, Content, Hash string
	DependsOn, Goals, NonGoals                                      []string
	Items                                                           []SpecItem
	DoneWhen, OpenQuestions, Skills, Agents                         []string
	DoneWhenDone                                                    []bool
}

// SpecsSummary is a workspace's specification in .agents/specs/ as Studio › Specs shows it.
type SpecsSummary struct {
	Dir, Kind, Overview string
	HarnessStale        bool
	Stages              []SpecStage
}

// SpecUpdate describes an atomic update to an existing stage or creation of a new one.
type SpecUpdate struct {
	Name                    string
	Title                   string
	Context                 string
	Status                  string
	SetDependsOn            bool
	DependsOn               []string
	ToggleItemID            string
	ItemDone                bool
	ToggleDoneWhenIndex     int
	DoneWhenDone            bool
	AddRequirement          string
	AddDoneWhen             string
	AddOpenQuestion         string
	RemoveOpenQuestionIndex int
	Content                 string
	Hash                    string
}

// ProjectSpecsDir is where /spec writes the workspace's specification: .agents/specs.
func (w *Workspace) ProjectSpecsDir() string { return filepath.Join(w.Dir(), ".agents", "specs") }

// Specs reads the workspace's specification from .agents/specs/, ordered by
// stage number, and links the workspace skills and agents that cite each stage.
func (w *Workspace) Specs() SpecsSummary {
	dir := w.ProjectSpecsDir()
	out := SpecsSummary{Dir: dir, Overview: readSpecsOverview(filepath.Join(dir, "README.md")), HarnessStale: w.HarnessStale()}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	var names []string
	byNum, known := map[string]string{}, map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !e.Type().IsRegular() {
			continue
		}
		if m := specFilePattern.FindStringSubmatch(e.Name()); m != nil {
			stem := strings.TrimSuffix(e.Name(), ".md")
			names, known[stem] = append(names, stem), true
			if _, exists := byNum[m[1]]; !exists {
				byNum[m[1]] = stem
			}
		}
	}
	slices.Sort(names)
	skills := w.specCitations(filepath.Join(w.Dir(), ".agents", "skills"), true, names)
	agents := w.specCitations(filepath.Join(w.Dir(), ".agents", "agents"), false, names)
	for _, stem := range names {
		raw, err := os.ReadFile(filepath.Join(dir, stem+".md"))
		if err != nil {
			continue
		}
		st := ParseSpecStage(stem, ".agents/specs/"+stem+".md", string(raw))
		st.DependsOn = resolveSpecDeps(st.DependsOn, stem, known, byNum)
		st.Skills, st.Agents = skills[stem], agents[stem]
		if out.Kind == "" && st.Kind != "" {
			out.Kind = st.Kind
		}
		out.Stages = append(out.Stages, st)
	}
	return out
}

// ParseSpecStage parses a stage spec file (.agents/specs/NNN-<name>.md).
func ParseSpecStage(name, relPath, text string) SpecStage {
	num, slug := "", name
	if i := strings.IndexByte(name, '-'); i == 3 {
		num, slug = name[:3], name[4:]
	}
	st := SpecStage{
		Name: name, Path: relPath, Number: num,
		Title: strings.ReplaceAll(slug, "-", " "), Status: "draft",
		Content: text, Hash: skills.FileHash([]byte(text)),
	}
	section := ""
	var ctxLines []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && section == "" {
			if h := specTitlePrefix.ReplaceAllString(strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")), ""); h != "" {
				st.Title = h
			}
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			continue
		}
		if section == "" {
			parseSpecHeaderLine(trimmed, &st)
		} else {
			parseSpecSectionLine(section, trimmed, &st, &ctxLines)
		}
	}
	st.Context = strings.TrimSpace(strings.Join(ctxLines, " "))
	return st
}

func parseSpecHeaderLine(trimmed string, st *SpecStage) {
	clean := strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* "), "**", "")
	lower := strings.ToLower(clean)
	switch {
	case strings.HasPrefix(lower, "status:"):
		if raw := strings.TrimSpace(clean[len("status:"):]); raw != "" {
			if v := strings.ToLower(raw); slices.Contains(SpecStatuses, v) {
				st.Status = v
			} else {
				st.Status = raw
			}
		}
	case strings.HasPrefix(lower, "kind:"):
		st.Kind = strings.TrimSpace(clean[len("kind:"):])
	case strings.HasPrefix(lower, "depends on:"):
		for _, part := range strings.Split(strings.TrimSpace(clean[len("depends on:"):]), ",") {
			if d := strings.TrimSuffix(strings.Trim(strings.TrimSpace(part), "`*_."), ".md"); !isNoneText(d) {
				st.DependsOn = append(st.DependsOn, d)
			}
		}
	}
}

func parseSpecSectionLine(section, trimmed string, st *SpecStage, ctxLines *[]string) {
	if trimmed == "" || strings.HasPrefix(trimmed, "### ") {
		return
	}
	if m := specItemPattern.FindStringSubmatch(trimmed); m != nil {
		id := cmp.Or(m[2], m[3])
		done := strings.EqualFold(m[1], "x")
		st.Items = append(st.Items, SpecItem{ID: id, Section: section, Text: strings.TrimSpace(m[4]), Done: done})
		return
	}
	bullet, isBullet, done := trimBullet(trimmed)
	switch {
	case strings.HasPrefix(section, "context"):
		*ctxLines = append(*ctxLines, bullet)
	case section == "goals" && isBullet && !isNoneText(bullet):
		st.Goals = append(st.Goals, bullet)
	case (section == "non-goals" || section == "non goals") && isBullet && !isNoneText(bullet):
		st.NonGoals = append(st.NonGoals, bullet)
	case (strings.HasPrefix(section, "done when") || strings.HasPrefix(section, "acceptance")) && isBullet && !isNoneText(bullet):
		st.DoneWhen = append(st.DoneWhen, bullet)
		st.DoneWhenDone = append(st.DoneWhenDone, done)
	case strings.HasPrefix(section, "open question") && isBullet && !isNoneText(bullet):
		st.OpenQuestions = append(st.OpenQuestions, bullet)
	}
}

func trimBullet(s string) (string, bool, bool) {
	for _, p := range []string{"- [x] ", "- [X] ", "* [x] ", "* [X] "} {
		if strings.HasPrefix(s, p) {
			return strings.TrimSpace(s[len(p):]), true, true
		}
	}
	for _, p := range []string{"- [ ] ", "* [ ] ", "- ", "* "} {
		if strings.HasPrefix(s, p) {
			return strings.TrimSpace(s[len(p):]), true, false
		}
	}
	return s, false, false
}

func isNoneText(s string) bool {
	l := strings.TrimRight(strings.ToLower(strings.TrimSpace(s)), ".")
	return l == "" || l == "none" || l == "n/a" || l == "-" || l == "—" || l == "–"
}

func readSpecsOverview(readmePath string) string {
	data, err := os.ReadFile(readmePath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		isListOrHead := t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "-") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "|")
		isNumbered := len(t) >= 3 && t[0] >= '0' && t[0] <= '9' && (strings.Contains(t, ". ") || strings.Contains(t, "-"))
		if !isListOrHead && !isNumbered {
			return t
		}
	}
	return ""
}

func resolveOneSpecDep(raw string, known map[string]bool, byNum map[string]string) string {
	d := strings.TrimSuffix(strings.Trim(strings.TrimSpace(raw), "`*"), ".md")
	if i := strings.IndexByte(d, ' '); i > 0 {
		d = d[:i]
	}
	if known[d] {
		return d
	}
	if len(d) == 3 {
		return byNum[d]
	}
	return ""
}

func resolveSpecDeps(raw []string, self string, known map[string]bool, byNum map[string]string) []string {
	var out []string
	for _, r := range raw {
		if res := resolveOneSpecDep(r, known, byNum); res != "" && res != self && !slices.Contains(out, res) {
			out = append(out, res)
		}
	}
	slices.Sort(out)
	return out
}

func hasSpecCycle(target string, targetDeps []string, stages map[string]SpecStage) bool {
	visiting, visited := map[string]bool{}, map[string]bool{}
	var dfs func(string) bool
	dfs = func(n string) bool {
		if visiting[n] || visited[n] {
			return visiting[n]
		}
		visiting[n] = true
		deps := stages[n].DependsOn
		if n == target {
			deps = targetDeps
		}
		if slices.ContainsFunc(deps, dfs) {
			return true
		}
		visiting[n], visited[n] = false, true
		return false
	}
	return dfs(target)
}

func (w *Workspace) specCitations(dir string, skillDir bool, stages []string) map[string][]string {
	out := make(map[string][]string, len(stages))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		var name, file string
		if skillDir && e.IsDir() {
			name, file = e.Name(), filepath.Join(dir, e.Name(), "SKILL.md")
		} else if !skillDir && !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			name, file = strings.TrimSuffix(e.Name(), ".md"), filepath.Join(dir, e.Name())
		} else {
			continue
		}
		if raw, err := os.ReadFile(file); err == nil {
			body := string(raw)
			for _, st := range stages {
				if strings.Contains(body, st) {
					out[st] = append(out[st], name)
				}
			}
		}
	}
	return out
}
