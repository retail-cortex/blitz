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
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ghostDir is a directory entry for one that's gone by the time it's read.
type ghostDir struct{ name string }

func (g ghostDir) Name() string               { return g.name }
func (g ghostDir) IsDir() bool                { return true }
func (g ghostDir) Type() fs.FileMode          { return fs.ModeDir }
func (g ghostDir) Info() (fs.FileInfo, error) { return nil, fs.ErrNotExist }

// A directory removed between reading its parent and walking it is
// skipped, and not remembered.
func TestBlockedScanDirectoryGone(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".env"), "x")
	m, err := NewPathMatcher([]string{".env"}, []string{ws})
	require.NoError(t, err)
	scan := newBlockedScan(OSSandboxSpec{WritableDirs: []string{ws}, Blocked: m})
	scan.readDir = func(path string) ([]os.DirEntry, error) {
		list, err := os.ReadDir(path)
		if path == ws {
			list = append(list, ghostDir{"gone"})
		}
		return list, err
	}
	files, dirs := scan.expand()
	assert.Equal(t, []string{filepath.Join(ws, ".env")}, files)
	assert.Empty(t, dirs)
	assert.NotContains(t, scan.dirs, filepath.Join(ws, "gone"))
}

// fakeInfo is a FileInfo without a system stat.
type fakeInfo struct{}

func (fakeInfo) Name() string       { return "f" }
func (fakeInfo) Size() int64        { return 0 }
func (fakeInfo) Mode() fs.FileMode  { return 0 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (fakeInfo) IsDir() bool        { return false }
func (fakeInfo) Sys() any           { return nil }

// A file the system says nothing about has no inode.
func TestInodeUnknown(t *testing.T) {
	assert.Zero(t, inode(fakeInfo{}))
}
