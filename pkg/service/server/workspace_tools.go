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

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h workspaceService) ListSkills(ctx context.Context, r req[pb.ListSkillsRequest]) (*connect.Response[pb.ListSkillsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	list := w.ListSkills()
	if r.Msg.Query != "" {
		list = w.SearchSkills(r.Msg.Query)
	}
	return ok(&pb.ListSkillsResponse{Skills: skillMsgs(list)})
}

func (h workspaceService) GetSkill(ctx context.Context, r req[pb.GetSkillRequest]) (*connect.Response[pb.GetSkillResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, found := w.Skill(r.Msg.Name)
	if !found {
		return nil, apiError(connect.CodeNotFound, "SKILL_NOT_FOUND", fmt.Errorf("no skill %q", r.Msg.Name), "name", r.Msg.Name)
	}
	return ok(&pb.GetSkillResponse{Skill: skillMsg(s)})
}

func (h workspaceService) ListEnvs(ctx context.Context, r req[pb.ListEnvsRequest]) (*connect.Response[pb.ListEnvsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	list, err := w.ListEnvs()
	if err != nil {
		return nil, toAPI(err)
	}
	out := &pb.ListEnvsResponse{}
	for _, e := range list {
		out.Envs = append(out.Envs, &pb.Env{Key: e.Key, Deps: e.Deps, Skills: e.Skills, Size: e.Size, LastUsed: timestamp(e.LastUsed), Ready: e.Ready})
	}
	return ok(out)
}

func (h workspaceService) RemoveEnv(ctx context.Context, r req[pb.RemoveEnvRequest]) (*connect.Response[pb.RemoveEnvResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.RemoveEnv(r.Msg.Key); err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.RemoveEnvResponse{})
}

func (h workspaceService) PruneEnvs(ctx context.Context, r req[pb.PruneEnvsRequest]) (*connect.Response[pb.PruneEnvsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.PruneEnvs()
	if err != nil {
		return nil, toAPI(err)
	}
	out := &pb.PruneEnvsResponse{Removed: int32(res.Removed), Freed: res.Freed}
	for _, f := range res.Failed {
		out.Failed = append(out.Failed, &pb.EnvError{Key: f.Key, Error: f.Err.Error()})
	}
	return ok(out)
}

func (h workspaceService) ListMCPServers(ctx context.Context, r req[pb.ListMCPServersRequest]) (*connect.Response[pb.ListMCPServersResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListMCPServersResponse{}
	for _, m := range w.ListMCPServers() {
		out.Servers = append(out.Servers, &pb.MCPServer{Name: m.Name, Target: m.Target, AutoApprove: m.AutoApprove, Disabled: m.Disabled, State: m.State, Error: m.Error, Tools: int32(m.Tools)})
	}
	return ok(out)
}

func (h workspaceService) ListTools(ctx context.Context, r req[pb.ListToolsRequest]) (*connect.Response[pb.ListToolsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if r.Msg.All {
		out := &pb.ListToolsResponse{}
		for _, t := range w.AllTools() {
			out.Tools = append(out.Tools, &pb.ToolInfo{Name: t.Name, Description: t.Description, PlanAllowed: t.PlanAllowed})
		}
		return ok(out)
	}
	at := w.ActiveAgentTools()
	out := &pb.ListToolsResponse{Agent: at.Agent}
	for _, t := range at.Tools {
		out.Tools = append(out.Tools, &pb.ToolInfo{Name: t.Name, Description: t.Description, PlanAllowed: t.PlanAllowed})
	}
	for _, m := range at.MCP {
		out.Mcp = append(out.Mcp, &pb.MCPOffer{Server: m.Server, Tools: m.Tools, Prefix: m.Prefix})
	}
	return ok(out)
}

func (h workspaceService) ReloadMemory(ctx context.Context, r req[pb.ReloadMemoryRequest]) (*connect.Response[pb.ReloadMemoryResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	paths, err := w.ReloadMemory(ctx)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.ReloadMemoryResponse{Paths: paths, Files: w.MemoryFiles()})
}

func (h workspaceService) ReloadAgentFiles(ctx context.Context, r req[pb.ReloadAgentFilesRequest]) (*connect.Response[pb.ReloadAgentFilesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.Reload(ctx)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.ReloadAgentFilesResponse{Agents: int32(res.Agents), Skills: int32(res.Skills), Files: res.Files, Problems: res.Problems})
}

func (h workspaceService) AddMemory(ctx context.Context, r req[pb.AddMemoryRequest]) (*connect.Response[pb.AddMemoryResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	p, err := w.AddMemory(ctx, r.Msg.Text)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.AddMemoryResponse{Path: p})
}

func (h workspaceService) ListHooks(ctx context.Context, r req[pb.ListHooksRequest]) (*connect.Response[pb.ListHooksResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListHooksResponse{}
	for _, k := range w.ListHooks() {
		m := &pb.Hook{Event: k.Event, Type: k.Type, Match: k.Match, If: k.If, Runs: k.Runs, Source: k.Source, FailClosed: k.FailClosed}
		for _, f := range k.Failures {
			m.Failures = append(m.Failures, &pb.HookFailure{Time: timestamppb.New(f.Time), Error: f.Error})
		}
		out.Hooks = append(out.Hooks, m)
	}
	return ok(out)
}

func (h workspaceService) ListNotes(ctx context.Context, r req[pb.ListNotesRequest]) (*connect.Response[pb.ListNotesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	notes, err := w.ListNotes()
	if err != nil {
		return nil, toAPI(err)
	}
	out := &pb.ListNotesResponse{}
	for _, n := range notes {
		out.Notes = append(out.Notes, &pb.Note{Name: n.Name, Kind: n.Kind, Text: n.Text, Time: timestamppb.New(n.Time), Path: n.Path})
	}
	return ok(out)
}

func (h workspaceService) ForgetNote(ctx context.Context, r req[pb.ForgetNoteRequest]) (*connect.Response[pb.ForgetNoteResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.ForgetNote(r.Msg.Name); err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.ForgetNoteResponse{})
}

func (h workspaceService) LoadImage(ctx context.Context, r req[pb.LoadImageRequest]) (*connect.Response[pb.LoadImageResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	img, err := w.LoadImage(r.Msg.Path)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.LoadImageResponse{Image: w.keepImage(img)})
}

func (h workspaceService) AddImage(ctx context.Context, r req[pb.AddImageRequest]) (*connect.Response[pb.AddImageResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	img, err := w.AddImage(r.Msg.Name, r.Msg.Data)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.AddImageResponse{Image: w.keepImage(img)})
}

func (h workspaceService) ReadStoredFile(ctx context.Context, r req[pb.ReadStoredFileRequest]) (*connect.Response[pb.ReadStoredFileResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	data, mime, err := w.ReadStoredFile(r.Msg.Uri)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, apiError(connect.CodeNotFound, "FILE_NOT_FOUND", err)
	case errors.Is(err, engine.ErrUploadTooLarge):
		return nil, apiError(connect.CodeResourceExhausted, "UPLOAD_TOO_LARGE", err)
	case err != nil:
		return nil, toAPI(err)
	}
	return ok(&pb.ReadStoredFileResponse{Data: data, MimeType: mime})
}

func (h workspaceService) GetSearchProvider(ctx context.Context, r req[pb.GetSearchProviderRequest]) (*connect.Response[pb.GetSearchProviderResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	provider, err := w.SearchProvider()
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.GetSearchProviderResponse{Provider: provider})
}

func (h workspaceService) SearchWeb(ctx context.Context, r req[pb.SearchWebRequest]) (*connect.Response[pb.SearchWebResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	provider, err := w.SearchProvider()
	if err != nil {
		return nil, toAPI(err)
	}
	res, err := w.SearchWeb(ctx, r.Msg.Terms)
	if err != nil {
		return nil, toAPI(err)
	}
	out := &pb.SearchWebResponse{Provider: provider, Prompt: res.Prompt}
	for _, l := range res.Links {
		out.Links = append(out.Links, &pb.Link{Title: l.Title, Url: l.URL})
	}
	return ok(out)
}

func (h workspaceService) SearchWorkspace(ctx context.Context, r req[pb.SearchWorkspaceRequest]) (*connect.Response[pb.SearchWorkspaceResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.Search(ctx, api.SearchQuery{Text: r.Msg.Query, Sources: r.Msg.Sources, Limit: int(r.Msg.Limit), Mode: r.Msg.Mode, Hidden: r.Msg.Hidden, Folder: r.Msg.Folder})
	if err != nil {
		return nil, toAPI(err)
	}
	out := &pb.SearchWorkspaceResponse{Sources: res.Sources}
	for _, h := range res.Hits {
		out.Hits = append(out.Hits, &pb.SearchHit{Source: h.Source, Ref: h.Ref, Title: h.Title, Line: int32(h.Line), Section: h.Section, Snippet: h.Snippet, Score: h.Score, Summary: h.Summary, Tags: h.Tags})
	}
	return ok(out)
}

func (h workspaceService) GetSearchStatus(ctx context.Context, r req[pb.GetSearchStatusRequest]) (*connect.Response[pb.GetSearchStatusResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s := w.SearchStatus()
	st := &pb.SearchStatus{Enabled: s.Enabled, Scanning: s.Scanning, Unreadable: int32(s.Unreadable), Error: s.Error, Items: map[string]int32{},
		EmbeddingModel: s.EmbeddingModel, Embedded: int32(s.Embedded), EmbedError: s.EmbedError, Enriched: int32(s.Enriched), EnrichError: s.EnrichError,
		Problem: s.Problem, Settings: &pb.SearchSettings{Enabled: s.Settings.Enabled, Sources: s.Settings.Sources, EmbeddingModel: s.Settings.EmbeddingModel,
			Enrich: s.Settings.Enrich, EnrichModel: s.Settings.EnrichModel, EnrichDailyLimit: int32(s.Settings.EnrichDailyLimit), IncludeIgnored: s.Settings.IncludeIgnored}}
	for k, v := range s.Items {
		st.Items[k] = int32(v)
	}
	if !s.LastScan.IsZero() {
		st.LastScan = timestamppb.New(s.LastScan)
	}
	return ok(&pb.GetSearchStatusResponse{Status: st})
}

func (h workspaceService) Reindex(ctx context.Context, r req[pb.ReindexRequest]) (*connect.Response[pb.ReindexResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.Reindex(); err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.ReindexResponse{})
}
