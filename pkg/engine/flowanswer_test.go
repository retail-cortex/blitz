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

	"github.com/retail-cortex/blitz/pkg/a2ui"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A human task's form is filled from the run as the step starts: a
// template alone keeps its type, one in text is text; a bad one fails the
// step (WF-18).
func TestFillForm(t *testing.T) {
	vars := flows.Vars{Input: "42", Steps: map[string]flows.Result{"pull": {Data: map[string]any{"amount": 240}}}}
	got, err := fillForm(&flows.Form{
		Components: []map[string]any{{"id": "root", "component": "Text", "text": "Order {{ input }}"}},
		Data:       map[string]any{"amount": "{{ steps.pull.data.amount }}"},
	}, vars)
	require.NoError(t, err)
	assert.Equal(t, "Order 42", got.Components[0]["text"])
	assert.Equal(t, 240.0, got.Data["amount"], "a number stays a number")

	none, err := fillForm(nil, vars)
	require.NoError(t, err)
	assert.Nil(t, none)
	_, err = fillForm(&flows.Form{Data: map[string]any{"x": "{{ nope( }}"}}, vars)
	assert.ErrorContains(t, err, "its form")
}

// The engine checks an answer against the task's form before a run goes
// on: no client is trusted to have (WF-18).
func TestCheckAnswer(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	writeFlow(t, w, "approve", "nodes:\n  - id: ask\n    type: human_task\n    "+approveForm+"\n  - {id: wait, type: timer, wait: 1h}\n")
	s := newSession(t, w)
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/approve", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	var ask, wait string
	for _, rec := range w.FlowTasks() {
		for _, task := range rec.Tasks {
			if task.Kind == flows.HumanTask {
				ask = task.ID
			} else {
				wait = task.ID
			}
		}
	}
	require.NotEmpty(t, ask)
	require.NotEmpty(t, wait)

	action := func(surface, note string) []byte {
		return []byte(`{"version": "v0.9", "action": {"name": "approve", "surfaceId": "` + surface + `", "sourceComponentId": "ok", "timestamp": "2026-10-18T09:00:00Z", "context": {"note": "` + note + `"}}}`)
	}
	answer, err := w.CheckAnswer(ask, action(ask, "fine"), nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"action": "approve", "note": "fine"}, answer)

	cases := []struct {
		name         string
		task         string
		action, data []byte
		is           error
		problem      string
	}{
		{name: "no such task", task: "run/none", action: action("", "x"), is: ErrNoTask},
		{name: "a timer", task: wait, action: action("", "x"), is: ErrNotAnswerable},
		{name: "not an action", task: ask, action: []byte(`{}`), problem: "version"},
		{name: "another surface", task: ask, action: action("other/step", "x"), problem: "it answers other/step"},
		{name: "a data model that isn't an object", task: ask, action: action(ask, "x"), data: []byte(`[1]`), problem: "isn't a JSON object"},
		{name: "context and data disagree", task: ask, action: action(ask, "x"), data: []byte(`{"note": "y"}`), problem: "isn't what the form holds"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := w.CheckAnswer(c.task, c.action, c.data)
			if c.is != nil {
				assert.ErrorIs(t, err, c.is)
				return
			}
			var bad *AnswerError
			require.ErrorAs(t, err, &bad)
			assert.Contains(t, bad.Error(), c.problem)
		})
	}
	assert.Equal(t, "the answer isn't accepted: a: b; c", (&AnswerError{Problems: []a2ui.Problem{{Component: "a", Message: "b"}, {Message: "c"}}}).Error())
}

// A step whose failure an error link caught before the run paused is
// replayed as caught when it resumes: the run goes the way it went
// (WF-12, WF-16). A script step naming a skill that isn't there fails.
func TestFlowResumeReplaysACaughtStep(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	writeFlow(t, w, "caught", `nodes:
  - {id: miss, type: read_file, path: nope.md}
  - id: ask
    type: human_task
    `+approveForm+`
  - {id: save, type: save_file, path: out/note.md}
edges:
  - {from: miss, to: ask, error: true}
  - {from: ask, to: save}
`)
	s := newSession(t, w)
	status := map[string]string{}
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/caught", Command: true}, func(e api.Event) {
		if e.Step != nil {
			status[e.Step.ID] = e.Step.Status
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "waiting", status["ask"])
	tasks := w.FlowTasks()
	require.Len(t, tasks, 1)
	resumed := map[string]string{}
	_, err = w.ResumeFlow(context.Background(), tasks[0].Tasks[0].ID, map[string]any{"note": "ok"}, func(e api.Event) {
		if e.Step != nil {
			resumed[e.Step.ID] = e.Step.Status
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "done", resumed["save"])
	assert.NotContains(t, resumed, "miss", "replayed, not run again")
	data, err := os.ReadFile(filepath.Join(w.Dir(), "out", "note.md"))
	require.NoError(t, err)
	assert.Equal(t, "note: ok", string(data))

	writeFlow(t, w, "script", "nodes:\n  - {id: run, type: script, skill: nope, script: go.py}\n")
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/script", Command: true}, func(api.Event) {})
	assert.Error(t, err)
}
