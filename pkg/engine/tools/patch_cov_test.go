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

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A unified diff adds, deletes and renames files through /dev/null and
// differing headers, ignores timestamps, "\ No newline" markers and git
// metadata between hunks.
func TestParseUnifiedFileOperations(t *testing.T) {
	patches, err := parsePatch("diff --git a/new.txt b/new.txt\n" +
		"--- /dev/null\n+++ b/new.txt\t2026-01-01 00:00:00\n@@ -0,0 +1,2 @@\n+one\n+two\n\\ No newline at end of file\n" +
		"index 123..456\n" +
		"--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n" +
		"--- a/old.txt\n+++ b/renamed.txt\n" +
		"--- a/edit.txt\n+++ b/edit.txt\n@@ -1 +1 @@\n-a\n+b\ntrailing prose\n")
	require.NoError(t, err)
	require.Len(t, patches, 4)
	assert.Equal(t, opAdd, patches[0].op)
	assert.Equal(t, "new.txt", patches[0].path)
	assert.Equal(t, "one\ntwo\n", patches[0].content)
	assert.Nil(t, patches[0].hunks)
	assert.Equal(t, opDelete, patches[1].op)
	assert.Equal(t, "gone.txt", patches[1].path)
	assert.Equal(t, opUpdate, patches[2].op)
	assert.Equal(t, "renamed.txt", patches[2].moveTo)
	require.Len(t, patches[3].hunks, 1, "the hunk ended at the prose")
	assert.Equal(t, []string{"b"}, patches[3].hunks[0].new)
}

// Unified diffs that can't be applied are rejected with the reason.
func TestParseUnifiedErrors(t *testing.T) {
	cases := map[string]struct{ patch, want string }{
		"both null":  {"--- /dev/null\n+++ /dev/null\n", "both sides are /dev/null"},
		"bad header": {"--- a/x\n+++ b/x\n@@ nonsense\n", "malformed hunk header"},
		"no hunks":   {"--- a/x\n+++ b/x\n", "no hunks"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parsePatch(c.patch)
			assert.ErrorContains(t, err, c.want)
		})
	}
}

// A Begin Patch update may have several anchored hunks and an End of File
// marker.
func TestParseBeginPatchSeveralHunks(t *testing.T) {
	patches, err := parsePatch("*** Begin Patch\n*** Update File: main.go\n@@ func main() {\n-\tfmt.Println(\"hello\")\n+\tfmt.Println(\"hi\")\n@@ func helper() int {\n-\treturn 1\n+\treturn 3\n*** End of File\n*** End Patch")
	require.NoError(t, err)
	require.Len(t, patches[0].hunks, 2)
	got, err := applyHunks(goFile, patches[0].hunks)
	require.NoError(t, err)
	assert.Contains(t, got, `"hi"`)
	assert.Contains(t, got, "return 3")
}

