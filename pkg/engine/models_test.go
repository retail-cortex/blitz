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

package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentsAndModel(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	a := w.ActiveAgent()
	require.Equal(t, "blitz", a.Name, "active agent %+v", a)
	require.True(t, a.Active, "active agent %+v", a)
	require.NotEqual(t, "", a.DisplayName, "active agent %+v", a)
	i := slices.IndexFunc(w.ListAgents(), func(a api.AgentInfo) bool { return a.Name == "qa" })
	require.GreaterOrEqual(t, i, 0, "qa not listed")
	_, err := w.SetAgent(ctx, "nobody")
	assert.Error(t, err, "switched to an unknown agent")
	a, err = w.SetAgent(ctx, "qa")
	require.NoError(t, err, "switch: %+v", a)
	require.True(t, a.Active, "switch: %+v %v", a, err)
	require.Equal(t, "qa", w.ActiveAgent().Name, "switch: %+v %v", a, err)

	_, err = w.SetModel(ctx, "broken")
	assert.Error(t, err, "switched to a model that failed to build")
	pin, err := w.SetModel(ctx, "openai/gpt-5")
	require.NoError(t, err, "set model: %q %v %+v", pin, err, w.Model())
	require.Equal(t, "", pin, "set model: %q %v %+v", pin, err, w.Model())
	require.Equal(t, "gpt-5", w.Model().Name, "set model: %q %v %+v", pin, err, w.Model())
	require.Equal(t, "openai/gpt-5", w.Config().Blitz.DefaultModel, "set model: %q %v %+v", pin, err, w.Model())
	// The active agent's pin still decides what it runs on.
	_, err = w.PinModel(ctx, "qa", "anthropic/claude-haiku-4-5")
	require.NoError(t, err)
	pin, err = w.SetModel(ctx, "gemini-3.8-flash")
	assert.NoError(t, err, "active pin = %q,", pin)
	assert.Equal(t, "claude-haiku-4-5", pin, "active pin = %q, %v", pin, err)
}

// workspacePins are the pins in force in w's workspace: the global ones
// with its own over them ("" there: not pinned).
func workspacePins(t *testing.T, w *Workspace) map[string]string {
	t.Helper()
	cfg, err := config.LoadWorkspace("", w.Dir())
	require.NoError(t, err)
	out := map[string]string{}
	for agent, ref := range cfg.AgentModels {
		if ref != "" {
			out[agent] = ref
		}
	}
	return out
}

// A pin is the workspace's: saved in its own settings, not the global
// ones. Unpinning an agent the global settings pin masks that pin there.
func TestPinAndUnpinSaveToTheWorkspace(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	var unknown *api.UnknownAgentError
	_, err := w.PinModel(ctx, "nobody", "x")
	require.ErrorAs(t, err, &unknown, "unknown agent: %v", err)
	require.Equal(t, "nobody", unknown.Name, "unknown agent: %v", err)
	res, err := w.PinModel(ctx, "qa", "anthropic/claude-haiku-4-5")
	require.NoError(t, err, "pin: %+v", res)
	require.Equal(t, "claude-haiku-4-5", res.Model, "pin: %+v %v", res, err)
	require.NoError(t, res.Saved.Err, "pin: %+v %v", res, err)
	require.NotEqual(t, "", res.Saved.Path, "pin: %+v %v", res, err)
	got := workspacePins(t, w)["qa"]
	assert.Equal(t, "anthropic/claude-haiku-4-5", got, "saved pin %q", got)
	assert.Empty(t, savedConfig(t).AgentModels, "the pin went to the global settings")
	a := w.ListAgents()[slices.IndexFunc(w.ListAgents(), func(a api.AgentInfo) bool { return a.Name == "qa" })]
	assert.Equal(t, "claude-haiku-4-5", a.PinnedModel, "listed pin %q", a.PinnedModel)
	res, err = w.Unpin(ctx, "qa")
	require.NoError(t, err, "unpin")
	require.Equal(t, "gemini-3.8-flash", res.Model, "unpin: %+v", res)
	assert.Empty(t, workspacePins(t, w), "pin still saved")

	// A global pin applies here until the workspace unpins it, which
	// leaves the global pin for the other workspaces.
	_, err = config.SaveAgentModel(config.ConfigDir(""), "qa", "anthropic/claude-opus-5")
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude-opus-5", workspacePins(t, w)["qa"])
	_, err = w.Unpin(ctx, "qa")
	require.NoError(t, err)
	assert.Empty(t, workspacePins(t, w), "the global pin still applies here")
	assert.Equal(t, "anthropic/claude-opus-5", savedConfig(t).AgentModels["qa"], "the global pin went")
}

