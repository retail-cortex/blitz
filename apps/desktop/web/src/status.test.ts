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
import { contextShare, forgetStatus, publishStatus, statusOf, tokens } from "./status";

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
