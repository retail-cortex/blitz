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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"google.golang.org/adk/v2/session"
)

// Background tasks: sub-agents invoke_agent starts with background: true,
// running beside the turn that started them, detached from it
// (spec_background_agents_032). A task runs as a background run
// (tools.Background): what the workspace's rules and mode allow runs, and
// anything that would ask the person is refused. Its tokens count for the
// session that started it and for itself. When it ends, a note for the
// main agent waits: it goes with the session's next tool result, or in
// front of its next prompt.

// TaskUpdatesKey is the tool-result field that carries notes about
// background tasks that have ended.
const TaskUpdatesKey = "task_updates"

// Limits of background tasks.
const (
	maxTaskEvents    = 200  // events kept per task
	maxTasksRetained = 16   // finished tasks kept per session
	maxNoteResult    = 2000 // characters of a result in its note
)

type (
	taskKey          struct{}
	subagentEventKey struct{}
)

// task is one background task.
type task struct {
	info   api.TaskInfo // guarded by taskManager.mu
	events []string     // guarded by taskManager.mu
	cancel context.CancelCauseFunc
	done   chan struct{}
}

// taskManager holds a workspace's tasks.
type taskManager struct {
	mu    sync.Mutex
	next  int
	tasks map[string]*task
	// notes are ended tasks' notes for their sessions' main agent, not
	// yet delivered.
	notes  map[string][]string
	onDone func(api.TaskInfo)
}

// errTaskStopped is why a stopped task ended.
var errTaskStopped = errors.New("stopped")

// WithTaskDone calls f when a background task ends (the workspace records
// it in the session's transcript).
func WithTaskDone(f func(api.TaskInfo)) Option {
	return func(e *Engine) { e.tasks.onDone = f }
}

func newTaskManager() *taskManager {
	return &taskManager{tasks: map[string]*task{}, notes: map[string][]string{}}
}

// StartTask starts agentName on prompt in the background, for the session
// whose run ctx is, and returns at once.
func (e *Engine) StartTask(ctx context.Context, agentName, prompt string) (api.TaskInfo, error) {
	st := stateFrom(ctx)
	if st == nil || st.sessionID == "" {
		return api.TaskInfo{}, errors.New("background tasks need a session")
	}
	if ctx.Value(taskKey{}) != nil {
		return api.TaskInfo{}, errors.New("a background task can't start background tasks")
	}
	if _, ok := e.agentReg.Get(agentName); !ok {
		return api.TaskInfo{}, fmt.Errorf("agent '%s' not found", agentName)
	}
	limit := e.cfg.Tools.MaxBackgroundAgents
	if limit <= 0 {
		limit = 4
	}
	timeout, err := time.ParseDuration(e.cfg.Tools.BackgroundAgentTimeout)
	if err != nil || timeout <= 0 {
		timeout = 30 * time.Minute
	}
	maxTurns := e.cfg.Tools.BackgroundAgentMaxTurns
	if maxTurns <= 0 {
		maxTurns = 50
	}

	m := e.tasks
	m.mu.Lock()
	running := 0
	for _, t := range m.tasks {
		if t.info.State == api.TaskRunning {
			running++
		}
	}
	if running >= limit {
		m.mu.Unlock()
		return api.TaskInfo{}, fmt.Errorf("%d background tasks are running already (tools.max_background_agents); wait for one or stop one", running)
	}
	m.next++
	id := "task-" + strconv.Itoa(m.next)
	first, _, _ := strings.Cut(strings.TrimSpace(prompt), "\n")
	t := &task{info: api.TaskInfo{ID: id, Agent: agentName, Prompt: textutil.Ellipsize(first, 200), Session: st.sessionID, State: api.TaskRunning, Started: time.Now()}, done: make(chan struct{})}
	m.tasks[id] = t
	m.pruneLocked(st.sessionID)
	m.mu.Unlock()

	// Detached from the turn: it keeps running when the turn ends. Its own
	// run state charges its session and has its own budget; nobody is
	// asked anything; the turn's notices aren't its.
	runCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	runCtx, stopTimer := context.WithTimeoutCause(runCtx, timeout, fmt.Errorf("its time limit (%s, tools.background_agent_timeout)", timeout))
	t.cancel = cancel
	own := &runState{sessionID: st.sessionID, maxTurns: maxTurns, taskID: id, agent: st.agent, model: st.model, models: st.models}
	runCtx = context.WithValue(runCtx, runStateKey{}, own)
	runCtx = context.WithValue(runCtx, taskKey{}, id)
	runCtx = context.WithValue(runCtx, turnNoticeKey{}, (func(string))(nil))
	runCtx = context.WithValue(runCtx, subagentEventKey{}, func(ev *session.Event) { e.taskEvent(t, ev) })
	runCtx = tools.Background(runCtx)

	go func() {
		defer stopTimer()
		result, err := e.InvokeSubagent(runCtx, agentName, prompt)
		e.endTask(runCtx, t, result, err)
	}()
	return e.taskInfo(t), nil
}

