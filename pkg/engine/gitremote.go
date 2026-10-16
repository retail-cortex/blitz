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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// The workspace and its remote: fetch, pull, push, and a pull's conflicts
// (Changes, spec_desktop_024 DSK-90b). The user asks for each; nothing
// here runs on the agent's behalf.
//
// Fetching and pulling run git's transport, credential helpers, filters
// and merge drivers, all of which git config can point at any command. The
// agent can write the repository's .git/config, so those settings are
// taken from the user's own (global and system) config only: when the
// repository's sets one, the action stops and names it (RepoConfigError).
// Hooks run, as they do for Commit. Git never prompts: a remote that needs
// a password the credential helper doesn't have fails (ErrRemoteAuth).

var (
	// ErrNoRemote is a push from a repository with no remote.
	ErrNoRemote = errors.New("the repository has no remote")
	// ErrNoUpstream is a pull on a branch that doesn't track a remote one.
	ErrNoUpstream = errors.New("the branch doesn't track a remote branch")
	// ErrPushRejected is a push the remote refused because it has commits
	// the branch doesn't: pull first.
	ErrPushRejected = errors.New("the remote has commits this branch doesn't have: pull them first")
	// ErrRemoteAuth is a fetch, pull or push the remote refused for want of
	// credentials.
	ErrRemoteAuth = errors.New("the remote wants credentials Blitz doesn't have: sign in with git in a terminal (or set up a credential helper or an SSH key), then try again")
	// ErrInProgress is a pull or push while a merge or rebase waits.
	ErrInProgress = errors.New("a merge or rebase is in progress: complete or abort it first")
	// ErrNoOperation is completing or aborting with no merge or rebase in
	// progress.
	ErrNoOperation = errors.New("no merge or rebase is in progress")
	// ErrConflictsRemain is completing a merge or rebase with files still in
	// conflict.
	ErrConflictsRemain = errors.New("some files are still in conflict")
	// ErrMarkersRemain is marking a file resolved that still has conflict
	// markers.
	ErrMarkersRemain = errors.New("the file still has conflict markers")
	// ErrNotConflicted is resolving a file that isn't in conflict.
	ErrNotConflicted = errors.New("the file isn't in conflict")
)

// RepoConfigError is a remote action refused because the repository's own
// git config (which the agent can write) sets something that runs a
// command.
type RepoConfigError struct {
	Key string // such as "core.sshcommand"
}

// Error says which setting, and what to do, in the user's language.
func (e *RepoConfigError) Error() string {
	return i18n.T("git.repo_config_command", "key", e.Key)
}

// PullOutcome is how a pull went.
type PullOutcome string

const (
	// PullUpToDate brought nothing new.
	PullUpToDate PullOutcome = "up_to_date"
	// PullFastForward moved the branch to the remote's.
	PullFastForward PullOutcome = "fast_forward"
	// PullMerged made a merge commit.
	PullMerged PullOutcome = "merged"
	// PullRebased replayed the branch's commits on the remote's.
	PullRebased PullOutcome = "rebased"
	// PullConflicts stopped on conflicts: a merge or rebase is in progress.
	PullConflicts PullOutcome = "conflicts"
)

// PullResult is what Pull did, and the repository's state after it.
type PullResult struct {
	Outcome PullOutcome
	Commits int // commits brought in from the remote
	Status  api.GitStatus
}

// ConflictChoice is how ResolveConflict settles a file.
type ConflictChoice string

const (
	// KeepMine keeps the branch's version (the one from before the pull).
	KeepMine ConflictChoice = "mine"
	// TakeTheirs takes the remote's version.
	TakeTheirs ConflictChoice = "theirs"
	// MarkResolved stages the file as edited by hand.
	MarkResolved ConflictChoice = "resolved"
)

// remoteTimeout bounds a fetch, pull or push: the network, and hooks.
var remoteTimeout = 2 * time.Minute

// remoteState adds the remote, the upstream with its ahead and behind
// counts, and a merge or rebase in progress with its conflicts to st. git
// runs read-only (GitStatus's runner).
func (w *Workspace) remoteState(git func(args ...string) ([]byte, error), st *api.GitStatus) {
	if out, err := git("remote"); err == nil && strings.TrimSpace(string(out)) != "" {
		st.Remote = true
	}
	if out, err := git("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		st.Upstream = strings.TrimSpace(string(out))
	}
	if st.Upstream != "" {
		if out, err := git("rev-list", "--left-right", "--count", "HEAD...@{upstream}"); err == nil {
			if f := strings.Fields(string(out)); len(f) == 2 {
				st.Ahead, _ = strconv.Atoi(f[0])
				st.Behind, _ = strconv.Atoi(f[1])
			}
		}
	}
	st.Operation = w.operation(git)
	if out, err := git("diff", "--name-only", "--diff-filter=U", "-z"); err == nil {
		for _, p := range strings.Split(string(out), "\x00") {
			if p != "" {
				st.Conflicts = append(st.Conflicts, p)
			}
		}
	}
}

