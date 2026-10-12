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

// Pictures in Markdown (spec_files_029 FIL-65): shown when they're in the
// document (a data: image) or in the workspace (read through the service,
// shown from memory), never fetched from the web (DSK-24): a web picture
// stays a link. Preview, Visual and the chat share this.
import { files } from "../api";
import { resolveLink } from "./mdlinks";

// The image types a data: URL may carry. An SVG shown by <img> runs no
// script and loads nothing.
const dataImage = /^data:image\/(png|jpe?g|gif|webp|avif|bmp|svg\+xml)(;[a-z0-9=._+-]+)*,/i;

/** Whether src is a data: URL of an image type Blitz shows. */
export function isDataImage(src: string): boolean {
  return dataImage.test(src.trim());
}

/** Where a picture comes from: the document itself, a workspace file, the web, or nowhere Blitz shows. */
export type PictureSource = { kind: "data"; url: string } | { kind: "file"; path: string } | { kind: "web"; url: string } | { kind: "none" };

/** Where src (as written in the document at docPath; null for the chat) comes from. */
export function pictureSource(src: string, docPath: string | null): PictureSource {
  const s = src.trim();
  if (isDataImage(s)) return { kind: "data", url: s };
  if (!s || docPath === null) return /^https?:/i.test(s) ? { kind: "web", url: s } : { kind: "none" };
  const to = resolveLink(s, docPath);
  if (to.kind === "url") return /^https?:/i.test(to.href) ? { kind: "web", url: to.href } : { kind: "none" };
  if (to.kind === "path" && !to.folder) return { kind: "file", path: to.path };
  return { kind: "none" };
}

/** A picture written as one HTML <img> tag: what Blitz keeps of it. */
export interface HtmlPicture {
  src: string;
  alt: string;
  width?: string;
  height?: string;
}

const lonelyImg = /^\s*<img\b((?:\s+[\w:-]+(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'=<>`]+))?)*)\s*\/?>\s*$/i;
const attribute = /([\w:-]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>`]+)))?/g;
const size = /^\d{1,5}(px|%)?$/;

/**
 * The picture in html when html is nothing but one <img> tag (raw HTML
 * is otherwise never rendered, DSK-24): its src and alt, and a width and
 * height that are plain sizes. Every other attribute is dropped.
 */
export function htmlPicture(html: string): HtmlPicture | null {
  const m = lonelyImg.exec(html);
  if (!m) return null;
  const attrs = new Map<string, string>();
  for (const a of m[1].matchAll(attribute)) attrs.set(a[1].toLowerCase(), decodeEntities(a[2] ?? a[3] ?? a[4] ?? ""));
  const src = attrs.get("src");
  if (!src) return null;
  const out: HtmlPicture = { src, alt: attrs.get("alt") ?? "" };
  const w = attrs.get("width"), h = attrs.get("height");
  if (w && size.test(w)) out.width = w;
  if (h && size.test(h)) out.height = h;
  return out;
}

function decodeEntities(s: string): string {
  return s.replace(/&(amp|lt|gt|quot|#39);/g, (_m, e: string) => ({ amp: "&", lt: "<", gt: ">", quot: '"', "#39": "'" })[e] ?? _m);
}

// Workspace pictures read once, kept as object URLs (the latest 64).
const cache = new Map<string, Promise<string>>();
const cacheSize = 64;

/** Reads a workspace picture; tests replace it. */
export let readPicture: (dir: string, path: string) => Promise<{ mime: string; data: Uint8Array }> = (dir, path) => files.readPreview({ workspace: dir, path });

/** For tests: replaces how pictures are read, and forgets what was read. */
export function setReadPicture(read: typeof readPicture) {
  readPicture = read;
  cache.clear();
}

/**
 * A URL the page can show src from (written in the document at docPath,
 * in the workspace dir): the data: URL itself, a workspace image from
 * memory, or "" (a web picture, a file that isn't an image or can't be
 * read).
 */
export function pictureURL(dir: string, docPath: string | null, src: string): Promise<string> {
  const from = pictureSource(src, docPath);
  if (from.kind === "data") return Promise.resolve(from.url);
  if (from.kind !== "file") return Promise.resolve("");
  const key = `${dir}\u0000${from.path}`;
  let url = cache.get(key);
  if (!url) {
    url = readPicture(dir, from.path).then(
      ({ mime, data }) => (mime.startsWith("image/") ? URL.createObjectURL(new Blob([data as BlobPart], { type: mime })) : ""),
      () => "",
    );
    cache.set(key, url);
    if (cache.size > cacheSize) {
      const [oldest, gone] = cache.entries().next().value as [string, Promise<string>];
      cache.delete(oldest);
      void gone.then((u) => u && URL.revokeObjectURL(u));
    }
  }
  return url;
}

