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
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

func (h workspaceService) ListAgents(ctx context.Context, r req[pb.ListAgentsRequest]) (*connect.Response[pb.ListAgentsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListAgentsResponse{}
	for _, a := range w.ListAgents() {
		out.Agents = append(out.Agents, agentMsg(a))
	}
	return ok(out)
}

func (h workspaceService) SetAgent(ctx context.Context, r req[pb.SetAgentRequest]) (*connect.Response[pb.SetAgentResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	a, err := w.SetAgent(ctx, r.Msg.Name)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.SetAgentResponse{Agent: agentMsg(a)})
}

func (h workspaceService) GetModel(ctx context.Context, r req[pb.GetModelRequest]) (*connect.Response[pb.GetModelResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	// Asking is when a sign-in made outside Blitz is noticed.
	w.RetryModel(ctx)
	m := w.Model()
	out := &pb.GetModelResponse{Name: m.Name, Provider: m.Provider}
	if err := w.ModelErr(); err != nil {
		out.Unavailable = engine.ModelErrorSummary(err, w.Config())
	}
	return ok(out)
}

func (h workspaceService) SetModel(ctx context.Context, r req[pb.SetModelRequest]) (*connect.Response[pb.SetModelResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	pin, err := w.SetModel(ctx, r.Msg.Ref)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.SetModelResponse{ActivePin: pin})
}

func (h workspaceService) PinModel(ctx context.Context, r req[pb.PinModelRequest]) (*connect.Response[pb.PinModelResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.PinModel(ctx, r.Msg.Agent, r.Msg.Ref)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.PinModelResponse{Agent: res.Agent, Model: res.Model, Saved: savedMsg(res.Saved)})
}

func (h workspaceService) UnpinModel(ctx context.Context, r req[pb.UnpinModelRequest]) (*connect.Response[pb.UnpinModelResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.Unpin(ctx, r.Msg.Agent)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.UnpinModelResponse{Agent: res.Agent, Model: res.Model, Saved: savedMsg(res.Saved)})
}

func (h workspaceService) GetModelSettings(ctx context.Context, r req[pb.GetModelSettingsRequest]) (*connect.Response[pb.GetModelSettingsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if r.Msg.Ref == "" {
		out := &pb.GetModelSettingsResponse{All: map[string]*pb.ModelSettings{}}
		for name, s := range w.AllModelSettings() {
			out.All[name] = modelSettingsMsg(s)
		}
		return ok(out)
	}
	info, err := w.ModelSettings(r.Msg.Ref)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.GetModelSettingsResponse{Model: modelSettingsInfoMsg(info)})
}

func (h workspaceService) UpdateModelSettings(ctx context.Context, r req[pb.UpdateModelSettingsRequest]) (*connect.Response[pb.UpdateModelSettingsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	var changes []api.Setting
	for _, c := range r.Msg.Changes {
		changes = append(changes, api.Setting{Key: c.Key, Value: c.Value})
	}
	res, err := w.UpdateModelSettings(r.Msg.Ref, r.Msg.Reset_, changes)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.UpdateModelSettingsResponse{Model: modelSettingsInfoMsg(res.ModelSettingsInfo), Unsupported: res.Unsupported, Saved: savedMsg(res.Saved)})
}

func (h workspaceService) GetSettings(ctx context.Context, r req[pb.GetSettingsRequest]) (*connect.Response[pb.GetSettingsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	st := w.Settings()
	return ok(&pb.GetSettingsResponse{
		Agency: st.Agency,
		Model:  st.Model.Name, Provider: st.Model.Provider, Agent: st.Agent, Locale: st.Locale,
		ImagesEnabled: w.ImagesEnabled(), PermissionMode: st.PermissionMode, ModeNote: st.ModeNote, Effort: st.Effort, Style: st.Style,
		SpeechModel: st.SpeechModel, SpeechError: st.SpeechError,
		ImageModel: st.ImageModel, ImageError: st.ImageError,
		AcceptedMedia: acceptedMediaMsg(w.AcceptedMedia()),
	})
}

func (h workspaceService) SetSetting(ctx context.Context, r req[pb.SetSettingRequest]) (*connect.Response[pb.SetSettingResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	key, err := w.Set(ctx, r.Msg.Key, r.Msg.Value)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.SetSettingResponse{Key: key})
}

func (h workspaceService) ListLocales(ctx context.Context, r req[pb.ListLocalesRequest]) (*connect.Response[pb.ListLocalesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	list, dir := w.AvailableLocales()
	out := &pb.ListLocalesResponse{CustomDir: dir}
	for _, l := range list {
		out.Locales = append(out.Locales, &pb.LocaleInfo{Tag: l.Tag, Name: l.Name})
	}
	return ok(out)
}

func (h workspaceService) SetLocale(ctx context.Context, r req[pb.SetLocaleRequest]) (*connect.Response[pb.SetLocaleResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.SetLocale(ctx, r.Msg.Input)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.SetLocaleResponse{Tag: res.Tag, NativeName: res.NativeName, LanguageName: res.LanguageName, HasCatalog: res.HasCatalog, Saved: savedMsg(res.Saved)})
}

func (h workspaceService) ListStyles(ctx context.Context, r req[pb.ListStylesRequest]) (*connect.Response[pb.ListStylesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListStylesResponse{}
	for _, s := range w.ListStyles() {
		out.Styles = append(out.Styles, &pb.Style{Name: s.Name, Description: s.Description, Source: s.Source, Active: s.Active})
	}
	return ok(out)
}

func (h workspaceService) ListCommands(ctx context.Context, r req[pb.ListCommandsRequest]) (*connect.Response[pb.ListCommandsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res := &pb.ListCommandsResponse{}
	for _, c := range w.ListCommands() {
		res.Commands = append(res.Commands, &pb.Command{Name: c.Name, Description: c.Description, ArgumentHint: c.ArgumentHint, Source: c.Source})
	}
	return ok(res)
}

func (h workspaceService) ListPermissionRules(ctx context.Context, r req[pb.ListPermissionRulesRequest]) (*connect.Response[pb.ListPermissionRulesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res := &pb.ListPermissionRulesResponse{}
	if err := w.PermissionsProblem(); err != nil {
		res.Problem = err.Error()
	}
	for _, x := range w.ListPermissionRules() {
		res.Rules = append(res.Rules, &pb.PermissionRule{Effect: x.Effect, Rule: x.Rule, Source: x.Source})
	}
	return ok(res)
}

func (h workspaceService) AddPermissionRule(ctx context.Context, r req[pb.AddPermissionRuleRequest]) (*connect.Response[pb.AddPermissionRuleResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.AddPermissionRule(r.Msg.Effect, r.Msg.Rule, saveScope(r.Msg.Save, r.Msg.Scope))
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.AddPermissionRuleResponse{Rule: res.Rule, NextStart: res.NextStart, Saved: savedMsg(res.Saved)})
}

func (h workspaceService) RemovePermissionRule(ctx context.Context, r req[pb.RemovePermissionRuleRequest]) (*connect.Response[pb.RemovePermissionRuleResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.RemovePermissionRuleOf(r.Msg.Rule, r.Msg.Effect, saveScope(r.Msg.Save, r.Msg.Scope))
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.RemovePermissionRuleResponse{Rule: res.Rule, Removed: int32(res.Removed), Saved: savedMsg(res.Saved)})
}

func (h workspaceService) SetPermissionMode(ctx context.Context, r req[pb.SetPermissionModeRequest]) (*connect.Response[pb.SetPermissionModeResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	mode, err := w.SetPermissionMode(r.Msg.Mode)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(&pb.SetPermissionModeResponse{Mode: mode})
}

// saveScope is where a rule is saved: nowhere without save, else the
// workspace's settings with scope "workspace", else the global ones.
func saveScope(save bool, scope string) api.Scope {
	switch {
	case !save:
		return api.ScopeSession
	case scope == string(api.ScopeWorkspace):
		return api.ScopeWorkspace
	}
	return api.ScopeGlobal
}
