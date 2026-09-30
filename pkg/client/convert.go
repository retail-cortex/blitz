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
	"errors"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/images"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Conversions from the service's messages to pkg/api's types, and from
// its errors back to api's typed errors, so front ends handle a remote
// workspace's results exactly like a local one's.

// sentinels are app's errors that carry no data, by ErrorInfo reason.
var sentinels = map[string]error{
	"NO_ACTIVE_SESSION":        api.ErrNoActiveSession,
	"MAX_TURNS":                api.ErrMaxTurns,
	"UNKNOWN_MODE":             api.ErrUnknownMode,
	"BAD_RULE":                 api.ErrBadRule,
	"UNKNOWN_COMMAND":          api.ErrUnknownCommand,
	"BYPASS_NEEDS_SANDBOX":     api.ErrBypassNeedsSandbox,
	"COST_LIMIT":               api.ErrCostLimit,
	"TIME_LIMIT":               api.ErrTimeLimit,
	"SNAPSHOT_NAME_TAKEN":      api.ErrSnapshotNameTaken,
	"NO_NOTE":                  api.ErrNoNote,
	"NO_GOAL":                  api.ErrNoGoal,
	"BAD_MODEL_REF":            api.ErrBadModelRef,
	"INVALID_AGENCY":           api.ErrInvalidAgency,
	"UNDO_CONFLICT":            api.ErrUndoConflict,
	"NOTHING_TO_UNDO":          api.ErrNothingToUndo,
	"SESSION_BUSY":             api.ErrSessionBusy,
	"NOT_REWIND_POINT":         api.ErrNotRewindPoint,
	"CANT_REWIND_CONVERSATION": api.ErrCantRewindConversation,
	"UNKNOWN_REWIND_MODE":      api.ErrUnknownRewindMode,
	"SCRIPTS_DISABLED":         api.ErrScriptsDisabled,
	"UNKNOWN_LOCALE":           api.ErrUnknownLocale,
	"IMAGES_DISABLED":          api.ErrImagesDisabled,
	"NO_FETCH":                 api.ErrNoFetch,
	"NO_SEARCH":                api.ErrNoSearch,
	"NOTHING_TO_COMPACT":       api.ErrNothingToCompact,
	"WORKSPACE_BUSY":           api.ErrWorkspaceBusy,
	"UNKNOWN_WORKER":           api.ErrUnknownWorker,
	"WORKER_DISABLED":          api.ErrWorkerNotEnabled,
	"RUN_IN_PROGRESS":          api.ErrRunInProgress,
	"WORKERS_DISABLED":         api.ErrWorkersDisabled,
	"HASH_MISMATCH":            api.ErrHashMismatch,
	"UNKNOWN_PROCESS":          api.ErrUnknownProcess,
	"PROJECT_CHANGED":          api.ErrProjectChanged,
	"UNKNOWN_TASK":             api.ErrUnknownTask,
	"UNKNOWN_RUN":              api.ErrUnknownRun,
	"UNKNOWN_REQUEST":          api.ErrUnknownRequest,
}

// fromAPI turns a failed call's error into app's typed error for its
// reason, or an error with the service's message.
func fromAPI(err error) error {
	if err == nil {
		return nil
	}
	var ce *connect.Error
	if !errors.As(err, &ce) {
		return err
	}
	for _, d := range ce.Details() {
		if v, derr := d.Value(); derr == nil {
			if info, ok := v.(*pb.ErrorInfo); ok {
				return errorFromInfo(info)
			}
		}
	}
	return errors.New(ce.Message())
}

// errorFromInfo is app's error for an ErrorInfo (nil for nil).
func errorFromInfo(info *pb.ErrorInfo) error {
	if info == nil {
		return nil
	}
	msg := errors.New(info.Message)
	switch info.Reason {
	case "UNKNOWN_AGENT":
		return &api.UnknownAgentError{Name: info.Metadata["name"]}
	case "RESUME_FAILED":
		return &api.ResumeError{Err: msg}
	case "INVALID_SETTING":
		return &api.InvalidSettingError{Err: msg}
	case "UNKNOWN_SETTING":
		return &api.UnknownSettingError{Key: info.Metadata["key"]}
	case "PROMPT_BLOCKED":
		return &api.BlockedError{Reason: info.Metadata["reason"]}
	}
	if s, ok := sentinels[info.Reason]; ok {
		// errors.Is finds the sentinel; the message stays the service's.
		return wrapped{msg: info.Message, sentinel: s}
	}
	return msg
}

// wrapped is a sentinel error with the service's message.
type wrapped struct {
	msg      string
	sentinel error
}

func (w wrapped) Error() string { return w.msg }
func (w wrapped) Unwrap() error { return w.sentinel }

func timeOf(t *timestamppb.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.AsTime()
}

func session(s *pb.SessionInfo) api.SessionInfo {
	if s == nil {
		return api.SessionInfo{}
	}
	out := api.SessionInfo{
		ID: s.Id, Title: s.Title, Agent: s.Agent, Workspace: s.Workspace, Snapshot: s.Snapshot, From: s.From,
		MovedFrom: s.MovedFrom, MovedAt: int(s.MovedAt),
		MessageCount: int(s.MessageCount), Created: timeOf(s.Created), Updated: timeOf(s.Updated),
	}
	for _, m := range s.Messages {
		out.Messages = append(out.Messages, api.Message{Role: m.Role, Text: m.Text, Time: timeOf(m.Time), Kind: m.Kind})
	}
	return out
}

func sessions(list []*pb.SessionInfo) []api.SessionInfo {
	out := make([]api.SessionInfo, len(list))
	for i, s := range list {
		out[i] = session(s)
	}
	return out
}

