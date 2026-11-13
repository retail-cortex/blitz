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
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/ostrash"
)

// The user's own file operations (the desktop app's files, spec_029_files):
// confined to the workspace directory through os.Root, like the agent's,
// but not bound by the agent's blocked paths or read-only roots (they're
// the user's files) and not checkpointed as the agent's turns are. Paths
// are relative to the workspace, with either separator.

// ErrGitDir means the path is in the repository's .git directory, which is
// never read or written.
var ErrGitDir = errors.New("the .git directory can't be opened here")

// UserPath cleans a workspace-relative path from the user ("" is the
// workspace itself).
func UserPath(p string) (string, error) {
	if p == "" {
		return ".", nil
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%w: %q is not relative to the workspace", ErrOutsideWorkspace, p)
	}
	c := filepath.Clean(filepath.FromSlash(p))
	if escapesRoot(c) {
		return "", fmt.Errorf("%w: %q", ErrOutsideWorkspace, p)
	}
	if c == ".git" || strings.HasPrefix(c, ".git"+string(filepath.Separator)) {
		return "", ErrGitDir
	}
	return c, nil
}

func (w *Workspace) userRoot() *os.Root { return w.roots[0].root }

// AgentRule is the agent's rule for the workspace-relative path rel:
// "blocked", "read_only", or "" when the agent may read and write it.
func (w *Workspace) AgentRule(rel string) string {
	loc, ok := w.locate(filepath.Join(w.Dir(), rel))
	if !ok {
		return "blocked"
	}
	err := w.check(loc, true)
	switch {
	case errors.Is(err, ErrBlockedPath):
		return "blocked"
	case errors.Is(err, ErrReadOnlyPath):
		return "read_only"
	}
	return ""
}

// UserReadDir lists the directory rel.
func (w *Workspace) UserReadDir(rel string) ([]fs.DirEntry, error) {
	return fs.ReadDir(w.userRoot().FS(), filepath.ToSlash(rel))
}

// UserStat stats rel (following a symlink, which must stay in the workspace).
func (w *Workspace) UserStat(rel string) (fs.FileInfo, error) {
	return w.userRoot().Stat(rel)
}

// UserReadFile reads the regular file rel if it's at most limit bytes;
// larger, it returns the file's information and ErrTooLarge.
func (w *Workspace) UserReadFile(rel string, limit int64) ([]byte, fs.FileInfo, error) {
	f, err := w.userRoot().Open(rel)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return nil, info, fmt.Errorf("%s is a folder", filepath.ToSlash(rel))
	}
	if info.Size() > limit {
		return nil, info, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, info, err
	}
	if int64(len(data)) > limit {
		return nil, info, ErrTooLarge
	}
	return data, info, nil
}

// ErrTooLarge means the file is over the size the caller reads.
var ErrTooLarge = errors.New("file too large")

// UserWriteFile replaces rel with data atomically (its mode kept), or
// creates it, under the same per-path lock as the agent's edits. check, if
// set, runs under the lock with the file's current content (nil, false
// when it doesn't exist) and can refuse the write.
func (w *Workspace) UserWriteFile(ctx context.Context, rel string, data []byte, check func(current []byte, exists bool) error) error {
	if rel == "." {
		return errors.New("path must name a file")
	}
	unlock, err := w.lockPaths(ctx, rel)
	if err != nil {
		return err
	}
	defer unlock()
	if check != nil {
		current, err := w.userRoot().ReadFile(rel)
		exists := err == nil
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := check(current, exists); err != nil {
			return err
		}
	}
	return w.writeAtomic(location{root: w.roots[0], rel: rel}, data, nil)
}

// UserMkdir creates the folder rel (and missing parents); it must not exist.
func (w *Workspace) UserMkdir(rel string) error {
	if rel == "." {
		return fs.ErrExist
	}
	if _, err := w.userRoot().Lstat(rel); err == nil {
		return fmt.Errorf("%s: %w", filepath.ToSlash(rel), fs.ErrExist)
	}
	return w.userRoot().MkdirAll(rel, 0o755)
}

// UserRename moves from to to, which must not exist.
func (w *Workspace) UserRename(ctx context.Context, from, to string) error {
	if from == "." || to == "." {
		return errors.New("can't rename the workspace")
	}
	unlock, err := w.lockPaths(ctx, from, to)
	if err != nil {
		return err
	}
	defer unlock()
	r := w.userRoot()
	if _, err := r.Lstat(from); err != nil {
		return err
	}
	if _, err := r.Lstat(to); err == nil {
		return fmt.Errorf("%s: %w", filepath.ToSlash(to), fs.ErrExist)
	}
	if err := mkdirParent(r, to); err != nil {
		return err
	}
	return r.Rename(from, to)
}

