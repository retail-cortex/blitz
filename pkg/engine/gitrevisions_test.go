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
	_, err := w.FileHistory(ctx, "a.txt", 0)
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
	commit("second")
	gitOut(t, dir, "mv", "docs/old.md", "docs/new.md")
	write(t, dir, "docs/new.md", "# One\n\nthird\n")
	commit("renamed")
	write(t, dir, "docs/new.md", "# One\n\nunsaved\n")

	revs, err := w.FileHistory(ctx, "docs/new.md", 0)
	require.NoError(t, err)
	require.Len(t, revs, 3)
	var subjects, paths []string
	for _, r := range revs {
		subjects, paths = append(subjects, r.Subject), append(paths, r.Path)
		assert.Len(t, r.Commit, 40)
		assert.True(t, strings.HasPrefix(r.Commit, r.Short))
		assert.Equal(t, "t", r.Author)
		assert.False(t, r.When.IsZero())
	}
	assert.Equal(t, []string{"renamed", "second", "first"}, subjects, "newest first")
	assert.Equal(t, []string{"docs/new.md", "docs/old.md", "docs/old.md"}, paths, "following the rename")
	one, err := w.FileHistory(ctx, "docs/new.md", 1)
	require.NoError(t, err)
	assert.Len(t, one, 1)

	diff, err := w.FileRevisionDiff(ctx, "docs/new.md", first)
	require.NoError(t, err)
	assert.Contains(t, diff, "-first")
	assert.Contains(t, diff, "+unsaved")

	current, err := os.ReadFile(filepath.Join(dir, "docs/new.md"))
	require.NoError(t, err)
	_, err = w.RestoreFileRevision(ctx, "docs/new.md", first[:7], "stale")
	var changed *FileChangedError
	assert.True(t, errors.As(err, &changed), "refused over a change since: %v", err)
	version, err := w.RestoreFileRevision(ctx, "docs/new.md", first[:7], FileVersion(current))
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(dir, "docs/new.md"))
	require.NoError(t, err)
	assert.Equal(t, "# One\n\nfirst\n", string(data), "as it was, from its old name")
	assert.Equal(t, FileVersion(data), version)
	assert.Empty(t, gitOut(t, dir, "diff", "--cached", "--name-only"), "the index is left alone")
	assert.Contains(t, w.takeUserEdits(), "docs/new.md")

	for name, rev := range map[string]string{"an option": "--output=/tmp/x", "a branch": "HEAD", "not the file's": "0000000", "too short": "abc"} {
		t.Run(name, func(t *testing.T) {
			_, err := w.FileRevisionDiff(ctx, "docs/new.md", rev)
			assert.ErrorIs(t, err, ErrBadRevision)
			_, err = w.RestoreFileRevision(ctx, "docs/new.md", rev, "")
			assert.ErrorIs(t, err, ErrBadRevision)
		})
	}
	for _, p := range []string{"../outside.md", ""} {
		_, err := w.FileHistory(ctx, p, 0)
		assert.ErrorIs(t, err, ErrBadPath, p)
		_, err = w.FileRevisionDiff(ctx, p, first)
		assert.ErrorIs(t, err, ErrBadPath, p)
		_, err = w.RestoreFileRevision(ctx, p, first, "")
		assert.ErrorIs(t, err, ErrBadPath, p)
	}
}

// Outside a repository there are no revisions, and a .gitignore that
// can't be read is an error, not an empty file.
func TestRevisionsAndIgnoreErrors(t *testing.T) {
	needGit(t)
	w := openTest(t)
	ctx, dir := context.Background(), w.Dir()
	write(t, dir, "a.txt", "a")
	_, err := w.FileRevisionDiff(ctx, "a.txt", "0123456")
	assert.ErrorIs(t, err, ErrNotARepository)
	_, err = w.RestoreFileRevision(ctx, "a.txt", "0123456", "")
	assert.ErrorIs(t, err, ErrNotARepository)

	gitOut(t, dir, "init", "-q")
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".gitignore"), 0o755))
	assert.Error(t, w.GitFileAction(ctx, "a.txt", GitIgnore))
	assert.Error(t, w.GitFileAction(ctx, "a.txt", GitUnignore))
}
