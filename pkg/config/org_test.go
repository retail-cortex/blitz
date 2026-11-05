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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeOrg lays out an organization's catalog as the Blitz service does,
// in home's settings, for the service at https://blitz.acme.com.
func writeOrg(t *testing.T, home, policy string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(home, ".blitz", "enterprise", "blitz.acme.com")
	for p, data := range files {
		full := filepath.Join(dir, "catalog", filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o700))
		require.NoError(t, os.WriteFile(full, []byte(data), 0o600))
	}
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "policy.json"), []byte(policy), 0o600))
	return dir
}

// The organization's catalog shapes the settings as they load: its layers
// around the person's and the workspace's folders, its tools denied, its
// MCP servers merged by lock (the person's own dropped unless allowed),
// and its expiry (spec_053_enterprise EN-31, EN-33, EN-35).
func TestOrgLayer(t *testing.T) {
	home, _ := keysEnv(t)
	_, err := SetEnterpriseURL("", "https://blitz.acme.com")
	require.NoError(t, err)
	cfg, err := Load("")
	require.NoError(t, err)
	assert.True(t, cfg.Org.Active(), "connected, but no policy yet: the strictest applies (EN-33)")
	assert.Equal(t, "not fetched yet", cfg.Org.Problem)
	assert.True(t, cfg.Org.Expired(time.Now()), "the organization's work waits")
	require.Len(t, cfg.Warnings, 1)
	assert.Contains(t, cfg.Warnings[0], "policy.json) can't be read (not fetched yet)")

	f, err := os.OpenFile(filepath.Join(home, ".blitz", ".env.toml"), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("\n[[mcp.servers]]\nname = \"github\"\ncommand = \"mine\"\n\n[[mcp.servers]]\nname = \"notes\"\ncommand = \"notes\"\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	expires := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	dir := writeOrg(t, home, `{"policy":{"tools":{"off":["web_fetch"]},"mcp":{"own":false}},"expires":"2026-11-09T00:00:00Z"}`, map[string]string{
		"locked/mcp/github.toml": "command = \"theirs\"\n",
		"default/mcp/jira.toml":  "command = \"jira\"\n",
	})
	cfg, err = Load("")
	require.NoError(t, err)
	require.True(t, cfg.Org.Active())
	assert.Equal(t, expires, cfg.Org.Expires.UTC())
	assert.False(t, cfg.Org.Expired(expires.Add(-time.Hour)))
	assert.True(t, cfg.Org.Expired(expires.Add(time.Hour)))
	assert.Contains(t, cfg.Permissions.Deny, "web_fetch")

	ws := t.TempDir()
	agents := cfg.AgentSearchPaths(ws)
	assert.Equal(t, filepath.Join(dir, "catalog", "default", "agents"), agents[0], "defaults first: the person's and the workspace's win")
	assert.Equal(t, filepath.Join(dir, "catalog", "locked", "agents"), agents[len(agents)-1], "locked last: it wins")
	skills := cfg.SkillSearchPaths(ws)
	assert.Equal(t, filepath.Join(dir, "catalog", "locked", "skills"), skills[len(skills)-1])
	rules := cfg.Org.LockedFirst([]string{"ws"}, "rules", "guardrails")
	assert.Equal(t, []string{filepath.Join(dir, "catalog", "locked", "rules"), filepath.Join(dir, "catalog", "locked", "guardrails"), "ws",
		filepath.Join(dir, "catalog", "default", "rules"), filepath.Join(dir, "catalog", "default", "guardrails")}, rules)
	assert.Equal(t, "locked", cfg.Org.LockOf(filepath.Join(dir, "catalog", "locked", "agents", "a.md")))
	assert.Equal(t, "default", cfg.Org.LockOf(filepath.Join(dir, "catalog", "default", "agents", "a.md")))
	assert.Equal(t, "", cfg.Org.LockOf(filepath.Join(home, ".blitz", "agents", "a.md")))
	assert.Equal(t, "", OrgLayer{}.LockOf("/x"))
	cmds := cfg.Org.around("commands", []string{"mine"})
	assert.Equal(t, "mine", cmds[1])

	names := map[string]string{}
	for _, s := range cfg.MCP.Servers {
		names[s.Name] = s.Command
	}
	assert.Equal(t, map[string]string{"github": "theirs", "jira": "jira"}, names, "the person's own dropped; the locked one; the default one")

	t.Run("the person's own allowed", func(t *testing.T) {
		writeOrg(t, home, `{"policy":{"mcp":{"own":true}}}`, nil)
		cfg, err := Load("")
		require.NoError(t, err)
		names := map[string]string{}
		for _, s := range cfg.MCP.Servers {
			names[s.Name] = s.Command
		}
		assert.Equal(t, "theirs", names["github"], "locked wins over theirs")
		assert.Equal(t, "notes", names["notes"])
		assert.Equal(t, "jira", names["jira"])
		assert.False(t, cfg.Org.Expired(time.Now()), "no expiry")
	})
	t.Run("a locked MCP server that doesn't decode", func(t *testing.T) {
		writeOrg(t, home, `{"policy":{"mcp":{"own":true}}}`, map[string]string{"locked/mcp/broken.toml": "command = \"x\"\nnope\n"})
		cfg, err := Load("")
		require.NoError(t, err)
		require.Len(t, cfg.Warnings, 1, "said, not dropped silently (N26 api-client-config#16)")
		assert.Contains(t, cfg.Warnings[0], "MCP server")
		assert.Contains(t, cfg.Warnings[0], "broken.toml isn't used")
		require.NoError(t, os.Remove(filepath.Join(home, ".blitz", "enterprise", "blitz.acme.com", "catalog", "locked", "mcp", "broken.toml")))
	})
	t.Run("a policy file that isn't JSON", func(t *testing.T) {
		writeOrg(t, home, `{"policy":{"mcp":{"own":true},"tools":{"shell_commands":true}}`, nil)
		cfg, err := Load("")
		require.NoError(t, err)
		require.True(t, cfg.Org.Active(), "never without the organization's rules (EN-33)")
		assert.NotEmpty(t, cfg.Org.Problem)
		assert.True(t, cfg.Org.Expired(time.Now()))
		assert.Equal(t, OrgPolicy{}, cfg.Org.Policy, "the strictest policy, not the damaged file's")
		assert.Equal(t, cfg.Org, OrgLayerFor(""), "the connectors see it too")
		names := map[string]string{}
		for _, s := range cfg.MCP.Servers {
			names[s.Name] = s.Command
		}
		assert.NotContains(t, names, "notes", "the person's own servers are dropped")
		assert.Len(t, cfg.Warnings, 1)
	})
	t.Run("the person's own plugins", func(t *testing.T) {
		for _, tc := range []struct {
			name, policy string
			off          bool
		}{
			{"the policy doesn't say: allowed, as from an older service", `{"policy":{"mcp":{"own":true}}}`, false},
			{"own allowed", `{"policy":{"plugins":{"own":true}}}`, false},
			{"own off", `{"policy":{"plugins":{"own":false}}}`, true},
			{"a policy that can't be read", `{"policy":`, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				writeOrg(t, home, tc.policy, nil)
				cfg, err := Load("")
				require.NoError(t, err)
				assert.Equal(t, tc.off, cfg.Org.PluginsOff())
			})
		}
		assert.False(t, OrgLayer{}.PluginsOff(), "no organization")
	})
	t.Run("the organization's folder", func(t *testing.T) {
		assert.Equal(t, "", EnterpriseDir("", ""))
		assert.Equal(t, "", EnterpriseDir("", "::nope"))
		assert.Equal(t, filepath.Join(home, ".blitz", "enterprise", "127.0.0.1_8443"), EnterpriseDir("", "http://127.0.0.1:8443"))
	})
}

