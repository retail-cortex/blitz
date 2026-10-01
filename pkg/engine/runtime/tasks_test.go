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
	"fmt"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
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
	started, err := f.eng.StartTask(ctx, "qa", "review the cart\nin detail", "")
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
			_, err := f.eng.StartTask(ctx, "qa", "review", "")
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
	_, err := f.eng.StartTask(ctx, "qa", "one", "")
	require.NoError(t, err)
	_, err = f.eng.StartTask(ctx, "qa", "two", "")
	assert.ErrorContains(t, err, "max_background_agents")
	_, err = f.eng.StartTask(ctx, "nobody", "x", "")
	assert.ErrorContains(t, err, "not found")
	_, err = f.eng.StartTask(context.WithValue(ctx, taskKey{}, "task-1"), "qa", "nested", "")
	assert.ErrorContains(t, err, "can't start background tasks")
	_, err = f.eng.StartTask(context.Background(), "qa", "no session", "")
	assert.Error(t, err)
}

// A running turn hears about an ended task with its next tool result.
func TestTaskNoteReachesTheRunningTurn(t *testing.T) {
	sub := newGated()
	call := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_files", Args: map[string]any{}}}}}
	f, ctx, _ := taskEngine(t, sub, nil, call, textContent("ok"))
	_, err := f.eng.StartTask(ctx, "qa", "review", "")
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
			_, err := f.eng.StartTask(ctx, "qa", "make a file", "")
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

// agentFile writes an agent with frontmatter front into dir and loads it.
func agentFile(t *testing.T, e *Engine, dir, name, front string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\nname: "+name+"\ndescription: test\ntools: [create_file, list_files]\n"+front+"\n---\nDo it."), 0o644))
	require.NoError(t, e.agentReg.LoadExternalAgents(dir))
}

// An agent's frontmatter sets its own mode and budget through invoke_agent.
func TestAgentRunDefaults(t *testing.T) {
	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "made.txt", "content": "hi\n"}}}}}
	list := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "list_files", Args: map[string]any{}}}}}
	cases := []struct {
		name    string
		front   string
		inside  bool // the project's own agent
		replies []*genai.Content
		created bool
		err     error
	}{
		{name: "accept-edits, the user's agent", front: "permission_mode: accept-edits", replies: []*genai.Content{create, textContent("ok")}, created: true},
		{name: "accept-edits, an untrusted project's agent", front: "permission_mode: accept-edits", inside: true, replies: []*genai.Content{create, textContent("ok")}},
		{name: "plan", front: "permission_mode: plan", replies: []*genai.Content{create, textContent("ok")}},
		{name: "its own budget", front: "max_turns: 2", replies: []*genai.Content{list, list, list, list}, err: api.ErrMaxTurns},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sub := NewMockLLM("sub", c.replies...)
			f := newEngineWith(t, fixtureOpts{
				cfg:  func(cfg *config.Config) { cfg.Blitz.AutoApprove = false },
				opts: []Option{WithAgentModel("worker", sub)},
			})
			dir := t.TempDir()
			if c.inside {
				dir = filepath.Join(f.cfg.Tools.WorkspaceDir, "agents")
			}
			agentFile(t, f.eng, dir, "worker", c.front)
			ctx := context.WithValue(context.Background(), runStateKey{}, &runState{sessionID: "s", maxTurns: 50})
			_, err := f.eng.InvokeSubagent(ctx, "worker", "make a file")
			if c.err != nil {
				assert.ErrorIs(t, err, c.err)
				return
			}
			require.NoError(t, err)
			_, statErr := os.Stat(filepath.Join(f.cfg.Tools.WorkspaceDir, "made.txt"))
			assert.Equal(t, c.created, statErr == nil, "made.txt created: %v", statErr == nil)
		})
	}
}

// An agent whose frontmatter says background: true runs in the
// background unless the call says otherwise.
func TestAgentBackgroundByDefault(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		task bool
	}{
		{name: "the agent's default", args: map[string]any{"agent_name": "worker", "prompt": "review"}, task: true},
		{name: "the call says no", args: map[string]any{"agent_name": "worker", "prompt": "review", "background": false}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			invoke := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "invoke_agent", Args: c.args}}}}
			f := newEngineWith(t, fixtureOpts{opts: []Option{WithAgentModel("worker", NewMockLLM("sub"))}}, invoke, textContent("ok"))
			t.Cleanup(f.eng.StopTasks)
			agentFile(t, f.eng, t.TempDir(), "worker", "background: true")
			got, err := functionResponses(t, f.eng, "s", "go")
			require.NoError(t, err)
			_, isTask := got["invoke_agent"]["task_id"]
			assert.Equal(t, c.task, isTask, "%v", got["invoke_agent"])
		})
	}
}

