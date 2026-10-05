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
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, dir, rel, data string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(data), 0o644))
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %v\n%s", args, err, out)
}

func names(l DirListing) []string {
	var out []string
	for _, e := range l.Entries {
		out = append(out, e.Name)
	}
	return out
}

func entry(t *testing.T, l DirListing, name string) FileEntry {
	t.Helper()
	for _, e := range l.Entries {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no %s in %v", name, names(l))
	return FileEntry{}
}

func TestListDirHiddenAndGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	w, _ := openTestWith(t, func(c *config.Config) { c.Sandbox.BlockedPaths = []string{".env", "*.pem"} })
	dir, ctx := w.Dir(), context.Background()
	gitIn(t, dir, "init", "-q")
	write(t, dir, ".gitignore", "build/\n*.log\n")
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "pkg/a.go", "package pkg\n")
	write(t, dir, "clean.txt", "same\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "first")
	write(t, dir, "main.go", "package main // changed\n")
	write(t, dir, "pkg/new.go", "package pkg\n")
	write(t, dir, "build/out.bin", "x")
	write(t, dir, "debug.log", "x")
	write(t, dir, ".env", "KEY=1")
	write(t, dir, ".editorconfig", "root = true")

	l, err := w.ListDir(ctx, "", false)
	require.NoError(t, err)
	got, want := names(l), []string{"pkg", "clean.txt", "main.go"}
	assert.Equal(t, want, got, "visible %v (repo %v), want %v", got, l.Repo, want)
	assert.True(t, l.Repo, "visible %v (repo %v), want %v", got, l.Repo, want)
	e := entry(t, l, "main.go")
	assert.Equal(t, "modified", e.Git, "main.go: %+v", e)
	assert.Equal(t, KindFile, e.Kind, "main.go: %+v", e)
	e = entry(t, l, "pkg")
	assert.Equal(t, "changed", e.Git, "pkg: %+v", e)
	assert.Equal(t, KindFolder, e.Kind, "pkg: %+v", e)
	e = entry(t, l, "clean.txt")
	assert.Equal(t, "", e.Git, "clean.txt: %+v", e)

	all, err := w.ListDir(ctx, ".", true)
	require.NoError(t, err)
	for name, hidden := range map[string]string{"build": "ignored", "debug.log": "ignored", ".env": "blocked", ".editorconfig": "dot", ".gitignore": "dot"} {
		t.Run(name, func(t *testing.T) {
			e := entry(t, all, name)
			assert.Equal(t, hidden, e.Hidden, "%s hidden %q, want %q", name, e.Hidden, hidden)
		})
	}
	e = entry(t, all, ".env")
	assert.Equal(t, "blocked", e.AgentRule, ".env rule %q", e.AgentRule)
	assert.NotContains(t, names(all), ".git", ".git listed")
	sub, _ := w.ListDir(ctx, "pkg", false)
	e = entry(t, sub, "new.go")
	assert.Equal(t, "untracked", e.Git, "pkg/new.go: %+v", e)
	assert.Equal(t, "pkg/new.go", e.Path, "pkg/new.go: %+v", e)
}

