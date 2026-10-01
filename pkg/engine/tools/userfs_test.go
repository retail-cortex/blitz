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
	})
}
