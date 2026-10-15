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
import { authorHue, changeShare, dateGroup, sameGroup } from "./history";

describe("dateGroup", () => {
  const now = new Date(2026, 9, 15, 10, 0);
  it.each([
    [new Date(2026, 9, 15, 1, 0), "today"],
    [new Date(2026, 9, 14, 23, 59), "yesterday"],
    [new Date(2026, 9, 10, 12, 0), "week"],
    [new Date(2026, 9, 2, 12, 0), "month"],
    [new Date(2026, 8, 30, 12, 0), "older"],
  ])("%s is %s", (when, kind) => expect(dateGroup(when, now).kind).toBe(kind));
  it("names an older one's month", () => {
    expect(dateGroup(new Date(2025, 11, 3), now)).toEqual({ kind: "older", year: 2025, month: 11 });
  });
  it("compares groups", () => {
    expect(sameGroup({ kind: "today" }, { kind: "today" })).toBe(true);
    expect(sameGroup({ kind: "older", year: 2025, month: 1 }, { kind: "older", year: 2025, month: 2 })).toBe(false);
    expect(sameGroup({ kind: "week" }, { kind: "month" })).toBe(false);
  });
});

describe("authorHue and changeShare", () => {
  it("gives an address the same colour, whatever its case", () => {
    expect(authorHue("Ana@Example.com")).toBe(authorHue("ana@example.com"));
    expect(authorHue("ana@example.com")).not.toBe(authorHue("sam@example.com"));
    expect(authorHue("x")).toBeGreaterThanOrEqual(0);
  });
  it("scales a change against the largest, never invisible", () => {
    expect(changeShare(0, 100)).toBe(0);
    expect(changeShare(100, 100)).toBe(1);
    expect(changeShare(1, 10_000)).toBeGreaterThanOrEqual(0.08);
    expect(changeShare(10, 0)).toBe(0);
  });
});
