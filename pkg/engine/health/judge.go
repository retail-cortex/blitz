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

package health

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// RubricVersion changes when the rubrics do: every item is judged again.
const RubricVersion = 1

// Rubrics are what the judge scores, per kind (HL-10).
var Rubrics = map[string][]string{
	KindAgent: {
		"Its instructions are clear and complete: what it's for, how it works, when it's done",
		"Its tools fit what it's for, and no more",
		"Its evals cover what it does",
		"Its runs succeed",
	},
	KindSkill: {
		"Its description says when to use it (it's what makes an agent choose it)",
		"Its instructions can be followed step by step",
		"Its scripts run and pass the policy (or it needs none)",
		"Something uses it",
	},
	KindRule: {
		"It can be judged pass or fail from a prompt or an answer alone",
		"It isn't redundant with another rule or guardrail",
		"It fires when it should, and only then",
	},
	KindGuardrail: {
		"What it checks is clear and can be decided",
		"Its action fits the risk (block, redact or warn)",
		"It fires when it should, and only then",
	},
	KindEval: {
		"Its cases cover the agent's job, the hard cases too",
		"Its expected outputs and thresholds are sensible",
		"It tells good from bad (not always passing or always failing)",
	},
	KindWorkflow: {
		"Its steps are well defined and in a sensible order",
		"It handles failure (retries, error links, timeouts)",
		"Its cost and duration fit what it does",
		"Its runs succeed",
		"Its permission mode fits what it does",
	},
	KindWorker: {
		"Its permissions are as tight as its work allows",
		"Its schedule and limits fit its work",
		"Its runs succeed",
	},
}

// Instruction tells the judge its job.
const Instruction = `You judge one item of an AI agent workspace (an agent, skill, rule, guardrail, eval, workflow or worker) for the user who maintains it.
Score each rubric line from 1 (poor) to 5 (excellent) on the evidence given: the item's file and what its records show. Don't reward length; reward what makes the item work. Without evidence for a line, judge the file alone and say so in the reason.
Then give an overall score (1 to 5), a one-sentence summary, and up to three fixes: each a concrete change to the item, written so it can be applied (not "improve the instructions" but what to write).
Answer with JSON only: {"scores": [{"line": "<the rubric line>", "score": 1-5, "reason": "one sentence"}], "overall": 1-5, "summary": "...", "fixes": ["..."]}`

// maxFile bounds how much of an item's file the judge sees.
const maxFile = 24 << 10

// Prompt is what the judge gets for it: the rubric, the item's file and
// its evidence (what the facts say about it).
func Prompt(it Item, file string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Item: the %s %q", it.Kind, it.Name)
	if it.Detail != "" {
		fmt.Fprintf(&b, " (%s)", it.Detail)
	}
	b.WriteString("\n\nRubric:\n")
	for _, line := range Rubrics[it.Kind] {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	b.WriteString("\nEvidence from the last 30 days:\n")
	b.WriteString(Evidence(it))
	if len(file) > maxFile {
		file = file[:maxFile] + "\n[…cut]"
	}
	fmt.Fprintf(&b, "\nThe file (%s):\n```\n%s\n```\n", it.Path, file)
	return b.String()
}

// Evidence is what the facts say about it, in words.
func Evidence(it Item) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, "- "+format+"\n", args...) }
	for _, f := range it.Flags {
		switch f {
		case FlagUncovered:
			line("no eval checks it")
		case FlagStale:
			line("it changed since its evals last ran")
		case FlagUnused:
			line("nothing used it")
		case FlagNeverRun:
			line("it has never run")
		case FlagFailing:
			line("most of its runs failed")
		case FlagDisabled:
			line("it's disabled")
		case FlagOff:
			line("it's switched off")
		}
	}
	if r := it.Runs; r != nil && r.Runs > 0 {
		line("%d runs: %d done, %d failed, %d stopped; $%.4f in all; median %s", r.Runs, r.Done, r.Failed, r.Stopped, r.CostUSD, r.Median.Round(time.Second))
		if r.FailingStep != "" {
			line("runs fail most at the step %q", r.FailingStep)
		}
	}
	if e := it.Evals; e != nil && e.Cases > 0 {
		line("its latest eval results: %d of %d cases passed (%s)", e.Passed, e.Cases, e.Last.Format(time.DateOnly))
	}
	switch it.Kind {
	case KindSkill:
		line("activated %d times", it.Uses)
	case KindRule, KindGuardrail:
		line("decided %d times; stopped, warned or revised %d of them", it.Uses, it.Fired)
	}
	if b.Len() == 0 {
		return "- nothing recorded\n"
	}
	return b.String()
}

// ErrNoVerdict means the judge's answer isn't one.
var ErrNoVerdict = errors.New("the judge's answer isn't a verdict")

// Parse reads the judge's answer (in a code fence or not) into a verdict
// for it, its scores clamped to 1–5.
func Parse(it Item, reply string) (Verdict, error) {
	s := strings.TrimSpace(reply)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	var a struct {
		Scores []struct {
			Line   string  `json:"line"`
			Score  float64 `json:"score"`
			Reason string  `json:"reason"`
		} `json:"scores"`
		Overall float64  `json:"overall"`
		Summary string   `json:"summary"`
		Fixes   []string `json:"fixes"`
	}
	if err := json.Unmarshal([]byte(s), &a); err != nil {
		return Verdict{}, fmt.Errorf("%w: %v", ErrNoVerdict, err)
	}
	if len(a.Scores) == 0 && a.Overall == 0 {
		return Verdict{}, fmt.Errorf("%w: no scores", ErrNoVerdict)
	}
	v := Verdict{Kind: it.Kind, Name: it.Name, Hash: it.Hash, Rubric: RubricVersion, Summary: strings.TrimSpace(a.Summary)}
	sum := 0
	for _, x := range a.Scores {
		sc := Score{Line: x.Line, Score: int(math.Round(clamp(x.Score))), Reason: strings.TrimSpace(x.Reason)}
		v.Scores = append(v.Scores, sc)
		sum += sc.Score
	}
	v.Overall = clamp(a.Overall)
	if a.Overall == 0 {
		v.Overall = float64(sum) / float64(len(v.Scores))
	}
	for _, f := range a.Fixes {
		if f = strings.TrimSpace(f); f != "" && len(v.Fixes) < 3 {
			v.Fixes = append(v.Fixes, f)
		}
	}
	return v, nil
}

func clamp(x float64) float64 { return math.Max(1, math.Min(5, x)) }
