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
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"github.com/retail-cortex/blitz/pkg/worktree"
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
	isolatedToolsKey struct{}
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
	seq   int // numbers requests
	tasks map[string]*task
	// notes are ended tasks' notes for their sessions' main agent, not
	// yet delivered.
	notes  map[string][]string
	onDone func(api.TaskInfo)
	// requests are tasks' approval requests and questions waiting for an
	// answer, by ID; watchers hear about tasks outside turns.
	requests map[string]*taskRequest
	watchers map[*watcher]bool
}

// taskRequest is a task's approval request or question, waiting.
type taskRequest struct {
	info  api.TaskRequest
	reply chan taskReply
}

// taskReply answers a taskRequest.
type taskReply struct {
	decision api.Decision
	text     string
}

// watcher receives the events of some sessions' tasks.
type watcher struct {
	sessions []string
	ch       chan api.SessionEvent
}

// errTaskStopped is why a stopped task ended.
var errTaskStopped = errors.New("stopped")

// WithTaskDone calls f when a background task ends (the workspace records
// it in the session's transcript).
func WithTaskDone(f func(api.TaskInfo)) Option {
	return func(e *Engine) { e.tasks.onDone = f }
}

func newTaskManager() *taskManager {
	return &taskManager{tasks: map[string]*task{}, notes: map[string][]string{}, requests: map[string]*taskRequest{}, watchers: map[*watcher]bool{}}
}

// publishLocked tells the watchers of session about ev. A watcher too
// slow to take it misses it (it reads the pending requests again when it
// reconnects); m.mu must be held.
func (m *taskManager) publishLocked(session string, ev api.SessionEvent) {
	for w := range m.watchers {
		if slices.Contains(w.sessions, session) {
			select {
			case w.ch <- ev:
			default:
			}
		}
	}
}

