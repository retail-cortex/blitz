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
	assert.Equal(t, api.GitStatus{Git: true}, st, "not a repository")

	git("init", "-q")
	write("a.txt", "a\n")
	st, err = w.GitStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, api.GitStatus{Git: true, Repo: true, Branch: "main", Changed: 1}, st, "no commit yet: a.txt")
	write("b.txt", "b\n")
	git("add", ".")
	git("commit", "-qm", "init")
	st, err = w.GitStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, api.GitStatus{Git: true, Repo: true, Branch: "main"}, st)

	git("checkout", "-qb", "feature")
	write(".gitattributes", "*.txt filter=evil\n")
	git("config", "filter.evil.clean", "touch pwned-clean; cat")
	git("config", "core.fsmonitor", "touch pwned-fsmonitor")
	write("a.txt", "changed\n")
	write("new.txt", "new\n")
	st, err = w.GitStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, api.GitStatus{Git: true, Repo: true, Branch: "feature", Changed: 3}, st, "a.txt, new.txt and .gitattributes")
	got, _ := filepath.Glob(filepath.Join(dir, "pwned-*"))
	assert.Empty(t, got, "git status ran commands from the repository's config")

	git("-c", "core.fsmonitor=false", "-c", "filter.evil.clean=", "checkout", "-q", "--detach")
	st, err = w.GitStatus(ctx)
	require.NoError(t, err)
	assert.Regexp(t, `^[0-9a-f]{7,}$`, st.Branch, "a detached head shows its commit")
}

// GitInit makes a folder a repository once; in one already, or without
// git, it changes nothing.
func TestGitInit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	ctx := context.Background()
	t.Run("a folder", func(t *testing.T) {
		w := openTest(t)
		st, err := w.GitInit(ctx)
		require.NoError(t, err)
		assert.True(t, st.Repo)
		assert.NotEmpty(t, st.Branch)
		assert.DirExists(t, filepath.Join(w.Dir(), ".git"))
		again, err := w.GitInit(ctx)
		require.NoError(t, err)
		assert.Equal(t, st, again)
	})
	t.Run("no git", func(t *testing.T) {
		w := openTest(t)
		t.Setenv("PATH", t.TempDir())
		st, err := w.GitInit(ctx)
		assert.ErrorIs(t, err, ErrNoGit)
		assert.Equal(t, api.GitStatus{}, st)
		assert.NoDirExists(t, filepath.Join(w.Dir(), ".git"))
	})
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

// Approvals are listed (this session's, then saved ones, with what they
// cover), revoked by key, and cleared.
func TestApprovals(t *testing.T) {
	w, _ := openTestWith(t, nil, toolCall("create_file", map[string]any{"path": "a.txt", "content": "a"}), text("done"))
	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionSession, nil }, nil)
	_, err := w.Run(context.Background(), newSession(t, w).ID, api.Turn{Text: "write"}, ignore)
	require.NoError(t, err)
	store := w.Tools().Hooks().Store()
	require.NoError(t, store.Add("cmd:/src\x00make test", "make test"))
	require.NoError(t, store.Add("uc-run:go\x00vet", ""))
	require.NoError(t, store.Add("plain", ""))

	list := w.ListApprovals()
	require.Len(t, list, 4)
	session := list[0]
	assert.False(t, session.Always, "the session's approval: %+v", session)
	assert.NotEmpty(t, session.Kind, "the session's approval: %+v", session)
	saved := map[string]api.Approval{}
	for _, a := range list[1:] {
		assert.True(t, a.Always, "%+v", a)
		saved[a.Key] = a
	}
	cmd := saved["cmd:/src\x00make test"]
	assert.Equal(t, []string{"cmd", "/src", "make test"}, []string{cmd.Kind, cmd.Dir, cmd.Subject})
	assert.Equal(t, "go vet", saved["uc-run:go\x00vet"].Subject)
	assert.Equal(t, api.Approval{Key: "plain", Subject: "plain", Always: true, Added: saved["plain"].Added}, saved["plain"])

	n, err := w.RevokeApprovals("plain")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Len(t, w.ListApprovals(), 3)
	n, err = w.ClearApprovals()
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Empty(t, w.ListApprovals())
}

