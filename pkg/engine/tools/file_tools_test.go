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

func TestReadFileTool(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "f.txt"), "one\ntwo\nthree\nfour\n")
	rt := toolOf(t)(NewReadFileTool(ws))

	// Positive: slicing returns only the requested lines, numbered.
	out := runTool(t, rt, map[string]any{"path": "f.txt", "start_line": 2, "end_line": 3})
	content := out["content"].(string)
	assert.Equal(t, "   2: two\n   3: three\n", content, "unexpected slice content %q", content)
	assert.Equal(t, float64(5), out["total_lines"].(float64), "expected 5 total lines, got %v", out["total_lines"])

	// Positive: out-of-range start clamps to the last line; end < start clamps to start.
	out = runTool(t, rt, map[string]any{"path": "f.txt", "start_line": 99})
	assert.Equal(t, "", errOf(out), "unexpected clamped output %v", out)
	assert.Equal(t, "   5: \n", out["content"].(string), "unexpected clamped output %v", out)
	out = runTool(t, rt, map[string]any{"path": "f.txt", "start_line": 3, "end_line": 1})
	assert.Equal(t, "   3: three\n", out["content"].(string), "expected end<start to clamp to start, got %q", out["content"])

	// Negative: outside the workspace, missing, and directories.
	for _, p := range []string{"../escape.txt", "/etc/hosts", "missing.txt", "."} {
		out := runTool(t, rt, map[string]any{"path": p})
		assert.NotEqual(t, "", errOf(out), "expected error reading %q, got %v", p, out)
	}
}

func TestReadFileToolLimits(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorkspace(dir, 2*maxReadOutputBytes)
	require.NoError(t, err)
	defer ws.Close()
	rt := toolOf(t)(NewReadFileTool(ws))

	// Output larger than maxReadOutputBytes is truncated with a paging hint.
	line := strings.Repeat("x", 99) + "\n"
	writeFile(t, filepath.Join(dir, "large.txt"), strings.Repeat(line, (maxReadOutputBytes/100)+500))
	out := runTool(t, rt, map[string]any{"path": "large.txt"})
	assert.Equal(t, true, out["truncated"], "expected truncated=true")
	assert.LessOrEqual(t, len(out["content"].(string)), maxReadOutputBytes+200, "content exceeds cap: %d bytes", len(out["content"].(string)))

	// Files above the workspace size limit are refused outright.
	writeFile(t, filepath.Join(dir, "huge.txt"), strings.Repeat("y", 2*maxReadOutputBytes+1))
	out = runTool(t, rt, map[string]any{"path": "huge.txt"})
	assert.Contains(t, errOf(out), "limit", "expected size limit error, got %v", out)
}

func TestListFilesTool(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "a")
	writeFile(t, filepath.Join(dir, "sub", "b.txt"), "b")
	writeFile(t, filepath.Join(dir, ".git", "config"), "x")
	rt := toolOf(t)(NewListFilesTool(ws))

	paths := func(out map[string]any) []string {
		var ps []string
		files, _ := out["files"].([]any)
		for _, f := range files {
			ps = append(ps, f.(map[string]any)["path"].(string))
		}
		return ps
	}

	// Positive: non-recursive lists top-level only and hides dot-dirs.
	got := strings.Join(paths(runTool(t, rt, map[string]any{})), ",")
	assert.Equal(t, "a.txt,sub", got, "non-recursive listing = %q", got)
	// Positive: recursive includes nested files.
	got = strings.Join(paths(runTool(t, rt, map[string]any{"recursive": true})), ",")
	assert.Equal(t, "a.txt,sub,sub/b.txt", got, "recursive listing = %q", got)
	// Positive: max_entries is honoured.
	n := len(paths(runTool(t, rt, map[string]any{"recursive": true, "max_entries": 1})))
	assert.Equal(t, 1, n, "expected 1 entry, got %d", n)

	// Negative: outside workspace and missing directory.
	out := runTool(t, rt, map[string]any{"directory": ".."})
	assert.NotEqual(t, "", errOf(out), "expected error listing outside workspace")
	out = runTool(t, rt, map[string]any{"directory": "nope"})
	assert.NotEqual(t, "", errOf(out), "expected error listing missing directory")
}

