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

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Studio's editors (spec_040_studio): what they share.

func (h workspaceService) DraftManifest(ctx context.Context, r req[pb.DraftManifestRequest]) (*connect.Response[pb.DraftManifestResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	d, err := w.DraftManifest(ctx, engine.DraftRequest{
		Kind: r.Msg.Kind, Name: r.Msg.Name, Description: r.Msg.Description,
		Current: r.Msg.Current, Request: r.Msg.Request, Language: r.Msg.Language,
	})
	if err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.DraftManifestResponse{Text: d.Text, Changes: d.Changes, Problem: d.Problem})
}

func specStageMsg(st engine.SpecStage) *pb.SpecStage {
	m := &pb.SpecStage{
		Name: st.Name, Path: st.Path, Number: st.Number, Title: st.Title,
		Status: st.Status, Kind: st.Kind, DependsOn: st.DependsOn,
		Context: st.Context, Goals: st.Goals, NonGoals: st.NonGoals,
		DoneWhen: st.DoneWhen, OpenQuestions: st.OpenQuestions,
		Skills: st.Skills, Agents: st.Agents,
		DoneWhenDone: st.DoneWhenDone, Content: st.Content, Hash: st.Hash,
	}
	for _, it := range st.Items {
		m.Items = append(m.Items, &pb.SpecItem{Id: it.ID, Section: it.Section, Text: it.Text, Done: it.Done})
	}
	return m
}

func (h workspaceService) ListSpecs(ctx context.Context, r req[pb.ListSpecsRequest]) (*connect.Response[pb.ListSpecsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	sum := w.Specs()
	out := &pb.ListSpecsResponse{
		Dir: sum.Dir, Kind: sum.Kind, Overview: sum.Overview,
		HarnessStale: sum.HarnessStale,
	}
	for _, st := range sum.Stages {
		out.Stages = append(out.Stages, specStageMsg(st))
	}
	return ok(out)
}

func (h workspaceService) UpdateSpec(ctx context.Context, r req[pb.UpdateSpecRequest]) (*connect.Response[pb.UpdateSpecResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	st, err := w.ApplySpecUpdate(engine.SpecUpdate{
		Name:                    r.Msg.Name,
		Title:                   r.Msg.Title,
		Context:                 r.Msg.Context,
		Status:                  r.Msg.Status,
		SetDependsOn:            r.Msg.SetDependsOn,
		DependsOn:               r.Msg.DependsOn,
		ToggleItemID:            r.Msg.ToggleItemId,
		ItemDone:                r.Msg.ItemDone,
		ToggleDoneWhenIndex:     int(r.Msg.ToggleDoneWhenIndex),
		DoneWhenDone:            r.Msg.DoneWhenDone,
		AddRequirement:          r.Msg.AddRequirement,
		AddDoneWhen:             r.Msg.AddDoneWhen,
		AddOpenQuestion:         r.Msg.AddOpenQuestion,
		RemoveOpenQuestionIndex: int(r.Msg.RemoveOpenQuestionIndex),
		Content:                 r.Msg.Content,
		Hash:                    r.Msg.Hash,
	})
	switch {
	case errors.Is(err, engine.ErrUnknownSpec):
		return nil, apiError(connect.CodeNotFound, "SPEC_NOT_FOUND", err)
	case errors.Is(err, engine.ErrSpecChanged):
		return nil, apiError(connect.CodeFailedPrecondition, "HASH_MISMATCH", err)
	case errors.Is(err, engine.ErrBadSpecStatus), errors.Is(err, engine.ErrSpecCycle), errors.Is(err, engine.ErrBadSpecTitle):
		return nil, invalid(err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return ok(&pb.UpdateSpecResponse{Stage: specStageMsg(st)})
}
