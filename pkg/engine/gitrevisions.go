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
	"fmt"
	"regexp"
	"strings"
	"time"
)

// A file's revisions in git, for the Files view's View revisions (FIL-59a):
// the commits that changed it, how it differs from each, and putting it
// back as it was in one. Git runs as gitDo runs it (no filters, no
// fsmonitor), and a revision must be a commit's hash, never an option.

// ErrBadRevision is a revision that isn't a commit hash of the file's.
var ErrBadRevision = errors.New("not a revision of the file")

// FileRevision is one commit that changed a file.
type FileRevision struct {
	Commit  string // the full hash
	Short   string // the abbreviated hash
	Author  string
	When    time.Time
	Subject string
	// Path is the file's path at that commit, from the repository's root
	// (it differs from today's when the file was renamed since).
	Path string
}

// maxRevisions bounds a file's history.
const maxRevisions = 200

// revisionRE is what a revision may be: a commit hash, whole or abbreviated.
var revisionRE = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// FileHistory returns the commits that changed the file p, newest first,
// following renames; at most limit (50 when 0).
func (w *Workspace) FileHistory(ctx context.Context, p string, limit int) ([]FileRevision, error) {
	slash, err := w.revisionFile(ctx, p)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, maxRevisions)
	out, err := w.gitDo(ctx, nil, "-c", "core.quotePath=false", "--literal-pathspecs", "log", "--follow", "--no-color",
		fmt.Sprintf("--max-count=%d", limit), "--format=%x1e%H%x1f%h%x1f%an%x1f%aI%x1f%s", "--name-only", "--", slash)
	if err != nil {
		return nil, err
	}
	var revs []FileRevision
	for _, rec := range strings.Split(string(out), "\x1e") {
		head, names, _ := strings.Cut(strings.TrimSpace(rec), "\n")
		f := strings.Split(head, "\x1f")
		if len(f) != 5 {
			continue
		}
		r := FileRevision{Commit: f[0], Short: f[1], Author: f[2], Subject: f[4]}
		r.When, _ = time.Parse(time.RFC3339, f[3])
		for _, n := range strings.Split(names, "\n") {
			if n = strings.TrimSpace(n); n != "" {
				r.Path = n
			}
		}
		revs = append(revs, r)
	}
	return revs, nil
}

// FileRevisionDiff returns how the file p today differs from its revision
// rev, as a unified diff ("" when they're the same).
func (w *Workspace) FileRevisionDiff(ctx context.Context, p, rev string) (string, error) {
	slash, then, err := w.revisionOf(ctx, p, rev)
	if err != nil {
		return "", err
	}
	// Both names, so a rename since shows as one: then from the
	// repository's root, today's from the workspace.
	out, err := w.gitDo(ctx, nil, "-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "-M", rev, "--",
		":(top,literal)"+then, ":(literal)"+slash)
	return string(out), err
}

// RestoreFileRevision makes the file p as it was in rev, unless it changed
// since version (its version as the caller last saw it, FileVersion; ""
// when it doesn't exist). The index is left alone, so the restore shows
// as a change to commit or discard.
func (w *Workspace) RestoreFileRevision(ctx context.Context, p, rev, version string) (string, error) {
	slash, then, err := w.revisionOf(ctx, p, rev)
	if err != nil {
		return "", err
	}
	data, err := w.gitDo(ctx, nil, "cat-file", "blob", rev+":"+then)
	if err != nil {
		return "", err
	}
	rel, _, _ := userPath(slash)
	err = w.files().UserWriteFile(ctx, rel, data, func(current []byte, exists bool) error {
		now := ""
		if exists {
			now = FileVersion(current)
		}
		if now != version {
			return &FileChangedError{Path: slash, Current: now}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	w.userEdited(slash, "restored as it was in commit "+rev[:7])
	return FileVersion(data), nil
}

// revisionFile checks p names a file in a repository; it returns its "/"
// form.
func (w *Workspace) revisionFile(ctx context.Context, p string) (string, error) {
	rel, slash, err := userPath(p)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == "" {
		return "", fmt.Errorf("%w: name a file", ErrBadPath)
	}
	if !w.inRepository(ctx) {
		return "", ErrNotARepository
	}
	return slash, nil
}

// revisionOf checks rev is one of the commits in the file p's history; it
// returns p's "/" form and the file's path in rev, from the repository's
// root. Anything else, an option or a branch name included, is refused.
func (w *Workspace) revisionOf(ctx context.Context, p, rev string) (slash, then string, err error) {
	if !revisionRE.MatchString(rev) {
		return "", "", fmt.Errorf("%w: %q", ErrBadRevision, rev)
	}
	revs, err := w.FileHistory(ctx, p, maxRevisions)
	for _, r := range revs {
		if strings.HasPrefix(r.Commit, rev) && r.Path != "" {
			slash, _ = w.revisionFile(ctx, p)
			return slash, r.Path, nil
		}
	}
	if err == nil {
		err = fmt.Errorf("%w: %s", ErrBadRevision, rev)
	}
	return "", "", err
}
