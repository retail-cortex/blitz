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
	assert.ErrorIs(t, w.GitFileAction(ctx, "", GitStage), ErrBadPath)
	assert.Error(t, w.GitFileAction(ctx, "f.txt", "push"))
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
