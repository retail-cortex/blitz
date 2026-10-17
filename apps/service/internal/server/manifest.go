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

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The workspace manifest's editors (spec_manifest_040): what they share.

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
