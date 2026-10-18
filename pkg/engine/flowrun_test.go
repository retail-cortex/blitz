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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
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

// Branches on ADK's engine (WF-11): two steps fed by none run side by
// side and join into one; a step feeding two last steps ends the run with
// both; the orchestration session is kept on disk.
func TestRunFlowBranches(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "a.md"), []byte("Alpha."), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "b.md"), []byte("Beta."), 0o644))
	writeFlow(t, w, "fan", `nodes:
  - {id: a, type: read_file, title: A, path: a.md}
  - {id: b, type: read_file, title: B, path: b.md}
  - {id: both, type: accumulate}
  - {id: one, type: save_file, path: out/one.md}
  - {id: two, type: save_file, path: out/two.md}
edges:
  - {from: a, to: both}
  - {from: b, to: both}
  - {from: both, to: one}
  - {from: both, to: two}
`)
	s := newSession(t, w)
	var mu sync.Mutex
	done := map[string]bool{}
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/fan", Command: true}, func(e api.Event) {
		if e.Step != nil && e.Step.Status == api.StepDone {
			mu.Lock()
			done[e.Step.ID] = true
			mu.Unlock()
		}
	})
	require.NoError(t, err)
	assert.Len(t, done, 5)
	both := "## A\n\nAlpha.\n\n## B\n\nBeta."
	assert.Equal(t, both+"\n\n"+both, res.Output, "both last steps")
	for _, f := range []string{"one.md", "two.md"} {
		data, err := os.ReadFile(filepath.Join(w.Dir(), "out", f))
		require.NoError(t, err)
		assert.Equal(t, both, string(data))
	}
	kept, _ := filepath.Glob(filepath.Join(w.cfg.Session.StorageDir, "flows", "*.events.jsonl"))
	assert.NotEmpty(t, kept, "the orchestration session is on disk")
}

// An exclusive gateway on CEL takes one branch; the other is skipped, and
// the step both lead to runs once, with the branch that ran.
func TestFlowExclusiveGateway(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "order.json"), []byte(`{"total": 150, "name": "ada"}`), 0o644))
	writeFlow(t, w, "orders", `nodes:
  - {id: read, type: read_file, path: order.json}
  - {id: size, type: exclusive}
  - {id: big, type: accumulate, title: Big}
  - {id: small, type: accumulate, title: Small}
  - {id: after, type: save_file, path: "out/{{ steps.read.data.name }}.md"}
edges:
  - {from: read, to: size}
  - {from: size, to: big, when: "output.data.total > 100"}
  - {from: size, to: small, default: true}
  - {from: big, to: after}
  - {from: small, to: after}
`)
	s := newSession(t, w)
	status := map[string]string{}
	var mu sync.Mutex
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/orders", Command: true}, func(e api.Event) {
		if e.Step != nil {
			mu.Lock()
			status[e.Step.ID] = e.Step.Status
			mu.Unlock()
		}
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"read": "done", "size": "done", "big": "done", "small": "skipped", "after": "done"}, status)
	data, err := os.ReadFile(filepath.Join(w.Dir(), "out", "ada.md"))
	require.NoError(t, err, "the template named the file")
	assert.Equal(t, "## size\n\n{\"total\": 150, \"name\": \"ada\"}", string(data), "only the branch that ran (big gathered the gateway's)")
}

