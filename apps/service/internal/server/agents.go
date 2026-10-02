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
	"sync"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The agent editors' files: the workspace's .agents/agents and the user's
// ~/.blitz/agents. A workspace notices the change on its next use
// (RefreshAgents); the one asked about is refreshed at once.

func (h workspaceService) ListAgentFiles(ctx context.Context, r req[pb.ListAgentFilesRequest]) (*connect.Response[pb.ListAgentFilesResponse], error) {
	dir, _, err := h.agentsDir(ctx, r.Msg.Workspace, r.Msg.Scope)
	if err != nil {
		return nil, err
	}
	files, err := agents.ListFiles(dir, isBuiltinAgent)
	if err != nil {
		return nil, err
	}
	out := &pb.ListAgentFilesResponse{Dir: dir}
	for _, f := range files {
		out.Files = append(out.Files, agentFileMsg(f))
	}
	return ok(out)
}

func (h workspaceService) SaveAgentFile(ctx context.Context, r req[pb.SaveAgentFileRequest]) (*connect.Response[pb.SaveAgentFileResponse], error) {
	dir, w, err := h.agentsDir(ctx, r.Msg.Workspace, r.Msg.Scope)
	if err != nil {
		return nil, err
	}
	if r.Msg.Agent == nil {
		return nil, invalid(errors.New("agent is required"))
	}
	spec := agentSpec(r.Msg.Agent)
	path, problems, err := agents.Save(dir, spec, r.Msg.PreviousName, isBuiltinAgent)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return ok(&pb.SaveAgentFileResponse{Problems: problems})
	}
	if w != nil {
		w.RefreshAgents(ctx)
	}
	spec.Path = path
	return ok(&pb.SaveAgentFileResponse{File: agentFileMsg(agents.File{Path: path, Spec: spec})})
}

func (h workspaceService) DeleteAgentFile(ctx context.Context, r req[pb.DeleteAgentFileRequest]) (*connect.Response[pb.DeleteAgentFileResponse], error) {
	dir, w, err := h.agentsDir(ctx, r.Msg.Workspace, r.Msg.Scope)
	if err != nil {
		return nil, err
	}
	if r.Msg.Path != "" {
		if err := agents.DeleteFile(dir, r.Msg.Path); err != nil {
			return nil, invalid(err) // outside the folder, or gone
		}
	} else if err := agents.Delete(dir, r.Msg.Name); err != nil {
		return nil, toAPI(err)
	}
	if w != nil {
		w.RefreshAgents(ctx)
	}
	return ok(&pb.DeleteAgentFileResponse{})
}

// agentsDir is a scope's folder and, when one is named, the workspace,
// which the user's scope doesn't need.
func (h workspaceService) agentsDir(ctx context.Context, dir string, scope pb.AgentScope) (string, *workspace, error) {
	var w *workspace
	if dir != "" || scope == pb.AgentScope_AGENT_SCOPE_WORKSPACE {
		var err error
		if w, err = h.s.workspace(ctx, dir); err != nil {
			return "", nil, err
		}
	}
	switch scope {
	case pb.AgentScope_AGENT_SCOPE_WORKSPACE:
		return w.ProjectAgentsDir(), w, nil
	case pb.AgentScope_AGENT_SCOPE_USER:
		return config.ExpandHome(config.UserAgentsDir), w, nil
	}
	return "", nil, invalid(errors.New("scope is required"))
}

// builtinAgents are the built-in agents' names, which files can't take.
var builtinAgents = sync.OnceValue(func() *agents.Registry {
	reg, _ := agents.NewRegistry() // the embedded agents always parse (tested)
	return reg
})

func isBuiltinAgent(name string) bool { return builtinAgents().IsBuiltin(name) }

func agentFileMsg(f agents.File) *pb.AgentFile {
	out := &pb.AgentFile{Path: f.Path, Problem: f.Problem}
	if f.Spec != nil {
		out.Agent = agentDefinition(f.Spec)
	}
	return out
}

func agentDefinition(s *agents.AgentSpec) *pb.AgentDefinition {
	d := &pb.AgentDefinition{
		Name: s.Name, DisplayName: s.DisplayName, Description: s.Description, Tools: s.Tools,
		DefaultModel: s.DefaultModel, AgencyLevel: s.AgencyLevel, PermissionMode: s.PermissionMode,
		MaxTurns: int32(s.MaxTurns), Background: s.Background, Isolation: s.Isolation,
		Temperature: s.Temperature, TopP: s.TopP, Effort: s.Effort, Prompt: s.SystemPrompt,
	}
	if s.MaxTokens != nil {
		d.MaxTokens = new(int32(*s.MaxTokens))
	}
	if s.ThinkingBudget != nil {
		d.ThinkingBudget = new(int32(*s.ThinkingBudget))
	}
	return d
}

func agentSpec(d *pb.AgentDefinition) *agents.AgentSpec {
	s := &agents.AgentSpec{
		Name: d.Name, DisplayName: d.DisplayName, Description: d.Description, Tools: d.Tools,
		DefaultModel: d.DefaultModel, AgencyLevel: d.AgencyLevel, PermissionMode: d.PermissionMode,
		MaxTurns: int(d.MaxTurns), Background: d.Background, Isolation: d.Isolation,
		Temperature: d.Temperature, TopP: d.TopP, Effort: d.Effort, SystemPrompt: d.Prompt,
	}
	if d.MaxTokens != nil {
		s.MaxTokens = new(int(*d.MaxTokens))
	}
	if d.ThinkingBudget != nil {
		s.ThinkingBudget = new(int(*d.ThinkingBudget))
	}
	return s
}

func (h workspaceService) GetSuggestions(ctx context.Context, r req[pb.GetSuggestionsRequest]) (*connect.Response[pb.GetSuggestionsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s := w.Suggestions(ctx)
	out := &pb.GetSuggestionsResponse{HarnessMissing: s.HarnessMissing, Pending: s.Pending}
	for _, t := range s.Tiles {
		out.Suggestions = append(out.Suggestions, &pb.Suggestion{
			Kind: suggestionKinds[t.Kind], Title: t.Title, Prompt: t.Prompt, SessionId: t.SessionID,
			Worker: t.Worker, Count: int32(t.Count), Detail: t.Detail,
		})
	}
	return ok(out)
}

var suggestionKinds = map[api.SuggestionKind]pb.SuggestionKind{
	api.SuggestSetup:        pb.SuggestionKind_SUGGESTION_KIND_SETUP,
	api.SuggestContinue:     pb.SuggestionKind_SUGGESTION_KIND_CONTINUE,
	api.SuggestChanges:      pb.SuggestionKind_SUGGESTION_KIND_CHANGES,
	api.SuggestWorkerFailed: pb.SuggestionKind_SUGGESTION_KIND_WORKER_FAILED,
	api.SuggestIdea:         pb.SuggestionKind_SUGGESTION_KIND_IDEA,
}
