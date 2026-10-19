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
	"context"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
)

// saveSkills writes the workspace's skills for a test.
func saveSkills(t *testing.T, w *Workspace, specs ...api.SkillSpec) {
	t.Helper()
	for _, spec := range specs {
		_, probs, err := w.SaveSkill(spec, "", "")
		require.NoError(t, err)
		require.Empty(t, probs)
	}
}

// Trying a skill: the agent picks it up (and a meta skill's members with
// it) or doesn't, or is given it; the listing gives its trigger phrases
// as examples; nothing is saved.
func TestTrySkill(t *testing.T) {
	w, llm := openTestWith(t, configtest.RunTools,
		toolCall("activate_skill", map[string]any{"skill_name": "release-notes"}), text("## What's new"),
		text("Hello."),
		text("Given it."),
		toolCall("activate_skill", map[string]any{"skill_name": "ship"}), text("Shipped."),
	)
	saveSkills(t, w,
		api.SkillSpec{Name: "release-notes", Description: "Write release notes", Instructions: "Read the log.", Triggers: []string{"write the release notes", "what changed since the last release"}},
		api.SkillSpec{Name: "ship", Description: "Cut a release", Members: []string{"release-notes"}, Instructions: "Tag it."},
	)
	ctx := context.Background()

	used, err := w.TrySkill(ctx, "release-notes", "", "Draft the notes for v2", false)
	require.NoError(t, err)
	assert.True(t, used.Triggered)
	assert.False(t, used.Forced)
	assert.Equal(t, []string{"release-notes"}, used.SkillsUsed)
	assert.Equal(t, "## What's new", used.Output)
	assert.Equal(t, w.engine.ActiveAgent(), used.Agent)
	sys := llm.Requests[0].Config.SystemInstruction
	require.NotNil(t, sys)
	assert.Contains(t, sys.Parts[0].Text, `**release-notes**: Write release notes For requests like: "write the release notes"; "what changed since the last release".`)
	assert.Contains(t, sys.Parts[0].Text, "match a request to them by meaning")

	missed, err := w.TrySkill(ctx, "release-notes", "", "Say hello", false)
	require.NoError(t, err)
	assert.False(t, missed.Triggered)
	assert.Empty(t, missed.SkillsUsed)

	given, err := w.TrySkill(ctx, "release-notes", "", "Draft the notes", true)
	require.NoError(t, err)
	assert.True(t, given.Triggered)
	assert.True(t, given.Forced)
	last := llm.Requests[len(llm.Requests)-1].Contents
	assert.Contains(t, last[len(last)-1].Parts[0].Text, "Read the log.", "the skill goes with the prompt")

	meta, err := w.TrySkill(ctx, "release-notes", "", "Ship it", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"ship", "release-notes"}, meta.SkillsUsed, "a meta skill's members are used with it")
	assert.True(t, meta.Triggered)

	_, err = w.TrySkill(ctx, "nope", "", "x", false)
	assert.ErrorIs(t, err, skills.ErrUnknownSkill)
	_, err = w.TrySkill(ctx, "release-notes", "", "  ", false)
	assert.ErrorIs(t, err, ErrNoPrompt)
	_, err = w.TrySkill(ctx, "release-notes", "nobody", "x", false)
	assert.ErrorContains(t, err, `agent "nobody" isn't defined`)
}

