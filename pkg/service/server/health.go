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
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/health"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The workspace dashboard over the API (spec_health_044).

// watchHealthEvery is how often WatchWorkspaceHealth looks again.
var watchHealthEvery = 5 * time.Second

// healthChecks are the checks going, their stop by workspace; and
// healthProgress how far each is.
var (
	healthChecks   sync.Map // dir → context.CancelFunc
	reflections    sync.Map // dir → true while a scheduled reflection runs
	healthProgress sync.Map // dir → engine.HealthProgress
)

func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func verdictMsg(v health.Verdict, current bool) *pb.HealthVerdict {
	m := &pb.HealthVerdict{Model: v.Model, At: ts(v.At), Overall: v.Overall, Grade: health.Grade(v.Overall), Summary: v.Summary, Fixes: v.Fixes, CostUsd: v.CostUSD, Current: current}
	for _, s := range v.Scores {
		m.Scores = append(m.Scores, &pb.HealthScore{Line: s.Line, Score: int32(s.Score), Reason: s.Reason})
	}
	return m
}

func healthMsg(r engine.HealthReport, checking bool) *pb.WorkspaceHealth {
	m := &pb.WorkspaceHealth{At: ts(r.At), Overall: r.Overall, ByKind: r.ByKind, PreviousOverall: r.PreviousOverall,
		LastCheck: ts(r.State.LastCheck), LastCostUsd: r.State.LastCost, Skipped: r.State.Skipped,
		Model: r.Model, Schedule: r.Schedule, Next: ts(r.Next), MaxCostUsd: r.MaxCostUSD, Checking: checking,
		LastJudged: int32(r.State.Judged), LastErrors: r.State.Errors,
		ReflectSchedule: r.ReflectSchedule, ReflectMaxCostUsd: r.ReflectMaxCostUSD, ReflectThreshold: int32(r.ReflectThreshold), ReflectSkipped: r.ReflectSkipped}
	for _, it := range r.Items {
		pi := &pb.HealthItem{Kind: it.Kind, Name: it.Name, Path: it.Path, Detail: it.Detail, Problem: it.Problem, Flags: it.Flags,
			Uses: int32(it.Uses), Fired: int32(it.Fired), History: r.History[it.Key()]}
		for _, d := range it.Daily {
			pi.Daily = append(pi.Daily, int32(d))
		}
		if s := it.Runs; s != nil {
			pi.Runs = &pb.HealthRuns{Runs: int32(s.Runs), Done: int32(s.Done), Failed: int32(s.Failed), Stopped: int32(s.Stopped),
				CostUsd: s.CostUSD, MedianMs: s.Median.Milliseconds(), Last: ts(s.Last), FailingStep: s.FailingStep}
		}
		if e := it.Evals; e != nil {
			pi.Evals = &pb.HealthEvals{Runs: int32(e.Runs), Last: ts(e.Last), Passed: int32(e.Passed), Cases: int32(e.Cases)}
		}
		if v, ok := r.Verdicts[it.Key()]; ok {
			pi.Verdict = verdictMsg(v, v.Hash == it.Hash && v.Rubric == health.RubricVersion)
		}
		m.Items = append(m.Items, pi)
	}
	for _, d := range r.Days {
		m.Days = append(m.Days, &pb.HealthDay{Date: d.Date, Done: int32(d.Done), Failed: int32(d.Failed), Stopped: int32(d.Stopped), CostUsd: d.CostUSD})
	}
	for _, s := range r.Signals {
		m.Signals = append(m.Signals, &pb.HealthSignal{Kind: s.Kind, About: s.About, Count: int32(s.Count), Last: ts(s.Last), Detail: s.Detail})
	}
	return m
}

func checking(dir string) bool {
	_, ok := healthChecks.Load(dir)
	return ok
}

// withProgress is m with how far the workspace's check is.
func withProgress(m *pb.WorkspaceHealth, dir string) *pb.WorkspaceHealth {
	if v, ok := healthProgress.Load(dir); ok && m.Checking {
		p := v.(engine.HealthProgress)
		m.Progress = &pb.HealthCheckProgress{Item: p.Item, Index: int32(p.Index), Total: int32(p.Total), CostUsd: p.CostUSD}
	}
	return m
}

// runCheck runs a check of w in the background, until it ends or is
// stopped, its progress kept for the dashboard. It's false when a check
// of w is going already.
func runCheck(w *workspace, c engine.HealthCheck, scheduled bool) bool {
	ctx, cancel := context.WithCancel(context.Background())
	if _, busy := healthChecks.LoadOrStore(w.Dir(), cancel); busy {
		cancel()
		return false
	}
	healthProgress.Store(w.Dir(), engine.HealthProgress{})
	var cost float64
	c.Starting = func(item string, index, total int) {
		healthProgress.Store(w.Dir(), engine.HealthProgress{Item: item, Index: index, Total: total, CostUSD: cost})
	}
	go func() {
		defer healthChecks.Delete(w.Dir())
		defer healthProgress.Delete(w.Dir())
		defer cancel()
		var err error
		if scheduled {
			err = w.ScheduledHealthCheck(ctx, c.Starting)
		} else {
			_, _, err = w.CheckHealth(ctx, c, func(p engine.HealthProgress) { cost = p.CostUSD })
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("health check", "workspace", w.Dir(), "scheduled", scheduled, "error", err)
		}
	}()
	return true
}

