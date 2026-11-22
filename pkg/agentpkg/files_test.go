// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package agentpkg

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A name or version an organization's service sends never reaches
// outside the folder it names: "../.." would have removed the home folder
// (packages#1, #2).
func TestAssetNamesStayInTheirFolder(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".blitz")
	keep := filepath.Join(home, "keep.txt")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "tone"), 0o755))
	require.NoError(t, os.WriteFile(keep, []byte("mine"), 0o644))
	for _, name := range []string{"../..", "..", "", "a/b", "Tone", "../keep"} {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, WriteAsset(root, "skill", name, nil), fs.ErrInvalid)
			assert.ErrorIs(t, RemoveAsset(root, "skill", name), fs.ErrInvalid)
			assert.ErrorIs(t, RemoveAsset(root, "agent", name), fs.ErrInvalid)
		})
	}
	assert.FileExists(t, keep)
	assert.DirExists(t, filepath.Join(root, "skills", "tone"))
	require.NoError(t, RemoveAsset(root, "skill", "tone"))
	assert.NoDirExists(t, filepath.Join(root, "skills", "tone"))

	for v, ok := range map[string]bool{"1.2.0": true, "v2": true, "2.0.0-rc.1+b5": true, "": false, "1/../../x": false, "..": false, "../x": false, "a\\b": false, ".hidden": false} {
		assert.Equal(t, ok, ValidVersion(v), v)
	}
}
