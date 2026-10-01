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
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenWorkspaceRoots opens extra read-write and read-only roots, refuses
// missing ones and bad blocked patterns, and describes what it opened.
func TestOpenWorkspaceRoots(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"rw", "ro"} {
		require.NoError(t, os.Mkdir(filepath.Join(dir, d), 0o755))
	}
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: dir, AllowedPaths: []string{"rw"}, ReadOnlyPaths: []string{"ro"}, BlockedPaths: []string{"*.key"}})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	assert.Len(t, ws.RootDirs(), 3)
	assert.Len(t, ws.WritableDirs(), 2)
	assert.Len(t, ws.ReadOnlyDirs(), 1)
	assert.Equal(t, DefaultMaxFileSize, ws.MaxFileSize(), "a zero limit means the default")
	d := ws.Describe()
	require.Len(t, d, 4)
	assert.Contains(t, d[1], "allowed (ro)", "roots are sorted: ro before rw")
	assert.Contains(t, d[2], "allowed (rw)")
	assert.Equal(t, "blocked: *.key", d[3])

	cases := map[string]WorkspaceOptions{
		"missing workspace":     {Dir: filepath.Join(dir, "nope")},
		"missing allowed root":  {Dir: dir, AllowedPaths: []string{"nope"}},
		"missing readonly root": {Dir: dir, ReadOnlyPaths: []string{"nope"}},
		"bad blocked pattern":   {Dir: dir, BlockedPaths: []string{"a\"b"}},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := OpenWorkspace(o)
			assert.Error(t, err)
		})
	}
}

// TestOpenWorkspaceDefaultsToCurrentDir opens "." when no directory is given.
func TestOpenWorkspaceDefaultsToCurrentDir(t *testing.T) {
	ws, err := OpenWorkspace(WorkspaceOptions{})
	require.NoError(t, err)
	defer ws.Close()
	wd, err := os.Getwd()
	require.NoError(t, err)
	real, err := filepath.EvalSymlinks(wd)
	require.NoError(t, err)
	assert.Equal(t, real, ws.Dir())
}

// TestWorkspaceSymlinkIntoReadOnlyRoot refuses writes through a symlink
// that leads into a read-only root.
func TestWorkspaceSymlinkIntoReadOnlyRoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "ro"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ro", "f"), []byte("x"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(dir, "ro", "f"), filepath.Join(dir, "link")))
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: dir, ReadOnlyPaths: []string{"ro"}})
	require.NoError(t, err)
	defer ws.Close()
	_, err = ws.WritablePath("link")
	assert.ErrorIs(t, err, ErrReadOnlyPath)
	_, err = ws.Rel("link")
	assert.NoError(t, err, "reading through the link is fine")
}

// TestWorkspaceRefusals checks the errors of each operation for paths
// outside the workspace, the workspace itself and directories.
func TestWorkspaceRefusals(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	ctx := context.Background()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644))

	_, err := ws.Abs("../x")
	assert.ErrorIs(t, err, ErrOutsideWorkspace, "Abs")
	_, err = ws.Abs("missing")
	assert.ErrorIs(t, err, fs.ErrNotExist, "Abs of a missing file")
	_, err = ws.Stat("../x")
	assert.ErrorIs(t, err, ErrOutsideWorkspace, "Stat")
	_, err = ws.ReadFile("../x")
	assert.ErrorIs(t, err, ErrOutsideWorkspace, "ReadFile")
	_, err = ws.ReadFile("sub")
	assert.ErrorContains(t, err, "is a directory")
	_, err = ws.WritablePath("../x")
	assert.ErrorIs(t, err, ErrOutsideWorkspace, "WritablePath")

	assert.ErrorIs(t, ws.CreateExclusive(ctx, "../x", nil), ErrOutsideWorkspace)
	assert.ErrorContains(t, ws.CreateExclusive(ctx, ".", nil), "must name a file")
	assert.Error(t, ws.CreateExclusive(ctx, "file/child", nil), "a file can't be a parent folder")
	assert.ErrorIs(t, ws.WriteFileAtomic(ctx, "../x", nil), ErrOutsideWorkspace)
	assert.ErrorContains(t, ws.WriteFileAtomic(ctx, ".", nil), "must name a file")
	assert.Error(t, ws.WriteFileAtomic(ctx, "file/child", nil), "a file can't be a parent folder")
	assert.ErrorIs(t, ws.RemoveFile(ctx, "../x"), ErrOutsideWorkspace)
	assert.ErrorContains(t, ws.RemoveFile(ctx, "."), "sandbox root")
	assert.ErrorIs(t, ws.RemoveFile(ctx, "missing"), fs.ErrNotExist)
	assert.ErrorContains(t, ws.RemoveFile(ctx, "sub"), "is a directory")
	assert.ErrorIs(t, ws.Walk("../x", func(WalkEntry) error { return nil }), ErrOutsideWorkspace)
	assert.ErrorIs(t, ws.Walk("missing", func(WalkEntry) error { return nil }), fs.ErrNotExist)
}

// TestWorkspaceWriteThroughSymlinkToDirectory refuses to write through a
// symlink that leads to a directory.
func TestWorkspaceWriteThroughSymlinkToDirectory(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.Symlink("sub", filepath.Join(dir, "link")))
	assert.ErrorContains(t, ws.WriteFileAtomic(context.Background(), "link", []byte("x")), "is a directory")
	require.NoError(t, os.Symlink("missing", filepath.Join(dir, "dangling")))
	assert.ErrorIs(t, ws.WriteFileAtomic(context.Background(), "dangling", []byte("x")), fs.ErrNotExist)
}

