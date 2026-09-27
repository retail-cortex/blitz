package app

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/internal/config"
	"github.com/retail-cortex/blitz/internal/runtime"
)

// Agents, models, pins, model settings and settings. Operations return data
// and typed errors, never text for the user: front ends word and localise
// the results.

// ListAgents returns every agent, in registry order.
func (w *Workspace) ListAgents() []api.AgentInfo {
	var out []api.AgentInfo
	for _, a := range w.agents.List() {
		out = append(out, w.agentInfo(a.Name))
	}
	return out
}

// ActiveAgent describes the agent that answers prompts.
func (w *Workspace) ActiveAgent() api.AgentInfo { return w.agentInfo(w.engine.ActiveAgent()) }

// SetAgent makes name the active agent.
func (w *Workspace) SetAgent(ctx context.Context, name string) (api.AgentInfo, error) {
	if err := w.engine.SetActiveAgent(ctx, name); err != nil {
		return api.AgentInfo{}, err
	}
	return w.agentInfo(name), nil
}

func (w *Workspace) agentInfo(name string) api.AgentInfo {
	info := api.AgentInfo{Name: name, Active: name == w.engine.ActiveAgent()}
	if spec, ok := w.agents.Get(name); ok {
		info.DisplayName, info.Description = spec.DisplayName, spec.Description
	}
	if m, pinned := w.engine.AgentModel(name); pinned {
		info.PinnedModel = m
	}
	return info
}

// Model returns the model the active agent runs on.
func (w *Workspace) Model() api.ModelInfo {
	return api.ModelInfo{Name: w.engine.ModelName(), Provider: w.cfg.LLM.Provider}
}

// SetModel switches every unpinned agent to ref (a name or "provider/model").
// It returns the active agent's pin when there is one, since that still
// decides what the active agent runs on.
func (w *Workspace) SetModel(ctx context.Context, ref string) (activePin string, err error) {
	llm, err := w.newModel(ctx, w.cfg, ref)
	if err == nil {
		err = w.engine.SetModel(ctx, llm)
	}
	if err != nil {
		return "", err
	}
	w.cfg.Blitz.DefaultModel = ref
	if m, pinned := w.engine.AgentModel(w.engine.ActiveAgent()); pinned {
		return m, nil
	}
	return "", nil
}

// PinModel runs agent on ref from now on and saves the pin in the config file.
func (w *Workspace) PinModel(ctx context.Context, agent, ref string) (api.PinResult, error) {
	if _, ok := w.agents.Get(agent); !ok {
		return api.PinResult{}, &api.UnknownAgentError{Name: agent}
	}
	llm, err := w.newModel(ctx, w.cfg, ref)
	if err == nil {
		err = w.engine.PinModel(ctx, agent, llm)
	}
	if err != nil {
		return api.PinResult{}, err
	}
	return api.PinResult{Agent: agent, Model: llm.Name(), Saved: w.saveAgentModel(agent, ref)}, nil
}

// Unpin returns agent to the configured model, or to its own default_model
// if it declares one, and removes the pin from the config file.
func (w *Workspace) Unpin(ctx context.Context, agent string) (api.PinResult, error) {
	spec, ok := w.agents.Get(agent)
	if !ok {
		return api.PinResult{}, &api.UnknownAgentError{Name: agent}
	}
	err := w.engine.Unpin(ctx, agent)
	if err == nil && spec.DefaultModel != "" {
		llm, merr := w.newModel(ctx, w.cfg, spec.DefaultModel)
		if err = merr; err == nil {
			err = w.engine.PinModel(ctx, agent, llm)
		}
	}
	if err != nil {
		return api.PinResult{}, err
	}
	m, _ := w.engine.AgentModel(agent)
	return api.PinResult{Agent: agent, Model: m, Saved: w.saveAgentModel(agent, "")}, nil
}

// saveAgentModel records (or with ref "" removes) a pin in the config file.
func (w *Workspace) saveAgentModel(agent, ref string) api.Saved {
	if ref == "" {
		delete(w.cfg.AgentModels, agent)
	} else {
		if w.cfg.AgentModels == nil {
			w.cfg.AgentModels = map[string]string{}
		}
		w.cfg.AgentModels[agent] = ref
	}
	path, err := config.SaveAgentModel(config.ConfigDir(""), agent, ref)
	return api.Saved{Path: path, Err: err}
}

// AllModelSettings returns every model's settings, by model name.
func (w *Workspace) AllModelSettings() map[string]config.ModelSettings {
	return w.engine.AllModelSettings()
}

