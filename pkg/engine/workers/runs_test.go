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

package workers

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunLogAppendListGet checks that runs are kept per worker, listed
// newest first and found by ID.
func TestRunLogAppendListGet(t *testing.T) {
	l := OpenRunLog(filepath.Join(t.TempDir(), "runs"))
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	assert.True(t, l.Last("/ws", "deps").IsZero(), "no runs yet")
	_, ok, err := l.Get("r1")
	require.NoError(t, err, "a log that doesn't exist yet")
	assert.False(t, ok)

	for i, id := range []string{"r1", "r2", "r3"} {
		require.NoError(t, l.Append(api.Run{ID: id, Workspace: "/ws", Worker: "deps", Started: t0.Add(time.Duration(i) * time.Hour)}))
	}
	require.NoError(t, l.Append(api.Run{ID: "o1", Workspace: "/other", Worker: "deps"}))

	runs, err := l.List("/ws", "deps", 0)
	require.NoError(t, err)
	var ids []string
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	assert.Equal(t, []string{"r3", "r2", "r1"}, ids, "newest first, this workspace only")
	runs, err = l.List("/ws", "deps", 2)
	require.NoError(t, err)
	assert.Len(t, runs, 2, "limited")
	assert.Equal(t, t0.Add(2*time.Hour), l.Last("/ws", "deps"))

	r, ok, err := l.Get("o1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "/other", r.Workspace)
	_, ok, err = l.Get("missing")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestRunLogSkipsTornLinesAndOtherFiles checks that a partly written line
// and files that aren't run logs are ignored.
func TestRunLogSkipsTornLinesAndOtherFiles(t *testing.T) {
	dir := t.TempDir()
	l := OpenRunLog(dir)
	require.NoError(t, l.Append(api.Run{ID: "r1", Workspace: "/ws", Worker: "deps"}))
	name, err := l.file("/ws", "deps")
	require.NoError(t, err)
	f, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`{"id":"r2","work`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600))

	runs, err := l.List("/ws", "deps", 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "r1", runs[0].ID)
	_, ok, err := l.Get("r1")
	require.NoError(t, err)
	assert.True(t, ok)
}

// TestRunLogErrors checks invalid worker names, unencodable runs and
// unusable directories.
func TestRunLogErrors(t *testing.T) {
	base := t.TempDir()
	l := OpenRunLog(filepath.Join(base, "runs"))
	assert.Error(t, l.Append(api.Run{Worker: "../escape"}), "invalid name")
	_, err := l.List("/ws", "../escape", 0)
	assert.Error(t, err, "invalid name")
	assert.Error(t, l.Append(api.Run{Worker: "deps", CostUSD: math.NaN()}), "a run that can't be encoded")

	file := filepath.Join(base, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	bad := OpenRunLog(filepath.Join(file, "runs"))
	assert.Error(t, bad.Append(api.Run{Worker: "deps"}), "the directory can't be created")
	_, _, err = OpenRunLog(file).Get("x")
	assert.Error(t, err, "the log directory is a file")

	dir := t.TempDir()
	l = OpenRunLog(dir)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "dir.jsonl"), 0o700))
	_, _, err = l.Get("x")
	assert.Error(t, err, "a log that can't be read")
	name, err := l.file("/ws", "deps")
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(name, 0o700))
	assert.Error(t, l.Append(api.Run{Workspace: "/ws", Worker: "deps"}), "the log file can't be opened")
}
