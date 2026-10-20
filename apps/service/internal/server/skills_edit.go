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

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Studio's skills (spec_skills_013 SK-100–SK-103).

func skillFileMsg(f api.SkillFile) *pb.SkillFile {
	return &pb.SkillFile{
		Skill: skillMsg(f.Info), Path: f.Path, Hash: f.Hash, Editable: f.Editable,
		Spec: &pb.SkillSpec{Name: f.Spec.Name, Description: f.Spec.Description, Tools: f.Spec.Tools, Agents: f.Spec.Agents, Members: f.Spec.Members, Instructions: f.Spec.Instructions, Triggers: f.Spec.Triggers},
	}
}

// skillErr is a skill edit's error as the API's: an unknown skill, an
// edit made elsewhere, else a path that won't do (outside the folder,
// not under references/ or scripts/, missing).
func skillErr(err error) error {
	switch {
	case errors.Is(err, skills.ErrUnknownSkill):
		return apiError(connect.CodeNotFound, "SKILL_NOT_FOUND", err)
	case errors.Is(err, skills.ErrChanged):
		return apiError(connect.CodeFailedPrecondition, "HASH_MISMATCH", err)
	}
	return apiError(connect.CodeInvalidArgument, "INVALID_PATH", err)
}

func (h workspaceService) GetSkillFile(ctx context.Context, r req[pb.GetSkillFileRequest]) (*connect.Response[pb.GetSkillFileResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	f, err := w.GetSkillFile(r.Msg.Name)
	if err != nil {
		return nil, skillErr(err)
	}
	return ok(&pb.GetSkillFileResponse{File: skillFileMsg(f)})
}

func (h workspaceService) SaveSkill(ctx context.Context, r req[pb.SaveSkillRequest]) (*connect.Response[pb.SaveSkillResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s := r.Msg.Skill
	if s == nil {
		return nil, invalid(errors.New("skill is required"))
	}
	f, problems, err := w.SaveSkill(api.SkillSpec{Name: s.Name, Description: s.Description, Tools: s.Tools, Agents: s.Agents, Members: s.Members, Instructions: s.Instructions, Triggers: s.Triggers}, r.Msg.PreviousName, r.Msg.Hash)
	if err != nil {
		return nil, skillErr(err)
	}
	if len(problems) > 0 {
		return ok(&pb.SaveSkillResponse{Problems: problems})
	}
	return ok(&pb.SaveSkillResponse{File: skillFileMsg(f)})
}

func (h workspaceService) DeleteSkill(ctx context.Context, r req[pb.DeleteSkillRequest]) (*connect.Response[pb.DeleteSkillResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.DeleteSkill(r.Msg.Name); err != nil {
		return nil, skillErr(err)
	}
	return ok(&pb.DeleteSkillResponse{})
}

func (h workspaceService) ReadSkillFile(ctx context.Context, r req[pb.ReadSkillFileRequest]) (*connect.Response[pb.ReadSkillFileResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	data, err := w.ReadSkillFile(r.Msg.Name, r.Msg.Path)
	if err != nil {
		return nil, skillErr(err)
	}
	return ok(&pb.ReadSkillFileResponse{Content: data})
}

func (h workspaceService) WriteSkillFile(ctx context.Context, r req[pb.WriteSkillFileRequest]) (*connect.Response[pb.WriteSkillFileResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	f, err := w.WriteSkillFile(r.Msg.Name, r.Msg.Path, r.Msg.Content)
	if err != nil {
		return nil, skillErr(err)
	}
	return ok(&pb.WriteSkillFileResponse{File: skillFileMsg(f)})
}

func (h workspaceService) DeleteSkillFile(ctx context.Context, r req[pb.DeleteSkillFileRequest]) (*connect.Response[pb.DeleteSkillFileResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	f, err := w.DeleteSkillFile(r.Msg.Name, r.Msg.Path)
	if err != nil {
		return nil, skillErr(err)
	}
	return ok(&pb.DeleteSkillFileResponse{File: skillFileMsg(f)})
}

func (h workspaceService) CopySkill(ctx context.Context, r req[pb.CopySkillRequest]) (*connect.Response[pb.CopySkillResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	f, problems, err := w.CopySkill(r.Msg.From, r.Msg.Name)
	if err != nil {
		return nil, skillErr(err)
	}
	if len(problems) > 0 {
		return ok(&pb.CopySkillResponse{Problems: problems})
	}
	return ok(&pb.CopySkillResponse{File: skillFileMsg(f)})
}

func (h workspaceService) RunSkillScript(ctx context.Context, r req[pb.RunSkillScriptRequest]) (*connect.Response[pb.RunSkillScriptResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	run := w.RunSkillScript(ctx, r.Msg.Name, r.Msg.Script, r.Msg.Args)
	return ok(&pb.RunSkillScriptResponse{
		ExitCode: int32(run.ExitCode), Stdout: run.Stdout, Stderr: run.Stderr, TimedOut: run.TimedOut,
		Sandbox: run.Sandbox, OutputFiles: run.Files, Error: run.Error,
	})
}
