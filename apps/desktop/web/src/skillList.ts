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

// The skills a workspace finds, as Settings › Skills lists them
// (spec_skills_013 SK-96a): where each comes from, and the file to open.

/** Where a skill was found. */
export type SkillWhere = "workspace" | "user" | "registry" | "other" | "builtin";

/** The order the places are listed in. */
export const skillWhereOrder: readonly SkillWhere[] = ["workspace", "user", "registry", "other", "builtin"];

const slash = (p: string) => p.replace(/\\/g, "/").replace(/\/+$/, "");

/**
 * Where the skill in dir was found, for the workspace: built in (no
 * folder), installed from a registry (one of registryDirs), the
 * workspace's (inside it), the user's (~/.blitz/skills) or elsewhere (a
 * plugin's, a configured path).
 */
export function skillWhere(dir: string, workspace: string, registryDirs: readonly string[]): SkillWhere {
  if (!dir) return "builtin";
  const d = slash(dir);
  if (registryDirs.some((r) => slash(r) === d)) return "registry";
  if (workspace && d.startsWith(slash(workspace) + "/")) return "workspace";
  if (d.includes("/.blitz/skills/")) return "user";
  return "other";
}

/** A workspace skill's SKILL.md, relative to the workspace ("" when it isn't in it). */
export function skillFile(dir: string, workspace: string): string {
  const d = slash(dir);
  const w = slash(workspace);
  return w && d.startsWith(w + "/") ? `${d.slice(w.length + 1)}/SKILL.md` : "";
}
