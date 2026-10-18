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

package gates

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func TestParse(t *testing.T) {
	tests := []struct {
		name, in string
		want     Rule
	}{
		{"an instruction rule", "Use tabs.\n", Rule{Name: "r", Path: "p", Text: "Use tabs."}},
		{"path-scoped", "---\npaths: \"*.go\"\n---\nGo style.", Rule{Name: "r", Path: "p", Paths: []string{"*.go"}, Text: "Go style."}},
		{"a gate, warn by default", "---\nwhen: after_agent\norder: 2\nagents: [qa]\n---\nTests pass.", Rule{Name: "r", Path: "p", When: AfterAgent, Order: 2, Agents: []string{"qa"}, Action: Warn, Text: "Tests pass."}},
		{"an unknown when", "---\nwhen: later\n---\nx", Rule{Name: "r", Path: "p", When: "later", Text: "x", Problem: `when "later": before_agent or after_agent`}},
		{"an unknown action", "---\nwhen: before_agent\naction: shout\n---\nx", Rule{Name: "r", Path: "p", When: BeforeAgent, Action: "shout", Text: "x", Problem: `action "shout": block, warn or revise`}},
		{"an empty gate", "---\nwhen: before_agent\naction: block\n---\n", Rule{Name: "r", Path: "p", When: BeforeAgent, Action: Block, Problem: "the rule is empty: say what to check"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Parse("r", "p", []byte(tt.in)))
		})
	}
	assert.Contains(t, Parse("r", "p", []byte("---\n[bad\n---\nx")).Problem, "front matter")
}

// Load orders gates before, then after, by order and name; then
// instruction rules; an earlier folder's name wins; Gates picks a stage's
// for an agent, without broken ones.
func TestLoadAndGates(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write(t, a, "b-check.md", "---\nwhen: before_agent\norder: 20\n---\nB")
	write(t, a, "a-check.md", "---\nwhen: before_agent\norder: 20\n---\nA")
	write(t, a, "first.md", "---\nwhen: before_agent\norder: 10\nagents: [qa]\n---\nF")
	write(t, a, "after.md", "---\nwhen: after_agent\naction: revise\n---\nAfter")
	write(t, a, "broken.md", "---\nwhen: before_agent\naction: nope\n---\nX")
	write(t, a, "style.md", "Tabs.")
	write(t, a, "notes.txt", "not a rule")
	write(t, b, "style.md", "Spaces.")
	write(t, b, "sub/deep.md", "Deep.")
	rules := Load(a, b, filepath.Join(a, "missing"))
	var names []string
	for _, r := range rules {
		names = append(names, r.Name)
	}
	assert.Equal(t, []string{"broken", "first", "a-check", "b-check", "after", "style", "sub/deep"}, names)
	assert.Equal(t, "Tabs.", rules[5].Text, "the first folder's")

	names = nil
	for _, r := range Gates(rules, BeforeAgent, "blitz") {
		names = append(names, r.Name)
	}
	assert.Equal(t, []string{"a-check", "b-check"}, names, "first is qa's; broken is skipped")
	assert.Len(t, Gates(rules, BeforeAgent, "qa"), 3)
	assert.Len(t, Gates(rules, AfterAgent, ""), 1)
	assert.True(t, rules[1].IsGate())
	assert.False(t, rules[5].IsGate())
}

func TestJudge(t *testing.T) {
	rule := Rule{Name: "tests", When: AfterAgent, Text: "The answer says how it was tested."}
	var got string
	ask := func(reply string, err error) Asker {
		return func(_ context.Context, instruction, prompt string) (string, error) {
			got = prompt
			return reply, err
		}
	}
	v, err := Judge(context.Background(), ask("```json\n{\"verdict\": \"fail\", \"reason\": \"No tests named.\"}\n```", nil), rule, "Done.")
	require.NoError(t, err)
	assert.Equal(t, Verdict{Reason: "No tests named."}, v)
	assert.Contains(t, got, "The agent's answer, and the files it changed:\nDone.")
	assert.Contains(t, got, "The rule (tests):\nThe answer says how it was tested.")

	rule.When = BeforeAgent
	v, err = Judge(context.Background(), ask(`{"verdict":"PASS","reason":"fine"}`, nil), rule, "")
	require.NoError(t, err)
	assert.True(t, v.Pass)
	assert.Contains(t, got, "The user's prompt to the agent:\n(nothing)")

	for _, reply := range []string{"no json", `{"verdict": "maybe"}`, `{"verdict": `} {
		_, err = Judge(context.Background(), ask(reply, nil), rule, "x")
		assert.Error(t, err, reply)
	}
	_, err = Judge(context.Background(), ask("", errors.New("down")), rule, "x")
	assert.EqualError(t, err, "down")
}

// Save writes a gate's or an instruction rule's front matter, keeps
// other fields, renames, refuses a taken name and bad values; Reorder
// numbers the gates in the order given; Delete removes one.
func TestSaveReorderDelete(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "tests.md", "---\nowner: qa\nwhen: after_agent\naction: warn\n---\nOld.")
	p, probs, err := Save(dir, Spec{Name: "tested", When: AfterAgent, Action: Revise, Order: 5, Agents: []string{"qa"}, Description: "Tested answers", Text: "Say how it was tested."}, "tests")
	require.NoError(t, err)
	require.Empty(t, probs)
	data, _ := os.ReadFile(p)
	assert.Equal(t, "---\nowner: qa\nwhen: after_agent\naction: revise\ndescription: Tested answers\norder: 5\nagents: [qa]\n---\n\nSay how it was tested.\n", string(data))
	assert.NoFileExists(t, filepath.Join(dir, "tests.md"), "renamed")

	p, probs, err = Save(dir, Spec{Name: "style", Paths: []string{"*.go"}, Text: "Go style.", Action: Block, Order: 3}, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	data, _ = os.ReadFile(p)
	assert.Equal(t, "---\npaths: ['*.go']\n---\n\nGo style.\n", string(data), "an instruction rule keeps no gate fields")

	_, probs, err = Save(dir, Spec{Name: "style", When: BeforeAgent, Action: Warn, Text: "x"}, "tested")
	require.NoError(t, err)
	assert.Len(t, probs, 1, "taken")
	_, probs, _ = Save(dir, Spec{Name: "Bad Name", When: "soon", Action: "shout"}, "")
	assert.Len(t, probs, 4)
	_, _, err = Save(dir, Spec{Name: "x", Text: "x"}, "missing")
	assert.ErrorIs(t, err, ErrUnknown)

	write(t, dir, "a.md", "---\nwhen: before_agent\norder: 99\n---\nA")
	write(t, dir, "b.md", "---\nwhen: before_agent\n---\nB")
	require.NoError(t, Reorder(dir, []string{"b", "a"}))
	rules := Load(dir)
	assert.Equal(t, "b", rules[0].Name)
	assert.Equal(t, 10, rules[0].Order)
	assert.Equal(t, 20, rules[1].Order)
	assert.ErrorIs(t, Reorder(dir, []string{"missing"}), ErrUnknown)
	assert.ErrorIs(t, Reorder(dir, []string{"../x"}), ErrUnknown)

	require.NoError(t, Delete(dir, "a"))
	assert.ErrorIs(t, Delete(dir, "a"), ErrUnknown)
	assert.ErrorIs(t, Delete(dir, "../x"), ErrUnknown)
}
