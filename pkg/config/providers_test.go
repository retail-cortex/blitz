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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/rrmcguinness/modenv/pkg/modenv"
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

// The form's provider, model and key are saved together, or not at all.
func TestSetProvider(t *testing.T) {
	_, store := keysEnv(t)
	ws := t.TempDir()

	// All three in a workspace: the key goes to the store, the rest to the file.
	path, err := SetProvider("", ws, ProviderChoice{Provider: "anthropic", Model: " claude-sonnet-5 ", Key: " sk-ant-project "})
	require.NoError(t, err)
	data, _ := os.ReadFile(path)
	assert.NotContains(t, string(data), "sk-ant-project", "file:\n%s", data)
	cfg, err := LoadWorkspace("", ws)
	require.NoError(t, err)
	assert.Equal(t, "anthropic", cfg.LLM.Provider)
	assert.Equal(t, "claude-sonnet-5", cfg.Blitz.DefaultModel)
	assert.Equal(t, "sk-ant-project", cfg.LLM.Anthropic.APIKey)

	// No key keeps the one stored; "" removes the provider and model.
	_, err = SetProvider("", ws, ProviderChoice{Provider: "anthropic", Model: "claude-opus-5-5"})
	require.NoError(t, err)
	cfg, _ = LoadWorkspace("", ws)
	assert.Equal(t, "claude-opus-5-5", cfg.Blitz.DefaultModel)
	assert.Equal(t, "sk-ant-project", cfg.LLM.Anthropic.APIKey, "the key was kept")
	_, err = SetProvider("", ws, ProviderChoice{})
	require.NoError(t, err)
	info, _ := Describe("", ws)
	assert.Equal(t, "", info.Provider, "provider removed")
	assert.Equal(t, "", info.DefaultModel, "model removed")
	assert.Equal(t, KeyKeychain, source(t, ws, "anthropic").KeySource, "removing the provider keeps its key")

	// Refused before anything is written.
	for _, tc := range []struct {
		name   string
		choice ProviderChoice
	}{
		{"an unknown provider", ProviderChoice{Provider: "nope", Model: "m"}},
		{"a key for a provider that takes none", ProviderChoice{Provider: "ollama", Model: "m", Key: "x"}},
		{"a key without a provider", ProviderChoice{Model: "m", Key: "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SetProvider("", "", tc.choice)
			assert.Error(t, err)
			info, _ := Describe("", "")
			assert.Equal(t, "", info.DefaultModel, "the model was written")
			_, err = store.Get("global/llm.ollama.api_key")
			assert.ErrorIs(t, err, secrets.ErrNotFound, "a key was stored")
		})
	}
}

// How a provider signs in: an API key, Google Cloud's ADC or an ant profile.
func TestSetAuth(t *testing.T) {
	keysEnv(t)
	ws := t.TempDir()

	for _, tc := range []struct {
		name     string
		provider string
		auth     ProviderAuth
		want     ProviderInfo
		err      string
	}{
		{name: "Gemini with ADC", provider: "gemini", auth: ProviderAuth{Method: AuthADC, ProjectID: " my-project ", Location: "us-central1"},
			want: ProviderInfo{Auth: AuthADC, ProjectID: "my-project", Location: "us-central1"}},
		{name: "Gemini back to a key keeps the project", provider: "gemini", auth: ProviderAuth{Method: AuthAPIKey},
			want: ProviderInfo{ProjectID: "my-project", Location: "us-central1"}},
		{name: "Claude on Vertex AI", provider: "anthropic", auth: ProviderAuth{Method: AuthADC, ProjectID: "claude-p", Location: "us-east5"},
			want: ProviderInfo{Auth: AuthADC, ProjectID: "claude-p", Location: "us-east5"}},
		{name: "Claude with ant's active profile", provider: "anthropic", auth: ProviderAuth{Method: AuthOAuth}, want: ProviderInfo{Auth: AuthOAuth, ProjectID: "claude-p", Location: "us-east5"}},
		{name: "Claude with a named profile", provider: "anthropic", auth: ProviderAuth{Method: AuthOAuth, Profile: "work"}, want: ProviderInfo{Auth: AuthOAuth, Profile: "work", ProjectID: "claude-p", Location: "us-east5"}},
		{name: "OAuth isn't Gemini's", provider: "gemini", auth: ProviderAuth{Method: AuthOAuth}, err: "gemini signs in with api_key or adc"},
		{name: "no such method for Claude", provider: "anthropic", auth: ProviderAuth{Method: "password"}, err: "anthropic signs in with api_key or oauth or adc"},
		{name: "OpenAI takes a key only", provider: "openai", auth: ProviderAuth{Method: AuthOAuth}, err: "API key only"},
		{name: "a path isn't a profile", provider: "anthropic", auth: ProviderAuth{Method: AuthOAuth, Profile: "../x"}, err: "isn't a profile name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SetAuth("", ws, tc.provider, tc.auth)
			if tc.err != "" {
				assert.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			p := source(t, ws, tc.provider)
			assert.Equal(t, tc.want, ProviderInfo{Auth: p.Auth, ProjectID: p.ProjectID, Location: p.Location, Profile: p.Profile})
		})
	}

	// The loaded settings say so too.
	cfg, err := LoadWorkspace("", ws)
	require.NoError(t, err)
	assert.True(t, cfg.LLM.Anthropic.UsesOAuth())
	assert.Equal(t, "work", cfg.LLM.Anthropic.Profile)
	assert.False(t, cfg.LLM.Gemini.UsesADC())

	// SetValue checks auth the same way.
	_, err = SetValue("", ws, "llm.gemini.auth", "adc")
	assert.NoError(t, err)
	_, err = SetValue("", ws, "llm.gemini.auth", "oauth")
	assert.Error(t, err)
}

