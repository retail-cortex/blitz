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
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Workflow runs' receipts (spec_workflows_043 WF-19): every run, wherever
// it was started, listed and followed by its receipt, so a client shows
// what's running and what each run did and spent.

var (
	// watchRunEvery is how often a run's watch looks for changes.
	watchRunEvery = 300 * time.Millisecond
	// recentRuns is how long an ended run stays in WatchWorkflowRuns.
	recentRuns = 10 * time.Minute
)

func runStepMsg(s engine.StepReceipt) *pb.WorkflowRunStep {
	m := &pb.WorkflowRunStep{Id: s.ID, Title: s.Title, Type: s.Type, Status: s.Status, Attempts: int32(s.Attempts), Agent: s.Agent,
		Models: s.Models, Usage: usageMsg(s.Usage), Output: s.Output, Stream: s.Stream, Error: s.Error}
	if !s.Started.IsZero() {
		m.Started = timestamppb.New(s.Started)
	}
	if !s.Ended.IsZero() {
		m.Ended = timestamppb.New(s.Ended)
	}
	return m
}

// runMsg is a receipt for the wire, with the background run following it
// (if one does).
func (s *Server) runMsg(r engine.FlowReceipt) *pb.WorkflowRun {
	m := &pb.WorkflowRun{Id: r.Run, Workspace: r.Workspace, Workflow: r.Workflow, Origin: r.Origin, SessionId: r.Session, Input: r.Input,
		Started: timestamppb.New(r.Started), Status: r.Status, Error: r.Error, Output: r.Output, Usage: usageMsg(r.Usage), Models: r.Models}
	if !r.Ended.IsZero() {
		m.Ended = timestamppb.New(r.Ended)
	}
	for _, st := range r.Steps {
		m.Steps = append(m.Steps, runStepMsg(st))
	}
	for _, e := range r.Events {
		m.Events = append(m.Events, &pb.WorkflowRunEvent{At: timestamppb.New(e.At), Step: e.Step, Kind: e.Kind, Text: e.Text, Error: e.Error})
	}
	if r.Running() {
		if bg := s.backgroundFor(r.Run); bg != nil {
			info := bg.info(s.broker)
			m.BackgroundId, m.Approvals = info.Id, info.Waiting
		}
	}
	return m
}

// backgroundFor is the background run running workflow run id (the
// latest, a resumed run's), if one is.
func (s *Server) backgroundFor(id string) *bgRun {
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	for i := len(s.runs.list) - 1; i >= 0; i-- {
		r := s.runs.list[i]
		r.mu.Lock()
		match := r.flowRun == id && r.state == "running"
		r.mu.Unlock()
		if match {
			return r
		}
	}
	return nil
}

// receipts are the runs' receipts in every storage dir, newest first,
// once each.
func (s *Server) receipts(list func(dir string) []engine.FlowReceipt) []engine.FlowReceipt {
	seen := map[string]bool{}
	var out []engine.FlowReceipt
	for _, d := range s.storageDirs() {
		for _, r := range list(d) {
			if !seen[r.Run] {
				seen[r.Run] = true
				out = append(out, r)
			}
		}
	}
	slices.SortFunc(out, func(a, b engine.FlowReceipt) int { return b.Started.Compare(a.Started) })
	return out
}

// receipt finds one run's receipt in a storage dir, with its version.
func (s *Server) receipt(workspace, id string) (engine.FlowReceipt, int, error) {
	for _, d := range s.storageDirs() {
		r, ver, err := engine.FlowReceiptOf(d, id)
		if err == nil && (workspace == "" || r.Workspace == workspace) {
			return r, ver, nil
		}
	}
	return engine.FlowReceipt{}, 0, apiError(connect.CodeNotFound, "RUN_NOT_FOUND", fmt.Errorf("%w: %q", engine.ErrNoRun, id), "id", id)
}

