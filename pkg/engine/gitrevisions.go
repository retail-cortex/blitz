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
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// History in git, for the Files view's View revisions (FIL-59a, FIL-59b):
// the commits that changed a file (following renames), a folder or the
// whole workspace, with their parents for the graph; how a revision
// differs from now, from its parent or from another revision; and putting
// a file, or a folder's files, back as they were. Git runs as gitDo runs
// it (no filters, no fsmonitor), and a revision must be a commit's hash,
// never an option or a branch name.

// ErrBadRevision is a revision that isn't a commit hash of the history's.
var ErrBadRevision = errors.New("not a revision in the history")

// Revision is one commit in a history.
type Revision struct {
	Commit  string // the full hash
	Short   string // the abbreviated hash
	Parents []string
	Author  string
	Email   string
	When    time.Time
	Subject string
	// Path is a file's path at that commit, from the repository's root (it
	// differs from today's when the file was renamed since); "" for a
	// folder's history.
	Path string
	// Added and Removed are the lines it changed in the file or folder,
	// in Files files (binary files count as files, not lines).
	Added, Removed, Files int
}

// History lengths: the default page, and the most in one.
const (
	defaultHistory = 100
	maxHistory     = 500
)

// maxRevisionDiff bounds a diff a history shows.
const maxRevisionDiff = 2 << 20

// emptyTree is git's empty tree, what a first commit is compared with.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// revisionRE is what a revision may be: a commit hash, whole or abbreviated.
var revisionRE = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// historyTarget is what a history is of: a file, a folder, or the
// workspace (slash "").
type historyTarget struct {
	slash  string
	folder bool
}

// spec is the target as a git pathspec, from the workspace.
func (t historyTarget) spec() string {
	if t.slash == "" {
		return "."
	}
	return ":(literal)" + t.slash
}

func (w *Workspace) historyTarget(ctx context.Context, p string) (historyTarget, error) {
	rel, slash, err := userPath(p)
	if err != nil {
		return historyTarget{}, err
	}
	if !w.inRepository(ctx) {
		return historyTarget{}, ErrNotARepository
	}
	if rel == "." || rel == "" {
		return historyTarget{folder: true}, nil
	}
	info, err := os.Stat(filepath.Join(w.Dir(), rel))
	return historyTarget{slash: slash, folder: err == nil && info.IsDir()}, nil
}

// History returns the commits that changed p (a file, following renames; a
// folder, anything under it; "" the workspace), newest first: limit of
// them (100 when 0, at most 500) after the first skip. more says older
// ones follow.
func (w *Workspace) History(ctx context.Context, p string, limit, skip int) (revs []Revision, more bool, err error) {
	t, err := w.historyTarget(ctx, p)
	if err != nil {
		return nil, false, err
	}
	if limit <= 0 {
		limit = defaultHistory
	}
	limit, skip = min(limit, maxHistory), max(skip, 0)
	args := []string{"-c", "core.quotePath=false", "log", "--no-color", "--parents", "--numstat",
		"--format=%x1e%H%x1f%h%x1f%P%x1f%an%x1f%ae%x1f%aI%x1f%s"}
	drop := 0
	if t.folder {
		args = append(args, fmt.Sprintf("--max-count=%d", limit+1), fmt.Sprintf("--skip=%d", skip))
	} else {
		// --skip doesn't follow renames: the first skip are read and dropped.
		args = append(args, "--follow", fmt.Sprintf("--max-count=%d", skip+limit+1))
		drop = skip
	}
	out, err := w.gitDo(ctx, nil, append(args, "--", t.spec())...)
	if err != nil {
		return nil, false, err
	}
	for _, rec := range strings.Split(string(out), "\x1e") {
		head, stats, _ := strings.Cut(strings.TrimSpace(rec), "\n")
		f := strings.Split(head, "\x1f")
		if len(f) != 7 {
			continue
		}
		r := Revision{Commit: f[0], Short: f[1], Parents: strings.Fields(f[2]), Author: f[3], Email: f[4], Subject: f[6]}
		r.When, _ = time.Parse(time.RFC3339, f[5])
		for _, line := range strings.Split(stats, "\n") {
			cols := strings.SplitN(line, "\t", 3)
			if len(cols) != 3 {
				continue
			}
			a, _ := strconv.Atoi(cols[0]) // "-" for a binary file
			d, _ := strconv.Atoi(cols[1])
			r.Added, r.Removed, r.Files = r.Added+a, r.Removed+d, r.Files+1
			if !t.folder {
				r.Path = renamedTo(cols[2])
			}
		}
		revs = append(revs, r)
	}
	revs = revs[min(drop, len(revs)):]
	if len(revs) > limit {
		return revs[:limit], true, nil
	}
	return revs, false, nil
}

