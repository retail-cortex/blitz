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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/lsp"
	"github.com/retail-cortex/blitz/pkg/engine/lsp/lsptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The lsp tool answers through the language server, says when a file has
// no problems, and fails for a file that doesn't exist.
func TestLSPToolRuns(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ok.go"), []byte("package main // fixed\n"), 0o644))
	m := lsp.NewManager(ws.Dir(), []lsp.Server{{Language: "go", Command: []string{"fake"}, Extensions: []string{".go"}}}, lsptest.Launcher())
	t.Cleanup(m.Close)
	rt := toolOf(t)(NewLSPTool(ws, m))

	tests := []struct {
		name    string
		args    map[string]any
		key     string
		want    string
		wantErr bool
	}{
		{"hover", map[string]any{"operation": "hover", "path": "ok.go", "line": 1, "column": 1}, "hover", "func Foo()", false},
		{"no problems", map[string]any{"operation": "diagnostics", "path": "ok.go"}, "note", "No problems reported.", false},
		{"a missing file", map[string]any{"operation": "hover", "path": "gone.go", "line": 1, "column": 1}, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runTool(t, rt, tt.args)
			if tt.wantErr {
				assert.NotEmpty(t, errOf(out))
				return
			}
			assert.Empty(t, errOf(out))
			assert.Contains(t, out[tt.key], tt.want)
		})
	}
}

// An edit's diagnostics are only asked for when the file's server runs.
func TestEditDiagnosticsWithoutServer(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
	ctx := context.Background()
	assert.Nil(t, editDiagnostics(ctx, nil, ws, "main.go"), "no language servers")
	m := lsp.NewManager(ws.Dir(), []lsp.Server{{Language: "go", Command: []string{"fake"}, Extensions: []string{".go"}}}, lsptest.Launcher())
	t.Cleanup(m.Close)
	assert.Nil(t, editDiagnostics(ctx, m, ws, ""), "no file")
	assert.Nil(t, editDiagnostics(ctx, m, ws, "main.go"), "the server isn't running yet")
}

// A server that can't be prepared (here, its credentials can't be
// copied) isn't started; one that ignores its closed stdin is killed.
func TestLSPLauncherFailures(t *testing.T) {
	src := filepath.Join(t.TempDir(), "adc.json")
	require.NoError(t, os.WriteFile(src, []byte(`{}`), 0o600))
	useCredentialsDir(t)
	failing := errors.New("no cache directory")
	credentialsDir = func() (string, error) { return "", failing }
	env := &ExecEnv{Credentials: []Credential{{Env: "GOOGLE_APPLICATION_CREDENTIALS", Path: func() string { return src }}}}
	_, err := lspLauncher(env)(context.Background(), []string{"cat"})
	assert.ErrorIs(t, err, failing)

	p, err := lspLauncher(&ExecEnv{Dir: t.TempDir()})(context.Background(), []string{"sh", "-c", "exec sleep 30 </dev/null"})
	require.NoError(t, err)
	start := time.Now()
	p.Stop()
	select {
	case <-p.(*lspProcess).done:
	case <-time.After(10 * time.Second):
		t.Fatal("the server outlived Stop")
	}
	assert.Less(t, time.Since(start), 10*time.Second)
}
