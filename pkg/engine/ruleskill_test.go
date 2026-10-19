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
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each verdict of a gate, before and after the agent: what it says and
// what it hands on.
func TestRuleVerdicts(t *testing.T) {
	tests := []struct {
		name, rule, when string
		verdicts         []string
		notice           string
	}{
		{"before, passes", "---\nwhen: before_agent\naction: block\n---\nPolite.", "before", []string{"pass: fine"}, ""},
		{"after, unjudged", "---\nwhen: after_agent\naction: warn\n---\nTested.", "after", nil, "couldn't"},
		{"after, warns", "---\nwhen: after_agent\naction: warn\n---\nTested.", "after", []string{"fail: no tests"}, "no tests"},
		{"after, passes", "---\nwhen: after_agent\naction: block\n---\nTested.", "after", []string{"pass: fine"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := openTest(t)
			writeRule(t, w, "check", tt.rule)
			withJudge(t, w, tt.verdicts...)
			var notices []string
			notice := func(s string, _ bool) { notices = append(notices, s) }
			if tt.when == "before" {
				guidance, err := w.beforeRules(context.Background(), "hi", notice)
				require.NoError(t, err)
				assert.Empty(t, guidance)
			} else {
				assert.Empty(t, w.afterRules(context.Background(), "s", "Done.", notice))
			}
			if tt.notice == "" {
				assert.Empty(t, notices)
				return
			}
			require.Len(t, notices, 1)
			assert.Contains(t, notices[0], tt.notice)
		})
	}
	t.Run("memory off", func(t *testing.T) {
		w, _ := openTestWith(t, func(c *config.Config) { c.Memory.Enabled = false })
		writeRule(t, w, "check", "---\nwhen: before_agent\n---\nPolite.")
		assert.Nil(t, w.Rules())
	})
}

// A workspace skill's files, copies that can't be made, and a test run
// with skills off.
func TestSkillFileEdges(t *testing.T) {
	w := openTest(t)
	_, probs, err := w.SaveSkill(api.SkillSpec{Name: "notes", Description: "Notes", Instructions: "Take notes."}, "", "")
	require.NoError(t, err)
	require.Empty(t, probs)
	f, err := w.WriteSkillFile("notes", "references/a.md", []byte("a"))
	require.NoError(t, err)
	assert.True(t, f.Editable)
	_, err = w.DeleteSkillFile("notes", "references/missing.md")
	assert.Error(t, err)
	f, err = w.DeleteSkillFile("notes", "references/a.md")
	require.NoError(t, err)
	assert.Equal(t, "notes", f.Spec.Name)

	_, probs, err = w.CopySkill("notes", "Bad Name")
	require.NoError(t, err)
	assert.NotEmpty(t, probs, "a bad name")
	require.NoError(t, os.MkdirAll(filepath.Join(w.ProjectSkillsDir(), "stray"), 0o755))
	_, probs, err = w.CopySkill("notes", "stray")
	require.NoError(t, err)
	assert.NotEmpty(t, probs, "a folder by that name, not a skill")

	// .agents/skills as a file: nothing can be made in it.
	w2 := openTest(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(w2.ProjectSkillsDir()), 0o755))
	require.NoError(t, os.WriteFile(w2.ProjectSkillsDir(), nil, 0o644))
	_, _, err = w2.SaveSkill(api.SkillSpec{Name: "notes", Description: "Notes"}, "", "")
	assert.Error(t, err)

	w3, _ := openTestWith(t, func(c *config.Config) { c.Skills.Enabled = false })
	assert.Contains(t, w3.RunSkillScript(context.Background(), "x", "y", nil).Error, "skills are off")
}
