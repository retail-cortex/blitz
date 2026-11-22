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
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserPath cleans the user's workspace-relative paths and refuses
// absolute ones, escapes and the .git directory.
func TestUserPath(t *testing.T) {
	ok := map[string]string{
		"":           ".",
		"a/b.txt":    filepath.Join("a", "b.txt"),
		"./a/../b":   "b",
		".github/x":  filepath.Join(".github", "x"),
		".gitignore": ".gitignore",
	}
	for in, want := range ok {
		t.Run("ok "+in, func(t *testing.T) {
			got, err := UserPath(in)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
	bad := map[string]error{
		"/etc/passwd":    ErrOutsideWorkspace,
		"../x":           ErrOutsideWorkspace,
		"a/../../x":      ErrOutsideWorkspace,
		".git":           ErrGitDir,
		".git/config":    ErrGitDir,
		"a/../.git/HEAD": ErrGitDir,
	}
	for in, want := range bad {
		t.Run("bad "+in, func(t *testing.T) {
			_, err := UserPath(in)
			assert.ErrorIs(t, err, want)
		})
	}
}

// TestAgentRule reports the agent's rule for a path: blocked, read-only or
// open.
func TestAgentRule(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "ro"), 0o755))
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: dir, ReadOnlyPaths: []string{"ro"}, BlockedPaths: []string{"**/.env"}})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })

	cases := map[string]string{
		"a.go":       "",
		".env":       "blocked",
		"ro/x.txt":   "read_only",
		"../outside": "blocked",
	}
	for rel, want := range cases {
		t.Run(rel, func(t *testing.T) {
			assert.Equal(t, want, ws.AgentRule(rel))
		})
	}
}

// TestUserFileOperations covers the user's own reads, writes, folders,
// renames and deletes inside the workspace.
func TestUserFileOperations(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	ctx := context.Background()

	require.NoError(t, ws.UserWriteFile(ctx, "a/b.txt", []byte("hello"), nil))
	data, info, err := ws.UserReadFile(filepath.Join("a", "b.txt"), 100)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
	assert.Equal(t, int64(5), info.Size())

	t.Run("too large", func(t *testing.T) {
		_, info, err := ws.UserReadFile(filepath.Join("a", "b.txt"), 2)
		assert.ErrorIs(t, err, ErrTooLarge)
		assert.NotNil(t, info, "the file's information comes back with ErrTooLarge")
	})
	t.Run("folder", func(t *testing.T) {
		_, info, err := ws.UserReadFile("a", 100)
		assert.ErrorContains(t, err, "is a folder")
		assert.True(t, info.IsDir())
	})
	t.Run("missing", func(t *testing.T) {
		_, _, err := ws.UserReadFile("nope", 100)
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	entries, err := ws.UserReadDir("a")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "b.txt", entries[0].Name())
	st, err := ws.UserStat("a")
	require.NoError(t, err)
	assert.True(t, st.IsDir())

	t.Run("write check", func(t *testing.T) {
		var seen []byte
		var existed bool
		err := ws.UserWriteFile(ctx, filepath.Join("a", "b.txt"), []byte("new"), func(cur []byte, exists bool) error {
			seen, existed = cur, exists
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, "hello", string(seen))
		assert.True(t, existed)

		refused := errors.New("changed on disk")
		err = ws.UserWriteFile(ctx, "fresh.txt", []byte("x"), func(cur []byte, exists bool) error {
			assert.False(t, exists, "a new file doesn't exist yet")
			assert.Nil(t, cur)
			return refused
		})
		assert.ErrorIs(t, err, refused)
		assert.NoFileExists(t, filepath.Join(dir, "fresh.txt"), "a refused write leaves nothing")

		err = ws.UserWriteFile(ctx, "a", []byte("x"), func([]byte, bool) error { return nil })
		assert.Error(t, err, "reading a folder as the current content fails")
		assert.Error(t, ws.UserWriteFile(ctx, ".", []byte("x"), nil), "the workspace itself isn't a file")
	})

	t.Run("mkdir", func(t *testing.T) {
		require.NoError(t, ws.UserMkdir(filepath.Join("d", "e")))
		assert.DirExists(t, filepath.Join(dir, "d", "e"))
		assert.ErrorIs(t, ws.UserMkdir(filepath.Join("d", "e")), fs.ErrExist)
		assert.ErrorIs(t, ws.UserMkdir("."), fs.ErrExist)
	})

	t.Run("rename", func(t *testing.T) {
		require.NoError(t, ws.UserRename(ctx, filepath.Join("a", "b.txt"), filepath.Join("x", "y", "c.txt")))
		assert.FileExists(t, filepath.Join(dir, "x", "y", "c.txt"))
		assert.NoFileExists(t, filepath.Join(dir, "a", "b.txt"))
		assert.ErrorIs(t, ws.UserRename(ctx, "missing", "z"), fs.ErrNotExist)
		assert.ErrorIs(t, ws.UserRename(ctx, "x", "d"), fs.ErrExist)
		assert.Error(t, ws.UserRename(ctx, ".", "z"))
		assert.Error(t, ws.UserRename(ctx, "x", "."))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "file"), nil, 0o644))
		assert.Error(t, ws.UserRename(ctx, "x", filepath.Join("file", "sub")), "a file can't be a parent folder")
	})

	t.Run("copy", func(t *testing.T) {
		require.NoError(t, os.Symlink(filepath.Join("y", "c.txt"), filepath.Join(dir, "x", "link")))
		require.NoError(t, ws.UserCopy(ctx, "x", filepath.Join("copies", "x2")), "a folder, its folders made")
		assert.FileExists(t, filepath.Join(dir, "copies", "x2", "y", "c.txt"))
		target, err := os.Readlink(filepath.Join(dir, "copies", "x2", "link"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join("y", "c.txt"), target, "a link is copied as a link")
		require.NoError(t, ws.UserCopy(ctx, filepath.Join("x", "y", "c.txt"), "c-copy.txt"), "a file")
		assert.FileExists(t, filepath.Join(dir, "x", "y", "c.txt"), "the original stays")
		assert.FileExists(t, filepath.Join(dir, "c-copy.txt"))

		for name, tc := range map[string]struct {
			from, to string
			is       error
		}{
			"missing":            {"missing", "z", fs.ErrNotExist},
			"taken":              {"x", "copies", fs.ErrExist},
			"the workspace":      {".", "z", nil},
			"onto the workspace": {"x", ".", nil},
			"into itself":        {"x", filepath.Join("x", "inner"), nil},
			"under a file":       {"c-copy.txt", filepath.Join("file", "sub"), nil},
		} {
			err := ws.UserCopy(ctx, tc.from, tc.to)
			if tc.is != nil {
				assert.ErrorIs(t, err, tc.is, name)
			} else {
				assert.Error(t, err, name)
			}
		}

		cctx, cancel := context.WithCancel(ctx)
		cancel()
		assert.Error(t, ws.UserCopy(cctx, "x", "x3"), "cancelled")
		assert.NoDirExists(t, filepath.Join(dir, "x3"), "a copy that fails is taken back")
	})

	t.Run("remove", func(t *testing.T) {
		require.NoError(t, ws.UserRemove(ctx, "x"))
		assert.NoDirExists(t, filepath.Join(dir, "x"))
		assert.ErrorIs(t, ws.UserRemove(ctx, "x"), fs.ErrNotExist)
		assert.Error(t, ws.UserRemove(ctx, "."))
	})

	t.Run("cancelled", func(t *testing.T) {
		unlock, err := ws.lockPaths(ctx, "held")
		require.NoError(t, err)
		defer unlock()
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		assert.Error(t, ws.UserWriteFile(cctx, "held", nil, nil))
		assert.Error(t, ws.UserRename(cctx, "held", "other"))
		assert.Error(t, ws.UserRemove(cctx, "held"))
		assert.Error(t, ws.UserFinishUpload(cctx, "held.tmp", "held", false))
	})
}

