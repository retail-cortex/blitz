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
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/secretscan"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// orgItem is an asset the organization offers, as the window lists it
// (EN-80): required ones locked, available ones the person's to switch.
func orgItem(org config.OrgLayer, a config.OrgAsset) *pb.OrgItem {
	it := &pb.OrgItem{Kind: a.Kind, Name: a.Name, Version: a.Version, State: a.State, On: a.On, Description: a.Description, Lock: "default"}
	if a.State == config.OrgRequired {
		it.Lock = "locked"
	}
	if path := a.Kind + "s." + a.Name; org.Explain[path].Level != "" {
		it.DecidedBy = org.Why(path)
	}
	for _, d := range a.Deps {
		if d.Resolved != "" {
			it.Locks = append(it.Locks, d.Kind+" "+d.Name+" "+d.Resolved)
		}
	}
	return it
}

// ListOrgItems are the organization's assets the person is offered
// (spec_053 EN-80): its agents, skills, workflows, rules and guardrails,
// required (locked) or available (theirs to switch on), with the version,
// whether they have it and who decided it; and from a service before the
// library, the rules and guardrails their policy switches on, each locked.
func (h configService) ListOrgItems(_ context.Context, r req[pb.ListOrgItemsRequest]) (*connect.Response[pb.ListOrgItemsResponse], error) {
	if _, _, err := h.s.orgClient(); err != nil {
		return nil, err
	}
	out := &pb.ListOrgItemsResponse{}
	cfg, err := config.Load(h.s.configDir)
	if err != nil || cfg.Org.PolicyDir == "" {
		return ok(out)
	}
	org := cfg.Org
	listed := map[string]bool{}
	for _, a := range org.Assets() {
		if a.State == "dependency" || (r.Msg.Kind != "" && a.Kind != r.Msg.Kind) {
			continue
		}
		listed[a.Kind+"/"+a.Name] = true
		out.Items = append(out.Items, orgItem(org, a))
	}
	// The policy's rules and guardrails the library doesn't list (a
	// service before it): each applies, locked.
	for _, kind := range []string{"rule", "guardrail"} {
		if r.Msg.Kind != "" && r.Msg.Kind != kind {
			continue
		}
		entries, _ := os.ReadDir(filepath.Join(org.PolicyDir, kind+"s"))
		for _, e := range entries {
			name := strings.TrimSuffix(e.Name(), ".md")
			if listed[kind+"/"+name] {
				continue
			}
			it := &pb.OrgItem{Kind: kind, Name: name, Path: kind + "s/" + e.Name(), Lock: "locked", State: config.OrgRequired, On: true}
			if path := kind + "s." + name; org.Explain[path].Level != "" {
				it.DecidedBy = org.Why(path)
			}
			out.Items = append(out.Items, it)
		}
	}
	sort.Slice(out.Items, func(i, j int) bool {
		return out.Items[i].Kind+"/"+out.Items[i].Name < out.Items[j].Kind+"/"+out.Items[j].Name
	})
	return ok(out)
}

// SetOrgItem switches one of the organization's available assets on or
// off for the person (EN-83), then fetches their policy again, so it
// arrives (or goes) at once; the service keeps the choice, so it follows
// them to every computer.
func (h configService) SetOrgItem(ctx context.Context, r req[pb.SetOrgItemRequest]) (*connect.Response[pb.SetOrgItemResponse], error) {
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	if !c.Status().SignedIn {
		return nil, apiError(connect.CodeUnauthenticated, "NOT_SIGNED_IN", errors.New("sign in to your organization's service first"))
	}
	a, err := c.SetItem(ctx, r.Msg.GetKind(), r.Msg.GetName(), r.Msg.GetOn())
	if err != nil {
		var ref *enterprise.Refusal
		if errors.As(err, &ref) && ref.Status == http.StatusConflict && ref.Code == "required" {
			return nil, apiError(connect.CodeFailedPrecondition, "REQUIRED", err)
		}
		return nil, enterpriseError(err)
	}
	if _, _, err := h.s.refreshOrg(ctx, c, h.s.brandingDir(c)); err != nil {
		return nil, enterpriseError(err)
	}
	var org config.OrgLayer
	if cfg, err := config.Load(h.s.configDir); err == nil {
		org = cfg.Org
	}
	return ok(&pb.SetOrgItemResponse{Item: orgItem(org, a)})
}

// SyncOwnItems syncs the person's own assets with the organization now
// (EN-93).
func (h configService) SyncOwnItems(ctx context.Context, _ req[pb.SyncOwnItemsRequest]) (*connect.Response[pb.SyncOwnItemsResponse], error) {
	c, err := h.signedInOrg()
	if err != nil {
		return nil, err
	}
	h.s.ent.personalMu.Lock()
	rep, err := enterprise.SyncPersonal(ctx, c, config.ConfigDir(h.s.configDir), filepath.Join(h.s.brandingDir(c), "personal.json"))
	h.s.ent.personalMu.Unlock()
	if err != nil {
		return nil, enterpriseError(err)
	}
	if len(rep.Received)+len(rep.Removed) > 0 {
		configService{h.s}.changed(ctx, "", "")
	}
	return ok(&pb.SyncOwnItemsResponse{Sent: rep.Sent, Received: rep.Received, Removed: rep.Removed, Conflicts: rep.Conflicts, Skipped: rep.Skipped})
}

// signedInOrg is the organization's client, signed in.
func (h configService) signedInOrg() (*enterprise.Client, error) {
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	if !c.Status().SignedIn {
		return nil, apiError(connect.CodeUnauthenticated, "NOT_SIGNED_IN", errors.New("sign in to your organization's service first"))
	}
	return c, nil
}

