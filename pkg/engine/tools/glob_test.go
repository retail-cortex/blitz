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
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func globFiles(t *testing.T, ws *Workspace, in GlobInput) GlobOutput {
	t.Helper()
	out, err := globWorkspace(context.Background(), ws, in)
	require.NoError(t, err, "glob %+v", in)
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
		assert.False(t, !reflect.DeepEqual(sorted(got.Files), sorted(c.want)) && !(len(got.Files) == 0 && len(c.want) == 0), "glob %+v = %v, want %v", c.in, got.Files, c.want)
		assert.Equal(t, len(got.Files), got.Count, "count %d for %v", got.Count, got.Files)
	}
}

func TestGlobNewestFirstAndCapped(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	now := time.Now()
	for i, name := range []string{"old.txt", "mid.txt", "new.txt"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, name)
			writeFile(t, p, "x")
			mt := now.Add(time.Duration(i-3) * time.Hour)
			require.NoError(t, os.Chtimes(p, mt, mt))
		})
	}
	out := globFiles(t, ws, GlobInput{Pattern: "*.txt"})
	want := []string{"new.txt", "mid.txt", "old.txt"}
	assert.Equal(t, want, out.Files, "order %v (truncated %v), want %v", out.Files, out.Truncated, want)
	assert.False(t, out.Truncated, "order %v (truncated %v), want %v", out.Files, out.Truncated, want)
	out = globFiles(t, ws, GlobInput{Pattern: "*.txt", MaxResults: 2})
	assert.Len(t, out.Files, 2, "capped: %+v", out)
	assert.True(t, out.Truncated, "capped: %+v", out)
	assert.Equal(t, "new.txt", out.Files[0], "capped: %+v", out)
}

func TestGlobRespectsTheSandbox(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.go"), "x")
	writeFile(t, filepath.Join(dir, "secrets", "key.pem"), "x")
	writeFile(t, filepath.Join(dir, ".env"), "x")
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: dir, BlockedPaths: []string{"*.pem", ".env"}})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	out := globFiles(t, ws, GlobInput{Pattern: "**/*"})
	assert.Equal(t, []string{"app.go"}, out.Files, "blocked files listed: %v", out.Files)
	out = globFiles(t, ws, GlobInput{Pattern: ".env"})
	assert.Len(t, out.Files, 0, "blocked .env matched by name: %v", out.Files)
	for _, bad := range []GlobInput{{Pattern: "../*"}, {Pattern: "/etc/*"}, {Pattern: "a/../../*"}, {Pattern: "*", Path: ".."}, {Pattern: ""}, {Pattern: "{a,b"}} {
		_, err := globWorkspace(context.Background(), ws, bad)
		assert.Error(t, err, "glob %+v should fail", bad)
	}
}

func TestExpandBraces(t *testing.T) {
	got, err := expandBraces("src/{a,b/{c,d}}/*.{go,ts}")
	require.NoError(t, err)
	want := []string{"src/a/*.go", "src/a/*.ts", "src/b/c/*.go", "src/b/c/*.ts", "src/b/d/*.go", "src/b/d/*.ts"}
	assert.Equal(t, sorted(want), sorted(got), "expandBraces = %v", got)
	_, err = expandBraces(strings.Repeat("{a,b}", 7))
	assert.Error(t, err, "expected a cap on alternatives")
}
