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

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hookRepo is a git repository with a first commit, a staged change and
// two workflows: review on pre-commit (an organization's API: without an
// organization it fails), notes on pre-push (reads a file: it passes).
func hookRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	flows := filepath.Join(dir, ".agents", "workflows")
	require.NoError(t, os.MkdirAll(flows, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(flows, "review.flow.yaml"), []byte("triggers: [git.pre_commit]\nnodes: [{id: a, type: api_call, connector: review, method: POST, url: /check}]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(flows, "notes.flow.yaml"), []byte("triggers: [git.pre_push]\nnodes: [{id: a, type: read_file, path: README.md}]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644))
	git(t, dir, "init", "-q")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "first")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi there\n"), 0o644))
	git(t, dir, "add", "README.md")
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	return strings.TrimSpace(string(out))
}

// WF-32: blitz hook run runs the workflows on a hook: one that fails
// exits 1 (the commit stopped), one that passes 0; a hook none can run on
// is a usage error.
func TestHookCommand(t *testing.T) {
	isolate(t)
	dir := hookRepo(t)
	head := git(t, dir, "rev-parse", "HEAD")
	zeros := strings.Repeat("0", 40)
	tests := []struct {
		name  string
		event string
		stdin string
		code  int
		says  string
	}{
		{"pre-commit fails", "pre-commit", "", exitFailure, "✗ review"},
		{"pre-push, a new branch", "pre-push", "refs/heads/main " + head + " refs/heads/main " + zeros + "\n", 0, "✓ notes"},
		{"pre-push, a deleted branch", "pre-push", "(delete) " + zeros + " refs/heads/old " + head + "\n", 0, "✓ notes"},
		{"pre-push, an update", "pre-push", "refs/heads/main " + head + " refs/heads/main " + head + "\n", 0, "✓ notes"},
		{"an unknown hook", "post-merge", "", exitUsage, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runCLIWith(t, tt.stdin, "-d", dir, "hook", "run", tt.event)
			assert.Equal(t, tt.code, exitCodeFor(err), "%v\n%s", err, out)
			assert.Contains(t, out, tt.says)
		})
	}
	out, err := runCLIWith(t, "", "-d", dir, "hook", "run", "pre-commit")
	require.Error(t, err)
	assert.Contains(t, out, "isn't connected to an organization", "why it failed")
	assert.ErrorContains(t, err, "pre-commit: 1 of its workflows failed")
}

// A diff past what a hook's workflows read is cut, and says so; outside a
// repository git's failure is the hook's.
func TestHookInput(t *testing.T) {
	dir := hookRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat("x\n", maxHookInput)), 0o644))
	git(t, dir, "add", "big.txt")
	got, err := hookInput(context.Background(), dir, "pre-commit", strings.NewReader(""))
	require.NoError(t, err)
	assert.Less(t, len(got), maxHookInput+100)
	assert.Contains(t, got, "more characters cut")

	_, err = hookInput(context.Background(), t.TempDir(), "pre-commit", strings.NewReader(""))
	assert.ErrorContains(t, err, "git diff --cached")
	_, err = hookInput(context.Background(), dir, "pre-push", strings.NewReader("refs/heads/main nothere refs/heads/main "+strings.Repeat("1", 40)+"\n"))
	assert.Error(t, err, "a commit git doesn't know")
}
