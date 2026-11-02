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
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/urn"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The service's jobs in Studio (spec_053_enterprise EN-42): a job with
// runs_on: service shows the organization's runs of it, not this
// computer's.

// orgHandle is the handle of the organization workspace cloned at dir.
func (s *Server) orgHandle(dir string) (*enterprise.Client, string, error) {
	c, cfg, err := s.orgClient()
	if err != nil {
		return nil, "", err
	}
	if !c.Status().SignedIn {
		return nil, "", enterpriseError(enterprise.ErrNotSignedIn)
	}
	for handle, sf := range sharedFor(c.URL()) {
		if filepath.Clean(sf.Dir) == filepath.Clean(dir) {
			return c, handle, nil
		}
	}
	rel, err := filepath.Rel(s.orgFolder(c, cfg), filepath.Clean(dir))
	handle := filepath.ToSlash(rel)
	if err != nil || !enterprise.ValidHandle(handle) {
		return nil, "", apiError(connect.CodeFailedPrecondition, "NOT_ORG_WORKSPACE", fmt.Errorf("%s isn't one of the organization's workspaces", dir))
	}
	return c, handle, nil
}

func stamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil || t.IsZero() {
		return nil
	}
	return timestamppb.New(*t)
}

func serviceRunMsg(r enterprise.ServiceRun) *pb.ServiceRun {
	m := &pb.ServiceRun{Id: r.ID, Urn: r.URN, Trigger: r.Trigger, Status: r.Status, Note: r.Note, Output: r.Output, CostUsd: r.CostUSD,
		Created: stamp(&r.Created), Ended: stamp(r.Ended), Branch: r.Branch, Artifacts: r.Artifacts, Answer: r.Answer, AnswerNote: r.AnswerNote}
	if p := r.Pending; p != nil {
		m.Pending = &pb.ServiceRequest{Tool: p.Tool, Kind: p.Kind, Detail: p.Detail}
	}
	return m
}

func (h configService) GetServiceJob(ctx context.Context, r req[pb.GetServiceJobRequest]) (*connect.Response[pb.GetServiceJobResponse], error) {
	c, handle, err := h.s.orgHandle(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	jobs, err := c.Jobs(ctx)
	if err != nil {
		return nil, enterpriseError(err)
	}
	for _, j := range jobs {
		if j.Workspace != handle || j.Name != r.Msg.Name {
			continue
		}
		runs, err := c.JobRuns(ctx, j.ID)
		if err != nil {
			return nil, enterpriseError(err)
		}
		out := &pb.GetServiceJobResponse{Found: true, Job: &pb.ServiceJob{Id: j.ID, Urn: j.URN, Name: j.Name, Schedule: j.Schedule, Enabled: j.Enabled}}
		if j.Enabled {
			out.Job.NextRun = stamp(j.NextRun)
		}
		for _, run := range runs {
			out.Runs = append(out.Runs, serviceRunMsg(run))
		}
		return ok(out)
	}
	return ok(&pb.GetServiceJobResponse{})
}

func (h configService) RunServiceJob(ctx context.Context, r req[pb.RunServiceJobRequest]) (*connect.Response[pb.RunServiceJobResponse], error) {
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	run, err := c.RunJobNow(ctx, r.Msg.JobId)
	if err != nil {
		return nil, enterpriseError(err)
	}
	return ok(&pb.RunServiceJobResponse{Run: serviceRunMsg(run)})
}

func (h configService) SetServiceJobEnabled(ctx context.Context, r req[pb.SetServiceJobEnabledRequest]) (*connect.Response[pb.SetServiceJobEnabledResponse], error) {
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	if err := c.SetJobEnabled(ctx, r.Msg.JobId, r.Msg.Enabled); err != nil {
		return nil, enterpriseError(err)
	}
	return ok(&pb.SetServiceJobEnabledResponse{})
}

// artifactsName reads a name of a run's files (or, file, one of them).
func artifactsName(s string, file bool) (urn.Name, error) {
	n, err := urn.Parse(s)
	if err != nil || n.Kind != urn.Artifacts || len(n.IDs) != 3 || (n.Path != "") != file {
		return urn.Name{}, invalid(fmt.Errorf("%q isn't the name of a run's files", s))
	}
	return n, nil
}

func (h configService) ListServiceRunFiles(ctx context.Context, r req[pb.ListServiceRunFilesRequest]) (*connect.Response[pb.ListServiceRunFilesResponse], error) {
	n, err := artifactsName(r.Msg.Artifacts, false)
	if err != nil {
		return nil, err
	}
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	items, err := c.Children(ctx, n.String())
	if err != nil {
		return nil, enterpriseError(err)
	}
	out := &pb.ListServiceRunFilesResponse{}
	for _, it := range items {
		f, err := urn.Parse(it.URN)
		if err != nil || f.Path == "" {
			continue
		}
		out.Files = append(out.Files, &pb.ServiceRunFile{Urn: f.String(), Path: f.Path, Size: it.Size, MediaType: it.MediaType})
	}
	return ok(out)
}

// maxSave bounds a run's file saved here: the service keeps none larger.
const maxSave = 100 << 20

func (h configService) SaveServiceRunFile(ctx context.Context, r req[pb.SaveServiceRunFileRequest]) (*connect.Response[pb.SaveServiceRunFileResponse], error) {
	n, err := artifactsName(r.Msg.Urn, true)
	if err != nil {
		return nil, err
	}
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	body, err := c.OpenResource(ctx, n.String())
	if err != nil {
		return nil, enterpriseError(err)
	}
	defer body.Close()
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, apiError(connect.CodeInternal, "INTERNAL", err)
	}
	dir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, apiError(connect.CodeInternal, "INTERNAL", err)
	}
	f, err := createUnique(dir, path.Base(n.Path))
	if err != nil {
		return nil, apiError(connect.CodeInternal, "INTERNAL", err)
	}
	_, err = io.Copy(f, io.LimitReader(body, maxSave))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return nil, apiError(connect.CodeUnavailable, "UNREACHABLE", err)
	}
	return ok(&pb.SaveServiceRunFileResponse{Path: f.Name()})
}