func TestFilesStayInTheWorkspace(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	outside := t.TempDir()
	write(t, outside, "secret.txt", "no")
	require.NoError(t, os.Symlink(outside, filepath.Join(w.Dir(), "out")))
	for _, p := range []string{"../x", "/etc/passwd", "out/secret.txt", ".git/config", "a/../../x"} {
		t.Run(p, func(t *testing.T) {
			_, err := w.ReadFile(p)
			assert.Error(t, err, "read %s", p)
			_, err = w.WriteFile(ctx, p, "x", "")
			assert.Error(t, err, "wrote %s", p)
		})
	}
	_, err := os.Stat(filepath.Join(outside, "x"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "wrote outside")
	assert.Error(t, w.DeleteFile(ctx, ""), "deleted the workspace")
}

func TestReadWriteVersions(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	v, err := w.WriteFile(ctx, "src/a.txt", "one\n", "")
	require.NoError(t, err)
	f, err := w.ReadFile("src/a.txt")
	require.NoError(t, err, "read %+v", f)
	require.Equal(t, "one\n", f.Text, "read %+v %v", f, err)
	require.Equal(t, v, f.Version, "read %+v %v", f, err)
	require.Equal(t, "src/a.txt", f.Path, "read %+v %v", f, err)
	// Creating over an existing file, or saving an old version, is refused.
	var changed *FileChangedError
	_, err = w.WriteFile(ctx, "src/a.txt", "x", "")
	assert.ErrorAs(t, err, &changed, "create over existing: %v", err)
	assert.Equal(t, v, changed.Current, "create over existing: %v", err)
	write(t, w.Dir(), "src/a.txt", "the agent's\n")
	_, err = w.WriteFile(ctx, "src/a.txt", "two\n", v)
	assert.ErrorAs(t, err, &changed, "stale save: %v", err)
	assert.Equal(t, FileVersion([]byte("the agent's\n")), changed.Current, "stale save: %v", err)
	v2, err := w.WriteFile(ctx, "src/a.txt", "two\n", changed.Current)
	assert.NoError(t, err, "overwrite")
	assert.NotEqual(t, v, v2, "overwrite: %v", err)
	got := w.StatFiles([]string{"src/a.txt", "gone.txt"})
	assert.Equal(t, FileVersion([]byte("two\n")), got["src/a.txt"], "stat %v", got)
	assert.Equal(t, "", got["gone.txt"], "stat %v", got)

	write(t, w.Dir(), "bin.dat", "a\x00b")
	f, _ = w.ReadFile("bin.dat")
	assert.True(t, f.Binary, "binary %+v", f)
	assert.Equal(t, "", f.Text, "binary %+v", f)
	big := strings.Repeat("x", maxEditorBytes+1)
	write(t, w.Dir(), "big.txt", big)
	f, _ = w.ReadFile("big.txt")
	assert.True(t, f.TooLarge, "too large %+v", f)
	assert.Equal(t, int64(len(big)), f.Size, "too large %+v", f)
}

func TestCreateRenameDelete(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	require.NoError(t, w.CreateFolder("docs/guides"))
	err := w.CreateFolder("docs")
	assert.ErrorIs(t, err, fs.ErrExist, "existing folder: %v", err)
	write(t, w.Dir(), "docs/a.md", "a")
	write(t, w.Dir(), "docs/b.md", "b")
	err = w.RenameFile(ctx, "docs/a.md", "docs/b.md")
	assert.ErrorIs(t, err, fs.ErrExist, "rename over: %v", err)
	require.NoError(t, w.RenameFile(ctx, "docs/a.md", "notes/a.md"))
	_, err = os.Stat(filepath.Join(w.Dir(), "notes", "a.md"))
	assert.NoError(t, err, "not moved")
	require.NoError(t, w.DeleteFile(ctx, "docs"))
	_, err = os.Stat(filepath.Join(w.Dir(), "docs"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "not deleted")
	err = w.DeleteFile(ctx, "docs")
	assert.ErrorIs(t, err, fs.ErrNotExist, "delete missing: %v", err)
}

// The agent is told once about the user's edits, and the note isn't shown
// as part of the user's prompt.
func TestUserEditsReachTheNextTurn(t *testing.T) {
	w, llm := openTestWith(t, nil, text("ok"), text("ok again"))
	ctx := context.Background()
	_, err := w.WriteFile(ctx, "a.go", "package a\n", "")
	require.NoError(t, err)
	write(t, w.Dir(), "b.go", "package b\n")
	require.NoError(t, w.DeleteFile(ctx, "b.go"))
	sid := newSession(t, w).ID
	_, err = w.Run(ctx, sid, api.Turn{Text: "go on"}, ignore)
	require.NoError(t, err)
	sent := lastUserText(llm)
	assert.Contains(t, sent, "<user-edits>", "first turn: %q", sent)
	assert.Contains(t, sent, "a.go (created)", "first turn: %q", sent)
	assert.Contains(t, sent, "b.go (deleted)", "first turn: %q", sent)
	_, err = w.Run(ctx, sid, api.Turn{Text: "more"}, ignore)
	require.NoError(t, err)
	assert.NotContains(t, lastUserText(llm), "user-edits", "told twice")
	got := transcript(t, w)
	assert.Equal(t, []string{"user: go on", "model: ok", "user: more", "model: ok again"}, got, "transcript %q", got)
}

func TestFindFiles(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Sandbox.BlockedPaths = []string{"*.pem"} })
	for _, p := range []string{"internal/cart/discount.go", "internal/cart/discount_test.go", "docs/discounts.md", "cmd/shop/main.go", ".hidden/x.go", "key.pem"} {
		write(t, w.Dir(), p, "x")
	}
	got, err := w.FindFiles(context.Background(), "discount", 10)
	require.NoError(t, err)
	assert.Len(t, got, 3, "discount: %v", got)
	assert.Equal(t, "internal/cart/discount.go", got[0], "discount: %v", got)
	got, _ = w.FindFiles(context.Background(), "csm", 10)
	assert.NotEqual(t, 0, len(got), "csm: %v", got)
	assert.Equal(t, "cmd/shop/main.go", got[0], "csm: %v", got)
	all, _ := w.FindFiles(context.Background(), "", 100)
	assert.NotContains(t, all, ".hidden/x.go", "hidden files found: %v", all)
	assert.NotContains(t, all, "key.pem", "hidden files found: %v", all)

	// With folders: the folders of the files, matched alike.
	got, _ = w.FindPaths(context.Background(), "cart", 10, true)
	assert.Equal(t, "internal/cart/", got[0], "cart: %v", got)
	assert.Contains(t, got, "internal/cart/discount.go", "cart: %v", got)
	all, _ = w.FindPaths(context.Background(), "", 100, true)
	assert.Contains(t, all, "internal/", "all: %v", all)
	assert.NotContains(t, all, ".hidden/", "hidden folders found: %v", all)
	files, _ := w.FindFiles(context.Background(), "cart", 10)
	assert.NotContains(t, files, "internal/cart/", "FindFiles gave a folder: %v", files)
}