// operation is the merge or rebase the repository is in the middle of.
func (w *Workspace) operation(git func(args ...string) ([]byte, error)) api.GitOperation {
	exists := func(name string) bool {
		out, err := git("rev-parse", "--git-path", name)
		if err != nil {
			return false
		}
		p := strings.TrimSpace(string(out))
		if !filepath.IsAbs(p) {
			p = filepath.Join(w.Dir(), p)
		}
		_, err = os.Stat(p)
		return err == nil
	}
	switch {
	case exists("rebase-merge"), exists("rebase-apply"):
		return api.GitRebasing
	case exists("MERGE_HEAD"):
		return api.GitMerging
	}
	return ""
}

// commandKeys are the git settings whose values run commands (or let a
// remote URL do so) when fetching, merging or pushing.
var commandKeys = regexp.MustCompile(`^(core\.(sshcommand|gitproxy|askpass)|credential\..*helper|filter\..*\.(clean|smudge|process)|merge\..*\.driver|remote\..*\.(uploadpack|receivepack)|protocol\.(.*\.)?allow|gpg\.(.*\.)?program)$`)

// checkRepoConfig refuses a repository whose own config (with its
// includes) sets a setting that runs a command.
func (w *Workspace) checkRepoConfig(ctx context.Context) error {
	for _, scope := range []string{"--local", "--worktree"} {
		cmd := exec.CommandContext(ctx, "git", "config", scope, "--includes", "--name-only", "-z", "--list")
		cmd.Dir = w.Dir()
		out, err := cmd.Output()
		if err != nil {
			continue // --worktree without extensions.worktreeConfig, say
		}
		for _, key := range strings.Split(string(out), "\x00") {
			if key = strings.ToLower(strings.TrimSpace(key)); key != "" && commandKeys.MatchString(key) {
				return &RepoConfigError{Key: key}
			}
		}
	}
	return nil
}

// gitRemote runs git for a remote action: fsmonitor off, never prompting,
// no editor (merge and rebase take their messages as they are), within
// remoteTimeout. Its output is stdout and stderr together, git's progress
// and hints included.
func (w *Workspace) gitRemote(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false"}, args...)...)
	cmd.Dir = w.Dir()
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never",
		"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "GIT_MERGE_AUTOEDIT=no",
		"LC_ALL=C", // the messages remoteError reads
	)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		return text, remoteError(err, text)
	}
	return text, nil
}

// remoteError gives a failed remote action's error its kind.
func remoteError(err error, out string) error {
	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, "authentication failed"),
		strings.Contains(lower, "could not read username"),
		strings.Contains(lower, "could not read password"),
		strings.Contains(lower, "permission denied (publickey"),
		strings.Contains(lower, "terminal prompts disabled"):
		return fmt.Errorf("%w (%s)", ErrRemoteAuth, lastLine(out))
	case strings.Contains(lower, "[rejected]") && (strings.Contains(lower, "fetch first") || strings.Contains(lower, "non-fast-forward")):
		return ErrPushRejected
	}
	if out == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, out)
}

// lastLine is the last non-empty line of s.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// remoteStart checks a remote action may run (a repository, whose own
// config runs nothing) and returns the git status it starts from.
func (w *Workspace) remoteStart(ctx context.Context) (api.GitStatus, error) {
	if !w.inRepository(ctx) {
		return api.GitStatus{}, ErrNotARepository
	}
	if err := w.checkRepoConfig(ctx); err != nil {
		return api.GitStatus{}, err
	}
	return w.GitStatus(ctx)
}

// Fetch brings the remote's branches up to date (the upstream's remote,
// else every remote), for the ahead and behind counts.
func (w *Workspace) Fetch(ctx context.Context) (api.GitStatus, error) {
	st, err := w.remoteStart(ctx)
	if err != nil {
		return st, err
	}
	if !st.Remote {
		return st, ErrNoRemote
	}
	args := []string{"fetch", "--prune", "--quiet"}
	if remote, _, ok := strings.Cut(st.Upstream, "/"); ok {
		args = append(args, remote)
	} else {
		args = append(args, "--all")
	}
	if _, err := w.gitRemote(ctx, args...); err != nil {
		return st, fmt.Errorf("git fetch: %w", err)
	}
	return w.GitStatus(ctx)
}