// The provider form's sign-in is part of its one change.
func TestSetProviderWithAuth(t *testing.T) {
	_, store := keysEnv(t)
	_, err := SetProvider("", "", ProviderChoice{Provider: "gemini", Model: "gemini-3.8-flash", Auth: &ProviderAuth{Method: AuthADC, ProjectID: "p"}})
	require.NoError(t, err)
	cfg, _ := Load("")
	assert.True(t, cfg.LLM.Gemini.UsesADC())
	assert.Equal(t, "p", cfg.LLM.Gemini.ProjectID)

	_, err = SetProvider("", "", ProviderChoice{Provider: "gemini", Model: "m2", Key: "AIza", Auth: &ProviderAuth{Method: AuthADC}})
	assert.ErrorContains(t, err, "an API key is for signing in with api_key")
	info, _ := Describe("", "")
	assert.Equal(t, "gemini-3.8-flash", info.DefaultModel, "a refused change was written")
	_, err = store.Get("global/llm.gemini.api_key")
	assert.ErrorIs(t, err, secrets.ErrNotFound, "a refused change stored its key")
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

// fakeRuleCheck stands in for the rule parser (pkg/engine/tools): rules
// must be kind(pattern), "bad" isn't a kind, and "Bash" is shell.
func fakeRuleCheck(t *testing.T) {
	t.Helper()
	old := ValidatePermissionRule
	t.Cleanup(func() { ValidatePermissionRule = old })
	ValidatePermissionRule = func(_, rule string) (string, error) {
		rule = strings.TrimSpace(rule)
		if !strings.HasSuffix(rule, ")") || strings.HasPrefix(rule, "bad(") {
			return "", errors.New("invalid permission rule " + rule)
		}
		return strings.Replace(rule, "Bash(", "shell(", 1), nil
	}
}

// A workspace's permission rules add to the global ones; its
// read_only_defaults, when set, wins.
func TestPermissionsMerge(t *testing.T) {
	keysEnv(t)
	fakeRuleCheck(t)
	ws := t.TempDir()
	_, _, err := AddPermissionRule("", "", "allow", "shell(make)")
	require.NoError(t, err)
	_, _, err = AddPermissionRule("", "", "deny", "shell(rm)")
	require.NoError(t, err)
	_, canonical, err := AddPermissionRule("", ws, "allow", "Bash(go test *)")
	require.NoError(t, err)
	assert.Equal(t, "shell(go test *)", canonical)
	off := false
	_, err = SetReadOnlyDefaults("", ws, &off)
	require.NoError(t, err)

	cfg, err := LoadWorkspace("", ws)
	require.NoError(t, err)
	assert.Equal(t, []string{"shell(make)", "shell(go test *)"}, cfg.Permissions.Allow)
	assert.Equal(t, []string{"shell(rm)"}, cfg.Permissions.Deny)
	assert.False(t, cfg.Permissions.DefaultsOn())
	global, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, []string{"shell(make)"}, global.Permissions.Allow, "the workspace's rules stay in the workspace")
	assert.True(t, global.Permissions.DefaultsOn())

	// Each scope's own file, for the forms.
	own, _, err := ScopePermissions("", ws)
	require.NoError(t, err)
	assert.Equal(t, []string{"shell(go test *)"}, own.Allow)
	assert.Empty(t, own.Deny)

	// Removing takes the rule as written or canonical, from that scope only.
	_, n, err := RemovePermissionRule("", ws, "Bash(go test *)")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	_, n, err = RemovePermissionRule("", ws, "shell(make)")
	require.NoError(t, err)
	assert.Equal(t, 0, n, "the global rule isn't the workspace's to remove")
	_, err = SetReadOnlyDefaults("", ws, nil)
	require.NoError(t, err)
	cfg, _ = LoadWorkspace("", ws)
	assert.Equal(t, []string{"shell(make)"}, cfg.Permissions.Allow)
	assert.True(t, cfg.Permissions.DefaultsOn(), "the global setting again")
}

