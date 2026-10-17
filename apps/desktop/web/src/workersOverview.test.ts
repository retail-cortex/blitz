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
import { workspaceFlag } from "./workersOverview";

describe("workspaceFlag", () => {
  const listed = [{ dir: "/w/shop" }, { dir: "/w/blog" }];
  it.each([
    ["one of the app's", { workspace: "/w/shop", missing: false }, "listed"],
    ["not one of the app's", { workspace: "/w/notes", missing: false }, "unlisted"],
    ["a gone folder, listed or not", { workspace: "/w/blog", missing: true }, "missing"],
  ] as const)("%s", (_, g, want) => {
    expect(workspaceFlag(g, listed)).toBe(want);
  });
});
