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
	// Merged is whether its changes are on main too; MergeNote why not.
	Merged    bool
	MergeNote string
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
		Branch: m.GetBranch(), Merged: m.GetMerged(), MergeNote: m.GetMergeNote(), Artifacts: m.GetArtifacts()}
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

// ServiceJobMissing is why the organization's service hasn't got a job:
// Reason is outside, not_pushed, saving (SyncState says how sync stands),
// not_service or not_read; Detail says more (the path the service reads,
// or the service's why).
type ServiceJobMissing struct {
	Reason, Detail, SyncState string
}

func missing(r *pb.GetServiceJobResponse) *ServiceJobMissing {
	if r.GetFound() {
		return nil
	}
	return &ServiceJobMissing{Reason: r.GetReason(), Detail: r.GetDetail(), SyncState: r.GetSyncState()}
}

// WhyNotServiceJob says why the service hasn't got the job name in the
// workspace cloned at dir (nil when it has it), having it read main.
func (s *Settings) WhyNotServiceJob(ctx context.Context, dir, name string) (*ServiceJobMissing, error) {
	res, err := s.c.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: dir, Name: name}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return missing(res.Msg), nil
}

// PushServiceJob commits the job's folder, pushes the workspace at dir to
// its main and has the service read it; nil when the service has it now.
func (s *Settings) PushServiceJob(ctx context.Context, dir, name string) (*ServiceJobMissing, error) {
	res, err := s.c.PushServiceJob(ctx, connect.NewRequest(&pb.PushServiceJobRequest{Workspace: dir, Name: name}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return missing(res.Msg.GetJob()), nil
}

// TakeServiceRun adds a service run's changes, on its branch, to the main
// of the workspace cloned at dir, which takes them in.
func (s *Settings) TakeServiceRun(ctx context.Context, dir, jobID, runID string) (ServiceRun, error) {
	res, err := s.c.TakeServiceRun(ctx, connect.NewRequest(&pb.TakeServiceRunRequest{Workspace: dir, JobId: jobID, RunId: runID}))
	if err != nil {
		return ServiceRun{}, fromAPI(err)
	}
	return serviceRun(res.Msg.GetRun()), nil
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

// OrgUsage is the person's own use of the organization's models (spec_053
// EN-24; ConfigService.GetOrgUsage), as blitzd gives it.
type OrgUsage = *pb.GetOrgUsageResponse

// OrgUsage asks blitzd for the person's usage and limits.
func (s *Settings) OrgUsage(ctx context.Context) (OrgUsage, error) {
	res, err := s.c.GetOrgUsage(ctx, connect.NewRequest(&pb.GetOrgUsageRequest{}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg, nil
}

// Publishable are the workspace's items that can be published to the
// organization's catalog (ConfigService.ListPublishable).
func (s *Settings) Publishable(ctx context.Context, dir string) ([]*pb.PublishableItem, error) {
	res, err := s.c.ListPublishable(ctx, connect.NewRequest(&pb.ListPublishableRequest{Workspace: dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg.Items, nil
}

// PublishToOrganization sends items to the catalog as a review
// (ConfigService.PublishToOrganization).
func (s *Settings) PublishToOrganization(ctx context.Context, dir, title string, items []*pb.PublishItem) (*pb.CatalogReview, error) {
	res, err := s.c.PublishToOrganization(ctx, connect.NewRequest(&pb.PublishToOrganizationRequest{Workspace: dir, Title: title, Items: items}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg.Review, nil
}

// CatalogReviews are the catalog's reviews (ConfigService.ListCatalogReviews).
func (s *Settings) CatalogReviews(ctx context.Context, state string) ([]*pb.CatalogReview, error) {
	res, err := s.c.ListCatalogReviews(ctx, connect.NewRequest(&pb.ListCatalogReviewsRequest{State: state}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg.Reviews, nil
}

// OrgItems are the organization's catalog items for a workspace, of one
// kind or every one (ConfigService.ListOrgItems).
func (s *Settings) OrgItems(ctx context.Context, dir, kind string) (*pb.ListOrgItemsResponse, error) {
	res, err := s.c.ListOrgItems(ctx, connect.NewRequest(&pb.ListOrgItemsRequest{Workspace: dir, Kind: kind}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg, nil
}

// CopyOrgItem copies one of the organization's default items into the
// workspace's .agents folder (ConfigService.CopyOrgItem).
func (s *Settings) CopyOrgItem(ctx context.Context, dir, kind, name string) ([]string, error) {
	res, err := s.c.CopyOrgItem(ctx, connect.NewRequest(&pb.CopyOrgItemRequest{Workspace: dir, Kind: kind, Name: name}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg.Paths, nil
}

// OrgNotices are how the person hears of their jobs' runs on the
// organization's service (ConfigService.GetOrgNotices).
func (s *Settings) OrgNotices(ctx context.Context) (*pb.OrgNotices, error) {
	res, err := s.c.GetOrgNotices(ctx, connect.NewRequest(&pb.GetOrgNoticesRequest{}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg.Notices, nil
}

// SetOrgNotices keeps them (ConfigService.SetOrgNotices).
func (s *Settings) SetOrgNotices(ctx context.Context, n *pb.OrgNotices) (*pb.OrgNotices, error) {
	res, err := s.c.SetOrgNotices(ctx, connect.NewRequest(&pb.SetOrgNoticesRequest{Notices: n}))
	if err != nil {
		return nil, fromAPI(err)
	}
	return res.Msg.Notices, nil
}

// GoingServiceRun is a run going now on the organization's service, with
// its job and workspace (EN-42).
type GoingServiceRun struct {
	Run                            ServiceRun
	JobID, JobName, Workspace, Dir string
}

// WatchServiceRuns calls on with every run going now on the service, at
// once and again each time one changes (ConfigService.WatchServiceRuns),
// until ctx ends (nil) or the stream fails.
func (s *Settings) WatchServiceRuns(ctx context.Context, on func([]GoingServiceRun)) error {
	stream, err := s.c.WatchServiceRuns(ctx, connect.NewRequest(&pb.WatchServiceRunsRequest{}))
	if err != nil {
		return fromAPI(err)
	}
	defer stream.Close()
	for stream.Receive() {
		m := stream.Msg()
		if !m.GetListed() { // a keepalive
			continue
		}
		runs := make([]GoingServiceRun, len(m.GetRuns()))
		for i, g := range m.GetRuns() {
			runs[i] = GoingServiceRun{Run: serviceRun(g.GetRun()), JobID: g.GetJobId(), JobName: g.GetJobName(), Workspace: g.GetWorkspace(), Dir: g.GetDir()}
		}
		on(runs)
	}
	if ctx.Err() != nil {
		return nil
	}
	return fromAPI(stream.Err())
}
