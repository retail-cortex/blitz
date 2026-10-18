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

package evals

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sample = `---
description: Answers about the shop
agents: [blitz]
near: embedding
team: support
---
Cases from support tickets.

| ID | Name | Prompt | Output | Output type | Threshold | Scope |
|----|------|--------|--------|-------------|-----------|-------|
| c1 | Sum | What's 2+2? | 4 | Exact | | Fail |
| c2 | Pipes | Split a\|b<br>on the pipe | a and b | Near | 75% | Warn |
| c3 | | Hello | Hi there | | | |

Keep prompts short.
`

func TestParse(t *testing.T) {
	e := Parse("shop", "/x/shop.md", []byte(sample))
	require.Empty(t, e.Problem)
	assert.Equal(t, "Answers about the shop", e.Description)
	assert.Equal(t, []string{"blitz"}, e.Agents)
	assert.Equal(t, Embedding, e.Near)
	assert.Equal(t, "Cases from support tickets.", e.Intro)
	assert.Equal(t, "Keep prompts short.", e.Outro)
	require.Len(t, e.Cases, 3)
	assert.Equal(t, Case{ID: "c1", Name: "Sum", Prompt: "What's 2+2?", Output: "4", Type: Exact, Scope: Fail}, e.Cases[0])
	assert.Equal(t, Case{ID: "c2", Name: "Pipes", Prompt: "Split a|b\non the pipe", Output: "a and b", Type: Near, Threshold: 0.75, Scope: Warn}, e.Cases[1])
	assert.Equal(t, Case{ID: "c3", Prompt: "Hello", Output: "Hi there", Type: Near, Threshold: DefaultThreshold, Scope: Fail}, e.Cases[2], "the defaults")

	assert.Equal(t, Judge, Parse("x", "", []byte("| ID | Prompt | Output |\n|-|-|-|\n| a | b | c |\n")).Near, "judge by default")
}