// ModelSettings returns ref's settings ("provider/" optional).
func (w *Workspace) ModelSettings(ref string) (api.ModelSettingsInfo, error) {
	provider, name := runtime.ParseModelRef(ref, w.cfg.LLM.Provider)
	if strings.Contains(ref, "=") || name == "" {
		return api.ModelSettingsInfo{}, api.ErrBadModelRef
	}
	return api.ModelSettingsInfo{
		Model: name, Provider: provider, Settings: w.engine.ModelSettings(name),
		GlobalTemperature: w.cfg.Blitz.Temperature, GlobalMaxTokens: w.cfg.Blitz.MaxTokens,
	}, nil
}

// UpdateModelSettings applies changes to ref's settings (reset clears them
// all first). They apply from the next model call and are saved in the
// config file, under the key it already uses for the model if any.
func (w *Workspace) UpdateModelSettings(ref string, reset bool, changes []api.Setting) (api.ModelSettingsChange, error) {
	info, err := w.ModelSettings(ref)
	if err != nil {
		return api.ModelSettingsChange{}, err
	}
	s := info.Settings
	if reset {
		s = config.ModelSettings{}
	}
	for _, c := range changes {
		if err := s.Set(strings.TrimSpace(c.Key), c.Value); err != nil {
			return api.ModelSettingsChange{}, &api.InvalidSettingError{Err: err}
		}
	}
	w.engine.SetModelSettings(info.Model, s)
	info.Settings = s
	out := api.ModelSettingsChange{ModelSettingsInfo: info}
	for _, key := range config.ModelSettingKeys {
		if _, set := s.Get(key); set && !runtime.SettingSupported(info.Provider, info.Model, key) {
			out.Unsupported = append(out.Unsupported, key)
		}
	}
	path, err := config.SaveModelSettings(config.ConfigDir(""), settingsKey(w.cfg, info.Model), s)
	out.Saved = api.Saved{Path: path, Err: err}
	return out, nil
}

// settingsKey returns the [model_settings] key the config file already uses
// for model name, e.g. a hand-written "openai/gpt-5", so a change edits that
// table instead of adding a second one; otherwise name itself.
func settingsKey(cfg *config.Config, name string) string {
	if _, ok := cfg.ModelSettings[name]; ok {
		return name
	}
	for _, key := range slices.Sorted(maps.Keys(cfg.ModelSettings)) {
		if _, n := runtime.ParseModelRef(key, ""); n == name {
			return key
		}
	}
	return name
}

// Settings returns the current settings.
func (w *Workspace) Settings() api.Settings {
	return api.Settings{
		Agency: w.cfg.Blitz.AgencyLevel,
		Model:  w.Model(), Agent: w.engine.ActiveAgent(), Locale: w.reply.Tag().String(),
		PermissionMode: string(w.tools.Hooks().Mode()),
		Effort:         w.engine.Effort(),
	}
}

// SetPermissionMode changes which actions run without asking, for every
// session of the workspace, and returns the mode's canonical name. Bypass
// needs the OS sandbox (ErrBypassNeedsSandbox).
func (w *Workspace) SetPermissionMode(mode string) (string, error) {
	m, err := api.ParsePermissionMode(mode)
	if err != nil {
		return "", err
	}
	if err := w.tools.SetPermissionMode(m); err != nil {
		return "", err
	}
	return string(m), nil
}

// Set changes a setting for this session ("agency" or "agency_level") and
// returns the key in canonical form. The agents' instructions embed it, so
// they are rebuilt.
func (w *Workspace) Set(ctx context.Context, key, value string) (string, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "effort", "reasoning_effort":
		v := strings.ToLower(strings.TrimSpace(value))
		if v == "" || v == "auto" || v == "default" {
			w.engine.SetEffort("")
			return "effort", nil
		}
		effort, err := config.ParseEffort(v)
		if err != nil {
			return "effort", &api.InvalidSettingError{Err: err}
		}
		w.engine.SetEffort(effort)
		return "effort", nil
	case "agency", "agency_level":
		switch strings.ToLower(value) {
		case "low", "medium", "high", "extreme":
		default:
			return key, api.ErrInvalidAgency
		}
		w.cfg.Blitz.AgencyLevel = strings.ToLower(value)
	default:
		return key, &api.UnknownSettingError{Key: key}
	}
	return key, w.engine.Rebuild(ctx)
}
