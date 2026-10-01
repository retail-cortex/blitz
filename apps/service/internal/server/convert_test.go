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
	"errors"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A notice goes to clients as its own kind of turn event.
func TestNoticeEvent(t *testing.T) {
	for _, n := range []api.Notice{{Text: "Couldn't save this message", Error: true}, {Text: "Using the fallback model"}} {
		got := eventMsg(api.Event{Notice: &n})
		k, ok := got.Kind.(*pb.TurnEvent_Notice)
		require.True(t, ok, "kind %T", got.Kind)
		assert.Equal(t, n.Text, k.Notice.Text)
		assert.Equal(t, n.Error, k.Notice.Error)
	}
}

// The engine's values become the API's messages field for field; values
// the engine leaves out stay unset.
func TestConversions(t *testing.T) {
	assert.Nil(t, timestamp(time.Time{}), "a zero time is unset")
	assert.Equal(t, "read-only", savedMsg(api.Saved{Path: "p", Err: errors.New("read-only")}).Error)
	assert.Nil(t, structMsg(nil))
	assert.Nil(t, structMsg(map[string]any{"ch": make(chan int)}), "a value JSON can't carry")

	n, temp, effort := 5, 0.5, "high"
	ms := modelSettingsMsg(config.ModelSettings{Temperature: &temp, MaxTokens: &n, Seed: &n, ThinkingBudget: &n, ReasoningEffort: &effort})
	assert.Equal(t, int32(5), ms.GetMaxTokens())
	assert.Equal(t, int32(5), ms.GetSeed())
	assert.Equal(t, int32(5), ms.GetThinkingBudget())
	assert.Equal(t, "high", ms.GetReasoningEffort())

	sk := skillMsg(api.SkillInfo{Name: "s", Tools: []api.SkillTool{{Name: "t"}}, Scripts: []api.SkillScript{{Name: "run.py", Timeout: time.Minute}}})
	require.Len(t, sk.Tools, 1)
	require.Len(t, sk.Scripts, 1)
	assert.Equal(t, time.Minute, sk.Scripts[0].Timeout.AsDuration())

	p := projectMsg(api.ProjectSettings{Hash: "h", Pending: []api.ProjectItem{{File: ".blitz/settings.toml", Key: "allow"}}})
	require.Len(t, p.Pending, 1)
	assert.Equal(t, "allow", p.Pending[0].Key)
	assert.Empty(t, p.Applied)

	ended := taskMsg(api.TaskInfo{ID: "task-1", Started: time.Now(), Ended: time.Now()})
	assert.NotNil(t, ended.Ended)
	assert.Nil(t, taskMsg(api.TaskInfo{ID: "task-2"}).Ended, "a running task has no end")

	approval := taskRequestMsg(api.TaskRequest{ID: "r1", TaskID: "task-1", Agent: "qa", Approval: &api.ApprovalRequest{Tool: "write_file", Kind: api.ActionWrite}})
	assert.Equal(t, pb.ActionKind_ACTION_KIND_WRITE, approval.GetApprovalRequest().GetKind())
	question := taskRequestMsg(api.TaskRequest{ID: "r2", Question: "Tabs?", Options: []string{"tabs"}, MultiSelect: true})
	assert.True(t, question.GetQuestion().GetMultiSelect())
	assert.Equal(t, "task-1", taskEventMsg(api.SessionEvent{Task: &api.TaskInfo{ID: "task-1"}}).GetTask().GetId())
	assert.Equal(t, "r1", taskEventMsg(api.SessionEvent{Request: &api.TaskRequest{ID: "r1"}}).GetQuestion().GetRequestId())
	assert.Equal(t, "r1", taskEventMsg(api.SessionEvent{Resolved: "r1"}).GetResolved())

	tasks := eventMsg(api.Event{Tasks: []api.Task{{Content: "step", Status: "done"}}})
	require.Len(t, tasks.GetTasks().GetItems(), 1)
	call := eventMsg(api.Event{ToolCall: &api.ToolCall{ID: "c", Name: "list_files", Args: map[string]any{"path": "."}}})
	assert.Equal(t, ".", call.GetToolCall().GetArgs().AsMap()["path"])
	assert.Nil(t, eventMsg(api.Event{}).Kind, "an empty event")

	for k, want := range map[api.ActionKind]pb.ActionKind{
		api.ActionCommand: pb.ActionKind_ACTION_KIND_COMMAND,
		api.ActionWrite:   pb.ActionKind_ACTION_KIND_WRITE,
		api.ActionDelete:  pb.ActionKind_ACTION_KIND_DELETE,
		api.ActionNetwork: pb.ActionKind_ACTION_KIND_NETWORK,
		api.ActionMCP:     pb.ActionKind_ACTION_KIND_MCP,
		"other":           pb.ActionKind_ACTION_KIND_UNSPECIFIED,
	} {
		t.Run(string(k), func(t *testing.T) {
			assert.Equal(t, want, actionKind(k))
		})
	}
	for d, want := range map[pb.Decision]api.Decision{
		pb.Decision_DECISION_ONCE:    api.DecisionOnce,
		pb.Decision_DECISION_SESSION: api.DecisionSession,
		pb.Decision_DECISION_ALWAYS:  api.DecisionAlways,
		pb.Decision_DECISION_DENY:    api.DecisionDeny,
	} {
		t.Run(d.String(), func(t *testing.T) {
			assert.Equal(t, want, decision(d))
		})
	}
}

// Workers' states and runs' statuses become the API's enums; a run's
// refusals and error travel with it.
func TestWorkerConversions(t *testing.T) {
	for s, want := range map[api.State]pb.WorkerState{
		api.StateNew:      pb.WorkerState_WORKER_STATE_NEW,
		api.StateEnabled:  pb.WorkerState_WORKER_STATE_ENABLED,
		api.StateDisabled: pb.WorkerState_WORKER_STATE_DISABLED,
		api.StateChanged:  pb.WorkerState_WORKER_STATE_CHANGED,
		api.StateInvalid:  pb.WorkerState_WORKER_STATE_INVALID,
		"other":           pb.WorkerState_WORKER_STATE_UNSPECIFIED,
	} {
		t.Run(string(s), func(t *testing.T) {
			assert.Equal(t, want, workerState(s))
		})
	}
	for s, want := range map[api.RunStatus]pb.RunStatus{
		api.RunRunning:   pb.RunStatus_RUN_STATUS_RUNNING,
		api.RunSucceeded: pb.RunStatus_RUN_STATUS_SUCCEEDED,
		api.RunFailed:    pb.RunStatus_RUN_STATUS_FAILED,
		api.RunLimited:   pb.RunStatus_RUN_STATUS_LIMITED,
		api.RunSkipped:   pb.RunStatus_RUN_STATUS_SKIPPED,
		"other":          pb.RunStatus_RUN_STATUS_UNSPECIFIED,
	} {
		t.Run(string(s), func(t *testing.T) {
			assert.Equal(t, want, runStatus(s))
		})
	}
	run := api.Run{ID: "r", Status: api.RunLimited, Error: "cost limit", Refusals: []api.Refusal{{Tool: "run_shell_command", Kind: api.ActionCommand}}}
	w := workerMsg(api.WorkerInfo{Name: "w", LastRun: &run})
	require.NotNil(t, w.LastRun)
	assert.Equal(t, "RUN_LIMITED", w.LastRun.Error.GetReason())
	require.Len(t, w.LastRun.Refusals, 1)
	assert.Equal(t, pb.ActionKind_ACTION_KIND_COMMAND, w.LastRun.Refusals[0].Kind)
}
