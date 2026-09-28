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

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathMatcher(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	m, err := NewPathMatcher([]string{".env", "*.pem", "~/.ssh", "config/prod/*.yaml", "/opt/secret/**/token"}, []string{root})
	require.NoError(t, err)

	blocked := []string{
		filepath.Join(root, ".env"),
		filepath.Join(root, "deep", "nested", ".env"),
		filepath.Join(root, ".env", "inside-dir"), // a blocked directory blocks its contents
		filepath.Join(root, "certs", "server.pem"),
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(root, "config", "prod", "db.yaml"),
		"/opt/secret/token",
		"/opt/secret/a/b/token",
	}
	for _, p := range blocked {
		t.Run(p, func(t *testing.T) {
			_, ok := m.Match(p)
			assert.True(t, ok, "expected %s to be blocked", p)
		})
	}
	allowed := []string{
		filepath.Join(root, ".env.example"),
		filepath.Join(root, "env"),
		filepath.Join(root, "server.pem.txt"),
		filepath.Join(home, ".sshrc"),
		filepath.Join(root, "config", "dev", "db.yaml"),
		filepath.Join(root, "config", "prod", "sub", "db.yaml"), // "*" doesn't cross "/"
		"/opt/secret/tokens",
	}
	for _, p := range allowed {
		t.Run(p, func(t *testing.T) {
			pat, ok := m.Match(p)
			assert.False(t, ok, "expected %s to be allowed, blocked by %q", p, pat)
		})
	}

	if caseInsensitiveFS {
		_, ok := m.Match(filepath.Join(root, ".ENV"))
		assert.True(t, ok, "expected case-insensitive match on this platform")
	}

	// Negative: patterns that could break out of the sandbox profile string.
	_, err = NewPathMatcher([]string{`bad"pattern`}, nil)
	assert.Error(t, err, "expected error for pattern containing a quote")
	var nilMatcher *PathMatcher
	_, ok := nilMatcher.Match("/x")
	assert.False(t, ok, "nil matcher should match nothing")
}

type sandboxFixture struct {
	ws                          *Workspace
	work, shared, docs, outside string
}