func TestUpdateModelSettings(t *testing.T) {
	w := openTest(t)
	w.Config().LLM.Provider = "openai"
	_, err := w.UpdateModelSettings("temperature=1", false, nil)
	assert.ErrorIs(t, err, api.ErrBadModelRef, "bad ref: %v", err)
	res, err := w.UpdateModelSettings("openai/gpt-5", false, []api.Setting{{Key: "temperature", Value: "0.3"}, {Key: "seed", Value: "7"}})
	require.NoError(t, err, "update: %+v", res)
	require.Equal(t, "gpt-5", res.Model, "update: %+v %v", res, err)
	require.Equal(t, []string{"seed"}, res.Unsupported, "update: %+v %v", res, err)
	require.NoError(t, res.Saved.Err, "update: %+v %v", res, err)
	// An invalid value changes nothing, including the valid change before it.
	var invalid *api.InvalidSettingError
	_, err = w.UpdateModelSettings("gpt-5", false, []api.Setting{{Key: "top_p", Value: "0.5"}, {Key: "temperature", Value: "9"}})
	assert.ErrorAs(t, err, &invalid, "invalid: %v", err)
	s := savedConfig(t).ModelSettings["gpt-5"]
	assert.Nil(t, s.TopP, "saved %+v", s)
	assert.NotNil(t, s.Seed, "saved %+v", s)
	assert.Equal(t, 7, *s.Seed, "saved %+v", s)
	info, _ := w.ModelSettings("gpt-5")
	assert.Nil(t, info.Settings.TopP, "info %+v", info)
	assert.Equal(t, w.Config().Blitz.Temperature, info.GlobalTemperature, "info %+v", info)
	res, _ = w.UpdateModelSettings("gpt-5", true, nil)
	assert.True(t, res.Settings.IsZero(), "reset: %+v", res)
	assert.Len(t, w.AllModelSettings(), 0, "reset: %+v", res)

	// Thinking can't be turned off on a model that always thinks; a budget
	// is taken (as an effort).
	res, err = w.UpdateModelSettings("anthropic/claude-opus-5-5", false, []api.Setting{{Key: "thinking_budget", Value: "0"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"thinking_budget"}, res.Unsupported, "budget 0: %+v", res)
	res, err = w.UpdateModelSettings("anthropic/claude-opus-5-5", false, []api.Setting{{Key: "thinking_budget", Value: "8000"}})
	require.NoError(t, err)
	assert.Empty(t, res.Unsupported, "budget 8000: %+v", res)
}

func TestSetChangesSettings(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	_, err := w.Set(ctx, "agency", "reckless")
	assert.ErrorIs(t, err, api.ErrInvalidAgency, "agency: %v", err)
	var unknown *api.UnknownSettingError
	_, err = w.Set(ctx, "Colour", "blue")
	assert.ErrorAs(t, err, &unknown, "unknown: %v", err)
	assert.Equal(t, "colour", unknown.Key, "unknown: %v", err)
	key, err := w.Set(ctx, " Agency_Level ", "HIGH")
	require.NoError(t, err, "set: %q", key)
	require.Equal(t, "agency_level", key, "set: %q %v", key, err)
	s := w.Settings()
	assert.Equal(t, "high", s.Agency, "settings %+v", s)
	assert.Equal(t, "blitz", s.Agent, "settings %+v", s)
	assert.NotEqual(t, "", s.Locale, "settings %+v", s)
}

func TestSetEffort(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	key, err := w.Set(ctx, "reasoning_effort", "XHigh")
	require.NoError(t, err, "set: %q %v %q", key, err, w.Settings().Effort)
	require.Equal(t, "effort", key, "set: %q %v %q", key, err, w.Settings().Effort)
	require.Equal(t, "max", w.Settings().Effort, "set: %q %v %q", key, err, w.Settings().Effort)
	var invalid *api.InvalidSettingError
	_, err = w.Set(ctx, "effort", "extreme")
	assert.ErrorAs(t, err, &invalid, "invalid: %v, effort %q", err, w.Settings().Effort)
	assert.Equal(t, "max", w.Settings().Effort, "invalid: %v, effort %q", err, w.Settings().Effort)
	_, err = w.Set(ctx, "effort", "auto")
	assert.NoError(t, err, "auto: %v %q", err, w.Settings().Effort)
	assert.Equal(t, "", w.Settings().Effort, "auto: %v %q", err, w.Settings().Effort)
}

// Unpinning an agent with a default model of its own goes back to that
// model; one that can't be built is an error, as is an unknown agent.
func TestUnpinToTheAgentsDefaultModel(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	dir := t.TempDir()
	for name, ref := range map[string]string{"scout": "gemini-3.8-pro", "wreck": "broken"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\nname: "+name+"\ndescription: test agent\ndefault_model: "+ref+"\ntools: [read_file]\n---\nLook.\n"), 0o644))
	}
	require.NoError(t, w.agents.LoadExternalAgents(dir))
	res, err := w.Unpin(ctx, "scout")
	require.NoError(t, err)
	assert.Equal(t, "gemini-3.8-pro", res.Model)
	_, err = w.Unpin(ctx, "wreck")
	assert.ErrorContains(t, err, "no such model")
	var unknown *api.UnknownAgentError
	_, err = w.Unpin(ctx, "nobody")
	assert.ErrorAs(t, err, &unknown)
}
