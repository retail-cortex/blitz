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
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/images"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Conversions from internal/app's types to the API's messages.

func timestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func sessionMsg(s api.SessionInfo) *pb.SessionInfo {
	out := &pb.SessionInfo{
		Id: s.ID, Title: s.Title, Agent: s.Agent, Workspace: s.Workspace, Snapshot: s.Snapshot, From: s.From,
		MovedFrom: s.MovedFrom, MovedAt: int32(s.MovedAt),
		MessageCount: int32(s.MessageCount), Created: timestamp(s.Created), Updated: timestamp(s.Updated),
	}
	for _, m := range s.Messages {
		out.Messages = append(out.Messages, &pb.Message{Role: m.Role, Text: m.Text, Time: timestamp(m.Time), Kind: m.Kind})
	}
	return out
}

func sessionMsgs(list []api.SessionInfo) []*pb.SessionInfo {
	out := make([]*pb.SessionInfo, len(list))
	for i, s := range list {
		out[i] = sessionMsg(s)
	}
	return out
}

func usageMsg(u api.Usage) *pb.Usage {
	return &pb.Usage{
		Calls: int32(u.Calls), Input: u.Input, Cached: u.Cached, CacheWrite: u.CacheWrite, Output: u.Output,
		LastPrompt: u.LastPrompt, CostUsd: u.CostUSD, Priced: u.Priced,
		SearchQueries: int32(u.SearchQueries), SearchCostUsd: u.SearchCostUSD,
	}
}

func savedMsg(s api.Saved) *pb.Saved {
	out := &pb.Saved{Path: s.Path}
	if s.Err != nil {
		out.Error = s.Err.Error()
	}
	return out
}

func agentMsg(a api.AgentInfo) *pb.AgentInfo {
	return &pb.AgentInfo{Name: a.Name, DisplayName: a.DisplayName, Description: a.Description, Active: a.Active, PinnedModel: a.PinnedModel}
}

func modelSettingsMsg(s config.ModelSettings) *pb.ModelSettings {
	out := &pb.ModelSettings{Temperature: s.Temperature, TopP: s.TopP}
	if s.MaxTokens != nil {
		v := int32(*s.MaxTokens)
		out.MaxTokens = &v
	}
	if s.Seed != nil {
		v := int32(*s.Seed)
		out.Seed = &v
	}
	out.ReasoningEffort = s.ReasoningEffort
	if s.ThinkingBudget != nil {
		v := int32(*s.ThinkingBudget)
		out.ThinkingBudget = &v
	}
	return out
}

func modelSettingsInfoMsg(i api.ModelSettingsInfo) *pb.ModelSettingsInfo {
	return &pb.ModelSettingsInfo{
		Model: i.Model, Provider: i.Provider, Settings: modelSettingsMsg(i.Settings),
		GlobalTemperature: i.GlobalTemperature, GlobalMaxTokens: int32(i.GlobalMaxTokens),
	}
}

func skillMsg(s api.SkillInfo) *pb.SkillInfo {
	out := &pb.SkillInfo{
		Name: s.Name, Description: s.Description, Version: s.Version, License: s.License, Category: s.Category,
		Compatibility: s.Compatibility, Tags: s.Tags, Hash: s.Hash, Tier: s.Tier, Bypass: s.Bypass,
		Network: s.Network, NeedsNetwork: s.NeedsNetwork, Env: s.Env, Withheld: s.Withheld, Blocked: s.Blocked,
	}
	for _, t := range s.Tools {
		out.Tools = append(out.Tools, &pb.SkillTool{Name: t.Name, Scopes: t.Scopes, Why: t.Why})
	}
	for _, sc := range s.Scripts {
		out.Scripts = append(out.Scripts, &pb.SkillScript{
			Name: sc.Name, Language: sc.Language, Source: sc.Source, Timeout: durationpb.New(sc.Timeout),
			Deps: sc.Deps, Allowed: sc.Allowed, Reasons: sc.Reasons,
		})
	}
	return out
}

func skillMsgs(list []api.SkillInfo) []*pb.SkillInfo {
	out := make([]*pb.SkillInfo, len(list))
	for i, s := range list {
		out[i] = skillMsg(s)
	}
	return out
}

func imageMsg(img *images.Image) *pb.Image {
	return &pb.Image{
		Id: img.SHA256, Name: img.Name, MimeType: img.MIME, Width: int32(img.Width), Height: int32(img.Height),
		Size: int64(len(img.Data)), Resized: img.Resized,
	}
}

func approvalMsg(a api.Approval) *pb.Approval {
	return &pb.Approval{Key: a.Key, Kind: a.Kind, Subject: a.Subject, Dir: a.Dir, Always: a.Always, Added: timestamp(a.Added)}
}

// structMsg converts tool arguments or results; values JSON can't carry
// are dropped rather than failing the event.
func structMsg(m map[string]any) *structpb.Struct {
	if m == nil {
		return nil
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		return nil
	}
	return s
}

