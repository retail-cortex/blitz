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
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/secrets"
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
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, ".blitz", ".env.toml") {
		t.Errorf("wrote %s", path)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "AIza") || !strings.Contains(string(data), `api_key = "keychain:global/llm.gemini.api_key"`) {
		t.Errorf("file:\n%s", data)
	}
	if v, _ := store.Get("global/llm.gemini.api_key"); v != "AIza-global" {
		t.Errorf("stored %q", v)
	}
	cfg, err := Load("")
	if err != nil || cfg.LLM.Gemini.APIKey != "AIza-global" {
		t.Errorf("loaded %q, %v", cfg.LLM.Gemini.APIKey, err)
	}
	if p := source(t, "", "gemini"); p.KeySource != KeyKeychain || p.KeyMissing {
		t.Errorf("gemini: %+v", p)
	}
	if p := source(t, "", "openai"); p.KeySource != KeyNone {
		t.Errorf("openai: %+v", p)
	}
	t.Setenv("OPENAI_API_KEY", "sk-env")
	if p := source(t, "", "openai"); p.KeySource != KeyEnvironment {
		t.Errorf("openai from the environment: %+v", p)
	}
}

// A workspace's own key wins there, lives in the user's directory (not the
// workspace), and removing it falls back to the global one.
func TestWorkspaceKeys(t *testing.T) {
	home, store := keysEnv(t)
	ws := t.TempDir()
	if _, err := SetAPIKey("", "", "anthropic", "sk-ant-global"); err != nil {
		t.Fatal(err)
	}
	path, err := SetAPIKey("", ws, "anthropic", "sk-ant-project")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, filepath.Join(home, ".blitz", "workspaces")+string(filepath.Separator)) || !strings.Contains(filepath.Base(filepath.Dir(path)), filepath.Base(ws)+"-") {
		t.Errorf("workspace settings at %s", path)
	}
	if _, err := os.Stat(filepath.Join(ws, ".blitz")); !os.IsNotExist(err) {
		t.Error("wrote into the workspace")
	}
	cfg, err := LoadWorkspace("", ws)
	if err != nil || cfg.LLM.Anthropic.APIKey != "sk-ant-project" {
		t.Errorf("workspace: %q %v", cfg.LLM.Anthropic.APIKey, err)
	}
	if cfg, _ := Load(""); cfg.LLM.Anthropic.APIKey != "sk-ant-global" {
		t.Errorf("global: %q", cfg.LLM.Anthropic.APIKey)
	}
	if other, _ := LoadWorkspace("", t.TempDir()); other.LLM.Anthropic.APIKey != "sk-ant-global" {
		t.Errorf("another workspace: %q", other.LLM.Anthropic.APIKey)
	}
	if p := source(t, ws, "anthropic"); p.KeySource != KeyKeychain {
		t.Errorf("workspace anthropic: %+v", p)
	}

	if _, err := RemoveAPIKey("", ws, "anthropic"); err != nil {
		t.Fatal(err)
	}
	if p := source(t, ws, "anthropic"); p.KeySource != KeyInherited {
		t.Errorf("after removing: %+v", p)
	}
	if cfg, _ := LoadWorkspace("", ws); cfg.LLM.Anthropic.APIKey != "sk-ant-global" {
		t.Errorf("after removing: %q", cfg.LLM.Anthropic.APIKey)
	}
	if name := "workspace/" + filepath.Base(filepath.Dir(path)) + "/llm.anthropic.api_key"; mustMissing(store, name) != nil {
		t.Errorf("the workspace's secret is still stored")
	}
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
	if _, err := SetAPIKey("", "", "openai", "sk-1"); err != nil {
		t.Fatal(err)
	}
	store.Delete("global/llm.openai.api_key")
	if p := source(t, "", "openai"); p.KeySource != KeyKeychain || !p.KeyMissing {
		t.Errorf("openai: %+v", p)
	}
	if cfg, _ := Load(""); cfg.LLM.OpenAI.APIKey != "" {
		t.Errorf("loaded %q", cfg.LLM.OpenAI.APIKey)
	}
}

func TestSetValue(t *testing.T) {
	keysEnv(t)
	ws := t.TempDir()
	if _, err := SetValue("", "", "llm.provider", "anthropic"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetValue("", ws, "blitz.default_model", "claude-sonnet-5"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := LoadWorkspace("", ws)
	if cfg.LLM.Provider != "anthropic" || cfg.Blitz.DefaultModel != "claude-sonnet-5" {
		t.Errorf("provider %q model %q", cfg.LLM.Provider, cfg.Blitz.DefaultModel)
	}
	if _, err := SetValue("", ws, "blitz.default_model", ""); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := LoadWorkspace("", ws); cfg.Blitz.DefaultModel != "" {
		t.Errorf("removed default model: %q", cfg.Blitz.DefaultModel)
	}
	for _, bad := range [][2]string{{"blitz.auto_approve", "true"}, {"llm.provider", "nope"}} {
		if _, err := SetValue("", "", bad[0], bad[1]); err == nil {
			t.Errorf("SetValue(%s=%s) accepted", bad[0], bad[1])
		}
	}
	if _, err := SetAPIKey("", "", "ollama", "x"); err == nil {
		t.Error("a key for a provider that takes none")
	}
}

func TestWriteSettingsFile(t *testing.T) {
	keysEnv(t)
	path, warnings, err := WriteSettingsFile("", "", "# mine\n[llm]\nprovider = \"openai\"\n[llm.openai]\napi_key = \"sk-plain\"\n[llm.typo]\nx = 1\n")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "unknown setting llm.typo") || !strings.Contains(joined, "openai API key is in the file as plain text") {
		t.Errorf("warnings: %q", warnings)
	}
	if _, text, _ := ReadSettingsFile("", ""); !strings.HasPrefix(text, "# mine\n") {
		t.Errorf("read back %q", text)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	if _, _, err := WriteSettingsFile("", "", "[llm\nbroken"); err == nil {
		t.Error("saved invalid TOML")
	}
	if _, _, err := WriteSettingsFile("", "", "[llm]\nprovider = 3\n"); err == nil {
		t.Error("saved a setting of the wrong type")
	}
	if _, text, _ := ReadSettingsFile("", ""); !strings.HasPrefix(text, "# mine\n") {
		t.Error("a refused write changed the file")
	}
}

func TestSecureAPIKey(t *testing.T) {
	_, store := keysEnv(t)
	dir := ConfigDir("")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.toml"), []byte("[llm.openai]\napi_key = \"sk-plain\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SecureAPIKey("", "", "openai"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".env.toml"))
	if strings.Contains(string(data), "sk-plain") {
		t.Errorf("the key is still in the file:\n%s", data)
	}
	if v, _ := store.Get("global/llm.openai.api_key"); v != "sk-plain" {
		t.Errorf("stored %q", v)
	}
	if _, err := SecureAPIKey("", "", "openai"); err == nil {
		t.Error("moved a reference")
	}
}
