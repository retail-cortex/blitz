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
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/lsp"
	"github.com/retail-cortex/blitz/pkg/engine/lsp/lsptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLSPTool(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Foo() {}\n\nvar _ = Foo\n"), 0o644))
	m := lsp.NewManager(ws.Dir(), []lsp.Server{{Language: "go", Command: []string{"fake"}, Extensions: []string{".go"}}}, lsptest.Launcher())
	t.Cleanup(m.Close)
	ctx := context.Background()

	for _, tt := range []struct {
		name  string
		in    LSPInput
		check func(t *testing.T, out LSPOutput)
	}{
		{"definition", LSPInput{Operation: "definition", Path: "main.go", Line: 4, Column: 9}, func(t *testing.T, out LSPOutput) {
			require.Len(t, out.Locations, 1)
			assert.Equal(t, 2, out.Locations[0].Line)
		}},
		{"references", LSPInput{Operation: "references", Path: "main.go", Line: 2, Column: 6}, func(t *testing.T, out LSPOutput) {
			assert.Len(t, out.Locations, 2)
		}},
		{"hover", LSPInput{Operation: "hover", Path: "main.go", Line: 2, Column: 6}, func(t *testing.T, out LSPOutput) {
			assert.Contains(t, out.Hover, "func Foo()")
		}},
		{"symbols", LSPInput{Operation: "symbols", Path: "main.go", Query: "Foo"}, func(t *testing.T, out LSPOutput) {
			assert.Len(t, out.Symbols, 1)
		}},
		{"symbols without a query", LSPInput{Operation: "symbols", Path: "main.go"}, func(t *testing.T, out LSPOutput) {
			assert.Contains(t, out.Error, "needs a query")
		}},
		{"diagnostics", LSPInput{Operation: "diagnostics", Path: "main.go"}, func(t *testing.T, out LSPOutput) {
			require.Len(t, out.Diagnostics, 1)
			assert.Equal(t, "fake problem", out.Diagnostics[0].Message)
		}},
		{"an unknown operation", LSPInput{Operation: "rename", Path: "main.go"}, func(t *testing.T, out LSPOutput) {
			assert.Contains(t, out.Error, `operation "rename"`)
		}},
		{"a file outside", LSPInput{Operation: "hover", Path: "/etc/hosts", Line: 1, Column: 1}, func(t *testing.T, out LSPOutput) {
			assert.NotEmpty(t, out.Error)
		}},
		{"a language with no server", LSPInput{Operation: "hover", Path: "notes.txt", Line: 1, Column: 1}, func(t *testing.T, out LSPOutput) {
			assert.NotEmpty(t, out.Error)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.in.Path == "notes.txt" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644))
			}
			tt.check(t, runLSP(ctx, ws, m, tt.in))
		})
	}

	// After an edit, the file's new diagnostics (the server runs now).
	r := &Registry{workspace: ws, lsp: m}
	d := r.EditDiagnostics(ctx, "replace_in_file", map[string]any{"path": "main.go"}, map[string]any{"success": true})
	require.Len(t, d, 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main // fixed\n"), 0o644))
	assert.Empty(t, r.EditDiagnostics(ctx, "create_file", map[string]any{"path": "main.go"}, map[string]any{}))
	assert.Empty(t, r.EditDiagnostics(ctx, "read_file", map[string]any{"path": "main.go"}, map[string]any{}), "not an edit")
	assert.Empty(t, r.EditDiagnostics(ctx, "create_file", map[string]any{"path": "main.go"}, map[string]any{"error": "no"}), "a failed edit")
}

func TestLSPServersConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.LSP = map[string]config.LSPServerConfig{
		"go":     {Command: []string{"/opt/gopls"}},
		"python": {Disabled: true},
		"zig":    {Command: []string{"zls"}, Extensions: []string{".zig"}},
		"broken": {Command: []string{"x"}},
	}
	var got []string
	for _, s := range lspServers(cfg) {
		got = append(got, s.Language+":"+s.Command[0])
	}
	assert.Equal(t, []string{"go:/opt/gopls", "rust:rust-analyzer", "typescript:typescript-language-server", "zig:zls"}, got)
	_, err := lspLauncher(&ExecEnv{})(context.Background(), []string{"no-such-language-server"})
	assert.ErrorContains(t, err, "isn't installed")
}

// TestLSPGoplsSandboxed runs the real gopls as the registry does, guarded
// and in the OS sandbox; set BLITZ_TEST_GOPLS=1 with gopls installed.
func TestLSPGoplsSandboxed(t *testing.T) {
	if os.Getenv("BLITZ_TEST_GOPLS") == "" {
		t.Skip("set BLITZ_TEST_GOPLS=1 to ask gopls")
	}
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	cfg.Images.Enabled = false
	dir := cfg.Tools.WorkspaceDir
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { var x int = \"no\"; _ = x }\n"), 0o644))
	r, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer r.Close()
	out := runLSP(context.Background(), r.workspace, r.lsp, LSPInput{Operation: "diagnostics", Path: "main.go"})
	require.Empty(t, out.Error)
	require.NotEmpty(t, out.Diagnostics, "%+v", out)
	t.Log(out.Diagnostics[0].Message, " (sandbox: ", cfg.Sandbox.Shell, ")")
}

// The launcher starts a guarded process whose stdin and stdout are the
// server's; Stop ends it.
func TestLSPLauncher(t *testing.T) {
	p, err := lspLauncher(&ExecEnv{Dir: t.TempDir()})(context.Background(), []string{"cat"})
	require.NoError(t, err)
	_, err = p.Stdin().Write([]byte("hello\n"))
	require.NoError(t, err)
	buf := make([]byte, 6)
	_, err = io.ReadFull(p.Stdout(), buf)
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(buf))
	p.Stop()
}
