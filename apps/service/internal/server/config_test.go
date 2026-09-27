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
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/secrets"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
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
	if _, err := cfg.SetApiKey(ctx, connect.NewRequest(&pb.SetApiKeyRequest{Provider: "anthropic", Key: "sk-ant-secret"})); err != nil {
		t.Fatal(err)
	}
	desc, err := cfg.DescribeConfig(ctx, connect.NewRequest(&pb.DescribeConfigRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(desc.Msg.String(), "sk-ant-secret") {
		t.Error("the description shows the key")
	}
	for _, p := range desc.Msg.Providers {
		if p.Name == "anthropic" && p.KeySource != pb.KeySource_KEY_SOURCE_KEYCHAIN {
			t.Errorf("anthropic: %v", p)
		}
	}
	if file, _ := cfg.GetConfigFile(ctx, connect.NewRequest(&pb.GetConfigFileRequest{})); !strings.Contains(file.Msg.Text, `"keychain:global/llm.anthropic.api_key"`) {
		t.Errorf("file:\n%s", file.Msg.Text)
	}

	// A workspace key while the workspace is open: it reloads.
	dir := t.TempDir()
	if _, err := c.workspaces.ListAgents(ctx, connect.NewRequest(&pb.ListAgentsRequest{Workspace: dir})); err != nil {
		t.Fatal(err)
	}
	res, err := cfg.SetApiKey(ctx, connect.NewRequest(&pb.SetApiKeyRequest{Workspace: dir, Provider: "gemini", Key: "AIza-project"}))
	if err != nil || res.Msg.Change.ModelError != "" || !strings.Contains(res.Msg.Change.Path, "/.blitz/workspaces/") {
		t.Errorf("workspace key: %v %v", res, err)
	}
	wdesc, _ := cfg.DescribeConfig(ctx, connect.NewRequest(&pb.DescribeConfigRequest{Workspace: dir}))
	sources := map[string]pb.KeySource{}
	for _, p := range wdesc.Msg.Providers {
		sources[p.Name] = p.KeySource
	}
	if sources["gemini"] != pb.KeySource_KEY_SOURCE_KEYCHAIN || sources["anthropic"] != pb.KeySource_KEY_SOURCE_INHERITED {
		t.Errorf("workspace sources: %v", sources)
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
			_, err := cfg.DescribeConfig(ctx, connect.NewRequest(&pb.DescribeConfigRequest{Workspace: "relative/dir"}))
			return err
		},
	} {
		if err := call(); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("want invalid argument, got %v", err)
		}
	}
	saved, err := cfg.SaveConfigFile(ctx, connect.NewRequest(&pb.SaveConfigFileRequest{Text: "[llm]\nprovider = \"gemini\"\nnope = 1\n"}))
	if err != nil || len(saved.Msg.Warnings) != 1 {
		t.Errorf("save: %v %v", saved, err)
	}
}
