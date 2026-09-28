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
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
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
