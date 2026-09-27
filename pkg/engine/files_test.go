package engine

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

func write(t *testing.T, dir, rel, data string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(l), []string{"pkg", "clean.txt", "main.go"}; !slices.Equal(got, want) || !l.Repo {
		t.Errorf("visible %v (repo %v), want %v", got, l.Repo, want)
	}
	if e := entry(t, l, "main.go"); e.Git != "modified" || e.Kind != KindFile {
		t.Errorf("main.go: %+v", e)
	}
	if e := entry(t, l, "pkg"); e.Git != "changed" || e.Kind != KindFolder {
		t.Errorf("pkg: %+v", e)
	}
	if e := entry(t, l, "clean.txt"); e.Git != "" {
		t.Errorf("clean.txt: %+v", e)
	}

	all, err := w.ListDir(ctx, ".", true)
	if err != nil {
		t.Fatal(err)
	}
	for name, hidden := range map[string]string{"build": "ignored", "debug.log": "ignored", ".env": "blocked", ".editorconfig": "dot", ".gitignore": "dot"} {
		if e := entry(t, all, name); e.Hidden != hidden {
			t.Errorf("%s hidden %q, want %q", name, e.Hidden, hidden)
		}
	}
	if e := entry(t, all, ".env"); e.AgentRule != "blocked" {
		t.Errorf(".env rule %q", e.AgentRule)
	}
	if slices.Contains(names(all), ".git") {
		t.Error(".git listed")
	}
	sub, _ := w.ListDir(ctx, "pkg", false)
	if e := entry(t, sub, "new.go"); e.Git != "untracked" || e.Path != "pkg/new.go" {
		t.Errorf("pkg/new.go: %+v", e)
	}
}

