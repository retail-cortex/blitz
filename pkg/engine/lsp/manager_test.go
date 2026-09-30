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

package lsp_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/lsp"
	"github.com/retail-cortex/blitz/pkg/engine/lsp/lsptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	lsptest.MaybeServe()
	os.Exit(m.Run())
}

func TestManager(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	src := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(src, []byte("package main\nfunc Foo() {}\n\nvar _ = Foo\n"), 0o644))
	m := lsp.NewManager(dir, []lsp.Server{{Language: "go", Command: []string{"fake"}, Extensions: []string{".go"}}}, lsptest.Launcher())
	defer m.Close()
	ctx := context.Background()

	assert.False(t, m.Running(src))
	diags, err := m.Diagnostics(ctx, src, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, []lsp.Diagnostic{{Line: 1, Column: 3, Severity: "error", Message: "fake problem", Source: "fake"}}, diags)
	assert.True(t, m.Running(src))

	defs, err := m.Definition(ctx, src, 4, 9)
	require.NoError(t, err)
	assert.Equal(t, []lsp.Location{{Path: "main.go", Line: 2, Column: 6, Text: "func Foo() {}"}}, defs)
	refs, err := m.References(ctx, src, 2, 6)
	require.NoError(t, err)
	assert.Len(t, refs, 2)
	assert.Equal(t, "var _ = Foo", refs[1].Text)
	hover, err := m.Hover(ctx, src, 2, 6)
	require.NoError(t, err)
	assert.Contains(t, hover, "func Foo()")
	syms, err := m.Symbols(ctx, src, "Foo")
	require.NoError(t, err)
	require.Len(t, syms, 1)
	assert.Equal(t, "function", syms[0].Kind)
	assert.Equal(t, "main", syms[0].Container)

	// The file changes on disk: its new content goes to the server.
	require.NoError(t, os.WriteFile(src, []byte("package main // fixed\n"), 0o644))
	diags, err = m.Diagnostics(ctx, src, 5*time.Second)
	require.NoError(t, err)
	assert.Empty(t, diags)

	_, err = m.Definition(ctx, filepath.Join(dir, "notes.txt"), 1, 1)
	assert.ErrorIs(t, err, lsp.ErrNoServer)
	_, err = m.Definition(ctx, src, 99, 1)
	assert.ErrorContains(t, err, "line 99")
}

func TestManagerCantStart(t *testing.T) {
	m := lsp.NewManager(t.TempDir(), []lsp.Server{{Language: "go", Command: []string{"nope"}, Extensions: []string{".go"}}},
		func(context.Context, []string) (lsp.Process, error) { return nil, fmt.Errorf("nope isn't installed") })
	defer m.Close()
	_, err := m.Hover(context.Background(), filepath.Join(t.TempDir(), "a.go"), 1, 1)
	assert.ErrorContains(t, err, "starting the go language server (nope): nope isn't installed")
}

// TestGopls asks the real gopls; set BLITZ_TEST_GOPLS=1 with it installed
// (bazel test --test_env=BLITZ_TEST_GOPLS=1 --test_env=PATH=$PATH).
func TestGopls(t *testing.T) {
	if os.Getenv("BLITZ_TEST_GOPLS") == "" {
		t.Skip("set BLITZ_TEST_GOPLS=1 to ask gopls")
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o644))
	src := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(src, []byte("package main\n\nfunc Answer() int { return 42 }\n\nfunc main() { _ = Answer() + \"x\" }\n"), 0o644))
	launch := func(ctx context.Context, argv []string) (lsp.Process, error) { return startProcess(argv) }
	m := lsp.NewManager(dir, []lsp.Server{{Language: "go", Command: []string{"gopls"}, Extensions: []string{".go"}}}, launch)
	defer m.Close()
	ctx := context.Background()
	defs, err := m.Definition(ctx, src, 5, 20)
	require.NoError(t, err)
	require.NotEmpty(t, defs)
	assert.Equal(t, 3, defs[0].Line)
	diags, err := m.Diagnostics(ctx, src, 30*time.Second)
	require.NoError(t, err)
	require.NotEmpty(t, diags, "a mismatched type")
	fmt.Println(diags[0].Message)
}

type osProcess struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out io.ReadCloser
}

func (p *osProcess) Stdin() io.WriteCloser { return p.in }
func (p *osProcess) Stdout() io.ReadCloser { return p.out }
func (p *osProcess) Stop()                 { p.in.Close(); p.cmd.Process.Kill(); p.cmd.Wait() }

func startProcess(argv []string) (lsp.Process, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &osProcess{cmd: cmd, in: in, out: out}, nil
}
