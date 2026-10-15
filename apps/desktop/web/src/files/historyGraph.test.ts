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
import { layoutGraph, type GraphCommit } from "./historyGraph";

const c = (commit: string, ...parents: string[]): GraphCommit => ({ commit, parents });

describe("layoutGraph", () => {
  it("keeps a straight history on one lane", () => {
    const rows = layoutGraph([c("c", "b"), c("b", "a"), c("a")]);
    expect(rows.map((r) => r.lane)).toEqual([0, 0, 0]);
    expect(rows[0].segments).toEqual([{ from: 0, to: 0, half: "bottom", color: 0 }]);
    expect(rows[1].segments).toEqual([
      { from: 0, to: 0, half: "top", color: 0 },
      { from: 0, to: 0, half: "bottom", color: 0 },
    ]);
    expect(rows[2].segments).toEqual([{ from: 0, to: 0, half: "top", color: 0 }]); // the first commit ends the line
  });

  it("splits at a merge and joins where the branch began", () => {
    // m merges b2 (topic) into a2 (main); both come from a1.
    const rows = layoutGraph([c("m", "a2", "b2"), c("a2", "a1"), c("b2", "a1"), c("a1")]);
    expect(rows.map((r) => r.lane)).toEqual([0, 0, 1, 0]);
    expect(rows[0].segments).toContainEqual({ from: 0, to: 1, half: "bottom", color: 1 });
    expect(rows[1].segments).toContainEqual({ from: 1, to: 1, half: "bottom", color: 1 }); // the topic lane passes by
    expect(rows[2].segments).toContainEqual({ from: 1, to: 1, half: "bottom", color: 1 });
    expect(rows[3].segments).toContainEqual({ from: 1, to: 0, half: "top", color: 1 }); // the topic joins main at a1
    expect(Math.max(...rows.map((r) => r.width))).toBe(2);
  });

  it("frees a lane once its branch has joined, and reuses it", () => {
    const rows = layoutGraph([c("m2", "m1", "x"), c("x", "a"), c("m1", "a"), c("a", "z"), c("n", "z"), c("z")]);
    expect(rows[1].lane).toBe(1);
    expect(rows[4].lane).toBe(1); // a new branch takes the freed lane
  });

  it("keeps a lane to the bottom for a parent on the next page", () => {
    const rows = layoutGraph([c("b", "a")]);
    expect(rows[0].segments).toContainEqual({ from: 0, to: 0, half: "bottom", color: 0 });
  });

  it("starts unrelated histories on lanes of their own", () => {
    const rows = layoutGraph([c("y"), c("x")]);
    expect(rows.map((r) => r.lane)).toEqual([0, 0]);
    expect(layoutGraph([]).length).toBe(0);
  });
});
