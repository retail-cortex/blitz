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

package engine

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func toolCall(name string, args map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: name, Args: args}}}}
}

// The agent can write .git/config; showing the diff must not run what it
// names there.
func TestGitDiffRunsNothingFromRepoConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	w := openTest(t)
	dir := w.Dir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %v\n%s", args, err, out)
	}
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	git("init", "-q")
	write("f.txt", "before\n")
	git("add", "f.txt")
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")

	write(".gitattributes", "*.txt filter=evil diff=evil\n")
	for key, cmd := range map[string]string{
		"core.fsmonitor":      "touch pwned-fsmonitor",
		"diff.external":       "touch pwned-external",
		"diff.evil.textconv":  "touch pwned-textconv; cat",
		"filter.evil.clean":   "touch pwned-clean; cat",
		"filter.evil.process": "touch pwned-process",
	} {
		git("config", key, cmd)
	}
	git("config", "filter.evil.required", "true")
	write("f.txt", "after\n")

	out, err := w.GitDiff(context.Background(), false)
	require.NoError(t, err, "diff %v:\n%s", err, out)
	require.Contains(t, out, "+after", "diff %v:\n%s", err, out)
	got, _ := filepath.Glob(filepath.Join(dir, "pwned-*"))
	assert.LessOrEqual(t, len(got), 0, "git diff ran commands from the repository's config: %v", got)
}

// The status bar's git state: the branch (or short commit when detached)
// and the changed files, without running what the repository's config
// names; outside a repository, Repo is false.
func TestGitStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	w := openTest(t)
	dir := w.Dir()
	ctx := context.Background()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "init.defaultBranch=main"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %v\n%s", args, err, out)
	}
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}

	st, err := w.GitStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, api.GitStatus{}, st, "not a repository")

	git("init", "-q")
	write("a.txt", "a\n")
	write("b.txt", "b\n")
	git("add", ".")
	git("commit", "-qm", "init")
	st, err = w.GitStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, api.GitStatus{Repo: true, Branch: "main"}, st)

	git("checkout", "-qb", "feature")
	write(".gitattributes", "*.txt filter=evil\n")
	git("config", "filter.evil.clean", "touch pwned-clean; cat")
	git("config", "core.fsmonitor", "touch pwned-fsmonitor")
	write("a.txt", "changed\n")
	write("new.txt", "new\n")
	st, err = w.GitStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, api.GitStatus{Repo: true, Branch: "feature", Changed: 3}, st, "a.txt, new.txt and .gitattributes")
	got, _ := filepath.Glob(filepath.Join(dir, "pwned-*"))
	assert.Empty(t, got, "git status ran commands from the repository's config")

	git("-c", "core.fsmonitor=false", "-c", "filter.evil.clean=", "checkout", "-q", "--detach")
	st, err = w.GitStatus(ctx)
	require.NoError(t, err)
	assert.Regexp(t, `^[0-9a-f]{7,}$`, st.Branch, "a detached head shows its commit")
}

// Checkpoints outlive the process: after reopening the workspace and
// resuming the session, /undo and /diff still work.
func TestUndoAfterResume(t *testing.T) {
	var cfg *config.Config
	w, _ := openTestWith(t, func(c *config.Config) {
		c.Blitz.AutoApprove = false
		cfg = c
	}, toolCall("create_file", map[string]any{"path": "notes.txt", "content": "hi\n"}), text("created"))
	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil }, nil)
	s, _, err := w.OpenSession("", false)
	require.NoError(t, err)
	_, runErr := w.Run(context.Background(), s.ID, api.Turn{Text: "make notes"}, func(api.Event) {})
	require.NoError(t, runErr)
	cps := w.ListCheckpoints()
	require.Len(t, cps, 1, "checkpoints %+v", cps)
	w.Close()

	w2, err := Open(context.Background(), cfg, Options{Model: runtime.NewMockLLM("gemini-3.8-flash"), NewModel: mockModels})
	require.NoError(t, err)
	defer w2.Close()
	_, _, err = w2.OpenSession(s.ID, false)
	require.NoError(t, err)
	d := w2.SessionDiff()
	assert.Contains(t, d, "+hi", "diff after resuming:\n%s", d)
	res, err := w2.Undo(false)
	require.NoError(t, err, "undo after reopening: %+v", res)
	require.Equal(t, "make notes", res.Label, "undo after reopening: %+v %v", res, err)
	_, err = os.Stat(filepath.Join(cfg.Tools.WorkspaceDir, "notes.txt"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "notes.txt is still there")
}
