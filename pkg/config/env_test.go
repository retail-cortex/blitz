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

package config

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Environment variables fill in what the files leave empty, and set the
// provider, model, agent, agency, log level and telemetry outright.
func TestApplyEnvOverrides(t *testing.T) {
	for _, k := range []string{"LLM_PROVIDER", "GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_MODEL",
		"ANTHROPIC_API_KEY", "BLITZ_MODEL", "BLITZ_AGENT", "BLITZ_AGENCY", "BLITZ_LOG_LEVEL", "BLITZ_TELEMETRY"} {
		t.Setenv(k, "")
	}
	t.Setenv("LLM_PROVIDER", "OpenAI")
	t.Setenv("GOOGLE_API_KEY", "g-key")
	t.Setenv("OPENAI_API_KEY", "o-key")
	t.Setenv("OPENAI_BASE_URL", "http://local/v1")
	t.Setenv("OPENAI_MODEL", "gpt-x")
	t.Setenv("ANTHROPIC_API_KEY", "a-key")
	t.Setenv("BLITZ_MODEL", "m")
	t.Setenv("BLITZ_AGENT", "qa")
	t.Setenv("BLITZ_AGENCY", "LOW")
	t.Setenv("BLITZ_LOG_LEVEL", "DEBUG")
	t.Setenv("BLITZ_TELEMETRY", "yes")
	cfg := DefaultConfig()
	applyEnvOverrides(cfg)
	assert.Equal(t, "openai", cfg.LLM.Provider)
	assert.Equal(t, "g-key", cfg.LLM.Gemini.APIKey)
	assert.Equal(t, "o-key", cfg.LLM.OpenAI.APIKey)
	assert.Equal(t, "http://local/v1", cfg.LLM.OpenAI.BaseURL)
	assert.Equal(t, "gpt-x", cfg.LLM.OpenAI.Model)
	assert.Equal(t, "a-key", cfg.LLM.Anthropic.APIKey)
	assert.Equal(t, "m", cfg.Blitz.DefaultModel)
	assert.Equal(t, "qa", cfg.Blitz.DefaultAgent)
	assert.Equal(t, "low", cfg.Blitz.AgencyLevel)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.True(t, cfg.Telemetry.Enabled)

	t.Setenv("GEMINI_API_KEY", "gem-key")
	t.Setenv("BLITZ_TELEMETRY", "off")
	cfg = DefaultConfig()
	cfg.LLM.OpenAI.APIKey = "from-file"
	cfg.Telemetry.Enabled = true
	applyEnvOverrides(cfg)
	assert.Equal(t, "gem-key", cfg.LLM.Gemini.APIKey, "GEMINI_API_KEY before GOOGLE_API_KEY")
	assert.Equal(t, "from-file", cfg.LLM.OpenAI.APIKey, "a key in the files wins")
	assert.False(t, cfg.Telemetry.Enabled)
}

// ModelName is the default model when set, else the active provider's.
func TestModelName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LLM.Gemini.Model, cfg.LLM.OpenAI.Model, cfg.LLM.Anthropic.Model = "gem", "oai", "claude"
	cfg.LLM.Bedrock.Model, cfg.LLM.Azure.Model = "bed", "az"
	for provider, want := range map[string]string{
		"gemini": "gem", "": "gem", "openai": "oai", "Ollama": "oai", "anthropic": "claude",
		"vertex-anthropic": "claude", "bedrock": "bed", "azure": "az",
	} {
		t.Run(provider, func(t *testing.T) {
			cfg.LLM.Provider = provider
			assert.Equal(t, want, cfg.ModelName())
		})
	}
	cfg.Blitz.DefaultModel = "pinned"
	assert.Equal(t, "pinned", cfg.ModelName())
}

// Claude signs in with the configured profile, else ANTHROPIC_PROFILE,
// else ant's active one, else "default".
func TestOAuthProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTHROPIC_CONFIG_DIR", dir)
	t.Setenv("ANTHROPIC_PROFILE", "")
	gotDir, name := AnthropicConfig{Profile: "work"}.OAuthProfile()
	assert.Equal(t, dir, gotDir)
	assert.Equal(t, "work", name)
	_, name = AnthropicConfig{}.OAuthProfile()
	assert.Equal(t, "default", name)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "active_config"), []byte("team\n"), 0o600))
	_, name = AnthropicConfig{}.OAuthProfile()
	assert.Equal(t, "team", name)
	t.Setenv("ANTHROPIC_PROFILE", "env")
	_, name = AnthropicConfig{}.OAuthProfile()
	assert.Equal(t, "env", name)

	cfg := DefaultConfig()
	assert.False(t, cfg.UsesADC())
	cfg.LLM.Anthropic.Auth = AuthADC
	assert.True(t, cfg.LLM.Anthropic.UsesADC())
	assert.True(t, cfg.UsesADC())
}

// Without a home directory, there's no config directory: Load uses the
// defaults and the environment, and "~" stays as it is.
func TestNoHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("MODENV_PREFIX", "")
	t.Setenv("BLITZ_AGENT", "qa")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("CLOUDSDK_CONFIG", "")
	assert.Empty(t, ConfigDir(""))
	assert.Equal(t, "~/x", ExpandHome("~/x"))
	assert.Empty(t, ADCFile())
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "qa", cfg.Blitz.DefaultAgent)
}

// A workspace's auto-mode reviewer model and environment replace the
// user's.
func TestPermissionsMergeAuto(t *testing.T) {
	user := PermissionsConfig{Auto: AutoReviewConfig{Model: "u", Environment: "ue"}}
	got := user.Merge(PermissionsConfig{Auto: AutoReviewConfig{Model: "w", Environment: "we"}})
	assert.Equal(t, AutoReviewConfig{Model: "w", Environment: "we"}, got.Auto)
	assert.Equal(t, user.Auto, user.Merge(PermissionsConfig{}).Auto)
}

// sandbox.shell, unset, is "required" on Linux and "auto" elsewhere; the
// settings and then BLITZ_SANDBOX_SHELL say otherwise.
func TestShellModeDefault(t *testing.T) {
	isolateConfigEnv(t)
	want := "auto"
	if goruntime.GOOS == "linux" {
		want = "required"
	}
	assert.Equal(t, want, DefaultShellMode())
	assert.Equal(t, "auto", DefaultConfig().Sandbox.Shell, "the code's own default (tests, embedders) stays auto")
	tests := []struct {
		name, settings, env, want string
	}{
		{name: "unset", want: want},
		{name: "set in the settings", settings: "[sandbox]\nshell = \"auto\"\n", want: "auto"},
		{name: "the variable over the settings", settings: "[sandbox]\nshell = \"auto\"\n", env: "off", want: "off"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BLITZ_SANDBOX_SHELL", tt.env)
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), []byte(tt.settings), 0o600))
			cfg, err := Load(dir)
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Sandbox.Shell)
		})
	}
}
