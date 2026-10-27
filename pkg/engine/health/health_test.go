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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)

func ago(d time.Duration) time.Time { return now.Add(-d) }

func item(t *testing.T, f Facts, key string) Item {
	t.Helper()
	for _, it := range f.Items {
		if it.Key() == key {
			return it
		}
	}
	t.Fatalf("no item %s", key)
	return Item{}
}

// The facts: each kind's items with their flags, runs, eval coverage and
// signals, and the runs per day.
func TestBuild(t *testing.T) {
	day := 24 * time.Hour
	in := Inputs{
		Now:          now,
		DefaultAgent: "blitz",
		Agents: []Source{
			{Name: "writer", Modified: ago(10 * day)},
			{Name: "editor", Modified: ago(day)},
			{Name: "lonely"},
			{Name: "broken", Problem: "bad frontmatter"},
		},
		Skills: []Source{{Name: "pdf", Detail: "scripts"}, {Name: "used-by-flow"}, {Name: "idle"}},
		Rules:  []Source{{Name: "short", Agents: []string{"writer"}}, {Name: "ghost", Agents: []string{"nobody"}}},
		Guardrails: []Source{
			{Name: "secrets", Detail: "secrets"},
			{Name: "limits", Off: true},
		},
		Evals: []Source{
			{Name: "writing", Agents: []string{"writer", "editor"}, Cases: 2, EvalRuns: []EvalRun{
				{Started: ago(20 * day), Results: []EvalResult{{Agent: "writer", Passed: false}}},
				{Started: ago(5 * day), Results: []EvalResult{{Agent: "writer", Passed: true}, {Agent: "writer", Passed: true}, {Agent: "editor", Passed: false}}},
			}},
			{Name: "fresh"},
		},
		Workflows: []Source{
			{Name: "brief", Skills: []string{"used-by-flow"}, Runs: []Run{
				{Started: ago(2 * day), Status: RunDone, CostUSD: 0.02, Duration: 10 * time.Second},
				{Started: ago(time.Hour), Status: RunFailed, CostUSD: 0.01, Duration: 4 * time.Second, FailedStep: "Save"},
				{Started: ago(2 * time.Hour), Status: RunFailed, FailedStep: "Save"},
				{Started: ago(40 * day), Status: RunDone},
			}},
			{Name: "new"},
		},
		Workers: []Source{
			{Name: "nightly", Detail: "brief", Runs: []Run{{Started: ago(time.Hour), Status: RunDone, CostUSD: 0.5}}},
			{Name: "sleepy"},
			{Name: "off", Off: true},
		},
		Audit: []audit.Entry{
			{Time: ago(day), Kind: audit.KindToolCall, Tool: "activate_skill", Args: map[string]any{"skill_name": "pdf"}},
			{Time: ago(time.Hour), Kind: audit.KindToolCall, Tool: "run_skill_script", Args: map[string]any{"skill": "pdf"}},
			{Time: ago(day), Kind: audit.KindRule, Tool: "rule:short", Decision: "pass"},
			{Time: ago(time.Hour), Kind: audit.KindRule, Tool: "rule:short", Decision: "revise", Detail: "too long"},
			{Time: ago(time.Hour), Kind: audit.KindGuardrail, Tool: "guardrail:secrets", Decision: "redact", Detail: "a key"},
			{Time: ago(time.Hour), Kind: audit.KindDenial, Tool: "shell", Decision: "auto-deny: pushes to main"},
			{Time: ago(time.Hour), Kind: audit.KindDenial, Tool: "shell", Decision: "rule-deny"},
		},
	}
	f := Build(in)
	assert.Len(t, f.Items, 4+3+2+2+2+2+3)
	assert.Equal(t, now, f.At)

	writer := item(t, f, "agent/writer")
	assert.Empty(t, writer.Flags, "covered, evals newer than the file")
	require.NotNil(t, writer.Evals)
	assert.Equal(t, 2, writer.Evals.Passed)
	assert.Equal(t, 2, writer.Evals.Cases, "the latest run's")
	assert.Equal(t, 2, writer.Evals.Runs)
	assert.Equal(t, []string{FlagStale}, item(t, f, "agent/editor").Flags, "changed after its evals ran")
	assert.Equal(t, []string{FlagUncovered}, item(t, f, "agent/lonely").Flags)
	assert.Equal(t, "bad frontmatter", item(t, f, "agent/broken").Problem)

	pdf := item(t, f, "skill/pdf")
	assert.Equal(t, 2, pdf.Uses)
	assert.Equal(t, 1, pdf.Daily[Days-1], "today's")
	assert.Empty(t, item(t, f, "skill/used-by-flow").Flags, "a workflow uses it")
	assert.Equal(t, []string{FlagUnused}, item(t, f, "skill/idle").Flags)

	short := item(t, f, "rule/short")
	assert.Equal(t, 2, short.Uses)
	assert.Equal(t, 1, short.Fired)
	assert.Equal(t, []string{FlagUnused}, item(t, f, "rule/ghost").Flags, "no such agent")
	assert.Equal(t, 1, item(t, f, "guardrail/secrets").Fired)
	assert.Equal(t, []string{FlagOff}, item(t, f, "guardrail/limits").Flags)

	writing := item(t, f, "eval/writing")
	assert.Equal(t, 2, writing.Evals.Passed)
	assert.Equal(t, 3, writing.Evals.Cases)
	assert.Equal(t, []string{FlagNeverRun}, item(t, f, "eval/fresh").Flags)

	brief := item(t, f, "workflow/brief")
	require.NotNil(t, brief.Runs)
	assert.Equal(t, 3, brief.Runs.Runs, "the one 40 days ago is out")
	assert.Equal(t, 1, brief.Runs.Done)
	assert.Equal(t, 2, brief.Runs.Failed)
	assert.InDelta(t, 0.03, brief.Runs.CostUSD, 1e-9)
	assert.Equal(t, 4*time.Second, brief.Runs.Median)
	assert.Equal(t, "Save", brief.Runs.FailingStep)
	assert.Equal(t, []string{FlagFailing}, brief.Flags)
	assert.InDelta(t, 1.0/3, brief.Runs.SuccessRate(), 1e-9)
	assert.Equal(t, []string{FlagNeverRun}, item(t, f, "workflow/new").Flags)

	assert.Empty(t, item(t, f, "worker/nightly").Flags)
	assert.Equal(t, []string{FlagUnused}, item(t, f, "worker/sleepy").Flags)
	assert.Equal(t, []string{FlagDisabled}, item(t, f, "worker/off").Flags)

	require.Len(t, f.Days, Days)
	assert.Equal(t, now.Format(time.DateOnly), f.Days[Days-1].Date)
	today := f.Days[Days-1]
	assert.Equal(t, 2, today.Failed, "the workflow's; the worker running it isn't counted twice")
	assert.Equal(t, 0, today.Done)
	assert.Equal(t, 1, f.Days[Days-3].Done)

	require.Len(t, f.Signals, 3, "revise, redact and the reviewer's denial; a rule's denial isn't the reviewer's")
	kinds := map[string]Signal{}
	for _, s := range f.Signals {
		kinds[s.Kind] = s
	}
	assert.Equal(t, "pushes to main", kinds["denial"].Detail)
	assert.Equal(t, "short", kinds[KindRule].About)
	assert.Equal(t, "a key", kinds[KindGuardrail].Detail)
}

