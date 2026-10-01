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
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Limits on how much of the filesystem is scanned to find blocked paths
// before each sandboxed command.
const (
	blockedScanMaxEntries = 50_000
	blockedScanMaxDepth   = 12
)

var blockedScanSkip = map[string]bool{".git": true, "node_modules": true, "vendor": true, "target": true, "__pycache__": true}

// racyWindow: a directory whose modification time is this close to when
// it was read may change again without its time changing (file systems
// keep coarse times), so it is read again next time, as git treats "racy"
// index entries.
const racyWindow = 2 * time.Second

// expandBlocked resolves blocked-path patterns to concrete existing paths,
// since bubblewrap can only mask paths, not patterns. Absolute patterns are
// globbed directly; name patterns (".env", "*.pem") are found by scanning the
// writable and read-only roots.
func expandBlocked(spec OSSandboxSpec) (files, dirs []string) {
	return newBlockedScan(spec).expand()
}

// blockedScan is expandBlocked for one sandbox, run before each of its
// commands. It keeps what it read, so the next scan reads again only the
// directories that changed since (or changed just before they were read):
// one stat per directory walked instead of reading every directory and
// matching every entry. It walks as a fresh scan does, in the same order
// and within the same limits, so it masks the same paths.
type blockedScan struct {
	spec OSSandboxSpec

	mu   sync.Mutex
	dirs map[string]*scannedDir // by path
	// readDir and now read directories and tell the time; tests replace
	// them.
	readDir func(string) ([]fs.DirEntry, error)
	now     func() time.Time
}

// scannedDir is a directory as last read.
type scannedDir struct {
	key     dirKey
	racy    bool // read too soon after it changed to trust an unchanged key
	entries []scannedEntry
}

// scannedEntry is one of a directory's entries, matched once.
type scannedEntry struct {
	name    string
	isDir   bool
	blocked bool
}

// dirKey tells whether a directory's entries may have changed: adding,
// removing or renaming an entry changes its modification time, and a
// directory replaced by another has another inode.
type dirKey struct {
	mtime int64
	size  int64
	ino   uint64
}

func newBlockedScan(spec OSSandboxSpec) *blockedScan {
	return &blockedScan{spec: spec, dirs: map[string]*scannedDir{}, readDir: os.ReadDir, now: time.Now}
}

// expand is the blocked files and directories that exist now, sorted.
func (s *blockedScan) expand() (files, dirs []string) {
	files, dirs, _ = s.expandCtx(context.Background())
	return files, dirs
}

// expandCtx is expand, stopping with ctx's error when ctx ends: a cold
// scan reads up to blockedScanMaxEntries entries, which can take seconds,
// and a turn's time limit or cancellation shouldn't wait for it. What was
// read stays cached for the next scan.
func (s *blockedScan) expandCtx(ctx context.Context) (files, dirs []string, err error) {
	m := s.spec.Blocked
	if m == nil || len(m.rules) == 0 {
		return nil, nil, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := map[string]bool{}
	add := func(p string, isDir bool) {
		if seen[p] {
			return
		}
		seen[p] = true
		if isDir {
			dirs = append(dirs, p)
		} else {
			files = append(files, p)
		}
	}

	for _, pat := range m.Patterns() {
		p := filepath.ToSlash(expandHomePattern(pat))
		if !strings.HasPrefix(p, "/") || strings.Contains(p, "**") {
			continue
		}
		matches, _ := filepath.Glob(filepath.FromSlash(p))
		for _, match := range matches {
			if info, err := os.Lstat(match); err == nil {
				add(match, info.IsDir())
			}
		}
	}

	w := &blockedWalk{scan: s, add: add, visited: map[string]bool{}, ctx: ctx}
	for _, root := range append(append([]string(nil), s.spec.WritableDirs...), s.spec.ReadOnlyDirs...) {
		info, err := os.Lstat(root)
		if err != nil {
			continue
		}
		if w.count++; w.count > blockedScanMaxEntries {
			break
		}
		if info.IsDir() && w.dir(root, 0) {
			break
		}
	}
	if w.err != nil {
		return nil, nil, w.err // and nothing forgotten: the walk didn't finish
	}
	// Directories not walked this time (removed, or now past the limits)
	// are forgotten.
	for p := range s.dirs {
		if !w.visited[p] {
			delete(s.dirs, p)
		}
	}
	sort.Strings(files)
	sort.Strings(dirs)
	return files, dirs, nil
}

// blockedWalk is one walk of the roots.
type blockedWalk struct {
	scan    *blockedScan
	add     func(path string, isDir bool)
	visited map[string]bool
	count   int // entries walked, roots included
	ctx     context.Context
	err     error // ctx's, once it ended
}

// cancelCheck is how many entries the walk reads between checks of its
// context.
const cancelCheck = 256

// dir walks directory path, depth levels below its root, as
// filepath.WalkDir would: entries in name order, skipping the directories
// in blockedScanSkip and those deeper than blockedScanMaxDepth, and not
// descending into blocked ones. It reports whether the entry limit
// stopped the walk.
func (w *blockedWalk) dir(path string, depth int) bool {
	w.visited[path] = true
	for _, e := range w.scan.entries(path) {
		if w.count++; w.count > blockedScanMaxEntries {
			return true
		}
		if w.count%cancelCheck == 0 {
			if w.err = w.ctx.Err(); w.err != nil {
				return true
			}
		}
		switch {
		case e.isDir && (blockedScanSkip[e.name] || depth+1 > blockedScanMaxDepth):
		case e.blocked:
			w.add(filepath.Join(path, e.name), e.isDir)
		case e.isDir:
			if w.dir(filepath.Join(path, e.name), depth+1) {
				return true
			}
		}
	}
	return false
}

// entries are directory path's entries, as last read when it hasn't
// changed since, else read and matched again.
func (s *blockedScan) entries(path string) []scannedEntry {
	info, err := os.Lstat(path)
	if err != nil {
		delete(s.dirs, path)
		return nil
	}
	key := dirKey{mtime: info.ModTime().UnixNano(), size: info.Size(), ino: inode(info)}
	if d := s.dirs[path]; d != nil && !d.racy && d.key == key {
		return d.entries
	}
	read := s.now()
	list, err := s.readDir(path) // what it read before an error, as WalkDir walks
	d := &scannedDir{key: key, racy: err != nil || !info.ModTime().Before(read.Add(-racyWindow))}
	for _, e := range list {
		_, blocked := s.spec.Blocked.Match(filepath.Join(path, e.Name()))
		d.entries = append(d.entries, scannedEntry{name: e.Name(), isDir: e.IsDir(), blocked: blocked})
	}
	s.dirs[path] = d
	return d.entries
}
