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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// walkBlocked is the scan as it was before blockedScan kept anything: a
// filepath.WalkDir of every root before every command. blockedScan must
// find exactly what it finds.
func walkBlocked(spec OSSandboxSpec) (files, dirs []string) {
	m := spec.Blocked
	seen := map[string]bool{} // a root inside another is walked twice
	count := 0
	for _, root := range append(append([]string(nil), spec.WritableDirs...), spec.ReadOnlyDirs...) {
		base := strings.Count(root, string(filepath.Separator))
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			count++
			if count > blockedScanMaxEntries {
				return fs.SkipAll
			}
			if d.IsDir() && path != root && (blockedScanSkip[d.Name()] || strings.Count(path, string(filepath.Separator))-base > blockedScanMaxDepth) {
				return fs.SkipDir
			}
			if _, blocked := m.Match(path); blocked && path != root {
				if !seen[path] && d.IsDir() {
					dirs = append(dirs, path)
				} else if !seen[path] {
					files = append(files, path)
				}
				seen[path] = true
				if d.IsDir() {
					return fs.SkipDir
				}
			}
			return nil
		})
	}
	sort.Strings(files)
	sort.Strings(dirs)
	return files, dirs
}

// age sets every directory under root to an hour ago, so a scan trusts
// what it reads there (nothing changed just before).
func age(t testing.TB, root string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			return os.Chtimes(path, old, old)
		}
		return err
	}))
}

func scanFixture(t *testing.T) (ws string, spec OSSandboxSpec) {
	t.Helper()
	ws = t.TempDir()
	deep := ws
	for i := range blockedScanMaxDepth + 2 {
		deep = filepath.Join(deep, fmt.Sprintf("d%d", i))
		writeFile(t, filepath.Join(deep, ".env"), "x") // the deepest are past the limit
	}
	writeFile(t, filepath.Join(ws, ".env"), "x")
	writeFile(t, filepath.Join(ws, "sub", "server.pem"), "x")
	writeFile(t, filepath.Join(ws, "sub", "ok.txt"), "x")
	writeFile(t, filepath.Join(ws, "secrets", "inside.txt"), "x") // a blocked directory
	writeFile(t, filepath.Join(ws, "node_modules", "pkg", ".env"), "x")
	writeFile(t, filepath.Join(ws, "other", "readme.md"), "x")
	require.NoError(t, os.Symlink(filepath.Join(ws, "sub"), filepath.Join(ws, "link")))
	ro := t.TempDir()
	writeFile(t, filepath.Join(ro, "lib", ".env"), "x")

	m, err := NewPathMatcher([]string{".env", "*.pem", "secrets"}, []string{ws})
	require.NoError(t, err)
	return ws, OSSandboxSpec{WritableDirs: []string{ws, filepath.Join(ws, "missing")}, ReadOnlyDirs: []string{ro}, Blocked: m}
}

// A scan that keeps what it read masks what a full walk would, after
// every kind of change to the tree.
func TestBlockedScanSeesChanges(t *testing.T) {
	ws, spec := scanFixture(t)
	scan := newBlockedScan(spec)
	steps := []struct {
		name   string
		change func(t *testing.T)
		masked []string // must be masked after the change
		gone   []string // must not be
	}{
		{name: "as it is", masked: []string{filepath.Join(ws, ".env"), filepath.Join(ws, "sub", "server.pem"), filepath.Join(ws, "secrets")}},
		{
			name:   "a blocked file added to a known directory",
			change: func(t *testing.T) { writeFile(t, filepath.Join(ws, "sub", ".env"), "x") },
			masked: []string{filepath.Join(ws, "sub", ".env")},
		},
		{
			name:   "a new directory holding a blocked file",
			change: func(t *testing.T) { writeFile(t, filepath.Join(ws, "new", "deeper", "key.pem"), "x") },
			masked: []string{filepath.Join(ws, "new", "deeper", "key.pem")},
		},
		{
			name:   "a blocked file removed",
			change: func(t *testing.T) { require.NoError(t, os.Remove(filepath.Join(ws, ".env"))) },
			gone:   []string{filepath.Join(ws, ".env")},
		},
		{
			name:   "a directory renamed",
			change: func(t *testing.T) { require.NoError(t, os.Rename(filepath.Join(ws, "sub"), filepath.Join(ws, "sub2"))) },
			masked: []string{filepath.Join(ws, "sub2", "server.pem")},
			gone:   []string{filepath.Join(ws, "sub", "server.pem")},
		},
		{
			name: "a directory replaced by another with the same time",
			change: func(t *testing.T) {
				dir := filepath.Join(ws, "other")
				info, err := os.Stat(dir)
				require.NoError(t, err)
				writeFile(t, filepath.Join(ws, "other.new", "id.pem"), "x")
				require.NoError(t, os.Chtimes(filepath.Join(ws, "other.new"), info.ModTime(), info.ModTime()))
				require.NoError(t, os.RemoveAll(dir))
				require.NoError(t, os.Rename(filepath.Join(ws, "other.new"), dir))
			},
			masked: []string{filepath.Join(ws, "other", "id.pem")},
		},
		{
			name:   "a missing root created",
			change: func(t *testing.T) { writeFile(t, filepath.Join(ws, "missing", ".env"), "x") },
			masked: []string{filepath.Join(ws, "missing", ".env")},
		},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			age(t, ws)
			scan.expand() // read with nothing recent: kept
			if step.change != nil {
				step.change(t)
			}
			files, dirs := scan.expand()
			wantFiles, wantDirs := walkBlocked(spec)
			assert.Equal(t, wantFiles, files, "files")
			assert.Equal(t, wantDirs, dirs, "directories")
			got := append(files, dirs...)
			for _, p := range step.masked {
				assert.Contains(t, got, p)
			}
			for _, p := range step.gone {
				assert.NotContains(t, got, p)
			}
		})
	}
}

