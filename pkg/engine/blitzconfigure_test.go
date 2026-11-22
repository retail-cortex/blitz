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

package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
)

func TestSecretName(t *testing.T) {
	cases := map[[2]string]string{
		{"github", "GITHUB_TOKEN"}:     "GITHUB_TOKEN",
		{"github", "token"}:            "GITHUB_TOKEN",
		{"my-server", "Authorization"}: "MY_SERVER_AUTHORIZATION",
		{"linear", "X-Api-Key"}:        "LINEAR_X_API_KEY",
	}
	for in, want := range cases {
		assert.Equal(t, want, secretName(in[0], in[1]), "%v", in)
	}
}

// Every secret is written as {secrets:NAME}: the agent's ${NAME}, Blitz's
// own {secrets:NAME}, and a literal value of a secret-looking variable,
// which is dropped and asked (DRV-05).
func TestMCPValues(t *testing.T) {
	out, names, dropped := mcpValues("linear", map[string]any{
		"Authorization": "Bearer ${LINEAR_API_KEY}",
		"X-Region":      "eu",
		"API_TOKEN":     "sk-live-1234",
		"Other":         "{secrets:global:SHARED}",
	})
	assert.Equal(t, map[string]string{
		"Authorization": "Bearer {secrets:LINEAR_API_KEY}",
		"X-Region":      "eu",
		"API_TOKEN":     "{secrets:LINEAR_API_TOKEN}",
		"Other":         "{secrets:SHARED}",
	}, out)
	assert.Equal(t, []string{"LINEAR_API_KEY", "LINEAR_API_TOKEN", "SHARED"}, names)
	assert.Equal(t, []string{"API_TOKEN"}, dropped)
	out, _, dropped = mcpValues("x", map[string]any{"Authorization": "Bearer abc"})
	assert.Equal(t, "Bearer {secrets:X_AUTHORIZATION}", out["Authorization"], "the scheme stays, the token goes")
	assert.Equal(t, []string{"Authorization"}, dropped)
	out, names, _ = mcpValues("x", nil)
	assert.Nil(t, out)
	assert.Empty(t, names)
}

// mcp add, from a docs page: every setting in the approval's diff, where
// they came from, the secrets asked hidden after and kept as Blitz
// secrets, the settings naming them (CLP-31).
func TestMCPAddFromAPage(t *testing.T) {
	w, asked, questions := blitzWorkspace(t, api.DecisionOnce, "lin_api_123", nil)
	out, err := runCmd(t, w, "mcp add", map[string]any{
		"name": "linear", "url": "http://127.0.0.1:1/mcp", "source": "https://linear.app/docs/mcp",
		"headers": map[string]any{"Authorization": "Bearer ${LINEAR_API_KEY}"}, "prefix": "lin", "timeout_seconds": float64(30),
	})
	require.NoError(t, err)
	require.Len(t, *asked, 1)
	req := (*asked)[0]
	assert.True(t, req.MustAsk, "it brings code: always asked")
	assert.Contains(t, req.Detail, "connects to http://127.0.0.1:1/mcp")
	assert.Contains(t, req.Detail, "from https://linear.app/docs/mcp")
	assert.Contains(t, req.Detail, "asked for LINEAR_API_KEY")
	for _, want := range []string{"+[[mcp.servers]]", `+name = "linear"`, "{secrets:LINEAR_API_KEY}", `+prefix = "lin"`, "+timeout_seconds = 30"} {
		assert.Contains(t, req.Diff, want)
	}
	assert.NotContains(t, req.Diff, "lin_api_123", "never the value")
	require.Len(t, *questions, 1, "the secret asked, hidden")
	v, ok := config.Vault(w.cfg.Dir).Lookup(config.SecretScope(w.cfg.Dir, w.Dir()), "LINEAR_API_KEY")
	require.True(t, ok)
	assert.Equal(t, "lin_api_123", v)
	m := out.(map[string]any)
	assert.Equal(t, "linear", m["added"])
	assert.Equal(t, []string{"LINEAR_API_KEY"}, m["secrets"])
	assert.Contains(t, fmt.Sprint(m["test"]), "didn't answer", "nothing listens there")
	own, _, path, err := config.ScopeMCP(w.cfg.Dir, w.Dir())
	require.NoError(t, err)
	require.Len(t, own, 1)
	assert.Equal(t, "Bearer {secrets:LINEAR_API_KEY}", own[0].Headers["Authorization"])
	text, _ := os.ReadFile(path)
	assert.NotContains(t, string(text), "lin_api_123")

	// Again: kept secrets aren't asked again, and the name is taken.
	_, err = runCmd(t, w, "mcp add", map[string]any{"name": "linear", "url": "http://127.0.0.1:1/mcp"})
	assert.ErrorContains(t, err, "mcp edit changes it")
}