// An inclusive gateway takes every link whose condition holds (CEL, and
// plain English judged by the model); the join waits for both.
func TestFlowInclusiveGateway(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	judge := runtime.NewMockLLM("judge", text(`{"verdict": "yes", "reason": "It asks for a refund."}`))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return judge, nil }
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "msg.md"), []byte("Refund me, it's urgent"), 0o644))
	writeFlow(t, w, "triage", `nodes:
  - {id: read, type: read_file, path: msg.md}
  - {id: route, type: inclusive}
  - {id: urgent, type: accumulate, title: Urgent}
  - {id: refund, type: accumulate, title: Refund}
  - {id: other, type: accumulate, title: Other}
  - {id: both, type: accumulate}
edges:
  - {from: read, to: route}
  - {from: route, to: urgent, when: 'output.text.contains("urgent")'}
  - {from: route, to: refund, if: The message asks for a refund}
  - {from: route, to: other, default: true}
  - {from: urgent, to: both}
  - {from: refund, to: both}
  - {from: other, to: both}
`)
	s := newSession(t, w)
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/triage", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	assert.Equal(t, "## Urgent\n\n## route\n\nRefund me, it's urgent\n\n## Refund\n\n## route\n\nRefund me, it's urgent", res.Output, "both branches, joined")
	assert.Contains(t, judge.Requests[0].Contents[0].Parts[0].Text, "The message asks for a refund")
}

// An error link catches a step's failure: the run goes on, the error as
// the next step's input. Retries are tried first and said in the chat.
func TestFlowErrorLinkAndRetry(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	writeFlow(t, w, "careful", `nodes:
  - {id: read, type: read_file, path: missing.md, retry: {attempts: 2, delay: 1ms}}
  - {id: ok, type: accumulate, title: OK}
  - {id: handle, type: save_file, path: out/error.md}
edges:
  - {from: read, to: ok}
  - {from: read, to: handle, error: true}
`)
	s := newSession(t, w)
	var notices []string
	status := map[string]string{}
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/careful", Command: true}, func(e api.Event) {
		if e.Notice != nil {
			notices = append(notices, e.Notice.Text)
		}
		if e.Step != nil {
			status[e.Step.ID] = e.Step.Status
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "failed", status["read"])
	assert.Equal(t, "skipped", status["ok"])
	assert.Contains(t, res.Output, "missing.md")
	saved, err := os.ReadFile(filepath.Join(w.Dir(), "out", "error.md"))
	require.NoError(t, err)
	assert.Contains(t, string(saved), "missing.md")
	require.Len(t, notices, 1)
	assert.Contains(t, notices[0], "try 2 of 2")
}

// An extract step pulls typed fields out; a condition reads them.
func TestFlowExtract(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	judge := runtime.NewMockLLM("judge", text(`{"priority": "high", "amount": "42.5", "items": ["a", 2]}`))
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return judge, nil }
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "ticket.md"), []byte("High priority: refund 42.50 for a and 2"), 0o644))
	writeFlow(t, w, "ticket", `nodes:
  - {id: read, type: read_file, path: ticket.md}
  - id: pull
    type: extract
    fields: [{name: priority}, {name: amount, type: number}, {name: items, type: list}]
  - {id: high, type: save_file, path: out/high.md}
  - {id: low, type: save_file, path: out/low.md}
edges:
  - {from: read, to: pull}
  - {from: pull, to: high, when: 'output.data.priority == "high" && output.data.amount > 40.0'}
  - {from: pull, to: low, default: true}
`)
	s := newSession(t, w)
	_, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/ticket", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	saved, err := os.ReadFile(filepath.Join(w.Dir(), "out", "high.md"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"priority": "high", "amount": 42.5, "items": ["a", "2"]}`, string(saved))
	assert.NoFileExists(t, filepath.Join(w.Dir(), "out", "low.md"))
	assert.Contains(t, judge.Requests[0].Contents[0].Parts[0].Text, "amount (number)")
}

