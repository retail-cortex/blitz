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

// An approval's proposed change, for the editor's diff view: the files a
// unified diff names, and a file's text with its hunks applied.

/** One file's part of a unified diff. */
export interface FilePatch {
  /** The file before (undefined when the diff creates it). */
  oldPath?: string;
  /** The file after (undefined when the diff deletes it). */
  newPath?: string;
  hunks: Hunk[];
}

/** A hunk: where it starts in the old file, and its lines with their marks. */
export interface Hunk {
  oldStart: number;
  lines: string[];
}

function pathOf(header: string): string | undefined {
  const p = header.slice(4).split("\t")[0].trim();
  if (p === "/dev/null") return undefined;
  return p.replace(/^[ab]\//, "");
}

/** Reads a unified diff's files. */
export function parsePatch(diff: string): FilePatch[] {
  const out: FilePatch[] = [];
  let cur: FilePatch | undefined;
  let hunk: Hunk | undefined;
  const lines = diff.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const l = lines[i];
    if (l.startsWith("--- ") && lines[i + 1]?.startsWith("+++ ")) {
      cur = { oldPath: pathOf(l), newPath: pathOf(lines[i + 1]), hunks: [] };
      out.push(cur);
      hunk = undefined;
      i++;
      continue;
    }
    const h = /^@@ -(\d+)(?:,\d+)? \+\d+(?:,\d+)? @@/.exec(l);
    if (h && cur) {
      hunk = { oldStart: Number(h[1]), lines: [] };
      cur.hunks.push(hunk);
      continue;
    }
    if (hunk && (l.startsWith(" ") || l.startsWith("+") || l.startsWith("-"))) hunk.lines.push(l);
    else if (hunk && l === "" && i < lines.length - 1) hunk.lines.push(" "); // a blank context line, trimmed
  }
  return out;
}

/**
 * Applies a file's hunks to its text. A hunk whose context doesn't match
 * where it says is looked for nearby, as patch does; one that isn't found
 * is an error (the file changed since).
 */
export function applyPatch(text: string, p: FilePatch): string {
  const src = text === "" ? [] : text.split("\n");
  const trailing = src.length > 0 && src[src.length - 1] === "";
  if (trailing) src.pop();
  const out: string[] = [];
  let at = 0; // next line of src to copy
  for (const h of p.hunks) {
    const before = h.lines.filter((l) => !l.startsWith("+")).map((l) => l.slice(1));
    const after = h.lines.filter((l) => !l.startsWith("-")).map((l) => l.slice(1));
    const want = Math.max(h.oldStart - 1, 0);
    const start = findBlock(src, before, want, at);
    if (start < 0) throw new Error(`a change at line ${h.oldStart} doesn't fit the file as it is now`);
    out.push(...src.slice(at, start), ...after);
    at = start + before.length;
  }
  out.push(...src.slice(at));
  return out.join("\n") + (trailing || (src.length === 0 && out.length > 0) ? "\n" : "");
}

// Where block's lines are in src, from `from` on: at want, else the nearest.
function findBlock(src: string[], block: string[], want: number, from: number): number {
  const fits = (i: number) => i >= from && i + block.length <= src.length && block.every((l, k) => src[i + k] === l);
  if (block.length === 0) return Math.max(Math.min(want, src.length), from);
  for (let d = 0; d <= src.length; d++) {
    if (fits(want - d)) return want - d;
    if (fits(want + d)) return want + d;
  }
  return -1;
}