func TestParseProblems(t *testing.T) {
	tests := []struct{ name, in, problem string }{
		{"no output column", "| ID | Prompt |\n|-|-|\n| a | b |\n", "no Output column"},
		{"bad threshold", "| ID | Prompt | Output | Threshold |\n|-|-|-|-|\n| a | b | c | lots |\n", `threshold "lots"`},
		{"bad front matter", "---\nagents: [\n---\n", "front matter"},
		{"bad near", "---\nnear: vibes\n---\n", `near "vibes"`},
		{"bad allow", "---\nallow: [run_command]\n---\n", `allow "run_command"`},
		{"case problems", "| ID | Prompt | Output | Type | Threshold | Scope |\n|-|-|-|-|-|-|\n| a | p | o | fuzzy | | |\n| a | | o | near | 2 | later |\n|  | p | o | exact | | |\n",
			`case a: output type "fuzzy"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, Parse("x", "", []byte(tt.in)).Problem, tt.problem)
		})
	}
	e := Parse("x", "", []byte("| ID | Prompt | Output | Type | Threshold | Scope |\n|-|-|-|-|-|-|\n| a | p | o | exact | | |\n| a | | o | near | 2 | later |\n|  | p | o | exact | | |\n"))
	for _, want := range []string{"another case has that ID", "it needs a prompt", "threshold 2", `scope "later"`, "case 3: it needs an ID"} {
		assert.Contains(t, e.Problem, want)
	}
	assert.Contains(t, Parse("Bad Name", "", nil).Problem, "name")
	assert.Empty(t, Parse("empty", "", []byte("Nothing yet.")).Problem, "an eval with no table has no cases")
}

func TestTableRoundTrips(t *testing.T) {
	cases := []Case{
		{ID: "c1", Name: "Sum", Prompt: "What's 2+2?", Output: "4", Type: Exact, Scope: Fail},
		{ID: "c2", Prompt: "Two\nlines | and a pipe", Output: "ok", Type: Near, Threshold: 0.9, Scope: Info},
	}
	table := Table(cases)
	assert.Contains(t, table, "| c2 |  | Two<br>lines \\| and a pipe | ok | Near | 0.9 | Info |")
	_, got, _, err := ParseTable(table)
	require.NoError(t, err)
	assert.Equal(t, cases, got)
}

func TestSave(t *testing.T) {
	dir := t.TempDir()
	e := Eval{Name: "shop", Description: "Shop answers", Agents: []string{"blitz"}, Allow: []string{AllowNetwork}, Intro: "From tickets.",
		Cases: []Case{{ID: "c1", Prompt: "2+2?", Output: "4", Type: Exact, Threshold: 0.5}, {ID: "c2", Prompt: "Hi", Output: "Hello"}}}
	p, probs, err := Save(dir, e, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	data, _ := os.ReadFile(p)
	assert.Equal(t, "---\ndescription: Shop answers\nagents: [blitz]\nallow: [network]\n---\n\nFrom tickets.\n\n"+
		"| ID | Name | Prompt | Output | Output type | Threshold | Scope |\n| --- | ---- | ------ | ------ | ----------- | --------- | ----- |\n"+
		"| c1 |  | 2+2? | 4 | Exact |  | Fail |\n| c2 |  | Hi | Hello | Near | 0.8 | Fail |\n", string(data))

	// Other front matter is kept; a rename moves the file.
	require.NoError(t, os.WriteFile(p, append([]byte("---\nteam: support\n"), data[4:]...), 0o644))
	e.Name, e.Near = "store", Embedding
	p2, probs, err := Save(dir, e, "shop")
	require.NoError(t, err)
	require.Empty(t, probs)
	assert.NoFileExists(t, p)
	data, _ = os.ReadFile(p2)
	assert.Contains(t, string(data), "team: support")
	assert.Contains(t, string(data), "near: embedding")
	assert.Len(t, Load(dir), 1)

	_, probs, _ = Save(dir, Eval{Name: "store", Cases: []Case{{ID: "a", Prompt: "p"}}}, "")
	assert.Equal(t, []string{`name "store": there's an eval by that name`}, probs)
	_, probs, _ = Save(dir, Eval{Name: "x", Cases: []Case{{Prompt: "p"}}}, "")
	assert.NotEmpty(t, probs)
	_, _, err = Save(dir, Eval{Name: "y"}, "gone")
	assert.ErrorIs(t, err, ErrUnknown)
	_, _, err = Save(dir, Eval{Name: "y"}, "../x")
	assert.ErrorIs(t, err, ErrUnknown)

	require.NoError(t, Delete(dir, "store"))
	assert.ErrorIs(t, Delete(dir, "store"), ErrUnknown)
	assert.ErrorIs(t, Delete(dir, "../x"), ErrUnknown)
	assert.Empty(t, Load(dir))
}

func TestExactMatch(t *testing.T) {
	assert.True(t, ExactMatch("4", " 4\n"))
	assert.True(t, ExactMatch("a\nb", "a  \r\nb"))
	assert.False(t, ExactMatch("4", "four"))
	assert.False(t, ExactMatch("a\nb", "a b"))
}

func TestCosine(t *testing.T) {
	assert.InDelta(t, 1, Cosine([]float32{1, 2}, []float32{2, 4}), 1e-9)
	assert.InDelta(t, 0, Cosine([]float32{1, 0}, []float32{0, 1}), 1e-9)
	assert.Zero(t, Cosine([]float32{1, 0}, []float32{-1, 0}), "opposite: 0")
	assert.Zero(t, Cosine([]float32{1}, []float32{1, 2}))
	assert.Zero(t, Cosine([]float32{0, 0}, []float32{1, 2}))
}

func TestJudgeNear(t *testing.T) {
	answer := func(reply string, err error) func(context.Context, string, string) (string, error) {
		return func(_ context.Context, instruction, prompt string) (string, error) {
			assert.Contains(t, instruction, "score")
			assert.Contains(t, prompt, "The output expected:\nHello")
			return reply, err
		}
	}
	tests := []struct {
		name, reply string
		err         error
		score       float64
		problem     string
	}{
		{name: "a score", reply: "```json\n{\"score\": 0.85, \"reason\": \"Close.\"}\n```", score: 0.85},
		{name: "kept to 1", reply: `{"score": 3}`, score: 1},
		{name: "no score", reply: `{"reason": "?"}`, problem: "no score"},
		{name: "not JSON", reply: "Looks right", problem: "isn't JSON"},
		{name: "no model", err: errors.New("no model"), problem: "no model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, _, err := JudgeNear(context.Background(), answer(tt.reply, tt.err), "Greet me", "Hello", "Hi")
			if tt.problem != "" {
				assert.ErrorContains(t, err, tt.problem)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tt.score, score, 1e-9)
		})
	}
}

func TestRunLog(t *testing.T) {
	l := OpenRunLog(t.TempDir())
	none, err := l.List("/ws", "shop", 0)
	require.NoError(t, err)
	assert.Empty(t, none)
	for i, id := range []string{"r1", "r2", "r3"} {
		require.NoError(t, l.Append(api.EvalRun{ID: id, Workspace: "/ws", Eval: "shop", Started: time.Unix(int64(i), 0)}))
	}
	require.NoError(t, l.Append(api.EvalRun{ID: "other", Workspace: "/other", Eval: "shop"}))
	runs, err := l.List("/ws", "shop", 2)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, "r3", runs[0].ID, "newest first")
	_, err = l.List("/ws", "../x", 0)
	assert.ErrorIs(t, err, ErrUnknown)
	assert.ErrorIs(t, l.Append(api.EvalRun{Eval: "Bad"}), ErrUnknown)
	assert.FileExists(t, filepath.Join(l.dir, mustFile(t, l, "/ws", "shop")))
}

func mustFile(t *testing.T, l *RunLog, ws, eval string) string {
	t.Helper()
	p, err := l.file(ws, eval)
	require.NoError(t, err)
	return filepath.Base(p)
}
