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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Remove from .gitignore takes out the line Add wrote, and names any other
// rule that still ignores the path instead of rewriting it.
func TestGitUnignore(t *testing.T) {
	needGit(t)
	w := openTest(t)
	ctx, dir := context.Background(), w.Dir()
	gitOut(t, dir, "init", "-q")
	write(t, dir, ".gitignore", "*.log\n")
	write(t, dir, "notes.txt", "n")
	write(t, dir, "debug.log", "d")
	write(t, dir, ".cache/x", "c")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "build"), 0o755))
	for _, p := range []string{"notes.txt", "build", ".cache"} {
		require.NoError(t, w.GitFileAction(ctx, p, GitIgnore))
	}
	w.takeUserEdits()

	l, err := w.ListDir(ctx, "", true)
	require.NoError(t, err)
	for name, want := range map[string]bool{"notes.txt": true, "build": true, ".cache": true, "debug.log": true, ".gitignore": false} {
		assert.Equal(t, want, entry(t, l, name).Ignored, "%s ignored", name)
	}
	assert.Equal(t, "dot", entry(t, l, ".cache").Hidden, "a dotfile stays hidden as one")

	tests := []struct {
		path    string
		left    string // .gitignore after
		pattern string // the rule named, when another one ignores it
	}{
		{"notes.txt", "*.log\n/build/\n/.cache/\n", ""},
		{"build", "*.log\n/.cache/\n", ""},
		{".cache", "*.log\n", ""},
		{"debug.log", "*.log\n", "*.log"},
		{"notes.txt", "*.log\n", ""}, // not ignored any more: nothing to do
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			err := w.GitFileAction(ctx, tt.path, GitUnignore)
			if tt.pattern != "" {
				var other *IgnoredElsewhereError
				require.True(t, errors.As(err, &other), "error %v", err)
				assert.Equal(t, tt.pattern, other.Pattern)
				assert.Equal(t, ".gitignore:1", other.Source)
				assert.Contains(t, other.Error(), "*.log")
			} else {
				require.NoError(t, err)
			}
			data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
			require.NoError(t, err)
			assert.Equal(t, tt.left, string(data))
		})
	}
	assert.Contains(t, w.takeUserEdits(), ".gitignore", "the agent hears of it")

	// Ours and another rule both match: the file is put back as it was.
	require.NoError(t, w.GitFileAction(ctx, "debug.log", GitIgnore))
	before, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	assert.Error(t, w.GitFileAction(ctx, "debug.log", GitUnignore))
	after, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	assert.Equal(t, string(before), string(after), "unchanged when another rule still ignores it")
}

