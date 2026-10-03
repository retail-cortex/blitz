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
  return path.replace(/\.(md|markdown|mdx)$/i, "") + ".pdf";
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
}): Promise<string> {
  const copy = o.markdown.cloneNode(true) as HTMLElement;
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
  return pageHTML({ title: o.title, body: copy.outerHTML, css: windowCss(o.markdown.ownerDocument), userCss: o.userCss });
}
