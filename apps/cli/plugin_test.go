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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pluginDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "kit")
	for p, c := range map[string]string{
		"plugin.toml":    "name = \"kit\"\nversion = \"1.0.0\"\ndescription = \"A kit\"\n",
		"commands/hi.md": "---\ndescription: Say hi\n---\nSay hi.\n",
		"hooks.toml":     "[[stop]]\ncommand = \"true\"\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644))
	}
	return dir
}

func TestPluginCommand(t *testing.T) {
	isolate(t)
	src := pluginDir(t)

	out, err := runCLIWithInput(t, "n\n", "plugin", "install", src)
	require.NoError(t, err)
	assert.Contains(t, out, "runs code: stop hook: true")
	assert.Contains(t, out, "It runs code on your machine")
	assert.Contains(t, out, "Not installed.")

	out, err = runCLIWithInput(t, "y\n", "plugin", "install", src)
	require.NoError(t, err)
	assert.Contains(t, out, "Installed kit 1.0.0 (enabled).")

	out, err = runCLI(t, "plugin", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "kit\t1.0.0\tenabled\t"+src)
	out, err = runCLI(t, "plugin", "show", "kit")
	require.NoError(t, err)
	assert.Contains(t, out, "command: /hi")

	out, err = runCLI(t, "plugin", "disable", "kit")
	require.NoError(t, err)
	assert.Contains(t, out, "kit: disabled for every workspace")
	out, _ = runCLI(t, "plugin", "list")
	assert.Contains(t, out, "\tdisabled\t")
	_, err = runCLI(t, "plugin", "enable", "ghost")
	assert.ErrorContains(t, err, "no such plugin")

	out, err = runCLI(t, "plugin", "update", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "kit 1.0.0 is up to date.")
	require.NoError(t, os.WriteFile(filepath.Join(src, "plugin.toml"), []byte("name = \"kit\"\nversion = \"1.1.0\"\n"), 0o644))
	out, err = runCLI(t, "plugin", "update", "--yes", "kit")
	require.NoError(t, err)
	assert.Contains(t, out, "Installed kit 1.1.0 (disabled, as before).")

	out, err = runCLI(t, "plugin", "remove", "kit")
	require.NoError(t, err)
	assert.Contains(t, out, "Removed kit.")
	out, _ = runCLI(t, "plugin", "list")
	assert.Contains(t, out, "No plugins installed")

	out, err = runCLI(t, "plugin", "marketplace", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No marketplaces")
	_, err = runCLI(t, "plugin", "marketplace", "remove", "nope")
	assert.Error(t, err)
}

func TestPluginImport(t *testing.T) {
	isolate(t)
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, ".claude-plugin", "plugin.json"), []byte(`{"name": "tiny"}`), 0o644))
	out := filepath.Join(t.TempDir(), "tiny-blitz")
	text, err := runCLI(t, "plugin", "import", "claude", src, "--out", out)
	require.NoError(t, err)
	assert.Contains(t, text, "Made the plugin tiny 0.0.0 in "+out)
	_, err = runCLI(t, "plugin", "import", "cursor", src)
	assert.ErrorContains(t, err, "import claude or gemini")
}

// runCLIWithFile is runCLI with a file as stdin: not a terminal, so
// nothing can be asked.
func runCLIWithFile(t *testing.T, args ...string) (string, error) {
	t.Helper()
	f, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer f.Close()
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(f)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), err
}

// Installing asks first: without a terminal it's refused, an empty answer
// installs nothing, and a plugin that adds nothing says so.
func TestPluginInstallAsks(t *testing.T) {
	isolate(t)
	src := pluginDir(t)
	_, err := runCLIWithFile(t, "plugin", "install", src)
	assert.Equal(t, exitUsage, exitCodeFor(err))
	assert.ErrorContains(t, err, "add --yes")

	out, err := runCLIWithInput(t, "", "plugin", "install", src)
	require.NoError(t, err)
	assert.Contains(t, out, "Not installed.")

	empty := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.MkdirAll(empty, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(empty, "plugin.toml"), []byte("name = \"empty\"\nversion = \"0.1.0\"\n"), 0o644))
	out, err = runCLIWithInput(t, "yes\n", "plugin", "install", empty)
	require.NoError(t, err)
	assert.Contains(t, out, "(it adds nothing)")
	assert.Contains(t, out, "Install it? [y/N]")
	assert.Contains(t, out, "Installed empty 0.1.0 (enabled).")
}

