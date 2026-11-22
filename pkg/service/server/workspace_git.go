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

package server

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/secretscan"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h workspaceService) GetGitStatus(ctx context.Context, r req[pb.GetGitStatusRequest]) (*connect.Response[pb.GetGitStatusResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	st, err := w.GitStatus(ctx)
	if err != nil {
		return nil, toAPI(err)
	}
	return ok(gitStatusMsg(st))
}

func (h workspaceService) InitGitRepo(ctx context.Context, r req[pb.InitGitRepoRequest]) (*connect.Response[pb.InitGitRepoResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	st, err := w.GitInit(ctx)
	switch {
	case errors.Is(err, engine.ErrNoGit):
		return nil, apiError(connect.CodeFailedPrecondition, "NO_GIT", err)
	case err != nil:
		return nil, apiError(connect.CodeFailedPrecondition, "GIT_FAILED", err)
	}
	return ok(&pb.InitGitRepoResponse{Status: gitStatusMsg(st)})
}

func (h workspaceService) InstallSecretHook(ctx context.Context, r req[pb.InstallSecretHookRequest]) (*connect.Response[pb.InstallSecretHookResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.InstallSecretHook(ctx)
	switch {
	case errors.Is(err, secretscan.ErrNotARepository):
		return nil, apiError(connect.CodeFailedPrecondition, "NOT_A_REPOSITORY", err)
	case err != nil:
		return nil, err
	}
	return ok(&pb.InstallSecretHookResponse{Status: res.Status, Path: res.Path, Detail: res.Detail})
}

func (h workspaceService) DraftCommit(ctx context.Context, r req[pb.DraftCommitRequest]) (*connect.Response[pb.DraftCommitResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	d, err := w.DraftCommit(ctx, r.Msg.StageAll)
	if err != nil {
		return nil, commitError(err)
	}
	out := &pb.DraftCommitResponse{Message: d.Message, Problem: d.Problem}
	for _, f := range d.Files {
		out.Files = append(out.Files, &pb.StagedFile{Status: f.Status, Path: f.Path})
	}
	return ok(out)
}

func (h workspaceService) Commit(ctx context.Context, r req[pb.CommitRequest]) (*connect.Response[pb.CommitResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	hash, subject, err := w.Commit(ctx, r.Msg.Message)
	if err != nil {
		return nil, commitError(err)
	}
	return ok(&pb.CommitResponse{Hash: hash, Subject: subject})
}

// commitError gives DraftCommit's and Commit's errors their codes.
func commitError(err error) error {
	switch {
	case errors.Is(err, engine.ErrNotARepository):
		return apiError(connect.CodeFailedPrecondition, "NOT_A_REPOSITORY", err)
	case errors.Is(err, engine.ErrNothingStaged):
		return apiError(connect.CodeFailedPrecondition, "NOTHING_STAGED", err)
	case errors.Is(err, engine.ErrEmptyMessage):
		return apiError(connect.CodeInvalidArgument, "EMPTY_MESSAGE", err)
	default:
		return apiError(connect.CodeFailedPrecondition, "GIT_FAILED", err)
	}
}

func gitStatusMsg(st api.GitStatus) *pb.GetGitStatusResponse {
	out := &pb.GetGitStatusResponse{
		Git: st.Git, Repo: st.Repo, Branch: st.Branch, Changed: int32(st.Changed),
		Remote: st.Remote, Upstream: st.Upstream, Ahead: int32(st.Ahead), Behind: int32(st.Behind),
		Conflicts: st.Conflicts, Merging: st.Merging,
	}
	switch st.Operation {
	case api.GitMerging:
		out.Operation = pb.GitOperation_GIT_OPERATION_MERGE
	case api.GitRebasing:
		out.Operation = pb.GitOperation_GIT_OPERATION_REBASE
	}
	return out
}

func (h workspaceService) FetchRemote(ctx context.Context, r req[pb.FetchRemoteRequest]) (*connect.Response[pb.FetchRemoteResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	st, err := w.Fetch(ctx)
	if err != nil {
		return nil, remoteError(err)
	}
	return ok(&pb.FetchRemoteResponse{Status: gitStatusMsg(st)})
}

// pullOutcomes are the engine's pull outcomes as the API's.
var pullOutcomes = map[engine.PullOutcome]pb.PullOutcome{
	engine.PullUpToDate:    pb.PullOutcome_PULL_OUTCOME_UP_TO_DATE,
	engine.PullFastForward: pb.PullOutcome_PULL_OUTCOME_FAST_FORWARD,
	engine.PullMerged:      pb.PullOutcome_PULL_OUTCOME_MERGED,
	engine.PullRebased:     pb.PullOutcome_PULL_OUTCOME_REBASED,
	engine.PullConflicts:   pb.PullOutcome_PULL_OUTCOME_CONFLICTS,
}

func (h workspaceService) Pull(ctx context.Context, r req[pb.PullRequest]) (*connect.Response[pb.PullResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.Pull(ctx)
	if err != nil {
		return nil, remoteError(err)
	}
	return ok(&pb.PullResponse{Outcome: pullOutcomes[res.Outcome], Commits: int32(res.Commits), Status: gitStatusMsg(res.Status)})
}

func (h workspaceService) Push(ctx context.Context, r req[pb.PushRequest]) (*connect.Response[pb.PushResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	target, st, err := w.Push(ctx)
	if err != nil {
		return nil, remoteError(err)
	}
	return ok(&pb.PushResponse{Target: target, Status: gitStatusMsg(st)})
}

// conflictChoices are the API's conflict choices as the engine's.
var conflictChoices = map[pb.ConflictChoice]engine.ConflictChoice{
	pb.ConflictChoice_CONFLICT_CHOICE_MINE:     engine.KeepMine,
	pb.ConflictChoice_CONFLICT_CHOICE_THEIRS:   engine.TakeTheirs,
	pb.ConflictChoice_CONFLICT_CHOICE_RESOLVED: engine.MarkResolved,
}

func (h workspaceService) ResolveConflict(ctx context.Context, r req[pb.ResolveConflictRequest]) (*connect.Response[pb.ResolveConflictResponse], error) {
	choice, known := conflictChoices[r.Msg.Choice]
	if !known {
		return nil, apiError(connect.CodeInvalidArgument, "INVALID_CHOICE", errors.New("choice is required"))
	}
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	st, err := w.ResolveConflict(ctx, r.Msg.Path, choice)
	if err != nil {
		return nil, remoteError(err)
	}
	return ok(&pb.ResolveConflictResponse{Status: gitStatusMsg(st)})
}

func (h workspaceService) CompleteMerge(ctx context.Context, r req[pb.CompleteMergeRequest]) (*connect.Response[pb.CompleteMergeResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	st, err := w.CompleteMerge(ctx)
	if err != nil {
		return nil, remoteError(err)
	}
	return ok(&pb.CompleteMergeResponse{Status: gitStatusMsg(st)})
}

func (h workspaceService) AbortMerge(ctx context.Context, r req[pb.AbortMergeRequest]) (*connect.Response[pb.AbortMergeResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	st, err := w.AbortMerge(ctx)
	if err != nil {
		return nil, remoteError(err)
	}
	return ok(&pb.AbortMergeResponse{Status: gitStatusMsg(st)})
}

func (h workspaceService) ListBranches(ctx context.Context, r req[pb.ListBranchesRequest]) (*connect.Response[pb.ListBranchesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	bs, err := w.Branches(ctx)
	if err != nil {
		return nil, remoteError(err)
	}
	out := &pb.ListBranchesResponse{}
	for _, b := range bs {
		msg := &pb.Branch{Name: b.Name, Remote: b.Remote, Current: b.Current, Upstream: b.Upstream, Subject: b.Subject}
		if !b.Time.IsZero() {
			msg.Time = timestamppb.New(b.Time)
		}
		out.Branches = append(out.Branches, msg)
	}
	return ok(out)
}

func (h workspaceService) SwitchBranch(ctx context.Context, r req[pb.SwitchBranchRequest]) (*connect.Response[pb.SwitchBranchResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	st, err := w.SwitchBranch(ctx, r.Msg.Branch)
	if err != nil {
		return nil, remoteError(err)
	}
	return ok(&pb.SwitchBranchResponse{Status: gitStatusMsg(st)})
}

func (h workspaceService) CreateBranch(ctx context.Context, r req[pb.CreateBranchRequest]) (*connect.Response[pb.CreateBranchResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	st, err := w.CreateBranch(ctx, r.Msg.Branch)
	if err != nil {
		return nil, remoteError(err)
	}
	return ok(&pb.CreateBranchResponse{Status: gitStatusMsg(st)})
}

func (h workspaceService) MergeBranch(ctx context.Context, r req[pb.MergeBranchRequest]) (*connect.Response[pb.MergeBranchResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	res, err := w.MergeBranch(ctx, r.Msg.Branch)
	if err != nil {
		return nil, remoteError(err)
	}
	return ok(&pb.MergeBranchResponse{Outcome: pullOutcomes[res.Outcome], Commits: int32(res.Commits), Status: gitStatusMsg(res.Status)})
}

// remoteError gives the remote and branch actions' errors their codes.
func remoteError(err error) error {
	var rc *engine.RepoConfigError
	switch {
	case errors.As(err, &rc):
		return apiError(connect.CodeFailedPrecondition, "REPO_CONFIG_COMMAND", err, "key", rc.Key)
	case errors.Is(err, engine.ErrNotARepository):
		return apiError(connect.CodeFailedPrecondition, "NOT_A_REPOSITORY", err)
	case errors.Is(err, engine.ErrNoRemote):
		return apiError(connect.CodeFailedPrecondition, "NO_REMOTE", err)
	case errors.Is(err, engine.ErrNoUpstream):
		return apiError(connect.CodeFailedPrecondition, "NO_UPSTREAM", err)
	case errors.Is(err, engine.ErrInProgress):
		return apiError(connect.CodeFailedPrecondition, "GIT_BUSY", err)
	case errors.Is(err, engine.ErrPushRejected):
		return apiError(connect.CodeFailedPrecondition, "PUSH_REJECTED", err)
	case errors.Is(err, engine.ErrRemoteAuth):
		return apiError(connect.CodeUnauthenticated, "REMOTE_AUTH", err)
	case errors.Is(err, engine.ErrNoOperation):
		return apiError(connect.CodeFailedPrecondition, "NO_MERGE", err)
	case errors.Is(err, engine.ErrConflictsRemain):
		return apiError(connect.CodeFailedPrecondition, "CONFLICTS_REMAIN", err)
	case errors.Is(err, engine.ErrMarkersRemain):
		return apiError(connect.CodeFailedPrecondition, "MARKERS_REMAIN", err)
	case errors.Is(err, engine.ErrNotConflicted):
		return apiError(connect.CodeFailedPrecondition, "NOT_CONFLICTED", err)
	case errors.Is(err, engine.ErrBadPath):
		return apiError(connect.CodeInvalidArgument, "BAD_PATH", err)
	case errors.Is(err, engine.ErrNoBranch):
		return apiError(connect.CodeNotFound, "NO_BRANCH", err)
	case errors.Is(err, engine.ErrBranchExists):
		return apiError(connect.CodeAlreadyExists, "BRANCH_EXISTS", err)
	case errors.Is(err, engine.ErrBadBranchName):
		return apiError(connect.CodeInvalidArgument, "BAD_BRANCH_NAME", err)
	case errors.Is(err, engine.ErrLocalChanges):
		return apiError(connect.CodeFailedPrecondition, "LOCAL_CHANGES", err)
	case errors.Is(err, engine.ErrMergeSelf):
		return apiError(connect.CodeInvalidArgument, "MERGE_SELF", err)
	default:
		return apiError(connect.CodeFailedPrecondition, "GIT_FAILED", err)
	}
}
