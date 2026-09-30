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
	"errors"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/socket"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Background reaches the service's background runs, in every workspace
// (blitz agents, logs, stop).
type Background struct {
	c pb.SessionServiceClient
}

// AttachBackground reaches the background runs of the service listening
// on sock.
func AttachBackground(sock string) *Background {
	return AttachBackgroundHTTP(socket.Client(sock), socket.BaseURL)
}

// AttachBackgroundHTTP is AttachBackground over any HTTP client.
func AttachBackgroundHTTP(hc connect.HTTPClient, baseURL string) *Background {
	return &Background{c: pb.NewSessionServiceClient(hc, baseURL)}
}

// List returns the runs, newest first (SessionService.ListBackground).
func (b *Background) List(ctx context.Context) ([]api.BackgroundRun, error) {
	res, err := b.c.ListBackground(ctx, connect.NewRequest(&pb.ListBackgroundRequest{}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]api.BackgroundRun, len(res.Msg.Runs))
	for i, r := range res.Msg.Runs {
		out[i] = backgroundRun(r)
	}
	return out, nil
}

// Find returns the run with the ID (api.ErrUnknownRun).
func (b *Background) Find(ctx context.Context, id string) (api.BackgroundRun, error) {
	list, err := b.List(ctx)
	if err != nil {
		return api.BackgroundRun{}, err
	}
	for _, r := range list {
		if r.ID == id {
			return r, nil
		}
	}
	return api.BackgroundRun{}, api.ErrUnknownRun
}

// Stop stops a run and returns it as it ended (SessionService.StopBackground).
func (b *Background) Stop(ctx context.Context, id string) (api.BackgroundRun, error) {
	res, err := b.c.StopBackground(ctx, connect.NewRequest(&pb.StopBackgroundRequest{Id: id}))
	if err != nil {
		return api.BackgroundRun{}, fromAPI(err)
	}
	return backgroundRun(res.Msg.Run), nil
}

// Logs passes a run's events so far to on, and with follow those to come
// until it ends; it doesn't answer the run's requests. It returns the
// run's result once it has ended (SessionService.WatchBackground).
func (b *Background) Logs(ctx context.Context, id string, follow bool, on func(api.Event)) (api.TurnResult, bool, error) {
	stream, err := b.c.WatchBackground(ctx, connect.NewRequest(&pb.WatchBackgroundRequest{Id: id, Follow: follow}))
	if err != nil {
		return api.TurnResult{}, false, fromAPI(err)
	}
	defer stream.Close()
	for stream.Receive() {
		ev := stream.Msg().Event
		if f, ok := ev.Kind.(*pb.TurnEvent_Finished); ok {
			return finished(f.Finished), true, errorFromInfo(f.Finished.Error)
		}
		if e, ok := event(ev); ok {
			on(e)
		}
	}
	return api.TurnResult{}, false, fromAPI(stream.Err())
}

// StartBackground starts t in a new session of the workspace, run by the
// service with no client attached (SessionService.StartBackground).
func (r *Remote) StartBackground(ctx context.Context, t api.Turn) (api.BackgroundRun, error) {
	turn := &pb.Turn{
		Text: t.Text, Prompt: t.Prompt, Plan: t.Plan, ReadOnly: t.ReadOnly,
		MaxTurns: int32(t.MaxTurns), MaxCostUsd: t.MaxCostUSD, Command: t.Command,
	}
	if t.Timeout > 0 {
		turn.Timeout = durationpb.New(t.Timeout)
	}
	res, err := r.sessions.StartBackground(ctx, connect.NewRequest(&pb.StartBackgroundRequest{Workspace: r.dir, Turn: turn}))
	if err != nil {
		return api.BackgroundRun{}, fromAPI(err)
	}
	return backgroundRun(res.Msg.Run), nil
}

// FollowBackground shows a background run from its start until it ends,
// answering its waiting requests through the UI set with SetUI, and
// returns its result (blitz attach).
func (r *Remote) FollowBackground(ctx context.Context, id string, on func(api.Event)) (api.TurnResult, error) {
	stream, err := r.sessions.WatchBackground(ctx, connect.NewRequest(&pb.WatchBackgroundRequest{Id: id, Follow: true}))
	if err != nil {
		return api.TurnResult{}, fromAPI(err)
	}
	defer stream.Close()
	r.mu.Lock()
	approve, ask := r.approve, r.ask
	r.mu.Unlock()
	for stream.Receive() {
		ev := stream.Msg().Event
		switch k := ev.Kind.(type) {
		case *pb.TurnEvent_ApprovalRequest:
			r.answerApproval(ctx, approve, k.ApprovalRequest)
		case *pb.TurnEvent_Question:
			r.answerQuestion(ctx, ask, k.Question)
		case *pb.TurnEvent_Finished:
			return finished(k.Finished), errorFromInfo(k.Finished.Error)
		default:
			if e, ok := event(ev); ok {
				on(e)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return api.TurnResult{}, fromAPI(err)
	}
	return api.TurnResult{}, errors.New("the service stopped showing the run before it ended")
}

func finished(f *pb.TurnFinished) api.TurnResult {
	return api.TurnResult{Output: f.Output, Before: usage(f.Before), After: usage(f.After), Leftover: f.Leftover}
}

func backgroundRun(r *pb.BackgroundRun) api.BackgroundRun {
	out := api.BackgroundRun{
		ID: r.Id, Workspace: r.Workspace, SessionID: r.SessionId, Prompt: r.Prompt, State: r.State,
		Started: r.Started.AsTime(), CostUSD: r.CostUsd, Waiting: int(r.Waiting), Error: r.Error,
	}
	if r.Ended != nil {
		out.Ended = r.Ended.AsTime()
	}
	return out
}