func checkScope(s *pb.HealthCheckScope) engine.HealthCheck {
	return engine.HealthCheck{Kinds: s.GetKinds(), Items: s.GetItems(), Force: s.GetForce()}
}

func (h workspaceService) GetWorkspaceHealth(ctx context.Context, r req[pb.GetWorkspaceHealthRequest]) (*connect.Response[pb.GetWorkspaceHealthResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	return ok(&pb.GetWorkspaceHealthResponse{Health: withProgress(healthMsg(w.Health(ctx), checking(w.Dir())), w.Dir())})
}

func (h workspaceService) WatchWorkspaceHealth(ctx context.Context, r req[pb.WatchWorkspaceHealthRequest], stream *connect.ServerStream[pb.WatchWorkspaceHealthResponse]) error {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return err
	}
	var last [32]byte
	tick := time.NewTicker(watchHealthEvery)
	defer tick.Stop()
	for {
		m := withProgress(healthMsg(w.Health(ctx), checking(w.Dir())), w.Dir())
		at := m.At
		m.At = nil // the time alone isn't a change
		data, _ := json.Marshal(m)
		if sum := sha256.Sum256(data); sum != last {
			last = sum
			m.At = at
			if err := stream.Send(&pb.WatchWorkspaceHealthResponse{Health: m}); err != nil {
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

func (h workspaceService) EstimateHealthCheck(ctx context.Context, r req[pb.EstimateHealthCheckRequest]) (*connect.Response[pb.EstimateHealthCheckResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	n, cost, priced := w.EstimateHealthCheck(ctx, checkScope(r.Msg.Scope))
	return ok(&pb.EstimateHealthCheckResponse{Items: int32(n), CostUsd: cost, Priced: priced})
}

func (h workspaceService) CheckWorkspaceHealth(ctx context.Context, r req[pb.CheckWorkspaceHealthRequest], stream *connect.ServerStream[pb.CheckWorkspaceHealthResponse]) error {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, busy := healthChecks.LoadOrStore(w.Dir(), cancel); busy {
		return apiError(connect.CodeFailedPrecondition, "HEALTH_CHECK_RUNNING", engine.ErrHealthCheckRunning)
	}
	defer healthChecks.Delete(w.Dir())
	defer healthProgress.Delete(w.Dir())
	sc := checkScope(r.Msg.Scope)
	sc.Starting = func(item string, index, total int) {
		healthProgress.Store(w.Dir(), engine.HealthProgress{Item: item, Index: index, Total: total})
	}
	judged, cost, err := w.CheckHealth(cctx, sc, func(p engine.HealthProgress) {
		m := &pb.CheckWorkspaceHealthResponse{Item: p.Item, Index: int32(p.Index), Total: int32(p.Total), Error: p.Error, CostUsd: p.CostUSD}
		if p.Verdict != nil {
			m.Verdict = verdictMsg(*p.Verdict, true)
		}
		_ = stream.Send(m)
	})
	switch {
	case errors.Is(err, engine.ErrHealthCheckRunning):
		return apiError(connect.CodeFailedPrecondition, "HEALTH_CHECK_RUNNING", err)
	case err != nil && !errors.Is(err, context.Canceled):
		// Not Unavailable: that reads as the service gone.
		return apiError(connect.CodeFailedPrecondition, "HEALTH_CHECK_FAILED", err)
	}
	return stream.Send(&pb.CheckWorkspaceHealthResponse{Done: true, Judged: int32(judged), CostUsd: cost})
}

func (h workspaceService) StartHealthCheck(ctx context.Context, r req[pb.StartHealthCheckRequest]) (*connect.Response[pb.StartHealthCheckResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if !runCheck(w, checkScope(r.Msg.Scope), false) {
		return nil, apiError(connect.CodeFailedPrecondition, "HEALTH_CHECK_RUNNING", engine.ErrHealthCheckRunning)
	}
	return ok(&pb.StartHealthCheckResponse{})
}

func (h workspaceService) StopHealthCheck(ctx context.Context, r req[pb.StopHealthCheckRequest]) (*connect.Response[pb.StopHealthCheckResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if stop, ok := healthChecks.Load(w.Dir()); ok {
		stop.(context.CancelFunc)()
	}
	return ok(&pb.StopHealthCheckResponse{})
}

// healthDue runs the scheduled checks that are due, of the open
// workspaces, in the background (HL-14).
func (s *Server) healthDue() {
	now := time.Now()
	for _, dir := range s.openDirs() {
		s.mu.Lock()
		w := s.workspaces[dir]
		s.mu.Unlock()
		if w == nil || !w.HealthDue(now) {
			continue
		}
		runCheck(w, engine.HealthCheck{}, true)
	}
}

// reflectDue runs the scheduled reflections that are due, of the open
// workspaces, in the background, one at a time per workspace
// (spec_harness_learning_048 HE-06).
func (s *Server) reflectDue() {
	now := time.Now()
	for _, dir := range s.openDirs() {
		s.mu.Lock()
		w := s.workspaces[dir]
		s.mu.Unlock()
		if w == nil {
			continue
		}
		if _, busy := reflections.LoadOrStore(dir, true); busy {
			continue
		}
		if !w.ReflectDue(now) {
			reflections.Delete(dir)
			continue
		}
		go func() {
			defer reflections.Delete(dir)
			if err := w.ScheduledReflect(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("scheduled reflection", "workspace", dir, "error", err)
			}
		}()
	}
}