// taskEvent records one of a task's events, in words, and stops it once
// it has cost more than allowed.
func (e *Engine) taskEvent(t *task, ev *session.Event) {
	if ev.Partial || ev.Content == nil {
		return
	}
	var lines []string
	for _, p := range ev.Content.Parts {
		switch {
		case p.FunctionCall != nil:
			args, _ := json.Marshal(p.FunctionCall.Args)
			lines = append(lines, "→ "+p.FunctionCall.Name+" "+textutil.Ellipsize(string(args), 120))
		case p.FunctionResponse != nil:
			line := "← " + p.FunctionResponse.Name
			if msg, _ := p.FunctionResponse.Response["error"].(string); msg != "" {
				line += ": " + textutil.Ellipsize(msg, 120)
			}
			lines = append(lines, line)
		case p.Text != "" && !p.Thought:
			first, _, _ := strings.Cut(strings.TrimSpace(p.Text), "\n")
			if first != "" {
				lines = append(lines, textutil.Ellipsize(first, 160))
			}
		}
	}
	m := e.tasks
	m.mu.Lock()
	t.events = append(t.events, lines...)
	if over := len(t.events) - maxTaskEvents; over > 0 {
		t.events = slices.Delete(t.events, 0, over)
	}
	m.mu.Unlock()
	if limit := e.cfg.Tools.BackgroundAgentMaxCostUSD; limit > 0 && e.usage.Session(t.info.ID).CostUSD > limit {
		t.cancel(fmt.Errorf("its cost limit ($%.2f, tools.background_agent_max_cost_usd)", limit))
	}
}

// endTask records how task t ended, leaves a note for its session's main
// agent, and tells the workspace.
func (e *Engine) endTask(ctx context.Context, t *task, result string, err error) {
	m := e.tasks
	m.mu.Lock()
	t.info.Ended = time.Now()
	t.info.Result = strings.TrimSpace(result)
	switch cause := context.Cause(ctx); {
	case errors.Is(cause, errTaskStopped):
		t.info.State, t.info.Error = api.TaskStopped, "stopped"
	case cause != nil:
		t.info.State, t.info.Error = api.TaskFailed, "stopped by "+cause.Error()
	case err != nil:
		t.info.State, t.info.Error = api.TaskFailed, err.Error()
	default:
		t.info.State = api.TaskDone
	}
	info := t.info
	info.Usage = e.usage.Session(info.ID)
	m.notes[info.Session] = append(m.notes[info.Session], TaskNote(info))
	onDone := m.onDone
	m.mu.Unlock()
	if onDone != nil { // before waiters wake: the transcript has it by then
		onDone(info)
	}
	close(t.done)
}

// TaskNote is what the main agent is told when task t has ended.
func TaskNote(t api.TaskInfo) string {
	took := t.Runtime().Round(time.Second)
	switch t.State {
	case api.TaskDone:
		return fmt.Sprintf("%s (%s) finished in %s: %s\n(task_output %s has all of it.)", t.ID, t.Agent, took, textutil.Ellipsize(t.Result, maxNoteResult), t.ID)
	case api.TaskStopped:
		return fmt.Sprintf("%s (%s) was stopped after %s.", t.ID, t.Agent, took)
	}
	return fmt.Sprintf("%s (%s) failed after %s: %s", t.ID, t.Agent, took, t.Error)
}

