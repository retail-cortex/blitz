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

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// ProjectSettings are the workspace's project settings in the service
// (WorkspaceService.GetProjectSettings); on failure warn is told and they
// are empty, in state TrustUnknown (not "nothing to trust").
func (r *Remote) ProjectSettings() api.ProjectSettings {
	res, err := r.workspaces.GetProjectSettings(context.Background(), connect.NewRequest(&pb.GetProjectSettingsRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("reading the project settings", err)
		return api.ProjectSettings{State: api.TrustUnknown}
	}
	return projectSettings(res.Msg.Settings)
}

// TrustProject records the decision in the service, which reopens the
// workspace with it unless a turn is running there
// (WorkspaceService.TrustProject).
func (r *Remote) TrustProject(hash string, trusted bool) error {
	_, err := r.workspaces.TrustProject(context.Background(), connect.NewRequest(&pb.TrustProjectRequest{Workspace: r.dir, Hash: hash, Trusted: trusted}))
	return fromAPI(err)
}

// ForgetProjectTrust forgets the decision about the project settings in
// the service, which reopens the workspace without them unless a turn is
// running there (WorkspaceService.ForgetProjectTrust).
func (r *Remote) ForgetProjectTrust() error {
	_, err := r.workspaces.ForgetProjectTrust(context.Background(), connect.NewRequest(&pb.ForgetProjectTrustRequest{Workspace: r.dir}))
	return fromAPI(err)
}

func projectSettings(m *pb.ProjectSettings) api.ProjectSettings {
	if m == nil {
		return api.ProjectSettings{State: api.TrustNone}
	}
	list := func(in []*pb.ProjectItem) []api.ProjectItem {
		out := make([]api.ProjectItem, len(in))
		for i, it := range in {
			out[i] = api.ProjectItem{File: it.File, Kind: it.Kind, Key: it.Key, Value: it.Value, Reason: it.Reason}
		}
		return out
	}
	return api.ProjectSettings{
		Files: m.Files, State: m.State, Hash: m.Hash, Loaded: m.Loaded,
		Applied: list(m.Applied), Pending: list(m.Pending), Ignored: list(m.Ignored), Problems: m.Problems,
	}
}

// ListNotes are the notes the agent saved in the workspace
// (WorkspaceService.ListNotes).
func (r *Remote) ListNotes() ([]api.Note, error) {
	res, err := r.workspaces.ListNotes(context.Background(), connect.NewRequest(&pb.ListNotesRequest{Workspace: r.dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]api.Note, len(res.Msg.Notes))
	for i, n := range res.Msg.Notes {
		out[i] = api.Note{Name: n.Name, Kind: n.Kind, Text: n.Text, Time: n.Time.AsTime(), Path: n.Path}
	}
	return out, nil
}

// ListSpecs is the project's specification, in order
// (WorkspaceService.ListSpecs).
func (r *Remote) ListSpecs() ([]api.SpecSummary, error) {
	res, err := r.workspaces.ListSpecs(context.Background(), connect.NewRequest(&pb.ListSpecsRequest{Workspace: r.dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]api.SpecSummary, len(res.Msg.Specs))
	for i, s := range res.Msg.Specs {
		out[i] = api.SpecSummary{File: s.File, Path: s.Path, Number: int(s.Number), Title: s.Title, Status: s.Status, DependsOn: ints(s.DependsOn),
			Items: int(s.Items), Errors: int(s.Errors), Warnings: int(s.Warnings), OpenQuestions: int(s.OpenQuestions),
			Approved: s.Approved, Traced: int(s.Traced), Implemented: int(s.Implemented), Changed: int(s.Changed),
			Done: s.Done, Next: s.Next, BlockedBy: ints(s.BlockedBy)}
	}
	return out, nil
}

// ReviewSpec reviews a spec as edited (WorkspaceService.ReviewSpec).
func (r *Remote) ReviewSpec(ctx context.Context, path, text string, judge bool) (api.SpecReview, error) {
	res, err := r.workspaces.ReviewSpec(ctx, connect.NewRequest(&pb.ReviewSpecRequest{Workspace: r.dir, Path: path, Text: text, Judge: judge}))
	if err != nil {
		return api.SpecReview{}, fromAPI(err)
	}
	out := api.SpecReview{CostUSD: res.Msg.CostUsd, Problem: res.Msg.Problem}
	for _, f := range res.Msg.Findings {
		out.Findings = append(out.Findings, api.SpecFinding{Line: int(f.Line), ID: f.Id, Severity: f.Severity, Code: f.Code, Message: f.Message, Judged: f.Judged})
	}
	return out, nil
}

// specRequirements are requirements from the wire.
func specRequirements(list []*pb.SpecRequirement) []api.SpecRequirement {
	out := make([]api.SpecRequirement, len(list))
	for i, r := range list {
		out[i] = api.SpecRequirement{Spec: r.Spec, ID: r.Id, Text: r.Text, Line: int(r.Line), Change: r.Change, State: r.State, Judged: r.Judged, Reason: r.Reason, Stale: r.Stale}
		if r.VerifiedAt != nil {
			out[i].VerifiedAt = r.VerifiedAt.AsTime()
		}
		for _, e := range r.Evidence {
			out[i].Evidence = append(out[i].Evidence, api.SpecEvidence{Path: e.Path, Line: int(e.Line), Kind: e.Kind, Text: e.Text})
		}
	}
	return out
}

// TraceSpecs is a spec's requirements as traced
// (WorkspaceService.TraceSpecs).
func (r *Remote) TraceSpecs(path string) ([]api.SpecRequirement, error) {
	res, err := r.workspaces.TraceSpecs(context.Background(), connect.NewRequest(&pb.TraceSpecsRequest{Workspace: r.dir, Path: path}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return specRequirements(res.Msg.Requirements), nil
}

// ApproveSpec approves a saved spec (WorkspaceService.ApproveSpec).
func (r *Remote) ApproveSpec(ctx context.Context, path string, force bool) (api.SpecApproved, error) {
	res, err := r.workspaces.ApproveSpec(ctx, connect.NewRequest(&pb.ApproveSpecRequest{Workspace: r.dir, Path: path, Force: force}))
	if err != nil {
		return api.SpecApproved{}, fromAPI(err)
	}
	return api.SpecApproved{Text: res.Msg.Text, Diff: res.Msg.Diff}, nil
}

// VerifySpec verifies a saved spec (WorkspaceService.VerifySpec).
func (r *Remote) VerifySpec(ctx context.Context, path string, judge, estimate bool) (api.SpecVerification, error) {
	res, err := r.workspaces.VerifySpec(ctx, connect.NewRequest(&pb.VerifySpecRequest{Workspace: r.dir, Path: path, Judge: judge, Estimate: estimate}))
	if err != nil {
		return api.SpecVerification{}, fromAPI(err)
	}
	out := api.SpecVerification{Requirements: specRequirements(res.Msg.Requirements), CostUSD: res.Msg.CostUsd, EstimatedUSD: res.Msg.EstimatedUsd, Priced: res.Msg.Priced, Problem: res.Msg.Problem}
	for _, d := range res.Msg.Drift {
		out.Drift = append(out.Drift, api.SpecDrift{ID: d.Id, Why: d.Why})
	}
	return out, nil
}

// ints are ints from the wire.
func ints(list []int32) []int {
	out := make([]int, len(list))
	for i, n := range list {
		out[i] = int(n)
	}
	return out
}

// PlanSpec is how a spec would be implemented (WorkspaceService.PlanSpec).
func (r *Remote) PlanSpec(path string) (api.SpecPlan, error) {
	res, err := r.workspaces.PlanSpec(context.Background(), connect.NewRequest(&pb.PlanSpecRequest{Workspace: r.dir, Path: path}))
	if err != nil {
		return api.SpecPlan{}, fromAPI(err)
	}
	return api.SpecPlan{Pending: specRequirements(res.Msg.Pending), BlockedBy: ints(res.Msg.BlockedBy), Prompt: res.Msg.Prompt}, nil
}

// fromSpecChanges are changes from the wire.
func fromSpecChanges(list []*pb.SpecChange) []api.SpecChange {
	out := make([]api.SpecChange, len(list))
	for i, c := range list {
		out[i] = api.SpecChange{ID: c.Id, Kind: c.Kind, Old: c.Old, New: c.New}
	}
	return out
}

// RecordSpecChange records a saved change to an approved spec
// (WorkspaceService.RecordSpecChange).
func (r *Remote) RecordSpecChange(ctx context.Context, path string) (api.SpecRecorded, error) {
	res, err := r.workspaces.RecordSpecChange(ctx, connect.NewRequest(&pb.RecordSpecChangeRequest{Workspace: r.dir, Path: path}))
	if err != nil {
		return api.SpecRecorded{}, fromAPI(err)
	}
	return api.SpecRecorded{Changes: fromSpecChanges(res.Msg.Changes), Diff: res.Msg.Diff, Text: res.Msg.Text}, nil
}

// ImpactSpec scans a spec's changes' impact (WorkspaceService.ImpactSpec).
func (r *Remote) ImpactSpec(ctx context.Context, path string, judge, estimate bool) (api.SpecImpact, error) {
	res, err := r.workspaces.ImpactSpec(ctx, connect.NewRequest(&pb.ImpactSpecRequest{Workspace: r.dir, Path: path, Judge: judge, Estimate: estimate}))
	if err != nil {
		return api.SpecImpact{}, fromAPI(err)
	}
	m := res.Msg
	out := api.SpecImpact{Changes: fromSpecChanges(m.Changes), Summary: m.Summary, Report: m.Report, CostUSD: m.CostUsd, EstimatedUSD: m.EstimatedUsd, Priced: m.Priced, Problem: m.Problem}
	for _, s := range m.Symbols {
		out.Symbols = append(out.Symbols, api.SpecSymbol{Name: s.Name, Path: s.Path, Line: int(s.Line), Uses: int(s.Uses)})
	}
	for _, p := range m.Places {
		out.Places = append(out.Places, api.SpecPlace{Path: p.Path, Line: int(p.Line), Kind: p.Kind, Text: p.Text, Why: p.Why})
	}
	for _, st := range m.Steps {
		out.Steps = append(out.Steps, api.SpecStep{Title: st.Title, Detail: st.Detail, Files: st.Files})
	}
	for _, c := range m.Conflicts {
		out.Conflicts = append(out.Conflicts, api.SpecConflict{ID: c.Id, Why: c.Why})
	}
	return out, nil
}

// Reflection is what the harness has to learn
// (WorkspaceService.GetReflection).
func (r *Remote) Reflection() (api.Reflection, error) {
	res, err := r.workspaces.GetReflection(context.Background(), connect.NewRequest(&pb.GetReflectionRequest{Workspace: r.dir}))
	if err != nil {
		return api.Reflection{}, fromAPI(err)
	}
	return api.Reflection{Patterns: int(res.Msg.Patterns), Proposals: int(res.Msg.Proposals), Score: int(res.Msg.Score), Threshold: int(res.Msg.Threshold)}, nil
}

// ForgetNote deletes one (WorkspaceService.ForgetNote).
func (r *Remote) ForgetNote(name string) error {
	_, err := r.workspaces.ForgetNote(context.Background(), connect.NewRequest(&pb.ForgetNoteRequest{Workspace: r.dir, Name: name}))
	return fromAPI(err)
}

// ListHooks are the workspace's hooks in the service
// (WorkspaceService.ListHooks); on failure warn is told and there are none.
func (r *Remote) ListHooks() []api.HookInfo {
	res, err := r.workspaces.ListHooks(context.Background(), connect.NewRequest(&pb.ListHooksRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing the hooks", err)
		return nil
	}
	out := make([]api.HookInfo, len(res.Msg.Hooks))
	for i, k := range res.Msg.Hooks {
		out[i] = api.HookInfo{Event: k.Event, Type: k.Type, Match: k.Match, If: k.If, Runs: k.Runs, Source: k.Source, FailClosed: k.FailClosed}
		for _, f := range k.Failures {
			out[i].Failures = append(out[i].Failures, api.HookFailure{Time: f.Time.AsTime(), Error: f.Error})
		}
	}
	return out
}

// ListStyles are the output styles (WorkspaceService.ListStyles); on
// failure warn is told and there are none.
func (r *Remote) ListStyles() []api.StyleInfo {
	res, err := r.workspaces.ListStyles(context.Background(), connect.NewRequest(&pb.ListStylesRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing the output styles", err)
		return nil
	}
	out := make([]api.StyleInfo, len(res.Msg.Styles))
	for i, s := range res.Msg.Styles {
		out[i] = api.StyleInfo{Name: s.Name, Description: s.Description, Source: s.Source, Active: s.Active}
	}
	return out
}

// TakeProcessNotices are what the session's watched processes reported
// while it was idle (WorkspaceService.TakeProcessNotices).
func (r *Remote) TakeProcessNotices(session string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	res, err := r.workspaces.TakeProcessNotices(ctx, connect.NewRequest(&pb.TakeProcessNoticesRequest{Workspace: r.dir, SessionId: session}))
	if err != nil {
		r.failed("taking the processes' notices", err)
		return nil
	}
	return res.Msg.Notices
}

// RunBlitzCommand runs one of Blitz's own commands in the service (the
// CLI's blitz do, spec_060_blitz_tools DRV-02).
func (r *Remote) RunBlitzCommand(ctx context.Context, command, argsJSON string, confirm bool, secrets map[string]string) (*pb.RunBlitzCommandResponse, error) {
	res, err := r.workspaces.RunBlitzCommand(ctx, connect.NewRequest(&pb.RunBlitzCommandRequest{Workspace: r.dir, Command: command, ArgsJson: argsJSON, Confirm: confirm, Secrets: secrets}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg, nil
}