// A call step runs another workflow on its input, its steps in the same
// run; workflows calling each other in a loop, or one that isn't there,
// can't run. A repeated step runs once per item (WF-15).
func TestFlowCallAndForEach(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "people.json"), []byte(`[{"name": "ada"}, {"name": "bo"}, {"name": "cy"}]`), 0o644))
	writeFlow(t, w, "keep", "nodes: [{id: save, type: save_file, title: Keep it, path: \"out/{{ input.size() }}.md\"}]\n")
	writeFlow(t, w, "main", `nodes:
  - {id: read, type: read_file, path: people.json}
  - {id: each, type: save_file, path: "out/{{ item.name }}-{{ index }}.md", for_each: steps.read.data, concurrency: 2}
  - {id: call, type: workflow, workflow: keep}
edges:
  - {from: read, to: each}
  - {from: each, to: call}
`)
	writeFlow(t, w, "ping", "nodes: [{id: a, type: workflow, workflow: pong}]\n")
	writeFlow(t, w, "pong", "nodes: [{id: a, type: workflow, workflow: ping}]\n")
	writeFlow(t, w, "lost", "nodes: [{id: a, type: workflow, workflow: nowhere}]\n")
	byName := map[string]flows.Flow{}
	for _, f := range w.Workflows() {
		byName[f.Name] = f
	}
	assert.Empty(t, byName["main"].Problem)
	assert.Contains(t, byName["ping"].Problem, "ping → pong → ping")
	assert.Contains(t, byName["pong"].Problem, "loop")
	assert.Contains(t, byName["lost"].Problem, `no workflow "nowhere"`)

	s := newSession(t, w)
	var mu sync.Mutex
	var steps []string
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/main", Command: true}, func(e api.Event) {
		if e.Step != nil && e.Step.Status == api.StepDone {
			mu.Lock()
			steps = append(steps, e.Step.Workflow+"/"+e.Step.ID)
			mu.Unlock()
		}
	})
	require.NoError(t, err)
	for i, name := range []string{"ada", "bo", "cy"} {
		data, err := os.ReadFile(filepath.Join(w.Dir(), "out", fmt.Sprintf("%s-%d.md", name, i)))
		require.NoError(t, err)
		assert.Contains(t, string(data), `"ada"`, "each got the step's input")
	}
	assert.Contains(t, steps, "keep/save", "the called workflow's step, said as its own")
	called, err := os.ReadDir(filepath.Join(w.Dir(), "out"))
	require.NoError(t, err)
	assert.Len(t, called, 4, "three items and the called workflow's file")
	assert.True(t, strings.HasPrefix(res.Output, "## 1\n\n"), "the repeated step's output went on: %q", res.Output)

	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/ping", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, "loop")
	writeFlow(t, w, "notlist", "nodes: [{id: a, type: accumulate, for_each: input}]\n")
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/notlist x", Command: true}, func(api.Event) {})
	assert.ErrorContains(t, err, "not a list")
}

