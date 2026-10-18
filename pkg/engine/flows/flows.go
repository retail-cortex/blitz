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

// Package flows reads, checks and writes workflows (spec_workflows_043):
// steps joined into a graph, in .agents/workflows/<name>.flow.yaml, beside
// the .md commands there.
package flows

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"gopkg.in/yaml.v3"
)

// Suffix ends a workflow's file name.
const Suffix = ".flow.yaml"

// A step's type.
const (
	// Agent runs an agent on the step's instructions and its input.
	Agent = "agent"
	// Refine has an agent improve its input as the instructions say.
	Refine = "refine"
	// Script runs a skill's script, its input as its last argument.
	Script = "script"
	// Browser has an agent reach a goal with its browser tool.
	Browser = "browser"
	// Accumulate joins its inputs, each under its step's title.
	Accumulate = "accumulate"
	// Summarize has an agent sum up its input.
	Summarize = "summarize"
	// ReadFile reads a workspace file: its text is the step's output.
	ReadFile = "read_file"
	// SaveFile writes its input to a workspace file (asked first).
	SaveFile = "save_file"
	// Email and Data are shown, not run yet.
	Email = "email"
	Data  = "data"
)

// Types are the step types, in the order the palette shows them.
var Types = []string{Agent, Refine, Summarize, Accumulate, Browser, Script, ReadFile, SaveFile, Email, Data}

// comingSoon are the types shown but not run yet.
var comingSoon = []string{Email, Data}

// Position is where a step sits on the canvas.
type Position struct {
	X float64 `yaml:"x"`
	Y float64 `yaml:"y"`
}

// Node is a step.
type Node struct {
	ID       string   `yaml:"id"`
	Type     string   `yaml:"type"`
	Title    string   `yaml:"title,omitempty"`
	Position Position `yaml:"position"`
	// Agent is the agent an agent, refine, summarize or browser step runs
	// (none: the active one).
	Agent string `yaml:"agent,omitempty"`
	// Skills are given to the agent with the step's prompt.
	Skills []string `yaml:"skills,omitempty"`
	// Instructions are what the step is to do (a browser step's goal).
	Instructions string `yaml:"instructions,omitempty"`
	// Rules are rules the step's output must pass (judged, as after-agent
	// gates are: spec_gates_041).
	Rules []string `yaml:"rules,omitempty"`
	// Skill and ScriptName are a script step's script; Args its arguments.
	Skill      string   `yaml:"skill,omitempty"`
	ScriptName string   `yaml:"script,omitempty"`
	Args       []string `yaml:"args,omitempty"`
	// Path is a read_file or save_file step's file, in the workspace.
	Path string `yaml:"path,omitempty"`
}

// Edge carries a step's output to another.
type Edge struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// Flow is a workflow file.
type Flow struct {
	// Name is its file's name without .flow.yaml: run as /name.
	Name        string `yaml:"-"`
	Path        string `yaml:"-"`
	Description string `yaml:"description,omitempty"`
	Nodes       []Node `yaml:"nodes"`
	Edges       []Edge `yaml:"edges,omitempty"`
	Problem     string `yaml:"-"`
}

// ErrUnknown means there's no workflow of that name.
var ErrUnknown = errors.New("no such workflow")

// ValidName reports whether name can be a workflow's.
func ValidName(name string) bool { return gates.ValidName(name) }

// IsFlow reports whether a file name is a workflow's.
func IsFlow(file string) bool { return strings.HasSuffix(strings.ToLower(file), Suffix) }

// Parse reads a workflow file; what doesn't hold is its Problem.
func Parse(name, path string, data []byte) Flow {
	var f Flow
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Flow{Name: name, Path: path, Problem: fmt.Sprintf("the file isn't a workflow: %v", err)}
	}
	f.Name, f.Path = name, path
	if probs := Check(f); len(probs) > 0 {
		f.Problem = strings.Join(probs, "; ")
	}
	return f
}

