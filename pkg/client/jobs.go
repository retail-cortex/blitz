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
	"time"

	"connectrpc.com/connect"

	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// ServiceJob is a job the organization's service runs (spec_053 EN-42).
type ServiceJob struct {
	ID, Name, Schedule string
	// Workspace is its handle; Dir where it's cloned here.
	Workspace, Dir string
	Enabled        bool
	NextRun        time.Time // zero when none
}

// ServiceRun is one of a service job's runs.
type ServiceRun struct {
	ID, Trigger, Status, Note, Output string
	CostUSD                           float64
	Created                           time.Time
	Branch                            string
	// Artifacts names its files; Pending what a paused run asks for.
	Artifacts string
	Pending   *ServiceRequest
	// JobName and Workspace (its handle) are given with a run waiting
	// for the person's answer.
	JobName, Workspace string
}

// ServiceRequest is what a paused run asks for.
type ServiceRequest struct{ Tool, Kind, Detail string }

// ServiceFile is one of a run's files.
type ServiceFile struct {
	URN, Path string
	Size      int64
}

func serviceRun(m *pb.ServiceRun) ServiceRun {
	r := ServiceRun{ID: m.GetId(), Trigger: m.GetTrigger(), Status: m.GetStatus(), Note: m.GetNote(), Output: m.GetOutput(), CostUSD: m.GetCostUsd(),
		Branch: m.GetBranch(), Artifacts: m.GetArtifacts()}
	if m.GetCreated() != nil {
		r.Created = m.GetCreated().AsTime()
	}
	if p := m.GetPending(); p != nil {
		r.Pending = &ServiceRequest{Tool: p.GetTool(), Kind: p.GetKind(), Detail: p.GetDetail()}
	}
	return r
}

// ServiceJobs are the jobs the organization's service runs in the
// person's workspaces (ConfigService.ListServiceJobs).
func (s *Settings) ServiceJobs(ctx context.Context) ([]ServiceJob, error) {
	res, err := s.c.ListServiceJobs(ctx, connect.NewRequest(&pb.ListServiceJobsRequest{}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]ServiceJob, len(res.Msg.Jobs))
	for i, j := range res.Msg.Jobs {
		out[i] = ServiceJob{ID: j.GetId(), Name: j.GetName(), Schedule: j.GetSchedule(), Workspace: j.GetWorkspace(), Dir: j.GetDir(), Enabled: j.GetEnabled()}
		if j.GetNextRun() != nil {
			out[i].NextRun = j.GetNextRun().AsTime()
		}
	}
	return out, nil
}

// ServiceJobRuns are a service job's latest runs, by the folder its
// workspace is cloned in and its name (ConfigService.GetServiceJob).
func (s *Settings) ServiceJobRuns(ctx context.Context, dir, name string) ([]ServiceRun, error) {
	res, err := s.c.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: dir, Name: name}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]ServiceRun, len(res.Msg.Runs))
	for i, r := range res.Msg.Runs {
		out[i] = serviceRun(r)
	}
	return out, nil
}

// RunServiceJob starts a run of a service job now.
func (s *Settings) RunServiceJob(ctx context.Context, id string) (ServiceRun, error) {
	res, err := s.c.RunServiceJob(ctx, connect.NewRequest(&pb.RunServiceJobRequest{JobId: id}))
	if err != nil {
		return ServiceRun{}, fromAPI(err)
	}
	return serviceRun(res.Msg.Run), nil
}

// SetServiceJobEnabled switches a service job on or off.
func (s *Settings) SetServiceJobEnabled(ctx context.Context, id string, on bool) error {
	_, err := s.c.SetServiceJobEnabled(ctx, connect.NewRequest(&pb.SetServiceJobEnabledRequest{JobId: id, Enabled: on}))
	return fromAPI(err)
}

// ServiceRunFiles are the files a run left, by its artifacts' name.
func (s *Settings) ServiceRunFiles(ctx context.Context, artifacts string) ([]ServiceFile, error) {
	res, err := s.c.ListServiceRunFiles(ctx, connect.NewRequest(&pb.ListServiceRunFilesRequest{Artifacts: artifacts}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]ServiceFile, len(res.Msg.Files))
	for i, f := range res.Msg.Files {
		out[i] = ServiceFile{URN: f.GetUrn(), Path: f.GetPath(), Size: f.GetSize()}
	}
	return out, nil
}

// SaveServiceRunFile saves one of a run's files in Downloads, answering
// where.
func (s *Settings) SaveServiceRunFile(ctx context.Context, urn string) (string, error) {
	res, err := s.c.SaveServiceRunFile(ctx, connect.NewRequest(&pb.SaveServiceRunFileRequest{Urn: urn}))
	if err != nil {
		return "", fromAPI(err)
	}
	return res.Msg.Path, nil
}

// ServiceApprovals are the runs paused for the person's answer (EN-43).
func (s *Settings) ServiceApprovals(ctx context.Context) ([]ServiceRun, error) {
	res, err := s.c.ListServiceApprovals(ctx, connect.NewRequest(&pb.ListServiceApprovalsRequest{}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]ServiceRun, len(res.Msg.Runs))
	for i, g := range res.Msg.Runs {
		out[i] = serviceRun(g.GetRun())
		out[i].JobName, out[i].Workspace = g.GetJobName(), g.GetWorkspace()
	}
	return out, nil
}

// AnswerServiceRun approves or denies what a paused run asks for.
func (s *Settings) AnswerServiceRun(ctx context.Context, run string, approve bool, note string) (ServiceRun, error) {
	res, err := s.c.AnswerServiceRun(ctx, connect.NewRequest(&pb.AnswerServiceRunRequest{RunId: run, Approve: approve, Note: note}))
	if err != nil {
		return ServiceRun{}, fromAPI(err)
	}
	return serviceRun(res.Msg.Run), nil
}
