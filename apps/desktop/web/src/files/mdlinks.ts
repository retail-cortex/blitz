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

// Where a Markdown link goes (spec_files_029 FIL-56): web and mail links
// to the browser, #anchors within the document, and relative paths to the
// workspace file or folder they name, resolved against the document's own
// folder. Nothing outside the workspace opens.

/** What a link in a Markdown document points at. */
export type LinkTarget =
  | { kind: "url"; href: string }
  | { kind: "anchor"; id: string }
  | { kind: "path"; path: string; folder: boolean; line?: number; anchor?: string }
  | { kind: "outside" };

/**
 * Where href goes from the document at docPath (workspace-relative; null
 * when the text isn't a file, as in the chat, where paths start at the
 * workspace's root).
 */
export function resolveLink(href: string, docPath: string | null): LinkTarget {
  const h = href.trim();
  if (/^[a-z][a-z0-9+.-]*:/i.test(h) || h.startsWith("//")) return { kind: "url", href: h };
  if (h.startsWith("#")) return { kind: "anchor", id: decode(h.slice(1)) };
  const hash = h.indexOf("#");
  let rel = decode((hash < 0 ? h : h.slice(0, hash)).replace(/\?.*$/, ""));
  const frag = hash < 0 ? "" : decode(h.slice(hash + 1));
  const folder = rel.endsWith("/");
  const base = rel.startsWith("/") || docPath === null ? [] : docPath.split("/").slice(0, -1);
  rel = rel.replace(/^\/+/, "");
  const parts = [...base];
  for (const p of rel.split("/")) {
    if (p === "" || p === ".") continue;
    if (p === "..") {
      if (parts.length === 0) return { kind: "outside" };
      parts.pop();
    } else parts.push(p);
  }
  const target: LinkTarget = { kind: "path", path: parts.join("/"), folder: folder || parts.length === 0 };
  const line = /^L(\d+)(?:-L?\d+)?$/.exec(frag);
  if (line) target.line = Number(line[1]);
  else if (frag) target.anchor = frag;
  return target;
}

function decode(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}

/** A heading's id, as GitHub makes it: lower case, punctuation dropped, spaces to dashes. */
export function slug(text: string): string {
  return text
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s_-]/gu, "")
    .replace(/\s/g, "-");
}

/** Ids for a document's headings in order: a repeated one gets -1, -2, … */
export function slugger(): (text: string) => string {
  const seen = new Map<string, number>();
  return (text) => {
    const s = slug(text);
    const n = seen.get(s);
    seen.set(s, (n ?? -1) + 1);
    return n === undefined ? s : `${s}-${n + 1}`;
  };
}
