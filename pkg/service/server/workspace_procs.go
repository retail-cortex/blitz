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
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h workspaceService) ListCheckpoints(ctx context.Context, r req[pb.ListCheckpointsRequest]) (*connect.Response[pb.ListCheckpointsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListCheckpointsResponse{}
	for _, c := range w.ListCheckpoints() {
		out.Checkpoints = append(out.Checkpoints, &pb.Checkpoint{Id: int32(c.ID), Label: c.Label, Time: timestamppb.New(c.Time), Files: c.Files})
	}
	return ok(out)
}

func (h workspaceService) Undo(ctx context.Context, r req[pb.UndoRequest]) (*connect.Response[pb.UndoResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.Undo(r.Msg.SessionId, r.Msg.Force)
	// A conflict before anything was restored fails the call; anything
	// else is reported with what was restored.
	if err != nil && len(res.Restored) == 0 && errors.Is(err, api.ErrUndoConflict) {
		return nil, toAPI(err)
	}
	return ok(&pb.UndoResponse{Label: res.Label, Restored: res.Restored, Error: errorInfo(err)})
}

func (h workspaceService) GetDiff(ctx context.Context, r req[pb.GetDiffRequest]) (*connect.Response[pb.GetDiffResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if !r.Msg.Git {
		return ok(&pb.GetDiffResponse{Diff: w.SessionDiff()})
	}
	out, err := w.GitDiff(ctx, r.Msg.Color)
	if err != nil {
		return nil, apiError(connect.CodeFailedPrecondition, "GIT_FAILED", fmt.Errorf("%w: %s", err, out))
	}
	return ok(&pb.GetDiffResponse{Diff: out})
}

func (h workspaceService) ListApprovals(ctx context.Context, r req[pb.ListApprovalsRequest]) (*connect.Response[pb.ListApprovalsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListApprovalsResponse{}
	for _, a := range w.ListApprovals() {
		out.Approvals = append(out.Approvals, approvalMsg(a))
	}
	return ok(out)
}

func (h workspaceService) RevokeApprovals(ctx context.Context, r req[pb.RevokeApprovalsRequest]) (*connect.Response[pb.RevokeApprovalsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	var n int
	if r.Msg.All {
		n, err = w.ClearApprovals()
	} else {
		n, err = w.RevokeApprovals(r.Msg.Keys...)
	}
	return ok(&pb.RevokeApprovalsResponse{Revoked: int32(n), Error: errorInfo(err)})
}

func (h workspaceService) ListProcesses(ctx context.Context, r req[pb.ListProcessesRequest]) (*connect.Response[pb.ListProcessesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListProcessesResponse{}
	for _, p := range w.ListProcesses(r.Msg.SessionIds) {
		out.Processes = append(out.Processes, processMsg(p))
	}
	return ok(out)
}

func (h workspaceService) GetProcessOutput(ctx context.Context, r req[pb.GetProcessOutputRequest]) (*connect.Response[pb.GetProcessOutputResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	var text string
	var p api.ProcessInfo
	if r.Msg.AnySession {
		text, p, err = w.AnyProcessOutput(int(r.Msg.Id))
	} else {
		text, p, err = w.ProcessOutput(r.Msg.SessionIds, int(r.Msg.Id))
	}
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.GetProcessOutputResponse{Output: text, Process: processMsg(p)})
}

func (h workspaceService) KillProcess(ctx context.Context, r req[pb.KillProcessRequest]) (*connect.Response[pb.KillProcessResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	var p api.ProcessInfo
	if r.Msg.AnySession {
		p, err = w.StopProcess(int(r.Msg.Id))
	} else {
		p, err = w.KillProcess(r.Msg.SessionIds, int(r.Msg.Id))
	}
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.KillProcessResponse{Process: processMsg(p)})
}

func (h workspaceService) ListRunningProcesses(_ context.Context, _ req[pb.ListRunningProcessesRequest]) (*connect.Response[pb.ListRunningProcessesResponse], error) {
	return ok(&pb.ListRunningProcessesResponse{Processes: h.s.runningProcesses("")})
}

// runningProcesses are the background processes still running in the
// open workspaces (dir's alone, unless ""), by workspace then ID.
func (s *Server) runningProcesses(dir string) []*pb.WorkspaceProcess {
	if dir != "" {
		if real, err := canonical(dir); err == nil {
			dir = real
		}
	}
	s.mu.Lock()
	ws := make([]*workspace, 0, len(s.workspaces))
	for key, w := range s.workspaces {
		if dir == "" || key == dir {
			ws = append(ws, w)
		}
	}
	s.mu.Unlock()
	slices.SortFunc(ws, func(a, b *workspace) int { return strings.Compare(a.Dir(), b.Dir()) })
	var out []*pb.WorkspaceProcess
	for _, w := range ws {
		for _, p := range w.RunningProcesses() {
			out = append(out, &pb.WorkspaceProcess{Workspace: w.Dir(), Process: processMsg(p)})
		}
	}
	return out
}

// watchProcessesEvery is how often WatchProcesses looks for a change.
var watchProcessesEvery = 2 * time.Second

func (h workspaceService) WatchProcesses(ctx context.Context, r req[pb.WatchProcessesRequest], stream *connect.ServerStream[pb.WatchProcessesResponse]) error {
	last := "-" // the first list goes, empty or not
	tick := time.NewTicker(watchProcessesEvery)
	defer tick.Stop()
	for {
		procs := h.s.runningProcesses(r.Msg.Workspace)
		// What a client shows changing: which run, their flags, and their
		// usage to the percent and the megabyte; not their running time,
		// which a client counts itself.
		var b strings.Builder
		for _, wp := range procs {
			p := wp.Process
			fmt.Fprintf(&b, "%s:%d:%v:%.0f:%d|", wp.Workspace, p.Id, p.Flags, p.CpuPercent, p.MemoryBytes>>20)
		}
		if sig := b.String(); sig != last {
			last = sig
			if err := stream.Send(&pb.WatchProcessesResponse{Processes: procs, Listed: true}); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// RunShell runs a ! command for a client without a terminal, streaming
// its output (Workspace.RunUserShell); the call ending stops it.
func (h workspaceService) RunShell(ctx context.Context, r req[pb.RunShellRequest], stream *connect.ServerStream[pb.RunShellResponse]) error {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return err
	}
	var mu sync.Mutex
	var sendErr error
	send := func(m *pb.RunShellResponse) {
		mu.Lock()
		defer mu.Unlock()
		if sendErr == nil {
			sendErr = stream.Send(m)
		}
	}
	start := time.Now()
	code, truncated, startErr := w.RunUserShell(ctx, r.Msg.Command, func(p []byte) { send(&pb.RunShellResponse{Output: strings.ToValidUTF8(string(p), "\uFFFD")}) })
	end := &pb.RunShellResponse{Done: true, ExitCode: int32(code), ElapsedMs: time.Since(start).Milliseconds(), Truncated: truncated}
	if startErr != nil {
		end.Error = startErr.Error()
	}
	send(end)
	return sendErr
}

func (h workspaceService) AuditShell(ctx context.Context, r req[pb.AuditShellRequest]) (*connect.Response[pb.AuditShellResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	var startErr error
	if r.Msg.StartError != "" {
		startErr = errors.New(r.Msg.StartError)
	}
	w.AuditShell(r.Msg.Command, int(r.Msg.ExitCode), startErr)
	return ok(&pb.AuditShellResponse{})
}

func (h workspaceService) TakeProcessNotices(ctx context.Context, r req[pb.TakeProcessNoticesRequest]) (*connect.Response[pb.TakeProcessNoticesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	return ok(&pb.TakeProcessNoticesResponse{Notices: w.TakeProcessNotices(r.Msg.SessionId)})
}

func (h workspaceService) ListTasks(ctx context.Context, r req[pb.ListTasksRequest]) (*connect.Response[pb.ListTasksResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListTasksResponse{}
	for _, t := range w.ListTasksIn(r.Msg.SessionIds) {
		out.Tasks = append(out.Tasks, taskMsg(t))
	}
	return ok(out)
}

func (h workspaceService) GetTask(ctx context.Context, r req[pb.GetTaskRequest]) (*connect.Response[pb.GetTaskResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	t, events, err := w.TaskIn(scopedSessions(r.Msg.SessionIds), r.Msg.Id)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.GetTaskResponse{Task: taskMsg(t), Events: events})
}

func (h workspaceService) StopTask(ctx context.Context, r req[pb.StopTaskRequest]) (*connect.Response[pb.StopTaskResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	t, err := w.StopTaskIn(scopedSessions(r.Msg.SessionIds), r.Msg.Id)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.StopTaskResponse{Task: taskMsg(t)})
}

// scopedSessions are the sessions a request names: none is none, never
// every session.
func scopedSessions(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

func (h workspaceService) WatchAgentBrowser(ctx context.Context, r req[pb.WatchAgentBrowserRequest], stream *connect.ServerStream[pb.WatchAgentBrowserResponse]) error {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return err
	}
	return w.WatchAgentBrowser(ctx, func(c api.AgentBrowserCommand) error {
		return stream.Send(&pb.WatchAgentBrowserResponse{Command: &pb.AgentBrowserCommand{
			BrowserId: c.BrowserID, Kind: c.Kind, Message: c.Message, Show: c.Show, Profile: c.Profile, AllowLocal: c.AllowLocal, DenyDomains: c.DenyDomains, PrivateSites: c.PrivateSites, Url: c.URL,
			Path: c.Path, Line: int32(c.Line),
		}})
	})
}

func (h workspaceService) AgentBrowserReply(ctx context.Context, r req[pb.AgentBrowserReplyRequest]) (*connect.Response[pb.AgentBrowserReplyResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.AgentBrowserReply(r.Msg.BrowserId, r.Msg.Messages, r.Msg.Closed, r.Msg.Reason); err != nil {
		return nil, apiError(connect.CodeNotFound, "NO_AGENT_BROWSER", err)
	}
	return ok(&pb.AgentBrowserReplyResponse{})
}

func (h workspaceService) WatchTasks(ctx context.Context, r req[pb.WatchTasksRequest], stream *connect.ServerStream[pb.WatchTasksResponse]) error {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return err
	}
	sessions := scopedSessions(r.Msg.SessionIds)
	events := w.WatchTasks(ctx, sessions) // before the snapshot: nothing is missed between
	send := func(ev *pb.TaskEvent) error { return stream.Send(&pb.WatchTasksResponse{Event: ev}) }
	for _, t := range w.ListTasksIn(sessions) {
		if t.Active() {
			if err := send(&pb.TaskEvent{Kind: &pb.TaskEvent_Task{Task: taskMsg(t)}}); err != nil {
				return err
			}
		}
	}
	for _, rq := range w.PendingTaskRequestsIn(sessions) {
		if err := send(taskRequestMsg(rq)); err != nil {
			return err
		}
	}
	if err := send(&pb.TaskEvent{Kind: &pb.TaskEvent_Ready{Ready: true}}); err != nil {
		return err
	}
	settings := w.SettingsChanges(ctx)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if err := send(taskEventMsg(ev)); err != nil {
				return err
			}
		case <-settings:
			if err := send(&pb.TaskEvent{Kind: &pb.TaskEvent_SettingsChanged{SettingsChanged: true}}); err != nil {
				return err
			}
		}
	}
}

func (h workspaceService) ListTaskRequests(ctx context.Context, r req[pb.ListTaskRequestsRequest]) (*connect.Response[pb.ListTaskRequestsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListTaskRequestsResponse{}
	for _, rq := range w.PendingTaskRequestsIn(scopedSessions(r.Msg.SessionIds)) {
		out.Requests = append(out.Requests, taskRequestMsg(rq))
	}
	return ok(out)
}