func TestCreateFileTool(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	rt := toolOf(t)(NewCreateFileTool(ws, allowAll()))

	// Positive: creates nested file.
	out := runTool(t, rt, map[string]any{"path": "new/dir/f.txt", "content": "hello"})
	require.Equal(t, true, out["success"], "create failed: %v", out)
	require.Equal(t, float64(5), out["bytes_written"].(float64), "create failed: %v", out)

	// Negative: existing file without overwrite.
	out = runTool(t, rt, map[string]any{"path": "new/dir/f.txt", "content": "again"})
	assert.Contains(t, errOf(out), "already exists", "expected already-exists error, got %v", out)
	// Positive: overwrite=true replaces.
	out = runTool(t, rt, map[string]any{"path": "new/dir/f.txt", "content": "again", "overwrite": true})
	b, _ := os.ReadFile(filepath.Join(dir, "new", "dir", "f.txt"))
	assert.Equal(t, true, out["success"], "overwrite failed: %v, content %q", out, b)
	assert.Equal(t, "again", string(b), "overwrite failed: %v, content %q", out, b)

	// Negative: outside workspace, and the workspace root itself.
	outside := filepath.Join(t.TempDir(), "evil.txt")
	for _, p := range []string{"../evil.txt", outside, "."} {
		out := runTool(t, rt, map[string]any{"path": p, "content": "x"})
		assert.NotEqual(t, "", errOf(out), "expected error creating %q", p)
	}
	_, err := os.Stat(outside)
	assert.ErrorIs(t, err, fs.ErrNotExist, "file created outside workspace")
}

func TestDeleteFileTool(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "gone.txt"), "x")
	rt := toolOf(t)(NewDeleteFileTool(ws, allowAll()))

	out := runTool(t, rt, map[string]any{"path": "gone.txt"})
	assert.Equal(t, true, out["success"], "delete failed: %v", out)
	_, err := os.Stat(filepath.Join(dir, "gone.txt"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "file still exists after delete")

	outsideDir := t.TempDir()
	victim := filepath.Join(outsideDir, "victim.txt")
	writeFile(t, victim, "keep me")
	for _, p := range []string{victim, "../victim.txt", "missing.txt", "."} {
		out := runTool(t, rt, map[string]any{"path": p})
		assert.NotEqual(t, "", errOf(out), "expected error deleting %q", p)
	}
	_, err = os.Stat(victim)
	assert.NoError(t, err, "file outside workspace was deleted")
}

func TestReplaceInFileTool(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	path := filepath.Join(dir, "code.go")
	rt := toolOf(t)(NewReplaceInFileTool(ws, allowAll()))

	reset := func() { writeFile(t, path, "foo bar foo\n") }

	// Negative: empty target must be rejected (previously corrupted the file).
	reset()
	out := runTool(t, rt, map[string]any{"path": "code.go", "target_content": "", "replacement_content": "X", "allow_multiple": true})
	assert.NotEqual(t, "", errOf(out), "expected error for empty target_content")
	b, _ := os.ReadFile(path)
	assert.Equal(t, "foo bar foo\n", string(b), "file modified on rejected edit: %q", b)

	// Negative: ambiguous target without allow_multiple; missing target.
	out = runTool(t, rt, map[string]any{"path": "code.go", "target_content": "foo", "replacement_content": "baz"})
	assert.Contains(t, errOf(out), "matched 2 times", "expected ambiguity error, got %v", out)
	out = runTool(t, rt, map[string]any{"path": "code.go", "target_content": "absent", "replacement_content": "x"})
	assert.Contains(t, errOf(out), "not found", "expected not-found error, got %v", out)

	// Positive: allow_multiple replaces all.
	out = runTool(t, rt, map[string]any{"path": "code.go", "target_content": "foo", "replacement_content": "baz", "allow_multiple": true})
	assert.Equal(t, float64(2), out["replacements_count"].(float64), "expected 2 replacements, got %v", out)
	b, _ = os.ReadFile(path)
	assert.Equal(t, "baz bar baz\n", string(b), "unexpected content %q", b)

	// Negative: outside the workspace.
	out = runTool(t, rt, map[string]any{"path": "../x.go", "target_content": "a", "replacement_content": "b"})
	assert.NotEqual(t, "", errOf(out), "expected error editing outside workspace")
}

func TestDeleteSnippetTool(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	path := filepath.Join(dir, "s.txt")
	writeFile(t, path, "keep REMOVE keep")
	rt := toolOf(t)(NewDeleteSnippetTool(ws, allowAll()))

	out := runTool(t, rt, map[string]any{"path": "s.txt", "snippet": ""})
	assert.NotEqual(t, "", errOf(out), "expected error for empty snippet")
	out = runTool(t, rt, map[string]any{"path": "s.txt", "snippet": "absent"})
	assert.NotEqual(t, "", errOf(out), "expected error for missing snippet")
	out = runTool(t, rt, map[string]any{"path": "s.txt", "snippet": "REMOVE "})
	assert.Equal(t, true, out["success"], "delete_snippet failed: %v", out)
	b, _ := os.ReadFile(path)
	assert.Equal(t, "keep keep", string(b), "unexpected content %q", b)
}
