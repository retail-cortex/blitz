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
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Turns that outlive their stream (spec_021_service SVC-20a): a RunTurn
// with a turn_id runs in the service, not in the call. Its events are
// kept and numbered, so a client whose stream dropped (Chromium ends
// every connection, the loopback's too, when a network interface comes
// or goes: issue #128) asks again with the last seq it has and misses
// nothing. StopTurn stops it; so does a minute with no stream attached,
// so a client that died doesn't leave a turn spending.

var (
	// turnGrace is how long a turn runs with no stream attached before
	// it's stopped (a variable for tests).
	turnGrace = time.Minute
	// keptTurnEnd is how long an ended turn is remembered, for a client
	// whose stream dropped as it ended to read its last events.
	keptTurnEnd = 5 * time.Minute
)

var (
	// errTurnStopped is the cause of a turn StopTurn stopped.
	errTurnStopped = errors.New("the turn was stopped")
	// errTurnAbandoned is the cause of a turn stopped for want of a stream.
	errTurnAbandoned = errors.New("no client came back for the turn")
	// errUnknownTurn refuses a turn_id the service doesn't know.
	errUnknownTurn = errors.New("no such turn (it ended a while ago, or the service restarted)")
)

// turnIDs are the turn IDs taken: a UUID, or the like.
var turnIDs = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// liveTurn is one such turn.
type liveTurn struct {
	id, workspace string
	cancel        context.CancelCauseFunc
	done          chan struct{} // closed when it has ended

	mu       sync.Mutex
	events   []*pb.TurnEvent // the latest; the i-th has seq dropped+i+1
	dropped  int             // events dropped from the front, to keep keptRunEvents
	notify   map[chan struct{}]struct{}
	watchers int
	lease    *time.Timer // running while it has no watcher
	ended    bool
}

// liveTurns are the service's turns with an ID, running or just ended.
type liveTurns struct {
	mu   sync.Mutex
	byID map[string]*liveTurn
}

// add records an event and wakes the watchers.
func (t *liveTurn) add(ev *pb.TurnEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, ev)
	if over := len(t.events) - keptRunEvents; over > 0 {
		clear(t.events[:over])
		t.events = t.events[over:]
		t.dropped += over
	}
	for ch := range t.notify {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// attach counts a watcher in: the turn isn't abandoned while it has one.
func (t *liveTurn) attach(ch chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.notify[ch] = struct{}{}
	t.watchers++
	if t.lease != nil {
		t.lease.Stop()
		t.lease = nil
	}
}

// detach counts a watcher out; the last one's leaving starts the wait
// for another (turnGrace), after which the turn is stopped.
func (t *liveTurn) detach(ch chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.notify, ch)
	t.watchers--
	if t.watchers > 0 || t.ended {
		return
	}
	t.lease = time.AfterFunc(turnGrace, func() {
		t.mu.Lock()
		abandoned := t.watchers == 0 && !t.ended
		t.mu.Unlock()
		if abandoned {
			t.cancel(errTurnAbandoned)
		}
	})
}

