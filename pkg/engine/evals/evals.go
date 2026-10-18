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

// Package evals reads and writes eval files (spec_evals_042): cases, each
// a prompt and the output expected of it, in a Markdown table in
// .agents/evals/<name>.md; scores an agent's output against a case; and
// keeps the runs.
package evals

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/frontmatter"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"gopkg.in/yaml.v3"
)

// A case's output type.
const (
	// Exact passes when the output is the expected text (trimmed, line
	// endings and trailing spaces aside).
	Exact = "exact"
	// Near passes when the output's score against the expected text (0 to
	// 1) reaches the case's threshold.
	Near = "near"
)

// A case's scope: what its failure means.
const (
	// Fail fails the eval.
	Fail = "fail"
	// Warn is reported as a warning.
	Warn = "warn"
	// Info is reported only.
	Info = "info"
)

// How near cases are scored.
const (
	// Judge has a model score how close the output is in meaning.
	Judge = "judge"
	// Embedding scores by the cosine similarity of the texts' embeddings
	// (search.embedding_model).
	Embedding = "embedding"
)

// What an eval may allow its cases beyond reading: they never change
// files or run commands.
const (
	AllowNetwork = "network"
	AllowMCP     = "mcp"
)

// DefaultThreshold is a near case's threshold when it has none.
const DefaultThreshold = 0.8

// Case is one row of an eval's table.
type Case struct {
	ID     string
	Name   string
	Prompt string
	// Output is the output expected.
	Output    string
	Type      string
	Threshold float64
	Scope     string
}

// Eval is one eval file.
type Eval struct {
	// Name is its file's name without .md.
	Name        string
	Path        string
	Description string
	// Agents are the agents each case runs on (none: the active one).
	Agents []string
	// Skills are given to the agent with each case's prompt.
	Skills []string
	// Near is how near cases are scored: Judge (the default) or Embedding.
	Near string
	// Allow are what cases may do beyond reading: AllowNetwork, AllowMCP.
	Allow []string
	Cases []Case
	// Intro and Outro are the body's text before and after the table,
	// kept as written.
	Intro, Outro string
	Problem      string
}

type meta struct {
	Description string   `yaml:"description"`
	Agents      []string `yaml:"agents"`
	Skills      []string `yaml:"skills"`
	Near        string   `yaml:"near"`
	Allow       []string `yaml:"allow"`
}

// ErrUnknown means there's no eval of that name.
var ErrUnknown = errors.New("no such eval")

// ValidName reports whether name can be an eval's (its file's) name.
func ValidName(name string) bool { return gates.ValidName(name) }

// Parse reads an eval file; what doesn't hold is its Problem.
func Parse(name, path string, data []byte) Eval {
	e := Eval{Name: name, Path: path}
	front, body, ok := frontmatter.Split(data)
	var m meta
	if ok {
		if err := yaml.Unmarshal(front, &m); err != nil {
			e.Problem = fmt.Sprintf("front matter: %v", err)
			return e
		}
	}
	e.Description, e.Agents, e.Skills, e.Allow = strings.TrimSpace(m.Description), m.Agents, m.Skills, m.Allow
	e.Near = cmp.Or(strings.ToLower(strings.TrimSpace(m.Near)), Judge)
	var err error
	if e.Intro, e.Cases, e.Outro, err = ParseTable(string(body)); err != nil {
		e.Problem = err.Error()
		return e
	}
	if probs := Check(e); len(probs) > 0 {
		e.Problem = strings.Join(probs, "; ")
	}
	return e
}