// MCP servers that come after the settings load (a trusted project's, a
// plugin's) go through one check of the organization's policy: with
// mcp.own false, none is added (EN-35; N26 api-client-config#1).
func TestAddMCPServer(t *testing.T) {
	org := func(own bool) OrgLayer {
		o := OrgLayer{Defaults: "/org/default", Locked: "/org/locked"}
		o.Policy.MCP.Own = own
		return o
	}
	cases := []struct {
		name    string
		org     OrgLayer
		have    []MCPServerConfig
		wantErr error
	}{
		{name: "no organization", org: OrgLayer{}},
		{name: "the organization allows the person's own", org: org(true)},
		{name: "the organization doesn't", org: org(false), wantErr: ErrOrgMCP},
		{name: "a server of the name is there", have: []MCPServerConfig{{Name: "db", Command: "mine"}}, wantErr: ErrMCPNameTaken},
		{name: "a policy that can't be read", org: OrgLayer{Defaults: "/org/default", Problem: "not fetched yet"}, wantErr: ErrOrgMCP},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Org, cfg.MCP.Servers = tc.org, tc.have
			err := cfg.AddMCPServer(MCPServerConfig{Name: "db", Command: "npx", Args: []string{"@evil/mcp"}})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.Len(t, cfg.MCP.Servers, len(tc.have), "nothing added")
				return
			}
			require.NoError(t, err)
			require.Len(t, cfg.MCP.Servers, 1)
			assert.Equal(t, "npx", cfg.MCP.Servers[0].Command)
		})
	}

	t.Run("a trusted project's", func(t *testing.T) {
		ws := t.TempDir()
		writeProject(t, ws, map[string]string{".mcp.json": `{"mcpServers": {"db": {"command": "npx", "args": ["@evil/mcp"]}}}`})
		p := LoadProject(ws)
		cfg := DefaultConfig()
		cfg.Org = org(false)
		p.ApplyTrusted(cfg)
		assert.Empty(t, cfg.MCP.Servers, "the organization's policy holds after trust")
		assert.Equal(t, []ProjectItem{{File: ".mcp.json", Kind: ProjectMCP, Key: "db", Reason: ReasonOrgPolicy}}, p.Ignored)
	})
}
