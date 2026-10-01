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

package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Without a home directory there's nowhere to keep forged tools.
func TestUniversalConstructorNeedsHome(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := NewUniversalConstructorTool("", allowAll(), nil, nil)
	assert.ErrorContains(t, err, "resolve home directory")

	home := t.TempDir()
	t.Setenv("HOME", home)
	rt := toolOf(t)(NewUniversalConstructorTool("", allowAll(), nil, nil))
	out := runTool(t, rt, map[string]any{"action": "create", "tool_name": "hi", "code": "echo hi"})
	require.Equal(t, true, out["success"], "%v", out)
	assert.FileExists(t, filepath.Join(home, ".blitz", "uc_tools", "hi.sh"), "kept in ~/.blitz/uc_tools")
}

// Failures to save, run or delete a forged tool are reported, not fatal.
func TestUniversalConstructorFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	rt := toolOf(t)(NewUniversalConstructorTool(filepath.Join(file, "uc"), allowAll(), nil, nil))
	out := runTool(t, rt, map[string]any{"action": "create", "tool_name": "x", "code": "echo"})
	assert.Contains(t, errOf(out), "failed to create tools directory")

	dir := filepath.Join(t.TempDir(), "uc")
	rt = toolOf(t)(NewUniversalConstructorTool(dir, allowAll(), nil, nil))
	out = runTool(t, rt, map[string]any{"action": "create", "tool_name": "gotool", "language": "go", "code": "package main\nfunc main() { undefined() }\n"})
	require.Equal(t, true, out["success"], "%v", out)
	out = runTool(t, rt, map[string]any{"action": "run", "tool_name": "gotool"})
	assert.Contains(t, errOf(out), "tool execution failed", "a Go tool that doesn't build fails: %v", out)

	// A directory where the script would go can't be written over.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "blocked.sh"), 0o700))
	out = runTool(t, rt, map[string]any{"action": "create", "tool_name": "blocked", "code": "echo"})
	assert.Contains(t, errOf(out), "failed to save tool")
	// Nor its manifest.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "nomanifest.json"), 0o700))
	out = runTool(t, rt, map[string]any{"action": "create", "tool_name": "nomanifest", "code": "echo"})
	assert.Contains(t, errOf(out), "saved the script but not its manifest")

	// A manifest that is a directory is skipped when loading; a deletion
	// that can't remove the script says so.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub.json", "x"), 0o700))
	out = runTool(t, rt, map[string]any{"action": "create", "tool_name": "stuck", "code": "echo"})
	require.Equal(t, true, out["success"], "%v", out)
	require.NoError(t, os.Remove(filepath.Join(dir, "stuck.sh")))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "stuck.sh", "x"), 0o700))
	out = runTool(t, rt, map[string]any{"action": "delete", "tool_name": "stuck"})
	assert.Contains(t, errOf(out), "failed to delete")
	reloaded := toolOf(t)(NewUniversalConstructorTool(dir, allowAll(), nil, nil))
	out = runTool(t, reloaded, map[string]any{"action": "list"})
	assert.Equal(t, "1 custom tools available", out["result"], "only the Go tool loads: %v", out)
}
