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

const goFile = `package main

import "fmt"

func main() {
	fmt.Println("hello")
}

func helper() int {
	return 1
}
`

func TestApplyHunksUnified(t *testing.T) {
	patches, err := parsePatch(`--- a/main.go
+++ b/main.go
@@ -5,3 +5,3 @@ import "fmt"
 func main() {
-	fmt.Println("hello")
+	fmt.Println("goodbye")
 }
@@ -9,3 +9,4 @@ func main() {
 func helper() int {
-	return 1
+	x := 2
+	return x
 }
`)
	require.NoError(t, err)
	require.Len(t, patches, 1, "parse: %+v", patches)
	require.Len(t, patches[0].hunks, 2, "parse: %+v", patches)
	require.Equal(t, "main.go", patches[0].path, "parse: %+v", patches)
	got, err := applyHunks(goFile, patches[0].hunks)
	require.NoError(t, err)
	want := strings.Replace(strings.Replace(goFile, `"hello"`, `"goodbye"`, 1), "\treturn 1\n", "\tx := 2\n\treturn x\n", 1)
	assert.Equal(t, want, got, "result:\n%s\nwant:\n%s", got, want)
}

func TestApplyHunksTolerance(t *testing.T) {
	// Wrong line numbers and trailing-whitespace differences still apply.
	hunks := []patchHunk{{hint: 40, old: []string{"\tfmt.Println(\"hello\")   "}, new: []string{"\tfmt.Println(\"hi\")"}}}
	got, err := applyHunks(goFile, hunks)
	assert.NoError(t, err, "tolerant apply failed")
	assert.Contains(t, got, `"hi"`, "tolerant apply failed: %v", err)
	// Negative: context that doesn't exist.
	_, err = applyHunks(goFile, []patchHunk{{hint: -1, old: []string{"nope"}, new: []string{"x"}}})
	assert.Error(t, err, "expected not-found error, got")
	assert.Contains(t, err.Error(), "could not find", "expected not-found error, got %v", err)
	// Ambiguous context resolves to the occurrence nearest the hint.
	src := "a\nx\nb\nx\nc\n"
	got, _ = applyHunks(src, []patchHunk{{hint: 3, old: []string{"x"}, new: []string{"Y"}}})
	assert.Equal(t, "a\nx\nb\nY\nc\n", got, "nearest-hint match: %q", got)
	// Pure insertion at EOF, no trailing newline preserved.
	got, _ = applyHunks("a\nb", []patchHunk{{hint: -1, new: []string{"c"}}})
	assert.Equal(t, "a\nb\nc", got, "insertion: %q", got)
}

func TestParseBeginPatch(t *testing.T) {
	patches, err := parsePatch(`*** Begin Patch
*** Add File: docs/new.md
+# Title
+body
*** Update File: main.go
@@ func helper() int {
-	return 1
+	return 2
*** Delete File: old.txt
*** End Patch`)
	require.NoError(t, err)
	require.Len(t, patches, 3, "expected 3 files, got %d", len(patches))
	assert.Equal(t, opAdd, patches[0].op, "add: %+v", patches[0])
	assert.Equal(t, "# Title\nbody\n", patches[0].content, "add: %+v", patches[0])
	assert.Equal(t, opUpdate, patches[1].op, "update: %+v", patches[1])
	assert.Equal(t, "func helper() int {", patches[1].hunks[0].anchor, "update: %+v", patches[1])
	got, err := applyHunks(goFile, patches[1].hunks)
	assert.NoError(t, err, "anchored apply: %v\n%s", err, got)
	assert.Contains(t, got, "return 2", "anchored apply: %v\n%s", err, got)
	assert.NotContains(t, got, "return 1", "anchored apply: %v\n%s", err, got)
	assert.Equal(t, opDelete, patches[2].op, "delete: %+v", patches[2])

	for name, bad := range map[string]string{
		"no end":        "*** Begin Patch\n*** Add File: x\n+a\n",
		"empty":         "*** Begin Patch\n*** End Patch",
		"stray line":    "*** Begin Patch\nhello\n*** End Patch",
		"bad add line":  "*** Begin Patch\n*** Add File: x\nno plus\n*** End Patch",
		"empty update":  "*** Begin Patch\n*** Update File: x\n*** End Patch",
		"not a patch":   "just some text",
		"hunk w/o file": "@@ -1 +1 @@\n-a\n+b\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parsePatch(bad)
			assert.Error(t, err, "%s: expected parse error", name)
		})
	}
}

