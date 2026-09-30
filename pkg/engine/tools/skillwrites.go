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
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
)

// Scripts of skills that declare writes_workspace (BL-SK-02) run in a copy
// of the workspace. Afterwards what they changed is shown as a diff and,
// once approved, written through the workspace, so it joins the turn's
// checkpoint and /undo reverts it.

const (
	// wsCopyMaxBytes and wsCopyMaxFiles bound the copy a script gets.
	wsCopyMaxBytes = 512 << 20
	wsCopyMaxFiles = 50_000
)

// errWorkspaceTooBig refuses a copy past the bounds.
var errWorkspaceTooBig = errors.New("the workspace is too large to copy for a script that writes it")

// wsCopy is a workspace's copy for a script: the files as copied, by
// relative path, with their hashes.
type wsCopy struct {
	dir    string
	hashes map[string][32]byte
}

// copyWorkspace copies ws's files into a new temporary directory: not .git,
// symbolic links, blocked paths or files over the size limit, which the
// script neither sees nor may change.
func copyWorkspace(ws *Workspace) (*wsCopy, error) {
	tmp, err := os.MkdirTemp("", "blitz-skill-ws-*")
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	c := &wsCopy{dir: tmp, hashes: map[string][32]byte{}}
	var total int64
	root := ws.Dir()
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			if d.Name() == ".git" && p != root {
				return filepath.SkipDir
			}
			if _, blocked := ws.Blocked().Match(p); blocked && p != root {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(tmp, rel), 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if _, blocked := ws.Blocked().Match(p); blocked {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > ws.maxFileSize {
			return nil
		}
		if total += info.Size(); total > wsCopyMaxBytes || len(c.hashes) >= wsCopyMaxFiles {
			return errWorkspaceTooBig
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil // unreadable: left out
		}
		if err := os.WriteFile(filepath.Join(tmp, rel), data, info.Mode().Perm()|0o600); err != nil {
			return err
		}
		c.hashes[filepath.ToSlash(rel)] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	return c, nil
}

// wsChange is one file a script changed in the copy.
type wsChange struct {
	path    string // relative, slash-separated
	after   []byte
	deleted bool
}

// changes lists what the script changed in the copy, sorted: files whose
// content differs, new ones, and deleted ones.
func (c *wsCopy) changes() ([]wsChange, error) {
	var out []wsChange
	seen := map[string]bool{}
	err := filepath.WalkDir(c.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(c.dir, p)
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if h, ok := c.hashes[rel]; ok && h == sha256.Sum256(data) {
			return nil
		}
		out = append(out, wsChange{path: rel, after: data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	for rel := range c.hashes {
		if !seen[rel] {
			out = append(out, wsChange{path: rel, deleted: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

// diff renders the changes against the workspace as it is now.
func (c *wsCopy) diff(ws *Workspace, changes []wsChange) string {
	var sb strings.Builder
	for _, ch := range changes {
		before, _ := os.ReadFile(filepath.Join(ws.Dir(), filepath.FromSlash(ch.path)))
		after := string(ch.after)
		if ch.deleted {
			after = ""
		}
		sb.WriteString(unifiedDiff(ch.path, string(before), after))
	}
	return sb.String()
}

// keepChanges asks to keep what a script changed and, approved, writes it
// through the workspace (checkpointed with the call's turn). It returns
// the files written.
func (c *wsCopy) keepChanges(ctx context.Context, ws *Workspace, hooks *Hooks, skill string, changes []wsChange) ([]string, error) {
	var files []string
	for _, ch := range changes {
		files = append(files, ch.path)
	}
	if err := hooks.Approve(ctx, api.ApprovalRequest{
		Tool: "run_skill_script", Kind: api.ActionWrite, MustAsk: true, // as tier 3: every time
		Detail:  fmt.Sprintf("Keep the changes skill %s's script made: %s", skill, strings.Join(files, ", ")),
		Diff:    c.diff(ws, changes),
		Targets: files,
	}); err != nil {
		return nil, err
	}
	var written []string
	var errs []error
	for _, ch := range changes {
		var err error
		if ch.deleted {
			err = ws.RemoveFile(ctx, ch.path)
			if errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
		} else {
			err = ws.WriteFileAtomic(ctx, ch.path, bytes.Clone(ch.after))
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ch.path, err))
			continue
		}
		written = append(written, ch.path)
	}
	return written, errors.Join(errs...)
}

// remove deletes the copy.
func (c *wsCopy) remove() { os.RemoveAll(c.dir) }
