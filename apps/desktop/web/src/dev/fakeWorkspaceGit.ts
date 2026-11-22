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

import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { ServiceImpl } from "@connectrpc/connect";
import { ConflictChoice, GitOperation, PullOutcome, type WorkspaceService } from "../gen/blitz/v1/workspace_pb";
import { fakeError, now, sleep } from "./fakeHelpers";
import { fakeGitState, fakeGitStatus } from "./fakeWorkspaceFiles";

type GitMethods = Pick<
  ServiceImpl<typeof WorkspaceService>,
  | "listCheckpoints"
  | "undo"
  | "getGitStatus"
  | "fetchRemote"
  | "pull"
  | "push"
  | "resolveConflict"
  | "completeMerge"
  | "abortMerge"
  | "listBranches"
  | "switchBranch"
  | "createBranch"
  | "mergeBranch"
  | "reloadAgentFiles"
  | "draftCommit"
  | "commit"
  | "installSecretHook"
  | "initGitRepo"
  | "getDiff"
>;

/** Git, branch, merge, commit, checkpoint, and diff RPC handlers for WorkspaceService. */
export function fakeWorkspaceGitHandlers(): GitMethods {
  return {
    listCheckpoints: () => ({ checkpoints: [{ id: 2, label: "Fix the coupon rounding", time: now(), files: ["internal/cart/discount.go", "internal/cart/discount_test.go"] }] }),
    undo: () => ({ label: "Fix the coupon rounding", restored: ["internal/cart/discount.go"] }),
    getGitStatus: () => fakeGitStatus(),
    fetchRemote: async () => {
      await sleep(300);
      return { status: fakeGitStatus() };
    },
    pull: async () => {
      await sleep(700);
      const commits = fakeGitState.remote.behind;
      fakeGitState.remote.behind = 0;
      fakeGitState.remote.operation = GitOperation.MERGE;
      fakeGitState.remote.conflicts = ["internal/cart/discount.go"];
      fakeGitState.remote.merging = fakeGitState.remote.upstream;
      return { outcome: PullOutcome.CONFLICTS, commits, status: fakeGitStatus() };
    },
    push: async () => {
      await sleep(600);
      if (fakeGitState.remote.behind > 0) throw fakeError("the remote has commits this branch doesn't have: pull them first", "PUSH_REJECTED");
      const target = fakeGitState.remote.upstream || "origin/fix/coupon-rounding";
      fakeGitState.remote.upstream = target;
      fakeGitState.remote.ahead = 0;
      return { target, status: fakeGitStatus() };
    },
    resolveConflict: ({ path, choice }) => {
      if (choice === ConflictChoice.RESOLVED) throw fakeError("the file still has conflict markers", "MARKERS_REMAIN");
      fakeGitState.remote.conflicts = fakeGitState.remote.conflicts.filter((c) => c !== path);
      return { status: fakeGitStatus() };
    },
    completeMerge: async () => {
      await sleep(400);
      fakeGitState.remote.operation = GitOperation.UNSPECIFIED;
      fakeGitState.remote.merging = "";
      fakeGitState.remote.ahead += 1;
      return { status: fakeGitStatus() };
    },
    abortMerge: () => {
      fakeGitState.remote.operation = GitOperation.UNSPECIFIED;
      fakeGitState.remote.conflicts = [];
      fakeGitState.remote.merging = "";
      fakeGitState.remote.behind = 2;
      return { status: fakeGitStatus() };
    },
    listBranches: async () => {
      await sleep(200);
      return { branches: fakeGitState.branches.map((b, i) => ({ ...b, remote: !!b.remote, current: b.name === fakeGitState.branch, time: timestampFromDate(new Date(Date.now() - i * 86_400_000)) })) };
    },
    switchBranch: async ({ branch }) => {
      await sleep(300);
      if (fakeGitState.remote.operation !== GitOperation.UNSPECIFIED) throw fakeError("a merge or rebase is in progress: complete or abort it first", "GIT_BUSY");
      if (!fakeGitState.branches.some((b) => b.name === branch)) throw fakeError("there's no such branch", "NO_BRANCH");
      const local = branch.replace(/^origin\//, "");
      if (local !== branch && !fakeGitState.branches.some((b) => b.name === local)) fakeGitState.branches.push({ name: local, subject: fakeGitState.branches.find((b) => b.name === branch)!.subject, upstream: branch });
      fakeGitState.branch = local;
      return { status: fakeGitStatus() };
    },
    createBranch: async ({ branch }) => {
      await sleep(300);
      if (!branch || /\s|\.\.|^-/.test(branch)) throw fakeError("that isn't a valid branch name", "BAD_BRANCH_NAME");
      if (fakeGitState.branches.some((b) => b.name === branch)) throw fakeError("a branch with that name already exists", "BRANCH_EXISTS");
      fakeGitState.branches.unshift({ name: branch, subject: fakeGitState.branches.find((b) => b.name === fakeGitState.branch)?.subject ?? "", upstream: "" });
      fakeGitState.branch = branch;
      return { status: fakeGitStatus() };
    },
    mergeBranch: async ({ branch }) => {
      await sleep(500);
      if (branch === fakeGitState.branch) throw fakeError("that's the current branch", "MERGE_SELF");
      if (branch === "spike/lazy-totals") {
        fakeGitState.remote.operation = GitOperation.MERGE;
        fakeGitState.remote.conflicts = ["internal/cart/totals.go"];
        fakeGitState.remote.merging = branch;
        return { outcome: PullOutcome.CONFLICTS, commits: 3, status: fakeGitStatus() };
      }
      fakeGitState.remote.ahead += 2;
      return { outcome: PullOutcome.FAST_FORWARD, commits: 2, status: fakeGitStatus() };
    },
    reloadAgentFiles: async () => {
      await sleep(300);
      return { agents: 2, skills: 3, files: ["AGENTS.md", ".agents/rules/style.md"], problems: [] };
    },
    draftCommit: async ({ stageAll }) => {
      if (stageAll) fakeGitState.staged = true;
      if (!fakeGitState.staged) return { files: [], message: "", problem: "" };
      await sleep(900);
      return {
        files: [
          { status: "M", path: "internal/cart/discount.go" },
          { status: "A", path: "internal/cart/discount_test.go" },
        ],
        message: "Fix coupon rounding: round the total, not each line\n\nApplyCoupon rounded every line item before summing, so a cart could\nbe off by a cent. Sum first, then apply the discount and round once.\n\n- Add TestApplyCouponRounding for the 333/333/334 cart",
        problem: "",
      };
    },
    commit: async ({ message }) => {
      await sleep(500);
      fakeGitState.staged = false;
      return { hash: "3f2a9c1", subject: message.split("\n")[0] };
    },
    installSecretHook: ({ workspace }) => ({ status: "installed", path: `${workspace}/.githooks/pre-commit`, detail: "" }),
    initGitRepo: () => {
      fakeGitState.repo = true;
      return { status: { git: true, repo: true, branch: "main", changed: 0 } };
    },
    getDiff: ({ git }) => ({
      diff: git
        ? fakeGitState.remote.conflicts.length
          ? "--- a/internal/cart/discount.go\n+++ b/internal/cart/discount.go\n@@ -40,7 +40,13 @@ func ApplyCoupon(items []Item, c Coupon) int {\n \ttotal := 0\n \tfor _, it := range items {\n+<<<<<<< HEAD\n \t\ttotal += it.Cents\n+=======\n+\t\ttotal += it.Cents * it.Qty\n+>>>>>>> origin/fix/coupon-rounding\n \t}\n"
          : ""
        : "--- a/internal/cart/discount.go\n+++ b/internal/cart/discount.go\n@@ -40,9 +40,9 @@ func ApplyCoupon(items []Item, c Coupon) int {\n \ttotal := 0\n \tfor _, it := range items {\n-\t\ttotal += round(it.Cents * (100 - c.Percent) / 100)\n+\t\ttotal += it.Cents\n \t}\n-\treturn total\n+\treturn round(total * (100 - c.Percent) / 100)\n }\n--- /dev/null\n+++ b/internal/cart/discount_test.go\n@@ -0,0 +1,6 @@\n+package cart\n+\n+import \"testing\"\n+\n+func TestApplyCouponRounding(t *testing.T) {\n+}\n",
    }),
  };
}
