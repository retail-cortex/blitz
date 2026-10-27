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
	"fmt"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// MCP servers for the desktop app's Settings › Connectors › MCP servers,
// Studio › Connectors and the tray (spec_mcp_009 MCP-45..MCP-50): the
// global settings' [[mcp.servers]] and a workspace's own, edited as
// `blitz mcp` does, and applied to open workspaces at once.

func mcpConfigMsg(s config.MCPServerConfig) *pb.MCPServerConfig {
	return &pb.MCPServerConfig{
		Name: s.Name, Command: s.Command, Args: s.Args, Env: s.Env, Url: s.URL, Headers: s.Headers,
		Disabled: s.Disabled && !s.OffHere, Tools: s.Tools, AutoApprove: s.AutoApprove, Sandbox: s.Sandbox,
		Prefix: s.Prefix, Agents: s.Agents, TimeoutSeconds: int32(s.TimeoutSeconds), Scope: s.Scope, OffHere: s.OffHere,
	}
}

func mcpConfig(m *pb.MCPServerConfig) config.MCPServerConfig {
	return config.MCPServerConfig{
		Name: m.GetName(), Command: m.GetCommand(), Args: m.GetArgs(), Env: m.GetEnv(), URL: m.GetUrl(), Headers: m.GetHeaders(),
		Disabled: m.GetDisabled(), Tools: m.GetTools(), AutoApprove: m.GetAutoApprove(), Sandbox: m.Sandbox,
		Prefix: m.GetPrefix(), Agents: m.GetAgents(), TimeoutSeconds: int(m.GetTimeoutSeconds()),
	}
}

// mcpEditError is err as the API says it: an unknown server is NOT_FOUND,
// anything else a bad request.
func mcpEditError(err error, name string) error {
	if errors.Is(err, config.ErrNoMCPServer) {
		return apiError(connect.CodeNotFound, "MCP_SERVER_NOT_FOUND", err, "name", name)
	}
	return invalid(err)
}