// A skill's scenarios run as the eval "skill:<name>": each passes when
// the skills it expects were used (or, executes false, the skill wasn't)
// and the answer is near the outcome; the run is recorded.
func TestRunSkillScenarios(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools,
		toolCall("activate_skill", map[string]any{"skill_name": "release-notes"}), text("## What's new"),
		toolCall("activate_skill", map[string]any{"skill_name": "release-notes"}), text("Notes, again."),
		text("Hello."),
	)
	judge := runtime.NewMockLLM("judge", text(`{"score": 0.9, "reason": "Same notes."}`), text(`{"score": 0.9, "reason": "Fine."}`), text(`{"score": 0.4, "reason": "Off topic."}`))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return judge, nil }
	saveSkills(t, w, api.SkillSpec{Name: "release-notes", Description: "Write release notes", Instructions: "Read the log."})
	for _, sc := range []skills.Scenario{
		{Name: "a-uses-it", Prompt: "Write the notes", Executes: true, Threshold: 0.8, Outcome: "## What's new"},
		{Name: "b-not-for-bugs", Prompt: "Why does it crash?", Executes: false, Threshold: 0.5, Outcome: "Looks at the logs."},
		{Name: "c-misses-it", Prompt: "Notes for v2", Executes: true, ExpectedSkills: []string{"release-notes"}, Threshold: 0.8, Outcome: "## What's new"},
	} {
		_, probs, err := w.SaveSkillScenario("release-notes", sc, "")
		require.NoError(t, err)
		require.Empty(t, probs)
	}
	list, err := w.SkillScenarios("release-notes")
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, 3, w.ListSkills()[indexOfSkill(w, "release-notes")].Scenarios, "the count is listed")

	var streamed []string
	run, err := w.RunEval(context.Background(), "skill:release-notes", api.EvalOptions{OnResult: func(r api.EvalResult) { streamed = append(streamed, r.Case+":"+r.Status) }})
	require.NoError(t, err)
	assert.Equal(t, api.EvalFailed, run.Status)
	assert.Equal(t, "skill:release-notes", run.Eval)
	assert.Equal(t, []string{"a-uses-it:pass", "b-not-for-bugs:fail", "c-misses-it:fail"}, streamed)
	assert.Equal(t, []string{"release-notes"}, run.Results[0].SkillsUsed)
	assert.Contains(t, run.Results[1].Reason, "It used release-notes, which it shouldn't")
	assert.Contains(t, run.Results[2].Reason, "It didn't use release-notes.")
	assert.InDelta(t, 0.4, run.Results[2].Score, 1e-9)
	runs, err := w.EvalRuns("skill:release-notes", 0)
	require.NoError(t, err)
	require.Len(t, runs, 1, "recorded beside the evals")

	_, err = w.RunEval(context.Background(), "skill:nope", api.EvalOptions{})
	assert.ErrorIs(t, err, api.ErrUnknownEval)
	_, err = w.RunEval(context.Background(), "skill:release-notes", api.EvalOptions{Cases: []string{"zz"}})
	assert.ErrorIs(t, err, api.ErrEvalNotRunnable)

	require.NoError(t, w.DeleteSkillScenario("release-notes", "c-misses-it"))
	assert.ErrorIs(t, w.DeleteSkillScenario("release-notes", "c-misses-it"), skills.ErrUnknownScenario)
	_, _, err = w.SaveSkillScenario("code-review", skills.Scenario{Name: "x", Prompt: "p", Outcome: "o"}, "")
	assert.ErrorIs(t, err, skills.ErrUnknownSkill, "a built-in skill isn't the workspace's to change")
	_, err = w.SkillScenarios("nope")
	assert.ErrorIs(t, err, skills.ErrUnknownSkill)
}

// scenarioSkills says why the skills used don't fit a scenario.
func TestScenarioSkills(t *testing.T) {
	for _, tc := range []struct {
		name string
		sc   skills.Scenario
		used []string
		want string
	}{
		{"used, as expected", skills.Scenario{Executes: true}, []string{"notes"}, ""},
		{"not used", skills.Scenario{Executes: true}, nil, "It didn't use notes."},
		{"one of those expected missing", skills.Scenario{Executes: true, ExpectedSkills: []string{"notes", "pdf"}}, []string{"notes"}, "It didn't use pdf."},
		{"used when it shouldn't be", skills.Scenario{}, []string{"notes"}, "It used notes, which it shouldn't for this."},
		{"another used when it shouldn't be", skills.Scenario{}, []string{"pdf"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, scenarioSkills("notes", tc.sc, tc.used))
		})
	}
}

func indexOfSkill(w *Workspace, name string) int {
	for i, s := range w.ListSkills() {
		if s.Name == name {
			return i
		}
	}
	return -1
}
