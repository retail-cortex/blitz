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

// Simple and advanced (Show advanced, in the Settings title bar):
// what a newcomer sees, and what waits until asked for. Hidden settings
// keep working with what they're set to; hiding never resets anything.
// The smaller choices (a row, a button) are made where they're drawn,
// with useAdvanced (state.tsx); the larger ones are here.

/** The Settings dialog's sections, in order. */
export const allSections = ["providers", "appearance", "agents", "skills", "workers", "mcp", "connectors", "browser", "secrets", "workspaces", "permissions", "service", "logs", "about"] as const;
/** A section of the Settings dialog. */
export type SectionId = (typeof allSections)[number];

/**
 * The Library's tabs (its entry in the side list is agents'): the global
 * agents, skills, jobs and secrets, each a section of its own to open on.
 */
export const libraryTabs = ["agents", "skills", "workers", "secrets"] as const;

/**
 * Sections shown inside another's entry: the Library's tabs, and MCP
 * servers under Connectors (the systems the agents reach, together).
 */
const homes: Partial<Record<SectionId, SectionId>> = { skills: "agents", workers: "agents", secrets: "agents", mcp: "connectors" };

/** The side list's entry a section is shown under (itself, mostly). */
export const homeOf = (s: SectionId): SectionId => homes[s] ?? s;

/**
 * The side list's entries in groups, each under a heading
 * (desktop.settings.group.<id>): getting started; the global setup;
 * where Blitz works and what it may do; the system. With the sections
 * shown inside them (homeOf), they're allSections.
 */
export const sectionGroups: readonly { id: string; sections: readonly SectionId[] }[] = [
  { id: "start", sections: ["providers", "appearance"] },
  { id: "setup", sections: ["agents", "connectors", "browser"] },
  { id: "access", sections: ["workspaces", "permissions"] },
  { id: "system", sections: ["service", "logs", "about"] },
];

const simpleSections: ReadonlySet<SectionId> = new Set(["providers", "appearance", "connectors", "workspaces", "about"]);

/** The side list's entries shown: everything, or what a newcomer needs. */
export function settingsSections(advanced: boolean): SectionId[] {
  return sectionGroups.flatMap((g) => g.sections).filter((s) => advanced || simpleSections.has(s));
}

/** The groups shown, each with its sections shown (none left: the group isn't). */
export function shownGroups(advanced: boolean): { id: string; sections: SectionId[] }[] {
  const shown = settingsSections(advanced);
  return sectionGroups.map((g) => ({ id: g.id, sections: g.sections.filter((s) => shown.includes(s)) })).filter((g) => g.sections.length > 0);
}

/**
 * The section to open on: the one asked for, unless its entry is hidden
 * (then the first). MCP servers show with advanced settings only.
 */
export function sectionShown(asked: SectionId, advanced: boolean): SectionId {
  if (asked === "mcp" && !advanced) return "connectors";
  return settingsSections(advanced).includes(homeOf(asked)) ? asked : allSections[0];
}

/** The top bar's menus, in order. */
export type MenuId = "workspaces" | "changes" | "studio" | "help";

/** The top bar's menus shown: simple keeps Workspaces, Changes and Help; Studio (agents, skills, workflows… and workers) is advanced. */
export function menuIds(advanced: boolean): MenuId[] {
  return advanced ? ["workspaces", "changes", "studio", "help"] : ["workspaces", "changes", "help"];
}

/**
 * Whether a provider's sign-in choice (API key, Google Cloud, an Anthropic
 * account) and its fields are shown: always with advanced settings, and
 * in simple mode when it signs in some other way than an API key, so the
 * form never hides how it's set.
 */
export function showsSignIn(advanced: boolean, method: string): boolean {
  return advanced || (method !== "" && method !== "api_key");
}
