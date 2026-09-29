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

func TestSaveAgentModelPinsAndUnpins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.toml")
	orig := "# my settings\n[llm]\nprovider = \"gemini\"  # keep me\n"
	os.WriteFile(path, []byte(orig), 0o600)

	steps := []struct{ agent, ref string }{
		{"qa", "anthropic/claude-haiku-4-5"},
		{"helios", "openai/gpt-5"},
		{"qa", "gemini-3.8-flash"}, // replace
		{"my agent", "ollama/qwen2.5-coder:7b"},
		{"helios", ""}, // unpin
	}
	for _, s := range steps {
		_, err := SaveAgentModel(dir, s.agent, s.ref)
		require.NoError(t, err, "%v", s)
	}
	b, _ := os.ReadFile(path)
	var got struct {
		LLM         map[string]any    `toml:"llm"`
		AgentModels map[string]string `toml:"agent_models"`
	}
	_, err := toml.Decode(string(b), &got)
	require.NoError(t, err, "invalid TOML:\n%s\n%v", b, err)
	want := map[string]string{"qa": "gemini-3.8-flash", "my agent": "ollama/qwen2.5-coder:7b"}
	require.Len(t, got.AgentModels, len(want), "agent_models = %v\n%s", got.AgentModels, b)
	require.Equal(t, want["qa"], got.AgentModels["qa"], "agent_models = %v\n%s", got.AgentModels, b)
	require.Equal(t, want["my agent"], got.AgentModels["my agent"], "agent_models = %v\n%s", got.AgentModels, b)
	require.Contains(t, string(b), "# my settings", "other settings or comments lost:\n%s", b)
	require.Contains(t, string(b), "# keep me", "other settings or comments lost:\n%s", b)
	require.Equal(t, "gemini", got.LLM["provider"], "other settings or comments lost:\n%s", b)
	info, _ := os.Stat(path)
	require.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "mode %v", info.Mode().Perm())
	_, err = SaveAgentModel(dir, "nobody", "")
	require.NoError(t, err, "unpinning an unpinned agent")
}

// A workspace's pins lay over the global ones agent by agent; unpinning
// one the global settings pin masks it with "" rather than deleting it.
func TestWorkspacePinsOverlayTheGlobalOnes(t *testing.T) {
	root, ws := t.TempDir(), t.TempDir()
	dir := WorkspaceSettingsDir(root, ws)
	for agent, ref := range map[string]string{"qa": "openai/gpt-5", "helios": "gemini-3.8-flash", "planning-agent": "anthropic/claude-opus-5"} {
		_, err := SaveAgentModel(root, agent, ref)
		require.NoError(t, err)
	}
	_, err := SaveAgentModel(dir, "qa", "anthropic/claude-haiku-4-5")
	require.NoError(t, err)
	_, err = UnpinAgentModel(dir, "helios", true)
	require.NoError(t, err)

	cfg, err := LoadWorkspace(root, ws)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"qa": "anthropic/claude-haiku-4-5", "helios": "", "planning-agent": "anthropic/claude-opus-5"}, cfg.AgentModels)
	global, err := Load(root)
	require.NoError(t, err)
	assert.Equal(t, "openai/gpt-5", global.AgentModels["qa"], "the global pin changed")

	// Without a global pin to mask, unpinning removes the workspace's key.
	path, err := UnpinAgentModel(dir, "qa", false)
	require.NoError(t, err)
	b, _ := os.ReadFile(path)
	assert.NotContains(t, string(b), "qa", "unpinned key left:\n%s", b)
}
