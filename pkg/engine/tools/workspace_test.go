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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceRel(t *testing.T) {
	ws, dir := newTestWorkspace(t)

	// Positive: relative paths, cleaned paths, and absolute paths inside the
	// workspace (both the raw TempDir spelling and the symlink-resolved one).
	positive := map[string]string{
		"":                               ".",
		"a/b.txt":                        filepath.Join("a", "b.txt"),
		"./a/../b.txt":                   "b.txt",
		filepath.Join(dir, "x", "y.go"):  filepath.Join("x", "y.go"),
		filepath.Join(ws.Dir(), "z.txt"): "z.txt",
		dir:                              ".",
	}
	for in, want := range positive {
		got, err := ws.Rel(in)
		if !assert.NoError(t, err, "Rel(%q)", in) {
		continue
		}
		assert.Equal(t, want, got, "Rel(%q) = %q, want %q", in, got, want)
	}

	// Negative: traversal and absolute paths outside the workspace.
	negative := []string{
		"..",
		"../secret",
		"a/../../secret",
		"/etc/passwd",
		filepath.Join(filepath.Dir(dir), "sibling", "f.txt"),
	}
	for _, in := range negative {
		_, err := ws.Rel(in)
		assert.ErrorIs(t, err, ErrOutsideWorkspace, "Rel(%q) expected ErrOutsideWorkspace, got %v", in, err)
	}
}

func TestWorkspaceSymlinkEscapeRejected(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "top secret")
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	// Lexically "link/secret.txt" is inside the workspace; os.Root must refuse it.
	_, err := ws.ReadFile(filepath.Join("link", "secret.txt"))
	require.Error(t, err, "expected symlink escape to be rejected")
	out := runTool(t, toolOf(t)(NewReadFileTool(ws)), map[string]any{"path": "link/secret.txt"})
	require.NotEqual(t, "", errOf(out), "read_file followed a symlink out of the workspace: %v", out)
	require.NotContains(t, out["content"].(string), "top secret", "read_file followed a symlink out of the workspace: %v", out)

	// Writes through the symlink must also fail and leave the target untouched.
	require.Error(t, ws.WriteFileAtomic(filepath.Join("link", "secret.txt"), []byte("pwned")), "expected write through escaping symlink to fail")
	b, _ := os.ReadFile(filepath.Join(outside, "secret.txt"))
	require.Equal(t, "top secret", string(b), "outside file modified: %q", b)

	// Positive: a relative symlink that stays inside the workspace works.
	writeFile(t, filepath.Join(dir, "real", "f.txt"), "inside")
	require.NoError(t, os.Symlink("real", filepath.Join(dir, "alias")))
	b, err = ws.ReadFile(filepath.Join("alias", "f.txt"))
	require.NoError(t, err, "expected internal symlink to resolve, got %q,", b)
	require.Equal(t, "inside", string(b), "expected internal symlink to resolve, got %q, %v", b, err)

	// os.Root treats absolute symlink targets as escapes even when they point
	// back inside the root; document that behaviour.
	require.NoError(t, os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "absalias")))
	_, err = ws.ReadFile(filepath.Join("absalias", "f.txt"))
	require.Error(t, err, "expected absolute symlink target to be rejected by os.Root")
}

func TestWorkspaceReadFileSizeLimit(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorkspace(dir, 100)
	require.NoError(t, err)
	defer ws.Close()

	writeFile(t, filepath.Join(dir, "small.txt"), strings.Repeat("a", 100))
	writeFile(t, filepath.Join(dir, "big.txt"), strings.Repeat("a", 101))

	b, err := ws.ReadFile("small.txt")
	assert.NoError(t, err, "expected small file to read, got %d bytes,", len(b))
	assert.Len(t, b, 100, "expected small file to read, got %d bytes, %v", len(b), err)
	_, err = ws.ReadFile("big.txt")
	assert.Error(t, err, "expected size limit error, got")
	assert.Contains(t, err.Error(), "limit", "expected size limit error, got %v", err)
	_, err = ws.ReadFile(".")
	assert.Error(t, err, "expected error reading a directory")
}

func TestWorkspaceWriteFileAtomic(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	script := filepath.Join(dir, "run.sh")
	writeFile(t, script, "#!/bin/sh\necho old\n")
	require.NoError(t, os.Chmod(script, 0o755))

	// Positive: content replaced, executable mode preserved, no temp files left.
	require.NoError(t, ws.WriteFileAtomic("run.sh", []byte("#!/bin/sh\necho new\n")))
	info, _ := os.Stat(script)
	assert.Equal(t, fs.FileMode(0o755), info.Mode().Perm(), "expected mode 0755 preserved, got %v", info.Mode().Perm())
	b, _ := os.ReadFile(script)
	assert.Contains(t, string(b), "new", "content not replaced: %q", b)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp-", "temp file left behind: %s", e.Name())
	}

	// Positive: new nested file gets created with parents.
	require.NoError(t, ws.WriteFileAtomic(filepath.Join("a", "b", "c.txt"), []byte("x")))

	// Negative: refusing to replace a directory.
	assert.Error(t, ws.WriteFileAtomic("a", []byte("x")), "expected error writing over a directory")
}

func TestWorkspaceCreateExclusiveAndRemove(t *testing.T) {
	ws, dir := newTestWorkspace(t)

	require.NoError(t, ws.CreateExclusive("new.txt", []byte("hi")), "CreateExclusive")
	err := ws.CreateExclusive("new.txt", []byte("again"))
	assert.ErrorIs(t, err, fs.ErrExist, "expected fs.ErrExist, got %v", err)

	assert.NoError(t, ws.RemoveFile("new.txt"), "RemoveFile")
	assert.Error(t, ws.RemoveFile("new.txt"), "expected error removing missing file")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	assert.Error(t, ws.RemoveFile("sub"), "expected RemoveFile to refuse directories")
	assert.Error(t, ws.RemoveFile("."), "expected RemoveFile to refuse the workspace root")
}

func TestWorkspaceAbs(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	got, err := ws.Abs("sub")
	assert.NoError(t, err, "Abs(sub) = %q,", got)
	assert.Equal(t, filepath.Join(ws.Dir(), "sub"), got, "Abs(sub) = %q, %v", got, err)
	_, err = ws.Abs("missing")
	assert.Error(t, err, "expected error for missing path")
}

func TestWorkspaceWriteThroughSymlink(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "target.txt"), "old")
	if err := os.Symlink("target.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	rt := toolOf(t)(NewReplaceInFileTool(ws, allowAll()))
	out := runTool(t, rt, map[string]any{"path": "link.txt", "target_content": "old", "replacement_content": "new"})
	require.Equal(t, "", errOf(out), "edit through symlink failed: %v", out)
	// Positive: target updated, link preserved.
	b, _ := os.ReadFile(filepath.Join(dir, "target.txt"))
	assert.Equal(t, "new", string(b), "target not updated: %q", b)
	info, _ := os.Lstat(filepath.Join(dir, "link.txt"))
	assert.NotEqual(t, fs.FileMode(0), info.Mode()&os.ModeSymlink, "symlink was replaced by a regular file")
}
