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

/** Which tabs a tab's context menu closes (FIL-40a). */
export type CloseWhich = "this" | "others" | "right" | "saved" | "all";

interface Closable {
  path: string;
  dirty: boolean;
}

/** The tabs that "which", picked on the tab at path, closes, in tab order. */
export function tabsToClose<T extends Closable>(tabs: readonly T[], path: string, which: CloseWhich): T[] {
  const at = tabs.findIndex((t) => t.path === path);
  switch (which) {
    case "this":
      return tabs.filter((t) => t.path === path);
    case "others":
      return tabs.filter((t) => t.path !== path);
    case "right":
      return at < 0 ? [] : tabs.slice(at + 1);
    case "saved":
      return tabs.filter((t) => !t.dirty);
    case "all":
      return [...tabs];
  }
}

/**
 * The tabs left after closing some, and which is active: the same one if it
 * stays open (or none was), else the next one that stays, else the one
 * before it, else none.
 */
export function afterClosing<T extends { path: string }>(tabs: readonly T[], active: string | null, closed: ReadonlySet<string>): { tabs: T[]; active: string | null } {
  const next = tabs.filter((t) => !closed.has(t.path));
  if (active === null || !closed.has(active)) return { tabs: next, active };
  // The first tab after the closed active one that stays, else the last before it.
  const i = tabs.findIndex((t) => t.path === active);
  const after = tabs.slice(i + 1).find((t) => !closed.has(t.path));
  const before = tabs.slice(0, i).reverse().find((t) => !closed.has(t.path));
  return { tabs: next, active: (after ?? before)?.path ?? null };
}
