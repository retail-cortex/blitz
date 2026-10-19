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

// Package health is a workspace's dashboard (spec_health_044): the facts
// about what it holds and how its runs go, taken from what it already
// records (no model call), and the judge's verdicts on each item's
// quality, kept with their history.
package health

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/audit"
)

// The kinds of item, in the order the dashboard shows them.
const (
	KindAgent     = "agent"
	KindSkill     = "skill"
	KindRule      = "rule"
	KindGuardrail = "guardrail"
	KindEval      = "eval"
	KindWorkflow  = "workflow"
	KindWorker    = "worker"
)

// Kinds are the kinds of item, in order.
var Kinds = []string{KindAgent, KindSkill, KindRule, KindGuardrail, KindEval, KindWorkflow, KindWorker}

// What an item's flags say about it.
const (
	FlagUncovered = "uncovered" // an agent no eval checks
	FlagStale     = "stale"     // an agent changed since its evals last ran
	FlagUnused    = "unused"    // a skill or rule nothing uses, a worker that hasn't run
	FlagNeverRun  = "never_run" // a workflow or eval never run
	FlagFailing   = "failing"   // most of its recent runs failed
	FlagDisabled  = "disabled"  // a worker switched off
	FlagOff       = "off"       // a guardrail switched off
)

// Window is how far back the facts look.
const Window = 30 * 24 * time.Hour

// Days is how many days the charts cover.
const Days = 30

// Run is one run of a workflow or a worker, as the facts count it.
type Run struct {
	Started  time.Time
	Duration time.Duration
	// Status is done, failed or stopped.
	Status  string
	CostUSD float64
	// FailedStep is the step a workflow run failed at.
	FailedStep string
}

// The statuses a Run has.
const (
	RunDone    = "done"
	RunFailed  = "failed"
	RunStopped = "stopped"
)

// EvalRun is one run of an eval: when, and its cases' results.
type EvalRun struct {
	Started time.Time
	Results []EvalResult
}

// EvalResult is one case's result for one agent.
type EvalResult struct {
	Agent  string
	Passed bool
}

// Inputs are what the facts are made from: the workspace's items and
// their records, gathered by the engine.
type Inputs struct {
	Now time.Time
	// DefaultAgent is who an eval naming no agent checks.
	DefaultAgent string
	Agents       []Source
	Skills       []Source
	Rules        []Source
	Guardrails   []Source
	Evals        []Source
	Workflows    []Source
	Workers      []Source
	// Audit is the workspace's audit entries in the window.
	Audit []audit.Entry
}

// Source is one item as the engine read it.
type Source struct {
	Name, Path string
	// Detail is what kind of that kind it is: a skill's type, a rule's
	// stage, a guardrail's built-in check.
	Detail  string
	Problem string
	// Hash is the file's content's (verdicts follow it).
	Hash     string
	Modified time.Time
	// Agents are those a rule, a skill or an eval is for (empty: all, or
	// for an eval its default agent).
	Agents []string
	// Skills are those an agent step, a meta skill or an eval uses.
	Skills []string
	// Off is a guardrail switched off or a worker disabled.
	Off bool
	// Cases is an eval's number of cases.
	Cases    int
	Runs     []Run
	EvalRuns []EvalRun
}

// Facts are a workspace's dashboard without the judge's verdicts.
type Facts struct {
	At    time.Time
	Items []Item
	// Days are the runs per day, oldest first, the last of them today.
	Days    []Day
	Signals []Signal
}

// Item is one agent, skill, rule, guardrail, eval, workflow or worker.
type Item struct {
	Kind, Name, Path, Detail string
	Problem                  string
	Hash                     string
	Flags                    []string
	// Uses are its runs (a workflow, a worker, an eval), activations (a
	// skill) or verdicts (a rule, a guardrail) in the window: the weight of
	// its grade.
	Uses int
	// Fired is how often a rule or a guardrail stopped, warned or revised.
	Fired int
	Runs  *RunStats
	Evals *EvalStats
	// Daily are its uses per day, as Days.
	Daily []int
}

// Key is the item's identity in verdicts: kind and name.
func (it Item) Key() string { return it.Kind + "/" + it.Name }

// RunStats are a workflow's or a worker's runs in the window.
type RunStats struct {
	Runs, Done, Failed, Stopped int
	CostUSD                     float64
	Median                      time.Duration
	Last                        time.Time
	// FailingStep is the step its runs fail at most.
	FailingStep string
}

// SuccessRate is the share of runs done (0 without runs).
func (r RunStats) SuccessRate() float64 {
	if r.Runs == 0 {
		return 0
	}
	return float64(r.Done) / float64(r.Runs)
}

// EvalStats are an agent's or an eval's latest results.
type EvalStats struct {
	// Runs are the eval runs in the window.
	Runs   int
	Last   time.Time
	Passed int
	Cases  int
}

