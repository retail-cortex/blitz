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

	"github.com/retail-cortex/blitz/pkg/api"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Background runs (spec_parity_027 PAR-PAR-20): turns the service runs in
// sessions of their own with no client attached (blitz --bg). A run's
// events are kept, so a client can watch it from the start (blitz logs,
// attach); its approval requests and questions go through the broker as a
// turn's do, and wait until a watching client answers.

const (
	// keptRunEvents is how many of a run's latest events are kept.
	keptRunEvents = 5000
	// keptEndedRuns is how many ended runs the list keeps.
	keptEndedRuns = 50
)

// bgRun is one background run.
type bgRun struct {
	id, workspace, prompt string
	started               time.Time
	cancel                context.CancelFunc
	done                  chan struct{} // closed when it has ended
	w                     *workspace

	mu       sync.Mutex
	session  string
	state    string // running, done, failed or stopped
	ended    time.Time
	errText  string
	events   []*pb.TurnEvent
	dropped  int      // events dropped from the front, to keep keptRunEvents
	requests []string // the requests it made, by ID
	notify   map[chan struct{}]struct{}
}

// runs are the service's background runs.
type runs struct {
	mu   sync.Mutex
	next int
	list []*bgRun // oldest first
}

// add records an event and wakes the watchers.
func (r *bgRun) add(ev *pb.TurnEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch k := ev.Kind.(type) {
	case *pb.TurnEvent_ApprovalRequest:
		r.requests = append(r.requests, k.ApprovalRequest.RequestId)
	case *pb.TurnEvent_Question:
		r.requests = append(r.requests, k.Question.RequestId)
	}
	r.events = append(r.events, ev)
	if over := len(r.events) - keptRunEvents; over > 0 {
		r.events = slices.Delete(r.events, 0, over)
		r.dropped += over
	}
	for ch := range r.notify {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// info describes the run; b says which of its requests still wait.
func (r *bgRun) info(b *broker) *pb.BackgroundRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := &pb.BackgroundRun{
		Id: r.id, Workspace: r.workspace, SessionId: r.session, Prompt: r.prompt, State: r.state,
		Started: timestamppb.New(r.started), Error: r.errText,
	}
	if !r.ended.IsZero() {
		m.Ended = timestamppb.New(r.ended)
	}
	if r.session != "" {
		m.CostUsd = r.w.UsageOf(r.session).CostUSD
	}
	for _, id := range r.requests {
		if b.waiting(id) {
			m.Waiting++
		}
	}
	if m.Waiting > 0 && r.state == "running" {
		m.State = "waiting"
	}
	return m
}

// start runs t in a new session of w, in the background.
func (s *Server) startRun(w *workspace, t api.Turn) (*bgRun, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, apiError(connect.CodeUnavailable, "SHUTTING_DOWN", errServerClosed)
	}
	ctx, cancel := context.WithCancel(context.Background())
	first, _, _ := strings.Cut(strings.TrimSpace(t.Text), "\n")
	s.runs.mu.Lock()
	s.runs.next++
	r := &bgRun{
		id: fmt.Sprintf("bg-%d", s.runs.next), workspace: w.Dir(), prompt: first, started: time.Now(),
		cancel: cancel, done: make(chan struct{}), w: w, state: "running", notify: map[chan struct{}]struct{}{},
	}
	s.runs.list = append(s.runs.list, r)
	s.runs.prune()
	s.runs.mu.Unlock()

	started := make(chan struct{})
	go func() {
		defer close(r.done)
		ctx = withSink(ctx, r.add)
		res, err := w.RunDetached(ctx, t, func(id string) {
			r.mu.Lock()
			r.session = id
			r.mu.Unlock()
			close(started)
		}, func(e api.Event) { r.add(eventMsg(e)) })
		r.mu.Lock()
		switch {
		case ctx.Err() != nil:
			r.state = "stopped"
		case err != nil:
			r.state, r.errText = "failed", err.Error()
		default:
			r.state = "done"
		}
		r.ended = time.Now()
		r.mu.Unlock()
		select {
		case <-started:
		default: // it failed before its session existed
			close(started)
		}
		r.add(&pb.TurnEvent{Kind: &pb.TurnEvent_Finished{Finished: &pb.TurnFinished{
			Output: res.Output, Before: usageMsg(res.Before), After: usageMsg(res.After), Leftover: res.Leftover, Error: errorInfo(err),
		}}})
	}()
	<-started
	return r, nil
}

