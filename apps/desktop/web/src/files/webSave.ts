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

// Saving a web page into the workspace (spec_electron_038 EL-106): where
// it goes, and the page as HTML (Markdown is webMarkdown.ts's, loaded when
// it's asked for; a PDF is the window's).

/** What a page can be saved as. */
export type SaveFormat = "md" | "html" | "pdf";

/** A file name for a page: its title (else its host and path) made safe, without an extension. */
export function saveName(url: string, title: string): string {
  let base = title.trim();
  if (!base) {
    try {
      const u = new URL(url);
      base = (u.hostname + u.pathname).replace(/\/+$/, "");
    } catch {
      base = "";
    }
  }
  base = base
    .replace(/[\\/:*?"<>|\u0000-\u001f]+/g, "-")
    .replace(/\s+/g, " ")
    .replace(/^[.\s-]+|[.\s-]+$/g, "")
    .slice(0, 80);
  return base || "page";
}

/**
 * Where the page goes: name (its extension added) in folder, both relative
 * to the workspace; an error key when either can't be (outside it, or
 * empty).
 */
export function savePath(folder: string, name: string, format: SaveFormat): { path: string } | { error: string } {
  const parts = folder.trim().replace(/\\/g, "/").split("/").filter((p) => p !== "" && p !== ".");
  if (folder.trim().startsWith("/") || /^[a-z]:/i.test(folder.trim()) || parts.includes("..")) return { error: "desktop.web.save.outside" };
  let file = name.trim();
  if (!file || file.includes("/") || file.includes("\\") || file === "." || file === "..") return { error: "desktop.web.save.bad_name" };
  if (!file.toLowerCase().endsWith(`.${format}`)) file += `.${format}`;
  return { path: [...parts, file].join("/") };
}

/** A day as the user's calendar has it (yyyy-mm-dd). */
export const localDay = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

/**
 * The page's HTML to keep: with a base so its relative links and pictures
 * still reach the site, and a note of where it came from.
 */
export function pageHTML(html: string, url: string, when = new Date()): string {
  const note = `<!-- Saved by Blitz from ${url.replace(/--/g, "%2D%2D")} on ${localDay(when)} -->`;
  const base = `<base href="${url.replace(/&/g, "&amp;").replace(/"/g, "&quot;")}">`;
  if (/<base\s/i.test(html)) return `${note}\n${html}`;
  const head = html.match(/<head(\s[^>]*)?>/i);
  if (head) return `${note}\n${html.slice(0, head.index! + head[0].length)}${base}${html.slice(head.index! + head[0].length)}`;
  return `${note}\n${base}\n${html}`;
}
