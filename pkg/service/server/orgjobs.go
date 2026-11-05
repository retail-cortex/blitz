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
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/orgsync"
	"github.com/retail-cortex/blitz/pkg/runmerge"
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
		if samePath(sf.Dir, dir) {
			return c, handle, nil
		}
	}
	folder, at := s.orgFolder(c, cfg), filepath.Clean(dir)
	if real, err := canonical(folder); err == nil {
		folder = real
	}
	if real, err := canonical(at); err == nil {
		at = real
	}
	rel, err := filepath.Rel(folder, at)
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
		Created: stamp(&r.Created), Ended: stamp(r.Ended), Branch: r.Branch, Artifacts: r.Artifacts, Answer: r.Answer,
		Merged: r.Merged, MergeNote: r.MergeNote}
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
	out, err := h.serviceJob(ctx, c, jobs, handle, r.Msg.Name)
	if err != nil || out != nil {
		return ok2(out, err)
	}
	why, read := h.s.whyNotServiceJob(ctx, c, r.Msg.Workspace, handle, r.Msg.Name, r.Msg.Path)
	// Read just now: it's there, with its runs.
	if out, err := h.serviceJob(ctx, c, read, handle, r.Msg.Name); err != nil || out != nil {
		return ok2(out, err)
	}
	return ok(why)
}

func (h configService) TakeServiceRun(ctx context.Context, r req[pb.TakeServiceRunRequest]) (*connect.Response[pb.TakeServiceRunResponse], error) {
	dir := r.Msg.Workspace
	c, handle, err := h.s.orgHandle(dir)
	if err != nil {
		return nil, err
	}
	runs, err := c.JobRuns(ctx, r.Msg.JobId)
	if err != nil {
		return nil, enterpriseError(err)
	}
	i := slices.IndexFunc(runs, func(x enterprise.ServiceRun) bool { return x.ID == r.Msg.RunId })
	if i < 0 {
		return nil, apiError(connect.CodeNotFound, "UNKNOWN_RUN", fmt.Errorf("the job has no run %s", r.Msg.RunId))
	}
	run := runs[i]
	if run.Branch == "" {
		return nil, apiError(connect.CodeFailedPrecondition, "NO_CHANGES", errors.New("the run changed nothing in the workspace"))
	}
	if !run.Merged {
		if err := h.s.mergeRun(ctx, c, dir, handle, run.Branch); err != nil {
			return nil, err
		}
		if run, err = c.RunMerged(ctx, r.Msg.JobId, run.ID); err != nil {
			return nil, enterpriseError(err)
		}
	}
	// The folder takes main now, if sync keeps it in step; a plain clone
	// pulls it.
	if sy := h.s.syncer(dir); sy != nil {
		sy.Remote()
	}
	return ok(&pb.TakeServiceRunResponse{Run: serviceRunMsg(run)})
}

// mergeRun adds branch to the main of handle's workspace (whose clone is at
// dir) through a clone of its own, never touching the folder's files.
func (s *Server) mergeRun(ctx context.Context, c *enterprise.Client, dir, handle, branch string) error {
	_, remote, err := s.orgRemote(c, handle)
	if err != nil {
		return invalid(err)
	}
	url, err := gitText(ctx, dir, "remote", "get-url", remote)
	if err != nil {
		return invalid(fmt.Errorf("the folder's remote %s: %w", remote, err))
	}
	tmp, err := os.MkdirTemp("", "blitz-take-")
	if err != nil {
		return apiError(connect.CodeInternal, "INTERNAL", err)
	}
	defer os.RemoveAll(tmp)
	if err := s.git(ctx, tmp, "clone", "--quiet", "--no-checkout", url, "."); err != nil {
		return apiError(connect.CodeUnavailable, "CLONE_FAILED", fmt.Errorf("cloning the workspace: %w", err))
	}
	who := cmp.Or(c.Status().Person.Name, c.Status().Person.Email)
	git := func(ctx context.Context, args ...string) ([]byte, error) { return s.gitOut(ctx, tmp, args...) }
	switch err := runmerge.IntoMain(ctx, git, "origin", branch, who); {
	case errors.Is(err, runmerge.ErrConflicts):
		return apiError(connect.CodeFailedPrecondition, "CONFLICTS", fmt.Errorf("the run's changes weren't added: %w; they're on its branch %s", err, branch))
	case err != nil:
		return apiError(connect.CodeUnavailable, "PUSH_FAILED", fmt.Errorf("the run's changes weren't added to main: %w", err))
	}
	return nil
}

