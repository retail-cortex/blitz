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
	"google.golang.org/protobuf/types/known/timestamppb"
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
		out.Specs = append(out.Specs, &pb.SpecSummary{
			File: s.File, Path: s.Path, Number: int32(s.Number), Title: s.Title, Status: s.Status, DependsOn: ints32(s.DependsOn),
			Items: int32(s.Items), Errors: int32(s.Errors), Warnings: int32(s.Warnings), OpenQuestions: int32(s.OpenQuestions),
			Approved: s.Approved, Traced: int32(s.Traced), Implemented: int32(s.Implemented), Changed: int32(s.Changed),
			Done: s.Done, Next: s.Next, BlockedBy: ints32(s.BlockedBy),
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
	if err != nil {
		return nil, specError(err)
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

// specError is a spec call's error on the wire.
func specError(err error) error {
	switch {
	case errors.Is(err, engine.ErrNotASpec):
		return invalid(err)
	case errors.Is(err, engine.ErrSpecNotReady):
		return apiError(connect.CodeFailedPrecondition, "SPEC_NOT_READY", err)
	}
	return fileError(err)
}

// specRequirements are requirements on the wire.
func specRequirements(list []api.SpecRequirement) []*pb.SpecRequirement {
	out := make([]*pb.SpecRequirement, len(list))
	for i, r := range list {
		m := &pb.SpecRequirement{Spec: r.Spec, Id: r.ID, Text: r.Text, Line: int32(r.Line), Change: r.Change, State: r.State, Judged: r.Judged, Reason: r.Reason, Stale: r.Stale}
		if !r.VerifiedAt.IsZero() {
			m.VerifiedAt = timestamppb.New(r.VerifiedAt)
		}
		for _, e := range r.Evidence {
			m.Evidence = append(m.Evidence, &pb.SpecEvidence{Path: e.Path, Line: int32(e.Line), Kind: e.Kind, Text: e.Text})
		}
		out[i] = m
	}
	return out
}

// TraceSpecs is a spec's requirements as traced (SDD-10).
func (h workspaceService) TraceSpecs(ctx context.Context, r req[pb.TraceSpecsRequest]) (*connect.Response[pb.TraceSpecsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	list, err := w.TraceSpecs(r.Msg.Path)
	if err != nil {
		return nil, specError(err)
	}
	return ok(&pb.TraceSpecsResponse{Requirements: specRequirements(list)})
}

// ApproveSpec approves a saved spec (SDD-11).
func (h workspaceService) ApproveSpec(ctx context.Context, r req[pb.ApproveSpecRequest]) (*connect.Response[pb.ApproveSpecResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	a, err := w.ApproveSpec(ctx, r.Msg.Path, r.Msg.Force)
	if err != nil {
		return nil, specError(err)
	}
	return ok(&pb.ApproveSpecResponse{Text: a.Text, Diff: a.Diff})
}

// VerifySpec verifies a saved spec against the work (SDD-12).
func (h workspaceService) VerifySpec(ctx context.Context, r req[pb.VerifySpecRequest]) (*connect.Response[pb.VerifySpecResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	v, err := w.VerifySpec(ctx, r.Msg.Path, r.Msg.Judge, r.Msg.Estimate)
	if err != nil {
		return nil, specError(err)
	}
	out := &pb.VerifySpecResponse{Requirements: specRequirements(v.Requirements), CostUsd: v.CostUSD, EstimatedUsd: v.EstimatedUSD, Priced: v.Priced, Problem: v.Problem}
	for _, d := range v.Drift {
		out.Drift = append(out.Drift, &pb.SpecDrift{Id: d.ID, Why: d.Why})
	}
	return ok(out)
}

// specChanges are changes on the wire.
func specChanges(list []api.SpecChange) []*pb.SpecChange {
	out := make([]*pb.SpecChange, len(list))
	for i, c := range list {
		out[i] = &pb.SpecChange{Id: c.ID, Kind: c.Kind, Old: c.Old, New: c.New}
	}
	return out
}

// RecordSpecChange records a saved change to an approved spec (SDD-20).
func (h workspaceService) RecordSpecChange(ctx context.Context, r req[pb.RecordSpecChangeRequest]) (*connect.Response[pb.RecordSpecChangeResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	rec, err := w.RecordSpecChange(ctx, r.Msg.Path)
	if err != nil {
		return nil, specError(err)
	}
	return ok(&pb.RecordSpecChangeResponse{Changes: specChanges(rec.Changes), Diff: rec.Diff, Text: rec.Text})
}

// ImpactSpec scans a spec's changes' impact on the work (SDD-20, SDD-21).
func (h workspaceService) ImpactSpec(ctx context.Context, r req[pb.ImpactSpecRequest]) (*connect.Response[pb.ImpactSpecResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	im, err := w.ImpactSpec(ctx, r.Msg.Path, r.Msg.Judge, r.Msg.Estimate)
	if err != nil {
		return nil, specError(err)
	}
	out := &pb.ImpactSpecResponse{Changes: specChanges(im.Changes), Summary: im.Summary, Report: im.Report, CostUsd: im.CostUSD, EstimatedUsd: im.EstimatedUSD, Priced: im.Priced, Problem: im.Problem}
	for _, s := range im.Symbols {
		out.Symbols = append(out.Symbols, &pb.SpecSymbol{Name: s.Name, Path: s.Path, Line: int32(s.Line), Uses: int32(s.Uses)})
	}
	for _, p := range im.Places {
		out.Places = append(out.Places, &pb.SpecPlace{Path: p.Path, Line: int32(p.Line), Kind: p.Kind, Text: p.Text, Why: p.Why})
	}
	for _, st := range im.Steps {
		out.Steps = append(out.Steps, &pb.SpecStep{Title: st.Title, Detail: st.Detail, Files: st.Files})
	}
	for _, c := range im.Conflicts {
		out.Conflicts = append(out.Conflicts, &pb.SpecConflict{Id: c.ID, Why: c.Why})
	}
	return ok(out)
}

// ints32 are ints on the wire.
func ints32(list []int) []int32 {
	out := make([]int32, len(list))
	for i, n := range list {
		out[i] = int32(n)
	}
	return out
}

// PlanSpec is how a spec would be implemented (SDD-30).
func (h workspaceService) PlanSpec(ctx context.Context, r req[pb.PlanSpecRequest]) (*connect.Response[pb.PlanSpecResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	p, err := w.PlanSpec(r.Msg.Path)
	if err != nil {
		return nil, specError(err)
	}
	return ok(&pb.PlanSpecResponse{Pending: specRequirements(p.Pending), BlockedBy: ints32(p.BlockedBy), Prompt: p.Prompt})
}
