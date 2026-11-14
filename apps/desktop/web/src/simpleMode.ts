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

/** The Settings dialog's sections, each a tab of an entry (settingsEntries) or, MCP servers, inside one. */
export const allSections = ["providers", "organization", "personality", "voice", "appearance", "agents", "skills", "evals", "workers", "connectors", "mcp", "secrets", "browser", "workspaces", "permissions", "service", "logs", "about"] as const;
/** A section of the Settings dialog. */
export type SectionId = (typeof allSections)[number];

/**
 * Resources' tabs, the same in Settings (every workspace's) and Studio
 * (this workspace's): who does the work (with the evals that prove them,
 * Agents' second tab), what they know, what they reach, the browser they
 * sign in with, and what they need.
 */
export const resourceTabs = ["agents", "skills", "connectors", "browser", "secrets"] as const;
/** A tab of Resources. */
export type ResourceTab = (typeof resourceTabs)[number];

/**
 * Settings' side list: six entries, each holding its sections as tabs
 * (desktop.settings.entry.<id>): the models and who provides them; how
 * Blitz talks; how the app looks; the Resources every workspace shares;
 * the global jobs; where Blitz works and what it may do; the system.
 */
export const settingsEntries = [
  { id: "providers", tabs: ["providers", "organization"] },
  { id: "personality", tabs: ["personality", "voice"] },
  { id: "appearance", tabs: ["appearance"] },
  { id: "resources", tabs: resourceTabs },
  { id: "jobs", tabs: ["workers"] },
  { id: "access", tabs: ["workspaces", "permissions"] },
  { id: "system", tabs: ["service", "logs", "about"] },
] as const satisfies readonly { id: string; tabs: readonly SectionId[] }[];
/** An entry of Settings' side list. */
export type EntryId = (typeof settingsEntries)[number]["id"];

/** The tab a section shows in: its own, but MCP servers' is Connectors' and the evals' is Agents' (their second tab). */
export const tabOf = (s: SectionId): SectionId => (s === "mcp" ? "connectors" : s === "evals" ? "agents" : s);

/** The side list's entry a section is shown under. */
export function entryOf(s: SectionId): EntryId {
  const tab = tabOf(s);
  return settingsEntries.find((e) => (e.tabs as readonly SectionId[]).includes(tab))?.id ?? "providers";
}

const simpleSections: ReadonlySet<SectionId> = new Set(["providers", "organization", "appearance", "personality", "voice", "connectors", "workspaces", "about"]);

/** An entry's tabs shown: every one, or what a newcomer needs. */
export function shownTabs(entry: EntryId, advanced: boolean): SectionId[] {
  const e = settingsEntries.find((x) => x.id === entry);
  return (e?.tabs ?? []).filter((s) => advanced || simpleSections.has(s));
}

/** The side list's entries shown: those with a tab shown. */
export function shownEntries(advanced: boolean): EntryId[] {
  return settingsEntries.map((e) => e.id).filter((id) => shownTabs(id, advanced).length > 0);
}

/**
 * The section to open on: the one asked for, unless its tab is hidden
 * (then the first). MCP servers show with advanced settings only.
 */
export function sectionShown(asked: SectionId, advanced: boolean): SectionId {
  if (asked === "mcp" && !advanced) return "connectors";
  return shownTabs(entryOf(asked), advanced).includes(tabOf(asked)) ? asked : allSections[0];
}

/**
 * Studio's Resources tabs shown: every one with advanced settings; a
 * newcomer, who opens Studio only on its Settings (the Workspaces menu's
 * pencil, the status bar), sees the connectors, as Settings shows them.
 */
export function studioResourceTabs(advanced: boolean): ResourceTab[] {
  return resourceTabs.filter((s) => advanced || simpleSections.has(s));
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
