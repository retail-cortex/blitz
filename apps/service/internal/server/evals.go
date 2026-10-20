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
	"path/filepath"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/evals"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Studio's evals (spec_evals_042).

func evalCasesMsg(cases []evals.Case) []*pb.EvalCase {
	out := make([]*pb.EvalCase, 0, len(cases))
	for _, c := range cases {
		out = append(out, &pb.EvalCase{Id: c.ID, Name: c.Name, Prompt: c.Prompt, Output: c.Output, Type: c.Type, Threshold: c.Threshold, Scope: c.Scope})
	}
	return out
}

func evalMsg(w *engine.Workspace, e evals.Eval, live []api.EvalRun) *pb.EvalFile {
	m := &pb.EvalFile{Name: e.Name, Path: e.Path, Description: e.Description, Agents: e.Agents, Skills: e.Skills, Near: e.Near, Allow: e.Allow, Cases: evalCasesMsg(e.Cases), Problem: e.Problem}
	if runs, err := w.EvalRuns(e.Name, 1); err == nil {
		if runs = withLiveEvals(live, runs); len(runs) > 0 {
			m.LastRun = evalRunMsg(runs[0])
			m.LastRun.Results = nil
		}
	}
	return m
}

func evalOf(s *pb.EvalSpec) evals.Eval {
	e := evals.Eval{Name: s.GetName(), Description: s.GetDescription(), Agents: s.GetAgents(), Skills: s.GetSkills(), Near: s.GetNear(), Allow: s.GetAllow()}
	for _, c := range s.GetCases() {
		e.Cases = append(e.Cases, evals.Case{ID: c.GetId(), Name: c.GetName(), Prompt: c.GetPrompt(), Output: c.GetOutput(), Type: c.GetType(), Threshold: c.GetThreshold(), Scope: c.GetScope()})
	}
	return e
}

func evalResultMsg(r api.EvalResult) *pb.EvalResult {
	return &pb.EvalResult{Case: r.Case, Name: r.Name, Agent: r.Agent, Type: r.Type, Scope: r.Scope, Status: r.Status, Score: r.Score, Threshold: r.Threshold,
		Prompt: r.Prompt, Expected: r.Expected, Output: r.Output, Reason: r.Reason, Refusals: r.Refusals, CostUsd: r.CostUSD, Duration: durationpb.New(r.Duration), SkillsUsed: r.SkillsUsed}
}

func evalRunMsg(r api.EvalRun) *pb.EvalRun {
	m := &pb.EvalRun{Id: r.ID, Eval: r.Eval, Started: timestamppb.New(r.Started), Duration: durationpb.New(r.Duration), Status: r.Status, CostUsd: r.CostUSD, Error: r.Error, Counts: map[string]int32{}}
	for _, x := range r.Results {
		m.Results = append(m.Results, evalResultMsg(x))
	}
	for k, v := range r.Counts() {
		m.Counts[k] = int32(v)
	}
	return m
}

func evalError(err error) error {
	switch {
	case errors.Is(err, api.ErrUnknownEval), errors.Is(err, evals.ErrUnknown):
		return apiError(connect.CodeNotFound, "EVAL_NOT_FOUND", err)
	case errors.Is(err, api.ErrEvalNotRunnable):
		return apiError(connect.CodeFailedPrecondition, "EVAL_NOT_RUNNABLE", err)
	}
	return err
}

