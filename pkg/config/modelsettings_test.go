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
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelSettingsSetValidates(t *testing.T) {
	var s ModelSettings
	good := [][2]string{{"temperature", "0"}, {"temperature", "2"}, {"top_p", "1"}, {"top_p", "0.9"}, {"max_tokens", "1"}, {"seed", "-3"}, {"seed", "2147483647"}}
	for _, c := range good {
		assert.NoError(t, s.Set(c[0], c[1]), "%s=%s", c[0], c[1])
	}
	bad := [][2]string{{"temperature", "2.1"}, {"temperature", "-1"}, {"temperature", "NaN"}, {"top_p", "0"}, {"top_p", "1.5"},
		{"max_tokens", "0"}, {"max_tokens", "1.5"}, {"max_tokens", "99999999999"}, {"seed", "x"}, {"top_k", "5"}}
	for _, c := range bad {
		before := s
		assert.Error(t, s.Set(c[0], c[1]), "%s=%s accepted", c[0], c[1])
		assert.Equal(t, before, s, "%s=%s changed the settings on error", c[0], c[1])
	}
	err := s.Set("seed", "")
	require.NoError(t, err, "clearing: %v %v", err, s.Seed)
	require.Nil(t, s.Seed, "clearing: %v %v", err, s.Seed)
	v, ok := s.Get("temperature")
	require.True(t, ok, "a whole temperature must stay a TOML float: %q", v)
	require.Equal(t, "2.0", v, "a whole temperature must stay a TOML float: %q", v)
}

func TestSaveModelSettingsWritesAndRemoves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.toml")
	orig := "# mine\n[blitz]\ntemperature = 0.2  # global\n\n[agent_models]\nqa = \"gpt-5\"\n"
	os.WriteFile(path, []byte(orig), 0o600)

	var gpt, local ModelSettings
	gpt.Set("temperature", "1")
	gpt.Set("top_p", "0.00001")
	gpt.Set("max_tokens", "4096")
	local.Set("seed", "7")
	for model, s := range map[string]ModelSettings{"gpt-4.1": gpt, "qwen2.5-coder:7b": local} {
		_, err := SaveModelSettings(dir, model, s)
		require.NoError(t, err, "%s", model)
	}
	gpt.Set("max_tokens", "") // remove one key
	_, err := SaveModelSettings(dir, "gpt-4.1", gpt)
	require.NoError(t, err)

	b, _ := os.ReadFile(path)
	var cfg Config
	_, err = toml.Decode(string(b), &cfg)
	require.NoError(t, err, "invalid TOML:\n%s\n%v", b, err)
	g := cfg.ModelSettings["gpt-4.1"]
	require.NotNil(t, g.Temperature, "gpt-4.1 = %+v\n%s", g, b)
	require.Equal(t, float64(1), *g.Temperature, "gpt-4.1 = %+v\n%s", g, b)
	require.NotNil(t, g.TopP, "gpt-4.1 = %+v\n%s", g, b)
	require.Equal(t, 0.00001, *g.TopP, "gpt-4.1 = %+v\n%s", g, b)
	require.Nil(t, g.MaxTokens, "gpt-4.1 = %+v\n%s", g, b)
	l := cfg.ModelSettings["qwen2.5-coder:7b"]
	require.NotNil(t, l.Seed, "qwen = %+v\n%s", l, b)
	require.Equal(t, 7, *l.Seed, "qwen = %+v\n%s", l, b)
	for _, keep := range []string{"# mine", "# global", `qa = "gpt-5"`} {
		require.Contains(t, string(b), keep, "lost %q:\n%s", keep, b)
	}

	// Clearing everything removes the table.
	_, err = SaveModelSettings(dir, "qwen2.5-coder:7b", ModelSettings{})
	require.NoError(t, err)
	b, _ = os.ReadFile(path)
	require.NotContains(t, string(b), "qwen", "empty table left behind:\n%s", b)
	require.NotContains(t, string(b), "\n\n\n", "empty table left behind:\n%s", b)
	info, _ := os.Stat(path)
	require.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "mode %v", info.Mode().Perm())
}

// Settings saved by SaveModelSettings load through modenv, as at startup.
func TestLoadReadsModelSettings(t *testing.T) {
	home := isolateConfigEnv(t)
	dir := filepath.Join(home, ".blitz")
	var s ModelSettings
	s.Set("temperature", "0.7")
	s.Set("seed", "11")
	_, err := SaveModelSettings(dir, "claude-haiku-4-5", s)
	require.NoError(t, err)
	t.Chdir(t.TempDir())
	cfg, err := Load("")
	require.NoError(t, err)
	got := cfg.ModelSettings["claude-haiku-4-5"]
	require.NotNil(t, got.Temperature, "loaded %+v", got)
	require.Equal(t, 0.7, *got.Temperature, "loaded %+v", got)
	require.NotNil(t, got.Seed, "loaded %+v", got)
	require.Equal(t, 11, *got.Seed, "loaded %+v", got)
	require.Nil(t, got.MaxTokens, "loaded %+v", got)
}

func TestReasoningSettings(t *testing.T) {
	var s ModelSettings
	for _, c := range [][2]string{{"HIGH", "high"}, {`"low"`, "low"}, {"xhigh", "max"}, {" minimal ", "minimal"}} {
		in, want := c[0], c[1]
		err := s.Set("reasoning_effort", in)
		assert.NoError(t, err, "reasoning_effort=%s: %v %v", in, err, s.ReasoningEffort)
		assert.Equal(t, want, *s.ReasoningEffort, "reasoning_effort=%s: %v %v", in, err, s.ReasoningEffort)
	}
	err := s.Set("reasoning_effort", "extreme")
	assert.Error(t, err, "a bad effort was accepted or changed the setting")
	assert.Equal(t, "minimal", *s.ReasoningEffort, "a bad effort was accepted or changed the setting: %v", err)
	assert.Error(t, s.Set("thinking_budget", "-1"), "a negative thinking budget was accepted")
	err = s.Set("thinking_budget", "0")
	assert.NoError(t, err, "thinking_budget=0 (off)")
	assert.Equal(t, 0, *s.ThinkingBudget, "thinking_budget=0 (off): %v", err)
	v, ok := s.Get("reasoning_effort")
	assert.True(t, ok, "effort must be a TOML string: %q", v)
	assert.Equal(t, `"minimal"`, v, "effort must be a TOML string: %q", v)
	err = s.Set("reasoning_effort", "")
	assert.NoError(t, err, "clearing: %v %v", err, s.ReasoningEffort)
	assert.Nil(t, s.ReasoningEffort, "clearing: %v %v", err, s.ReasoningEffort)
	assert.False(t, s.IsZero(), "clearing: %v %v", err, s.ReasoningEffort)

	dir := t.TempDir()
	s.Set("reasoning_effort", "high")
	_, err = SaveModelSettings(dir, "claude-opus-5", s)
	require.NoError(t, err)
	b, _ := os.ReadFile(filepath.Join(dir, ".env.toml"))
	var got struct {
		ModelSettings map[string]ModelSettings `toml:"model_settings"`
	}
	_, err = toml.Decode(string(b), &got)
	require.NoError(t, err)
	m := got.ModelSettings["claude-opus-5"]
	require.NotNil(t, m.ReasoningEffort, "saved:\n%s", b)
	require.Equal(t, "high", *m.ReasoningEffort, "saved:\n%s", b)
	require.NotNil(t, m.ThinkingBudget, "saved:\n%s", b)
	require.Equal(t, 0, *m.ThinkingBudget, "saved:\n%s", b)
}
