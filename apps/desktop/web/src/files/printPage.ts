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

// Export as PDF (spec_desktop_024): a Markdown file's preview, as it's
// shown (highlighted code, Mermaid diagrams), made a page that the app's
// own web engine prints (PrintPDF): its pictures inline, styled by the
// window's CSS in the light theme, the user's light document CSS, and
// rules for paper.
import { resolveLink } from "./mdlinks";

/** The paper for a locale: Letter where its country uses it, else A4 (as the export_pdf tool's [pdf] page_size = "auto"). */
export function paperFor(locale: string): "A4" | "Letter" {
  let region = "";
  try {
    region = new Intl.Locale(locale).maximize().region ?? "";
  } catch {
    // not a locale: A4
  }
  return ["US", "CA", "MX", "PH", "CL", "CO", "VE", "PR"].includes(region) ? "Letter" : "A4";
}

/** The PDF's name for a Markdown file: the same name with .pdf. */
export function pdfPath(path: string): string {
  return exportPath(path, ".pdf");
}

/** An export's name for a Markdown file: the same name with ext (".docx"). */
export function exportPath(path: string, ext: string): string {
  return path.replace(/\.(md|markdown|mdx)$/i, "") + ext;
}

/** Rules for paper over the window's CSS: no app chrome, a white page, nothing cut across pages. */
export const printCss = `
html, body, #root { height: auto !important; overflow: visible !important; background: #fff !important; }
body { margin: 0; }
.preview-markdown { background: #fff !important; }
.preview-markdown .markdown { max-width: none !important; margin: 0 !important; padding: 0 !important; }
.heading-anchor, .link-icon, .code-head button { display: none !important; }
h1, h2, h3, h4, h5, h6 { break-after: avoid; }
pre, table, img, svg, .code-block, .mermaid-block, blockquote { break-inside: avoid; }
img { max-width: 100%; }
`;

const escapeHTML = (s: string) => s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]!);

/** The whole page to print: the preview's HTML under the window's CSS, then the user's, then printCss. */
export function pageHTML(o: { title: string; body: string; css: string; userCss: string }): string {
  // </style> can't end a style element early.
  const style = (css: string) => `<style>${css.replace(/<\/style/gi, "<\\/style")}</style>`;
  return (
    `<!doctype html><html data-theme="light"><head><meta charset="utf-8"><title>${escapeHTML(o.title)}</title>` +
    style(o.css) +
    style(o.userCss) +
    style(printCss) +
    `</head><body><div class="preview preview-markdown">${o.body}</div></body></html>`
  );
}

/** Reads a workspace file's bytes and media type (FileService.ReadPreview). */
export type ReadPicture = (path: string) => Promise<{ mime: string; data: Uint8Array }>;

/**
 * The URL a picture in the document prints from: a web or data URL as
 * written, a workspace file inline as a data URL, or "" when it can't be
 * shown (outside the workspace, unreadable).
 */
export async function pictureURL(src: string, docPath: string, read: ReadPicture): Promise<string> {
  const to = resolveLink(src, docPath);
  if (to.kind === "url") return /^(https?|data):/i.test(to.href) ? to.href : "";
  if (to.kind !== "path" || to.folder) return "";
  try {
    const { mime, data } = await read(to.path);
    if (!mime.startsWith("image/")) return "";
    let bin = "";
    for (let i = 0; i < data.length; i += 0x8000) bin += String.fromCharCode(...data.subarray(i, i + 0x8000));
    return `data:${mime};base64,${btoa(bin)}`;
  } catch {
    return "";
  }
}

/**
 * The window's own CSS, as text: not the user's document CSS for the
 * theme in effect (#user-doc-css; the page gets the light theme's), nor
 * sheets from another origin.
 */
export function windowCss(doc: Document): string {
  let out = "";
  for (const sheet of Array.from(doc.styleSheets)) {
    if ((sheet.ownerNode as Element | null)?.id === "user-doc-css") continue;
    try {
      for (const rule of Array.from(sheet.cssRules)) out += rule.cssText + "\n";
    } catch {
      // another origin's sheet
    }
  }
  return out;
}

/** Fetches a font of the app's own (tests replace it). */
export type FetchFont = (url: string) => Promise<ArrayBuffer>;
const fetchFont: FetchFont = (url) => fetch(url).then((r) => (r.ok ? r.arrayBuffer() : Promise.reject(new Error(`${r.status} ${url}`))));

/**
 * css with KaTeX's fonts put in, as data: URLs (their WOFF2 only): the
 * printed page has no base URL, so its fonts' relative URLs (resolved here
 * against base) would load nothing and math would print in other fonts
 * (#21). A font that can't be read keeps its rule as it was.
 */
export async function withMathFonts(css: string, base: string, fetch: FetchFont = fetchFont): Promise<string> {
  const faces = [...css.matchAll(/@font-face\s*\{[^}]*\}/g)].filter((m) => /font-family:\s*["']?KaTeX_/.test(m[0]));
  let out = css;
  for (const m of faces) {
    const woff2 = /url\(\s*["']?([^"')]+\.woff2)["']?\s*\)/.exec(m[0]);
    if (!woff2) continue;
    try {
      const data = new Uint8Array(await fetch(new URL(woff2[1], base).href));
      let bin = "";
      for (let i = 0; i < data.length; i += 0x8000) bin += String.fromCharCode(...data.subarray(i, i + 0x8000));
      const face = m[0].replace(/src:[^;}]*(;|(?=\}))/, `src: url(data:font/woff2;base64,${btoa(bin)}) format("woff2")$1`);
      out = out.replace(m[0], face);
    } catch {
      // that font prints as it would have
    }
  }
  return out;
}

/**
 * The page to print for a rendered preview (its .markdown element): a
 * copy, with each picture (the preview shows them as links, marked
 * .md-image) put in, and with redraw, each Mermaid diagram drawn again
 * (a dark window's in light colours).
 */
export async function printablePage(o: {
  markdown: HTMLElement;
  docPath: string;
  title: string;
  userCss: string;
  read: ReadPicture;
  redraw?: (source: string) => Promise<string>;
  fetchFont?: FetchFont;
}): Promise<string> {
  const copy = o.markdown.cloneNode(true) as HTMLElement;
  // Diagrams print as drawn, not as zoomed in the window (panzoom.ts).
  for (const svg of Array.from(copy.querySelectorAll<SVGElement>(".mermaid-diagram > svg"))) svg.style.transform = "";
  if (o.redraw) {
    for (const block of Array.from(copy.querySelectorAll<HTMLElement>(".mermaid-block[data-source]"))) {
      const diagram = block.querySelector(".mermaid-diagram");
      if (!diagram) continue; // showing its source
      try {
        diagram.innerHTML = await o.redraw(block.dataset.source ?? ""); // Mermaid's strict, sanitised SVG
      } catch {
        // keep the window's drawing
      }
    }
  }
  for (const el of Array.from(copy.querySelectorAll<HTMLElement>(".md-image"))) {
    const url = await pictureURL(el.dataset.src ?? "", o.docPath, o.read);
    if (!url) continue;
    const img = copy.ownerDocument.createElement("img");
    img.src = url;
    img.alt = el.dataset.alt ?? "";
    el.replaceWith(img);
  }
  let css = windowCss(o.markdown.ownerDocument);
  if (copy.querySelector(".katex")) css = await withMathFonts(css, o.markdown.ownerDocument.baseURI, o.fetchFont);
  return pageHTML({ title: o.title, body: copy.outerHTML, css, userCss: o.userCss });
}