// Load reads every eval (*.md) in dir, by name.
func Load(dir string) []Eval {
	entries, _ := os.ReadDir(dir)
	var out []Eval
	for _, ent := range entries {
		if !ent.Type().IsRegular() || !strings.HasSuffix(strings.ToLower(ent.Name()), ".md") {
			continue
		}
		p := filepath.Join(dir, ent.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out = append(out, Parse(strings.TrimSuffix(ent.Name(), filepath.Ext(ent.Name())), p, data))
	}
	slices.SortFunc(out, func(a, b Eval) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// columns are the table's columns as written, each with the names it's
// read by (lower case).
var columns = []struct {
	title string
	names []string
}{
	{"ID", []string{"id"}},
	{"Name", []string{"name"}},
	{"Prompt", []string{"prompt", "input"}},
	{"Output", []string{"output", "expected", "expected output"}},
	{"Output type", []string{"output type", "type"}},
	{"Threshold", []string{"threshold"}},
	{"Scope", []string{"scope"}},
}

// ParseTable reads the cases from the first Markdown table in body,
// returning the text around it. Columns are found by their titles, in
// any order; an empty type is near, an empty scope fail, an empty near
// threshold DefaultThreshold.
func ParseTable(body string) (intro string, cases []Case, outro string, err error) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	start := slices.IndexFunc(lines, isRow)
	if start < 0 {
		return strings.TrimSpace(body), nil, "", nil
	}
	end := start
	for end < len(lines) && isRow(lines[end]) {
		end++
	}
	intro = strings.TrimSpace(strings.Join(lines[:start], "\n"))
	outro = strings.TrimSpace(strings.Join(lines[end:], "\n"))
	head := splitRow(lines[start])
	col := map[int]int{} // a cell's column → its index in columns
	for i, h := range head {
		h = strings.ToLower(strings.TrimSpace(h))
		for j, c := range columns {
			if slices.Contains(c.names, h) {
				col[i] = j
			}
		}
	}
	for _, need := range []int{0, 2, 3} {
		if !slices.Contains(mapValues(col), need) {
			return intro, nil, outro, fmt.Errorf("the table has no %s column", columns[need].title)
		}
	}
	for n, line := range lines[start+1 : end] {
		cells := splitRow(line)
		if isSeparator(cells) {
			continue
		}
		var c Case
		var threshold string
		for i, cell := range cells {
			j, ok := col[i]
			if !ok {
				continue
			}
			v := unescape(strings.TrimSpace(cell))
			switch j {
			case 0:
				c.ID = v
			case 1:
				c.Name = v
			case 2:
				c.Prompt = v
			case 3:
				c.Output = v
			case 4:
				c.Type = strings.ToLower(v)
			case 5:
				threshold = v
			case 6:
				c.Scope = strings.ToLower(v)
			}
		}
		c.Type, c.Scope = cmp.Or(c.Type, Near), cmp.Or(c.Scope, Fail)
		if threshold != "" {
			t, perr := strconv.ParseFloat(strings.TrimSuffix(threshold, "%"), 64)
			if perr != nil {
				return intro, nil, outro, fmt.Errorf("row %d: threshold %q isn't a number", n+1, threshold)
			}
			if strings.HasSuffix(threshold, "%") {
				t /= 100
			}
			c.Threshold = t
		}
		if c.Type == Near && c.Threshold == 0 {
			c.Threshold = DefaultThreshold
		}
		cases = append(cases, c)
	}
	return intro, cases, outro, nil
}

func mapValues(m map[int]int) []int {
	out := make([]int, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func isRow(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "|") }

// splitRow splits a table row into its cells (\| is a | in a cell).
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			b.WriteString(`\|`)
			i++
		case line[i] == '|':
			cells = append(cells, b.String())
			b.Reset()
		default:
			b.WriteByte(line[i])
		}
	}
	return append(cells, b.String())
}

func isSeparator(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(strings.TrimSpace(c), ":-") != "" {
			return false
		}
	}
	return true
}

// A cell holds one line: a line break is <br>, a | is \|.
var (
	escaper   = strings.NewReplacer("\r\n", "<br>", "\n", "<br>", "|", `\|`)
	unescaper = strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", `\|`, "|")
)

func unescape(s string) string { return unescaper.Replace(s) }

