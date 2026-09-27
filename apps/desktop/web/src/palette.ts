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

// Colours a workspace can be given, shown as its dot in the drawer and its
// accent in the top bar. Each works on light and dark surfaces.
import { t } from "./i18n";

export const palette: Record<string, { light: string; dark: string }> = {
  blue: { light: "#0b57d0", dark: "#a8c7fa" },
  teal: { light: "#00696f", dark: "#6fd7df" },
  green: { light: "#146c2e", dark: "#6dd58c" },
  amber: { light: "#8b5000", dark: "#ffb870" },
  red: { light: "#b3261e", dark: "#f2b8b5" },
  pink: { light: "#a4176f", dark: "#ffafd9" },
  purple: { light: "#6750a4", dark: "#d0bcff" },
  grey: { light: "#5f6368", dark: "#bdc1c6" },
};

export const colorNames = Object.keys(palette);

/** A colour's name, in the window's language. */
export const colorLabel = (name: string) => t(`desktop.color.${name}`);

/** The colour for a workspace's colour name (blue when unset or unknown). */
export function workspaceColor(name: string | undefined, theme: "light" | "dark"): string {
  const c = palette[name ?? ""] ?? palette.blue;
  return c[theme];
}
