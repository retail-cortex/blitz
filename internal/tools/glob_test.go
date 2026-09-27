package tools

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func globFiles(t *testing.T, ws *Workspace, in GlobInput) GlobOutput {
	t.Helper()
	out, err := globWorkspace(context.Background(), ws, in)
	if err != nil {
		t.Fatalf("glob %+v: %v", in, err)
	}
	return out
}

func sorted(s []string) []string {
	c := append([]string(nil), s...)
	sort.Strings(c)
	return c
}

func globTree(t *testing.T) (*Workspace, string) {
	t.Helper()
	ws, dir := newTestWorkspace(t)
	for _, f := range []string{
		"main.go", "main_test.go", "README.md",
		"pkg/a/a.go", "pkg/a/a_test.go", "pkg/b/b.ts", "pkg/b/b.tsx",
		".github/workflows/ci.yml", ".hidden/x.go",
		"node_modules/dep/index.go", "vendor/v/v.go",
	} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(f)), "x")
	}
	return ws, dir
}

func TestGlobMatchesPatterns(t *testing.T) {
	ws, _ := globTree(t)
	cases := []struct {
		in   GlobInput
		want []string
	}{
		{GlobInput{Pattern: "*.go"}, []string{"main.go", "main_test.go"}},
		{GlobInput{Pattern: "**/*.go"}, []string{"main.go", "main_test.go", "pkg/a/a.go", "pkg/a/a_test.go"}},
		{GlobInput{Pattern: "**/*_test.go"}, []string{"main_test.go", "pkg/a/a_test.go"}},
		{GlobInput{Pattern: "pkg/**/*.{ts,tsx}"}, []string{"pkg/b/b.ts", "pkg/b/b.tsx"}},
		{GlobInput{Pattern: "?.go", Path: "pkg/a"}, []string{"pkg/a/a.go"}},
		// Dot and dependency directories are skipped unless named.
		{GlobInput{Pattern: ".github/**/*.yml"}, []string{".github/workflows/ci.yml"}},
		{GlobInput{Pattern: "node_modules/**/*.go"}, []string{"node_modules/dep/index.go"}},
		{GlobInput{Pattern: "**/*.yml"}, nil},
	}
	for _, c := range cases {
		got := globFiles(t, ws, c.in)
		if !reflect.DeepEqual(sorted(got.Files), sorted(c.want)) && !(len(got.Files) == 0 && len(c.want) == 0) {
			t.Errorf("glob %+v = %v, want %v", c.in, got.Files, c.want)
		}
		if got.Count != len(got.Files) {
			t.Errorf("count %d for %v", got.Count, got.Files)
		}
	}
}

func TestGlobNewestFirstAndCapped(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	now := time.Now()
	for i, name := range []string{"old.txt", "mid.txt", "new.txt"} {
		p := filepath.Join(dir, name)
		writeFile(t, p, "x")
		mt := now.Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	out := globFiles(t, ws, GlobInput{Pattern: "*.txt"})
	if want := []string{"new.txt", "mid.txt", "old.txt"}; !reflect.DeepEqual(out.Files, want) || out.Truncated {
		t.Errorf("order %v (truncated %v), want %v", out.Files, out.Truncated, want)
	}
	out = globFiles(t, ws, GlobInput{Pattern: "*.txt", MaxResults: 2})
	if len(out.Files) != 2 || !out.Truncated || out.Files[0] != "new.txt" {
		t.Errorf("capped: %+v", out)
	}
}

func TestGlobRespectsTheSandbox(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.go"), "x")
	writeFile(t, filepath.Join(dir, "secrets", "key.pem"), "x")
	writeFile(t, filepath.Join(dir, ".env"), "x")
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: dir, BlockedPaths: []string{"*.pem", ".env"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	if out := globFiles(t, ws, GlobInput{Pattern: "**/*"}); !reflect.DeepEqual(out.Files, []string{"app.go"}) {
		t.Errorf("blocked files listed: %v", out.Files)
	}
	if out := globFiles(t, ws, GlobInput{Pattern: ".env"}); len(out.Files) != 0 {
		t.Errorf("blocked .env matched by name: %v", out.Files)
	}
	for _, bad := range []GlobInput{{Pattern: "../*"}, {Pattern: "/etc/*"}, {Pattern: "a/../../*"}, {Pattern: "*", Path: ".."}, {Pattern: ""}, {Pattern: "{a,b"}} {
		if _, err := globWorkspace(context.Background(), ws, bad); err == nil {
			t.Errorf("glob %+v should fail", bad)
		}
	}
}

func TestExpandBraces(t *testing.T) {
	got, err := expandBraces("src/{a,b/{c,d}}/*.{go,ts}")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/a/*.go", "src/a/*.ts", "src/b/c/*.go", "src/b/c/*.ts", "src/b/d/*.go", "src/b/d/*.ts"}
	if !reflect.DeepEqual(sorted(got), sorted(want)) {
		t.Errorf("expandBraces = %v", got)
	}
	if _, err := expandBraces(strings.Repeat("{a,b}", 7)); err == nil {
		t.Error("expected a cap on alternatives")
	}
}
