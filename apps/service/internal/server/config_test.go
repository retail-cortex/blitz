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
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/secrets"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigService(t *testing.T) {
	c, s := serve(t, nil, text("hi"))
	for _, v := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(v, "")
	}
	store := &secrets.Memory{}
	secrets.SetDefault(store)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	// A global key goes to the keychain; the description never shows it.
	_, err := cfg.SetApiKey(ctx, connect.NewRequest(&pb.SetApiKeyRequest{Provider: "anthropic", Key: "sk-ant-secret"}))
	require.NoError(t, err)
	desc, err := cfg.DescribeConfig(ctx, connect.NewRequest(&pb.DescribeConfigRequest{}))
	require.NoError(t, err)
	assert.NotContains(t, desc.Msg.String(), "sk-ant-secret", "the description shows the key")
	for _, p := range desc.Msg.Providers {
		assert.False(t, p.Name == "anthropic" && p.KeySource != pb.KeySource_KEY_SOURCE_KEYCHAIN, "anthropic: %v", p)
	}
	file, _ := cfg.GetConfigFile(ctx, connect.NewRequest(&pb.GetConfigFileRequest{}))
	assert.Contains(t, file.Msg.Text, `"keychain:global/llm.anthropic.api_key"`, "file:\n%s", file.Msg.Text)

	// A workspace key while the workspace is open: it reloads.
	dir := t.TempDir()
	_, openErr := c.workspaces.ListAgents(ctx, connect.NewRequest(&pb.ListAgentsRequest{Workspace: dir}))
	require.NoError(t, openErr, "opening the workspace")
	res, err := cfg.SetApiKey(ctx, connect.NewRequest(&pb.SetApiKeyRequest{Workspace: dir, Provider: "gemini", Key: "AIza-project"}))
	assert.NoError(t, err, "workspace key: %v", res)
	assert.Equal(t, "", res.Msg.Change.ModelError, "workspace key: %v %v", res, err)
	assert.Contains(t, res.Msg.Change.Path, "/.blitz/workspaces/", "workspace key: %v %v", res, err)
	wdesc, _ := cfg.DescribeConfig(ctx, connect.NewRequest(&pb.DescribeConfigRequest{Workspace: dir}))
	sources := map[string]pb.KeySource{}
	for _, p := range wdesc.Msg.Providers {
		sources[p.Name] = p.KeySource
	}
	assert.Equal(t, pb.KeySource_KEY_SOURCE_KEYCHAIN, sources["gemini"], "workspace sources: %v", sources)
	assert.Equal(t, pb.KeySource_KEY_SOURCE_INHERITED, sources["anthropic"], "workspace sources: %v", sources)

	// Provider, model and key in one change: one reload, one model error.
	set, err := cfg.SetProvider(ctx, connect.NewRequest(&pb.SetProviderRequest{Workspace: dir, Provider: "openai", DefaultModel: "gpt-5", Key: "sk-project"}))
	require.NoError(t, err)
	assert.Contains(t, set.Msg.Change.Path, "/.blitz/workspaces/", "set provider: %v", set)
	wdesc, _ = cfg.DescribeConfig(ctx, connect.NewRequest(&pb.DescribeConfigRequest{Workspace: dir}))
	assert.Equal(t, "openai", wdesc.Msg.Provider, "workspace: %v", wdesc.Msg)
	assert.Equal(t, "gpt-5", wdesc.Msg.DefaultModel, "workspace: %v", wdesc.Msg)
	assert.NotContains(t, wdesc.Msg.String(), "sk-project", "the description shows the key")

	// Signing in with an ant profile instead: reported, and no key needed.
	_, err = cfg.SetProvider(ctx, connect.NewRequest(&pb.SetProviderRequest{Workspace: dir, Provider: "anthropic", DefaultModel: "claude-opus-5", Auth: &pb.ProviderAuth{Method: "oauth", Profile: "work"}}))
	require.NoError(t, err)
	wdesc, _ = cfg.DescribeConfig(ctx, connect.NewRequest(&pb.DescribeConfigRequest{Workspace: dir}))
	for _, p := range wdesc.Msg.Providers {
		if p.Name == "anthropic" {
			assert.Equal(t, "oauth", p.Auth, "anthropic: %v", p)
			assert.Equal(t, "work", p.Profile, "anthropic: %v", p)
		}
	}

	// Bad input is refused as such.
	for _, call := range []func() error{
		func() error {
			_, err := cfg.SaveConfigFile(ctx, connect.NewRequest(&pb.SaveConfigFileRequest{Text: "[llm\n"}))
			return err
		},
		func() error {
			_, err := cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Key: "blitz.auto_approve", Value: "true"}))
			return err
		},
		func() error {
			_, err := cfg.SetProvider(ctx, connect.NewRequest(&pb.SetProviderRequest{Provider: "ollama", Key: "x"}))
			return err
		},
		func() error {
			_, err := cfg.SetProvider(ctx, connect.NewRequest(&pb.SetProviderRequest{Provider: "openai", Auth: &pb.ProviderAuth{Method: "adc"}}))
			return err
		},
		func() error {
			_, err := cfg.DescribeConfig(ctx, connect.NewRequest(&pb.DescribeConfigRequest{Workspace: "relative/dir"}))
			return err
		},
	} {
		err := call()
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "want invalid argument, got %v", err)
	}
	saved, err := cfg.SaveConfigFile(ctx, connect.NewRequest(&pb.SaveConfigFileRequest{Text: "[llm]\nprovider = \"gemini\"\nnope = 1\n"}))
	assert.NoError(t, err, "save: %v", saved)
	assert.Len(t, saved.Msg.Warnings, 1, "save: %v %v", saved, err)
	assert.Equal(t, "line 3: unknown setting llm.nope", saved.Msg.Warnings[0])

	// Checked without saving, each problem at its line.
	checked, err := cfg.CheckConfigFile(ctx, connect.NewRequest(&pb.CheckConfigFileRequest{Text: "[permissions]\nallow = [\"shell(\"]\nnope = 1\n"}))
	require.NoError(t, err)
	require.Len(t, checked.Msg.Problems, 2, "problems: %v", checked.Msg.Problems)
	assert.Equal(t, int32(2), checked.Msg.Problems[0].Line)
	assert.True(t, checked.Msg.Problems[0].Error, "an invalid rule is an error")
	assert.Equal(t, int32(3), checked.Msg.Problems[1].Line)
	assert.False(t, checked.Msg.Problems[1].Error, "an unknown setting is a warning")
	file, _ = cfg.GetConfigFile(ctx, connect.NewRequest(&pb.GetConfigFileRequest{}))
	assert.NotContains(t, file.Msg.Text, "shell(", "checking saved the file")

	ref, err := cfg.GetSettingsReference(ctx, connect.NewRequest(&pb.GetSettingsReferenceRequest{}))
	require.NoError(t, err)
	require.NotEmpty(t, ref.Msg.Settings)
	assert.Equal(t, "blitz", ref.Msg.Settings[0].Key)
}

