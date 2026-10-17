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

// \skill-name in the composer (spec_skills_013 SK-104): finding the one
// being typed, the skills it could be, and putting the chosen one in its
// place. The service sends a named skill's instructions with the prompt.

import { workspaces } from "./api";

/** The \skill being typed: where its backslash is, and the name so far. */
export interface SkillToken {
  start: number;
  end: number;
  query: string;
}

/**
 * The \skill the caret is in: a backslash where a word starts (the start,
 * after whitespace or an opening bracket or quote), then name characters
 * up to the caret. Null when the caret isn't in one.
 */
export function skillAt(text: string, caret: number): SkillToken | null {
  const m = /(^|[\s(\[{"'])\\([a-z0-9-]*)$/.exec(text.slice(0, caret));
  if (!m) return null;
  const start = m.index + m[1].length;
  return { start, end: caret, query: m[2] };
}

/** The skills query could be, best first: names starting with it, then names and descriptions containing it; ten at most. */
export function skillOptions<T extends { name: string; description: string }>(query: string, skills: readonly T[]): T[] {
  const q = query.toLowerCase();
  const starts = skills.filter((s) => s.name.startsWith(q));
  const contains = skills.filter((s) => !s.name.startsWith(q) && (s.name.includes(q) || s.description.toLowerCase().includes(q)));
  return [...starts, ...contains].slice(0, 10);
}

/** Replaces the \skill being typed with \name and a space (unless one follows); returns the text and where the caret goes. */
export function insertSkill(text: string, token: SkillToken, name: string): { text: string; caret: number } {
  const after = text.slice(token.end).replace(/^[a-z0-9-]*/, "");
  const inserted = `\\${name}${after.startsWith(" ") ? "" : " "}`;
  return { text: text.slice(0, token.start) + inserted + after, caret: token.start + inserted.length };
}

/** How long a workspace's skill list is kept before it's asked for again. */
const skillsTTL = 30_000;
const cache = new Map<string, { at: number; list: Promise<{ name: string; description: string }[]> }>();

/** The workspace's skills for completion, asked for once every 30 s. */
export function skillsFor(workspace: string): Promise<{ name: string; description: string }[]> {
  const hit = cache.get(workspace);
  if (hit && Date.now() - hit.at < skillsTTL) return hit.list;
  const list = workspaces.listSkills({ workspace }).then(
    (r) => r.skills.map((s) => ({ name: s.name, description: s.description })),
    () => [],
  );
  cache.set(workspace, { at: Date.now(), list });
  return list;
}

/** Forgets a workspace's skills (one was added, renamed or deleted). */
export function forgetSkills(workspace: string) {
  cache.delete(workspace);
}
