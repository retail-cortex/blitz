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

package tui

import (
	"context"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelSettingsCommand(t *testing.T) {
	app, _ := newCommandApp(t, "")
	local(app).Config().LLM.Provider = "openai"
	run := func(cmd string) string {
		return captureStdout(t, func() { HandleCommand(context.Background(), cmd, app) })
	}

	assert.Contains(t, run("/model_settings"), "No model has settings", "empty list")
	assert.Contains(t, run("/model_settings temperature=1"), "Usage: /model_settings", "missing model")

	// Set several at once; a provider prefix is dropped from the name.
	out := run("/model_settings openai/gpt-5 temperature=0.3 max_tokens=2048 seed=7")
	assert.Contains(t, out, "gpt-5 now uses temperature=0.3 max_tokens=2048 seed=7", "set:\n%s", out)
	assert.Contains(t, out, "Saved in", "set:\n%s", out)
	assert.Contains(t, out, "openai doesn't accept seed for gpt-5", "no warning about the unsupported seed:\n%s", out)
	s := local(app).Engine().ModelSettings("gpt-5")
	require.NotNil(t, s.Temperature, "engine: %+v", s)
	require.Equal(t, 0.3, *s.Temperature, "engine: %+v", s)
	require.Equal(t, 2048, *s.MaxTokens, "engine: %+v", s)
	s, ok := savedConfig(t).ModelSettings["gpt-5"]
	require.True(t, ok, "saved = %+v", savedConfig(t).ModelSettings)
	require.NotNil(t, s.Seed, "saved = %+v", savedConfig(t).ModelSettings)
	require.Equal(t, 7, *s.Seed, "saved = %+v", savedConfig(t).ModelSettings)

	// An invalid value changes nothing, including the valid pair before it.
	assert.Contains(t, run("/model_settings gpt-5 top_p=0.5 temperature=9"), "Nothing changed", "invalid")
	s = local(app).Engine().ModelSettings("gpt-5")
	require.Nil(t, s.TopP, "partly applied: %+v", s)
	require.Nil(t, savedConfig(t).ModelSettings["gpt-5"].TopP, "partly applied: %+v", s)
	assert.Contains(t, run("/model_settings gpt-5 temperature"), "Expected key=value", "bad pair")

	// Clear one key, then show where the others come from.
	run("/model_settings gpt-5 temperature=")
	out = run("/model_settings gpt-5")
	assert.Contains(t, out, "(global: 0.2)", "show:\n%s", out)
	assert.Contains(t, out, "2048", "show:\n%s", out)
	assert.Contains(t, out, "(provider default)", "show:\n%s", out)
	list := run("/model_settings")
	assert.Contains(t, list, "gpt-5")
	assert.Contains(t, list, "max_tokens=2048 seed=7")

	out = run("/model_settings gpt-5 reset")
	assert.Contains(t, out, "gpt-5 now uses the global settings", "reset:\n%s", out)
	assert.True(t, local(app).Engine().ModelSettings("gpt-5").IsZero(), "reset:\n%s", out)
	s = savedConfig(t).ModelSettings["gpt-5"]
	require.True(t, s.IsZero(), "reset not saved: %+v", s)
}

// A hand-written "provider/model" table is edited in place, not duplicated.
func TestModelSettingsSavesUnderTheExistingKey(t *testing.T) {
	app, _ := newCommandApp(t, "")
	local(app).Config().ModelSettings = map[string]config.ModelSettings{"openai/gpt-5": {}}
	captureStdout(t, func() { HandleCommand(context.Background(), "/model_settings gpt-5 temperature=0.4", app) })
	saved := savedConfig(t).ModelSettings
	s, ok := saved["openai/gpt-5"]
	require.True(t, ok, "saved = %+v", saved)
	require.NotNil(t, s.Temperature, "saved = %+v", saved)
	require.Len(t, saved, 1, "saved = %+v", saved)
}