// watch sends the turn's events after seq after (from the first kept, if
// those went), then those to come, until its last. Requests that no
// longer wait are left out, so a card already answered doesn't come back.
func (t *liveTurn) watch(ctx context.Context, b *broker, after uint64, send func(seq uint64, ev *pb.TurnEvent) error) error {
	ch := make(chan struct{}, 1)
	t.attach(ch)
	defer t.detach(ch)
	t.mu.Lock()
	next := int(min(after, uint64(t.dropped+len(t.events))))
	t.mu.Unlock()
	for {
		t.mu.Lock()
		next = max(next, t.dropped)
		batch := slices.Clone(t.events[next-t.dropped:])
		first := next
		next += len(batch)
		t.mu.Unlock()
		for i, ev := range batch {
			if id := requestID(ev); id != "" && !b.waiting(id) {
				continue
			}
			if err := send(uint64(first+i+1), ev); err != nil {
				return err
			}
			if _, ok := ev.Kind.(*pb.TurnEvent_Finished); ok {
				return nil
			}
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// liveTurn finds the turn a RunTurn names in w, or starts it: started
// only by a request that has seen none of it (after_seq 0) and says what
// to run. ctx's values go with the turn; its end doesn't.
func (s *Server) liveTurn(ctx context.Context, w *workspace, m *pb.RunTurnRequest) (*liveTurn, error) {
	if !turnIDs.MatchString(m.TurnId) {
		return nil, apiError(connect.CodeInvalidArgument, "INVALID_TURN", errors.New("turn_id takes letters, digits, - and _, up to 64"))
	}
	find := func() (*liveTurn, error) {
		t, ok := s.turns.byID[m.TurnId]
		switch {
		case ok && t.workspace == w.Dir():
			return t, nil
		case ok, m.Turn == nil, m.AfterSeq > 0:
			return nil, apiError(connect.CodeNotFound, "UNKNOWN_TURN", fmt.Errorf("%w: %q", errUnknownTurn, m.TurnId), "turn_id", m.TurnId)
		}
		return nil, nil
	}
	s.turns.mu.Lock()
	t, err := find()
	s.turns.mu.Unlock()
	if t != nil || err != nil {
		return t, err
	}
	turn, err := s.turn(w, m.Workspace, m.Turn)
	if err != nil {
		return nil, err
	}

	// Checked and added in one step, as a background run is: Close's
	// stopTurns must see it.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, apiError(connect.CodeUnavailable, "SHUTTING_DOWN", errServerClosed)
	}
	s.turns.mu.Lock()
	defer s.turns.mu.Unlock()
	if t, err := find(); t != nil || err != nil {
		return t, err // the same request, sent twice at once
	}
	runCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	t = &liveTurn{id: m.TurnId, workspace: w.Dir(), cancel: cancel, done: make(chan struct{}), notify: map[chan struct{}]struct{}{}}
	if s.turns.byID == nil {
		s.turns.byID = map[string]*liveTurn{}
	}
	s.turns.byID[t.id] = t
	go func() {
		runCtx := withSink(runCtx, t.add)
		turn.OnAccepted = func() { t.add(&pb.TurnEvent{Kind: &pb.TurnEvent_Accepted{Accepted: &pb.Accepted{}}}) }
		res, runErr := w.Run(runCtx, m.SessionId, turn, func(e api.Event) { t.add(eventMsg(e)) })
		cancel(nil) // its context's done with
		t.mu.Lock()
		t.ended = true
		if t.lease != nil {
			t.lease.Stop()
			t.lease = nil
		}
		t.mu.Unlock()
		close(t.done)
		t.add(&pb.TurnEvent{Kind: &pb.TurnEvent_Finished{Finished: &pb.TurnFinished{
			Output: res.Output, Before: usageMsg(res.Before), After: usageMsg(res.After), Leftover: res.Leftover, Error: errorInfo(runErr),
		}}})
		time.AfterFunc(keptTurnEnd, func() {
			s.turns.mu.Lock()
			defer s.turns.mu.Unlock()
			if s.turns.byID[t.id] == t {
				delete(s.turns.byID, t.id)
			}
		})
	}()
	return t, nil
}

// stopTurns stops every turn with an ID and waits for them to end (the
// service is closing).
func (s *Server) stopTurns() {
	s.turns.mu.Lock()
	var list []*liveTurn
	for _, t := range s.turns.byID {
		list = append(list, t)
	}
	s.turns.mu.Unlock()
	for _, t := range list {
		t.cancel(errServerClosed)
	}
	for _, t := range list {
		<-t.done
	}
}

func (h sessionService) StopTurn(ctx context.Context, r req[pb.StopTurnRequest]) (*connect.Response[pb.StopTurnResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	h.s.turns.mu.Lock()
	t, found := h.s.turns.byID[r.Msg.TurnId]
	h.s.turns.mu.Unlock()
	if !found || t.workspace != w.Dir() {
		return nil, apiError(connect.CodeNotFound, "UNKNOWN_TURN", fmt.Errorf("%w: %q", errUnknownTurn, r.Msg.TurnId), "turn_id", r.Msg.TurnId)
	}
	t.cancel(errTurnStopped)
	select {
	case <-t.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return ok(&pb.StopTurnResponse{})
}
