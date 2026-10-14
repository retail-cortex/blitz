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
import { afterClosing, tabsToClose, type CloseWhich } from "./tabMenu";

const tabs = [
  { path: "a", dirty: false },
  { path: "b", dirty: true },
  { path: "c", dirty: false },
  { path: "d", dirty: true },
];

describe("tabsToClose", () => {
  it.each<[CloseWhich, string, string[]]>([
    ["this", "b", ["b"]],
    ["others", "b", ["a", "c", "d"]],
    ["right", "b", ["c", "d"]],
    ["right", "d", []],
    ["right", "missing", []],
    ["saved", "b", ["a", "c"]],
    ["all", "c", ["a", "b", "c", "d"]],
  ])("%s on %s", (which, path, want) => {
    expect(tabsToClose(tabs, path, which).map((t) => t.path)).toEqual(want);
  });
});

describe("afterClosing", () => {
  it.each<[string, string | null, string[], string[], string | null]>([
    ["keeps an open active tab", "a", ["b", "c"], ["a", "d"], "a"],
    ["moves to the next tab that stays", "b", ["b", "c"], ["a", "d"], "d"],
    ["else the one before", "d", ["c", "d"], ["a", "b"], "b"],
    ["none when all close", "a", ["a", "b", "c", "d"], [], null],
    ["none active stays none", null, ["a"], ["b", "c", "d"], null],
    ["no tabs, no active", null, ["a", "b", "c", "d"], [], null],
  ])("%s", (_, active, closed, left, want) => {
    const r = afterClosing(tabs, active, new Set(closed));
    expect(r.tabs.map((t) => t.path)).toEqual(left);
    expect(r.active).toBe(want);
  });
});
