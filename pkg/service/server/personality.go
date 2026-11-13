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
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The personalities (spec_064_personality PER-50): the person's files, so
// ConfigService's, with the open workspace's choice marked.

func personalityProto(p api.Personality) *pb.Personality {
	return &pb.Personality{Name: p.Name, Description: p.Description, Voice: p.Voice, Source: p.Source, Text: p.Text}
}

func (h configService) ListPersonalities(ctx context.Context, r req[pb.ListPersonalitiesRequest]) (*connect.Response[pb.ListPersonalitiesResponse], error) {
	cfg, err := config.Load(h.s.configDir)
	if err != nil {
		return nil, invalid(err)
	}
	def := strings.ToLower(strings.TrimSpace(cfg.UI.Personality))
	if def == "" {
		def = engine.DefaultPersonality
	}
	choice := ""
	if r.Msg.Workspace != "" {
		dir, err := scope(r.Msg.Workspace)
		if err != nil {
			return nil, err
		}
		h.s.mu.Lock()
		w := h.s.workspaces[dir]
		h.s.mu.Unlock()
		if w != nil {
			choice = w.Personality().Name
		}
	}
	out := &pb.ListPersonalitiesResponse{}
	for _, p := range engine.ListPersonalities(cfg.Org) {
		m := personalityProto(p)
		m.PersonDefault = p.Name == def
		m.WorkspaceChoice = p.Name == choice
		out.Personalities = append(out.Personalities, m)
	}
	return ok(out)
}

func (h configService) GetPersonality(_ context.Context, r req[pb.GetPersonalityRequest]) (*connect.Response[pb.GetPersonalityResponse], error) {
	cfg, err := config.Load(h.s.configDir)
	if err != nil {
		return nil, invalid(err)
	}
	p, err := engine.GetPersonality(cfg.Org, r.Msg.Name)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.GetPersonalityResponse{Personality: personalityProto(p)})
}

func (h configService) SavePersonality(ctx context.Context, r req[pb.SavePersonalityRequest]) (*connect.Response[pb.SavePersonalityResponse], error) {
	m := r.Msg.Personality
	if m == nil {
		return nil, invalid(errors.New("no personality"))
	}
	if err := engine.SavePersonality(api.Personality{Name: m.Name, Description: m.Description, Voice: m.Voice, Text: m.Text}); err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.SavePersonalityResponse{Change: h.changed(ctx, "", filepath.Join(engine.PersonalitiesDir(), m.Name+".md"))})
}

func (h configService) DeletePersonality(ctx context.Context, r req[pb.DeletePersonalityRequest]) (*connect.Response[pb.DeletePersonalityResponse], error) {
	cfg, err := config.Load(h.s.configDir)
	if err != nil {
		return nil, invalid(err)
	}
	if err := engine.DeletePersonality(cfg.Org, r.Msg.Name); err != nil {
		if errors.Is(err, engine.ErrPersonalityReadOnly) {
			return nil, apiError(connect.CodeFailedPrecondition, "READ_ONLY", err)
		}
		return nil, toAPI(err)
	}
	return ok(&pb.DeletePersonalityResponse{Change: h.changed(ctx, "", filepath.Join(engine.PersonalitiesDir(), r.Msg.Name+".md"))})
}

func (h workspaceService) TryPersonality(ctx context.Context, r req[pb.TryPersonalityRequest]) (*connect.Response[pb.TryPersonalityResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	answer, err := w.TryPersonality(ctx, r.Msg.Text)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.TryPersonalityResponse{Answer: answer, Question: engine.TryQuestion})
}
