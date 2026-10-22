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

// The user docs as pages (spec_desktop_024 DSK-103): the site's Markdown
// files (Hugo's content tree) read the way the site reads them, so the app
// shows the docs of its own release. A page's front matter gives its title
// and weight (the navigation's order); its links are relative .md paths
// (the site's render hook turns them into pages), which stay in the viewer
// when the page is one the app carries and open the website otherwise.

/** The website, where the docs the app doesn't carry are (and the latest of all of them). */
export const siteURL = "https://retail-cortex.github.io/blitz/";

/** One page of the docs. */
export interface DocPage {
  /** Its path on the site: "guide/models", "guide" for guide/_index.md, "" for the home page. */
  path: string;
  /** Its file in docs/content. */
  file: string;
  title: string;
  weight: number;
  /** The Markdown, without the front matter. */
  body: string;
  /** Its headings' text, for search. */
  headings: string[];
}

/** A page and the pages under it (a section's), in the site's order. */
export interface DocNode {
  page: DocPage;
  children: DocNode[];
}

/** Where a link in a page goes. */
export type DocTarget = { kind: "page"; path: string; anchor?: string } | { kind: "url"; href: string } | { kind: "anchor"; id: string };

/** A page's site path from its file: guide/models.md is guide/models, guide/_index.md is guide. */
export function pathOf(file: string): string {
  return file.replace(/\.md$/, "").replace(/(^|\/)_index$/, "");
}

