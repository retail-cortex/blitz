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
import { describe, expect, it } from "vitest";
import { WorkerPolicySettingsSchema } from "./gen/blitz/v1/worker_pb";
import { limitFields, toggleKind, workspaceFlag } from "./workersOverview";

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

describe("toggleKind", () => {
  it.each([
    ["turns one off", ["shell", "write", "web"], "write", "shell, web"],
    ["turns one on, in order", ["web", "shell"], "write", "shell, write, web"],
    ["the last one off: none", ["mcp"], "mcp", "none"],
    ["from none", [], "delete", "delete"],
  ])("%s", (_, allow, kind, want) => {
    expect(toggleKind(allow, kind)).toBe(want);
  });
});

describe("limitFields", () => {
  it("shows the numbers set, and nothing for those that aren't", () => {
    const p = create(WorkerPolicySettingsSchema, { maxConcurrent: 2, defaultMaxTurns: 50, maxCostUsd: 12.5, defaultTimeout: "30m" });
    expect(limitFields(p)).toEqual({ max_concurrent: "2", default_max_turns: "50", max_turns: "", default_max_cost_usd: "", max_cost_usd: "12.5", default_timeout: "30m", max_timeout: "" });
  });
});
