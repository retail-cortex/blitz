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

// The lanes of a history graph (spec_files_029 FIL-59b), as git log
// --graph draws them: each commit on a lane, a line down each lane to the
// commit it expects next, lines that join where a merge's parents meet
// and split where a commit has several. Pure, so the drawing (one row per
// commit, HistoryGraph.tsx) can't get it wrong.

/** A commit as the graph needs it: its hash and its parents' (first parent first). */
export interface GraphCommit {
  commit: string;
  parents: string[];
}

/** A line in a row, from a lane at the row's top or middle to a lane at its middle or bottom. */
export interface Segment {
  from: number;
  to: number;
  /** Which half of the row: "top" (top to the commit's height) or "bottom" (from there down). */
  half: "top" | "bottom";
  /** The lane whose colour it takes. */
  color: number;
}

/** One row of the graph: the commit's lane and the lines through the row. */
export interface GraphRow {
  lane: number;
  /** The commit's colour (its lane's). */
  color: number;
  segments: Segment[];
  /** How many lanes the row needs drawn. */
  width: number;
}

/**
 * Lays out commits (newest first, as a history lists them) on lanes. A
 * parent outside the list (an older page) keeps its lane to the bottom.
 */
export function layoutGraph(commits: readonly GraphCommit[]): GraphRow[] {
  // lanes[i]: the commit lane i leads to next, or null when it's free.
  let lanes: (string | null)[] = [];
  const colors: number[] = [];
  let nextColor = 0;
  const rows: GraphRow[] = [];
  for (const c of commits) {
    const before = lanes.slice();
    const beforeColors = colors.slice();
    let lane = lanes.indexOf(c.commit);
    if (lane < 0) {
      lane = free(lanes);
      lanes[lane] = c.commit;
      colors[lane] = nextColor++;
    }
    const color = colors[lane];
    const segments: Segment[] = [];
    // Top half: every lane coming down; those leading here bend into it.
    for (let i = 0; i < before.length; i++) {
      if (before[i] === null) continue;
      segments.push({ from: i, to: before[i] === c.commit ? lane : i, half: "top", color: beforeColors[i] });
    }
    // Lanes that led here are done, but the commit's own.
    for (let i = 0; i < lanes.length; i++) if (i !== lane && lanes[i] === c.commit) lanes[i] = null;
    // The first parent continues the commit's lane; others take a lane
    // already leading to them, or a new one.
    const [first, ...rest] = c.parents;
    lanes[lane] = first ?? null;
    const joins: number[] = [];
    for (const p of rest) {
      let to = lanes.indexOf(p);
      if (to < 0) {
        to = free(lanes, lane);
        lanes[to] = p;
        colors[to] = nextColor++;
      }
      joins.push(to);
    }
    // Bottom half: lanes going on down, and lines from the commit to each
    // parent's lane.
    for (let i = 0; i < lanes.length; i++) {
      if (lanes[i] === null) continue;
      if (i === lane) segments.push({ from: lane, to: lane, half: "bottom", color: colors[lane] });
      else if (joins.includes(i)) segments.push({ from: lane, to: i, half: "bottom", color: colors[i] });
      else if (before[i] !== null && before[i] !== c.commit) segments.push({ from: i, to: i, half: "bottom", color: colors[i] });
    }
    while (lanes.length > 0 && lanes[lanes.length - 1] === null) lanes = lanes.slice(0, -1);
    const width = Math.max(lane + 1, ...segments.map((s) => Math.max(s.from, s.to) + 1));
    rows.push({ lane, color, segments, width });
  }
  return rows;
}

// The first free lane (not skip), or a new one.
function free(lanes: (string | null)[], skip = -1): number {
  for (let i = 0; i < lanes.length; i++) if (lanes[i] === null && i !== skip) return i;
  lanes.push(null);
  return lanes.length - 1;
}
