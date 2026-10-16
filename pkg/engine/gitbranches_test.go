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
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// branchNames is the branches' names, in order.
func branchNames(bs []Branch) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Name)
	}
	return out
}

func TestBranches(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	gitOut(t, other, "switch", "-q", "-c", "theirs")
	commitIn(t, other, "t.txt", "t\n", "their feature", false)
	gitOut(t, other, "push", "-q", "-u", "origin", "theirs")
	gitOut(t, w.Dir(), "branch", "mine")
	_, err := w.Fetch(ctx)
	require.NoError(t, err)

	bs, err := w.Branches(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"main", "mine", "origin/theirs"}, branchNames(bs), "local first; origin/main is main's upstream, so not listed again")
	assert.True(t, bs[0].Current)
	assert.Equal(t, "origin/main", bs[0].Upstream)
	assert.Equal(t, "init", bs[0].Subject)
	assert.False(t, bs[0].Time.IsZero())
	assert.False(t, bs[1].Current)
	assert.True(t, bs[2].Remote)
	assert.Equal(t, "their feature", bs[2].Subject)
}

func TestCreateAndSwitchBranch(t *testing.T) {
	w, _ := remoteRepos(t)
	ctx := context.Background()
	dir := w.Dir()
	write(t, dir, "g.txt", "g, not yet committed\n")

	st, err := w.CreateBranch(ctx, "feature/x")
	require.NoError(t, err)
	assert.Equal(t, "feature/x", st.Branch)
	assert.Equal(t, "g, not yet committed\n", read(t, dir, "g.txt"), "uncommitted changes come along")

	commitIn(t, dir, "g.txt", "g on feature\n", "feature work", false)
	st, err = w.SwitchBranch(ctx, "main")
	require.NoError(t, err)
	assert.Equal(t, "main", st.Branch)
	assert.Equal(t, "g\n", read(t, dir, "g.txt"))

	_, err = w.CreateBranch(ctx, "feature/x")
	assert.ErrorIs(t, err, ErrBranchExists)
}

func TestSwitchToARemoteBranch(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	gitOut(t, other, "switch", "-q", "-c", "theirs")
	commitIn(t, other, "t.txt", "t\n", "their feature", false)
	gitOut(t, other, "push", "-q", "-u", "origin", "theirs")
	_, err := w.Fetch(ctx)
	require.NoError(t, err)

	st, err := w.SwitchBranch(ctx, "origin/theirs")
	require.NoError(t, err)
	assert.Equal(t, "theirs", st.Branch, "a local branch")
	assert.Equal(t, "origin/theirs", st.Upstream, "that tracks the remote's")
	assert.Equal(t, "t\n", read(t, w.Dir(), "t.txt"))
}

func TestSwitchRefusesClashingChanges(t *testing.T) {
	w, _ := remoteRepos(t)
	ctx := context.Background()
	dir := w.Dir()
	_, err := w.CreateBranch(ctx, "other")
	require.NoError(t, err)
	commitIn(t, dir, "f.txt", "other\n", "other's f", false)
	_, err = w.SwitchBranch(ctx, "main")
	require.NoError(t, err)
	write(t, dir, "f.txt", "mine, uncommitted\n")

	st, err := w.SwitchBranch(ctx, "other")
	require.ErrorIs(t, err, ErrLocalChanges)
	assert.Equal(t, "main", st.Branch, "still on main")
	assert.Equal(t, "mine, uncommitted\n", read(t, dir, "f.txt"), "nothing lost")
}

func TestBranchNamesRefused(t *testing.T) {
	w, _ := remoteRepos(t)
	ctx := context.Background()
	tests := []struct {
		name, branch string
		want         error
	}{
		{"empty", "", ErrBadBranchName},
		{"an option", "-f", ErrBadBranchName},
		{"two dots", "a..b", ErrBadBranchName},
		{"a space", "a b", ErrBadBranchName},
		{"unknown", "nope", ErrNoBranch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := w.SwitchBranch(ctx, tt.branch)
			assert.ErrorIs(t, err, tt.want, "switch")
			_, err = w.MergeBranch(ctx, tt.branch)
			assert.ErrorIs(t, err, tt.want, "merge")
			if tt.want == ErrBadBranchName {
				_, err = w.CreateBranch(ctx, tt.branch)
				assert.ErrorIs(t, err, tt.want, "create")
			}
		})
	}
}