// TestUserUploads refuses what an upload can't be (no name, a file for a
// folder, a missing hidden file) and puts it in place only where it may.
func TestUserUploads(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	ctx := context.Background()
	require.NoError(t, ws.UserWriteFile(ctx, "taken.txt", []byte("old"), nil))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "folder"), 0o755))
	upload := func(t *testing.T, rel, data string) string {
		t.Helper()
		tmp, err := ws.UserCreateUpload(rel)
		require.NoError(t, err)
		require.NoError(t, ws.UserAppend(tmp, []byte(data)))
		return tmp
	}

	tests := []struct {
		name string
		call func(t *testing.T) error
		want string
	}{
		{"no name", func(t *testing.T) error { _, err := ws.UserCreateUpload("."); return err }, "must name a file"},
		{"under a file", func(t *testing.T) error { _, err := ws.UserCreateUpload(filepath.Join("taken.txt", "x")); return err }, "taken.txt"},
		{"append to nothing", func(t *testing.T) error { return ws.UserAppend("missing.tmp", []byte("x")) }, "missing.tmp"},
		{"taken, not replacing", func(t *testing.T) error {
			return ws.UserFinishUpload(ctx, upload(t, "taken.txt", "new"), "taken.txt", false)
		}, "file already exists"},
		{"a folder, even replacing", func(t *testing.T) error {
			return ws.UserFinishUpload(ctx, upload(t, "folder", "new"), "folder", true)
		}, "is a folder"},
		{"finish nothing", func(t *testing.T) error { return ws.UserFinishUpload(ctx, "missing.tmp", "new.txt", false) }, "missing.tmp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, tt.call(t), tt.want)
		})
	}

	t.Run("replacing a file", func(t *testing.T) {
		require.NoError(t, ws.UserFinishUpload(ctx, upload(t, "taken.txt", "new"), "taken.txt", true))
		data, err := os.ReadFile(filepath.Join(dir, "taken.txt"))
		require.NoError(t, err)
		assert.Equal(t, "new", string(data))
	})
	t.Run("discarded", func(t *testing.T) {
		tmp := upload(t, "gone.txt", "x")
		ws.UserDiscardUpload(tmp)
		assert.NoFileExists(t, filepath.Join(dir, tmp))
	})
}