// A saved approval that can't be removed from its file is an error naming
// it: it's gone for this session but would come back at the next start.
func TestRevokeApprovalSaveFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	w := openTest(t)
	store := w.Tools().Hooks().Store()
	require.NoError(t, store.Add("plain", ""))
	require.NoError(t, store.Add("web:example.com", ""))
	dir := filepath.Dir(store.Path())
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	for name, revoke := range map[string]func() (int, error){
		"by key": func() (int, error) { return w.RevokeApprovals("plain") },
		"all":    w.ClearApprovals,
	} {
		t.Run(name, func(t *testing.T) {
			n, err := revoke()
			require.ErrorIs(t, err, api.ErrApprovalsStillSaved)
			assert.Positive(t, n)
			assert.Contains(t, err.Error(), "plain")
			assert.True(t, store.Has("plain"), "still in the file")
		})
	}
}

// The git diff is every change since the last commit, staged or not, and
// new files; before the first commit, what's staged and what isn't.
func TestGitDiffEveryChange(t *testing.T) {
	needGit(t)
	ctx := context.Background()
	t.Run("since the last commit", func(t *testing.T) {
		w := openTest(t)
		dir := w.Dir()
		gitIn(t, dir, "init", "-q")
		write(t, dir, "staged.txt", "one\n")
		write(t, dir, "unstaged.txt", "one\n")
		write(t, dir, "gone.txt", "one\n")
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-qm", "init")
		write(t, dir, "staged.txt", "staged two\n")
		gitIn(t, dir, "add", "staged.txt")
		write(t, dir, "unstaged.txt", "unstaged two\n")
		require.NoError(t, os.Remove(filepath.Join(dir, "gone.txt")))
		write(t, dir, "new.txt", "brand new\n")
		diff, err := w.GitDiff(ctx, false)
		require.NoError(t, err, diff)
		for _, want := range []string{"+staged two", "+unstaged two", "deleted file mode", "b/new.txt", "+brand new"} {
			assert.Contains(t, diff, want)
		}
	})
	t.Run("before the first commit", func(t *testing.T) {
		w := openTest(t)
		dir := w.Dir()
		gitIn(t, dir, "init", "-q")
		write(t, dir, "a.txt", "added\n")
		gitIn(t, dir, "add", "a.txt")
		write(t, dir, "a.txt", "added, then changed\n")
		write(t, dir, "n.txt", "untracked\n")
		diff, err := w.GitDiff(ctx, false)
		require.NoError(t, err, diff)
		for _, want := range []string{"+added", "+added, then changed", "+untracked"} {
			assert.Contains(t, diff, want)
		}
	})
	t.Run("many new files", func(t *testing.T) {
		old := maxUntrackedDiffs
		maxUntrackedDiffs = 2
		t.Cleanup(func() { maxUntrackedDiffs = old })
		w := openTest(t)
		dir := w.Dir()
		gitIn(t, dir, "init", "-q")
		for _, n := range []string{"a", "b", "c", "d"} {
			write(t, dir, n+".txt", n+"\n")
		}
		diff, err := w.GitDiff(ctx, false)
		require.NoError(t, err, diff)
		assert.Contains(t, diff, "b/a.txt")
		assert.NotContains(t, diff, "b/c.txt")
		assert.Contains(t, diff, "(2 more new files not shown)")
	})
	t.Run("not a repository", func(t *testing.T) {
		w := openTest(t)
		_, err := w.GitDiff(ctx, false)
		assert.Error(t, err)
	})
}

// The diff can be colored; with no active session there is no session
// diff.
func TestGitDiffColorAndNoSessionDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	w := openTest(t)
	assert.Equal(t, "", w.SessionDiff(), "no active session")
	gitIn(t, w.Dir(), "init", "-q")
	write(t, w.Dir(), "a.txt", "one\n")
	gitIn(t, w.Dir(), "add", ".")
	gitIn(t, w.Dir(), "commit", "-qm", "init")
	write(t, w.Dir(), "a.txt", "two\n")
	diff, err := w.GitDiff(context.Background(), true)
	require.NoError(t, err)
	assert.Contains(t, diff, "\x1b[", "colored")
	assert.Contains(t, diff, "two")
}