// Pull brings the upstream's commits into the branch as the repository's
// git config says (pull.rebase, pull.ff; a merge when neither is set),
// keeping uncommitted changes (--autostash). A pull that stops on
// conflicts isn't an error: its outcome is PullConflicts, and the merge or
// rebase waits for ResolveConflict and CompleteMerge (or AbortMerge).
func (w *Workspace) Pull(ctx context.Context) (PullResult, error) {
	before, err := w.remoteStart(ctx)
	if err != nil {
		return PullResult{}, err
	}
	switch {
	case before.Operation != "":
		return PullResult{Status: before}, ErrInProgress
	case before.Upstream == "":
		return PullResult{Status: before}, ErrNoUpstream
	}
	head := w.head(ctx)
	args := []string{"pull", "--autostash", "--no-stat"}
	if !w.pullConfigured(ctx, before.Branch) {
		args = append(args, "--no-rebase") // git's default, said: it refuses divergent branches unasked
	}
	_, pullErr := w.gitRemote(ctx, args...)
	after, err := w.GitStatus(ctx)
	if err != nil {
		return PullResult{}, err
	}
	res := PullResult{Status: after, Commits: w.newCommits(ctx, head, before.Upstream)}
	if pullErr != nil {
		if after.Operation != "" && len(after.Conflicts) > 0 {
			res.Outcome = PullConflicts
			return res, nil
		}
		return res, fmt.Errorf("git pull: %w", pullErr)
	}
	switch now := w.head(ctx); {
	case now == head:
		res.Outcome = PullUpToDate
	case w.isMerge(ctx, now) && before.Ahead > 0:
		res.Outcome = PullMerged
	case before.Ahead > 0:
		res.Outcome = PullRebased
	default:
		res.Outcome = PullFastForward
	}
	return res, nil
}

