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
	"github.com/retail-cortex/blitz/pkg/api"
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

// ListSpecs is the project's specification (spec_057_spec_driven SDD-01).
func (h workspaceService) ListSpecs(ctx context.Context, r req[pb.ListSpecsRequest]) (*connect.Response[pb.ListSpecsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	list, err := w.ListSpecs()
	if err != nil {
		return nil, fileError(err)
	}
	out := &pb.ListSpecsResponse{}
	for _, s := range list {
		deps := make([]int32, len(s.DependsOn))
		for i, d := range s.DependsOn {
			deps[i] = int32(d)
		}
		out.Specs = append(out.Specs, &pb.SpecSummary{
			File: s.File, Path: s.Path, Number: int32(s.Number), Title: s.Title, Status: s.Status, DependsOn: deps,
			Items: int32(s.Items), Errors: int32(s.Errors), Warnings: int32(s.Warnings), OpenQuestions: int32(s.OpenQuestions),
		})
	}
	return ok(out)
}

// ReviewSpec reviews a spec as edited (SDD-02, SDD-03).
func (h workspaceService) ReviewSpec(ctx context.Context, r req[pb.ReviewSpecRequest]) (*connect.Response[pb.ReviewSpecResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	rev, err := w.ReviewSpec(ctx, r.Msg.Path, r.Msg.Text, r.Msg.Judge)
	if errors.Is(err, engine.ErrNotASpec) {
		return nil, invalid(err)
	}
	if err != nil {
		return nil, fileError(err)
	}
	return ok(specReview(rev))
}

// specReview is a review on the wire.
func specReview(rev api.SpecReview) *pb.ReviewSpecResponse {
	out := &pb.ReviewSpecResponse{CostUsd: rev.CostUSD, Problem: rev.Problem}
	for _, f := range rev.Findings {
		out.Findings = append(out.Findings, &pb.SpecFinding{Line: int32(f.Line), Id: f.ID, Severity: f.Severity, Code: f.Code, Message: f.Message, Judged: f.Judged})
	}
	return out
}
