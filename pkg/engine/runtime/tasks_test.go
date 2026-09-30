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

package runtime

import (
	"context"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// gated answers "review done" once released, or fails with its context.
type gated struct {
	release chan struct{}
	once    sync.Once
}

func newGated() *gated        { return &gated{release: make(chan struct{})} }
func (g *gated) Name() string { return "gemini-3.8-flash" }
func (g *gated) open()        { g.once.Do(func() { close(g.release) }) }
func (g *gated) GenerateContent(ctx context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		select {
		case <-g.release:
			yield(&model.LLMResponse{
				Content:       genai.NewContentFromText("review done\nno problems found", genai.RoleModel),
				UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 10},
			}, nil)
		case <-ctx.Done():
			yield(nil, ctx.Err())
		}
	}
}

// taskEngine is an engine whose qa agent runs on sub (the tasks' model),
// and a run context for session "s".
func taskEngine(t *testing.T, sub model.LLM, cfg func(*config.Config), replies ...*genai.Content) (engineFixture, context.Context, *[]api.TaskInfo) {
	t.Helper()
	var mu sync.Mutex
	var ended []api.TaskInfo
	f := newEngineWith(t, fixtureOpts{cfg: cfg, opts: []Option{
		WithAgentModel("qa", sub),
		WithTaskDone(func(t api.TaskInfo) { mu.Lock(); ended = append(ended, t); mu.Unlock() }),
	}}, replies...)
	t.Cleanup(f.eng.StopTasks)
	ctx := context.WithValue(context.Background(), runStateKey{}, &runState{sessionID: "s"})
	return f, ctx, &ended
}

func TestTaskRunsInTheBackground(t *testing.T) {
	sub := newGated()
	f, ctx, ended := taskEngine(t, sub, nil)
	started, err := f.eng.StartTask(ctx, "qa", "review the cart\nin detail")
	require.NoError(t, err)
	assert.Equal(t, "task-1", started.ID)
	assert.Equal(t, api.TaskRunning, started.State)
	assert.Equal(t, "review the cart", started.Prompt)

	assert.Len(t, f.eng.ListTasks([]string{"s"}), 1)
	assert.Empty(t, f.eng.ListTasks([]string{"other"}), "another session's task listed")
	_, _, err = f.eng.WaitTask(ctx, []string{"other"}, "task-1", 0)
	assert.ErrorIs(t, err, api.ErrUnknownTask)
	assert.Empty(t, f.eng.TakeTaskNotes("s"), "a note before it ended")

	sub.open()
	done, events, err := f.eng.WaitTask(ctx, []string{"s"}, "task-1", 10*time.Second)
	require.NoError(t, err)
	assert.Equal(t, api.TaskDone, done.State)
	assert.Equal(t, "review done\nno problems found", done.Result)
	assert.False(t, done.Ended.IsZero())
	assert.Equal(t, 1, done.Usage.Calls, "the task's own usage")
	assert.GreaterOrEqual(t, f.eng.Usage("s").Calls, 1, "its session's usage includes it")
	assert.Contains(t, events, "review done")

	notes := f.eng.TakeTaskNotes("s")
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "task-1 (qa) finished")
	assert.Contains(t, notes[0], "no problems found")
	assert.Empty(t, f.eng.TakeTaskNotes("s"), "a note is given once")
	require.Len(t, *ended, 1)
	assert.Equal(t, api.TaskDone, (*ended)[0].State)
}

func TestTasksEnd(t *testing.T) {
	cases := []struct {
		name  string
		cfg   func(*config.Config)
		act   func(e *Engine) error
		state string
		why   string
	}{
		{
			name:  "stopped",
			act:   func(e *Engine) error { _, err := e.StopTask([]string{"s"}, "task-1"); return err },
			state: api.TaskStopped, why: "stopped",
		},
		{
			name:  "past its time limit",
			cfg:   func(c *config.Config) { c.Tools.BackgroundAgentTimeout = "50ms" },
			state: api.TaskFailed, why: "time limit",
		},
		{
			name:  "the workspace closing",
			act:   func(e *Engine) error { e.StopTasks(); return nil },
			state: api.TaskStopped, why: "stopped",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, ctx, _ := taskEngine(t, newGated(), c.cfg) // never answers
			_, err := f.eng.StartTask(ctx, "qa", "review")
			require.NoError(t, err)
			if c.act != nil {
				require.NoError(t, c.act(f.eng))
			}
			got, _, err := f.eng.WaitTask(ctx, []string{"s"}, "task-1", 10*time.Second)
			require.NoError(t, err)
			assert.Equal(t, c.state, got.State)
			assert.Contains(t, got.Error, c.why)
			notes := f.eng.TakeTaskNotes("s")
			require.Len(t, notes, 1)
			assert.Contains(t, notes[0], "task-1 (qa)")
		})
	}
}

