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
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keysEnv isolates the settings and gives them an in-memory secret store.
func keysEnv(t *testing.T) (home string, store *secrets.Memory) {
	t.Helper()
	home = isolateConfigEnv(t)
	for _, v := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(v, "")
	}
	store = &secrets.Memory{}
	secrets.SetDefault(store)
	return home, store
}

func source(t *testing.T, workspace, provider string) ProviderInfo {
	t.Helper()
	info, err := Describe("", workspace)
	require.NoError(t, err)
	for _, p := range info.Providers {
		if p.Name == provider {
			return p
		}
	}
	t.Fatalf("no provider %s", provider)
	return ProviderInfo{}
}

// A key set globally goes to the store; the file only refers to it, and
// Load resolves the reference.
func TestGlobalAPIKeyInTheStore(t *testing.T) {
	home, store := keysEnv(t)
	path, err := SetAPIKey("", "", "gemini", "  AIza-global  ")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".blitz", ".env.toml"), path, "wrote %s", path)
	data, _ := os.ReadFile(path)
	assert.NotContains(t, string(data), "AIza", "file:\n%s", data)
	assert.Contains(t, string(data), `api_key = "keychain:global/llm.gemini.api_key"`, "file:\n%s", data)
	v, _ := store.Get("global/llm.gemini.api_key")
	assert.Equal(t, "AIza-global", v, "stored %q", v)
	cfg, err := Load("")
	assert.NoError(t, err, "loaded %q,", cfg.LLM.Gemini.APIKey)
	assert.Equal(t, "AIza-global", cfg.LLM.Gemini.APIKey, "loaded %q, %v", cfg.LLM.Gemini.APIKey, err)
	p := source(t, "", "gemini")
	assert.Equal(t, KeyKeychain, p.KeySource, "gemini: %+v", p)
	assert.False(t, p.KeyMissing, "gemini: %+v", p)
	p = source(t, "", "openai")
	assert.Equal(t, KeyNone, p.KeySource, "openai: %+v", p)
	t.Setenv("OPENAI_API_KEY", "sk-env")
	p = source(t, "", "openai")
	assert.Equal(t, KeyEnvironment, p.KeySource, "openai from the environment: %+v", p)
}

// A workspace's own key wins there, lives in the user's directory (not the
// workspace), and removing it falls back to the global one.
func TestWorkspaceKeys(t *testing.T) {
	home, store := keysEnv(t)
	ws := t.TempDir()
	_, err := SetAPIKey("", "", "anthropic", "sk-ant-global")
	require.NoError(t, err)
	path, err := SetAPIKey("", ws, "anthropic", "sk-ant-project")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(path, filepath.Join(home, ".blitz", "workspaces")+string(filepath.Separator)), "workspace settings at %s", path)
	assert.Contains(t, filepath.Base(filepath.Dir(path)), filepath.Base(ws)+"-", "workspace settings at %s", path)
	assert.NoDirExists(t, filepath.Join(ws, ".blitz"), "nothing is written into the workspace")
	cfg, err := LoadWorkspace("", ws)
	assert.NoError(t, err, "workspace: %q", cfg.LLM.Anthropic.APIKey)
	assert.Equal(t, "sk-ant-project", cfg.LLM.Anthropic.APIKey, "workspace: %q %v", cfg.LLM.Anthropic.APIKey, err)
	cfg, _ = Load("")
	assert.Equal(t, "sk-ant-global", cfg.LLM.Anthropic.APIKey, "global: %q", cfg.LLM.Anthropic.APIKey)
	other, _ := LoadWorkspace("", t.TempDir())
	assert.Equal(t, "sk-ant-global", other.LLM.Anthropic.APIKey, "another workspace: %q", other.LLM.Anthropic.APIKey)
	p := source(t, ws, "anthropic")
	assert.Equal(t, KeyKeychain, p.KeySource, "workspace anthropic: %+v", p)

	_, err = RemoveAPIKey("", ws, "anthropic")
	require.NoError(t, err)
	p = source(t, ws, "anthropic")
	assert.Equal(t, KeyInherited, p.KeySource, "after removing: %+v", p)
	cfg, _ = LoadWorkspace("", ws)
	assert.Equal(t, "sk-ant-global", cfg.LLM.Anthropic.APIKey, "after removing: %q", cfg.LLM.Anthropic.APIKey)
	name := "workspace/" + filepath.Base(filepath.Dir(path)) + "/llm.anthropic.api_key"
	assert.NoError(t, mustMissing(store, name), "the workspace's secret is still stored")
}

