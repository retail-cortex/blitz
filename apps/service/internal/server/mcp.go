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

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// MCP servers for the desktop app's Settings › MCP servers and the tray
// (spec_mcp_009 MCP-45..MCP-48): the global settings' [[mcp.servers]],
// edited as `blitz mcp` does, and applied to open workspaces at once.

func mcpConfigMsg(s config.MCPServerConfig) *pb.MCPServerConfig {
	return &pb.MCPServerConfig{
		Name: s.Name, Command: s.Command, Args: s.Args, Env: s.Env, Url: s.URL, Headers: s.Headers,
		Disabled: s.Disabled, Tools: s.Tools, AutoApprove: s.AutoApprove, Sandbox: s.Sandbox,
		Prefix: s.Prefix, Agents: s.Agents, TimeoutSeconds: int32(s.TimeoutSeconds),
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

func (h configService) ListMCPServerConfigs(context.Context, req[pb.ListMCPServerConfigsRequest]) (*connect.Response[pb.ListMCPServerConfigsResponse], error) {
	cfg, err := config.Load(h.s.configDir)
	if err != nil {
		return nil, invalid(err)
	}
	path, _, err := config.ReadSettingsFile(h.s.configDir, "")
	if err != nil {
		return nil, invalid(err)
	}
	out := &pb.ListMCPServerConfigsResponse{Path: path}
	for _, s := range cfg.MCP.Servers {
		out.Servers = append(out.Servers, mcpConfigMsg(s))
	}
	return ok(out)
}

func (h configService) SaveMCPServer(ctx context.Context, r req[pb.SaveMCPServerRequest]) (*connect.Response[pb.SaveMCPServerResponse], error) {
	s := mcpConfig(r.Msg.GetServer())
	var path string
	var err error
	if r.Msg.Name == "" {
		path, err = config.AddMCPServer(h.s.configDir, s)
	} else {
		path, err = config.ReplaceMCPServer(h.s.configDir, r.Msg.Name, s)
	}
	if err != nil {
		return nil, mcpEditError(err, r.Msg.Name)
	}
	return ok(&pb.SaveMCPServerResponse{Change: h.changed(ctx, "", path)})
}

func (h configService) RemoveMCPServer(ctx context.Context, r req[pb.RemoveMCPServerRequest]) (*connect.Response[pb.RemoveMCPServerResponse], error) {
	path, err := config.RemoveMCPServer(h.s.configDir, r.Msg.Name)
	if err != nil {
		return nil, mcpEditError(err, r.Msg.Name)
	}
	return ok(&pb.RemoveMCPServerResponse{Change: h.changed(ctx, "", path)})
}

func (h configService) SetMCPServerEnabled(ctx context.Context, r req[pb.SetMCPServerEnabledRequest]) (*connect.Response[pb.SetMCPServerEnabledResponse], error) {
	path, err := config.SetMCPServerDisabled(h.s.configDir, r.Msg.Name, !r.Msg.Enabled)
	if err != nil {
		return nil, mcpEditError(err, r.Msg.Name)
	}
	return ok(&pb.SetMCPServerEnabledResponse{Change: h.changed(ctx, "", path)})
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