// A background run (Studio's Run) of /name runs the workflow, its agent
// steps in the run's own chat (WF-10); other commands expand too.
func TestRunDetachedFlow(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools, text("Drafted."))
	writeFlow(t, w, "bg", "nodes: [{id: a, type: agent, instructions: Draft it.}]\n")
	var session string
	var steps []string
	res, err := w.RunDetached(context.Background(), api.Turn{Text: "/bg topic", Command: true}, func(id string) { session = id }, func(e api.Event) {
		if e.Step != nil {
			steps = append(steps, e.Step.ID+":"+e.Step.Status)
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "Drafted.", res.Output)
	assert.Equal(t, []string{"a:running", "a:done"}, steps)
	data, err := os.ReadFile(filepath.Join(w.cfg.Session.StorageDir, session+".jsonl"))
	require.NoError(t, err, "the run's own chat")
	assert.Contains(t, string(data), "/bg › a")
	_, err = w.RunDetached(context.Background(), api.Turn{Text: "/no-such-command", Command: true}, nil, func(api.Event) {})
	assert.Error(t, err, "an unknown command isn't sent as a prompt")
}

const approveForm = `form:
      components:
        - {id: root, component: Column, children: [note, ok]}
        - {id: note, component: TextField, label: Note, value: {path: /note}}
        - {id: ok_label, component: Text, text: Approve}
        - {id: ok, component: Button, child: ok_label, action: {event: {name: approve, context: {note: {path: /note}}}}}`

// A human task pauses the run and records what it waits on; the answer
// resumes it, finished steps replayed rather than run again; a workflow
// changed since it paused isn't resumed. A timer is resumed once it's due
// (WF-16).
func TestFlowHumanTaskAndTimer(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "req.md"), []byte("Refund 42"), 0o644))
	writeFlow(t, w, "approve", `nodes:
  - {id: read, type: read_file, path: req.md}
  - id: ask
    type: human_task
    title: Approve the refund
    instructions: "{{ steps.read.text }}?"
    `+approveForm+`
  - {id: save, type: save_file, path: out/note.md}
edges:
  - {from: read, to: ask}
  - {from: ask, to: save}
`)
	s := newSession(t, w)
	var notices []string
	status := map[string]string{}
	res, err := w.Run(context.Background(), s.ID, api.Turn{Text: "/approve", Command: true}, func(e api.Event) {
		if e.Step != nil {
			status[e.Step.ID] = e.Step.Status
		}
		if e.Notice != nil {
			notices = append(notices, e.Notice.Text)
		}
	})
	require.NoError(t, err)
	assert.Empty(t, res.Output)
	assert.Equal(t, map[string]string{"read": "done", "ask": "waiting"}, status)
	assert.Contains(t, notices, "⏸ /approve waits for you: Approve the refund")
	assert.NoFileExists(t, filepath.Join(w.Dir(), "out", "note.md"))
	tasks := w.FlowTasks()
	require.Len(t, tasks, 1)
	require.Len(t, tasks[0].Tasks, 1)
	task := tasks[0].Tasks[0]
	assert.Equal(t, "Refund 42?", task.Message)
	assert.Equal(t, "Button", task.Form.Components[3]["component"])
	assert.Equal(t, s.ID, tasks[0].Session)

	resumed := map[string]string{}
	res, err = w.ResumeFlow(context.Background(), task.ID, map[string]any{"note": "fine by me"}, func(e api.Event) {
		if e.Step != nil {
			resumed[e.Step.ID] = e.Step.Status
		}
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"ask": "done", "save": "done"}, resumed, "read wasn't run again")
	saved, err := os.ReadFile(filepath.Join(w.Dir(), "out", "note.md"))
	require.NoError(t, err)
	assert.Equal(t, "note: fine by me", string(saved))
	assert.Equal(t, "note: fine by me", res.Output)
	assert.Empty(t, w.FlowTasks(), "the run's record is gone")
	_, err = w.ResumeFlow(context.Background(), task.ID, nil, func(api.Event) {})
	assert.ErrorIs(t, err, ErrNoTask)

	// A changed workflow isn't resumed.
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/approve", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	task = w.FlowTasks()[0].Tasks[0]
	writeFlow(t, w, "approve", "nodes: [{id: read, type: read_file, path: req.md}]\n")
	_, err = w.ResumeFlow(context.Background(), task.ID, map[string]any{}, func(api.Event) {})
	assert.ErrorContains(t, err, "changed since its run paused")
	assert.Empty(t, w.FlowTasks())

	// A timer.
	writeFlow(t, w, "later", "nodes:\n  - {id: wait, type: timer, title: Wait, wait: 1h}\n  - {id: save, type: save_file, path: out/later.md}\nedges: [{from: wait, to: save}]\n")
	_, err = w.Run(context.Background(), s.ID, api.Turn{Text: "/later", Command: true}, func(api.Event) {})
	require.NoError(t, err)
	dir := FlowRecordsDir(w.cfg.Session.StorageDir)
	assert.Empty(t, DueTimers(dir, time.Now()), "not due yet")
	due := DueTimers(dir, time.Now().Add(2*time.Hour))
	require.Len(t, due, 1)
	for id := range due {
		_, err = w.ResumeFlow(context.Background(), id, map[string]any{"fired_at": "now"}, func(api.Event) {})
		require.NoError(t, err)
	}
	assert.FileExists(t, filepath.Join(w.Dir(), "out", "later.md"))
}