// The plugin commands' errors: what isn't there, and sources that are
// gone.
func TestPluginCommandErrors(t *testing.T) {
	isolate(t)
	_, err := runCLI(t, "plugin", "install", "--yes", filepath.Join(t.TempDir(), "nothing"))
	assert.Error(t, err, "install from nowhere")
	for _, args := range [][]string{{"plugin", "show", "ghost"}, {"plugin", "remove", "ghost"}} {
		_, err := runCLI(t, args...)
		assert.Equal(t, exitUsage, exitCodeFor(err), "%v: %v", args, err)
	}

	// Two plugins; one's source goes away.
	kit := pluginDir(t)
	other := filepath.Join(t.TempDir(), "other")
	require.NoError(t, os.MkdirAll(other, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(other, "plugin.toml"), []byte("name = \"other\"\nversion = \"1.0.0\"\n"), 0o644))
	for _, src := range []string{kit, other} {
		_, err := runCLI(t, "plugin", "install", "--yes", src)
		require.NoError(t, err)
	}
	out, err := runCLI(t, "plugin", "update", "other")
	require.NoError(t, err)
	assert.Contains(t, out, "other 1.0.0 is up to date.")
	assert.NotContains(t, out, "kit", "only the named plugin is updated")
	require.NoError(t, os.RemoveAll(kit))
	_, err = runCLI(t, "plugin", "update", "--yes")
	assert.ErrorContains(t, err, "kit: ")
	_, err = runCLI(t, "plugin", "update", "ghost")
	assert.ErrorContains(t, err, "ghost")
}

// A broken plugin index fails the commands that read it.
func TestPluginBrokenIndex(t *testing.T) {
	home := isolate(t)
	dir := filepath.Join(home, ".blitz", "plugins")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "installed.toml"), []byte("[[[broken"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marketplaces.toml"), []byte("[[[broken"), 0o644))
	for _, args := range [][]string{{"plugin", "list"}, {"plugin", "update"}, {"plugin", "marketplace", "list"}} {
		_, err := runCLI(t, args...)
		assert.Error(t, err, "%v", args)
	}
}

// A marketplace lists plugins to install by name.
func TestPluginMarketplace(t *testing.T) {
	isolate(t)
	market := t.TempDir()
	kit := pluginDir(t)
	require.NoError(t, os.Rename(kit, filepath.Join(market, "kit")))
	hash, err := plugins.Hash(filepath.Join(market, "kit"))
	require.NoError(t, err)
	index := fmt.Sprintf("name = \"local\"\n\n[[plugins]]\nname = \"kit\"\nversion = \"1.0.0\"\ndescription = \"A kit\"\nsource = \"kit\"\nhash = %q\n", hash)
	require.NoError(t, os.WriteFile(filepath.Join(market, "marketplace.toml"), []byte(index), 0o644))

	out, err := runCLI(t, "plugin", "marketplace", "add", market)
	require.NoError(t, err, out)
	assert.Contains(t, out, "Added the marketplace local, with 1 plugins:")
	assert.Contains(t, out, "kit 1.0.0\tA kit")
	out, err = runCLI(t, "plugin", "marketplace", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "local\t"+market)
	out, err = runCLI(t, "plugin", "install", "--yes", "kit@local")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Installed kit 1.0.0")
	_, err = runCLI(t, "plugin", "install", "--yes", "ghost@local")
	assert.Error(t, err)
	out, err = runCLI(t, "plugin", "marketplace", "remove", "local")
	require.NoError(t, err)
	assert.Contains(t, out, "Removed the marketplace local.")
	_, err = runCLI(t, "plugin", "marketplace", "add", filepath.Join(t.TempDir(), "nowhere"))
	assert.Error(t, err)
}

// A Gemini CLI extension converts, with what couldn't be listed.
func TestPluginImportGemini(t *testing.T) {
	isolate(t)
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "gemini-extension.json"), []byte(`{"name": "gem", "version": "1.0.0",
		"mcpServers": {"events": {"url": "http://localhost:1/sse"}, "local": {"command": "node", "args": ["s.js"]}}}`), 0o644))
	out := filepath.Join(t.TempDir(), "gem-blitz")
	text, err := runCLI(t, "plugin", "import", "gemini", src, "-o", out)
	require.NoError(t, err)
	assert.Contains(t, text, "Made the plugin gem 1.0.0 in "+out)
	assert.Contains(t, text, "  + MCP server: local")
	assert.Contains(t, text, "Not converted:\n  - MCP server events: SSE transport")
	_, err = runCLI(t, "plugin", "import", "gemini", t.TempDir())
	assert.ErrorContains(t, err, "not a Gemini CLI extension")
}
