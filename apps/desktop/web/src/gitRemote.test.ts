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

import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import { branchMerge, inProgress, mergeMessage, mergingFrom, pullMessage, remoteErrorText, resolvePrompt, syncTitle } from "./gitRemote";
import { GitOperation, PullOutcome } from "./gen/blitz/v1/workspace_pb";
import { ErrorInfoSchema } from "./gen/blitz/v1/turn_pb";

const withReason = (why: string) => new ConnectError("the service's words", Code.FailedPrecondition, undefined, [{ desc: ErrorInfoSchema, value: create(ErrorInfoSchema, { reason: why }) }]);

describe("pullMessage", () => {
  it.each([
    [PullOutcome.UP_TO_DATE, 0, "Already up to date with origin/main."],
    [PullOutcome.FAST_FORWARD, 1, "Pulled 1 commit from origin/main."],
    [PullOutcome.FAST_FORWARD, 3, "Pulled 3 commits from origin/main."],
    [PullOutcome.MERGED, 2, "Pulled 2 commits from origin/main and merged them with yours."],
    [PullOutcome.REBASED, 1, "Pulled 1 commit from origin/main and replayed yours on top."],
    [PullOutcome.CONFLICTS, 2, "The pull from origin/main stopped on conflicts: settle each file, then complete it."],
  ])("%s, %d commits", (outcome, commits, want) => {
    expect(pullMessage({ outcome, commits }, "origin/main")).toBe(want);
  });
});

describe("remoteErrorText", () => {
  it.each([
    ["REMOTE_AUTH", /sign in/],
    ["PUSH_REJECTED", /pull them first/],
    ["NO_UPSTREAM", /publish it first/],
    ["GIT_BUSY", /complete or abort it first/],
    ["MARKERS_REMAIN", /conflict markers/],
    ["CONFLICTS_REMAIN", /still have conflicts/],
    ["NO_BRANCH", /doesn't exist/],
    ["BRANCH_EXISTS", /already exists/],
    ["BAD_BRANCH_NAME", /branch name isn't allowed/],
    ["LOCAL_CHANGES", /commit them first/],
    ["MERGE_SELF", /the branch you're on/],
    ["REPO_CONFIG_COMMAND", /the service's words/],
  ])("%s", (why, want) => {
    expect(remoteErrorText(withReason(why))).toMatch(want);
  });
  it("an error without a reason says its message", () => {
    expect(remoteErrorText(new Error("offline"))).toBe("offline");
  });
});

describe("inProgress", () => {
  it.each([
    [GitOperation.UNSPECIFIED, false],
    [GitOperation.MERGE, true],
    [GitOperation.REBASE, true],
  ])("%s", (operation, want) => {
    expect(inProgress({ operation })).toBe(want);
  });
  it("no status yet", () => {
    expect(inProgress(undefined)).toBe(false);
  });
});

describe("resolvePrompt", () => {
  it("names the files and leaves completing the merge to the user", () => {
    const p = resolvePrompt({ operation: GitOperation.MERGE, upstream: "origin/main", merging: "origin/main", branch: "main", conflicts: ["a.go", "b/c.md"] });
    expect(p).toContain("Pulling origin/main");
    expect(p).toContain("- a.go\n- b/c.md");
    expect(p).toMatch(/Don't commit or complete the merge/);
  });
  it("explains ours and theirs for a rebase", () => {
    const p = resolvePrompt({ operation: GitOperation.REBASE, upstream: "origin/main", merging: "", branch: "main", conflicts: ["a.go"] });
    expect(p).toContain(`"ours" is origin/main`);
    expect(p).toMatch(/Don't run git rebase --continue/);
  });
  it("names both branches for a branch's merge", () => {
    const p = resolvePrompt({ operation: GitOperation.MERGE, upstream: "origin/main", merging: "feature", branch: "main", conflicts: ["a.go"] });
    expect(p).toContain("Merging feature into main");
    expect(p).toMatch(/Don't commit or complete the merge/);
  });
});

describe("mergeMessage", () => {
  it.each([
    [PullOutcome.UP_TO_DATE, 0, "main already has everything in feature."],
    [PullOutcome.FAST_FORWARD, 1, "Brought 1 commit from feature into main."],
    [PullOutcome.FAST_FORWARD, 3, "Brought 3 commits from feature into main."],
    [PullOutcome.MERGED, 2, "Merged 2 commits from feature into main."],
    [PullOutcome.CONFLICTS, 2, "Merging feature stopped on conflicts: settle each file, then complete the merge."],
  ])("%s, %d commits", (outcome, commits, want) => {
    expect(mergeMessage({ outcome, commits }, "feature", "main")).toBe(want);
  });
});

describe("branchMerge and mergingFrom", () => {
  it.each([
    ["a pull's merge", GitOperation.MERGE, "origin/main", false, "origin/main"],
    ["a branch's merge", GitOperation.MERGE, "feature", true, "feature"],
    ["a merge git can't name", GitOperation.MERGE, "", false, "origin/main"],
    ["a rebase", GitOperation.REBASE, "", false, "origin/main"],
  ])("%s", (_, operation, merging, branch, from) => {
    const git = { operation, upstream: "origin/main", merging };
    expect(branchMerge(git)).toBe(branch);
    expect(mergingFrom(git)).toBe(from);
  });
});

describe("syncTitle", () => {
  it.each([
    [{ upstream: "", ahead: 0, behind: 0 }, /doesn't track a remote branch yet/],
    [{ upstream: "origin/main", ahead: 0, behind: 0 }, /^In step with origin\/main\.$/],
    [{ upstream: "origin/main", ahead: 1, behind: 2 }, /^origin\/main: 2 commits to pull, 1 commit to push$/],
    [{ upstream: "origin/main", ahead: 3, behind: 0 }, /^origin\/main: 3 commits to push$/],
  ])("%j", (git, want) => {
    expect(syncTitle(git)).toMatch(want);
  });
});
