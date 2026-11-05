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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuleMatchesCall checks a hook's if against calls: a shell rule
// against each command, and a script that doesn't parse runs the hook
// (N26 engine-tools-1#2); apply_patch's files come from its patch, and
// open_page and lsp have targets (#20).
func TestRuleMatchesCall(t *testing.T) {
	patch := "*** Begin Patch\n*** Update File: migrations/1.sql\n@@\n-a\n+b\n*** Delete File: old/x.go\n*** End Patch"
	for _, tc := range []struct {
		name string
		rule string
		tool string
		args map[string]any
		want bool
	}{
		{"shell command", "shell(git push *)", "run_shell_command", map[string]any{"command": "ls && git push origin"}, true},
		{"shell other", "shell(git push *)", "run_shell_command", map[string]any{"command": "ls"}, false},
		{"shell unparsable", "shell(git push *)", "run_shell_command", map[string]any{"command": "ls; bash -c 'if'"}, true},
		{"shell unparsable statement", "shell(git push *)", "run_shell_command", map[string]any{"command": "ls &&"}, true},
		{"patch write", "write(migrations/**)", "apply_patch", map[string]any{"patch": patch}, true},
		{"patch delete", "delete(old/**)", "apply_patch", map[string]any{"patch": patch}, true},
		{"patch other", "write(docs/**)", "apply_patch", map[string]any{"patch": patch}, false},
		{"patch unparsable", "write(migrations/**)", "apply_patch", map[string]any{"patch": "*** Update File: migrations/1.sql\n"}, true},
		{"open_page host", "web(evil.example)", "open_page", map[string]any{"url": "evil.example/x"}, true},
		{"open_page other", "web(evil.example)", "open_page", map[string]any{"url": "https://ok.example"}, false},
		{"lsp read", "read(secret/**)", "lsp", map[string]any{"path": "secret/a.go"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := ParsePermissionRule(EffectDeny, tc.rule, "test")
			require.NoError(t, err)
			assert.Equal(t, tc.want, r.matchesCall(tc.tool, tc.args))
		})
	}
}

// TestPatchTargets checks the files a patch names, parsed or not.
func TestPatchTargets(t *testing.T) {
	for _, tc := range []struct {
		name             string
		patch            string
		written, deleted []string
	}{
		{"begin patch", "*** Begin Patch\n*** Update File: a.go\n*** Move to: b.go\n*** Add File: c.go\n+x\n*** Delete File: d.go\n*** End Patch",
			[]string{"a.go", "b.go", "c.go"}, []string{"a.go", "d.go"}},
		{"unified", "--- a/src/x.go\t2026-01-01\n+++ b/src/x.go\n@@ -1 +1 @@\n-a\n+b\n--- a/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-a\n",
			[]string{"src/x.go"}, []string{"gone.go"}},
		{"unified added", "--- /dev/null\n+++ b/new.go\n@@ -0,0 +1 @@\n+x\n", []string{"new.go"}, nil},
		{"unparsable", "*** Update File: a.go\n*** Delete File: d.go\n--- a/e.go\n", []string{"a.go", "e.go"}, []string{"d.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			written, deleted := PatchTargets(tc.patch)
			assert.Equal(t, tc.written, written)
			assert.Equal(t, tc.deleted, deleted)
		})
	}
}
