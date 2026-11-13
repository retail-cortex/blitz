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

// A document's HTML drawn in the visual editor (VE-05a): elements made
// one by one from what safeHtml.ts kept, text as text. A link doesn't go
// anywhere (the editor's never do); a picture shows as the document's
// others do, from the document or the workspace only.
import type { Node as PMNode } from "@tiptap/pm/model";
import { pictureURL } from "../pictures";
import { safeHTML, type SafeNode } from "../safeHtml";

/** The document whose pictures a drawing shows. */
export interface HtmlDoc {
  dir: string;
  path: string;
}

/** nodes as elements, in a fragment. */
export function drawHTML(nodes: SafeNode[], doc: HtmlDoc): DocumentFragment {
  const out = document.createDocumentFragment();
  for (const n of nodes) out.append(drawNode(n, doc));
  return out;
}

function drawNode(n: SafeNode, doc: HtmlDoc): Node {
  if (typeof n === "string") return document.createTextNode(n);
  if (n.tag === "img") return picture(n.attrs, doc);
  const el = document.createElement(n.tag);
  for (const [k, v] of Object.entries(n.attrs)) {
    if (k === "href") el.title = v;
    else el.setAttribute(k, v);
  }
  if (n.tag === "details") el.setAttribute("open", ""); // its content, to read and see
  for (const c of n.children) el.append(drawNode(c, doc));
  return el;
}

// A picture: its box (alt or src) until the picture is read.
function picture(attrs: Record<string, string>, doc: HtmlDoc): HTMLElement {
  const box = document.createElement("span");
  const src = attrs.src ?? "";
  box.className = "md-image";
  box.textContent = attrs.alt || src;
  box.title = attrs.alt || src;
  if (!src) return box;
  void pictureURL(doc.dir, doc.path, src).then((url) => {
    if (!url) return;
    const img = document.createElement("img");
    img.src = url;
    img.alt = attrs.alt ?? "";
    if (attrs.width) img.style.width = /^\d+$/.test(attrs.width) ? `${attrs.width}px` : attrs.width;
    if (attrs.height) img.style.height = /^\d+$/.test(attrs.height) ? `${attrs.height}px` : attrs.height;
    img.draggable = false;
    box.className = "md-image shown";
    box.replaceChildren(img);
  });
  return box;
}

/**
 * An inline piece of HTML in the document: drawn when safeHtml.ts can
 * (its source as the tooltip), else its source in a chip, as before.
 */
export function rawInlineView(first: PMNode, doc: HtmlDoc) {
  const dom = document.createElement("span");
  dom.contentEditable = "false";
  let node = first;
  const show = () => {
    const text = (node.attrs.text as string) ?? "";
    const nodes = safeHTML(text);
    dom.title = text;
    if (nodes) {
      dom.className = "raw-html";
      dom.removeAttribute("data-raw-inline");
      dom.replaceChildren(drawHTML(nodes, doc));
      return;
    }
    const code = document.createElement("code");
    code.className = "raw-inline";
    code.dataset.rawInline = "";
    code.textContent = text;
    dom.className = "";
    dom.replaceChildren(code);
  };
  show();
  return {
    dom,
    update: (next: PMNode) => {
      if (next.type !== node.type) return false;
      const changed = next.attrs.text !== node.attrs.text;
      node = next;
      if (changed) show();
      return true;
    },
  };
}
