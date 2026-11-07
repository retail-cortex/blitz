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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MCP servers are added, edited, turned off and on and removed in the
// global settings, and an open workspace follows at once; a running one
// restarts.
func TestMCPServersOverTheAPI(t *testing.T) {
	c, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()
	running := func() map[string]*pb.MCPServer {
		res, err := c.workspaces.ListMCPServers(ctx, connect.NewRequest(&pb.ListMCPServersRequest{Workspace: dir}))
		require.NoError(t, err)
		out := map[string]*pb.MCPServer{}
		for _, m := range res.Msg.Servers {
			out[m.Name] = m
		}
		return out
	}
	require.Empty(t, running())
	code := func(err error) connect.Code { return connect.CodeOf(err) }

	off := false
	_, err := cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Server: &pb.MCPServerConfig{
		Name: "docs", Url: "http://127.0.0.1:1/mcp", Headers: map[string]string{"X-Team": "core"}, AutoApprove: true, Sandbox: &off, TimeoutSeconds: 30,
	}}))
	require.NoError(t, err)
	list, err := cfg.ListMCPServerConfigs(ctx, connect.NewRequest(&pb.ListMCPServerConfigsRequest{}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Servers, 1)
	got := list.Msg.Servers[0]
	assert.Equal(t, "docs", got.Name)
	assert.Equal(t, map[string]string{"X-Team": ""}, got.Headers, "values are withheld")
	assert.True(t, got.AutoApprove)
	require.NotNil(t, got.Sandbox)
	assert.False(t, *got.Sandbox)
	assert.EqualValues(t, 30, got.TimeoutSeconds)
	assert.NotEmpty(t, list.Msg.Path)
	assert.Equal(t, "idle", running()["docs"].State, "the open workspace has it at once")

	_, err = cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Server: &pb.MCPServerConfig{Name: "docs", Command: "x"}}))
	assert.Equal(t, connect.CodeInvalidArgument, code(err), "a name taken")
	_, err = cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Name: "nope", Server: &pb.MCPServerConfig{Name: "nope", Command: "x"}}))
	assert.Equal(t, connect.CodeNotFound, code(err))

	_, err = cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Name: "docs", Enabled: false}))
	require.NoError(t, err)
	assert.Equal(t, "disabled", running()["docs"].State)
	_, err = c.workspaces.RestartMCPServer(ctx, connect.NewRequest(&pb.RestartMCPServerRequest{Workspace: dir, Name: "docs"}))
	assert.Equal(t, connect.CodeNotFound, code(err), "a disabled server isn't running")
	_, err = cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Name: "docs", Enabled: true}))
	require.NoError(t, err)
	assert.Equal(t, "idle", running()["docs"].State)
	_, err = c.workspaces.RestartMCPServer(ctx, connect.NewRequest(&pb.RestartMCPServerRequest{Workspace: dir, Name: "docs"}))
	require.NoError(t, err)

	_, err = cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Name: "docs", Server: &pb.MCPServerConfig{Name: "wiki", Url: "http://127.0.0.1:1/wiki"}}))
	require.NoError(t, err)
	r := running()
	assert.Contains(t, r, "wiki")
	assert.NotContains(t, r, "docs", "renamed")
	assert.Equal(t, "http://127.0.0.1:1/wiki", r["wiki"].Target)

	_, err = cfg.RemoveMCPServer(ctx, connect.NewRequest(&pb.RemoveMCPServerRequest{Name: "wiki"}))
	require.NoError(t, err)
	assert.Empty(t, running())
	_, err = cfg.RemoveMCPServer(ctx, connect.NewRequest(&pb.RemoveMCPServerRequest{Name: "wiki"}))
	assert.Equal(t, connect.CodeNotFound, code(err))
	_, err = cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Name: "wiki"}))
	assert.Equal(t, connect.CodeNotFound, code(err))
}

