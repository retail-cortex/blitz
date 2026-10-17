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

// Web pages in the editor (spec_electron_038 EL-100): their tabs, the
// links that open in them, when the window's view of one must hide, and
// what Add to chat sends of one.
import type { WebBlock } from "../attachments";

/** A web tab's path in the editor's tabs: web: and its preview's ID. */
export const webPath = (id: string) => `web:${id}`;

/** Whether an editor tab's path is a web page's. */
export const isWebPath = (path: string) => path.startsWith("web:");

/** A web tab's preview ID, from its path. */
export const webId = (path: string) => path.slice("web:".length);

let next = 0;
/** A new preview ID, unique in this page. */
export const newWebId = () => `w${Date.now().toString(36)}-${(next++).toString(36)}`;

/** A web tab's name: the page's title, else its host and path. */
export function webTabName(url: string, title: string): string {
  if (title.trim()) return title.trim();
  try {
    const u = new URL(url);
    return u.host + (u.pathname === "/" ? "" : u.pathname);
  } catch {
    return url;
  }
}

/**
 * Whether a link is to a web server on this machine (localhost,
 * 127.0.0.1, [::1]), which opens in a web tab rather than the browser.
 */
export function isLocalLink(href: string): boolean {
  let u: URL;
  try {
    u = new URL(href);
  } catch {
    return false;
  }
  if (u.protocol !== "http:" && u.protocol !== "https:") return false;
  const h = u.hostname.toLowerCase();
  return h === "localhost" || h.endsWith(".localhost") || h === "127.0.0.1" || h === "[::1]";
}

/** A box on the window. */
export interface Box {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/** Whether any of others overlaps box (touching edges don't). */
export const covered = (box: Box, others: readonly Box[]) => others.some((o) => o.left < box.right && o.right > box.left && o.top < box.bottom && o.bottom > box.top);

/**
 * What draws over the page (a menu, a dialog, the palette, a drawer, a
 * snackbar, the composer's menus): the window's view of a web page is
 * drawn above all of the page, so it hides while one of these overlaps it.
 */
export const overlays = '.menu.floating, .scrim, [aria-modal="true"], .drawer:not([hidden]), .snackbar, .command-menu';

/** The largest page or selection Add to chat sends, as HTML (the service's limit). */
export const maxBlockHTML = 2 << 20;

/** What Add to chat sends of a page: all of it, or the selection. */
export function pageBlock(c: { url: string; title: string; html: string; selection: string }, which: "page" | "selection"): WebBlock {
  const html = which === "selection" ? c.selection : c.html;
  return { kind: which === "selection" ? "web_selection" : "web_page", url: c.url, title: c.title, html: html.slice(0, maxBlockHTML), text: "" };
}

/** What Add to chat sends of a page's console: its errors, the latest last. */
export function consoleBlock(url: string, title: string, lines: readonly string[]): WebBlock {
  return { kind: "console", url, title, html: "", text: lines.join("\n") };
}
