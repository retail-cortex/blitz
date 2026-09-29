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

// What the editor can show other than as text: Markdown rendered, and
// images and PDFs.

/**
 * How a file previews: markdown and svg are text with a rendered view
 * beside the source; image and pdf show only as a preview; null has none.
 */
export type PreviewKind = "markdown" | "svg" | "image" | "pdf" | null;

/** The preview a file has, by its name. */
export function previewKind(path: string): PreviewKind {
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
  if (path.lastIndexOf(".") <= path.lastIndexOf("/")) return null;
  if (["md", "markdown", "mdx"].includes(ext)) return "markdown";
  if (ext === "svg") return "svg";
  if (["png", "jpg", "jpeg", "gif", "webp", "bmp", "ico"].includes(ext)) return "image";
  if (ext === "pdf") return "pdf";
  return null;
}

/** Whether a kind shows only as a preview (it has no text to edit). */
export const previewOnly = (k: PreviewKind) => k === "image" || k === "pdf";