func TestApplyPatchTool(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "main.go"), goFile)
	writeFile(t, filepath.Join(dir, "old.txt"), "bye\n")
	writeFile(t, filepath.Join(dir, "move.txt"), "m\n")
	h, reqs := approverHooks(true)
	rt := toolOf(t)(NewApplyPatchTool(ws, h))

	out := runTool(t, rt, map[string]any{"patch": `*** Begin Patch
*** Add File: docs/new.md
+hello
*** Update File: main.go
@@
-	return 1
+	return 2
*** Update File: move.txt
*** Move to: moved/move.txt
@@
-m
+M
*** Delete File: old.txt
*** End Patch`})
	require.Equal(t, true, out["success"], "apply failed: %v", out)
	b, _ := os.ReadFile(filepath.Join(dir, "docs", "new.md"))
	assert.Equal(t, "hello\n", string(b), "add: %q", b)
	b, _ = os.ReadFile(filepath.Join(dir, "main.go"))
	assert.Contains(t, string(b), "return 2", "update not applied")
	_, err := os.Stat(filepath.Join(dir, "old.txt"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "delete not applied")
	b, _ = os.ReadFile(filepath.Join(dir, "moved", "move.txt"))
	assert.Equal(t, "M\n", string(b), "move: %q", b)
	_, err = os.Stat(filepath.Join(dir, "move.txt"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "moved source still exists")
	assert.Len(t, *reqs, 1, "single approval with combined diff expected: %+v", *reqs)
	assert.Contains(t, (*reqs)[0].Diff, "+hello", "single approval with combined diff expected: %+v", *reqs)
	assert.Contains(t, (*reqs)[0].Diff, "-\treturn 1", "single approval with combined diff expected: %+v", *reqs)
	files := out["files"].([]any)
	assert.Len(t, files, 4, "summary: %v", files)
}

func TestApplyPatchAllOrNothing(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "b\n")
	h, reqs := approverHooks(true)
	rt := toolOf(t)(NewApplyPatchTool(ws, h))

	// Second file's hunk doesn't match: nothing is written, nobody is asked.
	out := runTool(t, rt, map[string]any{"patch": "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-a\n+A\n--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-zzz\n+B\n"})
	assert.NotEqual(t, true, out["success"], "expected failure naming b.txt: %v", out)
	assert.Contains(t, errOf(out), "b.txt", "expected failure naming b.txt: %v", out)
	b, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	assert.Equal(t, "a\n", string(b), "partial patch was applied")
	assert.Len(t, *reqs, 0, "user asked to approve an invalid patch")

	// Negative: denial, sandbox escapes, duplicate files, add over existing.
	denied, _ := approverHooks(false)
	out = runTool(t, toolOf(t)(NewApplyPatchTool(ws, denied)), map[string]any{"patch": "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-a\n+A\n"})
	assert.Contains(t, errOf(out), "not approved", "expected denial: %v", out)
	for name, p := range map[string]string{
		"escape":    "*** Begin Patch\n*** Add File: ../evil.txt\n+x\n*** End Patch",
		"duplicate": "*** Begin Patch\n*** Update File: a.txt\n-a\n+1\n*** Update File: a.txt\n-a\n+2\n*** End Patch",
		"add exist": "*** Begin Patch\n*** Add File: a.txt\n+x\n*** End Patch",
		"del miss":  "*** Begin Patch\n*** Delete File: nope.txt\n*** End Patch",
	} {
		t.Run(name, func(t *testing.T) {
			out := runTool(t, rt, map[string]any{"patch": p})
			assert.NotEqual(t, true, out["success"], "%s: expected failure", name)
		})
	}
	_, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.txt"))
	assert.Error(t, err, "patch escaped the workspace")
}

func TestApplyPatchIsUndoable(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	cp.Begin("patch")
	out := runTool(t, toolOf(t)(NewApplyPatchTool(ws, allowAll())), map[string]any{"patch": "*** Begin Patch\n*** Update File: a.txt\n-a\n+A\n*** Add File: n.txt\n+n\n*** End Patch"})
	require.Equal(t, true, out["success"], "%v", out)
	_, err := cp.Undo(false)
	require.NoError(t, err)
	b, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	assert.Equal(t, "a\n", string(b), "undo did not revert patch")
	_, err = os.Stat(filepath.Join(dir, "n.txt"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "undo did not remove added file")
}

func TestApplyHunksCRLF(t *testing.T) {
	src := "line1\r\nold\r\nline3\r\n"
	got, err := applyHunks(src, []patchHunk{{hint: -1, old: []string{"line1", "old"}, new: []string{"line1", "new"}}})
	require.NoError(t, err)
	assert.Equal(t, "line1\r\nnew\r\nline3\r\n", got, "CRLF not preserved: %q", got)
}