func TestGradesAndScores(t *testing.T) {
	for score, want := range map[float64]string{0: "", 5: "A", 4.5: "A", 4.49: "B", 3.5: "B", 2.5: "C", 2.4: "D", 1: "D"} {
		assert.Equal(t, want, Grade(score), "%v", score)
	}
	items := []Item{
		{Kind: KindAgent, Name: "a", Uses: 3},
		{Kind: KindAgent, Name: "b"},
		{Kind: KindSkill, Name: "c", Problem: "broken"},
		{Kind: KindSkill, Name: "d"},
	}
	latest := map[string]Verdict{"agent/a": {Overall: 5}, "agent/b": {Overall: 1}}
	overall, by := Scores(items, latest)
	assert.InDelta(t, 4.0, by[KindAgent], 1e-9, "weighted by use: (5×3 + 1) / 4")
	assert.InDelta(t, 1.0, by[KindSkill], 1e-9, "a problem counts as a D; d isn't judged")
	assert.InDelta(t, (15+1+1)/5.0, overall, 1e-9)
	none, _ := Scores(items[3:], latest)
	assert.Zero(t, none, "nothing judged")
}

func TestDue(t *testing.T) {
	it := Item{Kind: KindAgent, Name: "a", Hash: "h1"}
	cur := map[string]Verdict{"agent/a": {Kind: KindAgent, Name: "a", Hash: "h1", Rubric: RubricVersion, Model: "m-001", Ref: "m"}}
	tests := []struct {
		name   string
		it     Item
		latest map[string]Verdict
		model  string
		force  bool
		want   bool
	}{
		{"current", it, cur, "m", false, false},
		{"forced", it, cur, "m", true, true},
		{"never judged", it, nil, "m", false, true},
		{"changed", Item{Kind: KindAgent, Name: "a", Hash: "h2"}, cur, "m", false, true},
		{"another model", it, cur, "other", false, true},
		{"a problem", Item{Kind: KindAgent, Name: "a", Problem: "x"}, nil, "m", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Due(tt.it, tt.latest, tt.model, tt.force))
		})
	}
	old := cur["agent/a"]
	old.Rubric = RubricVersion - 1
	assert.True(t, Due(it, map[string]Verdict{"agent/a": old}, "m", false), "an older rubric")
}

