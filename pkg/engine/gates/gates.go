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

// Package gates is the rules that gate an agent's turn (spec_gates_041):
// checks written in plain English in .agents/rules/<name>.md, with front
// matter when: before_agent or after_agent, judged in order by a model
// before the agent runs (on the prompt) or after (on its answer and the
// files it changed), each blocking, warning or asking for a revision. A
// rule without when: is an instruction rule (spec_memory_012 MEM-07),
// listed here so Studio can edit it, and loaded by the memory package.
package gates

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/frontmatter"
	"gopkg.in/yaml.v3"
)

// When a gate runs.
const (
	// BeforeAgent gates judge the prompt, before the agent runs.
	BeforeAgent = "before_agent"
	// AfterAgent gates judge the answer and the changed files, before the
	// turn ends.
	AfterAgent = "after_agent"
)

// What a failed gate does.
const (
	// Block stops the turn (before) or marks the answer as failing the
	// rule (after).
	Block = "block"
	// Warn says so in the conversation.
	Warn = "warn"
	// Revise gives the agent the reason: with the prompt (before), or as
	// a request for one more pass (after).
	Revise = "revise"
)

// Rule is one rule file: a gate (When set) or an instruction rule.
type Rule struct {
	// Name is its file's path in the rule folder, without .md.
	Name string
	Path string
	// When is BeforeAgent or AfterAgent for a gate, "" for an instruction
	// rule.
	When string
	// Order sorts gates (then their names): lower first.
	Order int
	// Agents are the agents it gates (none: every one).
	Agents []string
	// Action is Block, Warn or Revise (a gate; Warn when unset).
	Action      string
	Description string
	// Paths scope an instruction rule to files (MEM-07).
	Paths []string
	// Text is the rule, in plain English.
	Text string
	// Problem says why its front matter doesn't hold (it's skipped).
	Problem string
}

// IsGate reports whether the rule gates turns.
func (r Rule) IsGate() bool { return r.When != "" }

// OfferedTo reports whether the rule gates the named agent's turns
// (agents:; none means every agent, and an agent that can't be told is
// gated by all).
func (r Rule) OfferedTo(agent string) bool {
	return len(r.Agents) == 0 || agent == "" || slices.Contains(r.Agents, agent)
}

// meta is a rule file's front matter, as written.
type meta struct {
	When        string   `yaml:"when"`
	Order       int      `yaml:"order"`
	Agents      []string `yaml:"agents"`
	Action      string   `yaml:"action"`
	Description string   `yaml:"description"`
	Paths       any      `yaml:"paths"`
}

// Parse reads a rule file. A front matter that doesn't hold (an unknown
// when or action) is its Problem.
func Parse(name, path string, data []byte) Rule {
	r := Rule{Name: name, Path: path}
	front, body, ok := frontmatter.Split(data)
	r.Text = string(body)
	if !ok {
		return r
	}
	var m meta
	if err := yaml.Unmarshal(front, &m); err != nil {
		r.Problem = fmt.Sprintf("front matter: %v", err)
		return r
	}
	r.When, r.Order, r.Agents, r.Action, r.Description = strings.TrimSpace(m.When), m.Order, m.Agents, strings.TrimSpace(m.Action), m.Description
	switch v := m.Paths.(type) {
	case string:
		r.Paths = []string{v}
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				r.Paths = append(r.Paths, strings.TrimSpace(s))
			}
		}
	}
	switch {
	case r.When != "" && r.When != BeforeAgent && r.When != AfterAgent:
		r.Problem = fmt.Sprintf("when %q: before_agent or after_agent", r.When)
	case r.When != "" && r.Action == "":
		r.Action = Warn
	case r.When != "" && r.Action != Block && r.Action != Warn && r.Action != Revise:
		r.Problem = fmt.Sprintf("action %q: block, warn or revise", r.Action)
	case r.When != "" && strings.TrimSpace(r.Text) == "":
		r.Problem = "the rule is empty: say what to check"
	}
	return r
}

// Load reads every rule file (*.md, recursively) in dirs; a name in an
// earlier folder wins. Gates come first, by when (before, then after),
// order and name; then instruction rules by name.
func Load(dirs ...string) []Rule {
	var out []Rule
	seen := map[string]bool{}
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
				return nil
			}
			rel, _ := filepath.Rel(dir, p)
			name := strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel))
			if seen[name] {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			seen[name] = true
			out = append(out, Parse(name, p, data))
			return nil
		})
	}
	slices.SortStableFunc(out, compare)
	return out
}

// compare orders rules: gates before instruction rules; before-agent
// gates before after-agent ones; then by order and name.
func compare(a, b Rule) int {
	rank := func(r Rule) int {
		switch r.When {
		case BeforeAgent:
			return 0
		case AfterAgent:
			return 1
		}
		return 2
	}
	return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.Order, b.Order), cmp.Compare(a.Name, b.Name))
}

// Gates are the rules that run when for the agent, in order, without
// those whose front matter doesn't hold.
func Gates(rules []Rule, when, agent string) []Rule {
	var out []Rule
	for _, r := range rules {
		if r.When == when && r.Problem == "" && r.OfferedTo(agent) {
			out = append(out, r)
		}
	}
	return out
}

// Verdict is the judge's answer: whether the subject passes the rule,
// and why (for a failure, what to fix).
type Verdict struct {
	Pass   bool
	Reason string
}

// Asker asks a model: an instruction and a prompt, its reply's text.
type Asker func(ctx context.Context, instruction, prompt string) (string, error)

// judgeInstruction is the judge's brief.
const judgeInstruction = `You check one rule, written by the user, against what an AI agent was asked or did. Judge only that rule. Fail only when the rule is clearly broken; when it doesn't apply, it passes. Reply with JSON alone: {"verdict": "pass" or "fail", "reason": "one or two sentences: why, and for a failure what would make it pass"}.`