// UserCopy copies from (a file, or a folder with everything in it) to to,
// which must not exist; a symbolic link is copied as a link. Folders are
// made as the copy goes, and a copy that fails partway is taken back.
func (w *Workspace) UserCopy(ctx context.Context, from, to string) error {
	if from == "." || to == "." {
		return errors.New("can't copy the workspace")
	}
	if to == from || strings.HasPrefix(to, from+string(filepath.Separator)) {
		return fmt.Errorf("%s: can't copy a folder into itself", filepath.ToSlash(to))
	}
	unlock, err := w.lockPaths(ctx, from, to)
	if err != nil {
		return err
	}
	defer unlock()
	r := w.userRoot()
	if _, err := r.Lstat(from); err != nil {
		return err
	}
	if _, err := r.Lstat(to); err == nil {
		return fmt.Errorf("%s: %w", filepath.ToSlash(to), fs.ErrExist)
	}
	if err := mkdirParent(r, to); err != nil {
		return err
	}
	err = fs.WalkDir(r.FS(), filepath.ToSlash(from), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		dst := filepath.Join(to, strings.TrimPrefix(filepath.FromSlash(p), from))
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return r.Mkdir(dst, info.Mode().Perm()|0o700)
		case d.Type()&fs.ModeSymlink != 0:
			target, err := r.Readlink(filepath.FromSlash(p))
			if err != nil {
				return err
			}
			return r.Symlink(target, dst)
		case !d.Type().IsRegular():
			return nil // devices, sockets and pipes aren't copied
		}
		in, err := r.Open(filepath.FromSlash(p))
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := r.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
	if err != nil {
		_ = r.RemoveAll(to)
	}
	return err
}

// UserCreateUpload starts a file the user uploads to rel: an empty hidden
// file beside it (its folders made), which UserAppend fills and
// UserFinishUpload puts in place. It returns the hidden file's path.
func (w *Workspace) UserCreateUpload(rel string) (string, error) {
	if rel == "." {
		return "", errors.New("path must name a file")
	}
	r := w.userRoot()
	if err := mkdirParent(r, rel); err != nil {
		return "", err
	}
	tmp, err := tempName(rel)
	if err != nil {
		return "", err
	}
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	return tmp, f.Close()
}

// UserAppend adds data to the end of the upload tmp.
func (w *Workspace) UserAppend(tmp string, data []byte) error {
	f, err := w.userRoot().OpenFile(tmp, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// UserFinishUpload puts the upload tmp at rel, under rel's lock. rel must
// not exist unless replace, and a folder is never replaced.
func (w *Workspace) UserFinishUpload(ctx context.Context, tmp, rel string, replace bool) error {
	unlock, err := w.lockPaths(ctx, rel)
	if err != nil {
		return err
	}
	defer unlock()
	r := w.userRoot()
	if info, err := r.Lstat(rel); err == nil {
		if !replace {
			return fmt.Errorf("%s: %w", filepath.ToSlash(rel), fs.ErrExist)
		}
		if info.IsDir() {
			return fmt.Errorf("%s is a folder", filepath.ToSlash(rel))
		}
	}
	f, err := r.OpenFile(tmp, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return r.Rename(tmp, rel)
}

// UserDiscardUpload removes the unfinished upload tmp.
func (w *Workspace) UserDiscardUpload(tmp string) {
	_ = w.userRoot().Remove(tmp)
}

// UserTrash moves rel (a file or a folder) to the system's trash
// (FIL-37a) and returns where it is there, for UserRestoreTrashed;
// ostrash.ErrUnavailable when there's none. The folders above it must be
// the workspace's own, not links out of it.
func (w *Workspace) UserTrash(ctx context.Context, rel string) (string, error) {
	if rel == "." {
		return "", errors.New("can't delete the workspace")
	}
	unlock, err := w.lockPaths(ctx, rel)
	if err != nil {
		return "", err
	}
	defer unlock()
	if _, err := w.userRoot().Lstat(rel); err != nil {
		return "", err
	}
	abs, err := w.userAbs(rel)
	if err != nil {
		return "", err
	}
	return ostrash.Trash(ctx, abs)
}

// UserRestoreTrashed puts what UserTrash moved to where back at rel,
// which must not exist.
func (w *Workspace) UserRestoreTrashed(ctx context.Context, where, rel string) error {
	if rel == "." {
		return errors.New("can't restore over the workspace")
	}
	unlock, err := w.lockPaths(ctx, rel)
	if err != nil {
		return err
	}
	defer unlock()
	r := w.userRoot()
	if _, err := r.Lstat(rel); err == nil {
		return fmt.Errorf("%s: %w", filepath.ToSlash(rel), fs.ErrExist)
	}
	if err := mkdirParent(r, rel); err != nil {
		return err
	}
	abs, err := w.userAbs(rel)
	if err != nil {
		return err
	}
	return ostrash.Restore(where, abs)
}

// userAbs is rel's absolute path, its folder checked to be the
// workspace's: one that's a link out of it is refused, as os.Root would.
func (w *Workspace) userAbs(rel string) (string, error) {
	root := w.roots[0].dir
	parent, err := filepath.EvalSymlinks(filepath.Join(root, filepath.Dir(rel)))
	if err != nil {
		return "", err
	}
	if parent != root && !strings.HasPrefix(parent, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%s: outside the workspace", filepath.ToSlash(rel))
	}
	return filepath.Join(parent, filepath.Base(rel)), nil
}

// UserRemove deletes rel, a folder with everything in it.
func (w *Workspace) UserRemove(ctx context.Context, rel string) error {
	if rel == "." {
		return errors.New("can't delete the workspace")
	}
	unlock, err := w.lockPaths(ctx, rel)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := w.userRoot().Lstat(rel); err != nil {
		return err
	}
	return w.userRoot().RemoveAll(rel)
}