// ok2 is out's response, or err.
func ok2[T any](out *T, err error) (*connect.Response[T], error) {
	if err != nil {
		return nil, err
	}
	return ok(out)
}

// serviceJob is the job name in handle's workspace among jobs, with its
// runs (nil when it isn't there).
func (h configService) serviceJob(ctx context.Context, c *enterprise.Client, jobs []enterprise.ServiceJob, handle, name string) (*pb.GetServiceJobResponse, error) {
	for _, j := range jobs {
		if j.Workspace != handle || j.Name != name {
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
		return out, nil
	}
	return nil, nil
}

// jobDir is where the service reads a job from (its WORKER.md's folder),
// relative to its workspace.
func jobDir(name string) string { return path.Join(".agents", "workers", name) }

// syncState is the sync's state for the workspace at dir ("" when no
// syncer keeps it in step), and its last failure in Git's words.
func (s *Server) syncState(dir string) (string, string) {
	key, err := canonical(dir)
	if err != nil {
		return "", ""
	}
	s.ent.syncs.mu.Lock()
	defer s.ent.syncs.mu.Unlock()
	for d, rs := range s.ent.syncs.running {
		if k, err := canonical(d); err == nil && k == key {
			st := rs.s.State()
			// One just started hasn't saved yet: it's about to.
			return cmp.Or(st.State, orgsync.Saving), st.Err
		}
	}
	return "", ""
}

// whyNotServiceJob says why the service has no job name in the workspace
// at dir: its WORKER.md isn't where the service reads, its changes aren't
// on main (sync is putting them there, or nothing is), or main has it and
// the service, made to read main now, doesn't run it. read are the jobs
// the service has after reading main now (nil when it didn't).
func (s *Server) whyNotServiceJob(ctx context.Context, c *enterprise.Client, dir, handle, name, file string) (_ *pb.GetServiceJobResponse, read []enterprise.ServiceJob) {
	rel := jobDir(name)
	if file != "" && filepath.Clean(file) != filepath.Join(dir, filepath.FromSlash(rel), "WORKER.md") {
		return &pb.GetServiceJobResponse{Reason: "outside", Detail: rel + "/WORKER.md"}, nil
	}
	_, remote, err := s.orgRemote(c, handle)
	if err != nil {
		return &pb.GetServiceJobResponse{Reason: "not_read", Detail: err.Error()}, nil
	}
	changed, err := s.gitOut(ctx, dir, "status", "--porcelain", "--untracked-files=all", "--", rel)
	if err != nil {
		return &pb.GetServiceJobResponse{Reason: "not_read", Detail: err.Error()}, nil
	}
	if len(strings.TrimSpace(string(changed))) == 0 {
		// Committed here: is it on main as the service has it? (diff
		// --quiet exits 1 for a difference; anything else is git failing.)
		_, err := s.gitOut(ctx, dir, "diff", "--quiet", remote+"/main", "HEAD", "--", rel)
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee) && ee.ExitCode() == 1:
			changed = []byte("ahead")
		case err != nil:
			return &pb.GetServiceJobResponse{Reason: "not_read", Detail: err.Error()}, nil
		}
	}
	if len(strings.TrimSpace(string(changed))) > 0 {
		if st, why := s.syncState(dir); st != "" {
			return &pb.GetServiceJobResponse{Reason: "saving", SyncState: st, Detail: why}, nil
		}
		return &pb.GetServiceJobResponse{Reason: "not_pushed"}, nil
	}
	jobs, unrun, err := c.ReadJobs(ctx, handle)
	if err != nil {
		return &pb.GetServiceJobResponse{Reason: "not_read", Detail: err.Error()}, nil
	}
	if why, ok := unrun[name]; ok {
		return &pb.GetServiceJobResponse{Reason: "not_service", Detail: why}, jobs
	}
	return &pb.GetServiceJobResponse{Reason: "not_read"}, jobs
}

// orgRemote is the clone of handle's workspace on this computer and the
// Git remote that's the organization's there; an error when the settings
// that say where clones go can't be read.
func (s *Server) orgRemote(c *enterprise.Client, handle string) (string, string, error) {
	cfg, err := config.Load(s.configDir)
	if err != nil {
		return "", "", err
	}
	dir, remote := orgDir(sharedFor(c.URL()), s.orgFolder(c, cfg), handle)
	return dir, remote, nil
}

