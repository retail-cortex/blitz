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
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// \skill-name in a prompt sends that skill's instructions with it, once
// each, with its references and a meta skill's members; a name that
// isn't a skill, a word that only contains one, and a skill offered to
// other agents send nothing (SK-104).
func TestSkillMentions(t *testing.T) {
	w := openTest(t)
	save := func(spec api.SkillSpec) {
		_, probs, err := w.SaveSkill(spec, "", "")
		require.NoError(t, err)
		require.Empty(t, probs)
	}
	save(api.SkillSpec{Name: "release-notes", Description: "Notes", Instructions: "Read the log."})
	save(api.SkillSpec{Name: "combo", Description: "Both", Members: []string{"release-notes"}, Instructions: "Combine."})
	save(api.SkillSpec{Name: "staff", Description: "Staff", Agents: []string{"reviewer"}, Instructions: "Staff only."})
	_, err := w.WriteSkillFile("release-notes", "references/style.md", []byte("Short."))
	require.NoError(t, err)

	tests := []struct {
		name, text string
		want       []string
		none       bool
	}{
		{"one", `Write them \release-notes please`, []string{`skill "release-notes"`, "Read the log.", "references/style.md"}, false},
		{"once, at the start", `\release-notes then \release-notes`, []string{"Read the log."}, false},
		{"a meta skill", `use (\combo)`, []string{"Combine.", "activate each with activate_skill: release-notes"}, false},
		{"not a skill", `\nope`, nil, true},
		{"inside a word", `C:\release-notes`, nil, true},
		{"offered to another agent", `\staff`, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			note := w.skillMentionNote(tt.text)
			if tt.none {
				assert.Empty(t, note)
				return
			}
			for _, s := range tt.want {
				assert.Contains(t, note, s)
			}
			assert.Equal(t, 1, strings.Count(note, "The user asked for the skill"), "once each")
		})
	}

	// A turn sends it; the transcript keeps the prompt as typed.
	s := newSession(t, w)
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: `Draft \release-notes`}, func(api.Event) {})
	require.NoError(t, err)
	assert.Equal(t, "user: Draft \\release-notes", transcript(t, w)[0])
}

// A workspace's own skill is editable; a built-in isn't, and is copied
// in to be changed; deleting removes only the workspace's.
func TestWorkspaceSkillFiles(t *testing.T) {
	w := openTest(t)
	builtin := ""
	for _, s := range w.ListSkills() {
		if !s.Project {
			builtin = s.Name
			break
		}
	}
	require.NotEmpty(t, builtin)
	f, err := w.GetSkillFile(builtin)
	require.NoError(t, err)
	assert.False(t, f.Editable)
	assert.Empty(t, f.Hash)
	_, _, err = w.SaveSkill(api.SkillSpec{Name: builtin, Description: "d"}, builtin, "")
	assert.ErrorIs(t, err, skills.ErrUnknownSkill, "a built-in isn't the workspace's")
	_, probs, err := w.SaveSkill(api.SkillSpec{Name: builtin, Description: "d"}, "", "")
	require.NoError(t, err)
	assert.NotEmpty(t, probs, "a built-in's name is taken")

	copied, probs, err := w.CopySkill(builtin, "mine")
	require.NoError(t, err)
	require.Empty(t, probs)
	assert.True(t, copied.Editable)
	assert.NotEmpty(t, copied.Hash)
	_, _, err = w.CopySkill("nope", "x")
	assert.ErrorIs(t, err, skills.ErrUnknownSkill)
	data, err := w.ReadSkillFile("mine", "SKILL.md")
	require.NoError(t, err)
	assert.Contains(t, string(data), "name: mine")

	assert.Error(t, w.DeleteSkill(builtin))
	require.NoError(t, w.DeleteSkill("mine"))
	_, err = w.GetSkillFile("mine")
	assert.ErrorIs(t, err, skills.ErrUnknownSkill)
	_, err = w.ReadSkillFile("mine", "SKILL.md")
	assert.Error(t, err)
	_, err = w.WriteSkillFile("mine", "references/a.md", nil)
	assert.Error(t, err)
	_, err = w.DeleteSkillFile("mine", "references/a.md")
	assert.Error(t, err)
	run := w.RunSkillScript(context.Background(), "nope", "x", nil)
	assert.NotEmpty(t, run.Error)
}
