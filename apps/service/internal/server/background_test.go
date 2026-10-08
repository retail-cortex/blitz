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

// watchAll collects a background run's events (to its end with follow).
func watchAll(t *testing.T, c clients, id string, follow bool) []*pb.TurnEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := c.sessions.WatchBackground(ctx, connect.NewRequest(&pb.WatchBackgroundRequest{Id: id, Follow: follow}))
	require.NoError(t, err)
	defer stream.Close()
	var evs []*pb.TurnEvent
	for stream.Receive() {
		evs = append(evs, stream.Msg().Event)
	}
	require.NoError(t, stream.Err())
	return evs
}

// runState polls the list until the run is in one of states.
func runState(t *testing.T, c clients, id string, states ...string) *pb.BackgroundRun {
	t.Helper()
	var last *pb.BackgroundRun
	require.Eventually(t, func() bool {
		res, err := c.sessions.ListBackground(context.Background(), connect.NewRequest(&pb.ListBackgroundRequest{}))
		require.NoError(t, err)
		for _, r := range res.Msg.Runs {
			if r.Id == id {
				last = r
				for _, s := range states {
					if r.State == s {
						return true
					}
				}
			}
		}
		return false
	}, 10*time.Second, 10*time.Millisecond, "run %s never became %v: %v", id, states, last)
	return last
}

func TestBackgroundRuns(t *testing.T) {
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	c, s := serve(t, func(cfg *config.Config) { cfg.Blitz.AutoApprove = false }, create, text("made it"))
	dir := t.TempDir()
	ctx := context.Background()

	_, err := c.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: dir, Turn: &pb.Turn{Text: " "}}))
	_, info := errorReason(t, err)
	assert.Equal(t, "INVALID_TURN", info.Reason)

	res, err := c.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: dir, Turn: &pb.Turn{Text: "make a file\nwith more"}}))
	require.NoError(t, err)
	run := res.Msg.Run
	assert.Equal(t, "bg-1", run.Id)
	assert.Equal(t, "make a file", run.Prompt)
	assert.NotEmpty(t, run.SessionId)

	// It waits for its approval, which a watcher sees.
	waiting := runState(t, c, run.Id, "waiting")
	assert.EqualValues(t, 1, waiting.Waiting)
	evs := watchAll(t, c, run.Id, false)
	var request string
	for _, ev := range evs {
		if ar := ev.GetApprovalRequest(); ar != nil {
			request = ar.RequestId
		}
	}
	require.NotEmpty(t, request, "the waiting request isn't shown: %v", evs)

	// The run's session isn't the workspace's active one.
	active, err := c.sessions.GetActiveSession(ctx, connect.NewRequest(&pb.GetActiveSessionRequest{Workspace: dir}))
	if err == nil {
		assert.NotEqual(t, run.SessionId, active.Msg.GetSession().GetId())
	}

	_, err = c.sessions.Approve(ctx, connect.NewRequest(&pb.ApproveRequest{Workspace: dir, RequestId: request, Decision: pb.Decision_DECISION_ONCE}))
	require.NoError(t, err)
	done := runState(t, c, run.Id, "done")
	assert.NotNil(t, done.Ended)
	// Ended, it lets its workspace go (closing it frees its sessions).
	r, err := s.run(run.Id)
	require.NoError(t, err)
	r.mu.Lock()
	assert.Nil(t, r.w)
	r.mu.Unlock()
	_, err = os.Stat(filepath.Join(dir, "made.txt"))
	assert.NoError(t, err, "the approved change wasn't made")

	// Watched after it ended: everything but the answered request, then
	// the end.
	evs = watchAll(t, c, run.Id, true)
	require.NotEmpty(t, evs)
	var texts []string
	for _, ev := range evs {
		assert.Nil(t, ev.GetApprovalRequest(), "an answered request was shown again")
		if tx := ev.GetText(); tx != nil && !tx.Partial {
			texts = append(texts, tx.Text)
		}
	}
	assert.Contains(t, texts, "made it")
	assert.NotNil(t, evs[len(evs)-1].GetFinished())

	_, err = c.sessions.StopBackground(ctx, connect.NewRequest(&pb.StopBackgroundRequest{Id: "bg-9"}))
	_, info = errorReason(t, err)
	assert.Equal(t, "UNKNOWN_RUN", info.Reason)
	stream, err := c.sessions.WatchBackground(ctx, connect.NewRequest(&pb.WatchBackgroundRequest{Id: "bg-9"}))
	require.NoError(t, err) // a stream reports its error on receiving
	assert.False(t, stream.Receive())
	_, info = errorReason(t, stream.Err())
	assert.Equal(t, "UNKNOWN_RUN", info.Reason)
	stream.Close()
}

func TestStoppingABackgroundRun(t *testing.T) {
	create := call("create_file", map[string]any{"path": "made.txt", "content": "hi\n"})
	c, s := serve(t, func(cfg *config.Config) { cfg.Blitz.AutoApprove = false }, create, text("made it"))
	dir := t.TempDir()
	ctx := context.Background()
	res, err := c.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: dir, Turn: &pb.Turn{Text: "make a file"}}))
	require.NoError(t, err)
	id := res.Msg.Run.Id
	runState(t, c, id, "waiting")

	// A follower sees it end when it's stopped.
	followed := make(chan []*pb.TurnEvent, 1)
	go func() { followed <- watchAll(t, c, id, true) }()
	stopped, err := c.sessions.StopBackground(ctx, connect.NewRequest(&pb.StopBackgroundRequest{Id: id}))
	require.NoError(t, err)
	assert.Equal(t, "stopped", stopped.Msg.Run.State)
	evs := <-followed
	require.NotEmpty(t, evs)
	assert.NotNil(t, evs[len(evs)-1].GetFinished())
	_, err = os.Stat(filepath.Join(dir, "made.txt"))
	assert.True(t, os.IsNotExist(err), "a stopped run made its change")

	// Closing the service stops the rest.
	res, err = c.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: dir, Turn: &pb.Turn{Text: "again"}}))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	r, err := s.run(res.Msg.Run.Id)
	require.NoError(t, err)
	<-r.done
	_, err = c.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: dir, Turn: &pb.Turn{Text: "more"}}))
	assert.Error(t, err, "started after closing")
}

func TestEndedRunsAreBounded(t *testing.T) {
	var rs runs
	for i := range keptEndedRuns + 5 {
		r := &bgRun{id: string(rune('a' + i)), done: make(chan struct{})}
		close(r.done)
		rs.list = append(rs.list, r)
	}
	running := &bgRun{id: "running", done: make(chan struct{})}
	rs.list = append([]*bgRun{running}, rs.list...)
	rs.prune()
	assert.Len(t, rs.list, keptEndedRuns+1)
	assert.Equal(t, "running", rs.list[0].id, "a running run is kept")
	assert.Equal(t, string(rune('a'+5)), rs.list[1].id, "the oldest ended ones go")
}

func TestRunEventsAreBounded(t *testing.T) {
	r := &bgRun{notify: map[chan struct{}]struct{}{}}
	for range keptRunEvents + 10 {
		r.add(&pb.TurnEvent{Kind: &pb.TurnEvent_Accepted{Accepted: &pb.Accepted{}}})
	}
	assert.Len(t, r.events, keptRunEvents)
	assert.Equal(t, 10, r.dropped)
}
