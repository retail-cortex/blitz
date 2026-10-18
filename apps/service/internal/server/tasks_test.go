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

package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/a2ui"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFlowFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, ".agents", "workflows", name+".flow.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// A workflow run in the background pauses at a human task: its task is
// listed (every workspace's), answering it resumes the run in the
// background; a timer's run resumes once it's due (WF-16, WF-17).
func TestHumanTasksOverTheAPI(t *testing.T) {
	c, s := serve(t, configtest.RunTools)
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFlowFile(t, dir, "approve", `nodes:
  - id: ask
    type: human_task
    title: Approve it
    instructions: "Approve {{ input }}?"
    form:
      components:
        - {id: root, component: Column, children: [note, ok]}
        - id: note
          component: TextField
          label: Note
          value: {path: /note}
          checks: [{call: required, args: {value: {path: /note}}, message: say why}]
        - {id: ok_label, component: Text, text: Approve}
        - {id: ok, component: Button, child: ok_label, action: {event: {name: approve, context: {note: {path: /note}}}}}
      data: {note: "{{ input }}"}
  - {id: save, type: save_file, path: out/answer.md}
edges: [{from: ask, to: save}]
`)
	writeFlowFile(t, dir, "later", "nodes:\n  - {id: wait, type: timer, wait: 1h}\n  - {id: save, type: save_file, path: out/later.md}\nedges: [{from: wait, to: save}]\n")

	started, err := c.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: dir, Turn: &pb.Turn{Text: "/approve refund 42", Command: true}}))
	require.NoError(t, err)
	runState(t, c, started.Msg.Run.Id, "done")
	list, err := c.workspaces.ListHumanTasks(ctx, connect.NewRequest(&pb.ListHumanTasksRequest{}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Tasks, 1)
	assert.Equal(t, "notify", list.Msg.Alerts, "[ui] task_alerts' default")
	task := list.Msg.Tasks[0]
	assert.Equal(t, "Approve refund 42?", task.Message)
	assert.Equal(t, "approve", task.Workflow)
	assert.Equal(t, dir, task.Workspace)
	var msgs []map[string]any
	require.NoError(t, json.Unmarshal([]byte(task.A2UiJson), &msgs))
	form, surface, err := a2ui.FormOf(msgs)
	require.NoError(t, err)
	assert.Equal(t, task.Id, surface, "the surface is the task")
	assert.Equal(t, "refund 42", form.Data["note"], "the form's data filled from the run")

	answer := func(action, data string) error {
		return unary(c.workspaces.AnswerHumanTask, &pb.AnswerHumanTaskRequest{Workspace: dir, TaskId: task.Id, ActionJson: action, DataJson: data})
	}
	act := func(note string) string {
		return `{"version": "v0.9", "action": {"name": "approve", "surfaceId": "` + task.Id + `", "sourceComponentId": "ok", "timestamp": "2026-10-18T09:00:00Z", "context": {"note": "` + note + `"}}}`
	}
	for _, tc := range []struct{ name, action, data, problem string }{
		{name: "not an action", action: "[nope", problem: "not an A2UI message"},
		{name: "a required field empty", action: act(""), problem: "note: say why"},
		{name: "a button that isn't there", action: strings.Replace(act("yes"), `"ok"`, `"admin"`, 1), problem: "isn't one of the form's buttons"},
		{name: "the data model not an object", action: act("yes"), data: "[1]", problem: "isn't a JSON object"},
		{name: "context and data disagree", action: act("yes"), data: `{"note": "no"}`, problem: "isn't what the form holds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := answer(tc.action, tc.data)
			assert.Equal(t, "ANSWER_INVALID", reason(t, err))
			assert.ErrorContains(t, err, tc.problem)
		})
	}
	answered, err := c.workspaces.AnswerHumanTask(ctx, connect.NewRequest(&pb.AnswerHumanTaskRequest{Workspace: dir, TaskId: task.Id, ActionJson: act("yes"), DataJson: `{"note": "yes"}`}))
	require.NoError(t, err)
	runState(t, c, answered.Msg.Run.Id, "done")
	data, err := os.ReadFile(filepath.Join(dir, "out", "answer.md"))
	require.NoError(t, err)
	assert.Equal(t, "action: approve\nnote: yes", string(data))
	assert.Equal(t, "TASK_NOT_FOUND", reason(t, unary(c.workspaces.AnswerHumanTask, &pb.AnswerHumanTaskRequest{Workspace: dir, TaskId: task.Id})))

	started, err = c.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: dir, Turn: &pb.Turn{Text: "/later", Command: true}}))
	require.NoError(t, err)
	runState(t, c, started.Msg.Run.Id, "done")
	list, err = c.workspaces.ListHumanTasks(ctx, connect.NewRequest(&pb.ListHumanTasksRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Tasks, 1)
	assert.Equal(t, "timer", list.Msg.Tasks[0].Kind)
	assert.NotNil(t, list.Msg.Tasks[0].FireAt)
	assert.Equal(t, "NOT_ANSWERABLE", reason(t, unary(c.workspaces.AnswerHumanTask, &pb.AnswerHumanTaskRequest{Workspace: dir, TaskId: list.Msg.Tasks[0].Id})))
	s.resumeDueTimers(ctx)
	assert.NoFileExists(t, filepath.Join(dir, "out", "later.md"), "not due yet")
	timeNow = func() time.Time { return time.Now().Add(2 * time.Hour) }
	t.Cleanup(func() { timeNow = time.Now })
	s.resumeDueTimers(ctx)
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(dir, "out", "later.md")); return err == nil }, 10*time.Second, 20*time.Millisecond)
}

// A task watch sends the list at once, then each time it changes: a run
// pausing at a human task (WF-17).
func TestWatchHumanTasks(t *testing.T) {
	watchTasksEvery = 20 * time.Millisecond
	t.Cleanup(func() { watchTasksEvery = time.Second })
	c, _ := serve(t, configtest.RunTools)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFlowFile(t, dir, "ask", "nodes:\n  - id: ask\n    type: human_task\n    form: {components: [{id: root, component: Text, text: Hi}]}\n")

	stream, err := c.workspaces.WatchHumanTasks(ctx, connect.NewRequest(&pb.ListHumanTasksRequest{}))
	require.NoError(t, err)
	defer stream.Close()
	require.True(t, stream.Receive(), "%v", stream.Err())
	assert.Empty(t, stream.Msg().Tasks)
	assert.Equal(t, "notify", stream.Msg().Alerts)

	_, err = c.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: dir, Turn: &pb.Turn{Text: "/ask", Command: true}}))
	require.NoError(t, err)
	require.True(t, stream.Receive(), "%v", stream.Err())
	require.Len(t, stream.Msg().Tasks, 1)
	assert.Equal(t, "ask", stream.Msg().Tasks[0].Workflow)
}