// Table writes cases as a Markdown table.
func Table(cases []Case) string {
	var b strings.Builder
	titles := make([]string, len(columns))
	rule := make([]string, len(columns))
	for i, c := range columns {
		titles[i], rule[i] = c.title, strings.Repeat("-", max(3, len(c.title)))
	}
	b.WriteString("| " + strings.Join(titles, " | ") + " |\n")
	b.WriteString("| " + strings.Join(rule, " | ") + " |\n")
	for _, c := range cases {
		threshold := ""
		if c.Type == Near {
			threshold = strconv.FormatFloat(cmp.Or(c.Threshold, DefaultThreshold), 'f', -1, 64)
		}
		cells := []string{c.ID, c.Name, c.Prompt, c.Output, title(c.Type), threshold, title(c.Scope)}
		for i := range cells {
			cells[i] = escaper.Replace(strings.TrimSpace(cells[i]))
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	return b.String()
}

func title(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Check says what doesn't hold in e: its name, how it scores, what it
// allows, and each case.
func Check(e Eval) []string {
	var probs []string
	if !ValidName(e.Name) {
		probs = append(probs, fmt.Sprintf("name %q: lowercase letters, digits and single hyphens, up to 64", e.Name))
	}
	if e.Near != Judge && e.Near != Embedding {
		probs = append(probs, fmt.Sprintf("near %q: judge or embedding", e.Near))
	}
	for _, a := range e.Allow {
		if a != AllowNetwork && a != AllowMCP {
			probs = append(probs, fmt.Sprintf("allow %q: network or mcp (cases never change files or run commands)", a))
		}
	}
	seen := map[string]bool{}
	for i, c := range e.Cases {
		where := fmt.Sprintf("case %d", i+1)
		if c.ID != "" {
			where = fmt.Sprintf("case %s", c.ID)
		}
		switch {
		case c.ID == "":
			probs = append(probs, where+": it needs an ID")
		case seen[c.ID]:
			probs = append(probs, where+": another case has that ID")
		}
		seen[c.ID] = true
		if strings.TrimSpace(c.Prompt) == "" {
			probs = append(probs, where+": it needs a prompt")
		}
		if c.Type != Exact && c.Type != Near {
			probs = append(probs, fmt.Sprintf("%s: output type %q: exact or near", where, c.Type))
		}
		if c.Type == Near && (c.Threshold <= 0 || c.Threshold > 1) {
			probs = append(probs, fmt.Sprintf("%s: threshold %v: above 0, up to 1", where, c.Threshold))
		}
		if c.Scope != Fail && c.Scope != Warn && c.Scope != Info {
			probs = append(probs, fmt.Sprintf("%s: scope %q: fail, warn or info", where, c.Scope))
		}
	}
	return probs
}

// Save writes dir/<name>.md from e (its Path and Problem aside), keeping
// the file's other front matter, and returns its path, or the problems
// that keep it from being saved. previous is the eval edited ("" for a
// new one).
func Save(dir string, e Eval, previous string) (string, []string, error) {
	e.Name = strings.TrimSpace(e.Name)
	e.Near = cmp.Or(e.Near, Judge)
	for i := range e.Cases {
		c := &e.Cases[i]
		c.ID, c.Type, c.Scope = strings.TrimSpace(c.ID), cmp.Or(c.Type, Near), cmp.Or(c.Scope, Fail)
		switch {
		case c.Type == Exact:
			c.Threshold = 0
		case c.Threshold == 0:
			c.Threshold = DefaultThreshold
		}
	}
	if probs := Check(e); len(probs) > 0 {
		return "", probs, nil
	}
	target := filepath.Join(dir, e.Name+".md")
	var old []byte
	if previous != "" {
		if !ValidName(previous) {
			return "", nil, fmt.Errorf("%w: %q", ErrUnknown, previous)
		}
		var err error
		if old, err = os.ReadFile(filepath.Join(dir, previous+".md")); err != nil {
			return "", nil, fmt.Errorf("%w: %q", ErrUnknown, previous)
		}
	}
	if previous != e.Name {
		if _, err := os.Stat(target); err == nil {
			return "", []string{fmt.Sprintf("name %q: there's an eval by that name", e.Name)}, nil
		}
	}
	var body []string
	for _, part := range []string{strings.TrimSpace(e.Intro), strings.TrimSuffix(Table(e.Cases), "\n"), strings.TrimSpace(e.Outro)} {
		if part != "" {
			body = append(body, part)
		}
	}
	data, err := frontmatter.Edit(old, strings.Join(body, "\n\n"), func(f frontmatter.Fields) {
		f.String("description", strings.TrimSpace(e.Description))
		f.List("agents", e.Agents)
		f.List("skills", e.Skills)
		f.String("near", map[bool]string{true: Embedding}[e.Near == Embedding])
		f.List("allow", e.Allow)
	})
	if err != nil {
		return "", nil, fmt.Errorf("the eval's %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", nil, err
	}
	if previous != "" && previous != e.Name {
		os.Remove(filepath.Join(dir, previous+".md"))
	}
	return target, nil, nil
}

// Delete removes eval name from dir.
func Delete(dir, name string) error {
	if !ValidName(name) {
		return fmt.Errorf("%w: %q", ErrUnknown, name)
	}
	if err := os.Remove(filepath.Join(dir, name+".md")); err != nil {
		return fmt.Errorf("%w: %q", ErrUnknown, name)
	}
	return nil
}

// ExactMatch reports whether output is expected: both trimmed, line
// endings and each line's trailing spaces aside.
func ExactMatch(expected, output string) bool {
	norm := func(s string) string {
		lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
		for i, l := range lines {
			lines[i] = strings.TrimRight(l, " \t")
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return norm(expected) == norm(output)
}

// nearInstruction is the judge's brief for a near case.
const nearInstruction = `You score how well an AI agent's output matches the output expected for a prompt, in meaning: what it says and does, not its wording or format. 1 is the same in substance; 0 is unrelated or contradictory; in between for something partly right (missing or extra points, a wrong detail). Reply with JSON alone: {"score": a number from 0 to 1, "reason": "one or two sentences: what matches and what doesn't"}.`

// JudgeNear has the judge score output against expected (0 to 1), for
// prompt.
func JudgeNear(ctx context.Context, ask gates.Asker, prompt, expected, output string) (float64, string, error) {
	text, err := ask(ctx, nearInstruction, fmt.Sprintf("The prompt:\n%s\n\nThe output expected:\n%s\n\nThe agent's output:\n%s", prompt, expected, output))
	if err != nil {
		return 0, "", err
	}
	i, j := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if i < 0 || j < i {
		return 0, "", fmt.Errorf("the judge's answer isn't JSON: %q", text)
	}
	var v struct {
		Score  *float64 `json:"score"`
		Reason string   `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text[i:j+1]), &v); err != nil || v.Score == nil {
		return 0, "", fmt.Errorf("the judge's answer has no score: %q", text)
	}
	return math.Max(0, math.Min(1, *v.Score)), strings.TrimSpace(v.Reason), nil
}

// Cosine is the cosine similarity of a and b, from 0 (or below, for
// opposite vectors: 0) to 1.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return math.Max(0, dot/(math.Sqrt(na)*math.Sqrt(nb)))
}