// renamedTo is the path a numstat line names after a rename ("a => b",
// "dir/{a => b}/c"), or the path itself.
func renamedTo(s string) string {
	i := strings.Index(s, " => ")
	if i < 0 {
		return s
	}
	open, closing := strings.LastIndex(s[:i], "{"), strings.Index(s[i:], "}")
	if open < 0 || closing < 0 {
		return s[i+4:]
	}
	p := s[:open] + s[i+4:i+closing] + s[i+closing+1:]
	return strings.ReplaceAll(p, "//", "/")
}

// commit checks rev is a commit's hash and returns it in full; an option, a
// branch name or anything else is refused.
func (w *Workspace) commit(ctx context.Context, rev string) (string, error) {
	if !revisionRE.MatchString(rev) {
		return "", fmt.Errorf("%w: %q", ErrBadRevision, rev)
	}
	out, err := w.gitDo(ctx, nil, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrBadRevision, rev)
	}
	return strings.TrimSpace(string(out)), nil
}

// filePaths is where the file slash was in each commit of its history
// (from the repository's root), newest first.
func (w *Workspace) filePaths(ctx context.Context, slash string) ([][2]string, error) {
	out, err := w.gitDo(ctx, nil, "-c", "core.quotePath=false", "--literal-pathspecs", "log", "--follow", "--no-color",
		"--format=%x1e%H", "--name-only", "--", slash)
	if err != nil {
		return nil, err
	}
	var paths [][2]string
	for _, rec := range strings.Split(string(out), "\x1e") {
		hash, names, _ := strings.Cut(strings.TrimSpace(rec), "\n")
		if hash = strings.TrimSpace(hash); hash == "" {
			continue
		}
		path := ""
		for _, n := range strings.Split(names, "\n") {
			if n = strings.TrimSpace(n); n != "" {
				path = n
			}
		}
		paths = append(paths, [2]string{hash, path})
	}
	return paths, nil
}

// pathAt is the file slash's path at commit full (from the repository's
// root), and at the commit before it in its history; ErrBadRevision when
// the commit isn't in the file's history.
func (w *Workspace) pathAt(ctx context.Context, slash, full string) (then, before string, err error) {
	paths, err := w.filePaths(ctx, slash)
	if err != nil {
		return "", "", err
	}
	for i, p := range paths {
		if p[0] == full && p[1] != "" {
			before = p[1]
			if i+1 < len(paths) && paths[i+1][1] != "" {
				before = paths[i+1][1]
			}
			return p[1], before, nil
		}
	}
	return "", "", fmt.Errorf("%w: %s", ErrBadRevision, full)
}

// DiffAgainst is what a revision is compared with.
type DiffAgainst int

const (
	// AgainstNow compares the revision with the working copy.
	AgainstNow DiffAgainst = iota
	// AgainstParent shows what the revision changed.
	AgainstParent
	// AgainstOther compares the revision with another one.
	AgainstOther
)

