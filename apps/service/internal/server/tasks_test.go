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
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
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
        - {id: root, component: Column, children: [note]}
        - {id: note, component: TextField, label: Note, value: {path: /note}}
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
	assert.Contains(t, task.FormJson, `"TextField"`)

	_, err = c.workspaces.AnswerHumanTask(ctx, connect.NewRequest(&pb.AnswerHumanTaskRequest{Workspace: dir, TaskId: task.Id, AnswerJson: "[nope"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	answered, err := c.workspaces.AnswerHumanTask(ctx, connect.NewRequest(&pb.AnswerHumanTaskRequest{Workspace: dir, TaskId: task.Id, AnswerJson: `{"note": "yes"}`}))
	require.NoError(t, err)
	runState(t, c, answered.Msg.Run.Id, "done")
	data, err := os.ReadFile(filepath.Join(dir, "out", "answer.md"))
	require.NoError(t, err)
	assert.Equal(t, "note: yes", string(data))
	assert.Equal(t, "TASK_NOT_FOUND", reason(t, unary(c.workspaces.AnswerHumanTask, &pb.AnswerHumanTaskRequest{Workspace: dir, TaskId: task.Id})))

	started, err = c.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: dir, Turn: &pb.Turn{Text: "/later", Command: true}}))
	require.NoError(t, err)
	runState(t, c, started.Msg.Run.Id, "done")
	list, err = c.workspaces.ListHumanTasks(ctx, connect.NewRequest(&pb.ListHumanTasksRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Tasks, 1)
	assert.Equal(t, "timer", list.Msg.Tasks[0].Kind)
	assert.NotNil(t, list.Msg.Tasks[0].FireAt)
	s.resumeDueTimers(ctx)
	assert.NoFileExists(t, filepath.Join(dir, "out", "later.md"), "not due yet")
	timeNow = func() time.Time { return time.Now().Add(2 * time.Hour) }
	t.Cleanup(func() { timeNow = time.Now })
	s.resumeDueTimers(ctx)
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(dir, "out", "later.md")); return err == nil }, 10*time.Second, 20*time.Millisecond)
}