// prune forgets the oldest ended runs beyond keptEndedRuns. Call with mu
// held.
func (rs *runs) prune() {
	ended := 0
	for i := len(rs.list) - 1; i >= 0; i-- {
		select {
		case <-rs.list[i].done:
			if ended++; ended > keptEndedRuns {
				rs.list = slices.Delete(rs.list, i, i+1)
			}
		default:
		}
	}
}

// run finds a run by ID.
func (s *Server) run(id string) (*bgRun, error) {
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	for _, r := range s.runs.list {
		if r.id == id {
			return r, nil
		}
	}
	return nil, apiError(connect.CodeNotFound, "UNKNOWN_RUN", fmt.Errorf("%w: %q", api.ErrUnknownRun, id), "id", id)
}

// stopRuns stops every run and waits for them to end (the service is
// closing).
func (s *Server) stopRuns() {
	s.runs.mu.Lock()
	list := slices.Clone(s.runs.list)
	s.runs.mu.Unlock()
	for _, r := range list {
		r.cancel()
	}
	for _, r := range list {
		<-r.done
	}
}

// watch sends the run's events from the first kept one: requests that no
// longer wait are left out. With follow it goes on until the run ends.
func (r *bgRun) watch(ctx context.Context, b *broker, follow bool, send func(*pb.TurnEvent) error) error {
	ch := make(chan struct{}, 1)
	r.mu.Lock()
	r.notify[ch] = struct{}{}
	next := r.dropped
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.notify, ch)
		r.mu.Unlock()
	}()
	for {
		r.mu.Lock()
		next = max(next, r.dropped)
		batch := slices.Clone(r.events[next-r.dropped:])
		next += len(batch)
		r.mu.Unlock()
		for _, ev := range batch {
			if id := requestID(ev); id != "" && !b.waiting(id) {
				continue
			}
			if err := send(ev); err != nil {
				return err
			}
			if _, ok := ev.Kind.(*pb.TurnEvent_Finished); ok {
				return nil
			}
		}
		if !follow {
			return nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// requestID is the ID of the request an event asks, or "".
func requestID(ev *pb.TurnEvent) string {
	switch k := ev.Kind.(type) {
	case *pb.TurnEvent_ApprovalRequest:
		return k.ApprovalRequest.RequestId
	case *pb.TurnEvent_Question:
		return k.Question.RequestId
	}
	return ""
}

func (h sessionService) StartBackground(ctx context.Context, r req[pb.StartBackgroundRequest]) (*connect.Response[pb.StartBackgroundResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	t := r.Msg.Turn
	if t == nil || strings.TrimSpace(t.Text) == "" {
		return nil, apiError(connect.CodeInvalidArgument, "INVALID_TURN", errors.New("a background run needs a prompt"))
	}
	run, err := h.s.startRun(w, api.Turn{
		Text: t.Text, Prompt: t.Prompt, Plan: t.Plan, ReadOnly: t.ReadOnly, MaxTurns: int(t.MaxTurns),
		MaxCostUSD: t.MaxCostUsd, Timeout: t.GetTimeout().AsDuration(), Command: t.Command,
	})
	if err != nil {
		return nil, err
	}
	return ok(&pb.StartBackgroundResponse{Run: run.info(h.s.broker)})
}

func (h sessionService) ListBackground(ctx context.Context, r req[pb.ListBackgroundRequest]) (*connect.Response[pb.ListBackgroundResponse], error) {
	h.s.runs.mu.Lock()
	list := slices.Clone(h.s.runs.list)
	h.s.runs.mu.Unlock()
	res := &pb.ListBackgroundResponse{}
	for _, run := range slices.Backward(list) {
		res.Runs = append(res.Runs, run.info(h.s.broker))
	}
	return ok(res)
}

func (h sessionService) WatchBackground(ctx context.Context, r req[pb.WatchBackgroundRequest], stream *connect.ServerStream[pb.WatchBackgroundResponse]) error {
	run, err := h.s.run(r.Msg.Id)
	if err != nil {
		return err
	}
	return run.watch(ctx, h.s.broker, r.Msg.Follow, func(ev *pb.TurnEvent) error {
		return stream.Send(&pb.WatchBackgroundResponse{Event: ev})
	})
}

func (h sessionService) StopBackground(ctx context.Context, r req[pb.StopBackgroundRequest]) (*connect.Response[pb.StopBackgroundResponse], error) {
	run, err := h.s.run(r.Msg.Id)
	if err != nil {
		return nil, err
	}
	run.cancel()
	select {
	case <-run.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return ok(&pb.StopBackgroundResponse{Run: run.info(h.s.broker)})
}
