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
import { UsageGroupSchema } from "./gen/blitz/v1/session_pb";
import { UsageSchema } from "./gen/blitz/v1/turn_pb";
import { contextShare, costByModel, costText, dollars, forgetStatus, publishStatus, statusOf, tokens } from "./status";

// The session's cost: a call with no price makes only its own cost unknown
// (spec_056 CA-121), and the hover breaks it down by model (CA-125).
describe("cost", () => {
  it.each([
    ["all priced", { calls: 3, costUsd: 0.0123, priced: true }, "$0.0123"],
    ["dollars and up", { calls: 3, costUsd: 12.5, priced: true }, "$12.50"],
    ["some calls with no price", { calls: 5, costUsd: 0.0123, unpriced: 2 }, "$0.0123 + 2 not priced"],
    ["no call priced", { calls: 2, unpriced: 2 }, "not priced"],
    ["a session from before the ledger", { calls: 2, costUsd: 0.5 }, "not priced"],
  ] as const)("%s", (_, u, want) => {
    expect(costText(create(UsageSchema, u))).toBe(want);
  });

  it("is broken down by model when more than one answered", () => {
    const group = (name: string, u: Parameters<typeof create<typeof UsageSchema>>[1]) => create(UsageGroupSchema, { name, usage: create(UsageSchema, u) });
    expect(costByModel([group("gemini-3-pro", { calls: 4, costUsd: 0.2 })])).toEqual([]);
    expect(costByModel(undefined)).toEqual([]);
    expect(costByModel([group("claude-opus-5-5", { calls: 2, costUsd: 1.5, priced: true }), group("search", { calls: 0, searchQueries: 2, searchCostUsd: 0.07 }), group("llama-local", { calls: 3, unpriced: 3 })])).toEqual([
      "By model:",
      "claude-opus-5-5: $1.50 (2 calls)",
      "web searches: 2 queries, $0.0700",
      "llama-local: not priced (3 calls)",
    ]);
    expect(dollars(0.5)).toBe("$0.5000");
  });
});

describe("tokens", () => {
  it.each([
    [0, "0"],
    [950, "950"],
    [12_345, "12.3k"],
    [1_250_000n, "1.3M"],
  ])("%s is %s", (n, want) => expect(tokens(n)).toBe(want));
});

describe("contextShare", () => {
  it.each([
    { name: "no threshold", prompt: 1000, threshold: undefined, want: undefined },
    { name: "no prompt yet", prompt: 0, threshold: 100_000, want: undefined },
    { name: "a quarter", prompt: 25_000n, threshold: 100_000, want: 0.25 },
    { name: "past it", prompt: 150_000, threshold: 100_000, want: 1 },
  ])("$name", ({ prompt, threshold, want }) => expect(contextShare(prompt, threshold)).toBe(want));
});

describe("publishStatus", () => {
  it("merges, and forgets closed workspaces", () => {
    publishStatus("/w", { threshold: 10 });
    const before = statusOf("/w");
    publishStatus("/w", { autoCompact: true });
    expect(statusOf("/w")).toEqual({ threshold: 10, autoCompact: true });
    expect(before).toEqual({ threshold: 10 }); // a new snapshot, not changed in place
    publishStatus("/w", { threshold: undefined });
    expect(statusOf("/w").threshold).toBeUndefined();
    forgetStatus("/w");
    forgetStatus("/w"); // twice is fine
    expect(statusOf("/w")).toEqual({});
  });
});
