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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/config"
)

// runBlitz runs the CLI with args, its output and error.
func runBlitz(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// blitz config get, set, check and reference (CLP-40), on settings of the
// test's own (-c: never the running service's).
func TestConfigValues(t *testing.T) {
	cfg, ws := t.TempDir(), t.TempDir()
	out, err := runBlitz(t, "-c", cfg, "config", "set", "ui.theme", "dracula")
	require.NoError(t, err, out)
	assert.Contains(t, out, "ui.theme = dracula")
	out, err = runBlitz(t, "-c", cfg, "config", "get", "ui.theme")
	require.NoError(t, err)
	assert.Equal(t, "dracula\n", out)

	out, err = runBlitz(t, "-c", cfg, "-d", ws, "config", "set", "-w", "ui.banner", "compact")
	require.NoError(t, err, out)
	wcfg, err := config.LoadWorkspace(cfg, ws)
	require.NoError(t, err)
	assert.Equal(t, "compact", wcfg.UI.Banner, "this workspace's own")
	out, err = runBlitz(t, "-c", cfg, "-d", ws, "config", "get", "ui.banner")
	require.NoError(t, err)
	assert.Equal(t, "compact\n", out)

	out, err = runBlitz(t, "-c", cfg, "config", "get", "ui", "--json")
	require.NoError(t, err)
	var ui map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &ui))
	assert.Equal(t, "dracula", ui["theme"])

	_, err = runBlitz(t, "-c", cfg, "config", "get", "nope.nothing")
	assert.Error(t, err)
	assert.Equal(t, exitUsage, exitCodeFor(err))
	_, err = runBlitz(t, "-c", cfg, "config", "set", "sandbox.allow_network", "true")
	require.Error(t, err, "not one config set sets: edit the file")
	assert.Contains(t, err.Error(), "edit the file")

	out, err = runBlitz(t, "-c", cfg, "config", "reference", "ui.banner")
	require.NoError(t, err)
	assert.Contains(t, out, "ui.banner  string")
	assert.Contains(t, out, "(config set)")
	assert.Contains(t, out, "wordmark")
}

func TestConfigCheck(t *testing.T) {
	cfg := t.TempDir()
	path := filepath.Join(cfg, ".env.toml")
	require.NoError(t, os.WriteFile(path, []byte("[ui]\ntheme = \"dark\"\n"), 0o600))
	out, err := runBlitz(t, "-c", cfg, "config", "check")
	require.NoError(t, err, out)
	assert.Contains(t, out, "is fine")
	require.NoError(t, os.WriteFile(path, []byte("[ui\ntheme = 1\n"), 0o600))
	out, err = runBlitz(t, "-c", cfg, "config", "check")
	require.Error(t, err)
	assert.Contains(t, out, path+":")
	assert.Contains(t, out, "error")
	assert.Equal(t, exitFailure, exitCodeFor(err))
}

func TestConfigEdit(t *testing.T) {
	cfg := t.TempDir()
	path := filepath.Join(cfg, ".env.toml")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	_, err := runBlitz(t, "-c", cfg, "config", "edit")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "$EDITOR")
	// An "editor" that writes a good file: checked, fine.
	t.Setenv("EDITOR", `sh -c 'printf "[ui]\ntheme = \"light\"\n" > "$0"'`)
	out, err := runBlitz(t, "-c", cfg, "config", "edit")
	require.NoError(t, err, out)
	assert.Contains(t, out, "is fine")
	text, _ := os.ReadFile(path)
	assert.Contains(t, string(text), "light")
	// One that writes a broken file: not a terminal, so no second go.
	t.Setenv("EDITOR", `sh -c 'printf "[ui\n" > "$0"'`)
	_, err = runBlitz(t, "-c", cfg, "config", "edit")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "problems")
}

func TestSettingValue(t *testing.T) {
	c := config.DefaultConfig()
	c.UI.Theme = "pink"
	v, ok, err := settingValue(*c, "ui.theme")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "pink", v)
	_, ok, _ = settingValue(*c, "ui.theme.deeper")
	assert.False(t, ok)
	v, ok, _ = settingValue(*c, "ui")
	assert.True(t, ok)
	assert.IsType(t, map[string]any{}, v)
}

// blitz runs is the background runs; the old names work for a release,
// saying the new one (CLP-50, CLP-51).
func TestRunsAndOldNames(t *testing.T) {
	out, err := runBlitz(t, "runs", "--help")
	require.NoError(t, err)
	for _, sub := range []string{"list", "logs", "stop", "attach"} {
		assert.Contains(t, out, sub)
	}
	out, _ = runBlitz(t, "logs")
	assert.Contains(t, out, "use blitz runs logs")
	out, err = runBlitz(t, "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "runs")
	assert.NotContains(t, out, "\n  logs ", "hidden")
	out, err = runBlitz(t, "-w", "x", "--version")
	require.NoError(t, err)
	assert.Contains(t, out, "-w will mean --workspace")
}

// Lists print JSON for scripts; MCP servers' JSON carries no env or header
// values (CLP-53).
func TestListsAsJSON(t *testing.T) {
	cfg := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cfg, ".env.toml"), []byte("[[mcp.servers]]\nname = \"gh\"\ncommand = \"npx\"\nenv = { GITHUB_TOKEN = \"ghp_secret\" }\n"), 0o600))
	out, err := runBlitz(t, "-c", cfg, "mcp", "list", "--json")
	require.NoError(t, err)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &rows))
	require.Len(t, rows, 1)
	assert.Equal(t, "gh", rows[0]["name"])
	assert.NotContains(t, out, "ghp_secret")
}

func TestConfigReferenceAndValues(t *testing.T) {
	cfg := t.TempDir()
	out, err := runBlitz(t, "-c", cfg, "config", "reference", "ui", "--json")
	require.NoError(t, err)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &rows))
	assert.Greater(t, len(rows), 5)
	out, err = runBlitz(t, "-c", cfg, "config", "reference")
	require.NoError(t, err)
	assert.Contains(t, out, "ui.theme")
	_, err = runBlitz(t, "-c", cfg, "config", "reference", "nope")
	assert.Error(t, err)
	out, err = runBlitz(t, "-c", cfg, "config", "get", "search.sources")
	require.NoError(t, err)
	assert.Equal(t, strings.Join(config.DefaultConfig().Search.Sources, ", ")+"\n", out, "a list, comma-joined")
	out, err = runBlitz(t, "-c", cfg, "config", "get", "ui")
	require.NoError(t, err)
	assert.Contains(t, out, "theme =", "a table as TOML")
	_, err = runBlitz(t, "-c", cfg, "config", "check", filepath.Join(cfg, "none.toml"))
	assert.ErrorIs(t, err, os.ErrNotExist, "a file that isn't there is said so")
}

// A save with errors offers another go on a terminal: yes reopens the
// file until it's fixed (CLP-40).
func TestConfigEditAgain(t *testing.T) {
	old := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdoutIsTerminal = old })
	cfg := t.TempDir()
	count := filepath.Join(t.TempDir(), "count")
	// The first save is broken, the second fixed.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", `sh -c 'if [ -f "`+count+`" ]; then printf "[ui]\ntheme = \"light\"\n" > "$0"; else touch "`+count+`"; printf "[ui\n" > "$0"; fi'`)
	out, err := runBlitzIn(t, "y\n", "-c", cfg, "config", "edit")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Open it again to fix them? [y/N]")
	assert.Contains(t, out, "is fine")
	// No: the problems stay, said.
	require.NoError(t, os.Remove(count))
	_, err = runBlitzIn(t, "n\n", "-c", cfg, "config", "edit")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "problems")
}
