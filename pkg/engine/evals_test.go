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
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine/evals"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func writeEval(t *testing.T, w *Workspace, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(w.ProjectEvalsDir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.ProjectEvalsDir(), name+".md"), []byte(content), 0o644))
}

// TestRunEval runs three cases: an exact one that passes, a near one the
// judge scores, and one whose write is refused and whose answer misses
// (scoped warn); the cases' sessions are gone after.
func TestRunEval(t *testing.T) {
	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "notes.md", "content": "x\n"}}}}}
	w, llm := openTestWith(t, configtest.RunTools, text("4"), text("Paris."), create, text("I couldn't write it."))
	judge := runtime.NewMockLLM("judge", text(`{"score": 0.9, "reason": "Same city."}`))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return judge, nil }
	require.NoError(t, os.MkdirAll(filepath.Join(w.Dir(), ".agents", "skills", "terse"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), ".agents", "skills", "terse", "SKILL.md"), []byte("---\nname: terse\ndescription: Short answers\n---\nAnswer in one line.\n"), 0o644))
	writeEval(t, w, "basics", "---\nskills: [terse]\n---\n| ID | Name | Prompt | Output | Output type | Threshold | Scope |\n|-|-|-|-|-|-|-|\n"+
		"| sum | Sum | What's 2+2? | 4 | Exact | | Fail |\n| capital | | France's capital? | Paris | Near | 0.8 | Fail |\n| write | | Write notes.md | Done. | Exact | | Warn |\n")
	sessions := func() int { e, _ := os.ReadDir(w.cfg.Session.StorageDir); return len(e) }
	before := sessions()

	var started api.EvalRun
	var streamed []string
	run, err := w.RunEval(context.Background(), "basics", api.EvalOptions{
		OnStart:  func(r api.EvalRun) { started = r },
		OnResult: func(r api.EvalResult) { streamed = append(streamed, r.Case) },
	})
	require.NoError(t, err)
	assert.Equal(t, api.EvalRunning, started.Status)
	assert.Equal(t, run.ID, started.ID)
	assert.Equal(t, []string{"sum", "capital", "write"}, streamed)
	require.Len(t, run.Results, 3)
	assert.Equal(t, api.EvalPassed, run.Status, "only a warning: %+v", run.Results)
	sum, capital, write := run.Results[0], run.Results[1], run.Results[2]
	assert.Equal(t, api.EvalPass, sum.Status)
	assert.Equal(t, float64(1), sum.Score)
	assert.Equal(t, w.engine.ActiveAgent(), sum.Agent)
	assert.Equal(t, api.EvalPass, capital.Status)
	assert.InDelta(t, 0.9, capital.Score, 1e-9)
	assert.Equal(t, "Same city.", capital.Reason)
	assert.Equal(t, api.EvalWarn, write.Status)
	require.Len(t, write.Refusals, 1)
	assert.Contains(t, write.Refusals[0], "create_file")
	assert.NoFileExists(t, filepath.Join(w.Dir(), "notes.md"), "an eval changes nothing")
	assert.Equal(t, before, sessions(), "the cases' sessions are deleted")

	first := llm.Requests[0].Contents
	assert.Contains(t, first[len(first)-1].Parts[0].Text, "Answer in one line.", "the eval's skill goes with the prompt")
	runs, err := w.EvalRuns("basics", 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, run.ID, runs[0].ID)
}

func TestRunEvalRefusals(t *testing.T) {
	w, _ := openTestWith(t, nil, text("five"))
	_, err := w.RunEval(context.Background(), "none", api.EvalOptions{})
	assert.ErrorIs(t, err, api.ErrUnknownEval)
	writeEval(t, w, "broken", "| ID | Prompt |\n|-|-|\n| a | b |\n")
	_, err = w.RunEval(context.Background(), "broken", api.EvalOptions{})
	assert.ErrorIs(t, err, api.ErrEvalNotRunnable)
	writeEval(t, w, "one", "---\nagents: [nobody]\n---\n| ID | Prompt | Output | Type |\n|-|-|-|-|\n| a | 2+3? | 5 | exact |\n")
	_, err = w.RunEval(context.Background(), "one", api.EvalOptions{Cases: []string{"zz"}})
	assert.ErrorIs(t, err, api.ErrEvalNotRunnable, "no case to run")
	run, err := w.RunEval(context.Background(), "one", api.EvalOptions{Cases: []string{"a"}})
	require.NoError(t, err)
	require.Len(t, run.Results, 1)
	assert.Equal(t, api.EvalError, run.Results[0].Status)
	assert.Contains(t, run.Results[0].Reason, `agent "nobody" isn't defined`)
	assert.Equal(t, api.EvalFailed, run.Status, "an error in a fail-scoped case fails the eval")

	writeEval(t, w, "vectors", "---\nnear: embedding\n---\n| ID | Prompt | Output |\n|-|-|-|\n| a | p | o |\n")
	run, err = w.RunEval(context.Background(), "vectors", api.EvalOptions{})
	require.NoError(t, err)
	assert.Equal(t, api.EvalFailed, run.Status)
	assert.Contains(t, run.Error, "search.embedding_model")
}

func TestEvalFiles(t *testing.T) {
	w := openTest(t)
	e, probs, err := w.SaveEval(evals.Eval{Name: "shop", Cases: []evals.Case{{ID: "a", Prompt: "p", Output: "o"}}}, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	assert.Equal(t, filepath.Join(w.ProjectEvalsDir(), "shop.md"), e.Path)
	assert.Equal(t, evals.DefaultThreshold, e.Cases[0].Threshold)
	assert.Len(t, w.Evals(), 1)
	_, probs, _ = w.SaveEval(evals.Eval{Name: "Bad"}, "")
	assert.NotEmpty(t, probs)
	require.NoError(t, w.DeleteEval("shop"))
	assert.Empty(t, w.Evals())
}
