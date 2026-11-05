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

package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Write replaces a file whole, with the mode asked, and leaves no
// temporary file behind (N26 engine-sub-2#26).
func TestWrite(t *testing.T) {
	tests := []struct {
		name string
		old  bool
		perm fs.FileMode
	}{
		{"a new file, owner-only", false, 0o600},
		{"replacing one, readable by others", true, 0o644},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "SKILL.md")
			if tt.old {
				require.NoError(t, os.WriteFile(p, []byte("old"), 0o600))
			}
			require.NoError(t, Write(p, []byte("new"), tt.perm))
			data, err := os.ReadFile(p)
			require.NoError(t, err)
			assert.Equal(t, "new", string(data))
			info, err := os.Stat(p)
			require.NoError(t, err)
			assert.Equal(t, tt.perm, info.Mode().Perm())
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Len(t, entries, 1, "no temporary file left")
		})
	}
	assert.Error(t, Write(filepath.Join(t.TempDir(), "missing", "x"), nil, 0o600))
}
