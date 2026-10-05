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
import { deletable, filterHistory, historyCounts, isWorkerRun, toggleAll } from "./history";

const s = (id: string, title: string, origin = "", agent = "blitz", snapshot = "") => ({ id, title, origin, agent, snapshot });
const list = [s("1", "Fix the cart total"), s("2", "⏰ deps 2026-10-04 09:00", "worker"), s("3", "Cart tests", "background", "tester"), s("4", "", "", "blitz", "before-refactor")];
const ids = (l: { id: string }[]) => l.map((x) => x.id);

describe("the chat history", () => {
  it.each([
    ["everything", "all", "", ["1", "2", "3", "4"]],
    ["chats, detached runs among them", "chats", "", ["1", "3", "4"]],
    ["workers' runs", "workers", "", ["2"]],
    ["a word in any case", "all", "CART", ["1", "3"]],
    ["every word, in any order", "all", "total fix", ["1"]],
    ["the agent", "all", "tester", ["3"]],
    ["a snapshot's name", "all", "refactor", ["4"]],
    ["a search within a filter", "workers", "cart", []],
    ["spaces alone match everything", "chats", "  ", ["1", "3", "4"]],
  ])("shows %s", (_, filter, query, want) => {
    expect(ids(filterHistory(list, filter as "all" | "chats" | "workers", query))).toEqual(want);
  });

  it("counts each filter's sessions", () => {
    expect(historyCounts(list)).toEqual({ all: 4, chats: 3, workers: 1 });
    expect(isWorkerRun({ origin: "background" })).toBe(false);
  });

  it("deletes the ticked sessions still listed, never the chat in view", () => {
    expect(ids(deletable(list, new Set(["1", "2", "9"]), "1"))).toEqual(["2"]);
    expect(deletable(list, new Set(), "1")).toEqual([]);
  });

  it("ticks every shown session but the current one, then none", () => {
    const shown = list.slice(0, 3);
    const all = toggleAll(shown, new Set(["4"]), "1");
    expect([...all].sort()).toEqual(["2", "3", "4"]);
    expect([...toggleAll(shown, all, "1")]).toEqual(["4"]);
    expect([...toggleAll([s("1", "x")], new Set(), "1")]).toEqual([]);
  });
});