func (h workspaceService) ListEvals(ctx context.Context, r req[pb.ListEvalsRequest]) (*connect.Response[pb.ListEvalsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListEvalsResponse{Dir: w.ProjectEvalsDir()}
	for _, e := range w.Evals() {
		out.Evals = append(out.Evals, evalMsg(w.Workspace, e, h.s.liveEvals(w.Dir(), e.Name)))
	}
	return ok(out)
}

func (h workspaceService) SaveEval(ctx context.Context, r req[pb.SaveEvalRequest]) (*connect.Response[pb.SaveEvalResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if r.Msg.Eval == nil {
		return nil, invalid(errors.New("eval is required"))
	}
	e := evalOf(r.Msg.Eval)
	if r.Msg.PreviousName != "" { // the text around the table stays
		if old, _, err := evalNamed(w.Workspace, r.Msg.PreviousName); err == nil {
			e.Intro, e.Outro = old.Intro, old.Outro
		}
	}
	saved, problems, err := w.SaveEval(e, r.Msg.PreviousName)
	if err != nil {
		return nil, evalError(err)
	}
	if len(problems) > 0 {
		return ok(&pb.SaveEvalResponse{Problems: problems})
	}
	return ok(&pb.SaveEvalResponse{Eval: evalMsg(w.Workspace, saved, h.s.liveEvals(w.Dir(), saved.Name))})
}

func evalNamed(w *engine.Workspace, name string) (evals.Eval, bool, error) {
	for _, e := range w.Evals() {
		if e.Name == name && filepath.Dir(e.Path) == w.ProjectEvalsDir() {
			return e, true, nil
		}
	}
	return evals.Eval{}, false, api.ErrUnknownEval
}

func (h workspaceService) DeleteEval(ctx context.Context, r req[pb.DeleteEvalRequest]) (*connect.Response[pb.DeleteEvalResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.DeleteEval(r.Msg.Name); err != nil {
		return nil, evalError(err)
	}
	return ok(&pb.DeleteEvalResponse{})
}

// RunEval starts the run in the service (EV-09a) and follows it: with
// background it goes on when the stream ends, else it stops with it.
func (h workspaceService) RunEval(ctx context.Context, r req[pb.RunEvalRequest], stream *connect.ServerStream[pb.RunEvalResponse]) error {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return err
	}
	l, err := h.s.startEval(ctx, w, r.Msg.Name, r.Msg.Cases, r.Msg.Background)
	if err != nil {
		return evalError(err)
	}
	return l.follow(ctx, true, evalSender(stream))
}

// evalSender sends a run's events on stream: started, a result, finished.
func evalSender(stream *connect.ServerStream[pb.RunEvalResponse]) func(api.EvalRun, *api.EvalResult, bool) error {
	return func(run api.EvalRun, x *api.EvalResult, finished bool) error {
		switch {
		case finished:
			return stream.Send(&pb.RunEvalResponse{Event: &pb.RunEvalResponse_Finished{Finished: evalRunMsg(run)}})
		case x != nil:
			return stream.Send(&pb.RunEvalResponse{Event: &pb.RunEvalResponse_Result{Result: evalResultMsg(*x)}})
		}
		return stream.Send(&pb.RunEvalResponse{Event: &pb.RunEvalResponse_Started{Started: evalRunMsg(run)}})
	}
}

// evalWatcher is evalSender for WatchEvalRun's stream.
func evalWatcher(stream *connect.ServerStream[pb.WatchEvalRunResponse]) func(api.EvalRun, *api.EvalResult, bool) error {
	return func(run api.EvalRun, x *api.EvalResult, finished bool) error {
		switch {
		case finished:
			return stream.Send(&pb.WatchEvalRunResponse{Event: &pb.WatchEvalRunResponse_Finished{Finished: evalRunMsg(run)}})
		case x != nil:
			return stream.Send(&pb.WatchEvalRunResponse{Event: &pb.WatchEvalRunResponse_Result{Result: evalResultMsg(*x)}})
		}
		return stream.Send(&pb.WatchEvalRunResponse{Event: &pb.WatchEvalRunResponse_Started{Started: evalRunMsg(run)}})
	}
}

// WatchEvalRun follows a run going on, or sends a recorded one as
// finished.
func (h workspaceService) WatchEvalRun(ctx context.Context, r req[pb.WatchEvalRunRequest], stream *connect.ServerStream[pb.WatchEvalRunResponse]) error {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return err
	}
	if l := h.s.liveEval(w.Dir(), r.Msg.Id); l != nil {
		return l.follow(ctx, false, evalWatcher(stream))
	}
	for _, e := range w.Evals() {
		runs, err := w.EvalRuns(e.Name, 0)
		if err != nil {
			continue
		}
		for _, run := range runs {
			if run.ID == r.Msg.Id {
				return evalWatcher(stream)(run, nil, true)
			}
		}
	}
	return errUnknownEvalRun(r.Msg.Id)
}

// StopEvalRun stops a run going on; it records itself as stopped.
func (h workspaceService) StopEvalRun(ctx context.Context, r req[pb.StopEvalRunRequest]) (*connect.Response[pb.StopEvalRunResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	l := h.s.liveEval(w.Dir(), r.Msg.Id)
	if l == nil {
		return nil, errUnknownEvalRun(r.Msg.Id)
	}
	l.cancel()
	return ok(&pb.StopEvalRunResponse{})
}

func (h workspaceService) ListEvalRuns(ctx context.Context, r req[pb.ListEvalRunsRequest]) (*connect.Response[pb.ListEvalRunsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	limit := int(r.Msg.Limit)
	if limit <= 0 {
		limit = 20
	}
	runs, err := w.EvalRuns(r.Msg.Name, limit)
	if err != nil {
		return nil, evalError(err)
	}
	runs = withLiveEvals(h.s.liveEvals(w.Dir(), r.Msg.Name), runs)
	if len(runs) > limit {
		runs = runs[:limit]
	}
	out := &pb.ListEvalRunsResponse{}
	for _, run := range runs {
		out.Runs = append(out.Runs, evalRunMsg(run))
	}
	return ok(out)
}

func (h workspaceService) DraftEvalCases(ctx context.Context, r req[pb.DraftEvalCasesRequest]) (*connect.Response[pb.DraftEvalCasesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	existing := evalOf(&pb.EvalSpec{Cases: r.Msg.Existing}).Cases
	d, err := w.DraftEvalCases(ctx, r.Msg.Agent, r.Msg.Request, existing)
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.DraftEvalCasesResponse{Cases: evalCasesMsg(d.Cases), Problem: d.Problem})
}
