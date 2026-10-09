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
	"strings"
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

// inProcess runs lsptest.Serve in this process, over OS pipes (buffered,
// as a real server's stdio is).
func inProcess(context.Context, []string) (lsp.Process, error) {
	serverIn, clientOut, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	clientIn, serverOut, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	go func() { lsptest.Serve(serverIn, serverOut); serverOut.Close(); serverIn.Close() }()
	return &pipeProcess{in: clientOut, out: clientIn}, nil
}

type pipeProcess struct {
	in, out *os.File
}

func (p *pipeProcess) Stdin() io.WriteCloser { return p.in }
func (p *pipeProcess) Stdout() io.ReadCloser { return p.out }
func (p *pipeProcess) Stop()                 { p.in.Close(); p.out.Close() }

// The manager against the fake server, started as a process and in this
// one.
func TestManager(t *testing.T) {
	for name, launch := range map[string]lsp.Launcher{"process": lsptest.Launcher(), "in process": inProcess} {
		t.Run(name, func(t *testing.T) { testManager(t, launch) })
	}
}

func testManager(t *testing.T, launch lsp.Launcher) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	src := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(src, []byte("package main\nfunc Foo() {}\n\nvar _ = Foo\n"), 0o644))
	m := lsp.NewManager(dir, []lsp.Server{{Language: "go", Command: []string{"fake"}, Extensions: []string{".go"}}}, launch)
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

// TestGoplsSnippets asks gopls about code blocks (spec_visual_editor_037
// §7): files that exist only for the server, in the workspace's module, so
// a block sees the workspace's packages; and a fragment of statements made
// a whole file. Set BLITZ_TEST_GOPLS=1 with gopls installed.
func TestGoplsSnippets(t *testing.T) {
	if os.Getenv("BLITZ_TEST_GOPLS") == "" {
		t.Skip("set BLITZ_TEST_GOPLS=1 to ask gopls")
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n\ngo 1.22\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "greet"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet", "greet.go"), []byte("package greet\n\n// Hello greets.\nfunc Hello(name string) string { return \"hi \" + name }\n"), 0o644))
	launch := func(ctx context.Context, argv []string) (lsp.Process, error) { return startProcess(argv) }
	m := lsp.NewManager(dir, []lsp.Server{{Language: "go", Command: []string{"gopls"}, Extensions: []string{".go"}}}, launch)
	defer m.Close()
	ctx := context.Background()

	// A block with an import of the workspace's own package.
	text := "import \"example.com/demo/greet\"\n\nfunc main() {\n\tgreet.\n}\n"
	info := m.OpenSnippet("w", lsp.SnippetPath(dir, "abc", ".go"), text, 1)
	require.NotEmpty(t, info.ID)
	var list lsp.CompletionList
	require.Eventually(t, func() bool {
		var err error
		list, err = m.Complete(ctx, info.ID, 1, 4, 8, ".")
		return err == nil && len(list.Items) > 0
	}, 60*time.Second, 500*time.Millisecond, "completion in a block")
	var labels []string
	for _, c := range list.Items {
		labels = append(labels, c.Label)
	}
	assert.Contains(t, labels, "Hello", "the workspace's package, from a block")
	_, err := os.Stat(filepath.Join(dir, "blitz-snippets"))
	assert.ErrorIs(t, err, os.ErrNotExist, "nothing written")

	// A fragment of statements: wrapped, positions the block's own.
	frag := m.OpenSnippet("w", lsp.SnippetPath(dir, "def", ".go"), "x := 1\ny := undefinedName\nfmt.Println(x, y)\n", 1)
	got := make(chan lsp.DocumentDiagnostics, 8)
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_ = m.WatchDiagnostics(wctx, "w", func(d lsp.DocumentDiagnostics) error { got <- d; return nil })
	}()
	deadline := time.After(60 * time.Second)
	for {
		select {
		case d := <-got:
			if d.Document != frag.ID || len(d.Diagnostics) == 0 {
				continue
			}
			var undefined *lsp.DocumentDiagnostic
			severity := map[string]string{}
			for i, x := range d.Diagnostics {
				severity[x.Message] = x.Severity
				if strings.Contains(x.Message, "undefinedName") {
					undefined = &d.Diagnostics[i]
				}
			}
			require.NotNil(t, undefined)
			assert.Equal(t, "error", undefined.Severity)
			assert.Equal(t, "hint", severity["undefined: fmt"], "a fragment's missing import")
			assert.Equal(t, 2, undefined.Start.Line, "line 2 of the block, not of the wrapped file")
			assert.Equal(t, 6, undefined.Start.Column)
			return
		case <-deadline:
			t.Fatal("no diagnostics for the fragment")
		}
	}
}

// TestPyright asks pyright about a Python file and a Python code block
// (spec_visual_editor_037 §7): it answers only once told the client's
// configuration changed. Set BLITZ_TEST_PYRIGHT=1 with pyright installed.
func TestPyright(t *testing.T) {
	if os.Getenv("BLITZ_TEST_PYRIGHT") == "" {
		t.Skip("set BLITZ_TEST_PYRIGHT=1 to ask pyright")
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	launch := func(ctx context.Context, argv []string) (lsp.Process, error) { return startProcess(argv) }
	m := lsp.NewManager(dir, []lsp.Server{{Language: "python", Command: []string{"pyright-langserver", "--stdio"}, Extensions: []string{".py"}}}, launch)
	defer m.Close()
	ctx := context.Background()
	text := "import os\n\ndef greet(name: str) -> str:\n    return 'hi ' + name\n\nprint(greet(42))\nos.pa\n"
	for _, info := range []lsp.DocumentInfo{
		m.OpenDocument("w", filepath.Join(dir, "demo.py"), text, 1),
		m.OpenSnippet("w", lsp.SnippetPath(dir, "py", ".py"), text, 1),
	} {
		require.NotEmpty(t, info.ID)
		var list lsp.CompletionList
		require.Eventually(t, func() bool {
			var err error
			list, err = m.Complete(ctx, info.ID, 1, 7, 6, "")
			return err == nil && len(list.Items) > 0
		}, 60*time.Second, 500*time.Millisecond, "completion from pyright")
		var labels []string
		for _, it := range list.Items {
			labels = append(labels, it.Label)
		}
		assert.Contains(t, labels, "path")
	}
}
