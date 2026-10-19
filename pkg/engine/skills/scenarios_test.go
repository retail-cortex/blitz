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

package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Scenarios are read in Castor's format: executes true unless it says
// otherwise, skills_applied as expected_skills, a threshold of 0.8 unless
// given, the body the outcome; one that can't run says why.
func TestLoadScenarios(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "notes", "---\nname: notes\ndescription: d\n---\n", map[string]string{
		"scenarios/since-tag.md": "---\nprompt: Write the notes for v2\nexpected_skills: [notes]\nthreshold: 0.75\n---\n## What's new\n",
		"scenarios/not-bugs.md":  "---\ndescription: Not for bugs\nprompt: Why does it crash?\nexecutes: false\nskills_applied: [notes]\n---\nLooks at the logs.\n",
		"scenarios/no-prompt.md": "---\nthreshold: 2\n---\n",
		"scenarios/plain.md":     "no front matter",
		"scenarios/notes.txt":    "not a scenario",
		"scenarios/Bad Name.md":  "---\nprompt: p\n---\no\n",
	})
	list := LoadScenarios(filepath.Join(dir, "notes", ScenariosDir))
	byName := map[string]Scenario{}
	for _, sc := range list {
		byName[sc.Name] = sc
	}
	require.Len(t, list, 5, "the .md files, by name")
	assert.Equal(t, "Bad Name", list[0].Name)

	tag := byName["since-tag"]
	assert.Empty(t, tag.Problem)
	assert.True(t, tag.Executes, "executes unless it says otherwise")
	assert.Equal(t, []string{"notes"}, tag.ExpectedSkills)
	assert.InDelta(t, 0.75, tag.Threshold, 1e-9)
	assert.Equal(t, "## What's new", tag.Outcome)

	bugs := byName["not-bugs"]
	assert.False(t, bugs.Executes)
	assert.Equal(t, []string{"notes"}, bugs.ExpectedSkills, "skills_applied, Castor's alias")
	assert.InDelta(t, DefaultScenarioThreshold, bugs.Threshold, 1e-9)
	assert.Equal(t, "Not for bugs", bugs.Description)

	for name, want := range map[string]string{"no-prompt": "prompt", "plain": "no front matter", "Bad Name": "name"} {
		assert.Contains(t, byName[name].Problem, want, name)
	}
	assert.Contains(t, byName["no-prompt"].Problem, "threshold")
	assert.Nil(t, LoadScenarios(filepath.Join(dir, "none")))
	assert.Nil(t, Scenarios(nil))
	assert.Nil(t, Scenarios(&Skill{}), "a built-in skill's files aren't on disk")
}

// A scenario is saved in Castor's format, keeping fields it doesn't know,
// renamed, refused with its problems, and deleted.
func TestSaveScenario(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "notes", "---\nname: notes\ndescription: d\n---\n", nil)
	sc := Scenario{Name: "since-tag", Prompt: "Write the notes", Executes: true, ExpectedSkills: []string{"notes"}, Threshold: 0.8, Outcome: "## What's new"}
	p, probs, err := SaveScenario(dir, "notes", sc, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	assert.Equal(t, filepath.Join(dir, "notes", "scenarios", "since-tag.md"), p)
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "---\nprompt: Write the notes\nexecutes: true\nexpected_skills: [notes]\nthreshold: 0.8\n---\n\n## What's new\n", string(data))

	// A field Castor added and Blitz doesn't edit stays; executes false is written.
	require.NoError(t, os.WriteFile(p, []byte("---\nprompt: Write the notes\nexecutes: true\nskills_applied: [notes]\ncustom: kept\n---\nOld.\n"), 0o644))
	sc.Executes, sc.ExpectedSkills, sc.Name = false, nil, "not-notes"
	p, probs, err = SaveScenario(dir, "notes", sc, "since-tag")
	require.NoError(t, err)
	require.Empty(t, probs)
	assert.NoFileExists(t, filepath.Join(dir, "notes", "scenarios", "since-tag.md"), "renamed")
	data, _ = os.ReadFile(p)
	assert.Contains(t, string(data), "executes: false")
	assert.Contains(t, string(data), "custom: kept")
	assert.NotContains(t, string(data), "skills_applied", "expected_skills says it")
	got := LoadScenarios(filepath.Dir(p))
	require.Len(t, got, 1)
	assert.False(t, got[0].Executes)

	_, probs, err = SaveScenario(dir, "notes", Scenario{Name: "x", Threshold: 1.5}, "")
	require.NoError(t, err)
	assert.Len(t, probs, 3, "no prompt, a threshold over 1, no outcome: %v", probs)
	_, probs, err = SaveScenario(dir, "notes", Scenario{Name: "since-other", Prompt: "p", Outcome: "o", ExpectedSkills: []string{"Not A Skill"}}, "")
	require.NoError(t, err)
	assert.Len(t, probs, 1)
	_, probs, err = SaveScenario(dir, "notes", Scenario{Name: "not-notes", Prompt: "p", Outcome: "o"}, "")
	require.NoError(t, err)
	assert.Equal(t, []string{`name "not-notes": the skill has a scenario by that name`}, probs)
	_, _, err = SaveScenario(dir, "notes", Scenario{Name: "y", Prompt: "p", Outcome: "o"}, "gone")
	assert.ErrorIs(t, err, ErrUnknownScenario)
	_, _, err = SaveScenario(dir, "../x", Scenario{Name: "y", Prompt: "p", Outcome: "o"}, "")
	assert.ErrorIs(t, err, ErrUnknownSkill)

	require.NoError(t, DeleteScenario(dir, "notes", "not-notes"))
	assert.ErrorIs(t, DeleteScenario(dir, "notes", "not-notes"), ErrUnknownScenario)
	assert.ErrorIs(t, DeleteScenario(dir, "notes", "../SKILL"), ErrUnknownScenario)
}

