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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// Git for the workspace's files: the files the agent creates are added to
// git after its turn, and the Files view's menu stages, unstages, discards
// and ignores. Git runs with the repository's filter drivers and fsmonitor
// off (as GitDiff does), so nothing the agent wrote in .git/config runs.

// ErrNotARepository is a git action outside a git repository.
var ErrNotARepository = errors.New("the workspace isn't in a git repository")

// GitAction is what GitFileAction does to a file or folder.
type GitAction string

const (
	// GitStage adds it to the index (a folder: everything changed under it).
	GitStage GitAction = "stage"
	// GitUnstage takes it out of the index, keeping the working copy.
	GitUnstage GitAction = "unstage"
	// GitDiscard puts it back as it was in the last commit.
	GitDiscard GitAction = "discard"
	// GitIgnore adds a line for it to the workspace's .gitignore.
	GitIgnore GitAction = "ignore"
)

// gitTimeout bounds one git command here.
const gitTimeout = 15 * time.Second

// gitDo runs git in the workspace for an action, safely (see above), with
// stdin if given, a time limit, and git's message in the error.
func (w *Workspace) gitDo(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	base := append([]string{"-c", "core.fsmonitor=false"}, noFilters(ctx, w.Dir())...)
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Dir = w.Dir()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, err
}

// inRepository reports whether the workspace is in a git work tree.
func (w *Workspace) inRepository(ctx context.Context) bool {
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	out, err := w.gitDo(ctx, nil, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// GitFileAction runs action on p, a workspace-relative file or folder.
func (w *Workspace) GitFileAction(ctx context.Context, p string, action GitAction) error {
	rel, slash, err := userPath(p)
	if err != nil {
		return err
	}
	if rel == "." || rel == "" {
		return fmt.Errorf("%w: name a file or folder", ErrBadPath)
	}
	if !w.inRepository(ctx) {
		return ErrNotARepository
	}
	switch action {
	case GitStage:
		_, err = w.gitDo(ctx, nil, "--literal-pathspecs", "add", "--all", "--", slash)
	case GitUnstage:
		_, err = w.gitDo(ctx, nil, "--literal-pathspecs", "restore", "--staged", "--", slash)
	case GitDiscard:
		if _, err = w.gitDo(ctx, nil, "--literal-pathspecs", "restore", "--source=HEAD", "--staged", "--worktree", "--", slash); err == nil {
			w.userEdited(slash, "changes discarded (back to the last commit)")
		}
	case GitIgnore:
		err = w.ignore(slash)
	default:
		return fmt.Errorf("unknown git action %q", action)
	}
	return err
}

// ignore adds an anchored line for slash ("/path", "/folder/") to the
// workspace's .gitignore, unless it's there already.
func (w *Workspace) ignore(slash string) error {
	line := "/" + slash
	if info, err := os.Stat(filepath.Join(w.Dir(), filepath.FromSlash(slash))); err == nil && info.IsDir() {
		line += "/"
	}
	file := filepath.Join(w.Dir(), ".gitignore")
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == line {
			return nil
		}
	}
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}
	data = append(data, line+"\n"...)
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return err
	}
	w.userEdited(".gitignore", "added "+line)
	return nil
}

// stageCreated adds the files the agent created in the session's turn to
// git in one git add, when [git] auto_add is on and the workspace is in a
// repository, and says so in the turn. Files git ignores, temporary files
// and files gone by the turn's end are left out; files that existed before
// (tracked or not) are left as they are.
func (w *Workspace) stageCreated(ctx context.Context, sessionID string, on func(api.Event)) {
	if !w.cfg.Git.AutoAdd {
		return
	}
	created := w.tools.Checkpoints().Created(sessionID)
	if len(created) == 0 {
		return
	}
	ctx = context.WithoutCancel(ctx) // a stopped turn's files are added too
	if !w.inRepository(ctx) {
		return
	}
	var paths []string
	for _, abs := range created {
		rel, err := filepath.Rel(w.Dir(), abs)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		slash := filepath.ToSlash(rel)
		if isTemporary(slash) {
			continue
		}
		if info, err := os.Stat(abs); err != nil || !info.Mode().IsRegular() {
			continue
		}
		paths = append(paths, slash)
	}
	if len(paths) == 0 {
		return
	}
	// Ignored files are dropped first: git add refuses a path it ignores.
	// (check-ignore exits 1 when it ignores none; it takes no pathspec magic.)
	out, err := w.gitDo(ctx, []byte(strings.Join(paths, "\x00")+"\x00"), "check-ignore", "-z", "--stdin")
	var exit *exec.ExitError
	switch {
	case err == nil:
		ignored := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
		paths = slices.DeleteFunc(paths, func(p string) bool { return slices.Contains(ignored, p) })
	case errors.As(err, &exit) && exit.ExitCode() == 1:
	default:
		slog.Warn("checking which new files git ignores", "workspace", w.Dir(), "error", err)
		return
	}
	if len(paths) == 0 {
		return
	}
	if _, err := w.gitDo(ctx, nil, append([]string{"--literal-pathspecs", "add", "--"}, paths...)...); err != nil {
		slog.Warn("adding the agent's new files to git", "workspace", w.Dir(), "error", err)
		on(api.Event{Notice: &api.Notice{Text: i18n.T("git.auto_add_failed", "error", err.Error()), Error: true}})
		return
	}
	on(api.Event{Notice: &api.Notice{Text: i18n.T("git.auto_added", "files", strings.Join(paths, ", "))}})
}

// temporary are file names and folders that hold scratch files, which the
// agent's new files never go to git from even when no .gitignore says so.
var (
	temporarySuffixes = []string{".tmp", ".temp", ".swp", ".swo", "~", ".orig", ".rej", ".bak"}
	temporaryNames    = []string{".DS_Store", "Thumbs.db", "desktop.ini"}
	temporaryDirs     = []string{"tmp", "temp", ".tmp", ".cache"}
)

// isTemporary reports whether a workspace-relative path looks like a
// scratch file.
func isTemporary(slash string) bool {
	base := path.Base(slash)
	if slices.Contains(temporaryNames, base) || strings.HasPrefix(base, ".#") {
		return true
	}
	for _, s := range temporarySuffixes {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	dirs := strings.Split(path.Dir(slash), "/")
	return slices.ContainsFunc(dirs, func(d string) bool { return slices.Contains(temporaryDirs, d) })
}
