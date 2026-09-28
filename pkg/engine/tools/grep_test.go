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
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func grepMatches(t *testing.T, ws *Workspace, input GrepInput) []GrepMatch {
	t.Helper()
	m, err := grepWorkspace(context.Background(), ws, input)
	require.NoError(t, err, "grep %+v", input)
	return m
}

func TestGrepCaseInsensitiveLiteral(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "Hello World\nhello.world\nhelloXworld\n")

	// Positive (regression): case_insensitive + literal used to escape "(?i)".
	m := grepMatches(t, ws, GrepInput{Query: "HELLO", CaseInsensitive: true})
	assert.Len(t, m, 3, "expected 3 case-insensitive matches, got %d: %v", len(m), m)
	// Literal metacharacters stay literal: "hello.world" must not match "helloXworld".
	m = grepMatches(t, ws, GrepInput{Query: "HELLO.WORLD", CaseInsensitive: true})
	assert.Len(t, m, 1, "expected only the literal dotted line, got %v", m)
	assert.Equal(t, 2, m[0].LineNumber, "expected only the literal dotted line, got %v", m)
	// Negative: case-sensitive literal doesn't match other cases.
	m = grepMatches(t, ws, GrepInput{Query: "HELLO"})
	assert.Len(t, m, 0, "expected no case-sensitive matches, got %v", m)
}

func TestGrepRegex(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "a.go"), "func Foo() {}\nvar x = 1\nfunc bar() {}\n")

	m := grepMatches(t, ws, GrepInput{Query: `^func [A-Z]`, IsRegex: true})
	assert.Len(t, m, 1, "expected anchored regex to match line 1 only, got %v", m)
	assert.Equal(t, 1, m[0].LineNumber, "expected anchored regex to match line 1 only, got %v", m)
	m = grepMatches(t, ws, GrepInput{Query: `^FUNC`, IsRegex: true, CaseInsensitive: true})
	assert.Len(t, m, 2, "expected 2 case-insensitive regex matches, got %v", m)
	// Negative: invalid regex and empty query.
	_, err := grepWorkspace(context.Background(), ws, GrepInput{Query: "(", IsRegex: true})
	assert.Error(t, err, "expected invalid regex error, got")
	assert.Contains(t, err.Error(), "invalid regex", "expected invalid regex error, got %v", err)
	_, err = grepWorkspace(context.Background(), ws, GrepInput{})
	assert.Error(t, err, "expected error for empty query")
}

func TestGrepLongLinesAndBinaries(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	// A 200KB line exceeds bufio.Scanner's default 64KB token and used to
	// silently abort scanning that file.
	long := strings.Repeat("a", 200*1024) + "NEEDLE"
	writeFile(t, filepath.Join(dir, "min.js"), long+"\nNEEDLE on line two\n")
	writeFile(t, filepath.Join(dir, "blob.bin"), "NEEDLE\x00\x01\x02")

	m := grepMatches(t, ws, GrepInput{Query: "NEEDLE"})
	require.Len(t, m, 2, "expected 2 matches from min.js, got %v", len(m))
	for _, match := range m {
		assert.NotEqual(t, "blob.bin", match.File, "binary file should be skipped")
		assert.LessOrEqual(t, len(match.Content), grepMaxLineContent, "match content not truncated: %d bytes", len(match.Content))
	}
	assert.Equal(t, 1, m[0].LineNumber, "unexpected line numbers %v", m)
	assert.Equal(t, 2, m[1].LineNumber, "unexpected line numbers %v", m)
}

func TestGrepDefaultWorkspaceDot(t *testing.T) {
	// Regression: with workspace ".", the walk root is named "." and the
	// hidden-directory rule used to skip the entire tree.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.txt"), "findme\n")
	t.Chdir(dir)
	ws, err := NewWorkspace(".", 0)
	require.NoError(t, err)
	defer ws.Close()
	m := grepMatches(t, ws, GrepInput{Query: "findme"})
	assert.Len(t, m, 1, "expected 1 match with workspace '.', got %v", m)
}

func TestGrepSkipsVendoredAndHiddenDirs(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	for _, d := range []string{".git", "node_modules", "vendor", "target", "__pycache__"} {
		writeFile(t, filepath.Join(dir, d, "f.txt"), "token\n")
	}
	writeFile(t, filepath.Join(dir, "src", "f.txt"), "token\n")
	m := grepMatches(t, ws, GrepInput{Query: "token"})
	assert.Len(t, m, 1, "expected only src/f.txt, got %v", m)
	assert.Equal(t, "src/f.txt", m[0].File, "expected only src/f.txt, got %v", m)
	// Positive: explicitly searching inside a skipped dir still works.
	m = grepMatches(t, ws, GrepInput{Query: "token", Path: "vendor"})
	assert.Len(t, m, 1, "expected explicit path search to work, got %v", m)
	// Single-file path.
	m = grepMatches(t, ws, GrepInput{Query: "token", Path: "src/f.txt"})
	assert.Len(t, m, 1, "expected single-file search to work, got %v", m)
}

func TestGrepMaxMatchesDeterministic(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), "hit\nhit\nhit\n")
	}
	first := grepMatches(t, ws, GrepInput{Query: "hit", MaxMatches: 10})
	require.Len(t, first, 10, "expected 10 matches, got %d", len(first))
	assert.Equal(t, "f00.txt", first[0].File, "expected matches in walk order, got first=%s last=%s", first[0].File, first[9].File)
	assert.Equal(t, "f03.txt", first[9].File, "expected matches in walk order, got first=%s last=%s", first[0].File, first[9].File)
	for i := 0; i < 5; i++ {
		again := grepMatches(t, ws, GrepInput{Query: "hit", MaxMatches: 10})
		require.Equal(t, again, first, "parallel grep returned non-deterministic results")
	}
}

func TestGrepOutsideWorkspaceAndCancellation(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "x\n")
	_, err := grepWorkspace(context.Background(), ws, GrepInput{Query: "root", Path: "/etc"})
	assert.Error(t, err, "expected error searching outside workspace")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = grepWorkspace(ctx, ws, GrepInput{Query: "x"})
	assert.Error(t, err, "expected error for cancelled context")
}

func TestGrepTool(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "alpha\n")
	rt := toolOf(t)(NewGrepTool(ws))
	out := runTool(t, rt, map[string]any{"query": "ALPHA", "case_insensitive": true})
	assert.Equal(t, float64(1), out["total_matches"].(float64), "expected 1 match, got %v", out)
	out = runTool(t, rt, map[string]any{"query": "(", "is_regex": true})
	assert.NotEqual(t, "", errOf(out), "expected tool-level error for invalid regex")
}
