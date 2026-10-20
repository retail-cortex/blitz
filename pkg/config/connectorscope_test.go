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

// A connection is turned off in one workspace, not the others; the
// choice follows a rename and goes with a delete.
func TestConnectorScope(t *testing.T) {
	settings, a, b := t.TempDir(), t.TempDir(), t.TempDir()
	s := OpenConnectorScope(settings)
	assert.Empty(t, s.Off(a), "everything's on")
	require.NoError(t, s.Set(a, "global/databases/shop", false))
	require.NoError(t, s.Set(a, "global/databases/shop", false), "again: nothing to write")
	require.NoError(t, s.Set(b, "global/databases/shop", false))
	require.NoError(t, s.Set(b, "workspace/databases/kitchen", false))
	assert.Equal(t, map[string]bool{"global/databases/shop": true}, s.Off(a))

	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(a, link))
	assert.Equal(t, map[string]bool{"global/databases/shop": true}, OpenConnectorScope(settings).Off(link), "through a link, from a new store")

	require.NoError(t, s.Rename("", "global/databases/shop", "global/databases/store"))
	require.NoError(t, s.Rename("", "global/databases/none", "global/databases/other"), "nothing to rename")
	require.NoError(t, s.Rename("", "x", "x"))
	assert.Equal(t, map[string]bool{"global/databases/store": true}, s.Off(a))
	assert.Equal(t, map[string]bool{"global/databases/store": true, "workspace/databases/kitchen": true}, s.Off(b))

	require.NoError(t, s.Set(a, "workspace/databases/kitchen", false))
	require.NoError(t, s.Rename(a, "workspace/databases/kitchen", "global/databases/kitchen"))
	assert.Equal(t, map[string]bool{"global/databases/store": true, "global/databases/kitchen": true}, s.Off(a), "made global, still off")
	assert.Equal(t, map[string]bool{"global/databases/store": true, "workspace/databases/kitchen": true}, s.Off(b), "another workspace's own kitchen isn't touched")
	require.NoError(t, s.Forget(a, "global/databases/kitchen"))

	require.NoError(t, s.Set(a, "global/databases/store", true))
	assert.Empty(t, s.Off(a), "on again")
	require.NoError(t, s.Forget("", "global/databases/store"))
	require.NoError(t, s.Forget("", "global/databases/store"), "nothing to forget")
	assert.Equal(t, map[string]bool{"workspace/databases/kitchen": true}, s.Off(b))
	require.NoError(t, s.Forget(b, "workspace/databases/kitchen"))
	assert.Empty(t, s.Off(b))

	assert.Empty(t, s.Off(filepath.Join(a, "missing")))
	assert.Error(t, s.Set(filepath.Join(a, "missing"), "k", false))
	file := filepath.Join(a, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	assert.Error(t, OpenConnectorScope(file).Set(a, "k", false), "the settings folder is a file")
}
