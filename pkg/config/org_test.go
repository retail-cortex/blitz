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
	assert.False(t, cfg.Org.Active(), "connected, but no catalog yet")
	assert.Equal(t, []string{ExpandHome(UserAgentsDir)}, cfg.AgentSearchPaths("")[:1])

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
	cmds := cfg.Org.Around("commands", []string{"mine"})
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
	t.Run("a policy file that isn't JSON", func(t *testing.T) {
		writeOrg(t, home, `nope`, nil)
		cfg, err := Load("")
		require.NoError(t, err)
		assert.False(t, cfg.Org.Active())
	})
	t.Run("the organization's folder", func(t *testing.T) {
		assert.Equal(t, "", EnterpriseDir("", ""))
		assert.Equal(t, "", EnterpriseDir("", "::nope"))
		assert.Equal(t, filepath.Join(home, ".blitz", "enterprise", "127.0.0.1_8443"), EnterpriseDir("", "http://127.0.0.1:8443"))
	})
}
