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
