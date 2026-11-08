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
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// watching runs watchFolders on root in the background, collecting what
// it sends; stop ends it and returns its error.
type watching struct {
	mu     sync.Mutex
	sent   []FolderChanges
	done   chan error
	cancel context.CancelFunc
}

func startWatch(t *testing.T, root string, folders ...string) *watching {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	w := &watching{done: make(chan error, 1), cancel: cancel}
	go func() {
		w.done <- watchFolders(ctx, root, folders, func(c FolderChanges) error {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.sent = append(w.sent, c)
			return nil
		})
	}()
	t.Cleanup(func() { _ = w.stop() })
	require.Eventually(t, func() bool { return len(w.all()) > 0 }, 2*time.Second, 5*time.Millisecond, "the first message says the watch is on")
	return w
}

func (w *watching) all() []FolderChanges {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.sent)
}

// changed is every folder sent so far.
func (w *watching) changed() []string {
	var out []string
	for _, c := range w.all() {
		out = append(out, c.Folders...)
	}
	return out
}

func (w *watching) stop() error {
	w.cancel()
	select {
	case err := <-w.done:
		w.done <- err
		return err
	case <-time.After(2 * time.Second):
		return errors.New("the watch didn't stop")
	}
}

// The folders watched report changes to their entries, settled into one
// message; folders not asked for, .git and paths outside aren't watched
// (FIL-33b).
func TestWatchFolders(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a/b", ".git"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), nil, 0o644))
	w := startWatch(t, root, "", "a", "a", ".git", "../far", "/abs", "missing", "file")
	assert.Equal(t, []FolderChanges{{}}, w.all(), "on, nothing to poll")

	write := func(p string) { require.NoError(t, os.WriteFile(filepath.Join(root, p), []byte("x"), 0o644)) }
	write("top.txt")
	write("a/one.txt")
	write("a/two.txt")
	require.Eventually(t, func() bool {
		got := w.changed()
		return slices.Contains(got, "") && slices.Contains(got, "a")
	}, 2*time.Second, 10*time.Millisecond)
	for _, c := range w.all() {
		assert.NotContains(t, c.Folders, "a/b", "a folder not asked for isn't watched")
		assert.NotContains(t, c.Folders, ".git")
		assert.True(t, slices.IsSorted(c.Folders))
	}

	require.NoError(t, os.Remove(filepath.Join(root, "a", "one.txt")))
	n := len(w.all())
	require.Eventually(t, func() bool { return len(w.all()) > n }, 2*time.Second, 10*time.Millisecond, "a removal is a change")
	assert.Contains(t, w.all()[len(w.all())-1].Folders, "a")
	assert.NoError(t, w.stop(), "the watch ends with its context")
}

// More folders or entries than the bounds allow are left to polling, and
// said so once.
func TestWatchFoldersBounds(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "b"), 0o755))

	for _, tc := range []struct {
		name    string
		folders int
		entries int
		kqueue  bool
		polling bool
	}{
		{"within the bounds", 256, 4000, true, false},
		{"too many folders", 1, 4000, false, true},
		{"too many entries for kqueue", 256, 1, true, true},
		{"entries don't count for inotify", 256, 1, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mf, me, kq := watchMaxFolders, watchMaxEntries, kqueueWatches
			watchMaxFolders, watchMaxEntries, kqueueWatches = tc.folders, tc.entries, tc.kqueue
			defer func() { watchMaxFolders, watchMaxEntries, kqueueWatches = mf, me, kq }()
			watcher, err := fsnotify.NewWatcher()
			require.NoError(t, err)
			defer watcher.Close()
			watched, polling := addWatches(watcher, root, []string{"", "a", "b"})
			assert.Equal(t, tc.polling, polling)
			if !tc.polling {
				assert.Equal(t, []string{"", "a", "b"}, watched)
			}
		})
	}
}

// A send that fails ends the watch with its error.
func TestWatchFoldersSendFails(t *testing.T) {
	root := t.TempDir()
	refused := errors.New("the stream is gone")
	err := watchFolders(context.Background(), root, []string{""}, func(FolderChanges) error { return refused })
	assert.ErrorIs(t, err, refused)

	calls := 0
	done := make(chan error, 1)
	on := make(chan struct{})
	go func() {
		done <- watchFolders(context.Background(), root, []string{""}, func(FolderChanges) error {
			calls++
			if calls > 1 {
				return refused
			}
			close(on)
			return nil
		})
	}()
	<-on
	require.NoError(t, os.WriteFile(filepath.Join(root, "x"), nil, 0o644))
	select {
	case err := <-done:
		assert.ErrorIs(t, err, refused, "a change's send failing ends it")
	case <-time.After(2 * time.Second):
		t.Fatal("the watch went on after its send failed")
	}
}

func TestWatchablePaths(t *testing.T) {
	for in, want := range map[string]string{"": "", ".": "", "a": "a", "a/b/": "a/b", "./a": "a"} {
		got, ok := watchable(filepath.FromSlash(in))
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"..", "a/../..", "/etc", ".git", ".git/refs"} {
		_, ok := watchable(in)
		assert.False(t, ok, in)
	}
	root := t.TempDir()
	for name, want := range map[string]string{filepath.Join(root, "x"): "", filepath.Join(root, "a", "x"): "a"} {
		got, ok := changedFolder(root, name)
		assert.True(t, ok, name)
		assert.Equal(t, want, got)
	}
	_, ok := changedFolder(root, filepath.Join(filepath.Dir(root), "elsewhere", "x"))
	assert.False(t, ok, "outside the workspace")
	_, ok = changedFolder(root, filepath.Join(root, ".git", "HEAD"))
	assert.False(t, ok, ".git")
	assert.Zero(t, countEntries(filepath.Join(root, "missing")))
}
