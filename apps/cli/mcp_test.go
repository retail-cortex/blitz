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

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/mcpauth"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestMCPCommand(t *testing.T) {
	isolate(t)
	steps := []struct {
		args []string
		says []string
		err  string
	}{
		{args: []string{"mcp", "list"}, says: []string{"No MCP servers"}},
		{args: []string{"mcp", "add", "fs", "npx", "@acme/fs", "--env", "ROOT=/tmp/work"}, says: []string{`Added MCP server "fs"`}},
		{args: []string{"mcp", "add", "docs", "https://mcp.example/docs", "--header", "Authorization: Bearer abcdef123"}, says: []string{`Added MCP server "docs"`}},
		{args: []string{"mcp", "add", "fs", "other"}, err: "already configured"},
		{args: []string{"mcp", "add", "x", "cmd", "--header", "A:b"}, err: "--header is for http servers"},
		{args: []string{"mcp", "add", "x", "cmd", "--env", "novalue"}, err: "--env"},
		{args: []string{"mcp", "disable", "fs"}, says: []string{`Disabled MCP server "fs"`}},
		{args: []string{"mcp", "list"}, says: []string{"fs (disabled)\tnpx @acme/fs", "docs\thttps://mcp.example/docs"}},
		{args: []string{"mcp", "get", "docs"}, says: []string{"url:      https://mcp.example/docs", "header:   Authorization: Be****23"}},
		{args: []string{"mcp", "get", "fs"}, says: []string{"enabled:  false", "env:      ROOT=/t****rk"}},
		{args: []string{"mcp", "enable", "fs"}, says: []string{`Enabled MCP server "fs"`}},
		{args: []string{"mcp", "remove", "docs"}, says: []string{`Removed MCP server "docs"`}},
		{args: []string{"mcp", "remove", "docs"}, err: "no MCP server"},
		{args: []string{"mcp", "list"}, says: []string{"fs\tnpx @acme/fs"}},
	}
	for _, s := range steps {
		out, err := runCLI(t, s.args...)
		if s.err != "" {
			require.Error(t, err, "%v: %s", s.args, out)
			assert.Contains(t, err.Error()+out, s.err, "%v", s.args)
			continue
		}
		require.NoError(t, err, "%v: %s", s.args, out)
		for _, want := range s.says {
			assert.Contains(t, out, want, "%v", s.args)
		}
	}
}

// blitz mcp: a server's prefix, agents and short secrets, signing out, and
// what can't be signed in to.
func TestMCPCommandDetails(t *testing.T) {
	isolate(t)
	store := &secrets.Memory{}
	secrets.SetDefault(store)
	steps := []struct {
		args []string
		says []string
		err  string
	}{
		{args: []string{"mcp", "add", "x", "https://mcp.example/x", "--header", "nocolon"}, err: "--header"},
		{args: []string{"mcp", "add", "tools", "srv", "--prefix", "t", "--agents", "qa,helios", "--env", "K=ab"}, says: []string{`Added MCP server "tools"`}},
		{args: []string{"mcp", "get", "tools"}, says: []string{"prefix:   t", "agents:   qa, helios", "env:      K=****"}},
		{args: []string{"mcp", "get", "ghost"}, err: "no MCP server"},
		{args: []string{"mcp", "login", "ghost"}, err: "no MCP server"},
		{args: []string{"mcp", "login", "tools"}, err: "only http servers sign in"},
		{args: []string{"mcp", "logout", "tools"}, says: []string{"Signed out of tools."}},
	}
	for _, s := range steps {
		out, err := runCLIWithInput(t, "", s.args...)
		if s.err != "" {
			require.Error(t, err, "%v: %s", s.args, out)
			assert.Contains(t, err.Error()+out, s.err, "%v", s.args)
			continue
		}
		require.NoError(t, err, "%v: %s", s.args, out)
		for _, want := range s.says {
			assert.Contains(t, out, want, "%v", s.args)
		}
	}

	// A stored sign-in is listed.
	require.NoError(t, mcpauth.Save(store, "docs", &mcpauth.Record{Token: &oauth2.Token{AccessToken: "a"}}))
	_, err := runCLI(t, "mcp", "add", "docs", "https://mcp.example/docs")
	require.NoError(t, err)
	out, err := runCLI(t, "mcp", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "docs (signed in)\thttps://mcp.example/docs")
}

