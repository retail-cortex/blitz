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

// A web page as Markdown, for Save to workspace (spec_electron_038
// EL-106): the visual editor's writer, so it reads as the editor writes.
// Loaded only when a page is saved as Markdown (it brings TipTap).
import { generateJSON, getSchema } from "@tiptap/core";
import { Node as PMNode } from "@tiptap/pm/model";
import { serializeMarkdown } from "./visual/markdown";
import { visualExtensions } from "./visual/schema";
import { localDay } from "./webSave";

/**
 * The page's main content (main, else article, else the body) without
 * scripts, styles, forms or navigation, its links and pictures made
 * absolute. Needs a DOM (the window's).
 */
function mainContent(html: string, url: string): HTMLElement {
  const doc = new DOMParser().parseFromString(html, "text/html");
  const root = doc.querySelector("main") ?? doc.querySelector("article") ?? doc.body;
  root.querySelectorAll("script, style, noscript, template, iframe, svg, canvas, form, button, input, select, textarea, nav").forEach((el) => el.remove());
  const absolute = (el: Element, attr: string) => {
    const v = el.getAttribute(attr);
    if (v === null) return;
    try {
      el.setAttribute(attr, new URL(v, url).href);
    } catch {
      el.removeAttribute(attr);
    }
  };
  root.querySelectorAll("a[href]").forEach((a) => absolute(a, "href"));
  root.querySelectorAll("img[src]").forEach((img) => absolute(img, "src"));
  return root;
}

/** The web pictures (http, https) the page's Markdown shows, each once, in order. */
export function pagePictures(html: string, url: string): string[] {
  const srcs = Array.from(mainContent(html, url).querySelectorAll("img[src]"), (img) => img.getAttribute("src") ?? "");
  return [...new Set(srcs.filter((s) => /^https?:\/\//i.test(s)))];
}

/**
 * The page as Markdown, as the visual editor writes it: its main content
 * (mainContent), with where it came from under its first heading (the
 * page's title, when it starts without one). A picture saved beside it
 * (local: its web address to its path, relative to the Markdown) shows
 * from there; the rest from the site. Needs a DOM (the window's).
 */
export function pageMarkdown(html: string, url: string, title: string, when = new Date(), local: ReadonlyMap<string, string> = new Map()): string {
  const root = mainContent(html, url);
  root.querySelectorAll("img[src]").forEach((img) => {
    const path = local.get(img.getAttribute("src") ?? "");
    if (path) img.setAttribute("src", path);
  });
  const extensions = visualExtensions();
  const body = PMNode.fromJSON(getSchema(extensions), generateJSON(root.innerHTML, extensions));
  const markdown = serializeMarkdown(body, { sources: new Map(), trailing: "" }, new Map()).trim();
  const note = `Saved from <${url}> on ${localDay(when)}.`;
  const first = markdown.match(/^# .*(\n|$)/);
  if (first) return `${first[0].trimEnd()}\n\n${note}\n\n${markdown.slice(first[0].length).trim()}\n`.replace(/\n\n\n$/, "\n");
  return `${title.trim() ? `# ${title.trim()}\n\n` : ""}${note}\n\n${markdown}\n`;
}