func mustMissing(s secrets.Store, name string) error {
	if _, err := s.Get(name); err == nil {
		return os.ErrExist
	}
	return nil
}

// A reference to a secret that's gone is reported, and loads as no key.
func TestMissingSecret(t *testing.T) {
	_, store := keysEnv(t)
	_, err := SetAPIKey("", "", "openai", "sk-1")
	require.NoError(t, err)
	store.Delete("global/llm.openai.api_key")
	p := source(t, "", "openai")
	assert.Equal(t, KeyKeychain, p.KeySource, "openai: %+v", p)
	assert.True(t, p.KeyMissing, "openai: %+v", p)
	cfg, _ := Load("")
	assert.Equal(t, "", cfg.LLM.OpenAI.APIKey, "loaded %q", cfg.LLM.OpenAI.APIKey)
}

func TestSetValue(t *testing.T) {
	keysEnv(t)
	ws := t.TempDir()
	_, err := SetValue("", "", "llm.provider", "anthropic")
	require.NoError(t, err)
	_, err = SetValue("", ws, "blitz.default_model", "claude-sonnet-5")
	require.NoError(t, err)
	cfg, _ := LoadWorkspace("", ws)
	assert.Equal(t, "anthropic", cfg.LLM.Provider, "provider %q model %q", cfg.LLM.Provider, cfg.Blitz.DefaultModel)
	assert.Equal(t, "claude-sonnet-5", cfg.Blitz.DefaultModel, "provider %q model %q", cfg.LLM.Provider, cfg.Blitz.DefaultModel)
	_, err = SetValue("", ws, "blitz.default_model", "")
	require.NoError(t, err)
	cfg, _ = LoadWorkspace("", ws)
	assert.Equal(t, "", cfg.Blitz.DefaultModel, "removed default model: %q", cfg.Blitz.DefaultModel)
	for _, bad := range [][2]string{{"blitz.auto_approve", "true"}, {"llm.provider", "nope"}} {
		_, err := SetValue("", "", bad[0], bad[1])
		assert.Error(t, err, "SetValue(%s=%s) accepted", bad[0], bad[1])
	}
	_, err = SetAPIKey("", "", "ollama", "x")
	assert.Error(t, err, "a key for a provider that takes none")
}

func TestWriteSettingsFile(t *testing.T) {
	keysEnv(t)
	path, warnings, err := WriteSettingsFile("", "", "# mine\n[llm]\nprovider = \"openai\"\n[llm.openai]\napi_key = \"sk-plain\"\n[llm.typo]\nx = 1\n")
	require.NoError(t, err)
	joined := strings.Join(warnings, "\n")
	assert.Contains(t, joined, "unknown setting llm.typo", "warnings: %q", warnings)
	assert.Contains(t, joined, "openai API key is in the file as plain text", "warnings: %q", warnings)
	_, text, _ := ReadSettingsFile("", "")
	assert.True(t, strings.HasPrefix(text, "# mine\n"), "read back %q", text)
	info, _ := os.Stat(path)
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "mode %v", info.Mode().Perm())
	_, _, err = WriteSettingsFile("", "", "[llm\nbroken")
	assert.Error(t, err, "saved invalid TOML")
	_, _, err = WriteSettingsFile("", "", "[llm]\nprovider = 3\n")
	assert.Error(t, err, "saved a setting of the wrong type")
	_, text, _ = ReadSettingsFile("", "")
	assert.True(t, strings.HasPrefix(text, "# mine\n"), "a refused write changed the file")
}

func TestSecureAPIKey(t *testing.T) {
	_, store := keysEnv(t)
	dir := ConfigDir("")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), []byte("[llm.openai]\napi_key = \"sk-plain\"\n"), 0o600))
	_, err := SecureAPIKey("", "", "openai")
	require.NoError(t, err)
	data, _ := os.ReadFile(filepath.Join(dir, ".env.toml"))
	assert.NotContains(t, string(data), "sk-plain", "the key is still in the file:\n%s", data)
	v, _ := store.Get("global/llm.openai.api_key")
	assert.Equal(t, "sk-plain", v, "stored %q", v)
	_, err = SecureAPIKey("", "", "openai")
	assert.Error(t, err, "moved a reference")
}
