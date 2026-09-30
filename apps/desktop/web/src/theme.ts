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

// The page's theme: the user's choice, or the system's when they chose
// "system" (the default), applied as html[data-theme] and kept in step
// with the system's setting as it changes.

import { editorConfig, editorIsDark, onEditorThemeChange } from "./host";

/** The user's choice of theme: the system's, light or dark. */
export type ThemePref = "system" | "light" | "dark";
/** The theme in effect (the preference can also be "system"). */
export type Theme = "light" | "dark";

/** The theme to show for a preference, given whether the system is dark. */
export function resolveTheme(pref: ThemePref, systemDark: boolean): Theme {
  if (pref === "light" || pref === "dark") return pref;
  return systemDark ? "dark" : "light";
}

const darkQuery = () => window.matchMedia?.("(prefers-color-scheme: dark)");

/** Whether the system (inside an editor, the editor) is in dark mode now. */
export function systemIsDark(): boolean {
  if (editorConfig()) return editorIsDark();
  return darkQuery()?.matches ?? false;
}

/** Applies the theme, density and conversation width to the document. */
export function applyTheme(theme: Theme, density: "comfortable" | "compact" = "comfortable", width: "full" | "readable" = "full") {
  const root = document.documentElement;
  root.dataset.theme = theme;
  root.dataset.density = density;
  root.dataset.width = width;
}

/** Calls f whenever the system switches between light and dark. */
export function onSystemThemeChange(f: (dark: boolean) => void): () => void {
  if (editorConfig()) return onEditorThemeChange(f);
  const q = darkQuery();
  if (!q) return () => {};
  const listener = (e: MediaQueryListEvent) => f(e.matches);
  q.addEventListener("change", listener);
  return () => q.removeEventListener("change", listener);
}
