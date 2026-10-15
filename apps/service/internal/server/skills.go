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
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/pkg/skillreg"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Skill registries for the desktop app's Settings › Skills (spec_skills_013
// SK-90..SK-96): the same store `blitz skills` uses, in the service's
// settings directory.

func (s *Server) skillStore() *skillreg.Store {
	if s.configDir == "" {
		return skillreg.Default()
	}
	return &skillreg.Store{
		File:       filepath.Join(s.configDir, "skill-registries.toml"),
		UserSkills: filepath.Join(s.configDir, "skills"),
		Secrets:    secrets.Default(s.configDir),
	}
}

// skillError maps the registries' errors to Connect codes and reasons.
func skillError(err error) error {
	switch {
	case errors.Is(err, skillreg.ErrNoRegistry):
		return apiError(connect.CodeNotFound, "NO_REGISTRY", err)
	case errors.Is(err, skillreg.ErrNotOffered):
		return apiError(connect.CodeNotFound, "NOT_OFFERED", err)
	case errors.Is(err, skillreg.ErrAmbiguous):
		return apiError(connect.CodeInvalidArgument, "AMBIGUOUS", err)
	case errors.Is(err, skillreg.ErrHash):
		return apiError(connect.CodeFailedPrecondition, "HASH_MISMATCH", err)
	case errors.Is(err, skillreg.ErrExists):
		return apiError(connect.CodeAlreadyExists, "SKILL_EXISTS", err)
	case errors.Is(err, skillreg.ErrNotInstalled):
		return apiError(connect.CodeNotFound, "NOT_INSTALLED", err)
	}
	// An index or archive that couldn't be read: the registry's trouble,
	// never the service's (not Unavailable, which means the service went).
	return apiError(connect.CodeFailedPrecondition, "REGISTRY_FAILED", err)
}

func installedSkill(i skillreg.Installed) *pb.InstalledSkill {
	return &pb.InstalledSkill{Name: i.Name, Registry: i.Registry, Version: i.Version, Dir: i.Dir, Installed: timestamp(i.Time)}
}

// problems are a joined error's parts, one line each.
func problems(err error) []string {
	if err == nil {
		return nil
	}
	return strings.Split(err.Error(), "\n")
}

func (h configService) ListSkillRegistries(context.Context, req[pb.ListSkillRegistriesRequest]) (*connect.Response[pb.ListSkillRegistriesResponse], error) {
	store := h.s.skillStore()
	regs, err := store.Registries()
	if err != nil {
		return nil, skillError(err)
	}
	all, err := store.Installed()
	if err != nil {
		return nil, skillError(err)
	}
	out := &pb.ListSkillRegistriesResponse{}
	for _, r := range regs {
		out.Registries = append(out.Registries, &pb.SkillRegistry{Name: r.Name, Url: r.URL, HasToken: store.HasToken(r.Name)})
	}
	for _, i := range all {
		out.Installed = append(out.Installed, installedSkill(i))
	}
	return ok(out)
}

func (h configService) AddSkillRegistry(ctx context.Context, r req[pb.AddSkillRegistryRequest]) (*connect.Response[pb.AddSkillRegistryResponse], error) {
	if strings.TrimSpace(r.Msg.Url) == "" {
		return nil, invalid(errors.New("url is required"))
	}
	store := h.s.skillStore()
	idx, err := store.Add(ctx, r.Msg.Name, strings.TrimSpace(r.Msg.Url), r.Msg.Token)
	if err != nil {
		return nil, skillError(err)
	}
	regs, _ := store.Registries()
	out := &pb.AddSkillRegistryResponse{Skills: int32(len(idx.Skills))}
	for _, reg := range regs {
		if reg.URL == strings.TrimSpace(r.Msg.Url) {
			out.Registry = &pb.SkillRegistry{Name: reg.Name, Url: reg.URL, HasToken: r.Msg.Token != ""}
		}
	}
	return ok(out)
}

func (h configService) RemoveSkillRegistry(_ context.Context, r req[pb.RemoveSkillRegistryRequest]) (*connect.Response[pb.RemoveSkillRegistryResponse], error) {
	if err := h.s.skillStore().Remove(r.Msg.Name); err != nil {
		return nil, skillError(err)
	}
	return ok(&pb.RemoveSkillRegistryResponse{})
}

func (h configService) SearchRegistrySkills(ctx context.Context, r req[pb.SearchRegistrySkillsRequest]) (*connect.Response[pb.SearchRegistrySkillsResponse], error) {
	found, err := h.s.skillStore().Search(ctx, r.Msg.Query)
	out := &pb.SearchRegistrySkillsResponse{Problems: problems(err)}
	for _, f := range found {
		out.Skills = append(out.Skills, &pb.RegistrySkill{Registry: f.Registry, Name: f.Name, Description: f.Description, Version: f.Version, CastorId: f.CastorID})
	}
	return ok(out)
}

func (h configService) InstallRegistrySkill(ctx context.Context, r req[pb.InstallRegistrySkillRequest]) (*connect.Response[pb.InstallRegistrySkillResponse], error) {
	dir, err := h.scopeDir(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	i, err := h.s.skillStore().Install(ctx, r.Msg.Ref, dir)
	if err != nil {
		return nil, skillError(err)
	}
	return ok(&pb.InstallRegistrySkillResponse{Skill: installedSkill(i)})
}

func (h configService) UpdateRegistrySkills(ctx context.Context, r req[pb.UpdateRegistrySkillsRequest]) (*connect.Response[pb.UpdateRegistrySkillsResponse], error) {
	done, err := h.s.skillStore().Update(ctx, r.Msg.Names...)
	out := &pb.UpdateRegistrySkillsResponse{Problems: problems(err)}
	for _, i := range done {
		out.Updated = append(out.Updated, installedSkill(i))
	}
	return ok(out)
}

func (h configService) UninstallRegistrySkill(_ context.Context, r req[pb.UninstallRegistrySkillRequest]) (*connect.Response[pb.UninstallRegistrySkillResponse], error) {
	dir, err := h.scopeDir(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	i, err := h.s.skillStore().Uninstall(r.Msg.Name, dir)
	if err != nil {
		return nil, skillError(err)
	}
	return ok(&pb.UninstallRegistrySkillResponse{Skill: installedSkill(i)})
}

// scopeDir is a request's workspace, checked: "" for the user, else an
// absolute directory.
func (h configService) scopeDir(workspace string) (string, error) {
	if workspace == "" {
		return "", nil
	}
	if !filepath.IsAbs(workspace) {
		return "", apiError(connect.CodeInvalidArgument, "INVALID_WORKSPACE", errors.New("workspace must be an absolute directory"))
	}
	return filepath.Clean(config.ExpandHome(workspace)), nil
}