// Permission rules per scope: a workspace's add to the global ones, are
// checked before saving, and apply at once to the open workspace.
func TestConfigServicePermissions(t *testing.T) {
	c, s := serve(t, nil, text("hi"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()
	_, err := c.workspaces.ListAgents(ctx, connect.NewRequest(&pb.ListAgentsRequest{Workspace: dir}))
	require.NoError(t, err, "opening the workspace")

	_, err = cfg.AddPermission(ctx, connect.NewRequest(&pb.AddPermissionRequest{Effect: "deny", Rule: "shell(git push)"}))
	require.NoError(t, err)
	added, err := cfg.AddPermission(ctx, connect.NewRequest(&pb.AddPermissionRequest{Workspace: dir, Effect: "allow", Rule: "Bash(make)"}))
	require.NoError(t, err)
	assert.Equal(t, "shell(make)", added.Msg.Rule)

	desc, err := cfg.DescribePermissions(ctx, connect.NewRequest(&pb.DescribePermissionsRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Equal(t, []string{"allow shell(make)"}, entryStrings(desc.Msg.Rules))
	assert.Equal(t, []string{"deny shell(git push)"}, entryStrings(desc.Msg.Inherited))
	assert.True(t, desc.Msg.ReadOnlyDefaultsOn)
	assert.Contains(t, desc.Msg.ReadOnlyCommands, "git log")

	// The open workspace has them already.
	sources := func() map[string]string {
		res, err := c.workspaces.ListPermissionRules(ctx, connect.NewRequest(&pb.ListPermissionRulesRequest{Workspace: dir}))
		require.NoError(t, err)
		out := map[string]string{}
		for _, r := range res.Msg.Rules {
			out[r.Rule] = r.Source
		}
		return out
	}
	got := sources()
	assert.Equal(t, "workspace", got["shell(make)"])

	// Rules left out at a reload are reported with the rules.
	w, err := s.workspace(ctx, dir)
	require.NoError(t, err)
	broken := *w.Config()
	broken.Permissions.Deny = []string{"shell(re:[)"}
	require.Error(t, w.ReloadPermissions(&broken))
	listed, err := c.workspaces.ListPermissionRules(ctx, connect.NewRequest(&pb.ListPermissionRulesRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Contains(t, listed.Msg.Problem, "re:[")
	fixed := *w.Config()
	fixed.Permissions.Deny = nil
	require.NoError(t, w.ReloadPermissions(&fixed))
	assert.Equal(t, "global", got["shell(git push)"])
	assert.Equal(t, "built-in", got["shell(ls)"])

	// Checked, not saved.
	check, err := cfg.CheckPermission(ctx, connect.NewRequest(&pb.CheckPermissionRequest{Effect: "allow", Rule: "shell(git log)", Sample: "git log --oneline"}))
	require.NoError(t, err)
	assert.Equal(t, "prefix", check.Msg.Form)
	assert.True(t, check.Msg.Tested)
	assert.True(t, check.Msg.Matches)
	bad, err := cfg.CheckPermission(ctx, connect.NewRequest(&pb.CheckPermissionRequest{Effect: "allow", Rule: "shell(re:()"}))
	require.NoError(t, err)
	assert.Contains(t, bad.Msg.Error, "invalid")

	// The built-in rules off for the workspace.
	_, err = cfg.SetReadOnlyDefaults(ctx, connect.NewRequest(&pb.SetReadOnlyDefaultsRequest{Workspace: dir, Value: "off"}))
	require.NoError(t, err)
	desc, _ = cfg.DescribePermissions(ctx, connect.NewRequest(&pb.DescribePermissionsRequest{Workspace: dir}))
	assert.Equal(t, "off", desc.Msg.ReadOnlyDefaults)
	assert.False(t, desc.Msg.ReadOnlyDefaultsOn)
	assert.NotContains(t, sources(), "shell(ls)")

	removed, err := cfg.RemovePermission(ctx, connect.NewRequest(&pb.RemovePermissionRequest{Workspace: dir, Rule: "shell(make)"}))
	require.NoError(t, err)
	assert.Equal(t, int32(1), removed.Msg.Removed)
	assert.NotContains(t, sources(), "shell(make)")

	for _, call := range []func() error{
		func() error {
			_, err := cfg.AddPermission(ctx, connect.NewRequest(&pb.AddPermissionRequest{Effect: "allow", Rule: "nope(x)"}))
			return err
		},
		func() error {
			_, err := cfg.SetReadOnlyDefaults(ctx, connect.NewRequest(&pb.SetReadOnlyDefaultsRequest{Value: "maybe"}))
			return err
		},
	} {
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(call()))
	}
}

func entryStrings(es []*pb.PermissionEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Effect+" "+e.Rule)
	}
	return out
}

// The desktop app's System language follows the terminal's setting, with
// the user's catalogs (BL-DSK-41).
func TestGetInterfaceLanguage(t *testing.T) {
	_, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	cc := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	res, err := cc.GetInterfaceLanguage(context.Background(), connect.NewRequest(&pb.GetInterfaceLanguageRequest{}))
	require.NoError(t, err)
	assert.Empty(t, res.Msg.Locale, "unset")
	assert.Empty(t, res.Msg.Catalogs)

	home := os.Getenv("HOME")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz", "locales"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[ui]\nlocale = \"de\"\n"), 0o600))
	de := `{"meta":{"locale":"de"},"messages":{"desktop.settings":"Einstellungen"}}`
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", "locales", "de.json"), []byte(de), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", "locales", "broken.json"), []byte("{"), 0o600))
	res, err = cc.GetInterfaceLanguage(context.Background(), connect.NewRequest(&pb.GetInterfaceLanguageRequest{}))
	require.NoError(t, err)
	assert.Equal(t, "de", res.Msg.Locale)
	assert.Equal(t, []string{de}, res.Msg.Catalogs, "a broken catalog is left out")
}