func TestUserPath(t *testing.T) {
	for in, want := range map[string]string{"": ".", "a/b": filepath.Join("a", "b"), "a//b/": filepath.Join("a", "b"), "./x": "x"} {
		t.Run(in, func(t *testing.T) {
			got, err := tools.UserPath(in)
			assert.NoError(t, err, "%q: %q", in, got)
			assert.Equal(t, want, got, "%q: %q %v", in, got, err)
		})
	}
	for _, in := range []string{"..", "../a", "/a", ".git", ".git/HEAD"} {
		t.Run(in, func(t *testing.T) {
			_, err := tools.UserPath(in)
			assert.Error(t, err, "%q accepted", in)
		})
	}
}

// Images and PDFs come as bytes with their media type, for the preview;
// other files, and ones too big, don't.
func TestReadPreview(t *testing.T) {
	w := openTest(t)
	write(t, w.Dir(), "img/logo.PNG", "\x89PNG data")
	write(t, w.Dir(), "doc.pdf", "%PDF-1.7")
	write(t, w.Dir(), "icon.svg", "<svg/>")
	write(t, w.Dir(), "audio/overview.wav", "RIFF")
	write(t, w.Dir(), "talk.MP3", "ID3")
	write(t, w.Dir(), "demo.mov", "moov")
	write(t, w.Dir(), "main.go", "package main")
	for _, tc := range []struct {
		path, mime, data string
		err              error
	}{
		{"img/logo.PNG", "image/png", "\x89PNG data", nil},
		{"doc.pdf", "application/pdf", "%PDF-1.7", nil},
		{"icon.svg", "image/svg+xml", "<svg/>", nil},
		{"audio/overview.wav", "audio/wav", "RIFF", nil},
		{"talk.MP3", "audio/mpeg", "ID3", nil},
		{"demo.mov", "video/quicktime", "moov", nil},
		{"main.go", "", "", ErrNoPreview},
		{"none.png", "", "", fs.ErrNotExist},
		{"../x.png", "", "", ErrBadPath},
	} {
		t.Run(tc.path, func(t *testing.T) {
			mime, data, err := w.ReadPreview(tc.path)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.mime, mime)
			assert.Equal(t, tc.data, string(data))
		})
	}
}

// A save refused for a file changed or deleted since it was opened says
// which.
func TestFileChangedErrorMessage(t *testing.T) {
	assert.Equal(t, "a.txt was deleted since it was opened", (&FileChangedError{Path: "a.txt"}).Error())
	assert.Equal(t, "a.txt was changed since it was opened", (&FileChangedError{Path: "a.txt", Current: "v2"}).Error())
}

// Paths outside the workspace are refused by every file operation; a
// folder that isn't there can't be listed.
func TestFileOperationsRefuseBadPaths(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	_, err := w.ListDir(ctx, "../x", false)
	assert.ErrorIs(t, err, ErrBadPath)
	_, err = w.ListDir(ctx, "missing", false)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.ErrorIs(t, w.CreateFolder("../x"), ErrBadPath)
	assert.ErrorIs(t, w.RenameFile(ctx, "../x", "y"), ErrBadPath)
	write(t, w.Dir(), "y", "y")
	assert.ErrorIs(t, w.RenameFile(ctx, "y", "../x"), ErrBadPath)
	assert.ErrorIs(t, w.DeleteFile(ctx, "../x"), ErrBadPath)
	assert.Empty(t, w.StatFiles([]string{"../x"}))
}

// Outside a repository, a folder lists without git states; a link to a
// folder in the workspace lists as a folder, one to a file as a link.
func TestListDirLinksWithoutGit(t *testing.T) {
	w := openTest(t)
	write(t, w.Dir(), "real/a.txt", "a")
	require.NoError(t, os.Symlink("real", filepath.Join(w.Dir(), "linkdir")))
	require.NoError(t, os.Symlink("real/a.txt", filepath.Join(w.Dir(), "linkfile")))
	l, err := w.ListDir(context.Background(), "", false)
	require.NoError(t, err)
	assert.False(t, l.Repo)
	assert.Equal(t, KindFolder, entry(t, l, "linkdir").Kind)
	assert.Equal(t, KindSymlink, entry(t, l, "linkfile").Kind)
}