// View revisions: a file's commits (through a rename), how it differs from
// one, and putting it back as it was.
func TestFileRevisions(t *testing.T) {
	needGit(t)
	w := openTest(t)
	ctx, dir := context.Background(), w.Dir()
	_, _, err := w.History(ctx, "a.txt", 0, 0)
	assert.ErrorIs(t, err, ErrNotARepository)

	gitOut(t, dir, "init", "-q")
	commit := func(msg string) string {
		gitOut(t, dir, "add", "-A")
		gitOut(t, dir, "commit", "-qm", msg)
		return gitOut(t, dir, "rev-parse", "HEAD")
	}
	write(t, dir, "docs/old.md", "# One\n\nfirst\n")
	first := commit("first")
	write(t, dir, "docs/old.md", "# One\n\nsecond\n")
	second := commit("second")
	gitOut(t, dir, "mv", "docs/old.md", "docs/new.md")
	write(t, dir, "docs/new.md", "# One\n\nthird\n")
	renamed := commit("renamed")
	write(t, dir, "docs/new.md", "# One\n\nunsaved\n")

	revs, more, err := w.History(ctx, "docs/new.md", 0, 0)
	require.NoError(t, err)
	assert.False(t, more)
	require.Len(t, revs, 3)
	var subjects, paths []string
	for _, r := range revs {
		subjects, paths = append(subjects, r.Subject), append(paths, r.Path)
		assert.Len(t, r.Commit, 40)
		assert.True(t, strings.HasPrefix(r.Commit, r.Short))
		assert.Equal(t, "t", r.Author)
		assert.Equal(t, "t@t", r.Email)
		assert.False(t, r.When.IsZero())
	}
	assert.Equal(t, []string{"renamed", "second", "first"}, subjects, "newest first")
	assert.Equal(t, []string{"docs/new.md", "docs/old.md", "docs/old.md"}, paths, "following the rename")
	assert.Equal(t, []string{second}, revs[0].Parents)
	assert.Empty(t, revs[2].Parents, "the first commit")
	assert.Equal(t, [3]int{1, 1, 1}, [3]int{revs[1].Added, revs[1].Removed, revs[1].Files})
	page, more, err := w.History(ctx, "docs/new.md", 1, 1)
	require.NoError(t, err)
	assert.True(t, more)
	require.Len(t, page, 1)
	assert.Equal(t, "second", page[0].Subject, "the second page")

	diff, _, err := w.RevisionDiff(ctx, "docs/new.md", first, "", AgainstNow)
	require.NoError(t, err)
	assert.Contains(t, diff, "-first")
	assert.Contains(t, diff, "+unsaved")
	diff, _, err = w.RevisionDiff(ctx, "docs/new.md", second, "", AgainstParent)
	require.NoError(t, err)
	assert.Contains(t, diff, "-first")
	assert.Contains(t, diff, "+second", "what the commit changed")
	diff, _, err = w.RevisionDiff(ctx, "docs/new.md", first, "", AgainstParent)
	require.NoError(t, err)
	assert.Contains(t, diff, "+first", "the first commit, against nothing")
	diff, _, err = w.RevisionDiff(ctx, "docs/new.md", first, renamed[:7], AgainstOther)
	require.NoError(t, err)
	assert.Contains(t, diff, "rename from docs/old.md", "between two, across the rename")
	assert.Contains(t, diff, "+third")

	current, err := os.ReadFile(filepath.Join(dir, "docs/new.md"))
	require.NoError(t, err)
	_, err = w.RestoreFileRevision(ctx, "docs/new.md", first[:7], "stale")
	var changed *FileChangedError
	assert.True(t, errors.As(err, &changed), "refused over a change since: %v", err)
	version, files, err := w.Restore(ctx, "docs/new.md", first[:7], FileVersion(current), false)
	require.NoError(t, err)
	assert.Empty(t, files)
	data, err := os.ReadFile(filepath.Join(dir, "docs/new.md"))
	require.NoError(t, err)
	assert.Equal(t, "# One\n\nfirst\n", string(data), "as it was, from its old name")
	assert.Equal(t, FileVersion(data), version)
	assert.Empty(t, gitOut(t, dir, "diff", "--cached", "--name-only"), "the index is left alone")
	assert.Contains(t, w.takeUserEdits(), "docs/new.md")

	for name, rev := range map[string]string{"an option": "--output=/tmp/x", "a branch": "HEAD", "no such commit": "0000000", "too short": "abc"} {
		t.Run(name, func(t *testing.T) {
			_, _, err := w.RevisionDiff(ctx, "docs/new.md", rev, "", AgainstNow)
			assert.ErrorIs(t, err, ErrBadRevision)
			_, _, err = w.RevisionDiff(ctx, "docs/new.md", first, rev, AgainstOther)
			assert.ErrorIs(t, err, ErrBadRevision)
			_, err = w.RestoreFileRevision(ctx, "docs/new.md", rev, "")
			assert.ErrorIs(t, err, ErrBadRevision)
		})
	}
	write(t, dir, "other.md", "x")
	gitOut(t, dir, "add", "other.md")
	gitOut(t, dir, "commit", "-qm", "another file")
	other := gitOut(t, dir, "rev-parse", "HEAD")
	_, _, err = w.RevisionDiff(ctx, "docs/new.md", other, "", AgainstNow)
	assert.ErrorIs(t, err, ErrBadRevision, "a commit outside the file's history")
	_, _, err = w.History(ctx, "../outside.md", 0, 0)
	assert.ErrorIs(t, err, ErrBadPath)
	_, _, err = w.RevisionDiff(ctx, "../outside.md", first, "", AgainstNow)
	assert.ErrorIs(t, err, ErrBadPath)
	_, err = w.RestoreFileRevision(ctx, "docs", first, "")
	assert.ErrorIs(t, err, ErrBadPath, "a folder isn't a file")
	_, err = w.RestoreFolder(ctx, "other.md", first, false)
	assert.ErrorIs(t, err, ErrBadPath, "a file isn't a folder")
}