func agent(a *pb.AgentInfo) api.AgentInfo {
	return api.AgentInfo{Name: a.GetName(), DisplayName: a.GetDisplayName(), Description: a.GetDescription(), Active: a.GetActive(), PinnedModel: a.GetPinnedModel()}
}

func saved(s *pb.Saved) api.Saved {
	out := api.Saved{Path: s.GetPath()}
	if s.GetError() != "" {
		out.Err = errors.New(s.GetError())
	}
	return out
}

func usage(u *pb.Usage) api.Usage {
	if u == nil {
		return api.Usage{}
	}
	return api.Usage{
		Calls: int(u.Calls), Input: u.Input, Cached: u.Cached, CacheWrite: u.CacheWrite, Output: u.Output,
		LastPrompt: u.LastPrompt, CostUSD: u.CostUsd, Priced: u.Priced,
		SearchQueries: int(u.SearchQueries), SearchCostUSD: u.SearchCostUsd,
	}
}

func modelSettings(s *pb.ModelSettings) config.ModelSettings {
	out := config.ModelSettings{Temperature: s.Temperature, TopP: s.TopP}
	if s.MaxTokens != nil {
		v := int(*s.MaxTokens)
		out.MaxTokens = &v
	}
	if s.Seed != nil {
		v := int(*s.Seed)
		out.Seed = &v
	}
	out.ReasoningEffort = s.ReasoningEffort
	if s.ThinkingBudget != nil {
		v := int(*s.ThinkingBudget)
		out.ThinkingBudget = &v
	}
	return out
}

func modelSettingsInfo(m *pb.ModelSettingsInfo) api.ModelSettingsInfo {
	if m == nil {
		return api.ModelSettingsInfo{}
	}
	return api.ModelSettingsInfo{
		Model: m.Model, Provider: m.Provider, Settings: modelSettings(m.GetSettings()),
		GlobalTemperature: m.GlobalTemperature, GlobalMaxTokens: int(m.GlobalMaxTokens),
	}
}

func skill(s *pb.SkillInfo) api.SkillInfo {
	out := api.SkillInfo{
		Name: s.Name, Description: s.Description, Version: s.Version, License: s.License, Category: s.Category,
		Compatibility: s.Compatibility, Tags: s.Tags, Hash: s.Hash, Tier: s.Tier, Bypass: s.Bypass,
		Network: s.Network, NeedsNetwork: s.NeedsNetwork, Env: s.Env, Withheld: s.Withheld, Blocked: s.Blocked,
	}
	for _, t := range s.Tools {
		out.Tools = append(out.Tools, api.SkillTool{Name: t.Name, Scopes: t.Scopes, Why: t.Why})
	}
	for _, sc := range s.Scripts {
		out.Scripts = append(out.Scripts, api.SkillScript{
			Name: sc.Name, Language: sc.Language, Source: sc.Source, Timeout: sc.GetTimeout().AsDuration(),
			Deps: sc.Deps, Allowed: sc.Allowed, Reasons: sc.Reasons,
		})
	}
	return out
}

// image is an image held by the service: its data stays there.
func imageOf(m *pb.Image) *images.Image {
	return &images.Image{
		Name: m.Name, MIME: m.MimeType, Width: int(m.Width), Height: int(m.Height), SHA256: m.Id,
		Resized: m.Resized, Size: int(m.Size),
	}
}

// event converts a model event (text, tool call or result).
func event(ev *pb.TurnEvent) (api.Event, bool) {
	out := api.Event{Author: ev.Author}
	switch k := ev.Kind.(type) {
	case *pb.TurnEvent_Text:
		out.Text = &api.Text{Text: k.Text.Text, Partial: k.Text.Partial, Repeat: k.Text.Repeat, Thought: k.Text.Thought}
	case *pb.TurnEvent_ToolCall:
		out.ToolCall = &api.ToolCall{ID: k.ToolCall.Id, Name: k.ToolCall.Name, Args: k.ToolCall.GetArgs().AsMap(), Partial: k.ToolCall.Partial}
	case *pb.TurnEvent_ToolResult:
		out.ToolResult = &api.ToolResult{ID: k.ToolResult.Id, Name: k.ToolResult.Name, Result: k.ToolResult.GetResult().AsMap()}
	case *pb.TurnEvent_Tasks:
		out.Tasks = []api.Task{}
		for _, t := range k.Tasks.Items {
			out.Tasks = append(out.Tasks, api.Task{Content: t.Content, Status: t.Status})
		}
	case *pb.TurnEvent_Notice:
		out.Notice = &api.Notice{Text: k.Notice.Text, Error: k.Notice.Error}
	default:
		return api.Event{}, false
	}
	return out, true
}

func actionKind(k pb.ActionKind) api.ActionKind {
	switch k {
	case pb.ActionKind_ACTION_KIND_COMMAND:
		return api.ActionCommand
	case pb.ActionKind_ACTION_KIND_WRITE:
		return api.ActionWrite
	case pb.ActionKind_ACTION_KIND_DELETE:
		return api.ActionDelete
	case pb.ActionKind_ACTION_KIND_NETWORK:
		return api.ActionNetwork
	case pb.ActionKind_ACTION_KIND_MCP:
		return api.ActionMCP
	}
	return ""
}

func decisionMsg(d api.Decision) pb.Decision {
	switch d {
	case api.DecisionOnce:
		return pb.Decision_DECISION_ONCE
	case api.DecisionSession:
		return pb.Decision_DECISION_SESSION
	case api.DecisionAlways:
		return pb.Decision_DECISION_ALWAYS
	}
	return pb.Decision_DECISION_DENY
}