/** The page in a Markdown file: its front matter's title and weight, and the rest. */
export function parsePage(file: string, text: string): DocPage {
  const m = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/.exec(text);
  const front = m?.[1] ?? "";
  const field = (name: string) => new RegExp(`^${name}:\\s*(.*)$`, "m").exec(front)?.[1]?.trim().replace(/^(["'])(.*)\1$/, "$2");
  const body = m ? text.slice(m[0].length) : text;
  const path = pathOf(file);
  const headings = [...body.matchAll(/^#{1,6}\s+(.+?)\s*#*\s*$/gm)].map((h) => h[1]);
  const weight = Number(field("weight"));
  return { path, file, title: field("title") || headings[0] || path, weight: Number.isFinite(weight) ? weight : 0, body, headings };
}

const byOrder = (a: DocPage, b: DocPage) => a.weight - b.weight || a.title.localeCompare(b.title);

/** The navigation: the home page, its sections and their pages, each level in the site's order (weight, then title). */
export function docTree(pages: DocPage[]): DocNode {
  const byPath = new Map(pages.map((p) => [p.path, p]));
  const home = byPath.get("") ?? { path: "", file: "_index.md", title: "", weight: 0, body: "", headings: [] };
  const node = (page: DocPage): DocNode => ({
    page,
    children: pages
      .filter((p) => p.path !== page.path && parent(p.path) === page.path)
      .sort(byOrder)
      .map(node),
  });
  return node(home);
}

const parent = (path: string) => (path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : path === "" ? null : "");

// A relative path resolved from a folder ("" the content root); null when
// it climbs above the root.
function resolveFrom(dir: string, rel: string): string | null {
  const out = dir ? dir.split("/") : [];
  for (const part of rel.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      if (out.length === 0) return null;
      out.pop();
    } else out.push(part);
  }
  return out.join("/");
}

/** The website's address of a site path (and a heading on it). */
export function siteURLOf(path: string, anchor?: string): string {
  return siteURL + (path ? `${path.replace(/\/$/, "")}/` : "") + (anchor ? `#${anchor}` : "");
}

/**
 * Where a link in page goes: another page the app carries (by its .md
 * path, as the site's render hook takes it), a heading, or the website (a
 * page the app doesn't carry, a file the site serves, the web).
 */
export function docLink(page: DocPage, href: string, carried: Set<string>): DocTarget {
  if (/^[a-z][a-z0-9+.-]*:/i.test(href)) return { kind: "url", href };
  if (href.startsWith("#")) return { kind: "anchor", id: href.slice(1) };
  const [p, anchor] = href.split("#", 2);
  if (p.startsWith("/")) return { kind: "url", href: siteURL + p.slice(1) + (anchor ? `#${anchor}` : "") };
  if (/\.md$/.test(p)) {
    const fileDir = page.file.includes("/") ? page.file.slice(0, page.file.lastIndexOf("/")) : "";
    const target = resolveFrom(fileDir, p);
    if (target === null) return { kind: "url", href: siteURL };
    const path = pathOf(target);
    if (carried.has(path)) return path === page.path && anchor ? { kind: "anchor", id: anchor } : { kind: "page", path, anchor };
    return { kind: "url", href: siteURLOf(path, anchor) };
  }
  // Anything else is relative to the page's own address, as a browser takes it.
  const target = resolveFrom(page.path, p);
  if (target !== null && carried.has(target.replace(/\/$/, ""))) return { kind: "page", path: target.replace(/\/$/, ""), anchor };
  return { kind: "url", href: siteURL + (target ?? "") + (anchor ? `#${anchor}` : "") };
}

/**
 * A picture in page from the pictures the app carries (by the site path it's
 * served at), if it's one. In dark mode a picture's dark twin (name-dark.png,
 * as the desktop app's screenshots have) takes its place, as on the site.
 */
export function docPicture(page: DocPage, src: string, pictures: Record<string, string>, dark = false): string | undefined {
  if (/^[a-z]+:/i.test(src)) return undefined;
  const target = src.startsWith("/") ? src.slice(1) : resolveFrom(page.path, src);
  if (target === null) return undefined;
  const twin = dark && !target.endsWith("-dark.png") ? pictures[target.replace(/\.png$/, "-dark.png")] : undefined;
  return twin ?? pictures[target];
}

/** A search hit: the page, and a line of its text around the match. */
export interface DocHit {
  page: DocPage;
  snippet: string;
}

const plain = (s: string) =>
  s
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/[`*_>#|]/g, "")
    .replace(/\s+/g, " ")
    .trim();

/**
 * The pages with every word of query, best first: a word in the title
 * counts most, then in a heading, then in the text.
 */
export function searchDocs(pages: DocPage[], query: string, limit = 20): DocHit[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return [];
  const hits: { page: DocPage; score: number; snippet: string }[] = [];
  for (const page of pages) {
    const title = page.title.toLowerCase();
    const heads = page.headings.join(" ").toLowerCase();
    const text = plain(page.body);
    const lower = text.toLowerCase();
    if (!words.every((w) => title.includes(w) || heads.includes(w) || lower.includes(w))) continue;
    const score = words.reduce((s, w) => s + (title.includes(w) ? 10 : 0) + (heads.includes(w) ? 4 : 0) + (lower.includes(w) ? 1 : 0), 0);
    const at = Math.max(0, lower.indexOf(words[0]));
    const from = Math.max(0, at - 50);
    const snippet = (from > 0 ? "…" : "") + text.slice(from, from + 140).trim() + (from + 140 < text.length ? "…" : "");
    hits.push({ page, score, snippet });
  }
  return hits
    .sort((a, b) => b.score - a.score || byOrder(a.page, b.page))
    .slice(0, limit)
    .map(({ page, snippet }) => ({ page, snippet }));
}

/** The docs from their files: the pages by path, and the navigation. */
export function loadDocs(files: Record<string, string>): { pages: Map<string, DocPage>; tree: DocNode } {
  const list = Object.entries(files).map(([file, text]) => parsePage(file, text));
  return { pages: new Map(list.map((p) => [p.path, p])), tree: docTree(list) };
}

/** The release shortcode in a page (docs/layouts/shortcodes/release.html). */
const releaseShortcode = /\{\{<\s*release\s*>\}\}/g;

/**
 * The pages with the release shortcode filled in. On the site it's the
 * latest release; the docs the app carries are its release's, so it's the
 * app's version (0.6.2 is v0.6.2), or vX.Y.Z when the app isn't a release
 * (a development build, a commit), as a local build of the site shows.
 */
export function withRelease(files: Record<string, string>, appVersion: string): Record<string, string> {
  const tag = /^\d+\.\d+\.\d+(-rc\.\d+)?$/.test(appVersion) ? `v${appVersion}` : "vX.Y.Z";
  return Object.fromEntries(Object.entries(files).map(([path, text]) => [path, text.replace(releaseShortcode, tag)]));
}
