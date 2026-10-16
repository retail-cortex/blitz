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
	"strconv"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
)

// The workspace's branches: list, switch, create, and merge one into the
// current branch (Changes, spec_desktop_024 DSK-90c). The user asks for
// each; nothing here runs on the agent's behalf. Switching and merging
// check files out and merge them, which runs filters, merge drivers and
// hooks, so they take the same care as a pull (gitremote.go): the
// repository's own config may not set a command, and git never prompts.

var (
	// ErrNoBranch is a branch that doesn't exist, locally or on a remote.
	ErrNoBranch = errors.New("there's no such branch")
	// ErrBranchExists is creating a branch whose name is taken.
	ErrBranchExists = errors.New("a branch with that name already exists")
	// ErrBadBranchName is a name git doesn't accept for a branch.
	ErrBadBranchName = errors.New("that isn't a valid branch name")
	// ErrLocalChanges is a switch that would overwrite uncommitted changes.
	ErrLocalChanges = errors.New("uncommitted changes would be overwritten: commit or stash them first")
	// ErrMergeSelf is merging the current branch into itself.
	ErrMergeSelf = errors.New("that's the current branch")
)

// Branch is a branch the workspace can switch to or merge.
type Branch struct {
	// Name is the branch's short name: "main", or "origin/feature" for a
	// remote one.
	Name string
	// Remote is a remote's branch that no local branch tracks: switching to
	// it makes a local one that does.
	Remote bool
	// Current is the branch checked out.
	Current bool
	// Upstream is a local branch's remote one, such as "origin/main".
	Upstream string
	// Subject and Time are its last commit's.
	Subject string
	Time    time.Time
}

// MergeResult is what MergeBranch did, and the repository's state after it.
type MergeResult struct {
	// Outcome is PullUpToDate, PullFastForward, PullMerged or PullConflicts
	// (the merge waits for ResolveConflict and CompleteMerge, or AbortMerge).
	Outcome PullOutcome
	Commits int // commits brought in from the branch
	Status  api.GitStatus
}

// branchFormat is for-each-ref's format: one branch a line, fields split
// by NUL (a subject has no newline).
const branchFormat = "%(refname)%00%(HEAD)%00%(upstream:short)%00%(committerdate:unix)%00%(contents:subject)"

// Branches lists the local branches, then the remotes' branches no local
// branch tracks, each newest commit first.
func (w *Workspace) Branches(ctx context.Context) ([]Branch, error) {
	if !w.inRepository(ctx) {
		return nil, ErrNotARepository
	}
	out, err := w.gitDo(ctx, nil, "for-each-ref", "--sort=-committerdate", "--format="+branchFormat, "refs/heads", "refs/remotes")
	if err != nil {
		return nil, fmt.Errorf("git for-each-ref: %w", err)
	}
	var local, remote []Branch
	tracked := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.SplitN(line, "\x00", 5)
		if len(f) != 5 {
			continue
		}
		b := Branch{Current: f[1] == "*", Upstream: f[2], Subject: f[4]}
		if sec, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			b.Time = time.Unix(sec, 0)
		}
		if name, ok := strings.CutPrefix(f[0], "refs/heads/"); ok {
			b.Name = name
			local = append(local, b)
			tracked[b.Upstream] = true
		} else if name, ok := strings.CutPrefix(f[0], "refs/remotes/"); ok && !strings.HasSuffix(name, "/HEAD") {
			b.Name, b.Remote, b.Upstream = name, true, ""
			remote = append(remote, b)
		}
	}
	for _, b := range remote {
		if !tracked[b.Name] {
			local = append(local, b)
		}
	}
	return local, nil
}

// branchStart checks a branch action may run (a repository whose own
// config runs nothing, with no merge or rebase waiting) and returns the
// git status it starts from.
func (w *Workspace) branchStart(ctx context.Context) (api.GitStatus, error) {
	st, err := w.remoteStart(ctx)
	if err != nil {
		return st, err
	}
	if st.Operation != "" {
		return st, ErrInProgress
	}
	return st, nil
}

// checkBranchName refuses a name git wouldn't give a branch (or that
// would read as an option).
func (w *Workspace) checkBranchName(ctx context.Context, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return ErrBadBranchName
	}
	if _, err := w.gitDo(ctx, nil, "check-ref-format", "--branch", name); err != nil {
		return ErrBadBranchName
	}
	return nil
}