func TestMergeBranch(t *testing.T) {
	tests := []struct {
		name string
		// setup leaves main checked out with a branch "feature" to merge.
		setup   func(t *testing.T, dir string)
		want    PullOutcome
		commits int
	}{
		{"fast-forward", func(t *testing.T, dir string) {
			gitOut(t, dir, "switch", "-q", "-c", "feature")
			commitIn(t, dir, "h.txt", "h\n", "feature", false)
			gitOut(t, dir, "switch", "-q", "main")
		}, PullFastForward, 1},
		{"merge commit", func(t *testing.T, dir string) {
			gitOut(t, dir, "switch", "-q", "-c", "feature")
			commitIn(t, dir, "h.txt", "h\n", "feature", false)
			gitOut(t, dir, "switch", "-q", "main")
			commitIn(t, dir, "i.txt", "i\n", "main", false)
		}, PullMerged, 1},
		{"up to date", func(t *testing.T, dir string) {
			gitOut(t, dir, "branch", "feature")
		}, PullUpToDate, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := remoteRepos(t)
			tt.setup(t, w.Dir())
			res, err := w.MergeBranch(context.Background(), "feature")
			require.NoError(t, err)
			assert.Equal(t, tt.want, res.Outcome)
			assert.Equal(t, tt.commits, res.Commits)
			assert.Equal(t, "main", res.Status.Branch)
			assert.Empty(t, res.Status.Operation)
		})
	}
}

func TestMergeBranchConflicts(t *testing.T) {
	w, _ := remoteRepos(t)
	ctx := context.Background()
	dir := w.Dir()
	gitOut(t, dir, "switch", "-q", "-c", "feature")
	commitIn(t, dir, "f.txt", "one\nfeature\nthree\n", "feature", false)
	gitOut(t, dir, "switch", "-q", "main")
	commitIn(t, dir, "f.txt", "one\nmain\nthree\n", "main", false)

	res, err := w.MergeBranch(ctx, "feature")
	require.NoError(t, err, "conflicts aren't an error")
	require.Equal(t, PullConflicts, res.Outcome)
	assert.Equal(t, api.GitMerging, res.Status.Operation)
	assert.Equal(t, []string{"f.txt"}, res.Status.Conflicts)
	assert.Equal(t, "feature", res.Status.Merging, "the banner names the branch, not the upstream")

	_, err = w.SwitchBranch(ctx, "feature")
	require.ErrorIs(t, err, ErrInProgress)
	_, err = w.CreateBranch(ctx, "another")
	require.ErrorIs(t, err, ErrInProgress)

	_, err = w.ResolveConflict(ctx, "f.txt", TakeTheirs)
	require.NoError(t, err)
	st, err := w.CompleteMerge(ctx)
	require.NoError(t, err)
	assert.Empty(t, st.Operation)
	assert.Equal(t, "one\nfeature\nthree\n", read(t, dir, "f.txt"), "theirs is the merged branch's")
	assert.True(t, w.isMerge(ctx, w.head(ctx)))
	assert.Equal(t, "Merge branch 'feature'", strings.TrimSpace(gitOut(t, dir, "log", "-1", "--format=%s")))
}

func TestMergeARemoteBranch(t *testing.T) {
	w, other := remoteRepos(t)
	ctx := context.Background()
	commitIn(t, other, "h.txt", "h\n", "theirs", true)
	_, err := w.Fetch(ctx)
	require.NoError(t, err)

	res, err := w.MergeBranch(ctx, "origin/main")
	require.NoError(t, err)
	assert.Equal(t, PullFastForward, res.Outcome)
	assert.Equal(t, 1, res.Commits)
}

func TestMergeBranchIntoItself(t *testing.T) {
	w, _ := remoteRepos(t)
	_, err := w.MergeBranch(context.Background(), "main")
	assert.ErrorIs(t, err, ErrMergeSelf)
}

func TestBranchActionsOutsideARepository(t *testing.T) {
	needGit(t)
	w := openTest(t)
	ctx := context.Background()
	_, err := w.Branches(ctx)
	assert.ErrorIs(t, err, ErrNotARepository, "list")
	_, err = w.SwitchBranch(ctx, "main")
	assert.ErrorIs(t, err, ErrNotARepository, "switch")
	_, err = w.CreateBranch(ctx, "main")
	assert.ErrorIs(t, err, ErrNotARepository, "create")
	_, err = w.MergeBranch(ctx, "main")
	assert.ErrorIs(t, err, ErrNotARepository, "merge")
}

func TestBranchActionsRefuseRepoConfigCommands(t *testing.T) {
	w, _ := remoteRepos(t)
	ctx := context.Background()
	gitOut(t, w.Dir(), "branch", "feature")
	gitOut(t, w.Dir(), "config", "merge.evil.driver", "touch pwned")

	var rc *RepoConfigError
	_, err := w.SwitchBranch(ctx, "feature")
	require.ErrorAs(t, err, &rc, "switch")
	assert.Equal(t, "merge.evil.driver", rc.Key)
	_, err = w.MergeBranch(ctx, "feature")
	assert.ErrorAs(t, err, &rc, "merge")
	_, err = w.CreateBranch(ctx, "new")
	assert.ErrorAs(t, err, &rc, "create")
}
