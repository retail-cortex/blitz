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

	_, err := AddMCPServer("", MCPServerConfig{Name: "docs", URL: "https://mcp.example/docs", Headers: map[string]string{"X-Team": "core"}, Agents: []string{"qa"}})
	require.NoError(t, err)
	_, err = AddMCPServer("", MCPServerConfig{Name: "fs", Command: "other"})
	assert.ErrorContains(t, err, "already configured")

	got := servers(t)
	require.Len(t, got, 3)
	assert.Equal(t, "/tmp", got[0].Env["ROOT"], "the fs server's sub-table survived")
	assert.Equal(t, "https://mcp.example/docs", got[2].URL)
	assert.Equal(t, map[string]string{"X-Team": "core"}, got[2].Headers)
	assert.Equal(t, []string{"qa"}, got[2].Agents)

	_, err = SetMCPServerDisabled("", "db", true)
	require.NoError(t, err)
	assert.True(t, servers(t)[1].Disabled)
	_, err = SetMCPServerDisabled("", "db", false)
	require.NoError(t, err)
	assert.False(t, servers(t)[1].Disabled)

	_, err = RemoveMCPServer("", "fs")
	require.NoError(t, err)
	got = servers(t)
	require.Len(t, got, 2)
	assert.Equal(t, "db", got[0].Name)
	assert.Empty(t, got[0].Env, "the removed server's env stayed behind")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# my settings")
	assert.Contains(t, string(data), "[web]")

	_, err = RemoveMCPServer("", "nope")
	assert.ErrorIs(t, err, ErrNoMCPServer)
	_, err = SetMCPServerDisabled("", "nope", true)
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
	_, err := ReplaceMCPServer("", "fs", MCPServerConfig{
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

	_, err = ReplaceMCPServer("", "db", MCPServerConfig{Name: "files", Command: "x"})
	assert.ErrorContains(t, err, "already configured")
	assert.Equal(t, "db", servers(t)[1].Name, "nothing written")
	_, err = ReplaceMCPServer("", "nope", MCPServerConfig{Name: "nope", Command: "x"})
	assert.ErrorIs(t, err, ErrNoMCPServer)
	_, err = ReplaceMCPServer("", "db", MCPServerConfig{Name: "db"})
	assert.ErrorContains(t, err, "needs a name and either")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# my settings")
	assert.Contains(t, string(data), "[web]\nenabled = true")
}
