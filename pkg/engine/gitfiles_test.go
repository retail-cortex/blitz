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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// gitOut runs git in dir for a test, with an identity for commits, and
// returns what it printed.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
}

// The files the agent creates in a turn are added to git at its end, in one
// batch, leaving out what git ignores, scratch files and files that were
// there before; the turn says what was added.
func TestNewFilesGoToGit(t *testing.T) {
	needGit(t)
	creates := []*genai.Content{
		toolCall("create_file", map[string]any{"path": "a.go", "content": "package a\n"}),
		toolCall("create_file", map[string]any{"path": "docs/guide.md", "content": "# Guide\n"}),
		toolCall("create_file", map[string]any{"path": "notes.tmp", "content": "scratch"}),
		toolCall("create_file", map[string]any{"path": "tmp/out.txt", "content": "scratch"}),
		toolCall("create_file", map[string]any{"path": "run.log", "content": "ignored"}),
		toolCall("create_file", map[string]any{"path": "existing.go", "content": "package a // changed\n"}),
	}
	cases := []struct {
		name   string
		repo   bool
		off    bool
		staged string
	}{
		{"in a repository", true, false, "a.go\ndocs/guide.md"},
		{"auto_add off", true, true, ""},
		{"not a repository", false, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			replies := append(append([]*genai.Content{}, creates...), text("done"))
			w, _ := openTestWith(t, func(cfg *config.Config) {
				cfg.Blitz.PermissionMode = "accept-edits"
				cfg.Git.AutoAdd = !c.off
			}, replies...)
			require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), "existing.go"), []byte("package a\n"), 0o644))
			if c.repo {
				gitOut(t, w.Dir(), "init", "-q")
				require.NoError(t, os.WriteFile(filepath.Join(w.Dir(), ".gitignore"), []byte("*.log\n"), 0o644))
			}
			sess, err := w.NewSession()
			require.NoError(t, err)
			var notices []string
			_, err = w.Run(context.Background(), sess.ID, api.Turn{Text: "write them"}, func(e api.Event) {
				if e.Notice != nil {
					notices = append(notices, e.Notice.Text)
				}
			})
			require.NoError(t, err)
			require.FileExists(t, filepath.Join(w.Dir(), "a.go"))
			if !c.repo {
				assert.Empty(t, notices)
				return
			}
			assert.Equal(t, c.staged, gitOut(t, w.Dir(), "diff", "--cached", "--name-only"))
			if c.staged == "" {
				assert.Empty(t, notices)
				return
			}
			require.Len(t, notices, 1)
			assert.Contains(t, notices[0], "a.go, docs/guide.md")
		})
	}
}

// Files a turn's commands made (not the agent's file tools) go to git at
// its end too: those untracked that weren't there when it started, without
// what git ignores, scratch files or .agents/scratch.
func TestFilesCommandsMadeGoToGit(t *testing.T) {
	needGit(t)
	tests := []struct {
		name   string
		off    bool
		staged string
	}{
		{"auto_add on", false, "gen/client.go\npyproject.toml"},
		{"auto_add off", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := openTestWith(t, func(c *config.Config) { c.Git.AutoAdd = !tt.off })
			ctx := context.Background()
			dir := w.Dir()
			gitOut(t, dir, "init", "-q")
			write(t, dir, ".gitignore", "*.log\n")
			write(t, dir, "before.txt", "untracked before the turn\n")
			before := w.untrackedNow(ctx)
			if tt.off {
				assert.Nil(t, before, "nothing listed with auto_add off")
			}
			// What `uv init` or a generator would leave.
			write(t, dir, "pyproject.toml", "[project]\n")
			write(t, dir, "gen/client.go", "package gen\n")
			write(t, dir, "build.log", "ignored\n")
			write(t, dir, "notes.tmp", "temporary\n")
			write(t, dir, ".agents/scratch/draft.md", "scratch\n")
			write(t, dir, "web/node_modules/x/index.js", "build output\n")
			write(t, dir, ".venv/lib/site.py", "an environment\n")
			var notices []string
			w.stageCreated(ctx, "s", before, func(e api.Event) {
				if e.Notice != nil {
					notices = append(notices, e.Notice.Text)
				}
			})
			assert.Equal(t, tt.staged, gitOut(t, dir, "diff", "--cached", "--name-only"))
			if tt.staged != "" {
				require.Len(t, notices, 1)
				assert.Contains(t, notices[0], "gen/client.go, pyproject.toml")
			} else {
				assert.Empty(t, notices)
			}
		})
	}
}