// refExists reports whether ref (such as "refs/heads/main") exists.
func (w *Workspace) refExists(ctx context.Context, ref string) bool {
	_, err := w.gitDo(ctx, nil, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

// branchRef is name's full ref: a local branch, else a remote's.
func (w *Workspace) branchRef(ctx context.Context, name string) (ref string, remote bool, err error) {
	if err := w.checkBranchName(ctx, name); err != nil {
		return "", false, err
	}
	switch {
	case w.refExists(ctx, "refs/heads/"+name):
		return "refs/heads/" + name, false, nil
	case w.refExists(ctx, "refs/remotes/"+name):
		return "refs/remotes/" + name, true, nil
	}
	return "", false, ErrNoBranch
}

// SwitchBranch checks out branch name, carrying uncommitted changes along
// when they don't clash with it (ErrLocalChanges when they do). A remote's
// branch ("origin/feature") gets a local branch that tracks it, as git
// switch does.
func (w *Workspace) SwitchBranch(ctx context.Context, name string) (api.GitStatus, error) {
	st, err := w.branchStart(ctx)
	if err != nil {
		return st, err
	}
	ref, remote, err := w.branchRef(ctx, name)
	if err != nil {
		return st, err
	}
	args := []string{"switch", "--quiet", "--no-guess", strings.TrimPrefix(ref, "refs/heads/")}
	if remote {
		args = []string{"switch", "--quiet", "--track", ref}
	}
	if out, err := w.gitRemote(ctx, args...); err != nil {
		return st, branchError("switch", err, out)
	}
	return w.GitStatus(ctx)
}

// CreateBranch makes branch name at the current commit and switches to
// it, keeping uncommitted changes.
func (w *Workspace) CreateBranch(ctx context.Context, name string) (api.GitStatus, error) {
	st, err := w.branchStart(ctx)
	if err != nil {
		return st, err
	}
	if err := w.checkBranchName(ctx, name); err != nil {
		return st, err
	}
	if w.refExists(ctx, "refs/heads/"+name) {
		return st, ErrBranchExists
	}
	if out, err := w.gitRemote(ctx, "switch", "--quiet", "--create", name); err != nil {
		return st, branchError("switch --create", err, out)
	}
	return w.GitStatus(ctx)
}

// MergeBranch merges branch name (local or a remote's) into the current
// one: a fast-forward when it can, else a merge commit with git's message,
// keeping uncommitted changes (--autostash). A merge that stops on
// conflicts isn't an error: its outcome is PullConflicts, and it waits for
// ResolveConflict and CompleteMerge (or AbortMerge).
func (w *Workspace) MergeBranch(ctx context.Context, name string) (MergeResult, error) {
	before, err := w.branchStart(ctx)
	if err != nil {
		return MergeResult{Status: before}, err
	}
	ref, _, err := w.branchRef(ctx, name)
	if err != nil {
		return MergeResult{Status: before}, err
	}
	if ref == "refs/heads/"+before.Branch {
		return MergeResult{Status: before}, ErrMergeSelf
	}
	head := w.head(ctx)
	// The short name, so the merge commit says "Merge branch 'feature'".
	_, mergeErr := w.gitRemote(ctx, "merge", "--no-edit", "--autostash", "--no-stat", "--end-of-options", name)
	after, err := w.GitStatus(ctx)
	if err != nil {
		return MergeResult{}, err
	}
	res := MergeResult{Status: after, Commits: w.newCommits(ctx, head, ref)}
	if mergeErr != nil {
		if after.Operation == api.GitMerging && len(after.Conflicts) > 0 {
			res.Outcome = PullConflicts
			return res, nil
		}
		return res, fmt.Errorf("git merge: %w", mergeErr)
	}
	switch now := w.head(ctx); {
	case now == head:
		res.Outcome = PullUpToDate
	case w.isMerge(ctx, now):
		res.Outcome = PullMerged
	default:
		res.Outcome = PullFastForward
	}
	return res, nil
}

// branchError gives a failed switch its kind.
func branchError(action string, err error, out string) error {
	if strings.Contains(strings.ToLower(out), "would be overwritten") {
		return fmt.Errorf("%w (%s)", ErrLocalChanges, lastLine(out))
	}
	return fmt.Errorf("git %s: %w", action, err)
}
