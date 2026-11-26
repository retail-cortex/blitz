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
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// askFirst is a service whose turn asks before it writes made.txt, so it
// waits where a test wants it.
func askFirst(t *testing.T) (clients, *Server, string, string) {
	t.Helper()
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	c, s := serve(t, func(cfg *config.Config) { cfg.Blitz.AutoApprove, cfg.Blitz.PermissionMode = false, "default" }, create, text("done"))
	dir := t.TempDir()
	sess, err := c.sessions.NewSession(context.Background(), connect.NewRequest(&pb.NewSessionRequest{Workspace: dir}))
	require.NoError(t, err)
	return c, s, dir, sess.Msg.Session.Id
}

// untilApproval reads a turn's stream up to its approval request, and
// returns the request and the seq of the event before it.
func untilApproval(t *testing.T, stream *connect.ServerStreamForClient[pb.RunTurnResponse]) (*pb.ApprovalRequest, uint64) {
	t.Helper()
	var last uint64
	for stream.Receive() {
		m := stream.Msg()
		assert.Equal(t, last+1, m.Seq, "events are numbered from 1, in order")
		if ar := m.Event.GetApprovalRequest(); ar != nil {
			return ar, last
		}
		last = m.Seq
	}
	require.Fail(t, "the stream ended before the approval request", "%v", stream.Err())
	return nil, 0
}

// busy reports whether a turn runs in the server's workspace dir.
func busy(t *testing.T, s *Server, dir string) bool {
	t.Helper()
	key, err := canonical(dir)
	require.NoError(t, err)
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.workspaces[key]
	return w != nil && w.Busy()
}

// A turn with an ID goes on when its stream drops (issue #128): the same
// request, with the last seq received, takes its events up from there,
// a waiting approval among them, and the turn ends as if nothing had
// happened. Asked again once it has ended, it's read, not run again.
func TestTurnOutlivesItsStream(t *testing.T) {
	c, s, dir, id := askFirst(t)
	req := func(after uint64) *connect.Request[pb.RunTurnRequest] {
		return connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: id, Turn: &pb.Turn{Text: "make a file"}, TurnId: "turn-1", AfterSeq: after})
	}
	ctx, drop := context.WithCancel(context.Background())
	stream, err := c.sessions.RunTurn(ctx, req(0))
	require.NoError(t, err)
	_, last := untilApproval(t, stream)
	drop() // the network changed under the stream
	for stream.Receive() {
	}
	require.Error(t, stream.Err())
	time.Sleep(50 * time.Millisecond)
	assert.True(t, busy(t, s, dir), "the turn goes on without its stream")

	ctx = context.Background()
	stream, err = c.sessions.RunTurn(ctx, req(last))
	require.NoError(t, err)
	var kinds []string
	var finished *pb.TurnFinished
	seq := last
	for stream.Receive() {
		m := stream.Msg()
		assert.Greater(t, m.Seq, seq, "only what came after")
		seq = m.Seq
		switch {
		case m.Event.GetApprovalRequest() != nil:
			kinds = append(kinds, "approval")
			_, aerr := c.sessions.Approve(ctx, connect.NewRequest(&pb.ApproveRequest{Workspace: dir, RequestId: m.Event.GetApprovalRequest().RequestId, Decision: pb.Decision_DECISION_ONCE}))
			assert.NoError(t, aerr)
		case m.Event.GetFinished() != nil:
			kinds = append(kinds, "finished")
			finished = m.Event.GetFinished()
		}
	}
	require.NoError(t, stream.Err())
	assert.Equal(t, []string{"approval", "finished"}, kinds, "the approval still waiting is sent again")
	require.NotNil(t, finished)
	assert.Nil(t, finished.Error)
	assert.Equal(t, "done", finished.Output)
	assert.FileExists(t, filepath.Join(dir, "made.txt"))

	// The turn has ended: the request is answered from what's kept, the
	// approval (answered) left out, and nothing runs twice.
	stream, err = c.sessions.RunTurn(ctx, req(0))
	require.NoError(t, err)
	n, approvals := 0, 0
	for stream.Receive() {
		n++
		if stream.Msg().Event.GetApprovalRequest() != nil {
			approvals++
		}
		finished = stream.Msg().Event.GetFinished()
	}
	require.NoError(t, stream.Err())
	assert.Equal(t, int(seq)-1, n, "every event but the answered approval")
	assert.Zero(t, approvals)
	assert.NotNil(t, finished, "its last event last")
	active, err := c.sessions.GetActiveSession(ctx, connect.NewRequest(&pb.GetActiveSessionRequest{Workspace: dir}))
	require.NoError(t, err)
	prompts := 0
	for _, m := range active.Msg.Session.Messages {
		if m.Text == "make a file" {
			prompts++
		}
	}
	assert.Equal(t, 1, prompts, "the prompt ran once")
}