func newSandboxFixture(t *testing.T) sandboxFixture {
	t.Helper()
	f := sandboxFixture{work: t.TempDir(), shared: t.TempDir(), docs: t.TempDir(), outside: t.TempDir()}
	writeFile(t, filepath.Join(f.work, "main.go"), "package main\n// secret-token here\n")
	writeFile(t, filepath.Join(f.work, ".env"), "API_KEY=secret-token\n")
	writeFile(t, filepath.Join(f.work, "certs", "tls.pem"), "secret-token")
	writeFile(t, filepath.Join(f.work, "vendor-ro", "lib.go"), "package lib\n")
	writeFile(t, filepath.Join(f.shared, "notes.md"), "shared notes\n")
	writeFile(t, filepath.Join(f.docs, "guide.md"), "read only guide\n")
	writeFile(t, filepath.Join(f.outside, "x.txt"), "outside\n")

	ws, err := OpenWorkspace(WorkspaceOptions{
		Dir:           f.work,
		AllowedPaths:  []string{f.shared},
		ReadOnlyPaths: []string{f.docs, filepath.Join(f.work, "vendor-ro")},
		BlockedPaths:  []string{".env", "*.pem"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	f.ws = ws
	return f
}

func TestWorkspaceMultiRootAccess(t *testing.T) {
	f := newSandboxFixture(t)
	ws := f.ws

	// Positive: read and write in the extra read-write root by absolute path.
	notes := filepath.Join(f.shared, "notes.md")
	b, err := ws.ReadFile(notes)
	assert.NoError(t, err, "read allowed root: %q", b)
	assert.Equal(t, "shared notes\n", string(b), "read allowed root: %q %v", b, err)
	assert.NoError(t, ws.WriteFileAtomic(filepath.Join(f.shared, "new.md"), []byte("x")), "write allowed root")
	// Display paths: workspace-relative inside, absolute outside.
	got, _ := ws.Rel(filepath.Join(f.work, "main.go"))
	assert.Equal(t, "main.go", got, "Rel inside workspace = %q", got)
	got, _ = ws.Rel(notes)
	assert.Equal(t, filepath.Join(ws.RootDirs()[1], "notes.md"), got, "Rel in allowed root = %q", got)

	// Positive: read-only roots are readable...
	guide := filepath.Join(f.docs, "guide.md")
	_, err = ws.ReadFile(guide)
	assert.NoError(t, err, "read read-only root")
	// ...negative: but not writable or deletable, including a read-only root
	// nested inside the writable workspace.
	for _, p := range []string{guide, filepath.Join(f.docs, "new.md"), "vendor-ro/lib.go"} {
		t.Run(p, func(t *testing.T) {
			err := ws.WriteFileAtomic(p, []byte("x"))
			assert.ErrorIs(t, err, ErrReadOnlyPath, "write %s: expected ErrReadOnlyPath, got %v", p, err)
			_, err = ws.WritablePath(p)
			assert.ErrorIs(t, err, ErrReadOnlyPath, "WritablePath %s: expected ErrReadOnlyPath, got %v", p, err)
		})
	}
	err = ws.RemoveFile(guide)
	assert.ErrorIs(t, err, ErrReadOnlyPath, "delete in read-only root: %v", err)

	// Negative: paths outside every root.
	_, err = ws.ReadFile(filepath.Join(f.outside, "x.txt"))
	assert.ErrorIs(t, err, ErrOutsideWorkspace, "expected ErrOutsideWorkspace, got %v", err)
}

func TestWorkspaceBlockedPaths(t *testing.T) {
	f := newSandboxFixture(t)
	ws := f.ws

	for _, p := range []string{".env", "certs/tls.pem", filepath.Join(f.work, ".env")} {
		t.Run(p, func(t *testing.T) {
			_, err := ws.ReadFile(p)
			assert.ErrorIs(t, err, ErrBlockedPath, "read %s: expected ErrBlockedPath, got %v", p, err)
			err = ws.WriteFileAtomic(p, []byte("x"))
			assert.ErrorIs(t, err, ErrBlockedPath, "write %s: expected ErrBlockedPath, got %v", p, err)
		})
	}
	// Creating a new blocked file is refused too.
	assert.ErrorIs(t, ws.CreateExclusive("sub/.env", []byte("x")), ErrBlockedPath, "create blocked")

	// A symlink with an innocent name pointing at a blocked file is refused.
	require.NoError(t, os.Symlink(".env", filepath.Join(f.work, "innocent.txt")))
	_, err := ws.ReadFile("innocent.txt")
	assert.ErrorIs(t, err, ErrBlockedPath, "symlink to blocked file: expected ErrBlockedPath, got %v", err)

	// grep and list_files never surface blocked content.
	m, err := grepWorkspace(context.Background(), ws, GrepInput{Query: "secret-token"})
	require.NoError(t, err)
	assert.Len(t, m, 1, "grep leaked blocked files: %v", m)
	assert.Equal(t, "main.go", m[0].File, "grep leaked blocked files: %v", m)
	_, err = grepWorkspace(context.Background(), ws, GrepInput{Query: "x", Path: ".env"})
	assert.Error(t, err, "expected grep on a blocked path to fail")
	out := runTool(t, toolOf(t)(NewListFilesTool(ws)), map[string]any{"recursive": true})
	for _, e := range out["files"].([]any) {
		p := e.(map[string]any)["path"].(string)
		assert.False(t, strings.HasSuffix(p, ".pem"), "list_files showed blocked file %s", p)
	}
}

func TestWorkspaceRejectsBadRoots(t *testing.T) {
	dir := t.TempDir()
	_, err := OpenWorkspace(WorkspaceOptions{Dir: dir, AllowedPaths: []string{filepath.Join(dir, "missing")}})
	assert.Error(t, err, "expected error for missing allowed path")
	file := filepath.Join(dir, "f")
	writeFile(t, file, "x")
	_, err = OpenWorkspace(WorkspaceOptions{Dir: dir, ReadOnlyPaths: []string{file}})
	assert.Error(t, err, "expected error when a root is a file")
}

func TestWriteToolsSkipApprovalWhenSandboxRefuses(t *testing.T) {
	f := newSandboxFixture(t)
	hooks, reqs := approverHooks(true)
	create := toolOf(t)(NewCreateFileTool(f.ws, hooks))
	out := runTool(t, create, map[string]any{"path": filepath.Join(f.docs, "x.md"), "content": "x"})
	assert.Contains(t, errOf(out), "read-only", "expected read-only error, got %v", out)
	out = runTool(t, toolOf(t)(NewDeleteFileTool(f.ws, hooks)), map[string]any{"path": ".env"})
	assert.Contains(t, errOf(out), "blocked", "expected blocked error, got %v", out)
	assert.Len(t, *reqs, 0, "user was asked to approve %d writes the sandbox refuses", len(*reqs))
}

func TestDefaultShellWritableDirs(t *testing.T) {
	dirs := DefaultShellWritableDirs()
	tmp, err := canonicalDir(os.TempDir())
	require.NoError(t, err)
	found := false
	for _, d := range dirs {
		t.Run(d, func(t *testing.T) {
			if d == tmp || strings.HasPrefix(tmp, d+string(filepath.Separator)) {
				found = true
			}
			assert.True(t, filepath.IsAbs(d), "non-absolute writable dir %q", d)
		})
	}
	assert.True(t, found, "temp dir %s not writable by default: %v", tmp, dirs)
}

func TestExecEnvScrubsCredentials(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-leak")
	t.Setenv("MY_SERVICE_SECRET", "hunter2")
	t.Setenv("HARMLESS_VALUE", "visible")
	ws, _ := newTestWorkspace(t)
	cfg := ShellConfig{Workspace: ws, Hooks: allowAll(), Exec: &ExecEnv{ScrubEnv: []string{"*_api_key", "*_SECRET"}}}

	out := runShellCommand(context.Background(), cfg, RunShellCommandInput{Command: "env"})
	assert.NotContains(t, out.Output, "sk-leak", "credentials leaked to child process:\n%s", out.Output)
	assert.NotContains(t, out.Output, "hunter2", "credentials leaked to child process:\n%s", out.Output)
	assert.Contains(t, out.Output, "HARMLESS_VALUE=visible", "non-secret variables should pass through")
	// Without scrubbing configured, the environment is inherited unchanged.
	cfg.Exec = &ExecEnv{}
	out = runShellCommand(context.Background(), cfg, RunShellCommandInput{Command: "env"})
	assert.Contains(t, out.Output, "sk-leak", "expected unscrubbed environment when ScrubEnv is empty")
}

// Commands run in the ExecEnv's directory, not the process's.
func TestExecEnvRunsInItsDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(t.TempDir())
	cmd, err := (&ExecEnv{Dir: dir}).command(context.Background(), []string{"pwd", "-P"})
	require.NoError(t, err)
	out, err := cmd.Output()
	want, _ := filepath.EvalSymlinks(dir)
	assert.NoError(t, err, "ran in %q (%v), want %q", out, err, want)
	assert.Equal(t, want, strings.TrimSpace(string(out)), "ran in %q (%v), want %q", out, err, want)
}

// Relative extra roots are relative to the workspace, wherever the process is.
func TestExtraRootsResolveAgainstTheWorkspace(t *testing.T) {
	base := t.TempDir()
	ws, shared := filepath.Join(base, "ws"), filepath.Join(base, "shared")
	os.MkdirAll(ws, 0o755)
	os.MkdirAll(shared, 0o755)
	t.Chdir(t.TempDir())
	w, err := OpenWorkspace(WorkspaceOptions{Dir: ws, ReadOnlyPaths: []string{"../shared"}})
	require.NoError(t, err)
	defer w.Close()
	want, _ := filepath.EvalSymlinks(shared)
	assert.Contains(t, w.RootDirs(), want, "roots %v lack %s", w.RootDirs(), want)
}
