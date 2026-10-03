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

// What the editor can show other than as text: Markdown rendered, images
// and PDFs, and sound to play.

/**
 * How a file previews: markdown and svg are text with a rendered view
 * beside the source; image, pdf and audio show only as a preview (audio as
 * a player); null has none.
 */
export type PreviewKind = "markdown" | "svg" | "image" | "pdf" | "audio" | null;

/** The preview a file has, by its name. */
export function previewKind(path: string): PreviewKind {
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
  if (path.lastIndexOf(".") <= path.lastIndexOf("/")) return null;
  if (["md", "markdown", "mdx"].includes(ext)) return "markdown";
  if (ext === "svg") return "svg";
  if (["png", "jpg", "jpeg", "gif", "webp", "bmp", "ico"].includes(ext)) return "image";
  if (ext === "pdf") return "pdf";
  if (["wav", "mp3", "m4a", "aac", "ogg", "opus", "flac"].includes(ext)) return "audio";
  return null;
}

/** Whether a kind shows only as a preview (it has no text to edit). */
export const previewOnly = (k: PreviewKind) => k === "image" || k === "pdf" || k === "audio";

/** How a file with a preview shows: as its text, or rendered. */
export type View = "source" | "preview";

/**
 * How a file shows when it opens: Markdown rendered, unless it opened at a
 * line (a search hit, a link to one) or is empty (a new file, to write);
 * an SVG as its source; images and PDFs only as previews.
 */
export function defaultView(k: PreviewKind, opened: { atLine: boolean; empty: boolean }): View {
  if (previewOnly(k)) return "preview";
  return k === "markdown" && !opened.atLine && !opened.empty ? "preview" : "source";
}
