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

// The colour themes, for light and for dark (Settings › Appearance, as
// VS Code's preferred light and dark themes): Blitz's own (m3.css, with
// its glass), and the others themes.css gives (solid). Their names are
// the themes' own, not translated.

/** The themes for light. */
export const lightThemes = [
  { id: "blitz", name: "Blitz" },
  { id: "one-light", name: "One Light" },
] as const;
/** The themes for dark. */
export const darkThemes = [
  { id: "blitz", name: "Blitz" },
  { id: "one-dark-pro", name: "One Dark Pro" },
] as const;
/** A theme for light, by its ID (html[data-palette]). */
export type LightThemeId = (typeof lightThemes)[number]["id"];
/** A theme for dark, by its ID (html[data-palette]). */
export type DarkThemeId = (typeof darkThemes)[number]["id"];
