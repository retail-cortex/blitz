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
	"github.com/retail-cortex/blitz/pkg/api"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// ProjectSettings are the workspace's project settings in the service
// (WorkspaceService.GetProjectSettings); on failure warn is told and they
// are empty.
func (r *Remote) ProjectSettings() api.ProjectSettings {
	res, err := r.workspaces.GetProjectSettings(context.Background(), connect.NewRequest(&pb.GetProjectSettingsRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("reading the project settings", err)
		return api.ProjectSettings{State: api.TrustNone}
	}
	return projectSettings(res.Msg.Settings)
}

// TrustProject records the decision in the service, which reopens the
// workspace with it unless a turn is running there
// (WorkspaceService.TrustProject).
func (r *Remote) TrustProject(hash string, trusted bool) error {
	_, err := r.workspaces.TrustProject(context.Background(), connect.NewRequest(&pb.TrustProjectRequest{Workspace: r.dir, Hash: hash, Trusted: trusted}))
	return fromAPI(err)
}

// ForgetProjectTrust forgets the decision about the project settings in
// the service, which reopens the workspace without them unless a turn is
// running there (WorkspaceService.ForgetProjectTrust).
func (r *Remote) ForgetProjectTrust() error {
	_, err := r.workspaces.ForgetProjectTrust(context.Background(), connect.NewRequest(&pb.ForgetProjectTrustRequest{Workspace: r.dir}))
	return fromAPI(err)
}

func projectSettings(m *pb.ProjectSettings) api.ProjectSettings {
	if m == nil {
		return api.ProjectSettings{State: api.TrustNone}
	}
	list := func(in []*pb.ProjectItem) []api.ProjectItem {
		out := make([]api.ProjectItem, len(in))
		for i, it := range in {
			out[i] = api.ProjectItem{File: it.File, Kind: it.Kind, Key: it.Key, Value: it.Value, Reason: it.Reason}
		}
		return out
	}
	return api.ProjectSettings{
		Files: m.Files, State: m.State, Hash: m.Hash, Loaded: m.Loaded,
		Applied: list(m.Applied), Pending: list(m.Pending), Ignored: list(m.Ignored), Problems: m.Problems,
	}
}

// ListNotes are the notes the agent saved in the workspace
// (WorkspaceService.ListNotes).
func (r *Remote) ListNotes() ([]api.Note, error) {
	res, err := r.workspaces.ListNotes(context.Background(), connect.NewRequest(&pb.ListNotesRequest{Workspace: r.dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]api.Note, len(res.Msg.Notes))
	for i, n := range res.Msg.Notes {
		out[i] = api.Note{Name: n.Name, Kind: n.Kind, Text: n.Text, Time: n.Time.AsTime(), Path: n.Path}
	}
	return out, nil
}

// ForgetNote deletes one (WorkspaceService.ForgetNote).
func (r *Remote) ForgetNote(name string) error {
	_, err := r.workspaces.ForgetNote(context.Background(), connect.NewRequest(&pb.ForgetNoteRequest{Workspace: r.dir, Name: name}))
	return fromAPI(err)
}

// ListHooks are the workspace's hooks in the service
// (WorkspaceService.ListHooks); on failure warn is told and there are none.
func (r *Remote) ListHooks() []api.HookInfo {
	res, err := r.workspaces.ListHooks(context.Background(), connect.NewRequest(&pb.ListHooksRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing the hooks", err)
		return nil
	}
	out := make([]api.HookInfo, len(res.Msg.Hooks))
	for i, k := range res.Msg.Hooks {
		out[i] = api.HookInfo{Event: k.Event, Type: k.Type, Match: k.Match, If: k.If, Runs: k.Runs, Source: k.Source, FailClosed: k.FailClosed}
		for _, f := range k.Failures {
			out[i].Failures = append(out[i].Failures, api.HookFailure{Time: f.Time.AsTime(), Error: f.Error})
		}
	}
	return out
}

// ListStyles are the output styles (WorkspaceService.ListStyles); on
// failure warn is told and there are none.
func (r *Remote) ListStyles() []api.StyleInfo {
	res, err := r.workspaces.ListStyles(context.Background(), connect.NewRequest(&pb.ListStylesRequest{Workspace: r.dir}))
	if err != nil {
		r.failed("listing the output styles", err)
		return nil
	}
	out := make([]api.StyleInfo, len(res.Msg.Styles))
	for i, s := range res.Msg.Styles {
		out[i] = api.StyleInfo{Name: s.Name, Description: s.Description, Source: s.Source, Active: s.Active}
	}
	return out
}

// TakeProcessNotices are what the session's watched processes reported
// while it was idle (WorkspaceService.TakeProcessNotices).
func (r *Remote) TakeProcessNotices(session string) []string {
	res, err := r.workspaces.TakeProcessNotices(context.Background(), connect.NewRequest(&pb.TakeProcessNoticesRequest{Workspace: r.dir, SessionId: session}))
	if err != nil {
		return nil
	}
	return res.Msg.Notices
}
