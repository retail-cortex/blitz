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
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shellComplete is what the shell's completion asks for args (cobra's
// __complete), one value per line, without the directive.
func shellComplete(t *testing.T, args ...string) []string {
	t.Helper()
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append([]string{"__complete"}, args...))
	require.NoError(t, root.Execute())
	var vals []string
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l != "" && !strings.HasPrefix(l, ":") {
			v, _, _ := strings.Cut(l, "\t")
			vals = append(vals, v)
		}
	}
	return vals
}

// The shell completes arguments and flags from the same settings the
// commands read (CLP-14).
func TestShellCompletion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cfg, ".env.toml"), []byte("[[mcp.servers]]\nname = \"github\"\nurl = \"https://example.com/mcp\"\n"), 0o600))

	cases := map[string]struct {
		args []string
		want []string
	}{
		"Blitz's commands":   {[]string{"do", ""}, []string{"connector list", "mcp add"}},
		"providers":          {[]string{"config", "set-key", ""}, []string{"gemini", "anthropic", "openai"}},
		"auth kinds":         {[]string{"config", "set-auth", "gemini", ""}, []string{"api_key", "adc", "oauth"}},
		"MCP servers":        {[]string{"-c", cfg, "mcp", "remove", ""}, []string{"github"}},
		"a flag's values":    {[]string{"--permission-mode", ""}, []string{"auto", "plan", "bypass"}},
		"a persistent flag":  {[]string{"--color", ""}, []string{"auto", "always", "never"}},
		"exec's flags too":   {[]string{"exec", "--output-format", ""}, []string{"text", "json", "stream-json"}},
		"completion install": {[]string{"completion", "install", ""}, []string{"zsh", "bash", "fish"}},
		"completion's sub":   {[]string{"completion", "ins"}, []string{"install"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := shellComplete(t, c.args...)
			for _, w := range c.want {
				assert.Contains(t, got, w)
			}
		})
	}
	assert.NotContains(t, shellComplete(t, "--permission-mode", ""), "unsafe", "unsafe is chosen in the session, each time")
}

func TestCompletionInstall(t *testing.T) {
	root := newRootCommand()
	cases := map[string]struct{ script, rc string }{
		"zsh":  {filepath.Join(".blitz", "completion", "blitz.zsh"), ".zshrc"},
		"bash": {filepath.Join(".blitz", "completion", "blitz.bash"), ".bashrc"},
		"fish": {filepath.Join(".config", "fish", "completions", "blitz.fish"), ""},
	}
	for shell, c := range cases {
		t.Run(shell, func(t *testing.T) {
			home := t.TempDir()
			if c.rc != "" {
				require.NoError(t, os.WriteFile(filepath.Join(home, c.rc), []byte("export X=1"), 0o644))
			}
			script, rc, err := installCompletion(root, shell, home)
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(home, c.script), script)
			body, err := os.ReadFile(script)
			require.NoError(t, err)
			assert.Contains(t, string(body), "blitz")
			if c.rc == "" {
				assert.Empty(t, rc)
				return
			}
			assert.Equal(t, filepath.Join(home, c.rc), rc)
			_, rc, err = installCompletion(root, shell, home)
			require.NoError(t, err)
			assert.Empty(t, rc, "the line is added once")
			got, err := os.ReadFile(filepath.Join(home, c.rc))
			require.NoError(t, err)
			assert.Equal(t, 1, strings.Count(string(got), completionMark))
			assert.True(t, strings.HasPrefix(string(got), "export X=1\n"), "what was there stays, on its own line: %q", got)
		})
	}
	t.Run("another shell", func(t *testing.T) {
		_, _, err := installCompletion(root, "tcsh", t.TempDir())
		require.Error(t, err)
		assert.Equal(t, exitUsage, exitCodeFor(err))
	})
}

func TestCompletionInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	assert.False(t, completionInstalled())
	_, _, err := installCompletion(newRootCommand(), "zsh", home)
	require.NoError(t, err)
	assert.True(t, completionInstalled())
	t.Setenv("SHELL", "/bin/tcsh")
	assert.True(t, completionInstalled(), "a shell it can't install for isn't nagged about")
}

// blitz completion install runs from the command line, for $SHELL.
func TestCompletionInstallCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/usr/bin/fish")
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"completion", "install"})
	require.NoError(t, root.Execute())
	assert.Contains(t, out.String(), "fish")
	assert.FileExists(t, filepath.Join(home, ".config", "fish", "completions", "blitz.fish"))
}

func TestShellQuote(t *testing.T) {
	assert.Equal(t, `'/home/a b/x'`, shellQuote("/home/a b/x"))
	assert.Equal(t, `'it'\''s'`, shellQuote("it's"))
}

// blitz help names a command's help; blitz docs reads the docs offline.
func TestHelpAndDocsCommands(t *testing.T) {
	run := func(args ...string) string {
		root := newRootCommand()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		require.NoError(t, root.Execute())
		return out.String()
	}
	assert.Contains(t, run("help", "mcp", "add"), "blitz mcp add <name>")
	assert.Contains(t, run("help"), "Setup and maintenance:")
	assert.Contains(t, run("docs", "mcp"), "MCP servers")
	assert.Contains(t, run("docs", "add", "an", "mcp", "server"), "blitz docs guide/mcp")
	assert.Contains(t, shellComplete(t, "docs", "guide/m"), "guide/mcp")
	assert.Contains(t, shellComplete(t, "help", "mc"), "mcp")
}

func TestErrorText(t *testing.T) {
	notFound := connect.NewError(connect.CodeNotFound, errors.New("no MCP server named github"))
	cases := map[string]struct {
		err   error
		debug bool
		want  string
	}{
		"the service's message":    {notFound, false, "no MCP server named github"},
		"what wraps it stays":      {fmt.Errorf("mcp remove: %w", notFound), false, "mcp remove: no MCP server named github"},
		"gRPC's words go":          {errors.New("rpc error: code = Unavailable desc = the service isn't running"), false, "the service isn't running"},
		"a plain error is itself":  {errors.New("no such file"), false, "no such file"},
		"BLITZ_DEBUG keeps it all": {notFound, true, "not_found: no MCP server named github"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { assert.Equal(t, c.want, errorText(c.err, c.debug)) })
	}
}

// config get, set and reference complete settings keys, not tables.
func TestConfigKeysComplete(t *testing.T) {
	got := shellComplete(t, "config", "set", "ui.th")
	assert.Contains(t, got, "ui.theme")
	assert.NotContains(t, shellComplete(t, "config", "get", ""), "ui", "a table isn't a key")
	assert.Empty(t, shellComplete(t, "config", "set", "ui.theme", ""), "the value is yours")
}