// ShareOwnItem submits one of the person's own assets to share (EN-95).
func (h configService) ShareOwnItem(ctx context.Context, r req[pb.ShareOwnItemRequest]) (*connect.Response[pb.ShareOwnItemResponse], error) {
	c, err := h.signedInOrg()
	if err != nil {
		return nil, err
	}
	key := r.Msg.GetKind() + "/" + r.Msg.GetName()
	own, skipped := enterprise.PersonalFiles(config.ConfigDir(h.s.configDir))
	files, found := own[key]
	if why, no := skipped[key]; no {
		return nil, apiError(connect.CodeInvalidArgument, "INVALID_ARGUMENT", errors.New(key+": "+why))
	}
	if !found {
		return nil, apiError(connect.CodeNotFound, "NOT_FOUND", errors.New("you have no "+key+" of your own (~/.blitz)"))
	}
	// Read for secrets before it leaves the computer (EN-36a).
	if secrets := enterprise.PersonalSecrets(files); len(secrets) > 0 {
		return nil, apiError(connect.CodeFailedPrecondition, "SECRETS_FOUND", fmt.Errorf(
			"nothing was shared: %s holds what look like secrets: %s. Remove them, or mark a false alarm with %s on its line",
			key, enterprise.SecretsSummary(secrets), secretscan.AllowMarker))
	}
	sub, err := c.Share(ctx, r.Msg.GetKind(), r.Msg.GetName(), files, r.Msg.GetNote())
	if ref, ok := errors.AsType[*enterprise.Refusal](err); ok && ref.Status/100 == 4 && ref.Code != "" {
		return nil, apiError(connect.CodeFailedPrecondition, strings.ToUpper(ref.Code), err)
	}
	if err != nil {
		return nil, enterpriseError(err)
	}
	return ok(&pb.ShareOwnItemResponse{Submission: submissionMsg(sub)})
}

// ListSubmissions is the person's submissions, their own assets and the
// kinds the organization takes (EN-95).
func (h configService) ListSubmissions(ctx context.Context, _ req[pb.ListSubmissionsRequest]) (*connect.Response[pb.ListSubmissionsResponse], error) {
	c, err := h.signedInOrg()
	if err != nil {
		return nil, err
	}
	subs, err := c.Submissions(ctx)
	if err != nil {
		return nil, enterpriseError(err)
	}
	out := &pb.ListSubmissionsResponse{}
	for _, s := range subs {
		out.Submissions = append(out.Submissions, submissionMsg(s))
	}
	own, skipped := enterprise.PersonalFiles(config.ConfigDir(h.s.configDir))
	for _, key := range slices.Sorted(maps.Keys(own)) {
		kind, name, _ := strings.Cut(key, "/")
		out.Own = append(out.Own, &pb.OwnItem{Kind: kind, Name: name})
	}
	for _, key := range slices.Sorted(maps.Keys(skipped)) {
		kind, name, _ := strings.Cut(key, "/")
		out.Own = append(out.Own, &pb.OwnItem{Kind: kind, Name: name, Skipped: skipped[key]})
	}
	if cfg, err := config.Load(h.s.configDir); err == nil {
		for _, kind := range slices.Sorted(maps.Keys(enterprise.PersonalKinds)) {
			if on, _ := cfg.Org.Explain["library.contribute."+enterprise.PersonalKinds[kind]].Value.(bool); on {
				out.Contribute = append(out.Contribute, kind)
			}
		}
	}
	return ok(out)
}

func submissionMsg(s enterprise.Submission) *pb.Submission {
	m := &pb.Submission{Id: s.ID, Kind: s.Kind, Name: s.Name, Note: s.Note, State: s.State, Reason: s.Reason, PublishedVersion: s.Published, CreatedAt: timestamppb.New(s.CreatedAt)}
	if s.DecidedAt != nil {
		m.DecidedAt = timestamppb.New(*s.DecidedAt)
	}
	for _, v := range s.Verdicts {
		m.Verdicts = append(m.Verdicts, &pb.SubmissionVerdict{Check: v.Check, Score: v.Score, Passed: v.Passed, Reason: v.Reason, Findings: v.Findings})
	}
	return m
}

// errCatalogRemoved answers the retired organization catalog's calls
// (spec_053 EN-97): kept in the API so an older client's call fails
// plainly.
func errCatalogRemoved() error {
	return apiError(connect.CodeUnimplemented, "CATALOG_REMOVED", errors.New("your organization's catalog is retired: its library comes with your policy, and Share offers yours to it"))
}

// ListPublishable is the retired catalog's: CATALOG_REMOVED.
func (h configService) ListPublishable(context.Context, req[pb.ListPublishableRequest]) (*connect.Response[pb.ListPublishableResponse], error) {
	return nil, errCatalogRemoved()
}

// PublishToOrganization is the retired catalog's: CATALOG_REMOVED.
func (h configService) PublishToOrganization(context.Context, req[pb.PublishToOrganizationRequest]) (*connect.Response[pb.PublishToOrganizationResponse], error) {
	return nil, errCatalogRemoved()
}

// ListCatalogReviews is the retired catalog's: CATALOG_REMOVED.
func (h configService) ListCatalogReviews(context.Context, req[pb.ListCatalogReviewsRequest]) (*connect.Response[pb.ListCatalogReviewsResponse], error) {
	return nil, errCatalogRemoved()
}

// CopyOrgItem is the retired catalog's: CATALOG_REMOVED.
func (h configService) CopyOrgItem(context.Context, req[pb.CopyOrgItemRequest]) (*connect.Response[pb.CopyOrgItemResponse], error) {
	return nil, errCatalogRemoved()
}