func TestTaskLimits(t *testing.T) {
	f, ctx, _ := taskEngine(t, newGated(), func(c *config.Config) { c.Tools.MaxBackgroundAgents = 1 })
	_, err := f.eng.StartTask(ctx, "qa", "one")
	require.NoError(t, err)
	_, err = f.eng.StartTask(ctx, "qa", "two")
	assert.ErrorContains(t, err, "max_background_agents")
	_, err = f.eng.StartTask(ctx, "nobody", "x")
	assert.ErrorContains(t, err, "not found")
	_, err = f.eng.StartTask(context.WithValue(ctx, taskKey{}, "task-1"), "qa", "nested")
	assert.ErrorContains(t, err, "can't start background tasks")
	_, err = f.eng.StartTask(context.Background(), "qa", "no session")
	assert.Error(t, err)
}

// A running turn hears about an ended task with its next tool result.
func TestTaskNoteReachesTheRunningTurn(t *testing.T) {
	sub := newGated()
	call := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_files", Args: map[string]any{}}}}}
	f, ctx, _ := taskEngine(t, sub, nil, call, textContent("ok"))
	_, err := f.eng.StartTask(ctx, "qa", "review")
	require.NoError(t, err)
	sub.open()
	_, _, err = f.eng.WaitTask(ctx, []string{"s"}, "task-1", 10*time.Second)
	require.NoError(t, err)

	got, err := functionResponses(t, f.eng, "s", "carry on")
	require.NoError(t, err)
	updates, ok := got["list_files"][TaskUpdatesKey].([]string)
	require.True(t, ok, "no task updates in %v", got["list_files"])
	require.Len(t, updates, 1)
	assert.True(t, strings.HasPrefix(updates[0], "task-1 (qa) finished"))
}

// A task's approval requests and questions wait for its session's people,
// who hear of them through Watch; the task goes on once answered.
func TestTaskAsksItsSession(t *testing.T) {
	cases := []struct {
		name   string
		call   *genai.FunctionCall
		answer func(e *Engine, r api.TaskRequest) error
		state  string
	}{
		{
			name: "an approval, allowed",
			call: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "made.txt", "content": "hi\n"}},
			answer: func(e *Engine, r api.TaskRequest) error {
				return e.AnswerRequest([]string{"s"}, r.ID, api.DecisionOnce, "")
			},
			state: api.TaskDone,
		},
		{
			name: "a question, answered",
			call: &genai.FunctionCall{Name: "ask_user_question", Args: map[string]any{"question": "Tabs or spaces?"}},
			answer: func(e *Engine, r api.TaskRequest) error {
				return e.AnswerRequest([]string{"s"}, r.ID, 0, "tabs")
			},
			state: api.TaskDone,
		},
		{
			name:   "stopped while it waits",
			call:   &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "made.txt", "content": "hi\n"}},
			answer: func(e *Engine, r api.TaskRequest) error { _, err := e.StopTask([]string{"s"}, r.TaskID); return err },
			state:  api.TaskStopped,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sub := NewMockLLM("qa-model", &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: c.call}}}, textContent("finished"))
			f, ctx, _ := taskEngine(t, sub, func(cfg *config.Config) { cfg.Blitz.AutoApprove = false })
			watchCtx, stop := context.WithCancel(context.Background())
			defer stop()
			events := f.eng.Watch(watchCtx, []string{"s"})
			_, err := f.eng.StartTask(ctx, "qa", "make a file")
			require.NoError(t, err)

			var req api.TaskRequest
			waiting := false
			for req.ID == "" || !waiting {
				select {
				case ev := <-events:
					if ev.Request != nil {
						req = *ev.Request
					}
					if ev.Task != nil && ev.Task.State == api.TaskWaiting {
						waiting = true
					}
				case <-time.After(10 * time.Second):
					t.Fatal("the task didn't ask")
				}
			}
			assert.Equal(t, "qa · task-1", req.Label())
			assert.Equal(t, []api.TaskRequest{req}, f.eng.PendingRequests([]string{"s"}))
			assert.Empty(t, f.eng.PendingRequests([]string{"other"}))
			assert.ErrorIs(t, f.eng.AnswerRequest([]string{"other"}, req.ID, api.DecisionOnce, ""), api.ErrUnknownRequest)

			require.NoError(t, c.answer(f.eng, req))
			got, _, err := f.eng.WaitTask(ctx, []string{"s"}, "task-1", 10*time.Second)
			require.NoError(t, err)
			assert.Equal(t, c.state, got.State, "%+v", got)
			assert.Empty(t, f.eng.PendingRequests(nil), "a request still waits")
		})
	}
}
