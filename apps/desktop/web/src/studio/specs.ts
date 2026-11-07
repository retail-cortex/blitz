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

// The project's specification, as /spec (the project-spec agent) writes
// it: .agents/specs/NNN-<name>.md in order of execution, and README.md,
// the index (spec_040_studio STU-11, spec_057_spec_driven). The service
// reads and checks it (ListSpecs, ReviewSpec); these are what the page
// does itself: a new spec's name, and a refined spec's changes.

/** Where the specs live in a workspace. */
export const specsDir = ".agents/specs";

/** The index /spec keeps beside the specs. */
export const specsIndex = "README.md";

/** A spec's file name for a title: the next number after the last, and the title's words, lowercase with - between. "" when the title has no words. */
export function newSpecFile(title: string, numbers: number[]): string {
  const slug = title
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  if (!slug) return "";
  const next = Math.min(999, Math.max(0, ...numbers) + 1);
  return `${String(next).padStart(3, "0")}-${slug}.md`;
}

/** A new spec's text, in /spec's format: its number and title as the heading, a draft. */
export function newSpecText(title: string, file: string): string {
  const n = /^(\d{3})-/.exec(file)?.[1];
  return `# ${n ? `${n}. ` : ""}${title.trim()}\n\nStatus: draft\n\n## Context\n\n## Requirements\n\n## Done when\n\n## Open questions\n`;
}

/** One change between two texts: at line `at` of the first (0-based), `removed` lines give way to `added`. */
export interface Hunk {
  at: number;
  removed: string[];
  added: string[];
}

/** The changes from a to b, line by line (a longest common subsequence), as hunks in order. */
export function lineHunks(a: string, b: string): Hunk[] {
  const x = a.split("\n");
  const y = b.split("\n");
  const n = x.length;
  const m = y.length;
  // lcs[i][j]: the common lines of x[i:] and y[j:].
  const lcs: Uint32Array[] = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--) lcs[i][j] = x[i] === y[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
  const out: Hunk[] = [];
  let open: Hunk | null = null;
  let i = 0;
  let j = 0;
  const hunk = () => (open ??= { at: i, removed: [], added: [] });
  while (i < n || j < m) {
    if (i < n && j < m && x[i] === y[j]) {
      if (open) out.push(open);
      open = null;
      i++;
      j++;
    } else if (j < m && (i === n || lcs[i][j + 1] >= lcs[i + 1][j])) {
      hunk().added.push(y[j++]);
    } else {
      hunk().removed.push(x[i++]);
    }
  }
  if (open) out.push(open);
  return out;
}

/** a with the hunks kept (keep[k] for hunks[k]) applied, and the rest left as they were. */
export function applyHunks(a: string, hunks: Hunk[], keep: boolean[]): string {
  const x = a.split("\n");
  const out: string[] = [];
  let i = 0;
  hunks.forEach((h, k) => {
    while (i < h.at) out.push(x[i++]);
    if (keep[k]) out.push(...h.added);
    else out.push(...h.removed);
    i += h.removed.length;
  });
  while (i < x.length) out.push(x[i++]);
  return out.join("\n");
}