// A folder's and the workspace's history: merges' parents for the graph,
// what each commit changed, and a restore that keeps files added since.
func TestFolderHistory(t *testing.T) {
	needGit(t)
	w := openTest(t)
	ctx, dir := context.Background(), w.Dir()
	gitOut(t, dir, "init", "-q", "-b", "main")
	commit := func(msg string) string {
		gitOut(t, dir, "add", "-A")
		gitOut(t, dir, "commit", "-qm", msg)
		return gitOut(t, dir, "rev-parse", "HEAD")
	}
	write(t, dir, "src/a.go", "package a\n")
	write(t, dir, "src/b.go", "package b\n")
	write(t, dir, "README.md", "readme\n")
	base := commit("base")
	gitOut(t, dir, "checkout", "-qb", "topic")
	write(t, dir, "src/a.go", "package a // topic\n")
	topic := commit("topic change")
	gitOut(t, dir, "checkout", "-q", "main")
	write(t, dir, "src/b.go", "package b // main\n")
	main := commit("main change")
	gitOut(t, dir, "merge", "-q", "--no-ff", "-m", "merge topic", "topic")
	merge := gitOut(t, dir, "rev-parse", "HEAD")
	write(t, dir, "README.md", "readme 2\n")
	commit("docs only")

	revs, _, err := w.History(ctx, "src", 0, 0)
	require.NoError(t, err)
	var subjects []string
	for _, r := range revs {
		subjects = append(subjects, r.Subject)
		assert.Empty(t, r.Path, "a folder's revisions name no path")
	}
	assert.Contains(t, subjects, "merge topic")
	assert.NotContains(t, subjects, "docs only", "only commits under the folder")
	for _, r := range revs {
		if r.Commit == merge {
			assert.ElementsMatch(t, []string{main, topic}, r.Parents, "a merge's two parents, for the graph's lanes")
		}
	}
	all, _, err := w.History(ctx, "", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, "docs only", all[0].Subject, "the workspace's history has every commit")

	diff, _, err := w.RevisionDiff(ctx, "src", topic, "", AgainstParent)
	require.NoError(t, err)
	assert.Contains(t, diff, "+package a // topic")
	assert.NotContains(t, diff, "b.go")
	diff, _, err = w.RevisionDiff(ctx, "", base, merge, AgainstOther)
	require.NoError(t, err)
	assert.Contains(t, diff, "a.go")
	assert.Contains(t, diff, "b.go")

	// Restore src as it was at base: a.go and b.go back, a deleted file
	// back, a new one kept.
	require.NoError(t, os.Remove(filepath.Join(dir, "src/b.go")))
	write(t, dir, "src/c.go", "package c\n")
	gitOut(t, dir, "add", "src/c.go")
	preview, err := w.RestoreFolder(ctx, "src", base, true)
	require.NoError(t, err)
	assert.ElementsMatch(t, []RestoredFile{{Path: "src/a.go"}, {Path: "src/b.go", Recreated: true}}, preview)
	data, _ := os.ReadFile(filepath.Join(dir, "src/a.go"))
	assert.Equal(t, "package a // topic\n", string(data), "a preview changes nothing")
	_, files, err := w.Restore(ctx, "src", base[:8], "", false)
	require.NoError(t, err)
	assert.Len(t, files, 2)
	data, _ = os.ReadFile(filepath.Join(dir, "src/a.go"))
	assert.Equal(t, "package a\n", string(data))
	assert.FileExists(t, filepath.Join(dir, "src/b.go"), "recreated")
	assert.FileExists(t, filepath.Join(dir, "src/c.go"), "added since: kept")
	assert.Equal(t, "src/c.go", gitOut(t, dir, "diff", "--cached", "--name-only"), "the index is left alone")
	assert.Contains(t, w.takeUserEdits(), "src")
	_, files, err = w.Restore(ctx, "src", base, "", false)
	require.NoError(t, err)
	assert.Empty(t, files, "nothing left to put back")
	_, files, err = w.Restore(ctx, "src/a.go", base, "", true)
	require.NoError(t, err)
	assert.Empty(t, files, "a file's preview says nothing")

	assert.Equal(t, "b.md", renamedTo("a.md => b.md"))
	assert.Equal(t, "docs/new/x.md", renamedTo("docs/{old => new}/x.md"))
	assert.Equal(t, "docs/x.md", renamedTo("docs/{old => }/x.md"))
	assert.Equal(t, "plain.md", renamedTo("plain.md"))
}

// Outside a repository there are no revisions, and a .gitignore that
// can't be read is an error, not an empty file.
func TestRevisionsAndIgnoreErrors(t *testing.T) {
	needGit(t)
	w := openTest(t)
	ctx, dir := context.Background(), w.Dir()
	write(t, dir, "a.txt", "a")
	_, _, err := w.RevisionDiff(ctx, "a.txt", "0123456", "", AgainstNow)
	assert.ErrorIs(t, err, ErrNotARepository)
	_, err = w.RestoreFileRevision(ctx, "a.txt", "0123456", "")
	assert.ErrorIs(t, err, ErrNotARepository)
	_, err = w.RestoreFolder(ctx, "", "0123456", true)
	assert.ErrorIs(t, err, ErrNotARepository)

	gitOut(t, dir, "init", "-q")
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".gitignore"), 0o755))
	assert.Error(t, w.GitFileAction(ctx, "a.txt", GitIgnore))
	assert.Error(t, w.GitFileAction(ctx, "a.txt", GitUnignore))
}
