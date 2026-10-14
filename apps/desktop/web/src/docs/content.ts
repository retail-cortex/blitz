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

// The user docs the app carries (spec_desktop_024 DSK-103): the
// repository's docs/content, the sections a user reads, as the release was
// built with them. Loaded with the viewer (its own chunk), never with the
// page.
const raw = import.meta.glob(
  ["../../../../../docs/content/_index.md", "../../../../../docs/content/{getting-started,tutorials,guide,products,releases}/**/*.md"],
  { query: "?raw", import: "default", eager: true },
) as Record<string, string>;

/** The docs' Markdown files, by their path in docs/content (guide/models.md, _index.md). */
export const docFiles: Record<string, string> = Object.fromEntries(Object.entries(raw).map(([k, v]) => [k.replace(/^.*\/docs\/content\//, ""), v]));

/** The docs' pictures (static/images), by the path the site serves them at. */
export const docPictures: Record<string, string> = {
  "images/desktop.png": new URL("../../../../../docs/static/images/desktop.png", import.meta.url).href,
};
