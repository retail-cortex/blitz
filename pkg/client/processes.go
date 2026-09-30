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

package client

import (
	"context"
	"slices"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// pollEvery is how often WaitAll asks whether the processes have finished.
var pollEvery = 500 * time.Millisecond

// callTimeout bounds the calls made while the client exits.
const callTimeout = 10 * time.Second

// ranIn notes that this client ran a turn in session id.
func (r *Remote) ranIn(id string) {
	if id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !slices.Contains(r.ran, id) {
		r.ran = append(r.ran, id)
	}
}

// sessionsRan are the sessions this client ran turns in.
func (r *Remote) sessionsRan() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.ran)
}

// Processes are the service's background processes started in this
// client's turns (WorkspaceService.ListProcesses and KillProcess): the
// ones to wait for or stop when it exits. Others' stay.
func (r *Remote) Processes() api.Processes { return remoteProcesses{r} }

// AuditShell records a command the user ran here directly in the
// workspace's audit log (WorkspaceService.AuditShell).
func (r *Remote) AuditShell(command string, exitCode int, startErr error) {
	msg := &pb.AuditShellRequest{Workspace: r.dir, Command: command, ExitCode: int32(exitCode)}
	if startErr != nil {
		msg.StartError = startErr.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if _, err := r.workspaces.AuditShell(ctx, connect.NewRequest(msg)); err != nil {
		r.failed("recording the command in the audit log", err)
	}
}

// remoteProcesses is api.Processes over the service.
type remoteProcesses struct{ r *Remote }

// Running are the processes of this client's sessions still running.
func (p remoteProcesses) Running() []api.ProcessInfo {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	running, err := p.running(ctx)
	if err != nil {
		p.r.failed("listing background processes", err)
	}
	return running
}

func (p remoteProcesses) running(ctx context.Context) ([]api.ProcessInfo, error) {
	sessions := p.r.sessionsRan()
	if len(sessions) == 0 {
		return nil, nil
	}
	res, err := p.r.workspaces.ListProcesses(ctx, connect.NewRequest(&pb.ListProcessesRequest{Workspace: p.r.dir, SessionIds: sessions}))
	if err != nil {
		return nil, err
	}
	var out []api.ProcessInfo
	for _, m := range res.Msg.Processes {
		if m.Running {
			out = append(out, processInfo(m))
		}
	}
	return out, nil
}

// WaitAll waits until none of them runs, or ctx ends.
func (p remoteProcesses) WaitAll(ctx context.Context) error {
	for {
		running, err := p.running(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return fromAPI(err)
		}
		if len(running) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// Shutdown stops the ones still running.
func (p remoteProcesses) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	running, err := p.running(ctx)
	if err != nil {
		p.r.failed("listing background processes", err)
		return
	}
	sessions := p.r.sessionsRan()
	for _, proc := range running {
		if _, err := p.r.workspaces.KillProcess(ctx, connect.NewRequest(&pb.KillProcessRequest{Workspace: p.r.dir, SessionIds: sessions, Id: int32(proc.ID)})); err != nil {
			p.r.failed("stopping a background process", err)
		}
	}
}

func processInfo(m *pb.Process) api.ProcessInfo {
	return api.ProcessInfo{ID: int(m.Id), Command: m.Command, Running: m.Running, ExitCode: int(m.ExitCode), RuntimeMs: m.RuntimeMs}
}

// ListTasks are the service's background tasks started in this client's
// turns (WorkspaceService.ListTasks), oldest first.
func (r *Remote) ListTasks() []api.TaskInfo {
	sessions := r.sessionsRan()
	if len(sessions) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	res, err := r.workspaces.ListTasks(ctx, connect.NewRequest(&pb.ListTasksRequest{Workspace: r.dir, SessionIds: sessions}))
	if err != nil {
		r.failed("listing background tasks", err)
		return nil
	}
	out := make([]api.TaskInfo, 0, len(res.Msg.Tasks))
	for _, t := range res.Msg.Tasks {
		out = append(out, taskInfo(t))
	}
	return out
}

// Task is one of this client's tasks with its latest events
// (WorkspaceService.GetTask).
func (r *Remote) Task(id string) (api.TaskInfo, []string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	res, err := r.workspaces.GetTask(ctx, connect.NewRequest(&pb.GetTaskRequest{Workspace: r.dir, SessionIds: r.sessionsRan(), Id: id}))
	if err != nil {
		return api.TaskInfo{}, nil, fromAPI(err)
	}
	return taskInfo(res.Msg.Task), res.Msg.Events, nil
}

// StopTask stops one of this client's tasks (WorkspaceService.StopTask).
func (r *Remote) StopTask(id string) (api.TaskInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	res, err := r.workspaces.StopTask(ctx, connect.NewRequest(&pb.StopTaskRequest{Workspace: r.dir, SessionIds: r.sessionsRan(), Id: id}))
	if err != nil {
		return api.TaskInfo{}, fromAPI(err)
	}
	return taskInfo(res.Msg.Task), nil
}

func taskInfo(m *pb.BackgroundTask) api.TaskInfo {
	if m == nil {
		return api.TaskInfo{}
	}
	return api.TaskInfo{
		ID: m.Id, Agent: m.Agent, Prompt: m.Prompt, Session: m.SessionId, State: m.State,
		Started: timeOf(m.Started), Ended: timeOf(m.Ended), Result: m.Result, Error: m.Error, Usage: usage(m.Usage),
		Worktree: m.Worktree, Branch: m.Branch,
	}
}

// PendingTaskRequests are the approval requests and questions of this
// client's tasks waiting for an answer (WorkspaceService.ListTaskRequests).
func (r *Remote) PendingTaskRequests() []api.TaskRequest {
	sessions := r.sessionsRan()
	if len(sessions) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	res, err := r.workspaces.ListTaskRequests(ctx, connect.NewRequest(&pb.ListTaskRequestsRequest{Workspace: r.dir, SessionIds: sessions}))
	if err != nil {
		r.failed("listing background tasks' requests", err)
		return nil
	}
	var out []api.TaskRequest
	for _, ev := range res.Msg.Requests {
		switch k := ev.Kind.(type) {
		case *pb.TaskEvent_ApprovalRequest:
			a := k.ApprovalRequest
			out = append(out, api.TaskRequest{ID: a.RequestId, TaskID: a.TaskId, Agent: a.Agent, Approval: &api.ApprovalRequest{
				Tool: a.Tool, Kind: actionKind(a.Kind), Detail: a.Detail, Diff: a.Diff, KeyLabel: a.ScopeLabel,
			}})
		case *pb.TaskEvent_Question:
			q := k.Question
			out = append(out, api.TaskRequest{ID: q.RequestId, TaskID: q.TaskId, Agent: q.Agent, Question: q.Question, Options: q.Options})
		}
	}
	return out
}

// AnswerTaskRequest answers one of them (SessionService.Answer for a
// question, when answer is set; else Approve).
func (r *Remote) AnswerTaskRequest(id string, decision api.Decision, answer string) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var err error
	if answer != "" {
		_, err = r.sessions.Answer(ctx, connect.NewRequest(&pb.AnswerRequest{Workspace: r.dir, RequestId: id, Answer: answer}))
	} else {
		_, err = r.sessions.Approve(ctx, connect.NewRequest(&pb.ApproveRequest{Workspace: r.dir, RequestId: id, Decision: decisionMsg(decision)}))
	}
	return fromAPI(err)
}