func (h configService) PushServiceJob(ctx context.Context, r req[pb.PushServiceJobRequest]) (*connect.Response[pb.PushServiceJobResponse], error) {
	c, handle, err := h.s.orgHandle(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if n := r.Msg.Name; n == "" || n == "." || n == ".." || strings.ContainsAny(n, `/\`) {
		return nil, invalid(fmt.Errorf("%q isn't a job's name", r.Msg.Name))
	}
	dir, rel := r.Msg.Workspace, jobDir(r.Msg.Name)
	_, remote, err := h.s.orgRemote(c, handle)
	if err != nil {
		return nil, invalid(err)
	}
	failed := func(err error) error {
		return apiError(connect.CodeFailedPrecondition, "PUSH_FAILED", fmt.Errorf("couldn't put the job on the workspace's main: %w", err))
	}
	if _, err := h.s.gitOut(ctx, dir, "add", "-A", "--", rel); err != nil {
		return nil, failed(err)
	}
	if _, err := h.s.gitOut(ctx, dir, "diff", "--cached", "--quiet", "--", rel); err != nil {
		who := c.Status().Person.Name
		if who == "" {
			who, _, _ = strings.Cut(c.Status().Person.Email, "@")
		}
		if _, err := h.s.gitAs(c)(ctx, dir, "commit", "-q", "-m", who+": the job "+r.Msg.Name, "--", rel); err != nil {
			return nil, failed(err)
		}
	}
	if _, err := h.s.gitOut(ctx, dir, "push", "-q", remote, "HEAD:main"); err != nil {
		return nil, failed(err)
	}
	if _, _, err := c.ReadJobs(ctx, handle); err != nil {
		slog.Debug("reading a workspace's jobs", "workspace", handle, "error", err)
	}
	got, err := h.GetServiceJob(ctx, connect.NewRequest(&pb.GetServiceJobRequest{Workspace: dir, Name: r.Msg.Name}))
	if err != nil {
		return nil, err
	}
	return ok(&pb.PushServiceJobResponse{Job: got.Msg})
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
		out.Files = append(out.Files, &pb.ServiceRunFile{Urn: f.String(), Path: f.Path, Size: it.Size})
	}
	return ok(out)
}

// maxSave bounds a run's file saved here: the service keeps none larger
// (a variable for tests).
var maxSave int64 = 100 << 20

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
	// One byte past the limit says the file is larger: never saved cut off.
	n2, err := io.Copy(f, io.LimitReader(body, maxSave+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n2 > maxSave {
		_ = os.Remove(f.Name())
		return nil, apiError(connect.CodeFailedPrecondition, "UPLOAD_TOO_LARGE",
			fmt.Errorf("%s is larger than the %d MB a run's file may be: open it in the organization's service instead", path.Base(n.Path), maxSave>>20),
			"max_bytes", strconv.FormatInt(maxSave, 10))
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
				_ = send(&pb.ServiceRunChange{JobId: ch.Job})
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
	return &pb.OrgUsed{Model: model, Requests: u.Requests, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CostUsd: u.CostUSD}
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

func noticesMsg(n enterprise.Notices) *pb.OrgNotices {
	return &pb.OrgNotices{Email: n.Email, Webhook: n.Webhook, EveryRun: n.EveryRun, EmailAvailable: n.EmailAvailable}
}

func (h configService) GetOrgNotices(ctx context.Context, _ req[pb.GetOrgNoticesRequest]) (*connect.Response[pb.GetOrgNoticesResponse], error) {
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	n, err := c.Notices(ctx)
	if err != nil {
		return nil, enterpriseError(err)
	}
	return ok(&pb.GetOrgNoticesResponse{Notices: noticesMsg(n)})
}

func (h configService) SetOrgNotices(ctx context.Context, r req[pb.SetOrgNoticesRequest]) (*connect.Response[pb.SetOrgNoticesResponse], error) {
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	in := r.Msg.GetNotices()
	n, err := c.SetNotices(ctx, enterprise.Notices{Email: in.GetEmail(), Webhook: in.GetWebhook(), EveryRun: in.GetEveryRun()})
	if err != nil {
		return nil, enterpriseError(err)
	}
	return ok(&pb.SetOrgNoticesResponse{Notices: noticesMsg(n)})
}