func (h configService) ListMCPServerConfigs(_ context.Context, r req[pb.ListMCPServerConfigsRequest]) (*connect.Response[pb.ListMCPServerConfigsResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	global, _, path, err := config.ScopeMCP(h.s.configDir, "")
	if err != nil {
		return nil, invalid(err)
	}
	out := &pb.ListMCPServerConfigsResponse{Path: path}
	if dir == "" {
		for _, s := range global {
			s.Scope = config.MCPScopeGlobal
			out.Servers = append(out.Servers, mcpConfigMsg(s))
		}
		return ok(out)
	}
	// A workspace's: every server it runs, as the merge has them (the
	// global ones it turned off marked so, those its own stand in for
	// left out), then its project's, which run once the project's
	// settings are trusted.
	cfg, err := config.LoadWorkspace(h.s.configDir, dir)
	if err != nil {
		return nil, invalid(err)
	}
	if _, _, out.Path, err = config.ScopeMCP(h.s.configDir, dir); err != nil {
		return nil, invalid(err)
	}
	for _, s := range cfg.MCP.Servers {
		if s.Scope == config.MCPScopeGlobal || s.Scope == config.MCPScopeWorkspace {
			out.Servers = append(out.Servers, mcpConfigMsg(s))
		}
	}
	if cfg.Project != nil {
		for _, it := range cfg.Project.Pending {
			if it.Kind != config.ProjectMCP {
				continue
			}
			m := &pb.MCPServerConfig{Name: it.Key, Scope: config.MCPScopeProject}
			if strings.HasPrefix(it.Value, "http://") || strings.HasPrefix(it.Value, "https://") {
				m.Url = it.Value
			} else {
				m.Command = it.Value
			}
			out.Servers = append(out.Servers, m)
		}
	}
	return ok(out)
}

func (h configService) SaveMCPServer(ctx context.Context, r req[pb.SaveMCPServerRequest]) (*connect.Response[pb.SaveMCPServerResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s := mcpConfig(r.Msg.GetServer())
	var path string
	if r.Msg.Name == "" {
		path, err = config.AddMCPServer(h.s.configDir, dir, s)
	} else {
		path, err = config.ReplaceMCPServer(h.s.configDir, dir, r.Msg.Name, s)
	}
	if err != nil {
		return nil, mcpEditError(err, r.Msg.Name)
	}
	return ok(&pb.SaveMCPServerResponse{Change: h.changed(ctx, dir, path)})
}

func (h configService) RemoveMCPServer(ctx context.Context, r req[pb.RemoveMCPServerRequest]) (*connect.Response[pb.RemoveMCPServerResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	path, err := config.RemoveMCPServer(h.s.configDir, dir, r.Msg.Name)
	if err != nil {
		return nil, mcpEditError(err, r.Msg.Name)
	}
	return ok(&pb.RemoveMCPServerResponse{Change: h.changed(ctx, dir, path)})
}

// SetMCPServerEnabled turns a server on or off where it's configured: in
// the global settings, or in a workspace's own; a global server asked
// about with a workspace is turned off or on in that workspace alone.
func (h configService) SetMCPServerEnabled(ctx context.Context, r req[pb.SetMCPServerEnabledRequest]) (*connect.Response[pb.SetMCPServerEnabledResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	var path string
	if dir != "" && !h.ownMCP(dir, r.Msg.Name) {
		path, err = config.SetMCPServerOff(h.s.configDir, dir, r.Msg.Name, !r.Msg.Enabled)
	} else {
		path, err = config.SetMCPServerDisabled(h.s.configDir, dir, r.Msg.Name, !r.Msg.Enabled)
	}
	if err != nil {
		return nil, mcpEditError(err, r.Msg.Name)
	}
	return ok(&pb.SetMCPServerEnabledResponse{Change: h.changed(ctx, dir, path)})
}

// MakeMCPServerGlobal moves a workspace's own server to the global
// settings: added there first, so a failure leaves it where it was.
func (h configService) MakeMCPServerGlobal(ctx context.Context, r req[pb.MakeMCPServerGlobalRequest]) (*connect.Response[pb.MakeMCPServerGlobalResponse], error) {
	dir, err := scope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, invalid(errors.New("make global moves a workspace's server: none given"))
	}
	own, _, _, err := config.ScopeMCP(h.s.configDir, dir)
	if err != nil {
		return nil, invalid(err)
	}
	i := slices.IndexFunc(own, func(s config.MCPServerConfig) bool { return s.Name == r.Msg.Name })
	if i < 0 {
		return nil, mcpEditError(fmt.Errorf("%w: %s", config.ErrNoMCPServer, r.Msg.Name), r.Msg.Name)
	}
	gpath, err := config.AddMCPServer(h.s.configDir, "", own[i])
	if err != nil {
		return nil, apiError(connect.CodeAlreadyExists, "MCP_SERVER_EXISTS", err, "name", r.Msg.Name)
	}
	if _, err := config.RemoveMCPServer(h.s.configDir, dir, r.Msg.Name); err != nil {
		return nil, mcpEditError(err, r.Msg.Name)
	}
	return ok(&pb.MakeMCPServerGlobalResponse{Change: h.changed(ctx, "", gpath)})
}

// ownMCP says whether a workspace's own settings configure a server by
// that name.
func (h configService) ownMCP(dir, name string) bool {
	own, _, _, err := config.ScopeMCP(h.s.configDir, dir)
	return err == nil && slices.ContainsFunc(own, func(s config.MCPServerConfig) bool { return s.Name == name })
}

func (h workspaceService) RestartMCPServer(ctx context.Context, r req[pb.RestartMCPServerRequest]) (*connect.Response[pb.RestartMCPServerResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.RestartMCPServer(ctx, r.Msg.Name); err != nil {
		return nil, apiError(connect.CodeNotFound, "MCP_SERVER_NOT_FOUND", err, "name", r.Msg.Name)
	}
	return ok(&pb.RestartMCPServerResponse{})
}
