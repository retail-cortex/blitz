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
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blitz search waits for a workspace just opened to be indexed, then
// prints its hits, as text or JSON; --status and --reindex say how the
// index is; bad input is a usage error.
func TestSearchCommand(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hangar.md"), []byte("# Hangar\n\nThe zeppelin docks here.\n"), 0o644))

	out, err := runCLI(t, "-d", dir, "search", "--json", "zeppelin")
	require.NoError(t, err, out)
	var hits []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &hits), out)
	require.Len(t, hits, 1)
	assert.Equal(t, "hangar.md", hits[0]["ref"])
	assert.EqualValues(t, 3, hits[0]["line"])

	for _, tc := range []struct {
		name string
		args []string
		want string
		code int
	}{
		{"text", []string{"search", "--in", "files", "zeppelin"}, "1. hangar.md:3\n", 0},
		{"none", []string{"search", "--in", "chats", "zeppelin"}, "Nothing in the workspace matches zeppelin.", 0},
		{"status", []string{"search", "--status"}, "Search index:", 0},
		{"reindex", []string{"search", "--reindex"}, "Scanning the workspace for search.", 0},
		{"no terms", []string{"search"}, "", exitUsage},
		{"only quotes", []string{"search", `""`}, "", exitFailure},
		{"bad source", []string{"search", "--in", "email", "x"}, "", exitUsage},
		{"semantic without a model", []string{"search", "--mode", "semantic", "x"}, "", exitFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runCLI(t, append([]string{"-d", dir}, tc.args...)...)
			if tc.code != 0 {
				assert.Equal(t, tc.code, exitCodeFor(err), "%v", err)
				return
			}
			require.NoError(t, err, out)
			assert.Contains(t, out, tc.want)
		})
	}
	_, err = runCLI(t, "-d", dir, "search", "--in", "email", "x")
	assert.ErrorIs(t, err, api.ErrUnknownSearchSource)
}

// With search off in the settings, blitz search says so.
func TestSearchCommandOff(t *testing.T) {
	home := isolate(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[search]\nenabled = false\n"), 0o600))
	dir := t.TempDir()
	for _, args := range [][]string{{"search", "zeppelin"}, {"search", "--reindex"}} {
		_, err := runCLI(t, append([]string{"-d", dir}, args...)...)
		assert.ErrorIs(t, err, api.ErrSearchDisabled, "%v", args)
	}
	out, err := runCLI(t, "-d", dir, "search", "--status")
	require.NoError(t, err)
	assert.Contains(t, out, "workspace search is off")
}

// A workspace folder that isn't there is an error before anything opens.
func TestSearchCommandBadDir(t *testing.T) {
	isolate(t)
	_, err := runCLI(t, "-d", "/definitely/not/here", "search", "x")
	assert.Error(t, err)
}

// A workspace another blitz holds is busy: blitz search says so.
func TestSearchCommandBusy(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = dir
	w, err := engine.Open(context.Background(), cfg, engine.Options{})
	require.NoError(t, err)
	defer w.Close()
	_, err = runCLI(t, "-d", dir, "search", "x")
	assert.ErrorIs(t, err, api.ErrWorkspaceBusy)
}