// A read-only workspace (a Viewer's clone, EN-60) refuses every change
// the person's editor and file tree ask for, as the agent's tools do;
// reading still works (security review files#6).
func TestUserFilesReadOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644))
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: dir, ReadOnly: "you're a Viewer of Plans"})
	require.NoError(t, err)
	ctx := context.Background()
	_, err = ws.UserTrash(ctx, "a.txt")
	tmpErr := err
	_, upErr := ws.UserCreateUpload("up.txt")
	for name, err := range map[string]error{
		"write":   ws.UserWriteFile(ctx, "a.txt", []byte("b"), nil),
		"mkdir":   ws.UserMkdir("d"),
		"rename":  ws.UserRename(ctx, "a.txt", "b.txt"),
		"copy":    ws.UserCopy(ctx, "a.txt", "c.txt"),
		"upload":  upErr,
		"finish":  ws.UserFinishUpload(ctx, "x", "up.txt", true),
		"trash":   tmpErr,
		"restore": ws.UserRestoreTrashed(ctx, "x", "a.txt"),
		"remove":  ws.UserRemove(ctx, "a.txt"),
	} {
		require.ErrorIs(t, err, ErrReadOnlyPath, name)
		assert.Contains(t, err.Error(), "you're a Viewer of Plans", name)
	}
	b, _, err := ws.UserReadFile("a.txt", 1<<10)
	require.NoError(t, err, "reading is fine")
	assert.Equal(t, "a", string(b))
	assert.NoFileExists(t, filepath.Join(dir, "b.txt"))
}

// The system's trash takes a path, which a link swapped in on the way
// could send elsewhere: only the file checked in the workspace goes, and
// one that turns out not to be it is put back (security review files#3).
func TestTrashChecked(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "taxes.pdf"), []byte("mine"), 0o644))
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { root.Close() })
	trash := t.TempDir()
	var sent []string
	defer func(was func(context.Context, string) (string, error)) { trashPath = was }(trashPath)
	trashPath = func(_ context.Context, p string) (string, error) {
		sent = append(sent, p)
		where := filepath.Join(trash, filepath.Base(p))
		return where, os.Rename(p, where)
	}
	ctx := context.Background()

	// The path leads elsewhere than the file checked: refused, nothing sent.
	_, err = trashChecked(ctx, root, "a.txt", filepath.Join(outside, "taxes.pdf"))
	require.ErrorIs(t, err, ErrTrashMoved)
	assert.Empty(t, sent)
	assert.FileExists(t, filepath.Join(outside, "taxes.pdf"))

	// The same file: trashed.
	where, err := trashChecked(ctx, root, "a.txt", filepath.Join(dir, "a.txt"))
	require.NoError(t, err)
	assert.FileExists(t, where)
	assert.NoFileExists(t, filepath.Join(dir, "a.txt"))

	// Swapped between the check and the trash: what was trashed isn't it,
	// so it goes back.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644))
	trashPath = func(_ context.Context, p string) (string, error) {
		other := filepath.Join(outside, "taxes.pdf")
		where := filepath.Join(trash, "taxes.pdf")
		return where, os.Rename(other, where)
	}
	_, err = trashChecked(ctx, root, "b.txt", filepath.Join(dir, "b.txt"))
	require.ErrorIs(t, err, ErrTrashMoved)
	assert.FileExists(t, filepath.Join(trash, "taxes.pdf"), "something is at the path: it stays in the trash, findable")
	b, err := os.ReadFile(filepath.Join(dir, "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, "b", string(b), "never put back over a file")

	// Nothing at the path: it goes back there.
	require.NoError(t, os.Rename(filepath.Join(trash, "taxes.pdf"), filepath.Join(outside, "taxes.pdf")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c"), 0o644))
	trashPath = func(_ context.Context, p string) (string, error) {
		where := filepath.Join(trash, "taxes.pdf")
		if err := os.Rename(filepath.Join(outside, "taxes.pdf"), where); err != nil {
			return "", err
		}
		return where, os.Remove(p) // the link's way gone too
	}
	_, err = trashChecked(ctx, root, "c.txt", filepath.Join(dir, "c.txt"))
	require.ErrorIs(t, err, ErrTrashMoved)
	assert.NoFileExists(t, filepath.Join(trash, "taxes.pdf"), "put back")
}