// Verdicts are kept and read back, the latest per item, history in order,
// those older than Keep let go; the checks' state is kept.
func TestStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "health", "ws")
	s := NewStore(dir)
	assert.Empty(t, s.All())
	assert.Zero(t, s.State())
	t0 := time.Now()
	require.NoError(t, s.Add(Verdict{Kind: KindAgent, Name: "a", Overall: 2, At: t0.Add(-Keep - time.Hour)}))
	require.NoError(t, s.Add(Verdict{Kind: KindAgent, Name: "a", Overall: 3, At: t0.Add(-time.Hour)}))
	require.NoError(t, s.Add(Verdict{Kind: KindAgent, Name: "a", Overall: 4, At: t0}))
	require.NoError(t, s.Add(Verdict{Kind: KindSkill, Name: "b", Overall: 5, At: t0}))
	assert.Len(t, s.All(), 3, "the one older than 90 days went")
	assert.Equal(t, 4.0, s.Latest()["agent/a"].Overall)
	h := s.History("agent/a")
	require.Len(t, h, 2)
	assert.Equal(t, 3.0, h[0].Overall)

	require.NoError(t, s.SetState(State{LastCheck: t0.Truncate(time.Second), LastCost: 0.25}))
	assert.Equal(t, 0.25, s.State().LastCost)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "verdicts.jsonl"), []byte("not json\n{\"kind\":\"agent\",\"name\":\"z\"}\n"), 0o600))
	assert.Len(t, s.All(), 1, "a bad line is skipped")
}

// The judge's prompt has the rubric, the evidence and the file; its
// answer is read in a code fence too, its scores clamped, fixes at most
// three, the overall the lines' mean when missing.
func TestPromptAndParse(t *testing.T) {
	it := Item{Kind: KindWorkflow, Name: "brief", Path: "/w/brief.flow.yaml", Hash: "h", Flags: []string{FlagFailing},
		Runs: &RunStats{Runs: 3, Done: 1, Failed: 2, CostUSD: 0.03, Median: 4 * time.Second, FailingStep: "Save"}}
	p := Prompt(it, "nodes: []")
	assert.Contains(t, p, `the workflow "brief"`)
	assert.Contains(t, p, Rubrics[KindWorkflow][1])
	assert.Contains(t, p, "most of its runs failed")
	assert.Contains(t, p, `fail most at the step "Save"`)
	assert.Contains(t, p, "nodes: []")
	long := Prompt(it, string(make([]byte, maxFile+10)))
	assert.Contains(t, long, "[…cut]")
	assert.Equal(t, "- nothing recorded\n", Evidence(Item{Kind: KindAgent}))
	assert.Contains(t, Evidence(Item{Kind: KindRule, Uses: 4, Fired: 1}), "decided 4 times")
	assert.Contains(t, Evidence(Item{Kind: KindGuardrail}), "nothing has come its way", "no decisions isn't a fault")

	v, err := Parse(it, "Here:\n```json\n{\"scores\": [{\"line\": \"a\", \"score\": 9, \"reason\": \" ok \"}, {\"line\": \"b\", \"score\": 0}], \"summary\": \"Fine.\", \"fixes\": [\"one\", \"\", \"two\", \"three\", \"four\"]}\n```")
	require.NoError(t, err)
	assert.Equal(t, 5, v.Scores[0].Score)
	assert.Equal(t, 1, v.Scores[1].Score)
	assert.Equal(t, "ok", v.Scores[0].Reason)
	assert.Equal(t, 3.0, v.Overall, "the lines' mean")
	assert.Equal(t, []string{"one", "two", "three"}, v.Fixes)
	assert.Equal(t, "h", v.Hash)
	assert.Equal(t, RubricVersion, v.Rubric)
	v, err = Parse(it, `{"scores": [], "overall": 4.2}`)
	require.NoError(t, err)
	assert.Equal(t, 4.2, v.Overall)

	for _, bad := range []string{"no", `{"scores": []}`, `{"scores": "x"}`} {
		_, err := Parse(it, bad)
		assert.ErrorIs(t, err, ErrNoVerdict, bad)
	}
}
