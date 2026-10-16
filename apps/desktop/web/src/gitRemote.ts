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

// The workspace and its remote, in words: what a pull did, what an error
// means, and what the agent is asked when it resolves conflicts
// (spec_desktop_024 DSK-90b).

import { message, reason } from "./errors";
import { t, tn } from "./i18n";
import { GitOperation, PullOutcome, type GetGitStatusResponse, type PullResponse } from "./gen/blitz/v1/workspace_pb";

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

/** A remote action's error in the user's words: the service's reason, else its message. */
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
  }
  return message(e);
}

/** Whether a merge or rebase is in progress. */
export function inProgress(git: Pick<GetGitStatusResponse, "operation"> | undefined): boolean {
  return !!git && git.operation !== GitOperation.UNSPECIFIED;
}

/**
 * What the agent is asked to do with a pull's conflicts: settle each file
 * and stage it, leaving the merge (or rebase) for the user to complete.
 */
export function resolvePrompt(git: Pick<GetGitStatusResponse, "operation" | "upstream" | "conflicts">): string {
  const files = git.conflicts.map((f) => `- ${f}`).join("\n");
  const key = git.operation === GitOperation.REBASE ? "desktop.git.resolve_prompt_rebase" : "desktop.git.resolve_prompt";
  return t(key, { upstream: git.upstream, files });
}

/** The ahead and behind counts as a tooltip ("" when in step). */
export function syncTitle(git: Pick<GetGitStatusResponse, "upstream" | "ahead" | "behind">): string {
  if (!git.upstream) return t("desktop.git.no_upstream");
  const parts = [];
  if (git.behind > 0) parts.push(tn("desktop.git.behind", git.behind));
  if (git.ahead > 0) parts.push(tn("desktop.git.ahead", git.ahead));
  return parts.length ? `${git.upstream}: ${parts.join(", ")}` : t("desktop.git.in_step", { upstream: git.upstream });
}
