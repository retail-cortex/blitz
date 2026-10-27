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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mcpDoc = `# my settings
[llm]
provider = "gemini"

[[mcp.servers]]
name = "fs"          # the filesystem server
command = "npx"
args = ["@acme/fs"]

[mcp.servers.env]
ROOT = "/tmp"

[[mcp.servers]]
name = "db"
command = "db-mcp"

[web]
enabled = true
`

func servers(t *testing.T) []MCPServerConfig {
	t.Helper()
	cfg, err := Load("")
	require.NoError(t, err)
	return cfg.MCP.Servers
}

func TestEditMCPServers(t *testing.T) {
	home := isolateConfigEnv(t)
	path := filepath.Join(home, ".blitz", ".env.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(mcpDoc), 0o600))

	_, err := AddMCPServer("", "", MCPServerConfig{Name: "docs", URL: "https://mcp.example/docs", Headers: map[string]string{"X-Team": "core"}, Agents: []string{"qa"}})
	require.NoError(t, err)
	_, err = AddMCPServer("", "", MCPServerConfig{Name: "fs", Command: "other"})
	assert.ErrorContains(t, err, "already configured")

	got := servers(t)
	require.Len(t, got, 3)
	assert.Equal(t, "/tmp", got[0].Env["ROOT"], "the fs server's sub-table survived")
	assert.Equal(t, "https://mcp.example/docs", got[2].URL)
	assert.Equal(t, map[string]string{"X-Team": "core"}, got[2].Headers)
	assert.Equal(t, []string{"qa"}, got[2].Agents)

	_, err = SetMCPServerDisabled("", "", "db", true)
	require.NoError(t, err)
	assert.True(t, servers(t)[1].Disabled)
	_, err = SetMCPServerDisabled("", "", "db", false)
	require.NoError(t, err)
	assert.False(t, servers(t)[1].Disabled)

	_, err = RemoveMCPServer("", "", "fs")
	require.NoError(t, err)
	got = servers(t)
	require.Len(t, got, 2)
	assert.Equal(t, "db", got[0].Name)
	assert.Empty(t, got[0].Env, "the removed server's env stayed behind")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# my settings")
	assert.Contains(t, string(data), "[web]")

	_, err = RemoveMCPServer("", "", "nope")
	assert.ErrorIs(t, err, ErrNoMCPServer)
	_, err = SetMCPServerDisabled("", "", "nope", true)
	assert.ErrorIs(t, err, ErrNoMCPServer)
}

// Replacing an entry keeps its place and the rest of the file; it may
// rename the server, but not onto another's name.
func TestReplaceMCPServer(t *testing.T) {
	home := isolateConfigEnv(t)
	path := filepath.Join(home, ".blitz", ".env.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(mcpDoc), 0o600))

	off := false
	_, err := ReplaceMCPServer("", "", "fs", MCPServerConfig{
		Name: "files", Command: "npx", Args: []string{"@acme/fs", "--ro"}, Env: map[string]string{"ROOT": "/srv"},
		Tools: []string{"read"}, AutoApprove: true, Sandbox: &off, TimeoutSeconds: 60, Disabled: true,
	})
	require.NoError(t, err)
	got := servers(t)
	require.Len(t, got, 2)
	assert.Equal(t, "files", got[0].Name, "kept its place")
	assert.Equal(t, []string{"@acme/fs", "--ro"}, got[0].Args)
	assert.Equal(t, map[string]string{"ROOT": "/srv"}, got[0].Env, "the old sub-table went with the entry")
	assert.Equal(t, []string{"read"}, got[0].Tools)
	assert.True(t, got[0].AutoApprove)
	require.NotNil(t, got[0].Sandbox)
	assert.False(t, *got[0].Sandbox)
	assert.Equal(t, 60, got[0].TimeoutSeconds)
	assert.True(t, got[0].Disabled)
	assert.Equal(t, "db", got[1].Name)

	_, err = ReplaceMCPServer("", "", "db", MCPServerConfig{Name: "files", Command: "x"})
	assert.ErrorContains(t, err, "already configured")
	assert.Equal(t, "db", servers(t)[1].Name, "nothing written")
	_, err = ReplaceMCPServer("", "", "nope", MCPServerConfig{Name: "nope", Command: "x"})
	assert.ErrorIs(t, err, ErrNoMCPServer)
	_, err = ReplaceMCPServer("", "", "db", MCPServerConfig{Name: "db"})
	assert.ErrorContains(t, err, "needs a name and either")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# my settings")
	assert.Contains(t, string(data), "[web]\nenabled = true")
}

// A workspace's own servers add to the global ones (one with a global
// one's name stands in for it there), a global one turned off there is
// disabled there alone, and every edit lands in the workspace's file.
func TestWorkspaceMCPServers(t *testing.T) {
	home := isolateConfigEnv(t)
	path := filepath.Join(home, ".blitz", ".env.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(mcpDoc), 0o600))
	ws := t.TempDir()
	other := t.TempDir()

	wpath, err := AddMCPServer("", ws, MCPServerConfig{Name: "jira", URL: "https://mcp.example/jira"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(WorkspaceSettingsDir("", ws), ".env.toml"), wpath)
	_, err = AddMCPServer("", ws, MCPServerConfig{Name: "db", Command: "local-db-mcp"})
	require.NoError(t, err, "a workspace may stand in for a global server")
	_, err = SetMCPServerOff("", ws, "fs", true)
	require.NoError(t, err)

	load := func(dir string) map[string]MCPServerConfig {
		cfg, err := LoadWorkspace("", dir)
		require.NoError(t, err)
		out := map[string]MCPServerConfig{}
		for _, s := range cfg.MCP.Servers {
			out[s.Name] = s
		}
		return out
	}
	got := load(ws)
	require.Len(t, got, 3)
	assert.Equal(t, MCPScopeGlobal, got["fs"].Scope)
	assert.True(t, got["fs"].Disabled && got["fs"].OffHere, "off in this workspace")
	assert.Equal(t, MCPScopeWorkspace, got["db"].Scope)
	assert.Equal(t, "local-db-mcp", got["db"].Command, "the workspace's stands in")
	assert.Equal(t, MCPScopeWorkspace, got["jira"].Scope)

	theirs := load(other)
	require.Len(t, theirs, 2, "other workspaces see the global ones alone")
	assert.False(t, theirs["fs"].Disabled)
	assert.Equal(t, "db-mcp", theirs["db"].Command)
	for _, s := range servers(t) {
		assert.Equal(t, MCPScopeGlobal, s.Scope)
		assert.False(t, s.Disabled, "the global settings are unchanged: %s", s.Name)
	}

	own, off, _, err := ScopeMCP("", ws)
	require.NoError(t, err)
	assert.Len(t, own, 2)
	assert.Equal(t, []string{"fs"}, off)

	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{"on again", func() error { _, err := SetMCPServerOff("", ws, "fs", false); return err }, nil},
		{"off for a name the global settings lack", func() error { _, err := SetMCPServerOff("", ws, "jira", true); return err }, ErrNoMCPServer},
		{"off needs a workspace", func() error { _, err := SetMCPServerOff("", "", "fs", true); return err }, nil},
		{"remove the workspace's own", func() error { _, err := RemoveMCPServer("", ws, "jira"); return err }, nil},
		{"remove a global one from a workspace", func() error { _, err := RemoveMCPServer("", ws, "fs"); return err }, ErrNoMCPServer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			switch {
			case tc.name == "off needs a workspace":
				assert.Error(t, err)
			case tc.want != nil:
				assert.ErrorIs(t, err, tc.want)
			default:
				assert.NoError(t, err)
			}
		})
	}
	got = load(ws)
	assert.False(t, got["fs"].Disabled, "on again")
	assert.NotContains(t, got, "jira")
}

// SetMCPServerEnabled sets a workspace's own server's disabled, or a
// global one's off entry there; MakeMCPServerGlobal moves one, refusing a
// name the global settings have.
func TestMCPServerEnabledAndMakeGlobal(t *testing.T) {
	home := isolateConfigEnv(t)
	path := filepath.Join(home, ".blitz", ".env.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(mcpDoc), 0o600))
	ws := t.TempDir()
	_, err := AddMCPServer("", ws, MCPServerConfig{Name: "jira", URL: "https://mcp.example/jira"})
	require.NoError(t, err)
	_, err = AddMCPServer("", ws, MCPServerConfig{Name: "fs", Command: "local-fs"})
	require.NoError(t, err)

	tests := []struct {
		name, server string
		on           bool
		wantOff      []string
		wantOwnOff   bool
	}{
		{name: "a global one, off here", server: "db", wantOff: []string{"db"}},
		{name: "its own, disabled in its file", server: "jira", wantOff: []string{"db"}, wantOwnOff: true},
		{name: "the global one on again", server: "db", on: true, wantOff: []string{}, wantOwnOff: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SetMCPServerEnabled("", ws, tc.server, tc.on)
			require.NoError(t, err)
			own, off, _, err := ScopeMCP("", ws)
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.wantOff, off)
			for _, s := range own {
				if s.Name == "jira" {
					assert.Equal(t, tc.wantOwnOff, s.Disabled)
				}
			}
		})
	}

	_, err = MakeMCPServerGlobal("", ws, "fs")
	assert.ErrorIs(t, err, ErrMCPServerExists, "the global settings have an fs")
	_, err = MakeMCPServerGlobal("", ws, "nope")
	assert.ErrorIs(t, err, ErrNoMCPServer)
	_, err = MakeMCPServerGlobal("", "", "jira")
	assert.Error(t, err)
	gpath, err := MakeMCPServerGlobal("", ws, "jira")
	require.NoError(t, err)
	assert.Equal(t, path, gpath)
	own, _, _, err := ScopeMCP("", ws)
	require.NoError(t, err)
	assert.Len(t, own, 1, "moved out of the workspace")
	assert.Contains(t, func() []string {
		var n []string
		for _, s := range servers(t) {
			n = append(n, s.Name)
		}
		return n
	}(), "jira")
}