// The Files view's git actions: stage, unstage, discard and ignore.
func TestGitFileAction(t *testing.T) {
	needGit(t)
	w := openTest(t)
	ctx := context.Background()
	dir := w.Dir()
	assert.ErrorIs(t, w.GitFileAction(ctx, "f.txt", GitStage), ErrNotARepository)

	gitOut(t, dir, "init", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\n"), 0o644))
	gitOut(t, dir, "add", "f.txt")
	gitOut(t, dir, "commit", "-qm", "init")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("two\n"), 0o644))

	staged := func() string {
		t.Helper()
		l, err := w.ListDir(ctx, "", false)
		require.NoError(t, err)
		for _, e := range l.Entries {
			if e.Path == "f.txt" {
				assert.Equal(t, "modified", e.Git)
				return e.Staged
			}
		}
		t.Fatal("no f.txt")
		return ""
	}
	assert.Empty(t, staged())
	require.NoError(t, w.GitFileAction(ctx, "f.txt", GitStage))
	assert.Equal(t, "f.txt", gitOut(t, dir, "diff", "--cached", "--name-only"))
	assert.Equal(t, "all", staged(), "the Files view sees it staged")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("three\n"), 0o644))
	assert.Equal(t, "some", staged(), "changed again after staging")
	require.NoError(t, w.GitFileAction(ctx, "f.txt", GitUnstage))
	assert.Empty(t, gitOut(t, dir, "diff", "--cached", "--name-only"))
	assert.Empty(t, staged())
	require.NoError(t, w.GitFileAction(ctx, "f.txt", GitDiscard))
	data, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	require.NoError(t, err)
	assert.Equal(t, "one\n", string(data), "back to the last commit")
	assert.Contains(t, w.takeUserEdits(), "f.txt", "the agent hears of the discard")

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "build"), 0o755))
	require.NoError(t, w.GitFileAction(ctx, "build", GitIgnore))
	require.NoError(t, w.GitFileAction(ctx, "build", GitIgnore))
	require.NoError(t, w.GitFileAction(ctx, "f.txt", GitIgnore))
	ignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "/build/\n/f.txt\n", string(ignore), "anchored, a folder with /, each once")

	assert.ErrorIs(t, w.GitFileAction(ctx, "../outside", GitStage), ErrBadPath)
	assert.ErrorIs(t, w.GitFileAction(ctx, "", GitDiscard), ErrBadPath, "the whole workspace only to include or leave out")
	assert.ErrorIs(t, w.GitFileAction(ctx, "", GitIgnore), ErrBadPath)
	assert.Error(t, w.GitFileAction(ctx, "f.txt", "push"))
}

// Include all changes: every change in the workspace, new files too,
// what git ignores left out; and leaving them all out again.
func TestIncludeAllChanges(t *testing.T) {
	needGit(t)
	w := openTest(t)
	ctx := context.Background()
	dir := w.Dir()
	gitOut(t, dir, "init", "-q")
	write(t, dir, ".gitignore", "*.log\n")
	write(t, dir, "kept.txt", "one\n")
	gitOut(t, dir, "add", ".")
	gitOut(t, dir, "commit", "-qm", "init")
	write(t, dir, "kept.txt", "two\n")
	write(t, dir, "new/made-by-a-script.py", "print(1)\n")
	write(t, dir, "run.log", "ignored\n")
	require.NoError(t, os.Remove(filepath.Join(dir, ".gitignore")))
	write(t, dir, ".gitignore", "*.log\n# changed\n")

	for _, all := range []string{"", "."} {
		require.NoError(t, w.GitFileAction(ctx, all, GitStage))
		assert.Equal(t, ".gitignore\nkept.txt\nnew/made-by-a-script.py", gitOut(t, dir, "diff", "--cached", "--name-only"), "included with %q", all)
		require.NoError(t, w.GitFileAction(ctx, all, GitUnstage))
		assert.Empty(t, gitOut(t, dir, "diff", "--cached", "--name-only"), "left out with %q", all)
	}
}

func TestInBuildDir(t *testing.T) {
	for p, want := range map[string]bool{
		"a.go": false, "src/builder.go": false, "docs/build.md": false,
		"build/out.o": true, "web/node_modules/x/i.js": true, ".venv/lib/a.py": true, "pkg/__pycache__/a.pyc": true,
	} {
		assert.Equal(t, want, inBuildDir(p), p)
	}
}

func TestIsTemporary(t *testing.T) {
	for p, want := range map[string]bool{
		"a.go": false, "docs/guide.md": false, "tmpl/page.html": false, "src/temporal.go": false,
		"notes.tmp": true, "x.swp": true, "a.go~": true, ".DS_Store": true, "src/.#lock": true,
		"tmp/out.txt": true, "a/temp/b.go": true, ".cache/x": true, "patch.orig": true,
	} {
		assert.Equal(t, want, isTemporary(p), p)
	}
}

// Every git run here shares its options: fsmonitor off, filters blanked
// unless a restore asks for them, and no optional locks or prompts.
func TestGitArgs(t *testing.T) {
	needGit(t)
	empty := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", empty) // not the user's own filters (Git LFS)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	gitOut(t, dir, "init", "-q")
	gitOut(t, dir, "config", "filter.x.smudge", "touch pwned")
	for _, tc := range []struct {
		name    string
		filters bool
		want    []string
	}{
		{"filters blanked", false, []string{"-c", "core.fsmonitor=false", "-c", "filter.x.smudge=", "status"}},
		{"filters kept", true, []string{"-c", "core.fsmonitor=false", "status"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, gitArgs(context.Background(), dir, tc.filters, "status"))
		})
	}
	assert.Contains(t, gitEnv(), "GIT_OPTIONAL_LOCKS=0")
	assert.Contains(t, gitEnv(), "GIT_TERMINAL_PROMPT=0")
}

