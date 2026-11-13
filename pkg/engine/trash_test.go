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

package engine

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/ostrash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// With the system's trash (FIL-37a, the default where there's one), a
// deleted file or folder goes there and Undo brings it back; what's no
// longer there (emptied, or put back from the trash) can't be restored.
// tools.trash = "workspace" keeps the workspace's own.
func TestTrashToTheSystemTrash(t *testing.T) {
	sys := t.TempDir()
	t.Cleanup(ostrash.Fake(sys))
	w := openTest(t)
	ctx := context.Background()
	write(t, w.Dir(), "docs/a.md", "a")

	id, err := w.TrashFile(ctx, "docs")
	require.NoError(t, err)
	assert.Regexp(t, systemTrashIDs, id)
	assert.FileExists(t, filepath.Join(sys, "1", "docs", "a.md"), "in the system's trash")
	assert.NoDirExists(t, filepath.Join(w.Dir(), "docs"))
	assert.NoDirExists(t, filepath.Join(w.Dir(), ".blitz", "trash"), "not the workspace's")

	require.NoError(t, w.RestoreFile(ctx, id, "docs"))
	b, err := os.ReadFile(filepath.Join(w.Dir(), "docs", "a.md"))
	require.NoError(t, err)
	assert.Equal(t, "a", string(b))
	assert.ErrorIs(t, w.RestoreFile(ctx, id, "docs"), ErrNotInTrash, "restored already")

	// Emptied from the system's trash meanwhile: nothing to put back.
	id, err = w.TrashFile(ctx, "docs/a.md")
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(filepath.Join(sys, "2")))
	assert.ErrorIs(t, w.RestoreFile(ctx, id, "docs/a.md"), ErrNotInTrash)
	assert.ErrorIs(t, w.RestoreFile(ctx, "sys-0000000000000000", "docs/a.md"), ErrNotInTrash, "an ID it never gave")

	t.Run("the workspace's, by setting", func(t *testing.T) {
		w, _ := openTestWith(t, func(c *config.Config) { c.Tools.Trash = config.TrashWorkspace })
		write(t, w.Dir(), "b.md", "b")
		id, err := w.TrashFile(ctx, "b.md")
		require.NoError(t, err)
		assert.Regexp(t, trashIDs, id)
		assert.DirExists(t, filepath.Join(w.Dir(), ".blitz", "trash", id))
	})
}

// A deleted file or folder goes to the workspace's trash and comes back
// with its ID; old ones are emptied (FIL-37).
func TestTrashAndRestore(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	write(t, w.Dir(), "docs/a.md", "a")
	write(t, w.Dir(), "docs/sub/b.md", "b")

	id, err := w.TrashFile(ctx, "docs")
	require.NoError(t, err)
	assert.Regexp(t, trashIDs, id)
	_, err = os.Stat(filepath.Join(w.Dir(), "docs"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "still there")
	ignore, err := os.ReadFile(filepath.Join(w.Dir(), ".blitz", "trash", ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "*\n", string(ignore), "the trash stays out of git")

	require.NoError(t, w.RestoreFile(ctx, id, "docs"))
	b, err := os.ReadFile(filepath.Join(w.Dir(), "docs", "sub", "b.md"))
	require.NoError(t, err)
	assert.Equal(t, "b", string(b))
	_, err = os.Stat(filepath.Join(w.Dir(), ".blitz", "trash", id))
	assert.ErrorIs(t, err, fs.ErrNotExist, "the trash's folder for it is gone")

	t.Run("refused", func(t *testing.T) {
		for name, tc := range map[string]struct {
			id, path string
			want     error
		}{
			"restored already": {id, "docs", ErrNotInTrash},
			"not an ID":        {"../../x", "docs", ErrNotInTrash},
			"outside":          {id, "../x", ErrBadPath},
			"the workspace":    {id, "", ErrBadPath},
		} {
			t.Run(name, func(t *testing.T) {
				assert.ErrorIs(t, w.RestoreFile(ctx, tc.id, tc.path), tc.want)
			})
		}
		_, err := w.TrashFile(ctx, "")
		assert.Error(t, err, "trashed the workspace")
		_, err = w.TrashFile(ctx, "missing.md")
		assert.ErrorIs(t, err, fs.ErrNotExist)
		_, err = w.TrashFile(ctx, "../outside.md")
		assert.ErrorIs(t, err, ErrBadPath, "a path outside the workspace")
	})

	t.Run("something's there now", func(t *testing.T) {
		id, err := w.TrashFile(ctx, "docs/a.md")
		require.NoError(t, err)
		write(t, w.Dir(), "docs/a.md", "new")
		assert.ErrorIs(t, w.RestoreFile(ctx, id, "docs/a.md"), fs.ErrExist)
	})

	t.Run("emptied after a day", func(t *testing.T) {
		old := filepath.Join(w.Dir(), ".blitz", "trash", "20200101000000-00000000")
		require.NoError(t, os.MkdirAll(old, 0o755))
		write(t, old, "x.md", "x")
		w.emptyTrash(ctx, time.Now())
		_, err := os.Stat(old)
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("the trash itself goes for good", func(t *testing.T) {
		id, err := w.TrashFile(ctx, ".blitz/trash")
		require.NoError(t, err)
		assert.Empty(t, id)
		_, err = os.Stat(filepath.Join(w.Dir(), ".blitz", "trash"))
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})
}

// Copies of files, folders and links; never over something, or into
// itself (FIL-38).
func TestCopyFile(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	write(t, w.Dir(), "src/a.go", "package a\n")
	write(t, w.Dir(), "src/sub/b.go", "package sub\n")
	require.NoError(t, os.Symlink("a.go", filepath.Join(w.Dir(), "src", "link.go")))

	require.NoError(t, w.CopyFile(ctx, "src", "copy/src"))
	b, err := os.ReadFile(filepath.Join(w.Dir(), "copy", "src", "sub", "b.go"))
	require.NoError(t, err)
	assert.Equal(t, "package sub\n", string(b))
	target, err := os.Readlink(filepath.Join(w.Dir(), "copy", "src", "link.go"))
	require.NoError(t, err)
	assert.Equal(t, "a.go", target, "a link is copied as a link")

	require.NoError(t, w.CopyFile(ctx, "src/a.go", "src/a copy.go"))
	b, err = os.ReadFile(filepath.Join(w.Dir(), "src", "a copy.go"))
	require.NoError(t, err)
	assert.Equal(t, "package a\n", string(b))

	for name, tc := range map[string]struct {
		from, to string
		want     error
	}{
		"over a file": {"src/a.go", "src/sub/b.go", fs.ErrExist},
		"missing":     {"nope.go", "x.go", fs.ErrNotExist},
		"outside":     {"src/a.go", "../x.go", ErrBadPath},
	} {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, w.CopyFile(ctx, tc.from, tc.to), tc.want)
		})
	}
	assert.ErrorContains(t, w.CopyFile(ctx, "src", "src/inner"), "into itself")
	assert.Error(t, w.CopyFile(ctx, "", "x"), "copied the workspace")
}