// Only the directories that changed are read again.
func TestBlockedScanReadsOnlyChangedDirectories(t *testing.T) {
	ws, spec := scanFixture(t)
	scan := newBlockedScan(spec)
	var reads []string
	scan.readDir = func(p string) ([]fs.DirEntry, error) {
		reads = append(reads, p)
		return os.ReadDir(p)
	}
	age(t, ws)
	age(t, spec.ReadOnlyDirs[0])
	scan.expand()
	assert.NotEmpty(t, reads, "the first scan reads every directory")

	reads = nil
	scan.expand()
	assert.Empty(t, reads, "nothing changed, nothing read")

	writeFile(t, filepath.Join(ws, "sub", "new.txt"), "x")
	reads = nil
	scan.expand()
	assert.Equal(t, []string{filepath.Join(ws, "sub")}, reads, "only the changed directory is read")

	reads = nil
	scan.expand()
	assert.Equal(t, []string{filepath.Join(ws, "sub")}, reads, "a directory that just changed is read until it settles")
}

// A directory read too soon after it changed isn't trusted, even when its
// time doesn't change again.
func TestBlockedScanRereadsRacyDirectories(t *testing.T) {
	ws, spec := scanFixture(t)
	scan := newBlockedScan(spec)
	sub := filepath.Join(ws, "sub")
	stamp := time.Now().Add(-time.Minute)
	age(t, ws)
	require.NoError(t, os.Chtimes(sub, stamp, stamp))
	scan.now = func() time.Time { return stamp.Add(time.Second) } // read a second after the change
	scan.expand()
	// Changed again within the file system's time resolution: same time.
	writeFile(t, filepath.Join(sub, ".env"), "x")
	require.NoError(t, os.Chtimes(sub, stamp, stamp))
	scan.now = time.Now
	files, _ := scan.expand()
	assert.Contains(t, files, filepath.Join(sub, ".env"))
}

// BenchmarkBlockedScan is a sandboxed command's start-up cost on Linux in
// a 50,000-entry workspace: warm is what each command pays once the first
// has scanned (BL-SH-01: at most 20 ms), cold what a full walk costs.
func BenchmarkBlockedScan(b *testing.B) {
	ws := b.TempDir()
	for d := range 500 {
		dir := filepath.Join(ws, fmt.Sprintf("pkg%03d", d))
		require.NoError(b, os.MkdirAll(dir, 0o755))
		for f := range 99 {
			require.NoError(b, os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.go", f)), nil, 0o644))
		}
	}
	age(b, ws)
	m, err := NewPathMatcher([]string{".env", "*.pem", "*.key", "id_*"}, []string{ws})
	require.NoError(b, err)
	spec := OSSandboxSpec{WritableDirs: []string{ws}, Blocked: m}
	b.Run("cold", func(b *testing.B) {
		for b.Loop() {
			newBlockedScan(spec).expand()
		}
	})
	b.Run("warm", func(b *testing.B) {
		scan := newBlockedScan(spec)
		scan.expand()
		for b.Loop() {
			scan.expand()
		}
	})
}
