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