// StopTurn stops a turn with an ID and waits for it; the IDs the service
// doesn't take or know are refused.
func TestStopTurn(t *testing.T) {
	c, s, dir, id := askFirst(t)
	ctx := context.Background()
	stream, err := c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: id, Turn: &pb.Turn{Text: "make a file"}, TurnId: "turn-1"}))
	require.NoError(t, err)
	untilApproval(t, stream)
	_, err = c.sessions.StopTurn(ctx, connect.NewRequest(&pb.StopTurnRequest{Workspace: dir, TurnId: "turn-1"}))
	require.NoError(t, err)
	assert.False(t, busy(t, s, dir), "stopped by the time StopTurn answers")
	var finished *pb.TurnFinished
	for stream.Receive() {
		finished = stream.Msg().Event.GetFinished()
	}
	require.NoError(t, stream.Err())
	require.NotNil(t, finished, "the stream ends with the turn's last event")
	assert.NotNil(t, finished.Error)
	assert.NoFileExists(t, filepath.Join(dir, "made.txt"))
	_, err = c.sessions.StopTurn(ctx, connect.NewRequest(&pb.StopTurnRequest{Workspace: dir, TurnId: "turn-1"}))
	assert.NoError(t, err, "an ended turn is no error")

	for name, tc := range map[string]struct {
		call   func() error
		code   connect.Code
		reason string
	}{
		"stopping an unknown turn": {func() error {
			_, err := c.sessions.StopTurn(ctx, connect.NewRequest(&pb.StopTurnRequest{Workspace: dir, TurnId: "nope"}))
			return err
		}, connect.CodeNotFound, "UNKNOWN_TURN"},
		"another workspace's turn": {func() error {
			_, err := c.sessions.StopTurn(ctx, connect.NewRequest(&pb.StopTurnRequest{Workspace: t.TempDir(), TurnId: "turn-1"}))
			return err
		}, connect.CodeNotFound, "UNKNOWN_TURN"},
		"taking up an unknown turn": {func() error {
			return streamErr(c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: id, Turn: &pb.Turn{Text: "hi"}, TurnId: "gone", AfterSeq: 3})))
		}, connect.CodeNotFound, "UNKNOWN_TURN"},
		"an unknown turn and nothing to run": {func() error {
			return streamErr(c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: id, TurnId: "gone"})))
		}, connect.CodeNotFound, "UNKNOWN_TURN"},
		"an ID that isn't one": {func() error {
			return streamErr(c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: id, Turn: &pb.Turn{Text: "hi"}, TurnId: "a b"})))
		}, connect.CodeInvalidArgument, "INVALID_TURN"},
	} {
		t.Run(name, func(t *testing.T) {
			code, info := errorReason(t, tc.call())
			assert.Equal(t, tc.code, code)
			assert.Equal(t, tc.reason, info.Reason)
		})
	}
}

