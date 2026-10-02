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

import { describe, expect, it } from "vitest";
import { GitAction } from "../gen/blitz/v1/file_pb";
import { gitActions, gitKeys } from "./gitMenu";

const { STAGE, UNSTAGE, DISCARD, IGNORE } = GitAction;

describe("the Files tree's git actions", () => {
  it.each([
    ["a modified file", false, "modified", [STAGE, UNSTAGE, DISCARD, IGNORE]],
    ["a deleted file", false, "deleted", [STAGE, UNSTAGE, DISCARD, IGNORE]],
    ["a renamed file", false, "renamed", [STAGE, UNSTAGE, DISCARD, IGNORE]],
    ["an added file: nothing in the last commit to go back to", false, "added", [STAGE, UNSTAGE, IGNORE]],
    ["an untracked file: nothing staged, nothing to discard", false, "untracked", [STAGE, IGNORE]],
    ["a conflicted file", false, "conflicted", [STAGE, UNSTAGE, IGNORE]],
    ["a clean file", false, "", [IGNORE]],
    ["a folder with changes", true, "changed", [STAGE, UNSTAGE, DISCARD, IGNORE]],
    ["a clean folder", true, "", [IGNORE]],
  ])("%s", (_, folder, git, want) => {
    expect(gitActions({ folder, git }, true)).toEqual(want);
  });

  it("offers nothing outside a repository", () => {
    expect(gitActions({ folder: false, git: "modified" }, false)).toEqual([]);
  });

  it("has a label and a confirmation for every action it offers", () => {
    for (const a of [STAGE, UNSTAGE, DISCARD, IGNORE]) {
      expect(gitKeys[a].item).toMatch(/^desktop\.files\.git\./);
      expect(gitKeys[a].done).toMatch(/^desktop\.files\.git\./);
    }
  });
});