// A folder with more entries than are shown is cut short, and says so.
func TestListDirTruncates(t *testing.T) {
	w := openTest(t)
	dir := filepath.Join(w.Dir(), "many")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for i := range maxListed + 1 {
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("f%05d", i)))
		require.NoError(t, err)
		f.Close()
	}
	l, err := w.ListDir(context.Background(), "many", false)
	require.NoError(t, err)
	assert.True(t, l.Truncated)
	assert.Len(t, l.Entries, maxListed)
}

// tools.max_file_size_bytes lowers the editor's limit; a file over it
// comes without its text, and its version is its size and time.
func TestReadFileHonoursTheSizeLimit(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Tools.MaxFileSizeBytes = 4 })
	write(t, w.Dir(), "a.txt", "too long")
	f, err := w.ReadFile("a.txt")
	require.NoError(t, err)
	assert.True(t, f.TooLarge)
	big := filepath.Join(w.Dir(), "big.bin")
	require.NoError(t, os.WriteFile(big, nil, 0o644))
	require.NoError(t, os.Truncate(big, maxPreviewBytes+1))
	assert.Regexp(t, `^size:\d+:\d+$`, w.StatFiles([]string{"big.bin"})["big.bin"])
	require.NoError(t, os.Rename(big, filepath.Join(w.Dir(), "big.png")))
	_, _, err = w.ReadPreview("big.png")
	assert.ErrorIs(t, err, ErrNoPreview)
	assert.ErrorContains(t, err, "over 32 MB")
}

// Binary detection looks at the first 8 kB only.
func TestIsBinary(t *testing.T) {
	text := []byte(strings.Repeat("a", 9000))
	assert.False(t, isBinary(text))
	assert.True(t, isBinary(append([]byte("\x00"), text...)))
	assert.False(t, isBinary(append(text, 0)), "a NUL past 8 kB")
}

// The note about the user's edits is taken out of the prompt shown; an
// unterminated one is left.
func TestDisplayPrompt(t *testing.T) {
	assert.Equal(t, "go on", DisplayPrompt(withUserEdits("go on", "a.go (created)")))
	assert.Equal(t, "go on", DisplayPrompt("go on"))
	assert.Equal(t, "go on\n\n<user-edits>x", DisplayPrompt("go on\n\n<user-edits>x"))
	assert.Equal(t, "go on", withUserEdits("go on", ""))
}

// Each porcelain status has its name.
func TestGitState(t *testing.T) {
	for xy, want := range map[string]string{
		"??": "untracked", "UU": "conflicted", "AA": "conflicted", "DD": "conflicted",
		"R ": "renamed", " R": "renamed", "A ": "added", "D ": "deleted", " D": "deleted", " M": "modified",
	} {
		t.Run(xy, func(t *testing.T) { assert.Equal(t, want, gitState(xy)) })
	}
}

// How much of each porcelain status is staged.
func TestStagedState(t *testing.T) {
	for xy, want := range map[string]string{
		"M ": "all", "A ": "all", "D ": "all", "R ": "all",
		"MM": "some", "AM": "some", "RM": "some",
		" M": "", " D": "", "??": "", "UU": "", "AA": "", "DD": "", "AU": "",
	} {
		t.Run(xy, func(t *testing.T) { assert.Equal(t, want, stagedState(xy)) })
	}
}

// In a repository, finding files asks git, which also lists renamed
// files once; files deleted from disk but still in the index, and hidden
// ones, aren't found. A limit of 0 means the default, and results stop at
// the limit.
func TestFindFilesInARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	w := openTest(t)
	dir, ctx := w.Dir(), context.Background()
	gitIn(t, dir, "init", "-q")
	for _, p := range []string{"a1.go", "a2.go", "a3.go", "gone.go", ".cfg/x.go", "old.go"} {
		write(t, dir, p, "package a\n")
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "first")
	require.NoError(t, os.Remove(filepath.Join(dir, "gone.go")))
	gitIn(t, dir, "mv", "old.go", "new.go")

	all, err := w.FindPaths(ctx, "", 0, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a1.go", "a2.go", "a3.go", "new.go"}, all)
	got, err := w.FindPaths(ctx, "a", 2, false)
	require.NoError(t, err)
	assert.Len(t, got, 2)
	l, err := w.ListDir(ctx, "", false)
	require.NoError(t, err)
	assert.Equal(t, "renamed", entry(t, l, "new.go").Git)
}