// A turn nobody comes back for is stopped after turnGrace, so a client
// that died doesn't leave it running; one a client did come back for
// isn't.
func TestAbandonedTurnIsStopped(t *testing.T) {
	old := turnGrace
	turnGrace = 150 * time.Millisecond
	t.Cleanup(func() { turnGrace = old })
	c, s, dir, id := askFirst(t)
	req := connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: id, Turn: &pb.Turn{Text: "make a file"}, TurnId: "turn-1"})
	ctx, drop := context.WithCancel(context.Background())
	stream, err := c.sessions.RunTurn(ctx, req)
	require.NoError(t, err)
	untilApproval(t, stream)
	drop()

	// Back within the wait: it goes on for as long as this stream stays.
	ctx, drop = context.WithCancel(context.Background())
	stream, err = c.sessions.RunTurn(ctx, req)
	require.NoError(t, err)
	untilApproval(t, stream)
	time.Sleep(3 * turnGrace)
	assert.True(t, busy(t, s, dir), "a turn with a stream isn't abandoned")

	drop()
	require.Eventually(t, func() bool { return !busy(t, s, dir) }, 5*time.Second, 10*time.Millisecond, "stopped once nobody came back")
	_, err = os.Stat(filepath.Join(dir, "made.txt"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// Closing the service stops its turns with an ID, though no call holds
// them.
func TestCloseStopsTurnsWithAnID(t *testing.T) {
	c, s, dir, id := askFirst(t)
	ctx, drop := context.WithCancel(context.Background())
	stream, err := c.sessions.RunTurn(ctx, connect.NewRequest(&pb.RunTurnRequest{Workspace: dir, SessionId: id, Turn: &pb.Turn{Text: "make a file"}, TurnId: "turn-1"}))
	require.NoError(t, err)
	untilApproval(t, stream)
	drop()
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		require.Fail(t, "Close waited on a turn it should have stopped")
	}
}

// A turn's events are kept up to a limit, the oldest let go: a caller
// that asks from before them is sent from the first kept, numbered as
// they were; one that asks from past the end waits for the next.
func TestLiveTurnKeepsItsLatestEvents(t *testing.T) {
	lt := &liveTurn{notify: map[chan struct{}]struct{}{}, cancel: func(error) {}, ended: true}
	for i := range keptRunEvents + 10 {
		lt.add(&pb.TurnEvent{Kind: &pb.TurnEvent_Text{Text: &pb.Text{Text: fmt.Sprint(i)}}})
	}
	lt.add(&pb.TurnEvent{Kind: &pb.TurnEvent_Finished{Finished: &pb.TurnFinished{}}})
	var first, last uint64
	n := 0
	require.NoError(t, lt.watch(context.Background(), newBroker(), 3, func(seq uint64, _ *pb.TurnEvent) error {
		if n == 0 {
			first = seq
		}
		last = seq
		n++
		return nil
	}))
	assert.Equal(t, keptRunEvents, n, "the latest, no more")
	assert.EqualValues(t, 12, first, "from the first kept, numbered as it was")
	assert.EqualValues(t, keptRunEvents+11, last)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := lt.watch(ctx, newBroker(), 1<<40, func(uint64, *pb.TurnEvent) error { return errors.New("nothing is sent") })
	assert.ErrorIs(t, err, context.DeadlineExceeded, "past the end: it waits")
	assert.ErrorContains(t, lt.watch(context.Background(), newBroker(), 0, func(uint64, *pb.TurnEvent) error { return errors.New("the stream is gone") }), "the stream is gone")
}

// An ended turn is forgotten after a while: asked for again by a caller
// that had some of it, it's unknown, not run again.
func TestEndedTurnIsForgotten(t *testing.T) {
	old := keptTurnEnd
	keptTurnEnd = 20 * time.Millisecond
	t.Cleanup(func() { keptTurnEnd = old })
	c, s := serve(t, nil, text("hello"))
	ctx := context.Background()
	dir := t.TempDir()
	sess, err := c.sessions.NewSession(ctx, connect.NewRequest(&pb.NewSessionRequest{Workspace: dir}))
	require.NoError(t, err)
	req := &pb.RunTurnRequest{Workspace: dir, SessionId: sess.Msg.Session.Id, Turn: &pb.Turn{Text: "hi"}, TurnId: "turn-1"}
	require.NoError(t, streamErr(c.sessions.RunTurn(ctx, connect.NewRequest(req))))
	require.Eventually(t, func() bool {
		s.turns.mu.Lock()
		defer s.turns.mu.Unlock()
		return len(s.turns.byID) == 0
	}, 5*time.Second, 5*time.Millisecond)
	req.AfterSeq = 2
	_, info := errorReason(t, streamErr(c.sessions.RunTurn(ctx, connect.NewRequest(req))))
	assert.Equal(t, "UNKNOWN_TURN", info.Reason)
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = c.sessions.StopTurn(cctx, connect.NewRequest(&pb.StopTurnRequest{Workspace: dir, TurnId: "turn-1"}))
	assert.Error(t, err)
}
