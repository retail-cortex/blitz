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
	"time"
)

// The workspace's trash (spec_029_files FIL-37): a file or folder deleted
// from the tree is moved under .blitz/trash, so its deletion can be undone
// for a while, then it's gone.
const (
	trashDir = ".blitz/trash"
	// trashKeep is how long a deleted file can be restored.
	trashKeep = 24 * time.Hour
)

// ErrNotInTrash means a restore names nothing the trash holds (it was
// emptied, or never there).
var ErrNotInTrash = errors.New("it's no longer in the workspace's trash")

var trashIDs = regexp.MustCompile(`^[0-9]{14}-[0-9a-f]{8}$`)

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
	w.userEdited(slash, "deleted")
	return id, nil
}

// RestoreFile puts back what TrashFile moved to the trash under id, at p
// (where it was); p must not exist.
func (w *Workspace) RestoreFile(ctx context.Context, id, p string) error {
	if !trashIDs.MatchString(id) {
		return ErrNotInTrash
	}
	rel, slash, err := userPath(p)
	if err != nil {
		return err
	}
	if rel == "." {
		return ErrBadPath
	}
	held := filepath.FromSlash(path.Join(trashDir, id, path.Base(slash)))
	if _, err := w.files().UserStat(held); errors.Is(err, fs.ErrNotExist) {
		return ErrNotInTrash
	}
	if err := w.files().UserRename(ctx, held, rel); err != nil {
		return err
	}
	_ = w.files().UserRemove(ctx, filepath.FromSlash(path.Join(trashDir, id)))
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