func eventMsg(e api.Event) *pb.TurnEvent {
	out := &pb.TurnEvent{Author: e.Author}
	switch {
	case e.Text != nil:
		out.Kind = &pb.TurnEvent_Text{Text: &pb.Text{Text: e.Text.Text, Partial: e.Text.Partial, Repeat: e.Text.Repeat, Thought: e.Text.Thought}}
	case e.ToolCall != nil:
		out.Kind = &pb.TurnEvent_ToolCall{ToolCall: &pb.ToolCall{Id: e.ToolCall.ID, Name: e.ToolCall.Name, Args: structMsg(e.ToolCall.Args), Partial: e.ToolCall.Partial}}
	case e.ToolResult != nil:
		out.Kind = &pb.TurnEvent_ToolResult{ToolResult: &pb.ToolResult{Id: e.ToolResult.ID, Name: e.ToolResult.Name, Result: structMsg(e.ToolResult.Result)}}
	case e.Tasks != nil:
		tasks := &pb.Tasks{}
		for _, t := range e.Tasks {
			tasks.Items = append(tasks.Items, &pb.Task{Content: t.Content, Status: t.Status})
		}
		out.Kind = &pb.TurnEvent_Tasks{Tasks: tasks}
	case e.Notice != nil:
		out.Kind = &pb.TurnEvent_Notice{Notice: &pb.Notice{Text: e.Notice.Text, Error: e.Notice.Error}}
	}
	return out
}

func actionKind(k api.ActionKind) pb.ActionKind {
	switch k {
	case api.ActionCommand:
		return pb.ActionKind_ACTION_KIND_COMMAND
	case api.ActionWrite:
		return pb.ActionKind_ACTION_KIND_WRITE
	case api.ActionDelete:
		return pb.ActionKind_ACTION_KIND_DELETE
	case api.ActionNetwork:
		return pb.ActionKind_ACTION_KIND_NETWORK
	case api.ActionMCP:
		return pb.ActionKind_ACTION_KIND_MCP
	}
	return pb.ActionKind_ACTION_KIND_UNSPECIFIED
}

func decision(d pb.Decision) api.Decision {
	switch d {
	case pb.Decision_DECISION_ONCE:
		return api.DecisionOnce
	case pb.Decision_DECISION_SESSION:
		return api.DecisionSession
	case pb.Decision_DECISION_ALWAYS:
		return api.DecisionAlways
	}
	return api.DecisionDeny
}

func processMsg(p api.ProcessInfo) *pb.Process {
	return &pb.Process{Id: int32(p.ID), Command: p.Command, Running: p.Running, ExitCode: int32(p.ExitCode), RuntimeMs: p.RuntimeMs}
}

func projectMsg(p api.ProjectSettings) *pb.ProjectSettings {
	list := func(in []api.ProjectItem) []*pb.ProjectItem {
		out := make([]*pb.ProjectItem, len(in))
		for i, it := range in {
			out[i] = &pb.ProjectItem{File: it.File, Kind: it.Kind, Key: it.Key, Value: it.Value, Reason: it.Reason}
		}
		return out
	}
	return &pb.ProjectSettings{
		Files: p.Files, State: p.State, Hash: p.Hash, Loaded: p.Loaded,
		Applied: list(p.Applied), Pending: list(p.Pending), Ignored: list(p.Ignored), Problems: p.Problems,
	}
}

func taskMsg(t api.TaskInfo) *pb.BackgroundTask {
	m := &pb.BackgroundTask{
		Id: t.ID, Agent: t.Agent, Prompt: t.Prompt, SessionId: t.Session, State: t.State,
		Started: timestamppb.New(t.Started), Result: t.Result, Error: t.Error, Usage: usageMsg(t.Usage),
		Worktree: t.Worktree, Branch: t.Branch,
	}
	if !t.Ended.IsZero() {
		m.Ended = timestamppb.New(t.Ended)
	}
	return m
}

// taskRequestMsg is a task's approval request or question, as a turn's
// are, with who asks.
func taskRequestMsg(r api.TaskRequest) *pb.TaskEvent {
	if a := r.Approval; a != nil {
		return &pb.TaskEvent{Kind: &pb.TaskEvent_ApprovalRequest{ApprovalRequest: &pb.ApprovalRequest{
			RequestId: r.ID, Tool: a.Tool, Kind: actionKind(a.Kind), Detail: a.Detail, Diff: a.Diff, ScopeLabel: a.KeyLabel,
			Agent: r.Agent, TaskId: r.TaskID,
		}}}
	}
	return &pb.TaskEvent{Kind: &pb.TaskEvent_Question{Question: &pb.Question{RequestId: r.ID, Question: r.Question, Options: r.Options, Agent: r.Agent, TaskId: r.TaskID, MultiSelect: r.MultiSelect}}}
}

func taskEventMsg(ev api.SessionEvent) *pb.TaskEvent {
	switch {
	case ev.Task != nil:
		return &pb.TaskEvent{Kind: &pb.TaskEvent_Task{Task: taskMsg(*ev.Task)}}
	case ev.Request != nil:
		return taskRequestMsg(*ev.Request)
	}
	return &pb.TaskEvent{Kind: &pb.TaskEvent_Resolved{Resolved: ev.Resolved}}
}
