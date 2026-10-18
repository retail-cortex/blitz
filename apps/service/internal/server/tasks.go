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
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Paused workflow runs' tasks (spec_workflows_043 WF-16, WF-17).

// recordDirs are where paused runs are recorded: the default session
// storage, and each open workspace's.
func (s *Server) recordDirs() []string {
	var dirs []string
	for _, d := range s.storageDirs() {
		dirs = append(dirs, engine.FlowRecordsDir(d))
	}
	return dirs
}

// storageDirs are where sessions are kept: the default session storage,
// and each open workspace's.
func (s *Server) storageDirs() []string {
	dirs := []string{filepath.Join(config.ConfigDir(s.configDir), "sessions")}
	s.mu.Lock()
	for _, w := range s.workspaces {
		if d := w.StorageDir(); !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	s.mu.Unlock()
	return dirs
}

func humanTaskMsg(r engine.FlowRecord, t engine.FlowTask) *pb.HumanTask {
	m := &pb.HumanTask{Id: t.ID, Run: r.Run, Workspace: r.Workspace, Workflow: r.Workflow, Step: t.Step, Title: t.Title, Kind: t.Kind,
		Message: t.Message, Created: timestamppb.New(t.Created), SessionId: r.Session}
	if t.Form != nil {
		data, _ := json.Marshal(t.Form.Messages(t.ID))
		m.A2UiJson = string(data)
	}
	if !t.FireAt.IsZero() {
		m.FireAt = timestamppb.New(t.FireAt)
	}
	return m
}

func (h workspaceService) ListHumanTasks(ctx context.Context, r req[pb.ListHumanTasksRequest]) (*connect.Response[pb.ListHumanTasksResponse], error) {
	return ok(h.s.humanTasks(r.Msg.Workspace))
}

// humanTasks are the paused runs' tasks (one workspace's, or every one's),
// oldest first, with how the user wants to hear of them.
func (s *Server) humanTasks(workspace string) *pb.ListHumanTasksResponse {
	out := &pb.ListHumanTasksResponse{Alerts: "notify"}
	if cfg, err := config.Load(s.configDir); err == nil && cfg.UI.TaskAlerts != "" {
		out.Alerts = cfg.UI.TaskAlerts
	}
	seen := map[string]bool{}
	for _, dir := range s.recordDirs() {
		for _, rec := range engine.ListFlowRecords(dir, workspace) {
			for _, t := range rec.Tasks {
				if !seen[t.ID] {
					seen[t.ID] = true
					out.Tasks = append(out.Tasks, humanTaskMsg(rec, t))
				}
			}
		}
	}
	slices.SortFunc(out.Tasks, func(a, b *pb.HumanTask) int { return a.Created.AsTime().Compare(b.Created.AsTime()) })
	return out
}

// watchTasksEvery is how often a task watch looks for changes.
var watchTasksEvery = time.Second

func (h workspaceService) WatchHumanTasks(ctx context.Context, r req[pb.WatchHumanTasksRequest], stream *connect.ServerStream[pb.WatchHumanTasksResponse]) error {
	last := ""
	tick := time.NewTicker(watchTasksEvery)
	defer tick.Stop()
	for {
		cur := h.s.humanTasks(r.Msg.Workspace)
		// What a client sees changing: the tasks (by ID) and the setting.
		sig := cur.Alerts
		for _, t := range cur.Tasks {
			sig += "|" + t.Id
		}
		if sig != last {
			last = sig
			if err := stream.Send(&pb.WatchHumanTasksResponse{Tasks: cur.Tasks, Alerts: cur.Alerts}); err != nil {
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

func (h workspaceService) AnswerHumanTask(ctx context.Context, r req[pb.AnswerHumanTaskRequest]) (*connect.Response[pb.AnswerHumanTaskResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	answer, err := w.CheckAnswer(r.Msg.TaskId, []byte(r.Msg.ActionJson), []byte(r.Msg.DataJson))
	var bad *engine.AnswerError
	switch {
	case errors.Is(err, engine.ErrNoTask):
		return nil, apiError(connect.CodeNotFound, "TASK_NOT_FOUND", err)
	case errors.Is(err, engine.ErrNotAnswerable):
		return nil, apiError(connect.CodeFailedPrecondition, "NOT_ANSWERABLE", err)
	case errors.As(err, &bad):
		probs := make([]string, len(bad.Problems))
		for i, p := range bad.Problems {
			probs[i] = p.String()
		}
		return nil, apiError(connect.CodeInvalidArgument, "ANSWER_INVALID", err, "problems", strings.Join(probs, "\n"))
	case err != nil:
		return nil, err
	}
	run, err := h.s.resume(w, r.Msg.TaskId, answer)
	if err != nil {
		return nil, err
	}
	return ok(&pb.AnswerHumanTaskResponse{Run: run.info(h.s.broker)})
}

// resume answers a task and runs its workflow on in the background.
func (s *Server) resume(w *workspace, taskID string, answer map[string]any) (*bgRun, error) {
	runID, _, _ := strings.Cut(taskID, "/")
	label := "/" + runID
	for _, rec := range w.FlowTasks() {
		if rec.Run == runID {
			label = "/" + rec.Workflow
		}
	}
	return s.startExec(w, label, func(ctx context.Context, started func(string), on func(api.Event)) (api.TurnResult, error) {
		for _, rec := range w.FlowTasks() {
			if rec.Run == runID {
				started(rec.Session)
			}
		}
		return w.ResumeFlow(ctx, taskID, answer, on)
	})
}

// resumeDueTimers resumes the paused runs whose timers are due, each in
// the background in its workspace (opened if need be).
func (s *Server) resumeDueTimers(ctx context.Context) {
	now := timeNow()
	for _, dir := range s.recordDirs() {
		for id, rec := range engine.DueTimers(dir, now) {
			w, err := s.workspace(ctx, rec.Workspace)
			if err != nil {
				continue
			}
			if _, err := s.resume(w, id, map[string]any{"fired_at": now.Format("2006-01-02T15:04:05Z07:00")}); err != nil {
				slog.Warn("resuming a workflow at its timer", "workflow", rec.Workflow, "error", err)
			}
		}
	}
}

// timeNow is the clock timers are due by (tests set it).
var timeNow = time.Now
