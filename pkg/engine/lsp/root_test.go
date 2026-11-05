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

package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tree makes the files under root (folders end in /).
func tree(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if filepath.ToSlash(f)[len(f)-1] == '/' {
			require.NoError(t, os.MkdirAll(p, 0o755))
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, nil, 0o644))
	}
}

// A file's server runs in the nearest project root above it inside the
// workspace, by its language's markers, else in the workspace's root
// (FS-61a).
func TestProjectRoot(t *testing.T) {
	outer := t.TempDir()
	tree(t, outer, "tsconfig.json", "go.mod") // above the workspace: never a root
	root := filepath.Join(outer, "ws")
	tree(t, root,
		"frontend/package.json", "frontend/tsconfig.json", "frontend/src/main.tsx",
		"docs/package.json", "docs/site/app.ts",
		"packages/ui/package.json", "packages/ui/src/button.tsx",
		"scripts/build.ts",
		"backend/pyproject.toml", "backend/app/main.py",
		"tools/.venv/", "tools/run.py",
		"go.work", "svc/go.mod", "svc/cmd/main.go", "lib/go.mod", "lib/x.go",
		"crates/core/Cargo.toml", "crates/core/src/lib.rs",
	)
	m := NewManager(root, nil, nil)
	t.Cleanup(m.Close)
	for _, tc := range []struct {
		name, language, path, want string
	}{
		{"a TypeScript project in a folder", "typescript", "frontend/src/main.tsx", "frontend"},
		{"another beside it", "typescript", "docs/site/app.ts", "docs"},
		{"a workspace package", "typescript", "packages/ui/src/button.tsx", "packages/ui"},
		{"no marker below the workspace: its root", "typescript", "scripts/build.ts", "."},
		{"Python by pyproject.toml", "python", "backend/app/main.py", "backend"},
		{"Python by its .venv", "python", "tools/run.py", "tools"},
		{"go.work wins over a nearer go.mod", "go", "svc/cmd/main.go", "."},
		{"Rust by Cargo.toml", "rust", "crates/core/src/lib.rs", "crates/core"},
		{"a language with no markers", "elixir", "frontend/src/main.ex", "."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, filepath.Join(root, filepath.FromSlash(tc.want)), m.projectRoot(tc.language, filepath.Join(root, filepath.FromSlash(tc.path))))
		})
	}

	// Without go.work, each module is its own root.
	require.NoError(t, os.Remove(filepath.Join(root, "go.work")))
	assert.Equal(t, filepath.Join(root, "svc"), m.projectRoot("go", filepath.Join(root, "svc", "cmd", "main.go")))
	// Outside the workspace (a code block's scratch folder): the workspace.
	assert.Equal(t, root, m.projectRoot("typescript", filepath.Join(outer, "elsewhere", "a.ts")))
	assert.Equal(t, root, m.projectRoot("typescript", filepath.Join(root, "a.ts")), "the workspace's own files")
}

// recording launches fake servers and keeps each one's folder and
// initialize params.
type recording struct {
	mu    sync.Mutex
	dirs  []string
	inits []map[string]any
}

func (r *recording) launcher() Launcher {
	f := &fakeServer{answer: func(_ *fakeServer, method string, params json.RawMessage) (any, string) {
		if method == "initialize" {
			var p map[string]any
			json.Unmarshal(params, &p)
			r.mu.Lock()
			r.inits = append(r.inits, p)
			r.mu.Unlock()
		}
		return map[string]any{}, ""
	}}
	launch := f.launcher()
	return func(ctx context.Context, dir string, argv []string) (Process, error) {
		r.mu.Lock()
		r.dirs = append(r.dirs, dir)
		r.mu.Unlock()
		return launch(ctx, dir, argv)
	}
}

// Each project root gets its own server, started there and initialized
// with it as the root and workspace folder; files in one root share one;
// TypeScript is told where the project's own is.
func TestServersPerProjectRoot(t *testing.T) {
	root := t.TempDir()
	tree(t, root, "frontend/package.json", "frontend/node_modules/typescript/lib/tsserver.js", "frontend/a.ts", "frontend/src/b.ts",
		"docs/package.json", "docs/c.ts")
	rec := &recording{}
	m := NewManager(root, []Server{{Language: "typescript", Command: []string{"typescript-language-server", "--stdio"}, Extensions: []string{".ts"}}}, rec.launcher())
	t.Cleanup(m.Close)
	ctx := context.Background()
	front, err := m.For(ctx, filepath.Join(root, "frontend", "a.ts"))
	require.NoError(t, err)
	again, err := m.For(ctx, filepath.Join(root, "frontend", "src", "b.ts"))
	require.NoError(t, err)
	assert.Same(t, front, again, "one server for the project")
	docs, err := m.For(ctx, filepath.Join(root, "docs", "c.ts"))
	require.NoError(t, err)
	assert.NotSame(t, front, docs, "its own for another project")
	assert.True(t, m.Running(filepath.Join(root, "docs", "c.ts")))
	assert.False(t, m.Running(filepath.Join(root, "other.ts")), "none yet in the workspace's root")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Len(t, rec.inits, 2)
	assert.Equal(t, []string{filepath.Join(root, "frontend"), filepath.Join(root, "docs")}, rec.dirs, "started in their roots")
	for i, dir := range rec.dirs {
		assert.Equal(t, fileURI(dir), rec.inits[i]["rootUri"])
		assert.Equal(t, []any{map[string]any{"uri": fileURI(dir), "name": filepath.Base(dir)}}, rec.inits[i]["workspaceFolders"])
	}
	assert.Equal(t, map[string]any{"tsserver": map[string]any{"path": filepath.Join(root, "frontend", "node_modules", "typescript", "lib")}}, rec.inits[0]["initializationOptions"])

	// Restart stops both, and Status reports the language once.
	require.NoError(t, m.Restart("typescript"))
	m.mu.Lock()
	assert.Empty(t, m.running)
	m.mu.Unlock()
	assert.Len(t, m.Status(), 1)
}

