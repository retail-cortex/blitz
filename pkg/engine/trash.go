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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/ostrash"
)

// Deleting from the tree (spec_029_files FIL-37, FIL-37a): a file or
// folder goes to the system's trash, where the person sees, restores and
// empties it as any other (tools.trash = "system", the default, where the
// system has one); else to the workspace's own, under .blitz/trash, so its
// deletion can be undone for a while, then it's gone. Either way the
// snackbar's Undo puts it back.
const (
	trashDir = ".blitz/trash"
	// trashKeep is how long a deleted file can be restored.
	trashKeep = 24 * time.Hour
)

// ErrNotInTrash means a restore names nothing the trash holds (it was
// emptied, or never there).
var ErrNotInTrash = errors.New("it's no longer in the workspace's trash")

var trashIDs = regexp.MustCompile(`^[0-9]{14}-[0-9a-f]{8}$`)

// systemTrashIDs are the IDs of what went to the system's trash; where
// each went is kept in memory (Workspace.trashed), for Undo while the
// service runs, after which the system's trash restores it.
var systemTrashIDs = regexp.MustCompile(`^sys-[0-9a-f]{16}$`)

// systemTrashed is where a deletion went in the system's trash, by ID,
// and the deletions (either trash's) that git's index had (FIL-61).
type systemTrashed struct {
	mu     sync.Mutex
	where  map[string]string
	staged map[string]bool
}

// unstageTrashed takes slash, just trashed under id, out of git's index,
// remembering it was there for RestoreFile.
func (w *Workspace) unstageTrashed(ctx context.Context, id, slash string) {
	if len(w.unstageGone(ctx, slash)) == 0 {
		return
	}
	w.trashed.mu.Lock()
	if w.trashed.staged == nil {
		w.trashed.staged = map[string]bool{}
	}
	w.trashed.staged[id] = true
	w.trashed.mu.Unlock()
}

// restageRestored puts slash, restored from id, back in git's index when
// it was there.
func (w *Workspace) restageRestored(ctx context.Context, id, slash string) {
	w.trashed.mu.Lock()
	was := w.trashed.staged[id]
	delete(w.trashed.staged, id)
	w.trashed.mu.Unlock()
	if was {
		w.restage(ctx, slash)
	}
}

// TrashFile moves p (a file or a folder) to the workspace's trash and
// returns the ID that restores it (RestoreFile). Items older than a day
// are emptied first.
func (w *Workspace) TrashFile(ctx context.Context, p string) (string, error) {
	rel, slash, err := userPath(p)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "", errors.New("can't delete the workspace")
	}
	if slash == trashDir || strings.HasPrefix(slash, trashDir+"/") {
		return "", w.DeleteFile(ctx, p) // the trash itself goes for good
	}
	if w.files().UsesSystemTrash() {
		where, err := w.files().UserTrash(ctx, rel)
		if err == nil {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id := "sys-" + hex.EncodeToString(b)
			w.trashed.mu.Lock()
			if w.trashed.where == nil {
				w.trashed.where = map[string]string{}
			}
			w.trashed.where[id] = where
			w.trashed.mu.Unlock()
			w.unstageTrashed(ctx, id, slash)
			w.userEdited(slash, "deleted")
			return id, nil
		}
		if !errors.Is(err, ostrash.ErrUnavailable) {
			return "", err
		}
		// A file the system's trash can't take (another disk): the workspace's.
	}
	w.emptyTrash(ctx, time.Now())
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	id := time.Now().UTC().Format("20060102150405") + "-" + hex.EncodeToString(b)
	if err := w.files().UserMkdir(filepath.FromSlash(path.Join(trashDir, id))); err != nil {
		return "", err
	}
	// The trash stays out of git and the workspace's searches.
	_ = os.WriteFile(filepath.Join(w.Dir(), filepath.FromSlash(trashDir), ".gitignore"), []byte("*\n"), 0o644)
	if err := w.files().UserRename(ctx, rel, filepath.FromSlash(path.Join(trashDir, id, path.Base(slash)))); err != nil {
		_ = w.files().UserRemove(ctx, filepath.FromSlash(path.Join(trashDir, id)))
		return "", err
	}
	w.unstageTrashed(ctx, id, slash)
	w.userEdited(slash, "deleted")
	return id, nil
}

// RestoreFile puts back what TrashFile moved to the trash under id, at p
// (where it was); p must not exist.
func (w *Workspace) RestoreFile(ctx context.Context, id, p string) error {
	if !trashIDs.MatchString(id) && !systemTrashIDs.MatchString(id) {
		return ErrNotInTrash
	}
	rel, slash, err := userPath(p)
	if err != nil {
		return err
	}
	if rel == "." {
		return ErrBadPath
	}
	if systemTrashIDs.MatchString(id) {
		w.trashed.mu.Lock()
		where, ok := w.trashed.where[id]
		w.trashed.mu.Unlock()
		if !ok {
			return ErrNotInTrash
		}
		if _, err := os.Lstat(where); err != nil {
			return ErrNotInTrash // emptied, or put back from the trash already
		}
		if err := w.files().UserRestoreTrashed(ctx, where, rel); err != nil {
			return err
		}
		w.trashed.mu.Lock()
		delete(w.trashed.where, id)
		w.trashed.mu.Unlock()
		w.restageRestored(ctx, id, slash)
		w.userEdited(slash, "restored")
		return nil
	}
	held := filepath.FromSlash(path.Join(trashDir, id, path.Base(slash)))
	if _, err := w.files().UserStat(held); errors.Is(err, fs.ErrNotExist) {
		return ErrNotInTrash
	}
	if err := w.files().UserRename(ctx, held, rel); err != nil {
		return err
	}
	_ = w.files().UserRemove(ctx, filepath.FromSlash(path.Join(trashDir, id)))
	w.restageRestored(ctx, id, slash)
	w.userEdited(slash, "restored")
	return nil
}

// emptyTrash removes what was deleted longer than trashKeep before now.
func (w *Workspace) emptyTrash(ctx context.Context, now time.Time) {
	entries, err := w.files().UserReadDir(filepath.FromSlash(trashDir))
	if err != nil {
		return
	}
	for _, e := range entries {
		if !trashIDs.MatchString(e.Name()) {
			continue
		}
		at, err := time.Parse("20060102150405", e.Name()[:14])
		if err == nil && now.Sub(at) > trashKeep {
			if err := w.files().UserRemove(ctx, filepath.FromSlash(path.Join(trashDir, e.Name()))); err != nil {
				slog.Warn("emptying the trash", "item", e.Name(), "error", err)
			}
		}
	}
}
