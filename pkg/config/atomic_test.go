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

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A file is written whole or not at all: its folder made, the old one
// replaced; a folder that's a file, a path that's a folder or a value
// that isn't JSON is the error, and nothing is left behind.
func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "f.json")
	require.NoError(t, writeJSONAtomic(path, map[string]int{"a": 1}))
	require.NoError(t, writeJSONAtomic(path, map[string]int{"a": 2}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"a": 2}`, string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	assert.Error(t, writeFileAtomic(filepath.Join(file, "f"), nil, 0o600), "its folder is a file")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "taken"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "taken", "x"), nil, 0o600))
	assert.Error(t, writeFileAtomic(filepath.Join(dir, "taken"), []byte("x"), 0o600), "a folder is there")
	assert.Error(t, writeJSONAtomic(filepath.Join(dir, "chan.json"), make(chan int)), "not JSON")
	assert.NoFileExists(t, filepath.Join(dir, "chan.json"))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".f.json.", "no temporary file left")
	}
}
