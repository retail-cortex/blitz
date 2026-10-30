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

package session

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A session's sub-agent log holds one line per event, owner-only, and goes
// with the session when it's deleted.
func TestSubagentLog(t *testing.T) {
	s, dir := newStorage(t)
	rec, err := s.CreateSession("", "t", "blitz")
	require.NoError(t, err)
	require.NoError(t, s.AddMessage("user", "/setup"))
	require.NoError(t, s.AppendSubagentEvent(rec.ID, map[string]any{"author": "project-spec", "n": 1}))
	require.NoError(t, s.AppendSubagentEvent(rec.ID, map[string]any{"author": "project-spec", "n": 2}))
	path := filepath.Join(dir, rec.ID+subagentsSuffix)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(data), "\n"), "a line per event: %s", data)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "owner-only")

	assert.ErrorIs(t, s.AppendSubagentEvent("../x", nil), ErrInvalidID, "no path outside the folder")
	assert.Error(t, s.AppendSubagentEvent(rec.ID, func() {}), "an event that can't be encoded")

	_, err = s.CreateSession("", "next", "blitz")
	require.NoError(t, err)
	require.NoError(t, s.Delete(rec.ID))
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "deleted with its session")
}