// createUnique makes a new file named name in dir, or "name (2).ext" and
// on when it's taken.
func createUnique(dir, name string) (*os.File, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		try := name
		if i > 1 {
			try = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		f, err := os.OpenFile(filepath.Join(dir, try), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if !errors.Is(err, os.ErrExist) {
			return f, err
		}
	}
	return nil, fmt.Errorf("no free name for %s in %s", name, dir)
}

// watchRunsRetry is how long WatchServiceRuns waits to follow the
// service's stream again after it drops (a variable for tests).
var watchRunsRetry = 10 * time.Second

func (h configService) WatchServiceRuns(ctx context.Context, _ req[pb.WatchServiceRunsRequest], stream *connect.ServerStream[pb.WatchServiceRunsResponse]) error {
	c, cfg, err := h.s.orgClient()
	if err != nil {
		return err
	}
	folder := h.s.orgFolder(c, cfg)
	shared := sharedFor(c.URL())
	dir := func(handle string) string { d, _ := orgDir(shared, folder, handle); return d }
	send := func(change *pb.ServiceRunChange) error {
		list, err := c.GoingRuns(ctx)
		if err != nil {
			return enterpriseError(err)
		}
		out := &pb.WatchServiceRunsResponse{Change: change, Listed: true}
		for _, g := range list {
			if enterprise.ValidHandle(g.Workspace) {
				out.Runs = append(out.Runs, &pb.GoingServiceRun{Run: serviceRunMsg(g.ServiceRun), JobId: g.Job, JobName: g.JobName,
					Workspace: g.Workspace, Dir: dir(g.Workspace)})
			}
		}
		return stream.Send(out)
	}
	if err := send(nil); err != nil {
		return err
	}
	changes := make(chan enterprise.RunChange, 16)
	for {
		wctx, cancel := context.WithCancel(ctx)
		ended := make(chan error, 1)
		go func() {
			ended <- c.WatchRuns(wctx, func(ch enterprise.RunChange) {
				select {
				case changes <- ch:
				default: // the next change sends the list anyway
				}
			})
		}()
	follow:
		for {
			select {
			case <-ctx.Done():
				cancel()
				return nil
			case ch := <-changes:
				if !enterprise.ValidHandle(ch.Workspace) {
					continue
				}
				// A list that can't be had now comes with the next change.
				_ = send(&pb.ServiceRunChange{RunId: ch.Run, JobId: ch.Job, Workspace: ch.Workspace, Dir: dir(ch.Workspace), Status: ch.Status})
			case err := <-ended:
				cancel()
				var ref *enterprise.Refusal
				if errors.Is(err, enterprise.ErrNotSignedIn) || (errors.As(err, &ref) && ref.Status/100 == 4) {
					return enterpriseError(err)
				}
				break follow
			}
		}
		// Dropped (offline, the service restarting): again in a while, with
		// the list as it is then.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(watchRunsRetry):
		}
		_ = send(nil)
	}
}