func TestMCPEditRemoveAndTest(t *testing.T) {
	w, asked, questions := blitzWorkspace(t, api.DecisionOnce, "tok", nil)
	_, err := runCmd(t, w, "mcp edit", map[string]any{"name": "gh", "command": "npx"})
	assert.ErrorContains(t, err, "no MCP server \"gh\" of its own")
	_, err = runCmd(t, w, "mcp add", map[string]any{"name": "gh", "command": "true", "env": map[string]any{"GITHUB_TOKEN": "${GITHUB_TOKEN}"}})
	require.NoError(t, err)
	require.Len(t, *questions, 1)
	_, err = runCmd(t, w, "mcp edit", map[string]any{"name": "gh", "command": "true", "args": []any{"--read-only"}, "env": map[string]any{"GITHUB_TOKEN": "${GITHUB_TOKEN}"}})
	require.NoError(t, err)
	assert.Len(t, *questions, 1, "a kept secret isn't asked again")
	diff := (*asked)[len(*asked)-1].Diff
	assert.Contains(t, diff, `-command = "true"`, "the old entry")
	assert.Contains(t, diff, `+args = ["--read-only"]`, "and the new")
	assert.True(t, (*asked)[len(*asked)-1].MustAsk)

	out, err := runCmd(t, w, "mcp test", map[string]any{"name": "gh"})
	require.NoError(t, err)
	assert.Equal(t, false, out.(map[string]any)["ok"], "true serves no MCP")
	_, err = runCmd(t, w, "mcp test", map[string]any{"name": "nope"})
	assert.ErrorContains(t, err, "no MCP server")

	_, err = runCmd(t, w, "mcp remove", map[string]any{"name": "gh"})
	require.NoError(t, err)
	assert.Contains(t, (*asked)[len(*asked)-1].Diff, `-name = "gh"`)
	own, _, _, err := config.ScopeMCP(w.cfg.Dir, w.Dir())
	require.NoError(t, err)
	assert.Empty(t, own)
	_, err = runCmd(t, w, "mcp remove", map[string]any{"name": "gh"})
	assert.ErrorContains(t, err, "no MCP server")
}

func TestMCPArgsChecks(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	cases := map[string]struct {
		args map[string]any
		want string
	}{
		"a bad name":           {map[string]any{"name": "a b", "url": "https://x"}, "letters, digits"},
		"both":                 {map[string]any{"name": "x", "url": "https://x", "command": "y"}, "either command"},
		"headers on a command": {map[string]any{"name": "x", "command": "y", "headers": map[string]any{"A": "b"}}, "headers are for"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := runCmd(t, w, "mcp add", c.args)
			assert.ErrorContains(t, err, c.want)
		})
	}
	assert.Empty(t, *asked, "nothing asked for what can't be saved")
}