func TestFilesStayInTheWorkspace(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	outside := t.TempDir()
	write(t, outside, "secret.txt", "no")
	if err := os.Symlink(outside, filepath.Join(w.Dir(), "out")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"../x", "/etc/passwd", "out/secret.txt", ".git/config", "a/../../x"} {
		if _, err := w.ReadFile(p); err == nil {
			t.Errorf("read %s", p)
		}
		if _, err := w.WriteFile(ctx, p, "x", ""); err == nil {
			t.Errorf("wrote %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "x")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("wrote outside")
	}
	if err := w.DeleteFile(ctx, ""); err == nil {
		t.Error("deleted the workspace")
	}
}

func TestReadWriteVersions(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	v, err := w.WriteFile(ctx, "src/a.txt", "one\n", "")
	if err != nil {
		t.Fatal(err)
	}
	f, err := w.ReadFile("src/a.txt")
	if err != nil || f.Text != "one\n" || f.Version != v || f.Path != "src/a.txt" {
		t.Fatalf("read %+v %v", f, err)
	}
	// Creating over an existing file, or saving an old version, is refused.
	var changed *FileChangedError
	if _, err := w.WriteFile(ctx, "src/a.txt", "x", ""); !errors.As(err, &changed) || changed.Current != v {
		t.Errorf("create over existing: %v", err)
	}
	write(t, w.Dir(), "src/a.txt", "the agent's\n")
	if _, err := w.WriteFile(ctx, "src/a.txt", "two\n", v); !errors.As(err, &changed) || changed.Current != FileVersion([]byte("the agent's\n")) {
		t.Errorf("stale save: %v", err)
	}
	if v2, err := w.WriteFile(ctx, "src/a.txt", "two\n", changed.Current); err != nil || v2 == v {
		t.Errorf("overwrite: %v", err)
	}
	if got := w.StatFiles([]string{"src/a.txt", "gone.txt"}); got["src/a.txt"] != FileVersion([]byte("two\n")) || got["gone.txt"] != "" {
		t.Errorf("stat %v", got)
	}

	write(t, w.Dir(), "bin.dat", "a\x00b")
	if f, _ := w.ReadFile("bin.dat"); !f.Binary || f.Text != "" {
		t.Errorf("binary %+v", f)
	}
	big := strings.Repeat("x", maxEditorBytes+1)
	write(t, w.Dir(), "big.txt", big)
	if f, _ := w.ReadFile("big.txt"); !f.TooLarge || f.Size != int64(len(big)) {
		t.Errorf("too large %+v", f)
	}
}

func TestCreateRenameDelete(t *testing.T) {
	w := openTest(t)
	ctx := context.Background()
	if err := w.CreateFolder("docs/guides"); err != nil {
		t.Fatal(err)
	}
	if err := w.CreateFolder("docs"); !errors.Is(err, fs.ErrExist) {
		t.Errorf("existing folder: %v", err)
	}
	write(t, w.Dir(), "docs/a.md", "a")
	write(t, w.Dir(), "docs/b.md", "b")
	if err := w.RenameFile(ctx, "docs/a.md", "docs/b.md"); !errors.Is(err, fs.ErrExist) {
		t.Errorf("rename over: %v", err)
	}
	if err := w.RenameFile(ctx, "docs/a.md", "notes/a.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w.Dir(), "notes", "a.md")); err != nil {
		t.Error("not moved")
	}
	if err := w.DeleteFile(ctx, "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w.Dir(), "docs")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("not deleted")
	}
	if err := w.DeleteFile(ctx, "docs"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("delete missing: %v", err)
	}
}

// The agent is told once about the user's edits, and the note isn't shown
// as part of the user's prompt.
func TestUserEditsReachTheNextTurn(t *testing.T) {
	w, llm := openTestWith(t, nil, text("ok"), text("ok again"))
	ctx := context.Background()
	if _, err := w.WriteFile(ctx, "a.go", "package a\n", ""); err != nil {
		t.Fatal(err)
	}
	write(t, w.Dir(), "b.go", "package b\n")
	if err := w.DeleteFile(ctx, "b.go"); err != nil {
		t.Fatal(err)
	}
	sid := newSession(t, w).ID
	if _, err := w.Run(ctx, sid, api.Turn{Text: "go on"}, ignore); err != nil {
		t.Fatal(err)
	}
	sent := lastUserText(llm)
	if !strings.Contains(sent, "<user-edits>") || !strings.Contains(sent, "a.go (created)") || !strings.Contains(sent, "b.go (deleted)") {
		t.Errorf("first turn: %q", sent)
	}
	if _, err := w.Run(ctx, sid, api.Turn{Text: "more"}, ignore); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(lastUserText(llm), "user-edits") {
		t.Error("told twice")
	}
	if got := transcript(t, w); !slices.Equal(got, []string{"user: go on", "model: ok", "user: more", "model: ok again"}) {
		t.Errorf("transcript %q", got)
	}
}

func TestFindFiles(t *testing.T) {
	w, _ := openTestWith(t, func(c *config.Config) { c.Sandbox.BlockedPaths = []string{"*.pem"} })
	for _, p := range []string{"internal/cart/discount.go", "internal/cart/discount_test.go", "docs/discounts.md", "cmd/shop/main.go", ".hidden/x.go", "key.pem"} {
		write(t, w.Dir(), p, "x")
	}
	got, err := w.FindFiles(context.Background(), "discount", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "internal/cart/discount.go" {
		t.Errorf("discount: %v", got)
	}
	if got, _ := w.FindFiles(context.Background(), "csm", 10); len(got) == 0 || got[0] != "cmd/shop/main.go" {
		t.Errorf("csm: %v", got)
	}
	all, _ := w.FindFiles(context.Background(), "", 100)
	if slices.Contains(all, ".hidden/x.go") || slices.Contains(all, "key.pem") {
		t.Errorf("hidden files found: %v", all)
	}
}

func TestUserPath(t *testing.T) {
	for in, want := range map[string]string{"": ".", "a/b": filepath.Join("a", "b"), "a//b/": filepath.Join("a", "b"), "./x": "x"} {
		if got, err := tools.UserPath(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"..", "../a", "/a", ".git", ".git/HEAD"} {
		if _, err := tools.UserPath(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}