func (h configService) ListServiceApprovals(ctx context.Context, _ req[pb.ListServiceApprovalsRequest]) (*connect.Response[pb.ListServiceApprovalsResponse], error) {
	c, cfg, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	list, err := c.PendingRuns(ctx)
	if err != nil {
		return nil, enterpriseError(err)
	}
	folder, shared := h.s.orgFolder(c, cfg), sharedFor(c.URL())
	out := &pb.ListServiceApprovalsResponse{}
	for _, g := range list {
		if !enterprise.ValidHandle(g.Workspace) {
			continue
		}
		dir, _ := orgDir(shared, folder, g.Workspace)
		out.Runs = append(out.Runs, &pb.GoingServiceRun{Run: serviceRunMsg(g.ServiceRun), JobId: g.Job, JobName: g.JobName, Workspace: g.Workspace, Dir: dir})
	}
	return ok(out)
}

func (h configService) AnswerServiceRun(ctx context.Context, r req[pb.AnswerServiceRunRequest]) (*connect.Response[pb.AnswerServiceRunResponse], error) {
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	run, err := c.AnswerRun(ctx, r.Msg.RunId, r.Msg.Approve, r.Msg.Note)
	if err != nil {
		var ref *enterprise.Refusal
		if errors.As(err, &ref) && ref.Status == 409 {
			return nil, apiError(connect.CodeFailedPrecondition, "ANSWERED", err)
		}
		return nil, enterpriseError(err)
	}
	return ok(&pb.AnswerServiceRunResponse{Run: serviceRunMsg(run)})
}

func (h configService) ListServiceJobs(ctx context.Context, _ req[pb.ListServiceJobsRequest]) (*connect.Response[pb.ListServiceJobsResponse], error) {
	c, cfg, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	jobs, err := c.Jobs(ctx)
	if err != nil {
		return nil, enterpriseError(err)
	}
	folder, shared := h.s.orgFolder(c, cfg), sharedFor(c.URL())
	out := &pb.ListServiceJobsResponse{}
	for _, j := range jobs {
		if !enterprise.ValidHandle(j.Workspace) {
			continue
		}
		dir, _ := orgDir(shared, folder, j.Workspace)
		m := &pb.ServiceJob{Id: j.ID, Urn: j.URN, Name: j.Name, Schedule: j.Schedule, Enabled: j.Enabled, Workspace: j.Workspace, Dir: dir}
		if j.Enabled {
			m.NextRun = stamp(j.NextRun)
		}
		out.Jobs = append(out.Jobs, m)
	}
	return ok(out)
}

// orgUsed is u, for model, as the API says it.
func orgUsed(model string, u enterprise.Used) *pb.OrgUsed {
	return &pb.OrgUsed{Model: model, Requests: u.Requests, InputTokens: u.InputTokens, CachedInputTokens: u.Cached, OutputTokens: u.OutputTokens, CostUsd: u.CostUSD}
}

// byModel is a usage map as a list, the costliest first.
func byModel(m map[string]enterprise.Used) []*pb.OrgUsed {
	out := make([]*pb.OrgUsed, 0, len(m))
	for model, u := range m {
		out = append(out, orgUsed(model, u))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CostUsd != out[j].CostUsd {
			return out[i].CostUsd > out[j].CostUsd
		}
		return out[i].Model < out[j].Model
	})
	return out
}

func (h configService) GetOrgUsage(ctx context.Context, _ req[pb.GetOrgUsageRequest]) (*connect.Response[pb.GetOrgUsageResponse], error) {
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	u, err := c.Usage(ctx)
	if err != nil {
		return nil, enterpriseError(err)
	}
	out := &pb.GetOrgUsageResponse{Today: byModel(u.Today), Month: byModel(u.Month), PromptLogs: u.PromptLogs}
	for _, l := range u.Limits {
		resets := l.Resets
		out.Limits = append(out.Limits, &pb.OrgLimit{ScopeKind: l.ScopeKind, Period: l.Period, Unit: l.Unit, Model: l.Model, AmountUsd: l.AmountUSD,
			Tokens: l.Tokens, InputTokens: l.InputTokens, OutputTokens: l.OutputTokens, Used: orgUsed(l.Model, l.Used), Resets: stamp(&resets)})
	}
	for _, r := range u.RateLimits {
		out.RateLimits = append(out.RateLimits, &pb.OrgRateLimit{Model: r.Model, PersonRpm: int32(r.PersonRPM), PersonTpm: int32(r.PersonTPM), Rpm: int32(r.RPM), Tpm: int32(r.TPM)})
	}
	return ok(out)
}
