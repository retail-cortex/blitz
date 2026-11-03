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

	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// OrgSync is an organization workspace's sync on this computer (EN-64):
// State is saved, saving, offline, signed_out, conflicts or refused ("" when
// no sync keeps the workspace in step); Error is why it's offline, signed
// out or refused, in Git's words; Pairs are the files with both versions
// kept (the shared one, this person's).
type OrgSync struct {
	State, Error string
	Pairs        [][2]string
}

// OrgSync is the sync of the workspace at dir.
func (s *Settings) OrgSync(ctx context.Context, dir string) (OrgSync, error) {
	res, err := s.c.GetOrgSync(ctx, connect.NewRequest(&pb.GetOrgSyncRequest{Workspace: dir}))
	if err != nil {
		return OrgSync{}, fromAPI(err)
	}
	return orgSync(res.Msg.GetState(), res.Msg.GetError(), res.Msg.GetPairs()), nil
}

// SaveOrgSync saves the workspace at dir's changes and sends what waits
// now, and is its sync after.
func (s *Settings) SaveOrgSync(ctx context.Context, dir string) (OrgSync, error) {
	res, err := s.c.SaveOrgSync(ctx, connect.NewRequest(&pb.SaveOrgSyncRequest{Workspace: dir}))
	if err != nil {
		return OrgSync{}, fromAPI(err)
	}
	return orgSync(res.Msg.GetState(), res.Msg.GetError(), res.Msg.GetPairs()), nil
}

func orgSync(state, why string, pairs []*pb.OrgSyncPair) OrgSync {
	out := OrgSync{State: state, Error: why}
	for _, p := range pairs {
		out.Pairs = append(out.Pairs, [2]string{p.GetShared(), p.GetMine()})
	}
	return out
}
