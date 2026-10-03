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

// Package worktree gives work its own git worktree, so it can change files
// without touching the checkout the user works in (spec_parity_027 §8.2):
// <repo>/.blitz/worktrees/<name> on the branch blitz/<name>, from HEAD or
// another ref, with the untracked files .worktreeinclude lists (a .env,
// say) copied in.
package worktree

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Dir is where worktrees go, under the repository's root.
const Dir = ".blitz/worktrees"

// BranchPrefix names their branches.
const BranchPrefix = "blitz/"

// IncludeFile lists, one pattern a line, untracked files to copy into a
// new worktree.
const IncludeFile = ".worktreeinclude"

// ErrNotRepo means the directory isn't in a git repository.
var ErrNotRepo = errors.New("not in a git repository")

// Worktree is one of Blitz's worktrees.
type Worktree struct {
	Name   string
	Path   string
	Branch string
	// Missing means git knows it, but its directory is gone (prune removes
	// it).
	Missing bool
}

// validName is what a worktree may be called: its branch's and folder's
// name.
var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Root is the root of the repository dir is in: the main checkout's,
// even from inside one of its worktrees.
func Root(dir string) (string, error) {
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotRepo, dir)
	}
	root := strings.TrimSpace(top)
	if common, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
		if c := strings.TrimSpace(common); filepath.Base(c) == ".git" {
			root = filepath.Dir(c)
		}
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	return root, nil
}

// NewName is a name for a worktree: prefix and the time, with a little
// randomness.
func NewName(prefix string) string {
	b := make([]byte, 2)
	rand.Read(b)
	return prefix + time.Now().Format("0102-1504") + "-" + hex.EncodeToString(b)
}

// Create makes the worktree name (a new name when "") in the repository
// dir is in, on a new branch blitz/<name> from ref (HEAD when ""), and
// copies in the files .worktreeinclude lists.
func Create(dir, name, ref string) (Worktree, error) {
	root, err := Root(dir)
	if err != nil {
		return Worktree{}, err
	}
	if name == "" {
		name = NewName("wt-")
	}
	if !validName.MatchString(name) {
		return Worktree{}, fmt.Errorf("%q can't name a worktree: letters, digits, '.', '_' and '-'", name)
	}
	if ref == "" {
		ref = "HEAD"
	}
	w := Worktree{Name: name, Path: filepath.Join(root, Dir, name), Branch: BranchPrefix + name}
	if _, err := os.Stat(w.Path); err == nil {
		return Worktree{}, fmt.Errorf("worktree %q exists already (%s)", name, w.Path)
	}
	// Before git adds it: git writes beside the exclude file, so one that
	// can't be written fails the worktree anyway.
	if err := excludeWorktrees(root); err != nil {
		return Worktree{}, fmt.Errorf("keeping %s out of git status: %w", Dir, err)
	}
	if _, err := git(root, "worktree", "add", "-b", w.Branch, w.Path, ref); err != nil {
		return Worktree{}, err
	}
	if err := copyIncluded(root, w.Path); err != nil {
		return w, fmt.Errorf("worktree %q made, but copying %s's files: %w", name, IncludeFile, err)
	}
	return w, nil
}

// List is the repository's Blitz worktrees (those under .blitz/worktrees).
func List(dir string) ([]Worktree, error) {
	root, err := Root(dir)
	if err != nil {
		return nil, err
	}
	out, err := git(root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	base := filepath.Join(root, Dir) + string(filepath.Separator)
	var list []Worktree
	var cur *Worktree
	flush := func() {
		if cur != nil && strings.HasPrefix(cur.Path+string(filepath.Separator), base) && cur.Path != strings.TrimSuffix(base, string(filepath.Separator)) {
			cur.Name = filepath.Base(cur.Path)
			if _, err := os.Stat(cur.Path); err != nil {
				cur.Missing = true
			}
			list = append(list, *cur)
		}
		cur = nil
	}
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			p := strings.TrimPrefix(line, "worktree ")
			if real, err := filepath.EvalSymlinks(p); err == nil {
				p = real
			}
			cur = &Worktree{Path: p}
		case strings.HasPrefix(line, "branch ") && cur != nil:
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return list, nil
}

// Remove removes worktree name; force removes it with changes not
// committed. Its branch stays, to merge or delete.
func Remove(dir, name string, force bool) (Worktree, error) {
	list, err := List(dir)
	if err != nil {
		return Worktree{}, err
	}
	for _, w := range list {
		if w.Name != name {
			continue
		}
		args := []string{"worktree", "remove", w.Path}
		if force {
			args = []string{"worktree", "remove", "--force", w.Path}
		}
		root, _ := Root(dir)
		if _, err := git(root, args...); err != nil {
			return w, err
		}
		return w, nil
	}
	return Worktree{}, fmt.Errorf("no worktree %q", name)
}

// Prune forgets worktrees whose folders are gone.
func Prune(dir string) error {
	root, err := Root(dir)
	if err != nil {
		return err
	}
	_, err = git(root, "worktree", "prune")
	return err
}

// Dirty reports whether w has changes to tracked files not committed
// (untracked files, such as those copied in, don't count).
func Dirty(w Worktree) bool {
	out, err := git(w.Path, "status", "--porcelain", "--untracked-files=no")
	return err == nil && strings.TrimSpace(out) != ""
}

// excludeWorktrees keeps .blitz/worktrees out of the repository's status.
func excludeWorktrees(root string) error {
	out, err := git(root, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	path := strings.TrimSpace(out)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	line := "/" + Dir + "/"
	if strings.Contains(string(data), line) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		line = "\n" + line
	}
	_, err = f.WriteString(line + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// copyIncluded copies the files matching .worktreeinclude's patterns from
// root to the new worktree, keeping their paths.
func copyIncluded(root, to string) error {
	f, err := os.Open(filepath.Join(root, IncludeFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		pattern := strings.TrimSpace(sc.Text())
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			return fmt.Errorf("%s: %w", pattern, err)
		}
		for _, src := range matches {
			rel, err := filepath.Rel(root, src)
			if err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, Dir) {
				continue
			}
			info, err := os.Lstat(src)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if err := copyFile(src, filepath.Join(to, rel), info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
