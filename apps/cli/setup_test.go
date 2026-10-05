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

// --add-dir adds read-write roots for the run, as sandbox.allowed_paths
// does (BL-FS-01).
func TestAddDir(t *testing.T) {
	home := isolate(t)
	extra := filepath.Join(home, "shared")
	require.NoError(t, os.MkdirAll(extra, 0o755))
	file := filepath.Join(home, "notes.txt")
	require.NoError(t, os.WriteFile(file, nil, 0o644))

	cfg, err := loadConfig(&globalFlags{dir: t.TempDir(), addDirs: []string{"~/shared"}})
	require.NoError(t, err)
	assert.Contains(t, cfg.Sandbox.AllowedPaths, extra)
	assert.NotEmpty(t, cfg.Sandbox.BlockedPaths, "blocked paths still apply")

	for _, bad := range []string{filepath.Join(home, "missing"), file} {
		_, err := loadConfig(&globalFlags{addDirs: []string{bad}})
		assert.Equal(t, exitUsage, exitCodeFor(err), "%s: %v", bad, err)
		assert.ErrorContains(t, err, "--add-dir")
	}
}

func TestWorkersUndoUnknownRun(t *testing.T) {
	isolate(t)
	out, err := runCLI(t, "-d", t.TempDir(), "workers", "undo", "20260930T000000-nope")
	assert.Equal(t, exitUsage, exitCodeFor(err), "%v\n%s", err, out)
	assert.ErrorContains(t, err, "nothing to undo")
}

// The run's flags override the settings: model, agent and agency; settings
// that don't parse fail.
func TestLoadConfigOverrides(t *testing.T) {
	home := isolate(t)
	cfg, err := loadConfig(&globalFlags{model: "gemini-3.8-pro", agent: "qa", agency: "HIGH"})
	require.NoError(t, err)
	assert.Equal(t, "gemini-3.8-pro", cfg.Blitz.DefaultModel)
	assert.Equal(t, "qa", cfg.Blitz.DefaultAgent)
	assert.Equal(t, "high", cfg.Blitz.AgencyLevel)

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[blitz\n"), 0o600))
	_, err = loadConfig(&globalFlags{})
	assert.ErrorContains(t, err, "failed to load configuration")
}

// A workspace that can't open here is the run's error.
func TestOpenBackendFails(t *testing.T) {
	home := isolate(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[permissions]\nallow = [\"nonsense(\"]\n"), 0o600))
	stdio(t, "")
	_, err := runCLI(t, "-d", t.TempDir(), "hi")
	assert.ErrorContains(t, err, "[permissions]")
}
