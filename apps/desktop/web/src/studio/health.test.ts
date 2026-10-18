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
import { HealthItemSchema, HealthSignalSchema, type HealthItem } from "../gen/blitz/v1/workspace_pb";
import { chartGeometry, ringDash } from "../ui/chartMath";
import { attention, dayColumns, dayTotals, delta, grade, itemKey, kindSummary, topRuns } from "./health";

const item = (init: Parameters<typeof create<typeof HealthItemSchema>>[1]): HealthItem => create(HealthItemSchema, init);

describe("the workspace dashboard", () => {
  it.each([
    [0, ""],
    [5, "A"],
    [4.5, "A"],
    [4.49, "B"],
    [3.5, "B"],
    [2.5, "C"],
    [2.49, "D"],
    [1, "D"],
  ])("grades %s as %s, as the service does", (score, want) => {
    expect(grade(score)).toBe(want);
  });

  it("lists what needs attention, problems, failures and low grades first", () => {
    const items = [
      item({ kind: "skill", name: "idle", flags: ["unused"] }),
      item({ kind: "agent", name: "triage", flags: ["uncovered"], verdict: { grade: "C", overall: 2.8 } }),
      item({ kind: "agent", name: "broken", problem: "bad yaml" }),
      item({ kind: "workflow", name: "brief", flags: ["failing"], runs: { runs: 7, failed: 4 } }),
      item({ kind: "agent", name: "weak", verdict: { grade: "D", overall: 1.9 } }),
    ];
    const signals = [create(HealthSignalSchema, { kind: "denial", about: "shell", count: 3, detail: "pushes" })];
    const got = attention({ items, signals });
    expect(got.map((a) => `${a.level}:${a.key}:${a.why}`)).toEqual([
      "error:agent/broken:problem",
      "error:agent/weak:grade",
      "error:workflow/brief:failing",
      "warn:agent/triage:grade",
      "warn:agent/triage:uncovered",
      "warn:denial/shell:signal_denial",
      "info:skill/idle:unused",
    ]);
    expect(got[2].values).toEqual({ failed: 4, runs: 7 });
    expect(attention({ items: [], signals: [] })).toEqual([]);
  });

  it("sums up a kind: its items, problems, flags, verdicts and grades", () => {
    const items = [
      item({ kind: "agent", name: "a", verdict: { grade: "A" } }),
      item({ kind: "agent", name: "b", verdict: { grade: "C" }, flags: ["stale"] }),
      item({ kind: "agent", name: "c", problem: "x" }),
      item({ kind: "skill", name: "d" }),
    ];
    expect(kindSummary(items, "agent")).toEqual({ count: 3, problems: 1, flagged: 1, judged: 2, grades: { A: 1, B: 0, C: 1, D: 0 } });
    expect(itemKey(items[0])).toBe("agent/a");
  });

  it("charts the days, totals them, and ranks runs by cost and failures", () => {
    const days = [
      { date: "2026-10-17", done: 2, failed: 1, stopped: 0, costUsd: 0.03 },
      { date: "2026-10-18", done: 1, failed: 0, stopped: 1, costUsd: 0.01 },
    ] as Parameters<typeof dayColumns>[0];
    const cols = dayColumns(days, (d) => d.date);
    expect(cols[0].parts.map((p) => p.value)).toEqual([2, 1, 0]);
    expect(cols[1]).toMatchObject({ key: "2026-10-18", line: 0.01, title: "2026-10-18" });
    expect(dayTotals(days)).toEqual({ runs: 5, done: 3, cost: 0.04, rate: 0.6 });
    expect(dayTotals([]).rate).toBe(0);
    const runs = [
      item({ kind: "workflow", name: "a", runs: { costUsd: 0.1, failed: 1 } }),
      item({ kind: "workflow", name: "b", runs: { costUsd: 0.3, failed: 0 } }),
      item({ kind: "worker", name: "c", runs: { costUsd: 0.2, failed: 3 } }),
      item({ kind: "agent", name: "d" }),
    ];
    expect(topRuns(runs, "cost").map((i) => i.name)).toEqual(["b", "c", "a"]);
    expect(topRuns(runs, "failed").map((i) => i.name)).toEqual(["c", "a"]);
    expect(topRuns(runs, "cost", 1)).toHaveLength(1);
  });

  it("says how a score moved", () => {
    expect(delta(3.9, 3.6)).toBe("+0.3");
    expect(delta(3.2, 3.6)).toBe("-0.4");
    expect(delta(3.6, 3.62)).toBe("");
    expect(delta(0, 3)).toBe("");
  });

  it("lays out rings, columns and lines", () => {
    const r = ringDash(2.5, 10);
    expect(r.dash).toBeCloseTo(Math.PI * 10);
    expect(r.dash + r.gap).toBeCloseTo(2 * Math.PI * 10);
    expect(ringDash(9, 10).gap).toBeCloseTo(0);
    const g = chartGeometry([2, 4], [0, 1], 100, 50);
    expect(g.step).toBe(50);
    expect(g.columnScale).toBeCloseTo(47.5 / 4);
    expect(g.line).toBe("25.0,50.0 75.0,2.5");
    expect(chartGeometry([], [0, 0], 100, 50).line).toBe("25.0,50.0 75.0,50.0");
    expect(chartGeometry([1], [5], 100, 50).line).toBe("");
    expect(chartGeometry([], [], 100, 50).columnScale).toBe(0);
  });
});