// TestWorkspaceCheckpointsSymlinkAndLargeFiles snapshots a symlink with its
// target's mode and size (the link's own size, its target's name, is longer
// than the limit here), and a file over the size limit as too large to
// restore.
func TestWorkspaceCheckpointsSymlinkAndLargeFiles(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorkspace(dir, 4)
	require.NoError(t, err)
	defer ws.Close()
	cp := NewCheckpoints(ws, 0)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target"), []byte("ab"), 0o600))
	require.NoError(t, os.Symlink("target", filepath.Join(dir, "link")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "short"), []byte("0123456789"), 0o644))
	require.NoError(t, os.Symlink("short", filepath.Join(dir, "s")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big"), []byte("0123456789"), 0o644))

	cp.Begin("t")
	ctx := context.Background()
	require.NoError(t, ws.WriteFileAtomic(ctx, "link", []byte("cd")))
	require.NoError(t, ws.WriteFileAtomic(ctx, "big", []byte("x")))
	require.NoError(t, ws.WriteFileAtomic(ctx, "s", []byte("x")))
	l := cp.List()
	require.Len(t, l, 1)
	assert.Len(t, l[0].Files, 3)

	_, err = cp.Undo(false)
	assert.ErrorContains(t, err, "larger than the snapshot limit")
	res, err := cp.Undo(true)
	require.NoError(t, err)
	b, err := os.ReadFile(filepath.Join(dir, "target"))
	require.NoError(t, err)
	assert.Equal(t, "ab", string(b), "the link's target is restored")
	assert.Equal(t, []string{"link"}, res.Restored, "the large files can't be restored, through a short link or not")
}

// TestWorkspaceFailedChangeIsNotCheckpointed discards the snapshot when
// a write or delete fails.
func TestWorkspaceFailedChangeIsNotCheckpointed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "f"), []byte("x"), 0o644))
	require.NoError(t, os.Chmod(sub, 0o555))
	t.Cleanup(func() { os.Chmod(sub, 0o755) })

	cp.Begin("t")
	ctx := context.Background()
	assert.Error(t, ws.WriteFileAtomic(ctx, "sub/f", []byte("y")))
	assert.Error(t, ws.RemoveFile(ctx, "sub/f"))
	assert.Error(t, ws.CreateExclusive(ctx, "sub/new", []byte("y")))
	assert.Error(t, ws.WriteFileAtomic(ctx, "sub/new/f", []byte("y")), "its folder can't be made")
	assert.Empty(t, cp.List(), "failed changes leave no checkpoint")
}

// TestWorkspaceCheckpointsUnreadableFile records a file it can't read as
// one it can't restore, and still writes it.
func TestWorkspaceCheckpointsUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	p := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(p, []byte("secret"), 0o000))
	cp.Begin("t")
	require.NoError(t, ws.WriteFileAtomic(context.Background(), "f", []byte("new")))
	_, err := cp.Undo(false)
	assert.ErrorContains(t, err, "larger than the snapshot limit", "its snapshot has no content")
}

// TestWorkspaceRestore removes a file that didn't exist (a missing one is
// fine) and refuses paths outside the workspace.
func TestWorkspaceRestore(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	assert.NoError(t, ws.restore(filepath.Join(dir, "missing"), fileState{}, nil))
	assert.ErrorIs(t, ws.restore("/elsewhere/x", fileState{}, nil), ErrOutsideWorkspace)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file"), nil, 0o644))
	assert.Error(t, ws.restore(filepath.Join(dir, "file", "x"), fileState{Exists: true}, nil), "a file can't be a parent folder")
	require.NoError(t, ws.restore(filepath.Join(dir, "a", "b"), fileState{Exists: true, Mode: 0o600}, []byte("z")))
	b, err := os.ReadFile(filepath.Join(dir, "a", "b"))
	require.NoError(t, err)
	assert.Equal(t, "z", string(b))
}

// TestWorkspaceWalkSkipsBlockedAndUnreadable leaves out blocked files and
// directories, and carries on past a directory it can't read.
func TestWorkspaceWalkSkipsBlockedAndUnreadable(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.txt", "s.key", "secret/x", "locked/y"} {
		p := filepath.Join(dir, f)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, nil, 0o644))
	}
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: dir, BlockedPaths: []string{"*.key", "secret"}})
	require.NoError(t, err)
	defer ws.Close()
	if os.Geteuid() != 0 {
		require.NoError(t, os.Chmod(filepath.Join(dir, "locked"), 0o000))
		t.Cleanup(func() { os.Chmod(filepath.Join(dir, "locked"), 0o755) })
	}
	var seen []string
	require.NoError(t, ws.Walk("", func(e WalkEntry) error {
		seen = append(seen, filepath.ToSlash(e.Path))
		return nil
	}))
	assert.Contains(t, seen, "a.txt")
	assert.NotContains(t, seen, "s.key")
	assert.NotContains(t, seen, "secret")
	assert.NotContains(t, seen, "secret/x")
}

// TestResolveExistingPrefix resolves the existing part of a path and keeps
// the rest.
func TestResolveExistingPrefix(t *testing.T) {
	dir := t.TempDir()
	real, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	got, err := resolveExistingPrefix(filepath.Join(dir, "a", "b"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(real, "a", "b"), got)
}