// settings set changes the settings the agent may (CLP-32); not what
// loosens Blitz's limits, where its requests go, or the person's own.
func TestSettingsSetKeys(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	out, err := runCmd(t, w, "settings set", map[string]any{"key": "ui.theme", "value": "dracula"})
	require.NoError(t, err)
	assert.NotEmpty(t, out.(map[string]any)["file"])
	require.Len(t, *asked, 1)
	cfg, err := config.LoadWorkspace(w.cfg.Dir, w.Dir())
	require.NoError(t, err)
	assert.Equal(t, "dracula", cfg.UI.Theme)
	_, err = runCmd(t, w, "settings set", map[string]any{"key": "search.enabled", "value": "true"})
	require.NoError(t, err)
	for _, key := range []string{"blitz.permission_mode", "blitz.agency_level", "llm.openai.base_url", "llm.gemini.project_id", "llm.anthropic.auth", "images.folder", "workers.policy.allow", "profile.name"} {
		t.Run(key, func(t *testing.T) {
			_, err := runCmd(t, w, "settings set", map[string]any{"key": key, "value": "x"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "the person's to change")
		})
	}
	assert.Len(t, *asked, 2, "refused before anything's asked")
}

func TestSettingsReference(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	out, err := runCmd(t, w, "settings reference", nil)
	require.NoError(t, err)
	assert.Contains(t, fmt.Sprint(out), "ui")
	out, err = runCmd(t, w, "settings reference", map[string]any{"key": "ui.banner"})
	require.NoError(t, err)
	items := out.([]map[string]any)
	require.Len(t, items, 1)
	assert.Equal(t, true, items[0]["agent_can_set"])
	assert.Contains(t, items[0]["doc"], "wordmark")
	out, err = runCmd(t, w, "settings reference", map[string]any{"key": "blitz.permission_mode"})
	require.NoError(t, err)
	assert.Equal(t, false, out.([]map[string]any)[0]["agent_can_set"])
	_, err = runCmd(t, w, "settings reference", map[string]any{"key": "nope.nothing"})
	assert.ErrorContains(t, err, "no setting")
	out, err = runCmd(t, w, "settings reference", map[string]any{"key": "ui."})
	require.NoError(t, err)
	assert.Greater(t, len(out.([]map[string]any)), 5)
	for _, it := range out.([]map[string]any) {
		assert.True(t, strings.HasPrefix(it["key"].(string), "ui."))
	}
}

// /configure is a bundled command for the blitz agent (CLP-30).
func TestConfigureCommand(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	for _, c := range w.ListCommands() {
		if c.Name == "configure" {
			assert.Equal(t, "bundled", c.Source)
			return
		}
	}
	t.Fatal("no /configure")
}

// No value typed for a secret: nothing is saved, neither the secret nor
// the server (DRV-05); a name a global server has is refused.
func TestMCPAddWithoutTheSecret(t *testing.T) {
	w, asked, questions := blitzWorkspace(t, api.DecisionOnce, "", nil)
	_, err := runCmd(t, w, "mcp add", map[string]any{"name": "gh", "command": "true", "env": map[string]any{"GITHUB_TOKEN": "${GITHUB_TOKEN}"}})
	require.ErrorContains(t, err, "no value was typed")
	require.Len(t, *asked, 1)
	require.Len(t, *questions, 1)
	own, _, _, err := config.ScopeMCP(w.cfg.Dir, w.Dir())
	require.NoError(t, err)
	assert.Empty(t, own, "the server isn't saved")
	_, ok := config.Vault(w.cfg.Dir).Lookup(config.SecretScope(w.cfg.Dir, w.Dir()), "GITHUB_TOKEN")
	assert.False(t, ok)
}

func TestMCPAddRefusesAGlobalName(t *testing.T) {
	w, asked, _ := blitzWorkspace(t, api.DecisionOnce, "", func(c *config.Config) {
		c.MCP.Servers = []config.MCPServerConfig{{Name: "docs", URL: "http://127.0.0.1:1/mcp", Disabled: true}}
	})
	_, err := runCmd(t, w, "mcp add", map[string]any{"name": "docs", "url": "http://127.0.0.1:1/other"})
	assert.ErrorContains(t, err, "comes from your global settings or the project")
	assert.Empty(t, *asked)
}

// echoIn is the test server's tool's input.
type echoIn struct {
	Text string `json:"text"`
}

// mcp add against a server that answers: saved, its tools listed in what
// the agent gets back, and mcp test says the same (CLP-30, CLP-31).
func TestMCPAddAServerThatAnswers(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "docs", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "search_docs"}, func(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.Text}}}, nil, nil
	})
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	t.Cleanup(ts.Close)
	t.Cleanup(func() {
		for ss := range srv.Sessions() {
			ss.Close()
		}
	})
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "tok", nil)
	out, err := runCmd(t, w, "mcp add", map[string]any{"name": "docs", "url": ts.URL, "headers": map[string]any{"X-Api-Key": "literal-value"}})
	require.NoError(t, err)
	m := out.(map[string]any)
	require.Contains(t, m, "tools", "it answered: %v", m["test"])
	assert.Contains(t, fmt.Sprint(m["tools"]), "search_docs")
	assert.Contains(t, m["note"], "X-Api-Key were asked of the person", "the agent is told its value wasn't used")

	tested, err := runCmd(t, w, "mcp test", map[string]any{"name": "docs"})
	require.NoError(t, err)
	assert.Equal(t, true, tested.(map[string]any)["ok"])
	assert.Contains(t, fmt.Sprint(tested.(map[string]any)["tools"]), "search_docs")
}

// Declined, mcp remove keeps the server.
func TestMCPRemoveDeclined(t *testing.T) {
	w, _, _ := blitzWorkspace(t, api.DecisionOnce, "", nil)
	_, err := runCmd(t, w, "mcp add", map[string]any{"name": "gh", "command": "true"})
	require.NoError(t, err)
	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionDeny, nil }, nil)
	_, err = runCmd(t, w, "mcp remove", map[string]any{"name": "gh"})
	require.Error(t, err)
	own, _, _, err := config.ScopeMCP(w.cfg.Dir, w.Dir())
	require.NoError(t, err)
	assert.Len(t, own, 1, "still there")
}