// Deleting from the tree takes the path out of git's index (FIL-61): a
// file only staged leaves git altogether, a committed one is a staged
// deletion; restoring it from the trash puts it back as it was; a rename
// moves it in git; files a turn deleted leave the index at its end, and
// say so. With auto_add off the index is left alone.
func TestDeletionsLeaveGit(t *testing.T) {
	needGit(t)
	status := func(t *testing.T, dir string) string {
		t.Helper()
		return gitOut(t, dir, "status", "--porcelain", "--untracked-files=all", "--", ".", ":!.blitz")
	}
	t.Run("auto_add on", func(t *testing.T) {
		w := openTest(t)
		ctx := context.Background()
		dir := w.Dir()
		write(t, dir, "kept.go", "package a\n")
		write(t, dir, "docs/old.md", "old\n")
		gitOut(t, dir, "init", "-q")
		gitOut(t, dir, "add", ".")
		gitOut(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "first")

		write(t, dir, "new.go", "package a\n")
		gitOut(t, dir, "add", "new.go") // as auto_add stages an agent's file
		_, err := w.TrashFile(ctx, "new.go")
		require.NoError(t, err)
		assert.Empty(t, status(t, dir), "staged, then deleted: gone from git, no AD")

		id, err := w.TrashFile(ctx, "docs")
		require.NoError(t, err)
		assert.Equal(t, "D  docs/old.md", status(t, dir), "committed: a staged deletion")
		require.NoError(t, w.RestoreFile(ctx, id, "docs"))
		assert.Empty(t, status(t, dir), "restored: back as it was")

		require.NoError(t, w.RenameFile(ctx, "kept.go", "moved.go"))
		assert.Equal(t, "R  kept.go -> moved.go", status(t, dir), "a rename, staged")
		require.NoError(t, w.DeleteFile(ctx, "moved.go"))
		assert.Equal(t, "D  kept.go", status(t, dir))

		write(t, dir, "untracked.txt", "x\n")
		require.NoError(t, w.DeleteFile(ctx, "untracked.txt"))
		assert.Equal(t, "D  kept.go", status(t, dir), "never in git: nothing to do")

		// A turn's deletion: gone from disk between its start and end.
		gitOut(t, dir, "restore", "--staged", "--worktree", "--source=HEAD", "--", "kept.go")
		before := w.deletedNow(ctx)
		require.NotNil(t, before)
		require.NoError(t, os.Remove(filepath.Join(dir, "kept.go")))
		var notices []string
		w.unstageDeleted(ctx, before, func(e api.Event) {
			if e.Notice != nil {
				notices = append(notices, e.Notice.Text)
			}
		})
		assert.Equal(t, "D  kept.go", status(t, dir))
		assert.Equal(t, []string{"Removed from version control: kept.go"}, notices)
		w.unstageDeleted(ctx, nil, func(api.Event) { t.Fatal("nothing listed, nothing said") })
	})
	t.Run("auto_add off", func(t *testing.T) {
		w, _ := openTestWith(t, func(c *config.Config) { c.Git.AutoAdd = false })
		ctx := context.Background()
		dir := w.Dir()
		gitOut(t, dir, "init", "-q")
		write(t, dir, "new.go", "package a\n")
		gitOut(t, dir, "add", "new.go")
		require.NoError(t, w.DeleteFile(ctx, "new.go"))
		assert.Equal(t, "AD new.go", status(t, dir), "left alone")
		assert.Nil(t, w.deletedNow(ctx))
	})
}

// When git can't change its index (another git holds its lock), a
// deletion is still made and git's error only logged; an index git can't
// read lists nothing (FIL-61).
func TestDeletionsWhenGitFails(t *testing.T) {
	needGit(t)
	w := openTest(t)
	ctx := context.Background()
	dir := w.Dir()
	gitOut(t, dir, "init", "-q")
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "b.go", "package a\n")
	gitOut(t, dir, "add", ".")
	write(t, dir, ".git/index.lock", "")
	require.NoError(t, w.DeleteFile(ctx, "a.go"))
	assert.NoFileExists(t, filepath.Join(dir, "a.go"))
	assert.Empty(t, w.unstageGone(ctx, "a.go"), "git refused: nothing taken out")
	w.restage(ctx, "b.go") // refused too, and only logged
	require.NoError(t, os.Remove(filepath.Join(dir, ".git/index.lock")))
	write(t, dir, ".git/index", "not an index")
	assert.Nil(t, w.deletedNow(ctx))
}