// A task isolated in a worktree changes the worktree's files, not the
// workspace's, and says where its branch is.
func TestTaskInItsOwnWorktree(t *testing.T) {
	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "made.txt", "content": "hi\n"}}}}}
	sub := NewMockLLM("sub", create, textContent("made it"))
	f, ctx, _ := taskEngine(t, sub, func(c *config.Config) {
		c.Blitz.AutoApprove = false
		c.Permissions.Allow = []string{"write(**)"}
		ws := c.Tools.WorkspaceDir
		for _, args := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "first"}} {
			cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
			cmd.Dir = ws
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
		}
	})
	started, err := f.eng.StartTask(ctx, "qa", "make a file", "worktree")
	require.NoError(t, err)
	require.NotEmpty(t, started.Worktree)
	assert.True(t, strings.HasPrefix(started.Branch, "blitz/task-1-"))

	done, _, err := f.eng.WaitTask(ctx, []string{"s"}, started.ID, 10*time.Second)
	require.NoError(t, err)
	require.Equal(t, api.TaskDone, done.State, "%+v", done)
	_, err = os.Stat(filepath.Join(started.Worktree, "made.txt"))
	assert.NoError(t, err, "not made in the worktree")
	_, err = os.Stat(filepath.Join(f.cfg.Tools.WorkspaceDir, "made.txt"))
	assert.True(t, os.IsNotExist(err), "made in the workspace itself")
	assert.Contains(t, TaskNote(done), "on the branch "+started.Branch)

	_, err = f.eng.StartTask(ctx, "qa", "x", "worktree")
	require.NoError(t, err, "a second isolated task")
}

func TestTaskWorktreeNeedsARepository(t *testing.T) {
	f, ctx, _ := taskEngine(t, newGated(), nil)
	_, err := f.eng.StartTask(ctx, "qa", "x", "worktree")
	assert.ErrorContains(t, err, "not in a git repository")
	assert.Empty(t, f.eng.ListTasks(nil), "a task left behind")
}

// A task's events read as lines (calls, results with their errors, the
// first line of text), keep only the latest, and stop the task once it
// costs more than allowed.
func TestTaskEventLines(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{cfg: func(c *config.Config) {
		c.Tools.BackgroundAgentMaxCostUSD = 0.01
		c.Pricing = map[string]config.ModelPrice{"m": {InputPerMTok: 1_000_000}}
	}})
	var cause error
	tk := &task{info: api.TaskInfo{ID: "task-9"}, cancel: func(err error) { cause = err }}
	f.eng.taskEvent(tk, &session.Event{LLMResponse: model.LLMResponse{Partial: true, Content: textContent("partial")}})
	assert.Empty(t, tk.events, "partial events aren't recorded")
	f.eng.taskEvent(tk, &session.Event{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{
		{FunctionCall: &genai.FunctionCall{Name: "grep", Args: map[string]any{"q": "x"}}},
		{FunctionResponse: &genai.FunctionResponse{Name: "grep", Response: map[string]any{"error": "bad pattern"}}},
		{Text: "thinking", Thought: true},
		{Text: "\n  found it\nmore"},
	}}}})
	assert.Equal(t, []string{`→ grep {"q":"x"}`, "← grep: bad pattern", "found it"}, tk.events)
	assert.NoError(t, cause, "under the cost limit")

	for range maxTaskEvents {
		f.eng.taskEvent(tk, &session.Event{LLMResponse: model.LLMResponse{Content: textContent("line")}})
	}
	assert.Len(t, tk.events, maxTaskEvents)

	f.eng.usage.Record("task-9", "m", &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10})
	f.eng.taskEvent(tk, &session.Event{LLMResponse: model.LLMResponse{Content: textContent("costly")}})
	assert.ErrorContains(t, cause, "cost limit")
}

// Only the latest ended tasks of a session are kept; running ones stay.
func TestTaskPruning(t *testing.T) {
	m := newTaskManager()
	for i := 1; i <= maxTasksRetained+3; i++ {
		id := fmt.Sprintf("task-%d", i)
		m.tasks[id] = &task{info: api.TaskInfo{ID: id, Session: "s", State: api.TaskDone}}
	}
	m.tasks["task-1"].info.State = api.TaskRunning
	m.tasks["other"] = &task{info: api.TaskInfo{ID: "other", Session: "x", State: api.TaskDone}}
	m.pruneLocked("s")
	assert.Len(t, m.tasks, maxTasksRetained+2)
	assert.Contains(t, m.tasks, "task-1", "running")
	assert.NotContains(t, m.tasks, "task-2", "oldest ended")
	assert.NotContains(t, m.tasks, "task-3")
	assert.Contains(t, m.tasks, "task-5")
	assert.Equal(t, 0, taskNumber("other"))
}

// Waiting on a running task returns when the wait is over, and stopping an
// unknown task fails.
func TestWaitTaskTimesOut(t *testing.T) {
	sub := newGated()
	f, ctx, _ := taskEngine(t, sub, nil)
	_, err := f.eng.StartTask(ctx, "qa", "review", "")
	require.NoError(t, err)
	info, _, err := f.eng.WaitTask(ctx, []string{"s"}, "task-1", 10*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, api.TaskRunning, info.State)
	_, err = f.eng.StopTask([]string{"s"}, "task-404")
	assert.ErrorIs(t, err, api.ErrUnknownTask)
	sub.open()
}