// Insertions go at the hinted line, after an anchor, or at the end; an
// anchor may match part of a line; a missing anchor is an error.
func TestApplyHunksPlacement(t *testing.T) {
	src := "a\nb\nc\n"
	cases := map[string]struct {
		hunk patchHunk
		want string
	}{
		"at the hint":         {patchHunk{hint: 1, new: []string{"X"}}, "a\nX\nb\nc\n"},
		"after an anchor":     {patchHunk{hint: -1, anchor: "b", new: []string{"X"}}, "a\nb\nX\nc\n"},
		"after the last line": {patchHunk{hint: -1, anchor: "c", new: []string{"X"}}, "a\nb\nc\nX\n"},
		"at the end":          {patchHunk{hint: -1, new: []string{"X"}}, "a\nb\nc\nX\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := applyHunks(src, []patchHunk{c.hunk})
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
	got, err := applyHunks("func helper() {\n", []patchHunk{{hint: -1, anchor: "helper", new: []string{"x"}}})
	require.NoError(t, err)
	assert.Equal(t, "func helper() {\nx\n", got, "the anchor matched part of a line")
	_, err = applyHunks(src, []patchHunk{{hint: -1, anchor: "zzz", new: []string{"X"}}})
	assert.ErrorContains(t, err, "anchor \"zzz\" not found")
	got, err = applyHunks("", []patchHunk{{hint: -1, new: []string{"first"}}})
	require.NoError(t, err)
	assert.Equal(t, "first\n", got, "insertion into an empty file")
}

// Hunks given out of order still apply: the search wraps to the start.
func TestApplyHunksOutOfOrder(t *testing.T) {
	got, err := applyHunks("a\nb\nc\n", []patchHunk{
		{hint: -1, old: []string{"c"}, new: []string{"C"}},
		{hint: -1, old: []string{"a"}, new: []string{"A"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "A\nb\nC\n", got)
}

// A long block that can't be found is shown abridged in the error.
func TestApplyHunksLongPreview(t *testing.T) {
	old := strings.Split("1 2 3 4 5 6 7 8 9 10", " ")
	_, err := applyHunks("x\n", []patchHunk{{hint: -1, old: old, new: nil}})
	assert.ErrorContains(t, err, "(2 more lines)")
}

// The tool reports patches it can't parse, can't plan or can't lock.
func TestApplyPatchToolRejects(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	writeFile(t, filepath.Join(dir, "taken.txt"), "t\n")
	rt := toolOf(t)(NewApplyPatchTool(ws, allowAll()))
	cases := map[string]struct{ patch, want string }{
		"unparseable":   {"nothing here", "invalid patch"},
		"update absent": {"*** Begin Patch\n*** Update File: nope.txt\n-a\n+b\n*** End Patch", "cannot update"},
		"bad hunk":      {"*** Begin Patch\n*** Update File: a.txt\n-zzz\n+b\n*** End Patch", "could not find"},
		"move outside":  {"*** Begin Patch\n*** Update File: a.txt\n*** Move to: ../out.txt\n-a\n+b\n*** End Patch", "../out.txt"},
		"move onto":     {"*** Begin Patch\n*** Update File: a.txt\n*** Move to: taken.txt\n-a\n+b\n*** End Patch", "destination exists"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out := runTool(t, rt, map[string]any{"patch": c.patch})
			assert.Contains(t, errOf(out), c.want)
		})
	}

	t.Run("locked and cancelled", func(t *testing.T) {
		rel, err := ws.WritablePath("a.txt")
		require.NoError(t, err)
		unlock, err := ws.lockPaths(context.Background(), rel)
		require.NoError(t, err)
		defer unlock()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		out, err := rt.Run(createTestToolContextWith(ctx), map[string]any{"patch": "*** Begin Patch\n*** Update File: a.txt\n-a\n+b\n*** End Patch"})
		require.NoError(t, err)
		assert.Contains(t, errOf(out), "canceled")
	})
}

// A write that fails after approval is reported, and nothing is left
// changed.
func TestApplyPatchToolWriteFails(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	h := NewHooks(Policy{})
	h.SetApprover(func(ctx context.Context, req api.ApprovalRequest) (api.Decision, error) {
		// The move's destination appears while the user is asked.
		writeFile(t, filepath.Join(dir, "b.txt"), "someone else's\n")
		return api.DecisionOnce, nil
	})
	rt := toolOf(t)(NewApplyPatchTool(ws, h))
	out := runTool(t, rt, map[string]any{"patch": "*** Begin Patch\n*** Update File: a.txt\n*** Move to: b.txt\n-a\n+A\n*** End Patch"})
	assert.Contains(t, errOf(out), "no files were changed")
	assert.Equal(t, "a\n", readString(t, filepath.Join(dir, "a.txt")))
	assert.Equal(t, "someone else's\n", readString(t, filepath.Join(dir, "b.txt")))
}

// Each kind of change that fails rolls back the ones before it.
func TestExecutePlanRollsBack(t *testing.T) {
	ctx := context.Background()
	cases := map[string]plannedChange{
		"delete missing":   {path: "missing.txt", target: "missing.txt", deleteIt: true, existed: true},
		"move onto a file": {path: "a.txt", target: "taken.txt", existed: true, after: "x"},
		"move a directory": {path: "dir", target: "new.txt", existed: true, after: "x"},
		"create existing":  {path: "taken.txt", target: "taken.txt", after: "x"},
		"write a dir":      {path: "dir", target: "dir", existed: true, after: "x"},
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			ws, dir := newTestWorkspace(t)
			writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
			writeFile(t, filepath.Join(dir, "taken.txt"), "t\n")
			require.NoError(t, os.Mkdir(filepath.Join(dir, "dir"), 0o755))
			plan := []plannedChange{
				{path: "first.txt", target: "first.txt", after: "f"},
				{path: "a.txt", target: "a.txt", existed: true, before: "a\n", after: "A\n"},
				bad,
			}
			err := executePlan(ctx, ws, plan)
			assert.ErrorContains(t, err, "no files were changed")
			assert.Equal(t, "a\n", readString(t, filepath.Join(dir, "a.txt")))
			assert.NoFileExists(t, filepath.Join(dir, "first.txt"))
			assert.NoFileExists(t, filepath.Join(dir, "new.txt"))
		})
	}
}

// When rolling back fails too, both failures are reported.
func TestExecutePlanRollbackFails(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "x"), "x0")
	plan := []plannedChange{
		{path: "x", target: "x", existed: true, before: "x0", after: "x1"},
		{path: "x", target: "x", existed: true, before: "x1", deleteIt: true},
		// x is now a directory, which the rollback can't write over.
		{path: "x/y", target: "x/y", after: "y"},
		{path: "x/y", target: "x/y", after: "y"},
	}
	err := executePlan(context.Background(), ws, plan)
	assert.ErrorContains(t, err, "rollback also failed")
}