func (h workspaceService) ListWorkflowRuns(ctx context.Context, r req[pb.ListWorkflowRunsRequest]) (*connect.Response[pb.ListWorkflowRunsResponse], error) {
	limit := int(r.Msg.Limit)
	if limit <= 0 {
		limit = 50
	}
	all := h.s.receipts(func(d string) []engine.FlowReceipt { return engine.FlowReceipts(d, r.Msg.Workspace, r.Msg.Workflow) })
	out := &pb.ListWorkflowRunsResponse{}
	for _, rec := range all[:min(limit, len(all))] {
		out.Runs = append(out.Runs, h.s.runMsg(rec.Summary()))
	}
	return ok(out)
}

func (h workspaceService) GetWorkflowRun(ctx context.Context, r req[pb.GetWorkflowRunRequest]) (*connect.Response[pb.GetWorkflowRunResponse], error) {
	rec, _, err := h.s.receipt(r.Msg.Workspace, r.Msg.Id)
	if err != nil {
		return nil, err
	}
	return ok(&pb.GetWorkflowRunResponse{Run: h.s.runMsg(rec)})
}

func (h workspaceService) WatchWorkflowRun(ctx context.Context, r req[pb.WatchWorkflowRunRequest], stream *connect.ServerStream[pb.WatchWorkflowRunResponse]) error {
	last := ""
	tick := time.NewTicker(watchRunEvery)
	defer tick.Stop()
	for {
		rec, ver, err := h.s.receipt(r.Msg.Workspace, r.Msg.Id)
		if err != nil {
			return err
		}
		msg := h.s.runMsg(rec)
		// A live run's version changes with it; one read from disk by
		// what's there.
		sig := fmt.Sprint(ver, rec.Status, len(rec.Events), len(rec.Steps), msg.Approvals, msg.BackgroundId)
		if sig != last {
			last = sig
			if err := stream.Send(&pb.WatchWorkflowRunResponse{Run: msg}); err != nil {
				return err
			}
		}
		if !rec.Running() {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// activeRuns are the runs going, waiting, or ended lately (summaries).
func (s *Server) activeRuns(workspace string) []*pb.WorkflowRun {
	since := time.Now().Add(-recentRuns)
	var out []*pb.WorkflowRun
	for _, rec := range s.receipts(func(d string) []engine.FlowReceipt { return engine.RecentFlowReceipts(d, workspace, since) }) {
		if rec.Running() || rec.Status == engine.RunWaiting || rec.Ended.After(since) {
			out = append(out, s.runMsg(rec.Summary()))
		}
	}
	return out
}

func (h workspaceService) WatchWorkflowRuns(ctx context.Context, r req[pb.WatchWorkflowRunsRequest], stream *connect.ServerStream[pb.WatchWorkflowRunsResponse]) error {
	last := "-" // the first list goes, empty or not
	tick := time.NewTicker(watchTasksEvery)
	defer tick.Stop()
	for {
		runs := h.s.activeRuns(r.Msg.Workspace)
		// What a client shows changing: each run's state, its steps' and
		// the approvals it waits on.
		var b strings.Builder
		for _, m := range runs {
			fmt.Fprintf(&b, "%s:%s:%d|", m.Id, m.Status, m.Approvals)
			for _, st := range m.Steps {
				b.WriteString(st.Status[:1])
			}
		}
		if sig := b.String(); sig != last {
			last = sig
			if err := stream.Send(&pb.WatchWorkflowRunsResponse{Runs: runs}); err != nil {
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

func (h workspaceService) StopWorkflowRun(ctx context.Context, r req[pb.StopWorkflowRunRequest]) (*connect.Response[pb.StopWorkflowRunResponse], error) {
	rec, _, err := h.s.receipt(r.Msg.Workspace, r.Msg.Id)
	if err == nil && rec.Running() && engine.StopFlowRun(rec.Run) {
		return ok(&pb.StopWorkflowRunResponse{})
	}
	if err == nil {
		err = apiError(connect.CodeNotFound, "RUN_NOT_FOUND", errors.New("the workflow run isn't running"), "id", r.Msg.Id)
	}
	return nil, err
}