// A listing withholds env and header values (a secret's name aside); an
// edit sending one back empty keeps it, and the edit keeps the tools and
// prefix it's sent (scan N26-desktop-1#10, #11).
func TestMCPServerValuesWithheld(t *testing.T) {
	_, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	listed := func() *pb.MCPServerConfig {
		res, err := cfg.ListMCPServerConfigs(ctx, connect.NewRequest(&pb.ListMCPServerConfigsRequest{}))
		require.NoError(t, err)
		require.Len(t, res.Msg.Servers, 1)
		return res.Msg.Servers[0]
	}
	file := func() string {
		res, err := cfg.ListMCPServerConfigs(ctx, connect.NewRequest(&pb.ListMCPServerConfigsRequest{}))
		require.NoError(t, err)
		b, err := os.ReadFile(res.Msg.Path)
		require.NoError(t, err)
		return string(b)
	}
	_, err := cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Server: &pb.MCPServerConfig{
		Name: "github", Command: "npx", Env: map[string]string{"GITHUB_TOKEN": "ghp_secret", "TEAM": "{secrets:team}"},
		Tools: []string{"create_issue"}, Prefix: "gh",
	}}))
	require.NoError(t, err)
	got := listed()
	assert.Equal(t, map[string]string{"GITHUB_TOKEN": "", "TEAM": "{secrets:team}"}, got.Env)
	assert.Equal(t, []string{"create_issue"}, got.Tools)
	assert.Equal(t, "gh", got.Prefix)

	// In order: each edits what the one before saved.
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"empty keeps it", map[string]string{"GITHUB_TOKEN": "", "TEAM": "{secrets:team}"}, `"GITHUB_TOKEN" = "ghp_secret"`},
		{"a new one", map[string]string{"GITHUB_TOKEN": "ghp_new"}, `"GITHUB_TOKEN" = "ghp_new"`},
		{"empty again", map[string]string{"GITHUB_TOKEN": ""}, `"GITHUB_TOKEN" = "ghp_new"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := listed()
			got.Env = tc.env
			_, err := cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Name: "github", Server: got}))
			require.NoError(t, err)
			assert.Contains(t, file(), tc.want)
			assert.Equal(t, []string{"create_issue"}, listed().Tools)
			assert.Equal(t, "gh", listed().Prefix)
		})
	}
}

// A workspace's own MCP servers: added, listed with the global ones by
// scope, run by that workspace alone; a global one turned off there
// stays on elsewhere; Make global moves one to every workspace.
func TestWorkspaceMCPServersOverTheAPI(t *testing.T) {
	c, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir, other := t.TempDir(), t.TempDir()
	running := func(ws string) map[string]*pb.MCPServer {
		res, err := c.workspaces.ListMCPServers(ctx, connect.NewRequest(&pb.ListMCPServersRequest{Workspace: ws}))
		require.NoError(t, err)
		out := map[string]*pb.MCPServer{}
		for _, m := range res.Msg.Servers {
			out[m.Name] = m
		}
		return out
	}
	listed := func(ws string) map[string]*pb.MCPServerConfig {
		res, err := cfg.ListMCPServerConfigs(ctx, connect.NewRequest(&pb.ListMCPServerConfigsRequest{Workspace: ws}))
		require.NoError(t, err)
		out := map[string]*pb.MCPServerConfig{}
		for _, m := range res.Msg.Servers {
			out[m.Name] = m
		}
		return out
	}
	running(dir) // open both: changes reach open workspaces
	running(other)
	_, err := cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Server: &pb.MCPServerConfig{Name: "docs", Url: "http://127.0.0.1:1/docs"}}))
	require.NoError(t, err)
	_, err = cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Workspace: dir, Server: &pb.MCPServerConfig{Name: "jira", Url: "http://127.0.0.1:1/jira"}}))
	require.NoError(t, err)

	l := listed(dir)
	require.Len(t, l, 2)
	assert.Equal(t, "global", l["docs"].Scope)
	assert.Equal(t, "workspace", l["jira"].Scope)
	assert.Contains(t, running(dir), "jira")
	assert.NotContains(t, running(other), "jira", "a workspace's own stays there")
	assert.NotContains(t, listed(""), "jira")

	_, err = cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Workspace: dir, Name: "docs", Enabled: false}))
	require.NoError(t, err)
	assert.True(t, listed(dir)["docs"].OffHere)
	assert.False(t, listed(dir)["docs"].Disabled, "its own file's setting is unchanged")
	assert.Equal(t, "disabled", running(dir)["docs"].State)
	assert.Equal(t, "idle", running(other)["docs"].State, "on in the other workspace")

	_, err = cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Workspace: dir, Name: "jira", Enabled: false}))
	require.NoError(t, err)
	assert.True(t, listed(dir)["jira"].Disabled, "its own is disabled in its file")

	_, err = cfg.RemoveMCPServer(ctx, connect.NewRequest(&pb.RemoveMCPServerRequest{Workspace: dir, Name: "docs"}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "a workspace never removes a global one")

	_, err = cfg.MakeMCPServerGlobal(ctx, connect.NewRequest(&pb.MakeMCPServerGlobalRequest{Workspace: dir, Name: "jira"}))
	require.NoError(t, err)
	assert.Equal(t, "global", listed(dir)["jira"].Scope)
	assert.Contains(t, running(other), "jira", "every workspace has it now")
	_, err = cfg.MakeMCPServerGlobal(ctx, connect.NewRequest(&pb.MakeMCPServerGlobalRequest{Workspace: dir, Name: "jira"}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "moved already")
	_, err = cfg.MakeMCPServerGlobal(ctx, connect.NewRequest(&pb.MakeMCPServerGlobalRequest{Name: "jira"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "needs a workspace")

	// A workspace server named as a global one stands in for it; it can't
	// be made global while the global one is there.
	_, err = cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Workspace: dir, Server: &pb.MCPServerConfig{Name: "docs", Command: "local-docs"}}))
	require.NoError(t, err)
	assert.Equal(t, "workspace", listed(dir)["docs"].Scope)
	_, err = cfg.MakeMCPServerGlobal(ctx, connect.NewRequest(&pb.MakeMCPServerGlobalRequest{Workspace: dir, Name: "docs"}))
	assert.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))

	// The project's servers are listed by scope, as they are.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".blitz"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".blitz", "settings.toml"), []byte("[[mcp.servers]]\nname = \"issues\"\ncommand = \"node\"\nargs = [\"tools/issues.js\"]\n\n[[mcp.servers]]\nname = \"wiki\"\nurl = \"https://mcp.example/wiki\"\n"), 0o644))
	l = listed(dir)
	assert.Equal(t, "project", l["issues"].Scope)
	assert.Equal(t, "node tools/issues.js", l["issues"].Command)
	assert.Equal(t, "https://mcp.example/wiki", l["wiki"].Url)

	// A scope that isn't an absolute folder is refused by every call.
	bad := "not/absolute"
	for name, call := range map[string]func() error{
		"list": func() error {
			_, err := cfg.ListMCPServerConfigs(ctx, connect.NewRequest(&pb.ListMCPServerConfigsRequest{Workspace: bad}))
			return err
		},
		"save": func() error {
			_, err := cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Workspace: bad, Server: &pb.MCPServerConfig{Name: "x", Command: "x"}}))
			return err
		},
		"remove": func() error {
			_, err := cfg.RemoveMCPServer(ctx, connect.NewRequest(&pb.RemoveMCPServerRequest{Workspace: bad, Name: "x"}))
			return err
		},
		"enable": func() error {
			_, err := cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Workspace: bad, Name: "x"}))
			return err
		},
		"make global": func() error {
			_, err := cfg.MakeMCPServerGlobal(ctx, connect.NewRequest(&pb.MakeMCPServerGlobalRequest{Workspace: bad, Name: "x"}))
			return err
		},
		"check key": func() error {
			_, err := cfg.CheckApiKey(ctx, connect.NewRequest(&pb.CheckApiKeyRequest{Workspace: bad, Provider: "gemini", Key: "x"}))
			return err
		},
	} {
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(call()), name)
	}
}

// DraftManifest refuses a kind it has no brief for, and answers a problem
// (not an error) when the model can't be had.
func TestDraftManifestOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil)
	ctx := context.Background()
	dir := t.TempDir()
	_, err := c.workspaces.DraftManifest(ctx, connect.NewRequest(&pb.DraftManifestRequest{Workspace: dir, Kind: "poem", Request: "x"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	res, err := c.workspaces.DraftManifest(ctx, connect.NewRequest(&pb.DraftManifestRequest{Workspace: dir, Kind: "agent", Description: "a reviewer"}))
	require.NoError(t, err)
	assert.True(t, res.Msg.Text != "" || res.Msg.Problem != "", "a draft or why not")
}

// The specification over the API: listed, and a spec reviewed as edited
// (spec_057_spec_driven SDD-01, SDD-02).
func TestSpecsOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil)
	ctx := context.Background()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".agents", "specs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".agents", "specs", "001-overview.md"), []byte("# 001. Overview\nStatus: approved\nDepends on: 001\n- **OV-01** One store.\nDone when: open.\n"), 0o644))
	list, err := c.workspaces.ListSpecs(ctx, connect.NewRequest(&pb.ListSpecsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Specs, 1)
	s := list.Msg.Specs[0]
	assert.Equal(t, ".agents/specs/001-overview.md", s.Path)
	assert.Equal(t, []int32{1}, s.DependsOn)
	assert.EqualValues(t, 1, s.Errors, "it depends on itself")

	res, err := c.workspaces.ReviewSpec(ctx, connect.NewRequest(&pb.ReviewSpecRequest{Workspace: dir, Path: s.Path, Text: "# 001. Overview\nStatus: approved\n- **OV-01** One store.\nDone when: open.\n"}))
	require.NoError(t, err)
	assert.Empty(t, res.Msg.Findings, "the edited text, not the file")
	_, err = c.workspaces.ReviewSpec(ctx, connect.NewRequest(&pb.ReviewSpecRequest{Workspace: dir, Path: "README.md"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// Approve, trace and verify (SDD-10..12).
	_, err = c.workspaces.ApproveSpec(ctx, connect.NewRequest(&pb.ApproveSpecRequest{Workspace: dir, Path: s.Path}))
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "it depends on itself")
	a, err := c.workspaces.ApproveSpec(ctx, connect.NewRequest(&pb.ApproveSpecRequest{Workspace: dir, Path: s.Path, Force: true}))
	require.NoError(t, err)
	assert.Contains(t, a.Msg.Text, "Status: approved")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main // OV-01\n"), 0o644))
	v, err := c.workspaces.VerifySpec(ctx, connect.NewRequest(&pb.VerifySpecRequest{Workspace: dir, Path: s.Path}))
	require.NoError(t, err)
	require.Len(t, v.Msg.Requirements, 1)
	assert.Equal(t, "traced", v.Msg.Requirements[0].State)
	assert.Equal(t, "main.go", v.Msg.Requirements[0].Evidence[0].Path)
	assert.NotNil(t, v.Msg.Requirements[0].VerifiedAt)
	tr, err := c.workspaces.TraceSpecs(ctx, connect.NewRequest(&pb.TraceSpecsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, tr.Msg.Requirements, 1)
	_, err = c.workspaces.TraceSpecs(ctx, connect.NewRequest(&pb.TraceSpecsRequest{Workspace: dir, Path: "x.md"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	_, err = c.workspaces.VerifySpec(ctx, connect.NewRequest(&pb.VerifySpecRequest{Workspace: dir, Path: ".agents/specs/404.md"}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// A change recorded, and its impact (SDD-20, SDD-21).
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".agents", "specs", "001-overview.md"), []byte(strings.Replace(a.Msg.Text, "One store.", "Two stores.", 1)), 0o644))
	rec, err := c.workspaces.RecordSpecChange(ctx, connect.NewRequest(&pb.RecordSpecChangeRequest{Workspace: dir, Path: s.Path}))
	require.NoError(t, err)
	require.Len(t, rec.Msg.Changes, 1)
	assert.Equal(t, "changed", rec.Msg.Changes[0].Kind)
	assert.Contains(t, rec.Msg.Text, "Status: changed")
	im, err := c.workspaces.ImpactSpec(ctx, connect.NewRequest(&pb.ImpactSpecRequest{Workspace: dir, Path: s.Path}))
	require.NoError(t, err)
	require.Len(t, im.Msg.Changes, 1)
	require.NotEmpty(t, im.Msg.Places)
	assert.Equal(t, "main.go", im.Msg.Places[0].Path)
	im, err = c.workspaces.ImpactSpec(ctx, connect.NewRequest(&pb.ImpactSpecRequest{Workspace: dir, Path: s.Path, Judge: true}))
	require.NoError(t, err)
	assert.True(t, im.Msg.Summary != "" || im.Msg.Problem != "", "a report or why not")
	_, err = c.workspaces.RecordSpecChange(ctx, connect.NewRequest(&pb.RecordSpecChangeRequest{Workspace: dir, Path: "README.md"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// The plan, progress and drift (SDD-30..32).
	plan, err := c.workspaces.PlanSpec(ctx, connect.NewRequest(&pb.PlanSpecRequest{Workspace: dir, Path: s.Path}))
	require.NoError(t, err)
	assert.Contains(t, plan.Msg.Prompt, "OV-01")
	v, err = c.workspaces.VerifySpec(ctx, connect.NewRequest(&pb.VerifySpecRequest{Workspace: dir, Path: s.Path}))
	require.NoError(t, err)
	require.Len(t, v.Msg.Drift, 1)
	assert.Equal(t, "changed", v.Msg.Drift[0].Why)
	list, err = c.workspaces.ListSpecs(ctx, connect.NewRequest(&pb.ListSpecsRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.True(t, list.Msg.Specs[0].Next, "not done: it changed")
	_, err = c.workspaces.PlanSpec(ctx, connect.NewRequest(&pb.PlanSpecRequest{Workspace: dir, Path: "x.md"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// Every spec call needs a workspace it can open.
func TestSpecsNeedAWorkspace(t *testing.T) {
	c, _ := serve(t, nil)
	ctx := context.Background()
	ws := "relative"
	for name, call := range map[string]func() error{
		"ListSpecs": func() error {
			_, err := c.workspaces.ListSpecs(ctx, connect.NewRequest(&pb.ListSpecsRequest{Workspace: ws}))
			return err
		},
		"ReviewSpec": func() error {
			_, err := c.workspaces.ReviewSpec(ctx, connect.NewRequest(&pb.ReviewSpecRequest{Workspace: ws}))
			return err
		},
		"TraceSpecs": func() error {
			_, err := c.workspaces.TraceSpecs(ctx, connect.NewRequest(&pb.TraceSpecsRequest{Workspace: ws}))
			return err
		},
		"ApproveSpec": func() error {
			_, err := c.workspaces.ApproveSpec(ctx, connect.NewRequest(&pb.ApproveSpecRequest{Workspace: ws}))
			return err
		},
		"VerifySpec": func() error {
			_, err := c.workspaces.VerifySpec(ctx, connect.NewRequest(&pb.VerifySpecRequest{Workspace: ws}))
			return err
		},
		"RecordSpecChange": func() error {
			_, err := c.workspaces.RecordSpecChange(ctx, connect.NewRequest(&pb.RecordSpecChangeRequest{Workspace: ws}))
			return err
		},
		"ImpactSpec": func() error {
			_, err := c.workspaces.ImpactSpec(ctx, connect.NewRequest(&pb.ImpactSpecRequest{Workspace: ws}))
			return err
		},
		"PlanSpec": func() error {
			_, err := c.workspaces.PlanSpec(ctx, connect.NewRequest(&pb.PlanSpecRequest{Workspace: ws}))
			return err
		},
	} {
		assert.Error(t, call(), name)
	}
	// A path that isn't a spec, in a workspace that is.
	dir := t.TempDir()
	_, err := c.workspaces.ImpactSpec(ctx, connect.NewRequest(&pb.ImpactSpecRequest{Workspace: dir, Path: "x.md"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	_, err = c.workspaces.ApproveSpec(ctx, connect.NewRequest(&pb.ApproveSpecRequest{Workspace: dir, Path: "x.md"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// Pictures over the API without a model: nothing offered, a turn refused,
// what isn't there not found (spec_058_pictures).
func TestPicturesOverTheAPI(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	c, _ := serve(t, nil)
	ctx := context.Background()
	dir := t.TempDir()
	models, err := c.workspaces.ListPictureModels(ctx, connect.NewRequest(&pb.ListPictureModelsRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Empty(t, models.Msg.Models)
	assert.Equal(t, "pictures", models.Msg.Folder)
	_, err = c.workspaces.MakePicture(ctx, connect.NewRequest(&pb.MakePictureRequest{Workspace: dir, Model: "gemini/gemini-4-argon", Prompt: "x"}))
	assert.Error(t, err)
	list, err := c.workspaces.ListPictureSessions(ctx, connect.NewRequest(&pb.ListPictureSessionsRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Empty(t, list.Msg.Sessions)
	id := "pic-000000000000"
	for name, call := range map[string]func() error{
		"get": func() error {
			_, err := c.workspaces.GetPictureSession(ctx, connect.NewRequest(&pb.GetPictureSessionRequest{Workspace: dir, Id: id}))
			return err
		},
		"rename": func() error {
			_, err := c.workspaces.RenamePictureSession(ctx, connect.NewRequest(&pb.RenamePictureSessionRequest{Workspace: dir, Id: id, Name: "x"}))
			return err
		},
		"delete": func() error {
			_, err := c.workspaces.DeletePictureSession(ctx, connect.NewRequest(&pb.DeletePictureSessionRequest{Workspace: dir, Id: id}))
			return err
		},
		"read": func() error {
			_, err := c.workspaces.ReadPicture(ctx, connect.NewRequest(&pb.ReadPictureRequest{Workspace: dir, Session: id, Picture: "p"}))
			return err
		},
		"save": func() error {
			_, err := c.workspaces.SavePicture(ctx, connect.NewRequest(&pb.SavePictureRequest{Workspace: dir, Session: id, Picture: "p"}))
			return err
		},
		"drop": func() error {
			_, err := c.workspaces.DeletePicture(ctx, connect.NewRequest(&pb.DeletePictureRequest{Workspace: dir, Session: id, Picture: "p"}))
			return err
		},
	} {
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(call()), name)
	}
	ws := "relative"
	for name, call := range map[string]func() error{
		"models": func() error {
			_, err := c.workspaces.ListPictureModels(ctx, connect.NewRequest(&pb.ListPictureModelsRequest{Workspace: ws}))
			return err
		},
		"make": func() error {
			_, err := c.workspaces.MakePicture(ctx, connect.NewRequest(&pb.MakePictureRequest{Workspace: ws}))
			return err
		},
		"sessions": func() error {
			_, err := c.workspaces.ListPictureSessions(ctx, connect.NewRequest(&pb.ListPictureSessionsRequest{Workspace: ws}))
			return err
		},
		"get": func() error {
			_, err := c.workspaces.GetPictureSession(ctx, connect.NewRequest(&pb.GetPictureSessionRequest{Workspace: ws}))
			return err
		},
		"rename": func() error {
			_, err := c.workspaces.RenamePictureSession(ctx, connect.NewRequest(&pb.RenamePictureSessionRequest{Workspace: ws}))
			return err
		},
		"delete": func() error {
			_, err := c.workspaces.DeletePictureSession(ctx, connect.NewRequest(&pb.DeletePictureSessionRequest{Workspace: ws}))
			return err
		},
		"read": func() error {
			_, err := c.workspaces.ReadPicture(ctx, connect.NewRequest(&pb.ReadPictureRequest{Workspace: ws}))
			return err
		},
		"save": func() error {
			_, err := c.workspaces.SavePicture(ctx, connect.NewRequest(&pb.SavePictureRequest{Workspace: ws}))
			return err
		},
		"drop": func() error {
			_, err := c.workspaces.DeletePicture(ctx, connect.NewRequest(&pb.DeletePictureRequest{Workspace: ws}))
			return err
		},
	} {
		assert.Error(t, call(), name)
	}
}

// Pictures over the API with a key and a session on disk: the models and
// what they can do, an estimate, and a session listed, read, saved,
// renamed and deleted (spec_058_pictures).
func TestPictureSessionsOverTheAPI(t *testing.T) {
	store := t.TempDir()
	c, _ := serve(t, func(cfg *config.Config) {
		cfg.Session.StorageDir = store
		cfg.LLM.Gemini.APIKey = "k" // listing and estimates call nothing
	})
	ctx := context.Background()
	dir := t.TempDir()
	models, err := c.workspaces.ListPictureModels(ctx, connect.NewRequest(&pb.ListPictureModelsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.NotEmpty(t, models.Msg.Models)
	nano := models.Msg.Models[0]
	assert.Equal(t, "gemini/gemini-nano-banana-2.1", nano.Ref)
	assert.EqualValues(t, 3767, nano.SizeTokens["4K"])
	est, err := c.workspaces.MakePicture(ctx, connect.NewRequest(&pb.MakePictureRequest{Workspace: dir, Model: nano.Ref, Prompt: "a fox", Size: "4K", Estimate: true,
		References: []*pb.PictureReference{{Data: []byte("\x89PNG\r\n\x1a\nx"), Mime: "image/png", Role: "style"}}}))
	require.NoError(t, err)
	assert.InDelta(t, 0.113, est.Msg.CostUsd, 0.01)
	assert.Nil(t, est.Msg.Session)
	_, err = c.workspaces.MakePicture(ctx, connect.NewRequest(&pb.MakePictureRequest{Workspace: dir, Model: nano.Ref, Prompt: "a fox", Count: 3}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "the catalog refuses it")

	// A session, as the engine keeps it.
	seed := int32(3)
	s := engine.PictureSession{ID: "pic-0123456789ab", Name: "A fox", Turns: []engine.PictureTurn{{ID: "t-1", Model: nano.Ref, Kind: "gemini", Prompt: "a fox", Seed: &seed,
		References: []engine.PictureTurnRef{{File: "r-1.png", MIME: "image/png", Name: "me.png", Role: "character"}},
		Pictures:   []engine.PictureFile{{ID: "p-0123456789ab", File: "p-0123456789ab.png", MIME: "image/png"}, {ID: "p-gone", Deleted: true}},
		Sources:    []runtime.PictureSource{{Title: "Foxes", URL: "https://example.com"}}}}}
	data, err := json.Marshal(s)
	require.NoError(t, err)
	for _, d := range []string{dir, evalDir(t, dir)} {
		sum := sha256.Sum256([]byte(d))
		sd := filepath.Join(store, "pictures", hex.EncodeToString(sum[:8]), s.ID)
		require.NoError(t, os.MkdirAll(sd, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(sd, "session.json"), data, 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(sd, "p-0123456789ab.png"), []byte("PNG"), 0o600))
	}
	list, err := c.workspaces.ListPictureSessions(ctx, connect.NewRequest(&pb.ListPictureSessionsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Sessions, 1)
	turn := list.Msg.Sessions[0].Turns[0]
	assert.Len(t, turn.Pictures, 1, "not the deleted one")
	assert.True(t, turn.HasSeed)
	assert.Equal(t, "character", turn.References[0].Role)
	assert.Equal(t, "Foxes", turn.Sources[0].Title)
	got, err := c.workspaces.GetPictureSession(ctx, connect.NewRequest(&pb.GetPictureSessionRequest{Workspace: dir, Id: s.ID}))
	require.NoError(t, err)
	assert.Equal(t, "A fox", got.Msg.Session.Name)
	pic, err := c.workspaces.ReadPicture(ctx, connect.NewRequest(&pb.ReadPictureRequest{Workspace: dir, Session: s.ID, Picture: "p-0123456789ab"}))
	require.NoError(t, err)
	assert.Equal(t, "PNG", string(pic.Msg.Data))
	saved, err := c.workspaces.SavePicture(ctx, connect.NewRequest(&pb.SavePictureRequest{Workspace: dir, Session: s.ID, Picture: "p-0123456789ab"}))
	require.NoError(t, err)
	assert.Equal(t, "pictures/a-fox-1.png", saved.Msg.Path)
	renamed, err := c.workspaces.RenamePictureSession(ctx, connect.NewRequest(&pb.RenamePictureSessionRequest{Workspace: dir, Id: s.ID, Name: "Fox"}))
	require.NoError(t, err)
	assert.Equal(t, "Fox", renamed.Msg.Session.Name)
	dropped, err := c.workspaces.DeletePicture(ctx, connect.NewRequest(&pb.DeletePictureRequest{Workspace: dir, Session: s.ID, Picture: "p-0123456789ab"}))
	require.NoError(t, err)
	assert.Empty(t, dropped.Msg.Session.Turns[0].Pictures)
	_, err = c.workspaces.DeletePictureSession(ctx, connect.NewRequest(&pb.DeletePictureSessionRequest{Workspace: dir, Id: s.ID}))
	require.NoError(t, err)
}

// evalDir is dir with its links resolved (macOS's /var is /private/var).
func evalDir(t *testing.T, dir string) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	return d
}