// A server that can't start in one project root fails there alone, and
// says where.
func TestStartFailsPerRoot(t *testing.T) {
	root := t.TempDir()
	tree(t, root, "bad/package.json", "bad/a.ts", "good/package.json", "good/b.ts")
	rec := &recording{}
	ok := rec.launcher()
	m := NewManager(root, []Server{{Language: "typescript", Command: []string{"tls"}, Extensions: []string{".ts"}}},
		func(ctx context.Context, dir string, argv []string) (Process, error) {
			if filepath.Base(dir) == "bad" {
				return nil, errors.New("Could not find a valid TypeScript installation")
			}
			return ok(ctx, dir, argv)
		})
	t.Cleanup(m.Close)
	_, err := m.For(context.Background(), filepath.Join(root, "bad", "a.ts"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "starting the typescript language server (tls) in bad: Could not find")
	_, err = m.For(context.Background(), filepath.Join(root, "good", "b.ts"))
	require.NoError(t, err, "another project isn't held back")
	state, _ := m.stateOf("typescript")
	assert.Equal(t, StateReady, state, "one runs")
}

// TypeScript for the typescript server: the project's, a folder's above
// it in the workspace, else a global install's (beside tsc, or beside the
// server), else none.
func TestTsserverLib(t *testing.T) {
	lib := func(dir string) string { return filepath.Join(dir, "node_modules", "typescript", "lib") }
	global := t.TempDir()
	tree(t, global, "lib/node_modules/typescript/lib/tsserver.js", "lib/node_modules/typescript/bin/tsc",
		"lib/node_modules/typescript-language-server/lib/cli.mjs", "bin/")
	require.NoError(t, os.Symlink(filepath.Join(global, "lib", "node_modules", "typescript", "bin", "tsc"), filepath.Join(global, "bin", "tsc")))
	require.NoError(t, os.Symlink(filepath.Join(global, "lib", "node_modules", "typescript-language-server", "lib", "cli.mjs"), filepath.Join(global, "bin", "typescript-language-server")))
	globalLib := filepath.Join(global, "lib", "node_modules", "typescript", "lib")
	look := tsLookPath
	t.Cleanup(func() { tsLookPath = look })
	for _, tc := range []struct {
		name   string
		files  []string
		onPath map[string]string // what tsLookPath finds
		root   string
		want   string
	}{
		{"the project's own", []string{"web/node_modules/typescript/lib/tsserver.js", "node_modules/typescript/lib/tsserver.js"}, nil, "web", "web"},
		{"hoisted to the workspace", []string{"node_modules/typescript/lib/tsserver.js"}, nil, "packages/ui", "."},
		{"beside tsc on PATH", nil, map[string]string{"tsc": filepath.Join(global, "bin", "tsc")}, "web", "global"},
		{"beside the server", nil, map[string]string{"typescript-language-server": filepath.Join(global, "bin", "typescript-language-server")}, "web", "global"},
		{"none", nil, nil, "web", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := t.TempDir()
			tree(t, ws, tc.files...)
			tsLookPath = func(name string) (string, error) {
				if p, ok := tc.onPath[name]; ok {
					return p, nil
				}
				return "", errors.New("not found")
			}
			m := NewManager(ws, nil, nil)
			t.Cleanup(m.Close)
			got := m.tsserverLib(filepath.Join(ws, filepath.FromSlash(tc.root)), "typescript-language-server")
			switch tc.want {
			case "":
				assert.Empty(t, got)
			case "global":
				want, _ := filepath.EvalSymlinks(globalLib)
				got, _ = filepath.EvalSymlinks(got)
				assert.Equal(t, want, got)
			default:
				assert.Equal(t, lib(filepath.Join(ws, filepath.FromSlash(tc.want))), got)
			}
		})
	}
}
