/**
 * Copyright 2026 Retail Cortex
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// The workspace's branches and remote, in words: what a pull or a merge
// did, what an error means, and what the agent is asked when it resolves
// conflicts (spec_desktop_024 DSK-90b, DSK-90c).

import { message, reason } from "./errors";
import { t, tn } from "./i18n";
import { GitOperation, PullOutcome, type GetGitStatusResponse, type MergeBranchResponse, type PullResponse } from "./gen/blitz/v1/workspace_pb";

/** What a pull did, for the snackbar. */
export function pullMessage(res: Pick<PullResponse, "outcome" | "commits">, upstream: string): string {
  switch (res.outcome) {
    case PullOutcome.UP_TO_DATE:
      return t("desktop.git.pulled_none", { upstream });
    case PullOutcome.MERGED:
      return tn("desktop.git.pulled_merged", res.commits, { upstream });
    case PullOutcome.REBASED:
      return tn("desktop.git.pulled_rebased", res.commits, { upstream });
    case PullOutcome.CONFLICTS:
      return t("desktop.git.pulled_conflicts", { upstream });
    default:
      return tn("desktop.git.pulled", res.commits, { upstream });
  }
}

/** What merging branch from into into did, for the snackbar. */
export function mergeMessage(res: Pick<MergeBranchResponse, "outcome" | "commits">, from: string, into: string): string {
  switch (res.outcome) {
    case PullOutcome.UP_TO_DATE:
      return t("desktop.git.merged_none", { from, into });
    case PullOutcome.MERGED:
      return tn("desktop.git.merged_commit", res.commits, { from, into });
    case PullOutcome.CONFLICTS:
      return t("desktop.git.merged_conflicts", { from });
    default:
      return tn("desktop.git.merged_ff", res.commits, { from, into });
  }
}

/** A remote or branch action's error in the user's words: the service's reason, else its message. */
export function remoteErrorText(e: unknown): string {
  switch (reason(e)) {
    case "REMOTE_AUTH":
      return t("desktop.git.err.auth");
    case "PUSH_REJECTED":
      return t("desktop.git.err.rejected");
    case "NO_UPSTREAM":
      return t("desktop.git.err.no_upstream");
    case "GIT_BUSY":
      return t("desktop.git.err.busy");
    case "MARKERS_REMAIN":
      return t("desktop.git.err.markers");
    case "CONFLICTS_REMAIN":
      return t("desktop.git.err.conflicts");
    case "NO_BRANCH":
      return t("desktop.git.err.no_branch");
    case "BRANCH_EXISTS":
      return t("desktop.git.err.branch_exists");
    case "BAD_BRANCH_NAME":
      return t("desktop.git.err.bad_name");
    case "LOCAL_CHANGES":
      return t("desktop.git.err.local_changes");
    case "MERGE_SELF":
      return t("desktop.git.err.merge_self");
  }
  return message(e);
}

/** Whether a merge or rebase is in progress. */
export function inProgress(git: Pick<GetGitStatusResponse, "operation"> | undefined): boolean {
  return !!git && git.operation !== GitOperation.UNSPECIFIED;
}

/**
 * Whether the merge in progress is of a branch the user chose (Merge into…),
 * not a pull's merge of the upstream.
 */
export function branchMerge(git: Pick<GetGitStatusResponse, "operation" | "upstream" | "merging">): boolean {
  return git.operation === GitOperation.MERGE && git.merging !== "" && git.merging !== git.upstream;
}

/** What a merge or rebase in progress brings in: the merged branch, else the upstream. */
export function mergingFrom(git: Pick<GetGitStatusResponse, "operation" | "upstream" | "merging">): string {
  return git.operation === GitOperation.MERGE && git.merging ? git.merging : git.upstream;
}

/**
 * What the agent is asked to do with a pull's or a merge's conflicts:
 * settle each file and stage it, leaving the merge (or rebase) for the
 * user to complete.
 */
export function resolvePrompt(git: Pick<GetGitStatusResponse, "operation" | "upstream" | "merging" | "branch" | "conflicts">): string {
  const files = git.conflicts.map((f) => `- ${f}`).join("\n");
  if (git.operation === GitOperation.REBASE) return t("desktop.git.resolve_prompt_rebase", { upstream: git.upstream, files });
  if (branchMerge(git)) return t("desktop.git.resolve_prompt_merge", { branch: git.merging, current: git.branch, files });
  return t("desktop.git.resolve_prompt", { upstream: git.upstream, files });
}

/** The ahead and behind counts as a tooltip ("" when in step). */
export function syncTitle(git: Pick<GetGitStatusResponse, "upstream" | "ahead" | "behind">): string {
  if (!git.upstream) return t("desktop.git.no_upstream");
  const parts = [];
  if (git.behind > 0) parts.push(tn("desktop.git.behind", git.behind));
  if (git.ahead > 0) parts.push(tn("desktop.git.ahead", git.ahead));
  return parts.length ? `${git.upstream}: ${parts.join(", ")}` : t("desktop.git.in_step", { upstream: git.upstream });
}
