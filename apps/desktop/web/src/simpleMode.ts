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

// Simple and advanced (Settings › Appearance › Show advanced settings):
// what a newcomer sees, and what waits until asked for. Hidden settings
// keep working with what they're set to; hiding never resets anything.
// The smaller choices (a row, a button) are made where they're drawn,
// with useAdvanced (state.tsx); the larger ones are here.

/** The Settings dialog's sections, in order. */
export const allSections = ["appearance", "documents", "providers", "permissions", "agents", "file", "workspaces", "service", "logs", "about"] as const;
/** A section of the Settings dialog. */
export type SectionId = (typeof allSections)[number];

const simpleSections: ReadonlySet<SectionId> = new Set(["appearance", "providers", "workspaces", "about"]);

/** The Settings dialog's sections shown: everything, or what a newcomer needs. */
export function settingsSections(advanced: boolean): SectionId[] {
  return allSections.filter((s) => advanced || simpleSections.has(s));
}

/** The section to open on: the one asked for, unless it's hidden (then Appearance). */
export function sectionShown(asked: SectionId, advanced: boolean): SectionId {
  return settingsSections(advanced).includes(asked) ? asked : "appearance";
}

/** The top bar's menus, in order. */
export type MenuId = "workspaces" | "changes" | "agents" | "workers" | "help";

/** The top bar's menus shown: simple keeps Workspaces, Changes and Help. */
export function menuIds(advanced: boolean): MenuId[] {
  return advanced ? ["workspaces", "changes", "agents", "workers", "help"] : ["workspaces", "changes", "help"];
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
