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

package api

import (
	"errors"
	"time"
)

// Evals (spec_evals_042): cases in .agents/evals/<name>.md, run against
// agents and scored.

// An eval result's status: it passed, or what its failure means by the
// case's scope; error is a case that couldn't be run or scored.
const (
	EvalPass  = "pass"
	EvalFail  = "fail"
	EvalWarn  = "warn"
	EvalInfo  = "info"
	EvalError = "error"
)

// An eval run's status.
const (
	EvalRunning = "running"
	EvalPassed  = "passed"
	EvalFailed  = "failed"
	EvalStopped = "stopped"
)

// ErrUnknownEval means the workspace has no eval of that name.
var ErrUnknownEval = errors.New("no such eval")

// ErrEvalNotRunnable means the eval has a problem, or no case to run.
var ErrEvalNotRunnable = errors.New("the eval can't run")

// EvalResult is one case run by one agent, and its score.
type EvalResult struct {
	Case  string `json:"case"`
	Name  string `json:"name,omitempty"`
	Agent string `json:"agent"`
	// Type is exact or near; Scope is fail, warn or info.
	Type  string `json:"type"`
	Scope string `json:"scope"`
	// Status is EvalPass, or EvalFail, EvalWarn or EvalInfo (the case's
	// scope) when it didn't pass, or EvalError.
	Status    string  `json:"status"`
	Score     float64 `json:"score"`
	Threshold float64 `json:"threshold,omitempty"`
	Prompt    string  `json:"prompt"`
	Expected  string  `json:"expected"`
	Output    string  `json:"output"`
	// Reason is the judge's, or why the case couldn't be run or scored.
	Reason string `json:"reason,omitempty"`
	// Refusals are the actions the eval didn't allow, as "tool: detail".
	Refusals []string      `json:"refusals,omitempty"`
	CostUSD  float64       `json:"cost_usd,omitempty"`
	Duration time.Duration `json:"duration"`
	// SkillsUsed are the skills the agent used (a skill's scenario,
	// spec_skills_013 SK-111).
	SkillsUsed []string `json:"skills_used,omitempty"`
}

// SkillEvalPrefix names a skill's scenarios as an eval: RunEval,
// EvalRuns and blitz eval take "skill:<name>" (spec_skills_013 SK-111).
const SkillEvalPrefix = "skill:"

// SkillTry is a prompt tried once with a skill (SK-112): what the agent
// answered, the skills it used, and whether this one was among them.
type SkillTry struct {
	Skill, Agent, Prompt string
	Output               string
	SkillsUsed           []string
	// Triggered: the agent used the skill; Forced: it was given it.
	Triggered, Forced bool
	Refusals          []string
	CostUSD           float64
	Duration          time.Duration
}

// EvalRun is a run of an eval: every case (or those asked for) by every
// agent it names.
type EvalRun struct {
	ID        string        `json:"id"`
	Workspace string        `json:"workspace"`
	Eval      string        `json:"eval"`
	Started   time.Time     `json:"started"`
	Duration  time.Duration `json:"duration"`
	// Status is EvalPassed (no fail-scope case failed or errored),
	// EvalFailed, EvalStopped or, while it runs, EvalRunning.
	Status  string       `json:"status"`
	Results []EvalResult `json:"results"`
	CostUSD float64      `json:"cost_usd,omitempty"`
	// Error is why the run couldn't start or finish.
	Error string `json:"error,omitempty"`
}

// Counts are the run's results by status.
func (r EvalRun) Counts() map[string]int {
	out := map[string]int{}
	for _, x := range r.Results {
		out[x.Status]++
	}
	return out
}

// EvalOptions are how an eval runs.
type EvalOptions struct {
	// Cases are the case IDs to run (none: every case).
	Cases []string
	// OnStart receives the run as it starts; OnResult each result as it's
	// scored.
	OnStart  func(EvalRun)
	OnResult func(EvalResult)
}