// Load reads every workflow (*.flow.yaml) in dir, by name.
func Load(dir string) []Flow {
	entries, _ := os.ReadDir(dir)
	var out []Flow
	for _, e := range entries {
		if !e.Type().IsRegular() || !IsFlow(e.Name()) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out = append(out, Parse(e.Name()[:len(e.Name())-len(Suffix)], p, data))
	}
	slices.SortFunc(out, func(a, b Flow) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// Check says what doesn't hold in f: its name, its steps' types and
// settings, its edges, and loops.
func Check(f Flow) []string {
	var probs []string
	if !ValidName(f.Name) {
		probs = append(probs, fmt.Sprintf("name %q: lowercase letters, digits and single hyphens, up to 64", f.Name))
	}
	if len(f.Nodes) == 0 {
		probs = append(probs, "it has no steps")
	}
	ids := map[string]bool{}
	for i, n := range f.Nodes {
		where := fmt.Sprintf("step %d", i+1)
		if n.ID != "" {
			where = fmt.Sprintf("step %s", n.ID)
		}
		switch {
		case n.ID == "":
			probs = append(probs, where+": it needs an ID")
		case ids[n.ID]:
			probs = append(probs, where+": another step has that ID")
		}
		ids[n.ID] = true
		switch {
		case !slices.Contains(Types, n.Type):
			probs = append(probs, fmt.Sprintf("%s: type %q: one of %s", where, n.Type, strings.Join(Types, ", ")))
		case slices.Contains(comingSoon, n.Type):
			probs = append(probs, fmt.Sprintf("%s: %s steps don't run yet", where, n.Type))
		case (n.Type == Agent || n.Type == Refine || n.Type == Browser) && strings.TrimSpace(n.Instructions) == "":
			probs = append(probs, where+": say what it's to do")
		case n.Type == Script && (n.Skill == "" || n.ScriptName == ""):
			probs = append(probs, where+": name the skill and its script")
		case (n.Type == ReadFile || n.Type == SaveFile) && strings.TrimSpace(n.Path) == "":
			probs = append(probs, where+": name the file")
		}
	}
	for _, e := range f.Edges {
		if !ids[e.From] || !ids[e.To] {
			probs = append(probs, fmt.Sprintf("the link %s → %s: no such step", e.From, e.To))
		} else if e.From == e.To {
			probs = append(probs, fmt.Sprintf("the link %s → %s: a step can't feed itself", e.From, e.To))
		}
	}
	if len(probs) == 0 {
		if _, err := Order(f); err != nil {
			probs = append(probs, err.Error())
		}
	}
	return probs
}

// Order is f's steps in the order they run: each after the steps that
// feed it, otherwise as listed. A loop is an error.
func Order(f Flow) ([]Node, error) {
	in := map[string]int{}
	next := map[string][]string{}
	for _, e := range f.Edges {
		in[e.To]++
		next[e.From] = append(next[e.From], e.To)
	}
	byID := map[string]Node{}
	var ready []string
	for _, n := range f.Nodes {
		byID[n.ID] = n
		if in[n.ID] == 0 {
			ready = append(ready, n.ID)
		}
	}
	listed := func(id string) int { return slices.IndexFunc(f.Nodes, func(n Node) bool { return n.ID == id }) }
	var out []Node
	for len(ready) > 0 {
		slices.SortFunc(ready, func(a, b string) int { return cmp.Compare(listed(a), listed(b)) })
		id := ready[0]
		ready = ready[1:]
		out = append(out, byID[id])
		for _, to := range next[id] {
			if in[to]--; in[to] == 0 {
				ready = append(ready, to)
			}
		}
	}
	if len(out) < len(f.Nodes) {
		return nil, errors.New("the steps loop: a step can't feed one that feeds it")
	}
	return out, nil
}

// Inputs are the steps that feed id, in the order their links are listed.
func Inputs(f Flow, id string) []string {
	var out []string
	for _, e := range f.Edges {
		if e.To == id {
			out = append(out, e.From)
		}
	}
	return out
}

// Save writes dir/<name>.flow.yaml from f and returns its path, or the
// problems that keep it from being saved. previous is the workflow edited
// ("" for a new one).
func Save(dir string, f Flow, previous string) (string, []string, error) {
	f.Name = strings.TrimSpace(f.Name)
	if probs := saveChecks(f); len(probs) > 0 {
		return "", probs, nil
	}
	target := filepath.Join(dir, f.Name+Suffix)
	if previous != "" {
		if !ValidName(previous) {
			return "", nil, fmt.Errorf("%w: %q", ErrUnknown, previous)
		}
		if _, err := os.Stat(filepath.Join(dir, previous+Suffix)); err != nil {
			return "", nil, fmt.Errorf("%w: %q", ErrUnknown, previous)
		}
	}
	if previous != f.Name {
		if _, err := os.Stat(target); err == nil {
			return "", []string{fmt.Sprintf("name %q: there's a workflow by that name", f.Name)}, nil
		}
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", nil, err
	}
	if previous != "" && previous != f.Name {
		os.Remove(filepath.Join(dir, previous+Suffix))
	}
	return target, nil, nil
}

// saveChecks are what keep a workflow from being saved: its name, IDs,
// types and links. A step not filled in yet, or a coming-soon one, is
// saved (a draft), and is the workflow's problem until it's fixed.
func saveChecks(f Flow) []string {
	var probs []string
	for _, p := range Check(f) {
		if strings.Contains(p, "say what it's to do") || strings.Contains(p, "name the") || strings.Contains(p, "don't run yet") || p == "it has no steps" {
			continue
		}
		probs = append(probs, p)
	}
	return probs
}

// Delete removes workflow name from dir.
func Delete(dir, name string) error {
	if !ValidName(name) {
		return fmt.Errorf("%w: %q", ErrUnknown, name)
	}
	if err := os.Remove(filepath.Join(dir, name+Suffix)); err != nil {
		return fmt.Errorf("%w: %q", ErrUnknown, name)
	}
	return nil
}
