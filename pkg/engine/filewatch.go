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

package engine

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FolderChanges is what WatchFolders reports (spec_029_files FIL-33b):
// the folders whose entries changed (an entry added, removed, renamed or
// written), to list again; or, with Polling, that some folders can't be
// watched (the system's limit), so the caller lists them on a timer.
type FolderChanges struct {
	Folders []string
	Polling bool
}

// Bounds on a watch: the most folders, and on systems whose watches hold
// a file open for each entry of a folder (kqueue: macOS and the BSDs) the
// most entries, so a watch never runs the service out of files. Variables
// so tests can lower them.
var (
	watchMaxFolders = 256
	watchMaxEntries = 4000
)

// watchSettle is how long a burst of changes settles before they're sent
// together (a build writing many files is one message).
const watchSettle = 150 * time.Millisecond

// kqueueWatches reports whether the system's watches hold a file open
// for each entry of a watched folder.
var kqueueWatches = goruntime.GOOS != "linux" && goruntime.GOOS != "windows"

// WatchFolders watches folders of the workspace (relative paths, "" for
// its root; the Files shelf's open folders) until ctx ends, sending which
// changed. The first send has no folders: the watch is on. Folders it
// can't watch are reported once as Polling; .git and paths outside the
// workspace are never watched.
func (w *Workspace) WatchFolders(ctx context.Context, folders []string, send func(FolderChanges) error) error {
	return watchFolders(ctx, w.Dir(), folders, send)
}

// watchFolders is WatchFolders for the workspace at root.
func watchFolders(ctx context.Context, root string, folders []string, send func(FolderChanges) error) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("watching the workspace's folders", "error", err)
		if err := send(FolderChanges{Polling: true}); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	}
	defer watcher.Close()

	watched, polling := addWatches(watcher, root, folders)
	if err := send(FolderChanges{Polling: polling}); err != nil {
		return err
	}

	pending := map[string]bool{}
	settle := time.NewTimer(time.Hour)
	settle.Stop()
	defer settle.Stop()
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		out := make([]string, 0, len(pending))
		for f := range pending {
			out = append(out, f)
		}
		slices.Sort(out)
		clear(pending)
		return send(FolderChanges{Folders: out})
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if ev.Op == fsnotify.Chmod {
				continue // a mode or time touched: nothing the tree shows
			}
			if f, ok := changedFolder(root, ev.Name); ok {
				pending[f] = true
				settle.Reset(watchSettle)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) { // changes were lost: list them all again
				for _, f := range watched {
					pending[f] = true
				}
				settle.Reset(watchSettle)
				continue
			}
			slog.Warn("watching the workspace's folders", "error", err)
		case <-settle.C:
			if err := flush(); err != nil {
				return err
			}
		}
	}
}

// addWatches watches each of folders under root that is a real folder
// (not a link, not .git), within the bounds, returning those watched and
// whether any were left to polling.
func addWatches(watcher *fsnotify.Watcher, root string, folders []string) (watched []string, polling bool) {
	entries := 0
	seen := map[string]bool{}
	for _, f := range folders {
		rel, ok := watchable(f)
		if !ok || seen[rel] {
			continue
		}
		seen[rel] = true
		abs := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(abs)
		if err != nil || !info.IsDir() {
			continue // gone, a file or a link: nothing to watch
		}
		if len(watched) == watchMaxFolders {
			polling = true
			continue
		}
		if kqueueWatches {
			n := countEntries(abs)
			if entries+n > watchMaxEntries {
				polling = true
				continue
			}
			entries += n
		}
		if err := watcher.Add(abs); err != nil {
			if errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) || errors.Is(err, syscall.ENOSPC) {
				polling = true // the system's limit (Linux's inotify watches)
			}
			continue
		}
		watched = append(watched, rel)
	}
	return watched, polling
}

// watchable is folder as a clean relative path ("" for the root) and
// whether it may be watched: inside the workspace, and not .git.
func watchable(folder string) (string, bool) {
	p := filepath.ToSlash(folder)
	if strings.HasPrefix(p, "/") || slices.Contains(strings.Split(p, "/"), "..") {
		return "", false
	}
	rel := strings.Trim(path.Clean("/"+p), "/")
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return "", false
	}
	return rel, true
}

// changedFolder is the workspace folder whose entries an event on name
// changed (its parent), and whether the tree shows it.
func changedFolder(root, name string) (string, bool) {
	rel, err := filepath.Rel(root, filepath.Dir(name))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		rel = ""
	}
	if _, ok := watchable(rel); !ok {
		return "", false
	}
	return rel, true
}

// countEntries is how many entries folder holds (0 when it can't be read).
func countEntries(folder string) int {
	f, err := os.Open(folder)
	if err != nil {
		return 0
	}
	defer f.Close()
	names, _ := f.Readdirnames(-1)
	return len(names)
}
