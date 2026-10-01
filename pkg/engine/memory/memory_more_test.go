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

package memory

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGlobalRulesAndMalformedRules checks the global rule directory and how
// rule files with odd frontmatter, no content or too much content load.
func TestGlobalRulesAndMalformedRules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rules := filepath.Join(home, ".blitz", "rules")
	write(t, filepath.Join(rules, "a-unclosed.md"), "---\npaths: x\nno closing line")
	write(t, filepath.Join(rules, "b-badyaml.md"), "---\nfoo: [x\n---\nbad yaml body")
	write(t, filepath.Join(rules, "c-empty.md"), "---\npaths: \"*.go\"\n---")
	write(t, filepath.Join(rules, "d-long.md"), "---\npaths: [\" \", \"*.go\"]\n---\n0123456789abcdef")
	locked := filepath.Join(rules, "locked")
	require.NoError(t, os.MkdirAll(locked, 0o755))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	cfg := defaults()
	cfg.MaxBytes = 10
	cfg.GlobalRules = "~/.blitz/rules"
	l := LoadAll(t.TempDir(), cfg)

	got := contents(l.Docs)
	require.Len(t, got, 2, "unclosed and unparsable frontmatter load as plain docs: %q", got)
	assert.Contains(t, got[0], "---", "the unclosed file is kept whole")
	assert.Contains(t, got[1], "---", "the unparsable file is kept whole")
	require.Len(t, l.Rules, 1, "the empty rule is dropped: %+v", l.Rules)
	assert.Equal(t, []string{"*.go"}, l.Rules[0].Paths, "blank globs are dropped")
	assert.Equal(t, "0123456789\n(truncated)", l.Rules[0].Content)
}

// TestImportFromHome checks that a ~/ import resolves inside ~/.blitz.
func TestImportFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, filepath.Join(home, ".blitz", "shared.md"), "shared rules")
	ws := t.TempDir()
	write(t, filepath.Join(ws, "BLITZ.md"), "see @~/.blitz/shared.md")
	docs := Load(ws, defaults())
	assert.Equal(t, []string{"see @~/.blitz/shared.md", "shared rules"}, contents(docs))
}

// TestUnreadableInstructionsAreSkipped checks that a file that exists but
// can't be read is skipped.
func TestUnreadableInstructionsAreSkipped(t *testing.T) {
	ws := t.TempDir()
	p := filepath.Join(ws, "BLITZ.md")
	write(t, p, "secret")
	require.NoError(t, os.Chmod(p, 0o000))
	if _, err := os.ReadFile(p); err == nil {
		t.Skip("running with privileges that ignore file modes")
	}
	assert.Empty(t, Load(ws, defaults()))
}

// TestRenderTruncatedAndRules checks the truncation marker and the format
// of rules handed out with a tool result.
func TestRenderTruncatedAndRules(t *testing.T) {
	r := Render([]Doc{{Path: "BLITZ.md", Content: "x", Truncated: true}})
	assert.Contains(t, r, "### BLITZ.md\nx\n(truncated)\n")

	got := RenderRules([]*Rule{
		{Path: "a.md", Paths: []string{"*.go", "cmd/*"}, Content: "A"},
		{Path: "b.md", Paths: []string{"*.ts"}, Content: "B"},
	})
	assert.Equal(t, "### a.md (applies to *.go, cmd/*)\nA\n\n### b.md (applies to *.ts)\nB", got)
	assert.Empty(t, RenderRules(nil))
}

// TestTrackedByGit checks that only files git tracks are reported.
func TestTrackedByGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	git("init", "-q")
	write(t, filepath.Join(repo, "tracked.md"), "x")
	write(t, filepath.Join(repo, "untracked.md"), "y")
	git("add", "tracked.md")
	assert.True(t, TrackedByGit(filepath.Join(repo, "tracked.md")))
	assert.False(t, TrackedByGit(filepath.Join(repo, "untracked.md")))
}

// TestAppendEdgeCases checks appending after a file without a final
// newline, and a workspace that doesn't exist.
func TestAppendEdgeCases(t *testing.T) {
	ws := t.TempDir()
	write(t, filepath.Join(ws, "BLITZ.md"), "# Mine")
	path, err := Append(ws, "BLITZ.md", "note")
	require.NoError(t, err)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "# Mine\n- note\n", string(b))

	_, err = Append(filepath.Join(ws, "missing"), "BLITZ.md", "note")
	assert.Error(t, err)
}

// TestNotesDirFollowsSymlinks checks that a workspace reached through a
// symlink shares the notes of its real directory.
func TestNotesDirFollowsSymlinks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(real, link))
	assert.Equal(t, NotesDir(real), NotesDir(link))
}

// TestNotesFilesystemErrors checks how notes handle directories and files
// that can't be used.
func TestNotesFilesystemErrors(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "file")
	write(t, file, "x")

	_, err := SaveNote(filepath.Join(file, "sub"), NoteFact, "a b c d e f g h")
	assert.Error(t, err, "the notes directory can't be created under a file")

	_, err = Notes(file)
	assert.Error(t, err, "a file isn't a notes directory")
	_, err = FindNote(file, "x")
	assert.Error(t, err)
	assert.Error(t, DeleteNote(base, "nothing"), "no note to delete")

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub.md"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "dangling.md")))
	notes, err := Notes(dir)
	require.NoError(t, err)
	assert.Empty(t, notes, "directories and unreadable files are skipped")

	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	_, err = SaveNote(dir, NoteFact, "x")
	if err == nil {
		t.Skip("running with privileges that ignore file modes")
	}
	assert.Error(t, err, "a read-only directory can't take a note")
}