// Signing in to a server that needs no OAuth says so.
func TestMCPLoginWithoutOAuth(t *testing.T) {
	isolate(t)
	secrets.SetDefault(&secrets.Memory{})
	_, err := runCLI(t, "mcp", "add", "live", mcpTestServer(t, "echo"))
	require.NoError(t, err)
	_, err = runCLIWithInput(t, "", "mcp", "login", "live")
	assert.ErrorContains(t, err, "it needs no OAuth")
}

// Settings that don't parse fail every mcp command that reads them.
func TestMCPBrokenSettings(t *testing.T) {
	home := isolate(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[mcp\n"), 0o600))
	for _, args := range [][]string{{"mcp", "list"}, {"mcp", "get", "x"}, {"mcp", "remove", "x"}} {
		_, err := runCLI(t, args...)
		assert.Error(t, err, "%v", args)
		assert.NotEqual(t, exitUsage, exitCodeFor(err), "%v: not a usage error", args)
	}
}

// blitz mcp --workspace: a workspace's own servers, beside the global
// ones; a global one turned off there alone; make-global moves one.
func TestMCPCommandWorkspace(t *testing.T) {
	isolate(t)
	ws := t.TempDir()
	steps := []struct {
		args []string
		says []string
		not  []string
		err  string
	}{
		{args: []string{"mcp", "add", "github", "npx", "@acme/github"}, says: []string{`Added MCP server "github"`}},
		{args: []string{"--dir", ws, "mcp", "add", "-w", "figma", "https://mcp.example/figma"}, says: []string{`Added MCP server "figma"`, "workspaces"}},
		{args: []string{"--dir", ws, "mcp", "list", "--workspace"}, says: []string{"github\tnpx @acme/github\tglobal", "figma\thttps://mcp.example/figma\tworkspace"}},
		{args: []string{"mcp", "list"}, says: []string{"github\tnpx @acme/github"}, not: []string{"figma"}},
		{args: []string{"--dir", ws, "mcp", "disable", "-w", "github"}, says: []string{`Disabled MCP server "github"`}},
		{args: []string{"--dir", ws, "mcp", "list", "-w"}, says: []string{"github (off here)"}},
		{args: []string{"--dir", ws, "mcp", "get", "-w", "github"}, says: []string{"scope:    global", "off in this workspace"}},
		{args: []string{"mcp", "list"}, says: []string{"github\tnpx"}, not: []string{"disabled", "off here"}},
		{args: []string{"--dir", ws, "mcp", "enable", "-w", "github"}, says: []string{`Enabled MCP server "github"`}},
		{args: []string{"--dir", ws, "mcp", "disable", "-w", "figma"}, says: []string{`Disabled MCP server "figma"`}},
		{args: []string{"--dir", ws, "mcp", "list", "-w"}, says: []string{"figma (disabled)", "github\tnpx"}},
		{args: []string{"--dir", ws, "mcp", "remove", "-w", "github"}, err: "no MCP server"},
		{args: []string{"--dir", ws, "mcp", "make-global", "github"}, err: "no MCP server"},
		{args: []string{"--dir", ws, "mcp", "make-global", "figma"}, says: []string{`Moved MCP server "figma"`, "every workspace"}},
		{args: []string{"mcp", "list"}, says: []string{"figma (disabled)\thttps://mcp.example/figma"}},
		{args: []string{"--dir", ws, "mcp", "list", "-w"}, says: []string{"figma (disabled)\thttps://mcp.example/figma\tglobal"}},
	}
	for _, s := range steps {
		out, err := runCLI(t, s.args...)
		if s.err != "" {
			require.Error(t, err, "%v: %s", s.args, out)
			assert.Contains(t, err.Error()+out, s.err, "%v", s.args)
			continue
		}
		require.NoError(t, err, "%v: %s", s.args, out)
		for _, want := range s.says {
			assert.Contains(t, out, want, "%v", s.args)
		}
		for _, no := range s.not {
			assert.NotContains(t, out, no, "%v", s.args)
		}
	}
}