// Watch sends the events of sessions' tasks until ctx ends: tasks
// starting, waiting, running again and ending, requests waiting for an
// answer, and requests no longer waiting.
func (e *Engine) Watch(ctx context.Context, sessions []string) <-chan api.SessionEvent {
	w := &watcher{sessions: slices.Clone(sessions), ch: make(chan api.SessionEvent, 64)}
	m := e.tasks
	m.mu.Lock()
	m.watchers[w] = true
	m.mu.Unlock()
	out := make(chan api.SessionEvent)
	go func() {
		defer close(out)
		defer func() {
			m.mu.Lock()
			delete(m.watchers, w)
			m.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-w.ch:
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

// PendingRequests are the requests of sessions' tasks (nil: every
// session's) waiting for an answer, oldest first.
func (e *Engine) PendingRequests(sessions []string) []api.TaskRequest {
	m := e.tasks
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []api.TaskRequest
	for _, r := range m.requests {
		if sessions == nil || slices.Contains(sessions, r.info.Session) {
			out = append(out, r.info)
		}
	}
	slices.SortFunc(out, func(a, b api.TaskRequest) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// AnswerRequest answers request id of one of sessions' tasks (nil: any):
// decision for an approval, text for a question.
func (e *Engine) AnswerRequest(sessions []string, id string, decision api.Decision, text string) error {
	m := e.tasks
	m.mu.Lock()
	r, ok := m.requests[id]
	if !ok || sessions != nil && !slices.Contains(sessions, r.info.Session) {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", api.ErrUnknownRequest, id)
	}
	delete(m.requests, id)
	m.mu.Unlock()
	r.reply <- taskReply{decision: decision, text: text}
	return nil
}

// askForTask puts task t's request to its session's people and waits for
// an answer, or for the task to end. The task is waiting meanwhile.
func (e *Engine) askForTask(ctx context.Context, t *task, req api.TaskRequest) (taskReply, error) {
	m := e.tasks
	m.mu.Lock()
	m.seq++
	req.ID = fmt.Sprintf("%s-%04d", t.info.ID, m.seq)
	req.TaskID, req.Agent, req.Session = t.info.ID, t.info.Agent, t.info.Session
	r := &taskRequest{info: req, reply: make(chan taskReply, 1)}
	m.requests[req.ID] = r
	t.info.State = api.TaskWaiting
	m.publishLocked(t.info.Session, api.SessionEvent{Request: &req})
	info := t.info
	m.publishLocked(t.info.Session, api.SessionEvent{Task: &info})
	m.mu.Unlock()

	var reply taskReply
	var err error
	select {
	case reply = <-r.reply:
	case <-ctx.Done():
		err = context.Cause(ctx)
	}
	m.mu.Lock()
	delete(m.requests, req.ID)
	if t.info.State == api.TaskWaiting {
		t.info.State = api.TaskRunning
	}
	m.publishLocked(t.info.Session, api.SessionEvent{Resolved: req.ID})
	info = t.info
	m.publishLocked(t.info.Session, api.SessionEvent{Task: &info})
	m.mu.Unlock()
	return reply, err
}

// StartTask starts agentName on prompt in the background, for the session
// whose run ctx is, and returns at once.
func (e *Engine) StartTask(ctx context.Context, agentName, prompt, isolation string) (api.TaskInfo, error) {
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
	if spec, _ := e.agentReg.Get(agentName); spec != nil && spec.MaxTurns > 0 {
		maxTurns = spec.MaxTurns // its own budget, as in the foreground
	}

	m := e.tasks
	m.mu.Lock()
	running := 0
	for _, t := range m.tasks {
		if t.info.State == api.TaskRunning || t.info.State == api.TaskWaiting {
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

	// A task isolated in a worktree gets its own tools there.
	var isolated *tools.Registry
	if isolation == "worktree" {
		reg, w, err := e.worktreeTools(id)
		if err != nil {
			m.mu.Lock()
			delete(m.tasks, id)
			m.mu.Unlock()
			return api.TaskInfo{}, fmt.Errorf("its worktree: %w", err)
		}
		isolated = reg
		m.mu.Lock()
		t.info.Worktree, t.info.Branch = w.Path, w.Branch
		m.mu.Unlock()
	}
	m.mu.Lock()
	started := t.info
	m.publishLocked(st.sessionID, api.SessionEvent{Task: &started})
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
	if isolated != nil {
		runCtx = context.WithValue(runCtx, isolatedToolsKey{}, isolated)
	}
	runCtx = tools.WithTaskAsker(runCtx, tools.TaskAsker{
		Approve: func(ctx context.Context, req api.ApprovalRequest) (api.Decision, error) {
			r, err := e.askForTask(ctx, t, api.TaskRequest{Approval: &req})
			if err != nil {
				return api.DecisionDeny, err
			}
			return r.decision, nil
		},
		Ask: func(ctx context.Context, question string, options []string) (string, error) {
			r, err := e.askForTask(ctx, t, api.TaskRequest{Question: question, Options: options, MultiSelect: api.IsMultiSelect(ctx)})
			return r.text, err
		},
	})

	go func() {
		defer stopTimer()
		if isolated != nil {
			defer isolated.Close()
		}
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
	m.publishLocked(info.Session, api.SessionEvent{Task: &info})
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
	where := ""
	if t.Worktree != "" {
		where = fmt.Sprintf("\nIts changes are in the worktree %s, on the branch %s: review them, merge the branch, then blitz worktrees remove %s.", t.Worktree, t.Branch, filepath.Base(t.Worktree))
	}
	switch t.State {
	case api.TaskDone:
		return fmt.Sprintf("%s (%s) finished in %s: %s\n(task_output %s has all of it.)", t.ID, t.Agent, took, textutil.Ellipsize(t.Result, maxNoteResult), t.ID) + where
	case api.TaskStopped:
		return fmt.Sprintf("%s (%s) was stopped after %s.", t.ID, t.Agent, took) + where
	}
	return fmt.Sprintf("%s (%s) failed after %s: %s", t.ID, t.Agent, took, t.Error) + where
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
		if t.info.State == api.TaskRunning || t.info.State == api.TaskWaiting {
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
		if t.info.Session == session && t.info.State != api.TaskRunning && t.info.State != api.TaskWaiting {
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

// worktreeTools makes task id's worktree and a tools registry rooted in
// it: its files, sandbox and checkpoints are the worktree's. It shares the
// workspace's settings, but not its MCP servers or hooks, which the
// workspace's own run already provides.
func (e *Engine) worktreeTools(id string) (*tools.Registry, worktree.Worktree, error) {
	w, err := worktree.Create(e.toolReg.Workspace().Dir(), worktree.NewName(id+"-"), "")
	if err != nil {
		return nil, worktree.Worktree{}, err
	}
	cfg := *e.cfg
	cfg.Tools.WorkspaceDir = w.Path
	cfg.MCP.Servers = nil
	cfg.Hooks = config.HooksConfig{}
	reg, err := tools.NewRegistry(&cfg, e.agentReg, e.skillProv)
	if err != nil {
		return nil, w, err
	}
	_ = reg.SetPermissionMode(e.toolReg.Hooks().Mode())
	return reg, w, nil
}
