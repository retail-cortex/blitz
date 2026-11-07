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
// the index (spec_040_studio STU-90).

/** Where the specs live in a workspace. */
export const specsDir = ".agents/specs";

/** The index /spec keeps beside the specs. */
export const specsIndex = "README.md";

/** A spec file, from its name. */
export interface SpecFile {
  /** The file's name: 001-overview.md, or README.md. */
  file: string;
  /** Its path in the workspace. */
  path: string;
  /** Its place in the order (0 for the index and unnumbered files). */
  number: number;
  /** The name's words: "overview" → "Overview". */
  title: string;
}

const numbered = /^(\d{3})-(.+)\.md$/i;

/** The Markdown files in the specs folder, the index first, then in order. */
export function specFiles(names: string[]): SpecFile[] {
  const out: SpecFile[] = [];
  for (const file of names) {
    if (!/\.md$/i.test(file)) continue;
    const m = numbered.exec(file);
    const words = (m ? m[2] : file.replace(/\.md$/i, "")).replace(/[-_]+/g, " ").trim();
    out.push({ file, path: `${specsDir}/${file}`, number: m ? Number(m[1]) : 0, title: words.charAt(0).toUpperCase() + words.slice(1) });
  }
  const rank = (s: SpecFile) => (s.file.toLowerCase() === specsIndex.toLowerCase() ? -1 : s.number || 1000);
  return out.sort((a, b) => rank(a) - rank(b) || a.file.localeCompare(b.file));
}

/** A spec's file name for a title: the next number after the last, and the title's words, lowercase with - between. "" when the title has no words. */
export function newSpecFile(title: string, specs: SpecFile[]): string {
  const slug = title
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  if (!slug) return "";
  const next = Math.min(999, Math.max(0, ...specs.map((s) => s.number)) + 1);
  return `${String(next).padStart(3, "0")}-${slug}.md`;
}

/** A new spec's text: its title as the heading. */
export function newSpecText(title: string): string {
  return `# ${title.trim()}\n\n`;
}
