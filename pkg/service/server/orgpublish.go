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
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"connectrpc.com/connect"

	"github.com/retail-cortex/blitz/pkg/catalog"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Publish to organization (spec_053 EN-36): a workspace's items, as the
// catalog lays them out (its .agents folder is laid out as the catalog
// is), sent as a review.

// agentsDir is where a workspace keeps its items.
func agentsDir(workspace string) string { return filepath.Join(workspace, ".agents") }

// itemFiles are the workspace's catalog files, by their paths in the
// catalog: every file catalog.Of knows, but MCP servers (those are the
// settings', not files here).
func itemFiles(workspace string) (map[string][]byte, error) {
	root := agentsDir(workspace)
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if kind, _, ok := catalog.Of(rel); !ok || kind == catalog.MCP {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = data
		return nil
	})
	return out, err
}

func (h configService) ListPublishable(_ context.Context, r req[pb.ListPublishableRequest]) (*connect.Response[pb.ListPublishableResponse], error) {
	files, err := itemFiles(r.Msg.Workspace)
	if err != nil {
		return nil, invalid(err)
	}
	out := &pb.ListPublishableResponse{}
	for _, p := range sortedPaths(files) {
		kind, name, _ := catalog.Of(p)
		if kind == catalog.SkillFile {
			continue // with its skill
		}
		it, problems := catalog.Check(p, files[p])
		lock := it.Lock
		if lock == "" {
			lock = catalog.Default
		}
		out.Items = append(out.Items, &pb.PublishableItem{Kind: kind, Name: name, Path: p, Problems: problems, Lock: lock, Groups: it.Groups})
	}
	return ok(out)
}

func sortedPaths(m map[string][]byte) []string {
	ps := make([]string, 0, len(m))
	for p := range m {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps
}

func reviewMsg(rv enterprise.Review) *pb.CatalogReview {
	m := &pb.CatalogReview{Id: rv.ID, Title: rv.Title, ByEmail: rv.ByEmail, Paths: rv.Paths, Problems: rv.Problems, State: rv.State, Note: rv.Note,
		Created: stamp(&rv.Created)}
	if rv.Version != nil {
		m.Version = int32(*rv.Version)
	}
	return m
}

func (h configService) PublishToOrganization(ctx context.Context, r req[pb.PublishToOrganizationRequest]) (*connect.Response[pb.PublishToOrganizationResponse], error) {
	if len(r.Msg.Items) == 0 {
		return nil, invalid(errors.New("choose what to publish"))
	}
	files, err := itemFiles(r.Msg.Workspace)
	if err != nil {
		return nil, invalid(err)
	}
	send := map[string][]byte{}
	var problems, names []string
	for _, it := range r.Msg.Items {
		var found bool
		for _, p := range sortedPaths(files) {
			kind, name, _ := catalog.Of(p)
			if name != it.Name || (kind != it.Kind && !(it.Kind == catalog.Skill && kind == catalog.SkillFile)) {
				continue
			}
			data := files[p]
			if kind != catalog.SkillFile {
				found = true
				if data, err = catalog.Place(p, data, it.Lock, it.Groups); err != nil {
					return nil, invalid(fmt.Errorf("%s: %w", p, err))
				}
				_, bad := catalog.Check(p, data)
				problems = append(problems, bad...)
			}
			send[p] = data
		}
		if !found {
			return nil, apiError(connect.CodeNotFound, "NOT_FOUND", fmt.Errorf("the workspace has no %s %q in its .agents folder", it.Kind, it.Name))
		}
		names = append(names, it.Kind+" "+it.Name)
	}
	if len(problems) > 0 {
		return nil, apiError(connect.CodeFailedPrecondition, "PROBLEMS", fmt.Errorf("the catalog would refuse it: %s", strings.Join(problems, "; ")))
	}
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(r.Msg.Title)
	if title == "" {
		title = "Publish " + strings.Join(names, ", ") + " from " + path.Base(filepath.ToSlash(r.Msg.Workspace))
	}
	rv, err := c.PublishCatalog(ctx, title, send)
	if err != nil {
		return nil, enterpriseError(err)
	}
	return ok(&pb.PublishToOrganizationResponse{Review: reviewMsg(rv)})
}

func (h configService) ListCatalogReviews(ctx context.Context, r req[pb.ListCatalogReviewsRequest]) (*connect.Response[pb.ListCatalogReviewsResponse], error) {
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	list, err := c.CatalogReviews(ctx, r.Msg.State)
	if err != nil {
		return nil, enterpriseError(err)
	}
	out := &pb.ListCatalogReviewsResponse{}
	for _, rv := range list {
		out.Reviews = append(out.Reviews, reviewMsg(rv))
	}
	return ok(out)
}