// API keys over the API: a plain key in the settings file is moved to the
// keychain, then removed; keys for providers that take none, and moves
// with nothing to move, are invalid. The global permissions are described
// on their own.
func TestApiKeysOverTheAPI(t *testing.T) {
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	_, err := cfg.SaveConfigFile(ctx, connect.NewRequest(&pb.SaveConfigFileRequest{Text: "[llm.openai]\napi_key = \"sk-plain\"\n\n[permissions]\nread_only_defaults = false\n"}))
	require.NoError(t, err)
	moved, err := cfg.SecureApiKey(ctx, connect.NewRequest(&pb.SecureApiKeyRequest{Provider: "openai"}))
	require.NoError(t, err)
	assert.NotEmpty(t, moved.Msg.Change.Path)
	file, err := cfg.GetConfigFile(ctx, connect.NewRequest(&pb.GetConfigFileRequest{}))
	require.NoError(t, err)
	assert.NotContains(t, file.Msg.Text, "sk-plain", "the key stayed in the file")
	_, err = cfg.RemoveApiKey(ctx, connect.NewRequest(&pb.RemoveApiKeyRequest{Provider: "openai"}))
	require.NoError(t, err)
	file, _ = cfg.GetConfigFile(ctx, connect.NewRequest(&pb.GetConfigFileRequest{}))
	assert.NotContains(t, file.Msg.Text, "api_key", "the key is still set")
	set, err := cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Key: "blitz.default_model", Value: "gpt-5"}))
	require.NoError(t, err)
	assert.Empty(t, set.Msg.Change.ModelError, "a global change reports no model error")

	desc, err := cfg.DescribePermissions(ctx, connect.NewRequest(&pb.DescribePermissionsRequest{}))
	require.NoError(t, err)
	assert.Equal(t, "off", desc.Msg.ReadOnlyDefaults)
	assert.Empty(t, desc.Msg.Inherited, "the global scope inherits nothing")

	for name, call := range map[string]func() error{
		"secure with nothing to move": func() error { return unary(cfg.SecureApiKey, &pb.SecureApiKeyRequest{Provider: "openai"}) },
		"secure for no provider":      func() error { return unary(cfg.SecureApiKey, &pb.SecureApiKeyRequest{Provider: "nope"}) },
		"remove for no provider":      func() error { return unary(cfg.RemoveApiKey, &pb.RemoveApiKeyRequest{Provider: "nope"}) },
		"set for no provider":         func() error { return unary(cfg.SetApiKey, &pb.SetApiKeyRequest{Provider: "nope", Key: "k"}) },
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(call()))
		})
	}
}

