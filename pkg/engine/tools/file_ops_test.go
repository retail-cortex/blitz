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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runToolCtx runs rt with a tool context over ctx.
func runToolCtx(t *testing.T, ctx context.Context, rt runnerTool, args map[string]any) map[string]any {
	t.Helper()
	out, err := rt.Run(createTestToolContextWith(ctx), args)
	require.NoError(t, err, "tool run returned error")
	return out
}

// cancelledWhileLocked holds the path lock on rel and returns a cancelled
// context, so a tool waiting for the lock gives up.
func cancelledWhileLocked(t *testing.T, ws *Workspace, rel string) context.Context {
	t.Helper()
	unlock, err := ws.lockPaths(context.Background(), rel)
	require.NoError(t, err)
	t.Cleanup(unlock)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// readOnlyDir makes dir unwritable for the test (skipped as root, which
// writes anyway).
func readOnlyDir(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}

// TestReadFileToolPastTheEndOfALongLine marks the output truncated when the
// last line, shown for a start past the end, is over the output limit.
func TestReadFileToolPastTheEndOfALongLine(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorkspace(dir, 2*maxReadOutputBytes)
	require.NoError(t, err)
	defer ws.Close()
	writeFile(t, filepath.Join(dir, "one.txt"), strings.Repeat("x", maxReadOutputBytes+1))
	out := runTool(t, toolOf(t)(NewReadFileTool(ws)), map[string]any{"path": "one.txt", "start_line": 5})
	assert.Equal(t, true, out["truncated"])
}

// TestListFilesToolSkipsHiddenFilesAndStopsWhenCancelled leaves out dot
// files, and reports a cancelled listing as an error.
func TestListFilesToolSkipsHiddenFilesAndStopsWhenCancelled(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, ".env"), "x")
	writeFile(t, filepath.Join(dir, "a.txt"), "a")
	rt := toolOf(t)(NewListFilesTool(ws))
	out := runTool(t, rt, map[string]any{})
	files, _ := out["files"].([]any)
	require.Len(t, files, 1)
	assert.Equal(t, "a.txt", files[0].(map[string]any)["path"])

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out = runToolCtx(t, ctx, rt, map[string]any{})
	assert.Contains(t, errOf(out), "context canceled")
}

// TestFileToolsGiveUpWaitingForALockWhenCancelled returns an error from
// each writing tool whose turn ends while another edit holds the file.
func TestFileToolsGiveUpWaitingForALockWhenCancelled(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "f.txt"), "hello\n")
	ctx := cancelledWhileLocked(t, ws, "f.txt")
	cases := map[string]struct {
		rt   runnerTool
		args map[string]any
	}{
		"create_file":     {toolOf(t)(NewCreateFileTool(ws, allowAll())), map[string]any{"path": "f.txt", "content": "x", "overwrite": true}},
		"delete_file":     {toolOf(t)(NewDeleteFileTool(ws, allowAll())), map[string]any{"path": "f.txt"}},
		"replace_in_file": {toolOf(t)(NewReplaceInFileTool(ws, allowAll())), map[string]any{"path": "f.txt", "target_content": "hello", "replacement_content": "bye"}},
		"delete_snippet":  {toolOf(t)(NewDeleteSnippetTool(ws, allowAll())), map[string]any{"path": "f.txt", "snippet": "hello"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out := runToolCtx(t, ctx, c.rt, c.args)
			assert.Contains(t, errOf(out), "context canceled")
		})
	}
	b, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(b), "nothing was written")
}

// TestFileToolsReportWriteFailures returns the error when the file can't
// be written (its folder is read-only).
func TestFileToolsReportWriteFailures(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	sub := filepath.Join(dir, "sub")
	writeFile(t, filepath.Join(sub, "f.txt"), "hello\n")
	readOnlyDir(t, sub)
	cases := map[string]struct {
		rt   runnerTool
		args map[string]any
		want string
	}{
		"create_file":     {toolOf(t)(NewCreateFileTool(ws, allowAll())), map[string]any{"path": "sub/f.txt", "content": "x", "overwrite": true}, "failed to write file"},
		"replace_in_file": {toolOf(t)(NewReplaceInFileTool(ws, allowAll())), map[string]any{"path": "sub/f.txt", "target_content": "hello", "replacement_content": "bye"}, "failed to write updated file"},
		"delete_snippet":  {toolOf(t)(NewDeleteSnippetTool(ws, allowAll())), map[string]any{"path": "sub/f.txt", "snippet": "hello"}, "failed to save file"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out := runTool(t, c.rt, c.args)
			assert.Contains(t, errOf(out), c.want)
		})
	}
}

// TestFileEditToolsRefuseBadPaths reports paths outside the workspace and
// files that can't be read.
func TestFileEditToolsRefuseBadPaths(t *testing.T) {
	ws, _ := newTestWorkspace(t)
	replace := toolOf(t)(NewReplaceInFileTool(ws, allowAll()))
	snippet := toolOf(t)(NewDeleteSnippetTool(ws, allowAll()))

	out := runTool(t, snippet, map[string]any{"path": "../x", "snippet": "a"})
	assert.Contains(t, errOf(out), "outside the workspace")
	out = runTool(t, snippet, map[string]any{"path": "missing", "snippet": "a"})
	assert.Contains(t, errOf(out), "failed to read file")
	out = runTool(t, replace, map[string]any{"path": "missing", "target_content": "a", "replacement_content": "b"})
	assert.Contains(t, errOf(out), "failed to read file")
}