// Trigger phrases are saved as trigger_phrases, tidied, within limits;
// the listing gives them as examples.
func TestTriggers(t *testing.T) {
	assert.Equal(t, []string{"write the notes", "ship it"}, CleanTriggers([]string{"  write   the notes ", "", "ship it", "write the notes"}))
	assert.Equal(t, "", TriggerHint(nil))
	assert.Equal(t, ` For requests like: "write the notes"; "ship it".`, TriggerHint([]string{"write the notes", " ship it "}))

	data, err := Render(nil, Spec{Name: "notes", Description: "d", Triggers: []string{" write the notes ", "write the notes"}})
	require.NoError(t, err)
	assert.Contains(t, string(data), "trigger_phrases: [write the notes]")
	s, err := ParseSkillMD(data, "SKILL.md")
	require.NoError(t, err)
	assert.Equal(t, []string{"write the notes"}, s.TriggerPhrases)

	many := make([]string, MaxTriggers+1)
	for i := range many {
		many[i] = strings.Repeat("x", i+1)
	}
	assert.Len(t, Check(Spec{Name: "n", Description: "d", Triggers: many}, nil), 1, "too many")
	assert.Len(t, Check(Spec{Name: "n", Description: "d", Triggers: []string{strings.Repeat("long ", 50)}}, nil), 1, "too long")
}

// A request matches a trigger phrase by most of its words, not its
// wording: no two people write them alike.
func TestTriggerMatches(t *testing.T) {
	for _, tc := range []struct {
		request, phrase string
		want            bool
	}{
		{"write the release notes", "write the release notes", true},
		{"can you draft release notes for 2.4?", "write the release notes", true},
		{"what's in this release? notes please", "release notes", true},
		{"scaffolding a Go microservice", "scaffold Go microservice", true},
		{"notes", "release notes", true},
		{"fix the checkout bug", "write the release notes", false},
		{"release the hounds", "write the release notes", false},
		{"turn this pdf into a csv", "pdf to csv", true},
		{"a csv of the sales", "pdf to csv", false},
		{"summarize this pdf", "pdf summary", true},
		{"how is it going", "how is it", true},
		{"control the contract", "contract", true},
		{"control panel", "contract panel", false},
		{"", "release notes", false},
		{"anything", "", false},
	} {
		t.Run(tc.request+" ~ "+tc.phrase, func(t *testing.T) {
			assert.Equal(t, tc.want, TriggerMatches(tc.request, tc.phrase))
		})
	}
	s := &Skill{SkillMetadata: SkillMetadata{Name: "notes", Description: "d", TriggerPhrases: []string{"write the release notes"}}}
	assert.True(t, s.Matches("draft release notes"), "search finds it by a trigger phrase")
	assert.False(t, s.Matches("checkout"))
}