// A server given a settings directory describes that one.
func TestConfigDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), []byte("[ui]\nlocale = \"fr-CA\"\n"), 0o600))
	s := New(nil, WithConfigDir(dir))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	defer s.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	file, err := cfg.GetConfigFile(context.Background(), connect.NewRequest(&pb.GetConfigFileRequest{}))
	require.NoError(t, err)
	assert.Equal(t, dir, filepath.Dir(file.Msg.Path))
	lang, err := cfg.GetInterfaceLanguage(context.Background(), connect.NewRequest(&pb.GetInterfaceLanguageRequest{}))
	require.NoError(t, err)
	assert.Equal(t, "fr-CA", lang.Msg.Locale)
}

// ListModels gives each configured provider's models, kept for a while
// (less when one failed), and forgotten when the settings change.
func TestListModelsOverTheAPI(t *testing.T) {
	_, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	cc := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	calls := 0
	failing := true
	was := listModels
	t.Cleanup(func() { listModels = was })
	listModels = func(_ context.Context, _ *config.Config, _ ...string) []runtime.ProviderModels {
		calls++
		out := []runtime.ProviderModels{{Provider: "gemini", Models: []runtime.ListedModel{{ID: "gemini-3.8-flash"}, {ID: "gemini-3.8-pro"}}}}
		if failing {
			out = append(out, runtime.ProviderModels{Provider: "anthropic", Err: errors.New("no key")})
		}
		return out
	}

	res, err := cc.ListModels(ctx, connect.NewRequest(&pb.ListModelsRequest{}))
	require.NoError(t, err)
	require.Len(t, res.Msg.Providers, 2)
	assert.Equal(t, []string{"gemini-3.8-flash", "gemini-3.8-pro"}, res.Msg.Providers[0].Ids)
	assert.Equal(t, "no key", res.Msg.Providers[1].Error)
	assert.NotEmpty(t, res.Msg.DefaultProvider)

	_, err = cc.ListModels(ctx, connect.NewRequest(&pb.ListModelsRequest{}))
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "kept")

	// A failed provider's listing is kept for less; a settings change drops it.
	s.models.mu.Lock()
	assert.WithinDuration(t, time.Now().Add(modelsRetry), s.models.byID[""].expires, 5*time.Second)
	s.models.mu.Unlock()
	failing = false
	_, err = cc.SetApiKey(ctx, connect.NewRequest(&pb.SetApiKeyRequest{Provider: "anthropic", Key: "sk-ant-x"}))
	require.NoError(t, err)
	res, err = cc.ListModels(ctx, connect.NewRequest(&pb.ListModelsRequest{}))
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "listed again after the key was set")
	assert.Len(t, res.Msg.Providers, 1)

	_, err = cc.ListModels(ctx, connect.NewRequest(&pb.ListModelsRequest{Workspace: "relative"}))
	assert.Error(t, err, "a workspace must be absolute")
}