// PassRate is the share of cases passed (0 without cases).
func (e EvalStats) PassRate() float64 {
	if e.Cases == 0 {
		return 0
	}
	return float64(e.Passed) / float64(e.Cases)
}

// Day is one day's runs.
type Day struct {
	Date                  string
	Done, Failed, Stopped int
	CostUSD               float64
}

// Signal is a safety signal in the window: the auto reviewer's denials,
// a guardrail's or a rule's decisions, by what they were about.
type Signal struct {
	// Kind is denial, guardrail or rule.
	Kind  string
	About string
	Count int
	Last  time.Time
	// Detail is the latest one's why.
	Detail string
}

// Build makes the facts from in.
func Build(in Inputs) Facts {
	now := in.Now
	start := dayStart(now).AddDate(0, 0, -(Days - 1))
	day := func(t time.Time) int {
		if t.Before(start) || t.After(now) {
			return -1
		}
		return int(dayStart(t).Sub(start).Hours()/24 + 0.5)
	}
	f := Facts{At: now, Days: make([]Day, Days)}
	for i := range f.Days {
		f.Days[i].Date = start.AddDate(0, 0, i).Format(time.DateOnly)
	}

	// What the audit says: activations, rules' and guardrails' decisions,
	// the reviewer's denials.
	activations, fired, verdicts := map[string][]int{}, map[string]int{}, map[string]int{}
	signals := map[string]*Signal{}
	signal := func(kind, about, detail string, at time.Time) {
		s := signals[kind+"/"+about]
		if s == nil {
			s = &Signal{Kind: kind, About: about}
			signals[kind+"/"+about] = s
		}
		s.Count++
		if !at.Before(s.Last) {
			s.Last, s.Detail = at, detail
		}
	}
	for _, e := range in.Audit {
		switch {
		case e.Kind == audit.KindToolCall && (e.Tool == "activate_skill" || e.Tool == "run_skill_script"):
			if name := argString(e.Args, "skill_name", "skill", "name"); name != "" {
				activations[name] = append(activations[name], day(e.Time))
			}
		case e.Kind == audit.KindRule || e.Kind == audit.KindGuardrail:
			name := strings.TrimPrefix(strings.TrimPrefix(e.Tool, "rule:"), "guardrail:")
			verdicts[e.Kind+"/"+name]++
			if d := strings.ToLower(e.Decision); d != "" && d != "pass" && d != "allow" {
				fired[e.Kind+"/"+name]++
				signal(e.Kind, name, e.Detail, e.Time)
			}
		case e.Kind == audit.KindDenial && strings.HasPrefix(e.Decision, "auto-deny"):
			signal("denial", e.Tool, strings.TrimSpace(strings.TrimPrefix(e.Decision, "auto-deny:")), e.Time)
		}
	}

	// What uses what.
	usedSkills := map[string]bool{}
	agents := map[string]bool{}
	for _, a := range in.Agents {
		agents[a.Name] = true
	}
	for _, list := range [][]Source{in.Skills, in.Workflows, in.Evals} {
		for _, s := range list {
			for _, name := range s.Skills {
				usedSkills[name] = true
			}
		}
	}

	// Each agent's evals: the latest run of each eval that checks it.
	type cover struct {
		last          time.Time
		passed, cases int
		runs          int
	}
	covers := map[string]*cover{}
	for _, e := range in.Evals {
		checks := e.Agents
		if len(checks) == 0 && in.DefaultAgent != "" {
			checks = []string{in.DefaultAgent}
		}
		latest := latestEvalRun(e.EvalRuns)
		for _, a := range checks {
			c := covers[a]
			if c == nil {
				c = &cover{}
				covers[a] = c
			}
			if latest == nil {
				continue
			}
			if latest.Started.After(c.last) {
				c.last = latest.Started
			}
			for _, r := range latest.Results {
				if r.Agent == a || r.Agent == "" {
					c.cases++
					if r.Passed {
						c.passed++
					}
				}
			}
			c.runs += countSince(e.EvalRuns, now.Add(-Window))
		}
	}

	add := func(kind string, s Source) *Item {
		it := Item{Kind: kind, Name: s.Name, Path: s.Path, Detail: s.Detail, Problem: s.Problem, Hash: s.Hash, Daily: make([]int, Days)}
		f.Items = append(f.Items, it)
		return &f.Items[len(f.Items)-1]
	}
	for _, s := range in.Agents {
		it := add(KindAgent, s)
		c := covers[s.Name]
		switch {
		case c == nil:
			it.Flags = append(it.Flags, FlagUncovered)
		default:
			it.Evals = &EvalStats{Runs: c.runs, Last: c.last, Passed: c.passed, Cases: c.cases}
			if c.last.IsZero() || s.Modified.After(c.last) {
				it.Flags = append(it.Flags, FlagStale)
			}
		}
	}
	for _, s := range in.Skills {
		it := add(KindSkill, s)
		for _, d := range activations[s.Name] {
			it.Uses++
			if d >= 0 {
				it.Daily[d]++
			}
		}
		if it.Uses == 0 && !usedSkills[s.Name] {
			it.Flags = append(it.Flags, FlagUnused)
		}
	}
	for _, s := range in.Rules {
		it := add(KindRule, s)
		it.Uses, it.Fired = verdicts[audit.KindRule+"/"+s.Name], fired[audit.KindRule+"/"+s.Name]
		if len(s.Agents) > 0 && !slices.ContainsFunc(s.Agents, func(a string) bool { return agents[a] }) {
			it.Flags = append(it.Flags, FlagUnused)
		}
	}
	for _, s := range in.Guardrails {
		it := add(KindGuardrail, s)
		it.Uses, it.Fired = verdicts[audit.KindGuardrail+"/"+s.Name], fired[audit.KindGuardrail+"/"+s.Name]
		if s.Off {
			it.Flags = append(it.Flags, FlagOff)
		}
	}
	for _, s := range in.Evals {
		it := add(KindEval, s)
		latest := latestEvalRun(s.EvalRuns)
		if latest == nil {
			it.Flags = append(it.Flags, FlagNeverRun)
			continue
		}
		st := &EvalStats{Runs: countSince(s.EvalRuns, now.Add(-Window)), Last: latest.Started}
		for _, r := range latest.Results {
			st.Cases++
			if r.Passed {
				st.Passed++
			}
		}
		it.Evals, it.Uses = st, st.Runs
		for _, r := range s.EvalRuns {
			if d := day(r.Started); d >= 0 {
				it.Daily[d]++
			}
		}
	}
	runs := func(kind string, s Source, chart bool) {
		it := add(kind, s)
		if s.Off {
			it.Flags = append(it.Flags, FlagDisabled)
		}
		st := runStats(s.Runs, now.Add(-Window))
		it.Runs, it.Uses = &st, st.Runs
		for _, r := range s.Runs {
			d := day(r.Started)
			if d < 0 {
				continue
			}
			it.Daily[d]++
			if chart {
				switch r.Status {
				case RunDone:
					f.Days[d].Done++
				case RunFailed:
					f.Days[d].Failed++
				default:
					f.Days[d].Stopped++
				}
				f.Days[d].CostUSD += r.CostUSD
			}
		}
		switch {
		case st.Runs >= 2 && st.Failed*2 > st.Runs:
			it.Flags = append(it.Flags, FlagFailing)
		case len(s.Runs) == 0 && kind == KindWorkflow:
			it.Flags = append(it.Flags, FlagNeverRun)
		case st.Runs == 0 && kind == KindWorker && !s.Off:
			it.Flags = append(it.Flags, FlagUnused)
		}
	}
	for _, s := range in.Workflows {
		runs(KindWorkflow, s, true)
	}
	// A worker running a workflow is charted by the workflow's receipts.
	for _, s := range in.Workers {
		runs(KindWorker, s, s.Detail == "")
	}

	for _, s := range signals {
		f.Signals = append(f.Signals, *s)
	}
	slices.SortFunc(f.Signals, func(a, b Signal) int { return cmp.Or(b.Count-a.Count, cmp.Compare(a.Kind+a.About, b.Kind+b.About)) })
	return f
}

