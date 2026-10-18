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
	"slices"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFlow(t *testing.T, w *Workspace, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(w.ProjectWorkflowsDir(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.ProjectWorkflowsDir(), name+flows.Suffix), []byte(content), 0o644))
}

// A workflow runs as a turn: it reads a file, an agent step's answer
// fails a revise rule and is revised, two inputs are joined, and the
// result is saved.
func TestRunFlow(t *testing.T) {
	w, llm := openTestWith(t, configtest.RunTools, text("A long brief about the sky."), text("Sky: blue."))
	withJudge(t, w, "fail: too long", "pass")
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "notes.md"), []byte("The sky is blue."), 0o644))
	// A gate for another agent: judged only because the step names it.
	writeRule(t, w, "short", "---\nwhen: after_agent\naction: revise\nagents: [editor]\n---\nThe answer is one line.\n")
	writeFlow(t, w, "brief", `description: A brief from the notes
nodes:
  - {id: notes, type: read_file, title: Read the notes, path: notes.md}
  - {id: draft, type: agent, title: Draft, instructions: Write a brief., rules: [short]}
  - {id: both, type: accumulate}
  - {id: save, type: save_file, path: out/brief.md}
edges:
  - {from: notes, to: draft}
  - {from: notes, to: both}
  - {from: draft, to: both}
  - {from: both, to: save}
`)
	require.True(t, slices.ContainsFunc(w.ListCommands(), func(c api.CommandInfo) bool { return c.Name == "brief" && c.Source == "workflow" }))
	s := newSession(t, w)
	var steps []string
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/brief for the team", Command: true}, func(e api.Event) {
		if e.Step != nil {
			steps = append(steps, e.Step.ID+":"+e.Step.Status)
		}
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"notes:running", "notes:done", "draft:running", "draft:done", "both:running", "both:done", "save:running", "save:done"}, steps)
	want := "## Read the notes\n\nThe sky is blue.\n\n## Draft\n\nSky: blue."
	assert.Equal(t, want, res.Output)
	saved, err := os.ReadFile(filepath.Join(w.Dir(), "out", "brief.md"))
	require.NoError(t, err)
	assert.Equal(t, want, string(saved))
	first := llm.Requests[0].Contents
	prompt := first[len(first)-1].Parts[0].Text
	assert.Contains(t, prompt, `You're the step "Draft" of the workflow "brief".`)
	assert.Contains(t, prompt, "Write a brief.")
	assert.Contains(t, prompt, "The sky is blue.", "the step's input")
	second := llm.Requests[1].Contents
	assert.Contains(t, second[len(second)-1].Parts[0].Text, "too long", "the revise rule's reason")
}

func TestRunFlowStops(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools, text("Nope."), text("Fine."))
	withJudge(t, w, "fail: it says nope")
	writeRule(t, w, "polite", "---\nwhen: after_agent\naction: block\nagents: [editor]\n---\nThe answer is polite.\n")
	writeFlow(t, w, "strict", "nodes:\n  - {id: a, type: agent, instructions: Answer., rules: [polite, missing]}\n  - {id: b, type: save_file, path: never.md}\nedges: [{from: a, to: b}]\n")
	s := newSession(t, w)
	var notices []api.Notice
	var failed *api.FlowStep
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/strict", Command: true}, func(e api.Event) {
		if e.Notice != nil {
			notices = append(notices, *e.Notice)
		}
		if e.Step != nil && e.Step.Status == api.StepFailed {
			failed = e.Step
		}
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped at a")
	assert.Contains(t, err.Error(), "the rule polite failed: it says nope")
	require.NotNil(t, failed)
	assert.Equal(t, "a", failed.ID)
	assert.NoFileExists(t, filepath.Join(w.Dir(), "never.md"))
	assert.Empty(t, notices, "polite blocked before missing was looked at")

	writeFlow(t, w, "broken", "nodes: [{id: a, type: agent}]\n")
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/broken", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, "can't run")
	writeFlow(t, w, "who", "nodes: [{id: a, type: agent, agent: nobody, instructions: Hi.}]\n")
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/who", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, `agent "nobody" isn't defined`)
	writeFlow(t, w, "reads", "nodes: [{id: a, type: read_file, path: ../outside.md}]\n")
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/reads", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, "reading ../outside.md")

	// A gate that applies to every agent is judged by the step's turn, not
	// again because the step names it.
	judge := withJudge(t, w, "pass")
	writeRule(t, w, "everyone", "---\nwhen: after_agent\naction: block\n---\nThe answer is kind.\n")
	writeFlow(t, w, "once", "nodes: [{id: a, type: agent, instructions: Answer., rules: [everyone]}]\n")
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/once", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	assert.Len(t, judge.Requests, 1, "judged once")

	f, probs, err := w.SaveWorkflow(flows.Flow{Name: "saved", Nodes: []flows.Node{{ID: "a", Type: flows.Accumulate}}}, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	assert.Equal(t, "saved", f.Name)
	require.NoError(t, w.DeleteWorkflow("saved"))
}
