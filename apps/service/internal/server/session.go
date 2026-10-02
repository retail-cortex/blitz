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
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/images"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// sessionService implements SessionService.
type sessionService struct{ s *Server }

func (h sessionService) ListSessions(ctx context.Context, r req[pb.ListSessionsRequest]) (*connect.Response[pb.ListSessionsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	list, err := w.ListSessions(r.Msg.All)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.ListSessionsResponse{Sessions: sessionMsgs(list)})
}

func (h sessionService) GetActiveSession(ctx context.Context, r req[pb.GetActiveSessionRequest]) (*connect.Response[pb.GetActiveSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.GetActiveSessionResponse{}
	if s, found := w.ActiveSession(); found {
		out.Session = sessionMsg(s)
	}
	return ok(out)
}

func (h sessionService) NewSession(ctx context.Context, r req[pb.NewSessionRequest]) (*connect.Response[pb.NewSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, err := w.NewSession()
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.NewSessionResponse{Session: sessionMsg(s)})
}

func (h sessionService) OpenSession(ctx context.Context, r req[pb.OpenSessionRequest]) (*connect.Response[pb.OpenSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, resumed, err := w.OpenSession(r.Msg.Resume, r.Msg.ContinueLatest)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.OpenSessionResponse{Session: sessionMsg(s), Resumed: resumed})
}

func (h sessionService) LoadSession(ctx context.Context, r req[pb.LoadSessionRequest]) (*connect.Response[pb.LoadSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, branched, err := w.LoadSession(r.Msg.Ref)
	if err != nil {
		return nil, apiError(connect.CodeNotFound, "SESSION_NOT_FOUND", err, "ref", r.Msg.Ref)
	}
	return ok(&pb.LoadSessionResponse{Session: sessionMsg(s), Branched: branched})
}

func (h sessionService) SaveSnapshot(ctx context.Context, r req[pb.SaveSnapshotRequest]) (*connect.Response[pb.SaveSnapshotResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, err := w.SaveSnapshot(r.Msg.Name, r.Msg.Force)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.SaveSnapshotResponse{Snapshot: sessionMsg(s)})
}

func (h sessionService) MoveSession(ctx context.Context, r req[pb.MoveSessionRequest]) (*connect.Response[pb.MoveSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, err := w.MoveSession(r.Msg.SessionId)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.MoveSessionResponse{Session: sessionMsg(s)})
}

func (h sessionService) RenameSession(ctx context.Context, r req[pb.RenameSessionRequest]) (*connect.Response[pb.RenameSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, err := w.RenameSession(r.Msg.Title)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.RenameSessionResponse{Session: sessionMsg(s)})
}

func (h sessionService) DeleteSession(ctx context.Context, r req[pb.DeleteSessionRequest]) (*connect.Response[pb.DeleteSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.DeleteSession(ctx, r.Msg.SessionId); err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.DeleteSessionResponse{})
}

func (h sessionService) ForkSession(ctx context.Context, r req[pb.ForkSessionRequest]) (*connect.Response[pb.ForkSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, err := w.ForkSession(ctx, int(r.Msg.Turn))
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.ForkSessionResponse{Session: sessionMsg(s)})
}

func (h sessionService) ExportSession(ctx context.Context, r req[pb.ExportSessionRequest]) (*connect.Response[pb.ExportSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	md, err := w.ExportSession(r.Msg.SessionId)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.ExportSessionResponse{Markdown: md})
}

// RunTurn runs a turn and streams its events. Events are sent from one
// goroutine at a time (the send lock), since the agent and approval
// requests can produce them concurrently.
func (h sessionService) RunTurn(ctx context.Context, r req[pb.RunTurnRequest], stream *connect.ServerStream[pb.RunTurnResponse]) error {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return err
	}
	t := r.Msg.Turn
	if t == nil {
		return apiError(connect.CodeInvalidArgument, "INVALID_TURN", errors.New("turn is required"))
	}
	var imgs []*images.Image
	for _, id := range t.ImageIds {
		img, err := w.image(id)
		if err != nil {
			return err
		}
		imgs = append(imgs, img)
	}

	var mu sync.Mutex
	var sendErr error
	send := func(ev *pb.TurnEvent) {
		mu.Lock()
		defer mu.Unlock()
		if sendErr == nil {
			sendErr = stream.Send(&pb.RunTurnResponse{Event: ev})
		}
	}
	ctx = withSink(ctx, send)
	res, runErr := w.Run(ctx, r.Msg.SessionId, api.Turn{
		Text: t.Text, Prompt: t.Prompt, Plan: t.Plan, ReadOnly: t.ReadOnly, Aside: t.Aside, Accepted: t.Accepted,
		Images: imgs, MaxTurns: int(t.MaxTurns), FetchGrants: t.FetchGrants,
		MaxCostUSD: t.MaxCostUsd, Timeout: t.GetTimeout().AsDuration(), Command: t.Command,
		OnAccepted: func() { send(&pb.TurnEvent{Kind: &pb.TurnEvent_Accepted{Accepted: &pb.Accepted{}}}) },
	}, func(e api.Event) { send(eventMsg(e)) })

	send(&pb.TurnEvent{Kind: &pb.TurnEvent_Finished{Finished: &pb.TurnFinished{
		Output: res.Output, Before: usageMsg(res.Before), After: usageMsg(res.After), Leftover: res.Leftover, Error: errorInfo(runErr),
	}}})
	if sendErr != nil {
		return fmt.Errorf("sending turn events: %w", sendErr)
	}
	return nil
}

func (h sessionService) Steer(ctx context.Context, r req[pb.SteerRequest]) (*connect.Response[pb.SteerResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.Steer(ctx, r.Msg.SessionId, r.Msg.Text); err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.SteerResponse{})
}

func (h sessionService) GetUsage(ctx context.Context, r req[pb.GetUsageRequest]) (*connect.Response[pb.GetUsageResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	u, err := w.SessionUsage()
	if err != nil {
		return nil, toAPI(err)
	}
	c, err := w.Context(r.Msg.Parts)
	if err != nil {
		return nil, toAPI(err)
	}
	res := &pb.GetUsageResponse{Usage: usageMsg(u), AutoCompact: c.AutoCompact, Threshold: int32(c.Threshold), Keep: int32(c.Keep)}
	for _, p := range c.Parts {
		res.Parts = append(res.Parts, &pb.ContextPart{Name: p.Name, Tokens: p.Tokens})
	}
	return ok(res)
}

func (h sessionService) Compact(ctx context.Context, r req[pb.CompactRequest]) (*connect.Response[pb.CompactResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.Compact(ctx, r.Msg.Focus)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.CompactResponse{EventsCompacted: int32(res.EventsCompacted), SummaryChars: int32(res.SummaryChars), Before: usageMsg(res.Before), After: usageMsg(res.After)})
}

func goalMsg(g api.Goal) *pb.Goal {
	return &pb.Goal{Condition: g.Condition, Continues: int32(g.Continues), Max: int32(g.Max), Last: g.Last}
}

func (h sessionService) SetGoal(ctx context.Context, r req[pb.SetGoalRequest]) (*connect.Response[pb.SetGoalResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	g, err := w.SetGoal(r.Msg.Condition)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.SetGoalResponse{Goal: goalMsg(g)})
}

func (h sessionService) GetGoal(ctx context.Context, r req[pb.GetGoalRequest]) (*connect.Response[pb.GetGoalResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	g, err := w.Goal()
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.GetGoalResponse{Goal: goalMsg(g)})
}

func (h sessionService) ClearGoal(ctx context.Context, r req[pb.ClearGoalRequest]) (*connect.Response[pb.ClearGoalResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.ClearGoal(); err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.ClearGoalResponse{})
}

func (h sessionService) SearchSession(ctx context.Context, r req[pb.SearchSessionRequest]) (*connect.Response[pb.SearchSessionResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	found, prompt := w.SearchSession(r.Msg.Terms)
	return ok(&pb.SearchSessionResponse{Found: int32(found), Prompt: prompt})
}

func (h sessionService) Approve(ctx context.Context, r req[pb.ApproveRequest]) (*connect.Response[pb.ApproveResponse], error) {
	if r.Msg.Decision == pb.Decision_DECISION_UNSPECIFIED {
		return nil, apiError(connect.CodeInvalidArgument, "INVALID_DECISION", errors.New("decision is required"))
	}
	if err := h.answer(ctx, r.Msg.Workspace, r.Msg.RequestId, reply{decision: decision(r.Msg.Decision)}); err != nil {
		return nil, err
	}
	return ok(&pb.ApproveResponse{})
}

func (h sessionService) Answer(ctx context.Context, r req[pb.AnswerRequest]) (*connect.Response[pb.AnswerResponse], error) {
	if err := h.answer(ctx, r.Msg.Workspace, r.Msg.RequestId, reply{text: r.Msg.Answer}); err != nil {
		return nil, err
	}
	return ok(&pb.AnswerResponse{})
}

// answer delivers a reply to a running turn's request, or else to one of
// the workspace's background tasks' requests.
func (h sessionService) answer(ctx context.Context, workspace, id string, rep reply) error {
	err := h.s.broker.answer(id, rep)
	if err == nil || workspace == "" {
		return err
	}
	w, werr := h.s.workspace(ctx, workspace)
	if werr != nil {
		return err
	}
	if terr := w.AnswerTaskRequest(id, rep.decision, rep.text); terr == nil {
		return nil
	}
	return err
}

func (h sessionService) ListRewindPoints(ctx context.Context, r req[pb.ListRewindPointsRequest]) (*connect.Response[pb.ListRewindPointsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	points, err := w.RewindPoints()
	if err != nil {
		return nil, toAPI(err)
	}
	out := &pb.ListRewindPointsResponse{}
	for _, p := range points {
		out.Points = append(out.Points, &pb.RewindPoint{Index: int32(p.Index), Text: p.Text, Time: timestamppb.New(p.Time), Files: p.Files, Conversation: p.Conversation})
	}
	return ok(out)
}

func (h sessionService) Rewind(ctx context.Context, r req[pb.RewindRequest]) (*connect.Response[pb.RewindResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.Rewind(ctx, int(r.Msg.Index), api.RewindMode(r.Msg.Mode), r.Msg.Force)
	if err != nil {
		return nil, toAPI(err)
	}
	c := res.Compacted
	return ok(&pb.RewindResponse{Mode: string(res.Mode), Restored: res.Restored, Prompt: res.Prompt,
		Compacted: &pb.CompactResponse{EventsCompacted: int32(c.EventsCompacted), SummaryChars: int32(c.SummaryChars), Before: usageMsg(c.Before), After: usageMsg(c.After)}})
}