// RevisionDiff returns a unified diff for p (a file, a folder, "" the
// workspace): from rev to now, rev's own changes, or from rev to other.
// truncated says it was cut at 2 MB, at a line.
func (w *Workspace) RevisionDiff(ctx context.Context, p, rev, other string, against DiffAgainst) (diff string, truncated bool, err error) {
	t, err := w.historyTarget(ctx, p)
	if err != nil {
		return "", false, err
	}
	from, err := w.commit(ctx, rev)
	if err != nil {
		return "", false, err
	}
	to := ""
	switch against {
	case AgainstParent:
		to, from = from, emptyTree
		if parent, err := w.gitDo(ctx, nil, "rev-parse", "--verify", "--quiet", "--end-of-options", to+"^"); err == nil {
			from = strings.TrimSpace(string(parent))
		}
	case AgainstOther:
		if to, err = w.commit(ctx, other); err != nil {
			return "", false, err
		}
	}
	specs := []string{t.spec()}
	if !t.folder {
		// Both names, so a rename between them shows as one: the file's
		// path in each revision (from the repository's root), or today's.
		thenFrom, before, err := w.pathAt(ctx, t.slash, map[bool]string{true: to, false: from}[against == AgainstParent])
		if err != nil {
			return "", false, err
		}
		switch against {
		case AgainstNow:
			specs = []string{":(top,literal)" + thenFrom, ":(literal)" + t.slash}
		case AgainstParent:
			specs = []string{":(top,literal)" + before, ":(top,literal)" + thenFrom}
		case AgainstOther:
			thenTo, _, err := w.pathAt(ctx, t.slash, to)
			if err != nil {
				return "", false, err
			}
			specs = []string{":(top,literal)" + thenFrom, ":(top,literal)" + thenTo}
		}
	}
	args := []string{"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "-M", from}
	if to != "" {
		args = append(args, to)
	}
	out, err := w.gitDo(ctx, nil, append(append(args, "--"), specs...)...)
	if err != nil {
		return "", false, err
	}
	if len(out) > maxRevisionDiff {
		cut := bytes.LastIndexByte(out[:maxRevisionDiff], '\n')
		return string(out[:cut+1]), true, nil
	}
	return string(out), false, nil
}

// Restore puts p back as it was in rev: a file as RestoreFileRevision does
// (its new version), a folder or the workspace as RestoreFolder does (the
// files it put back, or would with preview).
func (w *Workspace) Restore(ctx context.Context, p, rev, version string, preview bool) (string, []RestoredFile, error) {
	t, err := w.historyTarget(ctx, p)
	if err != nil {
		return "", nil, err
	}
	if t.folder {
		files, err := w.RestoreFolder(ctx, p, rev, preview)
		return "", files, err
	}
	if preview {
		return "", nil, nil
	}
	v, err := w.RestoreFileRevision(ctx, p, rev, version)
	return v, nil, err
}

// RestoreFileRevision makes the file p as it was in rev, unless it changed
// since version (its version as the caller last saw it, FileVersion; ""
// when it doesn't exist). The index is left alone, so the restore shows
// as a change to commit or discard.
func (w *Workspace) RestoreFileRevision(ctx context.Context, p, rev, version string) (string, error) {
	t, err := w.historyTarget(ctx, p)
	if err != nil {
		return "", err
	}
	if t.folder {
		return "", fmt.Errorf("%w: %s is a folder", ErrBadPath, p)
	}
	full, err := w.commit(ctx, rev)
	if err != nil {
		return "", err
	}
	then, _, err := w.pathAt(ctx, t.slash, full)
	if err != nil {
		return "", err
	}
	data, err := w.gitDo(ctx, nil, "cat-file", "blob", full+":"+then)
	if err != nil {
		return "", err
	}
	rel, _, _ := userPath(t.slash)
	err = w.files().UserWriteFile(ctx, rel, data, func(current []byte, exists bool) error {
		now := ""
		if exists {
			now = FileVersion(current)
		}
		if now != version {
			return &FileChangedError{Path: t.slash, Current: now}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	w.userEdited(t.slash, "restored as it was in commit "+full[:7])
	return FileVersion(data), nil
}

// RestoredFile is a file a folder's restore puts back: changed since
// (restored), or deleted since (recreated).
type RestoredFile struct {
	Path      string // workspace-relative, with "/"
	Recreated bool
}

// RestoreFolder puts the files under the folder p ("" the workspace) back
// as they were in rev: those changed since, and those deleted since.
// Files added since are kept. With preview, it only says which it would
// put back. The index is left alone.
func (w *Workspace) RestoreFolder(ctx context.Context, p, rev string, preview bool) ([]RestoredFile, error) {
	t, err := w.historyTarget(ctx, p)
	if err != nil {
		return nil, err
	}
	if !t.folder {
		return nil, fmt.Errorf("%w: %s isn't a folder", ErrBadPath, p)
	}
	full, err := w.commit(ctx, rev)
	if err != nil {
		return nil, err
	}
	out, err := w.gitDo(ctx, nil, "-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--no-renames", "--name-status", "-z", full, "--", t.spec())
	if err != nil {
		return nil, err
	}
	prefix, err := w.gitDo(ctx, nil, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	var files []RestoredFile
	var specs bytes.Buffer
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		status, path := fields[i], fields[i+1]
		if status != "M" && status != "D" && status != "T" {
			continue // added since: kept
		}
		files = append(files, RestoredFile{Path: strings.TrimPrefix(path, strings.TrimSpace(string(prefix))), Recreated: status == "D"})
		specs.WriteString(":(top,literal)" + path + "\x00")
	}
	if preview || len(files) == 0 {
		return files, nil
	}
	if _, err := w.gitDo(ctx, specs.Bytes(), "restore", "--source="+full, "--worktree", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
		return nil, err
	}
	w.userEdited(cmp.Or(t.slash, "."), fmt.Sprintf("%d files restored as they were in commit %s", len(files), full[:7]))
	return files, nil
}