// A workspace's search settings, set through the config service, apply at
// once: search off, then back on with other default sources; the status
// shows the settings either way.
func TestSearchSettingsApplyAtOnce(t *testing.T) {
	c, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()
	status := func() *pb.SearchStatus {
		res, err := c.workspaces.GetSearchStatus(ctx, connect.NewRequest(&pb.GetSearchStatusRequest{Workspace: dir}))
		require.NoError(t, err)
		return res.Msg.Status
	}
	require.True(t, status().Enabled)
	assert.Equal(t, []string{"files", "documents"}, status().Settings.Sources)

	_, err := cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Workspace: dir, Key: "search.enabled", Value: "false"}))
	require.NoError(t, err)
	st := status()
	assert.False(t, st.Enabled, "off at once")
	assert.False(t, st.Settings.Enabled)

	_, err = cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Workspace: dir, Key: "search.enabled", Value: ""}))
	require.NoError(t, err)
	_, err = cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Workspace: dir, Key: "search.sources", Value: "notes,chats"}))
	require.NoError(t, err)
	st = status()
	assert.True(t, st.Enabled, "on again")
	assert.Equal(t, []string{"notes", "chats"}, st.Settings.Sources)

	_, err = cfg.SetConfigValue(ctx, connect.NewRequest(&pb.SetConfigValueRequest{Workspace: dir, Key: "search.sources", Value: "email"}))
	assert.Error(t, err, "not a source")
}
