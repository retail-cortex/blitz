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
	"sort"

	"connectrpc.com/connect"

	"github.com/retail-cortex/blitz/pkg/catalog"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// ListOrgItems are the organization's rules and guardrails the person
// works under (spec_053 EN-34, EN-39), as this computer keeps them: from
// a service with the policy, those it switches on for them, each locked,
// with who decided it; from one before, its catalog's.
func (h configService) ListOrgItems(_ context.Context, r req[pb.ListOrgItemsRequest]) (*connect.Response[pb.ListOrgItemsResponse], error) {
	c, _, err := h.s.orgClient()
	if err != nil {
		return nil, err
	}
	out := &pb.ListOrgItemsResponse{}
	m := enterprise.LoadCatalog(h.s.brandingDir(c))
	if m == nil {
		return ok(out)
	}
	out.Version = int32(m.Version)
	var org config.OrgLayer
	if cfg, err := config.Load(h.s.configDir); err == nil {
		org = cfg.Org
	}
	for _, f := range m.Files {
		if (f.Kind != catalog.Rule && f.Kind != catalog.Guardrail) || (r.Msg.Kind != "" && f.Kind != r.Msg.Kind) {
			continue
		}
		it := &pb.OrgItem{Kind: f.Kind, Name: f.Name, Path: f.Path, Lock: f.Lock, Groups: f.Groups}
		if path := f.Kind + "s." + f.Name; org.Explain[path].Level != "" {
			it.DecidedBy = org.Why(path)
		}
		out.Items = append(out.Items, it)
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].Path < out.Items[j].Path })
	return ok(out)
}