// TakeTaskNotes removes and returns the notes about ended tasks waiting
// for sessionID's main agent.
func (e *Engine) TakeTaskNotes(sessionID string) []string {
	m := e.tasks
	m.mu.Lock()
	defer m.mu.Unlock()
	notes := m.notes[sessionID]
	delete(m.notes, sessionID)
	return notes
}

// taskInfo is t as it is now, with its usage.
func (e *Engine) taskInfo(t *task) api.TaskInfo {
	e.tasks.mu.Lock()
	info := t.info
	e.tasks.mu.Unlock()
	info.Usage = e.usage.Session(info.ID)
	return info
}

// ListTasks are the tasks of sessions (nil: every session), oldest first.
func (e *Engine) ListTasks(sessions []string) []api.TaskInfo {
	m := e.tasks
	m.mu.Lock()
	var list []*task
	for _, t := range m.tasks {
		if sessions == nil || slices.Contains(sessions, t.info.Session) {
			list = append(list, t)
		}
	}
	m.mu.Unlock()
	out := make([]api.TaskInfo, 0, len(list))
	for _, t := range list {
		out = append(out, e.taskInfo(t))
	}
	slices.SortFunc(out, func(a, b api.TaskInfo) int { return taskNumber(a.ID) - taskNumber(b.ID) })
	return out
}

func taskNumber(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "task-"))
	return n
}

// get is task id, when one of sessions (nil: any) started it.
func (e *Engine) get(sessions []string, id string) (*task, error) {
	e.tasks.mu.Lock()
	defer e.tasks.mu.Unlock()
	t, ok := e.tasks.tasks[id]
	if !ok || sessions != nil && !slices.Contains(sessions, t.info.Session) {
		return nil, fmt.Errorf("%w: %s", api.ErrUnknownTask, id)
	}
	return t, nil
}

// WaitTask is task id with its latest events, once it has ended or wait
// has passed.
func (e *Engine) WaitTask(ctx context.Context, sessions []string, id string, wait time.Duration) (api.TaskInfo, []string, error) {
	t, err := e.get(sessions, id)
	if err != nil {
		return api.TaskInfo{}, nil, err
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-t.done:
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
	}
	e.tasks.mu.Lock()
	events := slices.Clone(t.events)
	e.tasks.mu.Unlock()
	return e.taskInfo(t), events, nil
}

// StopTask stops task id and waits a few seconds for it to end.
func (e *Engine) StopTask(sessions []string, id string) (api.TaskInfo, error) {
	t, err := e.get(sessions, id)
	if err != nil {
		return api.TaskInfo{}, err
	}
	t.cancel(errTaskStopped)
	select {
	case <-t.done:
	case <-time.After(5 * time.Second):
	}
	return e.taskInfo(t), nil
}

// StopTasks stops every running task and waits for them (the workspace
// is closing).
func (e *Engine) StopTasks() {
	e.tasks.mu.Lock()
	var running []*task
	for _, t := range e.tasks.tasks {
		if t.info.State == api.TaskRunning {
			running = append(running, t)
		}
	}
	e.tasks.mu.Unlock()
	for _, t := range running {
		t.cancel(errTaskStopped)
	}
	for _, t := range running {
		select {
		case <-t.done:
		case <-time.After(5 * time.Second):
		}
	}
}

// pruneLocked drops session's oldest ended tasks beyond the ones kept.
func (m *taskManager) pruneLocked(session string) {
	var ended []*task
	for _, t := range m.tasks {
		if t.info.Session == session && t.info.State != api.TaskRunning {
			ended = append(ended, t)
		}
	}
	if len(ended) <= maxTasksRetained {
		return
	}
	slices.SortFunc(ended, func(a, b *task) int { return taskNumber(a.info.ID) - taskNumber(b.info.ID) })
	for _, t := range ended[:len(ended)-maxTasksRetained] {
		delete(m.tasks, t.info.ID)
	}
}
