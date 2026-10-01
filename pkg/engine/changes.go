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
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

// Checkpoints of the agent's file changes, and the approvals it was given.

// ListCheckpoints returns the turns that changed files, newest first.
func (w *Workspace) ListCheckpoints() []api.Checkpoint {
	var out []api.Checkpoint
	for _, c := range w.tools.Checkpoints().List() {
		out = append(out, api.Checkpoint{ID: c.ID, Label: c.Label, Time: c.Time, Files: c.Files})
	}
	return out
}

// Undo restores the files the latest turn with changes modified, and
// audits it. A partial restore returns both what was restored and why the
// rest wasn't.
func (w *Workspace) Undo(force bool) (api.UndoResult, error) {
	res, err := w.tools.Checkpoints().Undo(force)
	if errors.Is(err, tools.ErrNothingToUndo) {
		return api.UndoResult{}, api.ErrNothingToUndo // the API's, which the service maps
	}
	out := api.UndoResult{Label: res.Turn.Label, Restored: res.Restored}
	if len(res.Restored) > 0 {
		w.tools.Hooks().Audit().Log(audit.Entry{Kind: audit.KindUndo, Detail: strings.Join(res.Restored, ", ")})
	}
	return out, err
}

// UndoWorkerRun restores the files worker run runID changed, as they were
// before it (BL-WK-02), with /undo's conflict rules.
func (w *Workspace) UndoWorkerRun(runID string, force bool) (api.UndoResult, error) {
	res, err := w.tools.Checkpoints().UndoRun(runID, force)
	if errors.Is(err, tools.ErrNothingToUndo) {
		return api.UndoResult{}, fmt.Errorf("%w: run %s changed no files that can still be restored", api.ErrNothingToUndo, runID)
	}
	out := api.UndoResult{Label: res.Turn.Label, Restored: res.Restored}
	if len(res.Restored) > 0 {
		w.tools.Hooks().Audit().Log(audit.Entry{Kind: audit.KindUndo, Detail: "worker run " + runID + ": " + strings.Join(res.Restored, ", ")})
	}
	return out, err
}

// SessionDiff is a unified diff of every file the agent changed in the
// active session, against its state before the first change ("" when
// none). Checkpoints persist, so this spans the session's earlier runs.
func (w *Workspace) SessionDiff() string {
	active := w.storage.Active()
	if active == nil {
		return ""
	}
	return w.tools.Checkpoints().SessionDiff(active.ID)
}

// GitDiff runs git diff (stat and patch) in the workspace. color keeps
// git's terminal colours. On failure the output holds git's message.
//
// The agent can write the repository's .git/config, and git diff would run
// commands named there (fsmonitor, external diff, textconv and filter
// drivers) outside the sandbox. They are all switched off.
func (w *Workspace) GitDiff(ctx context.Context, color bool) (string, error) {
	ui := "never"
	if color {
		ui = "always"
	}
	args := []string{"-c", "color.ui=" + ui, "-c", "core.fsmonitor=false"}
	args = append(args, noFilters(ctx, w.Dir())...)
	args = append(args, "diff", "--no-ext-diff", "--no-textconv", "--stat", "--patch")
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = w.Dir()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// GitStatus reads the workspace's branch and changed files.
func (w *Workspace) GitStatus(ctx context.Context) (api.GitStatus, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return api.GitStatus{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	base := append([]string{"-c", "core.fsmonitor=false", "--no-optional-locks"}, noFilters(ctx, w.Dir())...)
	git := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", append(slices.Clone(base), args...)...)
		cmd.Dir = w.Dir()
		return cmd.Output()
	}
	st := api.GitStatus{Git: true}
	if out, err := git("rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(string(out)) != "true" {
		return st, nil // not a repository
	}
	st.Repo = true
	// The branch, also before its first commit; else detached.
	if out, err := git("symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		st.Branch = strings.TrimSpace(string(out))
	} else if sha, err := git("rev-parse", "--short", "HEAD"); err == nil {
		st.Branch = strings.TrimSpace(string(sha))
	}
	status, err := git("status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return st, err
	}
	for _, rec := range strings.Split(string(status), "\x00") {
		if len(rec) >= 3 && rec[2] == ' ' {
			st.Changed++
		}
	}
	return st, nil
}

// ErrNoGit is GitInit's error without git on PATH.
var ErrNoGit = errors.New("git isn't installed: it isn't on the Blitz service's PATH")

// GitInit makes the workspace a git repository (git init), unless it's in
// one already, and returns its git status.
func (w *Workspace) GitInit(ctx context.Context) (api.GitStatus, error) {
	st, err := w.GitStatus(ctx)
	switch {
	case err != nil:
		return st, err
	case !st.Git:
		return st, ErrNoGit
	case st.Repo:
		return st, nil
	}
	cmd := exec.CommandContext(ctx, "git", "init", "-q")
	cmd.Dir = w.Dir()
	if out, err := cmd.CombinedOutput(); err != nil {
		return st, fmt.Errorf("git init: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return w.GitStatus(ctx)
}

// noFilters returns git options that blank every filter driver configured
// for the repository in dir. Reading config runs nothing.
func noFilters(ctx context.Context, dir string) []string {
	cmd := exec.CommandContext(ctx, "git", "config", "-z", "--name-only", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`)
	cmd.Dir = dir
	out, _ := cmd.Output() // none configured: exit status 1
	var args []string
	for _, key := range strings.Split(string(out), "\x00") {
		switch {
		case key == "":
		case strings.HasSuffix(key, ".required"):
			args = append(args, "-c", key+"=false")
		default:
			args = append(args, "-c", key+"=")
		}
	}
	return args
}

func parseApproval(key string) api.Approval {
	a := api.Approval{Key: key}
	kind, rest, ok := strings.Cut(key, ":")
	if !ok {
		return api.Approval{Key: key, Subject: key}
	}
	a.Kind, a.Subject = kind, rest
	switch kind {
	case "cmd":
		if dir, cmd, ok := strings.Cut(rest, "\x00"); ok {
			a.Dir, a.Subject = dir, cmd
		}
	case "uc-run":
		a.Subject = strings.ReplaceAll(rest, "\x00", " ")
	}
	return a
}

// ListApprovals returns this session's approvals, then saved ones.
func (w *Workspace) ListApprovals() []api.Approval {
	hooks := w.tools.Hooks()
	var out []api.Approval
	for _, k := range hooks.SessionRules() {
		out = append(out, parseApproval(k))
	}
	for _, r := range hooks.Store().Rules() {
		a := parseApproval(r.Key)
		a.Always, a.Added = true, r.Added
		out = append(out, a)
	}
	return out
}

// RevokeApprovals removes approvals by key, for this session and from the
// saved ones, and returns how many keys it was given.
func (w *Workspace) RevokeApprovals(keys ...string) int {
	hooks := w.tools.Hooks()
	store := hooks.Store()
	for _, k := range keys {
		hooks.RevokeSession(k)
		if store != nil {
			store.Remove(k)
		}
	}
	return len(keys)
}

// ClearApprovals revokes every approval and returns how many there were.
func (w *Workspace) ClearApprovals() int {
	var keys []string
	for _, a := range w.ListApprovals() {
		keys = append(keys, a.Key)
	}
	return w.RevokeApprovals(keys...)
}