// Subjects: what a gate judges.
const (
	subjectPrompt = "The user's prompt to the agent"
	subjectAnswer = "The agent's answer, and the files it changed"
)

// Judge has ask judge whether subject (the prompt, for a before-agent
// gate; the answer and changed files, after) passes the rule.
func Judge(ctx context.Context, ask Asker, r Rule, subject string) (Verdict, error) {
	what := subjectPrompt
	if r.When == AfterAgent {
		what = subjectAnswer
	}
	prompt := fmt.Sprintf("The rule (%s):\n%s\n\n%s:\n%s\n", r.Name, strings.TrimSpace(r.Text), what, cmp.Or(strings.TrimSpace(subject), "(nothing)"))
	text, err := ask(ctx, judgeInstruction, prompt)
	if err != nil {
		return Verdict{}, err
	}
	return parseVerdict(text)
}

// parseVerdict reads the judge's JSON (fences and words around it
// tolerated).
func parseVerdict(text string) (Verdict, error) {
	i, j := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if i < 0 || j < i {
		return Verdict{}, fmt.Errorf("the judge's answer isn't JSON: %q", text)
	}
	var v struct {
		Verdict string `json:"verdict"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text[i:j+1]), &v); err != nil {
		return Verdict{}, fmt.Errorf("the judge's answer isn't JSON: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(v.Verdict)) {
	case "pass":
		return Verdict{Pass: true, Reason: strings.TrimSpace(v.Reason)}, nil
	case "fail":
		return Verdict{Reason: strings.TrimSpace(v.Reason)}, nil
	}
	return Verdict{}, fmt.Errorf("the judge answered %q, not pass or fail", v.Verdict)
}

// Spec is what Studio's rule form edits.
type Spec struct {
	Name        string
	When        string
	Action      string
	Order       int
	Agents      []string
	Description string
	Paths       []string
	Text        string
}

// nameRE is a rule's file name: lowercase letters, digits, single
// hyphens or underscores.
var nameRE = regexp.MustCompile(`^[a-z0-9]+([-_][a-z0-9]+)*$`)

// ValidName reports whether name may name a rule file (without .md).
func ValidName(name string) bool { return len(name) <= 64 && nameRE.MatchString(name) }

// ErrUnknown means there's no rule of that name.
var ErrUnknown = errors.New("no such rule")

// Check is what keeps spec from being saved.
func Check(spec Spec) []string {
	var probs []string
	if !ValidName(spec.Name) {
		probs = append(probs, fmt.Sprintf("name %q: lowercase letters, digits and single hyphens, up to 64", spec.Name))
	}
	switch spec.When {
	case "", BeforeAgent, AfterAgent:
	default:
		probs = append(probs, fmt.Sprintf("when %q: before_agent, after_agent, or none for an instruction rule", spec.When))
	}
	if spec.When != "" && spec.Action != Block && spec.Action != Warn && spec.Action != Revise {
		probs = append(probs, fmt.Sprintf("action %q: block, warn or revise", spec.Action))
	}
	if strings.TrimSpace(spec.Text) == "" {
		probs = append(probs, "the rule is empty: say what to check")
	}
	return probs
}

// Save writes dir/<name>.md from spec and returns its path, or the
// problems that keep it from being saved (nothing written). previous is
// the rule edited ("" for a new one); another name renames its file. The
// front matter's other fields are kept.
func Save(dir string, spec Spec, previous string) (string, []string, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	if probs := Check(spec); len(probs) > 0 {
		return "", probs, nil
	}
	target := filepath.Join(dir, spec.Name+".md")
	var old []byte
	if previous != "" {
		p, err := file(dir, previous)
		if err != nil {
			return "", nil, err
		}
		if old, err = os.ReadFile(p); err != nil {
			return "", nil, fmt.Errorf("%w: %q", ErrUnknown, previous)
		}
	}
	if previous != spec.Name {
		if _, err := os.Stat(target); err == nil {
			return "", []string{fmt.Sprintf("name %q: there's a rule by that name", spec.Name)}, nil
		}
	}
	gate := spec.When != ""
	data, err := frontmatter.Edit(old, spec.Text, func(f frontmatter.Fields) {
		f.String("description", strings.TrimSpace(spec.Description))
		f.String("when", spec.When)
		f.String("action", map[bool]string{true: spec.Action}[gate])
		f.Int("order", map[bool]int{true: spec.Order}[gate])
		f.List("agents", map[bool][]string{true: spec.Agents}[gate])
		f.List("paths", map[bool][]string{false: spec.Paths}[gate])
	})
	if err != nil {
		return "", nil, fmt.Errorf("the rule's %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", nil, err
	}
	if previous != "" && previous != spec.Name {
		if p, err := file(dir, previous); err == nil {
			os.Remove(p)
		}
	}
	return target, nil, nil
}

// file is the path of rule name in dir, checked to be a name and to stay
// in dir.
func file(dir, name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("%w: %q", ErrUnknown, name)
	}
	return filepath.Join(dir, name+".md"), nil
}

// Delete removes rule name from dir.
func Delete(dir, name string) error {
	p, err := file(dir, name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("%w: %q", ErrUnknown, name)
	}
	return nil
}

// Reorder gives the gates named, in dir, orders 10, 20, 30… in the order
// given: Studio's dragged lists.
func Reorder(dir string, names []string) error {
	for i, name := range names {
		p, err := file(dir, name)
		if err != nil {
			return err
		}
		old, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("%w: %q", ErrUnknown, name)
		}
		_, body, _ := frontmatter.Split(old)
		data, err := frontmatter.Edit(old, string(body), func(f frontmatter.Fields) { f.Int("order", (i+1)*10) })
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