// newCommits counts upstream's commits that head (the branch before a
// pull; "" before its first commit) didn't have: what the pull brought
// in. upstream is named, not @{upstream}, which a rebase's detached HEAD
// doesn't have.
func (w *Workspace) newCommits(ctx context.Context, head, upstream string) int {
	span := upstream
	if head != "" {
		span = head + ".." + upstream
	}
	out, err := w.gitDo(ctx, nil, "rev-list", "--count", span, "--")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// pullConfigured reports whether git config says how branch's pulls
// reconcile (pull.rebase, branch.<name>.rebase or pull.ff).
func (w *Workspace) pullConfigured(ctx context.Context, branch string) bool {
	for _, key := range []string{"pull.rebase", "branch." + branch + ".rebase", "pull.ff"} {
		if _, err := w.gitDo(ctx, nil, "config", "--get", key); err == nil {
			return true
		}
	}
	return false
}

// head is HEAD's commit, or "" before the first.
func (w *Workspace) head(ctx context.Context) string {
	out, err := w.gitDo(ctx, nil, "rev-parse", "-q", "--verify", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// isMerge reports whether commit has more than one parent.
func (w *Workspace) isMerge(ctx context.Context, commit string) bool {
	_, err := w.gitDo(ctx, nil, "rev-parse", "-q", "--verify", commit+"^2")
	return err == nil
}

// Push sends the branch's commits to its upstream; a branch without one is
// published to the remote ("origin", or the only one) under its own name,
// and tracks it from then on. It returns where it pushed, as
// "remote/branch".
func (w *Workspace) Push(ctx context.Context) (string, api.GitStatus, error) {
	st, err := w.remoteStart(ctx)
	if err != nil {
		return "", st, err
	}
	switch {
	case st.Operation != "":
		return "", st, ErrInProgress
	case !st.Remote:
		return "", st, ErrNoRemote
	}
	target := st.Upstream
	args := []string{"push", "--quiet"}
	if target == "" {
		remote, err := w.pushRemote(ctx)
		if err != nil {
			return "", st, err
		}
		args = append(args, "--set-upstream", remote, "HEAD")
		target = remote + "/" + st.Branch
	}
	if _, err := w.gitRemote(ctx, args...); err != nil {
		if errors.Is(err, ErrPushRejected) {
			return "", st, err
		}
		return "", st, fmt.Errorf("git push: %w", err)
	}
	after, err := w.GitStatus(ctx)
	return target, after, err
}

// pushRemote is where a branch without an upstream is published: origin,
// or the only remote there is.
func (w *Workspace) pushRemote(ctx context.Context) (string, error) {
	out, err := w.gitDo(ctx, nil, "remote")
	if err != nil {
		return "", err
	}
	remotes := strings.Fields(string(out))
	if len(remotes) == 1 {
		return remotes[0], nil
	}
	for _, r := range remotes {
		if r == "origin" {
			return r, nil
		}
	}
	return "", fmt.Errorf("the repository has several remotes and none is origin: push the branch once with git push -u <remote>")
}

// ResolveConflict settles a file a pull left in conflict: the branch's
// version, the remote's, or as edited (MarkResolved, refused while
// conflict markers remain). During a rebase git's "ours" is the remote's
// side, so mine and theirs are swapped for it. A side that deleted the
// file deletes it.
func (w *Workspace) ResolveConflict(ctx context.Context, p string, choice ConflictChoice) (api.GitStatus, error) {
	rel, slash, err := userPath(p)
	if err != nil {
		return api.GitStatus{}, err
	}
	if rel == "." || rel == "" {
		return api.GitStatus{}, fmt.Errorf("%w: name a file", ErrBadPath)
	}
	st, err := w.remoteStart(ctx)
	if err != nil {
		return st, err
	}
	if st.Operation == "" {
		return st, ErrNoOperation
	}
	conflicted := false
	for _, c := range st.Conflicts {
		conflicted = conflicted || c == slash
	}
	if !conflicted {
		return st, ErrNotConflicted
	}
	switch choice {
	case MarkResolved:
		data, err := os.ReadFile(filepath.Join(w.Dir(), filepath.FromSlash(slash)))
		if err == nil && hasMarkers(data) {
			return st, ErrMarkersRemain
		}
		if _, err := w.gitRemote(ctx, "--literal-pathspecs", "add", "--all", "--", slash); err != nil {
			return st, err
		}
	case KeepMine, TakeTheirs:
		side := "--ours"
		if (choice == TakeTheirs) != (st.Operation == api.GitRebasing) {
			side = "--theirs"
		}
		// Filters run (an LFS file is checked out as its content, not its
		// pointer): the repository's own config was checked.
		if _, err := w.gitRemote(ctx, "--literal-pathspecs", "checkout", side, "--", slash); err != nil {
			// That side deleted it.
			if _, rmErr := w.gitRemote(ctx, "--literal-pathspecs", "rm", "-q", "--", slash); rmErr != nil {
				return st, err
			}
		} else if _, err := w.gitRemote(ctx, "--literal-pathspecs", "add", "--", slash); err != nil {
			return st, err
		}
	default:
		return st, fmt.Errorf("unknown choice %q", choice)
	}
	w.userEdited(slash, "resolved a conflict")
	return w.GitStatus(ctx)
}

// conflictMarker finds a line git writes into a conflicted file.
var conflictMarker = regexp.MustCompile(`(?m)^(<{7}|={7}|>{7})( |$)`)

// hasMarkers reports whether data still has conflict markers.
func hasMarkers(data []byte) bool {
	return conflictMarker.Match(data)
}

// CompleteMerge finishes the merge or rebase a pull started once no file
// is in conflict: the merge commit with git's message, or the rebase's
// next commits (which may stop on conflicts of their own: the status
// says so).
func (w *Workspace) CompleteMerge(ctx context.Context) (api.GitStatus, error) {
	st, err := w.remoteStart(ctx)
	if err != nil {
		return st, err
	}
	switch {
	case st.Operation == "":
		return st, ErrNoOperation
	case len(st.Conflicts) > 0:
		return st, ErrConflictsRemain
	}
	var runErr error
	if st.Operation == api.GitRebasing {
		_, runErr = w.gitRemote(ctx, "rebase", "--continue")
	} else {
		_, runErr = w.gitRemote(ctx, "commit", "--no-edit")
	}
	after, err := w.GitStatus(ctx)
	if err != nil {
		return after, err
	}
	if runErr != nil && !(after.Operation == api.GitRebasing && len(after.Conflicts) > 0) {
		return after, fmt.Errorf("git %s: %w", st.Operation, runErr)
	}
	return after, nil
}

// AbortMerge gives up the merge or rebase a pull started, putting the
// branch and the uncommitted changes back as they were.
func (w *Workspace) AbortMerge(ctx context.Context) (api.GitStatus, error) {
	st, err := w.remoteStart(ctx)
	if err != nil {
		return st, err
	}
	if st.Operation == "" {
		return st, ErrNoOperation
	}
	if _, err := w.gitRemote(ctx, string(st.Operation), "--abort"); err != nil {
		return st, fmt.Errorf("git %s --abort: %w", st.Operation, err)
	}
	return w.GitStatus(ctx)
}