// Rules are checked before anything is saved: added one by one, or in the
// settings file.
func TestPermissionRulesCheckedBeforeSaving(t *testing.T) {
	keysEnv(t)
	fakeRuleCheck(t)
	for _, tc := range []struct {
		name string
		save func() error
	}{
		{"one rule", func() error { _, _, err := AddPermissionRule("", "", "allow", "bad(x)"); return err }},
		{"an effect", func() error { _, _, err := AddPermissionRule("", "", "maybe", "shell(x)"); return err }},
		{"a list", func() error {
			_, err := SavePermissionRules(ConfigDir(""), "deny", []string{"shell(ok)", "bad(x)"})
			return err
		}},
		{"the settings file", func() error {
			_, _, err := WriteSettingsFile("", "", "[permissions]\nallow = [\"shell(ls)\", \"bad(x)\"]\n")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Error(t, tc.save())
			own, _, err := ScopePermissions("", "")
			require.NoError(t, err)
			assert.Empty(t, own.Allow, "something was saved")
			assert.Empty(t, own.Deny, "something was saved")
		})
	}
}

// brokenStore is a secret store that can't be used.
type brokenStore struct{}

func (brokenStore) Kind() string               { return "broken" }
func (brokenStore) Get(string) (string, error) { return "", errors.New("locked") }
func (brokenStore) Set(string, string) error   { return errors.New("locked") }
func (brokenStore) Delete(string) error        { return errors.New("locked") }

// writeSettings writes the global settings file.
func writeSettings(t *testing.T, text string) string {
	t.Helper()
	dir := ConfigDir("")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, ".env.toml")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	return path
}

// Keys: unknown providers and empty keys are refused, and the store's
// failures are reported.
func TestAPIKeyErrors(t *testing.T) {
	keysEnv(t)
	for name, f := range map[string]func() (string, error){
		"set":    func() (string, error) { return SetAPIKey("", "", "ollama", "k") },
		"secure": func() (string, error) { return SecureAPIKey("", "", "ollama") },
		"remove": func() (string, error) { return RemoveAPIKey("", "", "ollama") },
		"auth":   func() (string, error) { return SetAuth("", "", "ollama", ProviderAuth{}) },
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f()
			assert.ErrorContains(t, err, `"ollama"`)
		})
	}
	_, err := SetAPIKey("", "", "openai", "  ")
	assert.ErrorContains(t, err, "empty")

	writeSettings(t, "[llm.openai]\napi_key = \"xor:zz\"\n")
	_, err = SecureAPIKey("", "", "openai")
	assert.ErrorContains(t, err, "obfuscated")

	writeSettings(t, "[llm.openai]\napi_key = \"keychain:global/llm.openai.api_key\"\n")
	secrets.SetDefault(brokenStore{})
	_, err = SetAPIKey("", "", "openai", "k")
	assert.ErrorContains(t, err, "locked")
	_, err = RemoveAPIKey("", "", "openai")
	assert.ErrorContains(t, err, "locked")
	_, err = SetProvider("", "", ProviderChoice{Provider: "openai", Key: "k"})
	assert.ErrorContains(t, err, "locked")
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.LLM.OpenAI.APIKey, "a secret that can't be read leaves the key empty")

	_, err = SetProvider("", "", ProviderChoice{Provider: "gemini", Auth: &ProviderAuth{Method: "telepathy"}})
	assert.ErrorContains(t, err, "telepathy")
}

// A settings file that isn't TOML is reported by everything that reads it.
func TestBadSettingsFile(t *testing.T) {
	keysEnv(t)
	writeSettings(t, "not = = toml")
	_, err := Describe("", "")
	assert.Error(t, err)
	_, err = SecureAPIKey("", "", "openai")
	assert.Error(t, err)
	_, err = RemoveAPIKey("", "", "openai")
	assert.Error(t, err)
	writeSettings(t, "")

	ws := t.TempDir()
	dir := WorkspaceSettingsDir("", ws)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), []byte("[[x"), 0o600))
	_, err = LoadWorkspace("", ws)
	assert.ErrorContains(t, err, "the workspace's settings")

	require.NoError(t, os.Remove(filepath.Join(dir, ".env.toml")))
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".env.toml"), 0o700))
	_, _, err = ReadSettingsFile("", ws)
	assert.Error(t, err, "the settings file is a directory")
}

// Without a home there's no settings directory; an obfuscated key is
// decoded when loaded and reported as such.
func TestSettingsWithoutHomeAndObfuscated(t *testing.T) {
	keysEnv(t)
	writeSettings(t, "[llm.openai]\napi_key = \""+modenv.EncryptSecret("sk-xor")+"\"\n")
	assert.Equal(t, KeyObfuscated, source(t, "", "openai").KeySource)
	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, "sk-xor", cfg.LLM.OpenAI.APIKey)

	t.Setenv("HOME", "")
	t.Setenv("MODENV_PREFIX", "") // Load set it
	assert.Empty(t, WorkspaceSettingsDir("", t.TempDir()))
	_, err = Describe("", "")
	assert.ErrorContains(t, err, "no settings directory")
	_, _, err = ReadSettingsFile("", "")
	assert.ErrorContains(t, err, "no settings directory")
}