// runStats counts the runs since.
func runStats(runs []Run, since time.Time) RunStats {
	var st RunStats
	var took []time.Duration
	failedAt := map[string]int{}
	for _, r := range runs {
		if r.Started.After(st.Last) {
			st.Last = r.Started
		}
		if r.Started.Before(since) {
			continue
		}
		st.Runs++
		st.CostUSD += r.CostUSD
		took = append(took, r.Duration)
		switch r.Status {
		case RunDone:
			st.Done++
		case RunFailed:
			st.Failed++
			if r.FailedStep != "" {
				failedAt[r.FailedStep]++
			}
		default:
			st.Stopped++
		}
	}
	if len(took) > 0 {
		slices.Sort(took)
		st.Median = took[len(took)/2]
	}
	for step, n := range failedAt {
		if n > failedAt[st.FailingStep] || n == failedAt[st.FailingStep] && step < st.FailingStep {
			st.FailingStep = step
		}
	}
	return st
}

func latestEvalRun(runs []EvalRun) *EvalRun {
	var latest *EvalRun
	for i := range runs {
		if latest == nil || runs[i].Started.After(latest.Started) {
			latest = &runs[i]
		}
	}
	return latest
}

func countSince(runs []EvalRun, since time.Time) int {
	n := 0
	for _, r := range runs {
		if !r.Started.Before(since) {
			n++
		}
	}
	return n
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func argString(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := args[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
