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

// The HTML a document's Markdown may show drawn (spec_037_visual_editor
// VE-05a): formatting tags (<br />, <sub>, <kbd>, <details>, a centred
// <div>, a table) and their few harmless attributes. It's read here into
// a tree, never handed to the page as HTML: Preview makes React elements
// of it and the visual editor elements of its own, with text as text, so
// nothing in it can run, style the app or load from the web (DSK-24).
// HTML with anything else (a script, a style, an event handler's tag, a
// comment, an unknown element, tags that don't close) isn't shown drawn.

/** HTML read for drawing: text, or an element with its kept attributes. */
export type SafeNode = string | SafeElement;

/** An element of HTML read for drawing. */
export interface SafeElement {
  tag: string;
  attrs: Record<string, string>;
  children: SafeNode[];
}

// The tags drawn, each with the attributes it keeps (title on every one).
const align = ["align"];
const tags: Record<string, readonly string[]> = {
  a: ["href"],
  abbr: [],
  b: [],
  blockquote: [],
  br: [],
  caption: [],
  center: [],
  code: [],
  dd: [],
  del: [],
  details: ["open"],
  div: align,
  dl: [],
  dt: [],
  em: [],
  figcaption: [],
  figure: [],
  h1: align,
  h2: align,
  h3: align,
  h4: align,
  h5: align,
  h6: align,
  hr: [],
  i: [],
  img: ["src", "alt", "width", "height"],
  ins: [],
  kbd: [],
  li: [],
  mark: [],
  ol: ["start"],
  p: align,
  pre: [],
  q: [],
  s: [],
  samp: [],
  small: [],
  span: [],
  strike: [],
  strong: [],
  sub: [],
  summary: [],
  sup: [],
  table: [],
  tbody: [],
  td: ["align", "colspan", "rowspan"],
  tfoot: [],
  th: ["align", "colspan", "rowspan"],
  thead: [],
  tr: [],
  u: [],
  ul: [],
  var: [],
  wbr: [],
};

/** Elements that never hold anything (no closing tag). */
const voids = new Set(["br", "hr", "img", "wbr"]);

// What a kept attribute's value may be; the rest are dropped.
const values: Record<string, RegExp> = {
  align: /^(left|right|center|justify)$/i,
  colspan: /^\d{1,3}$/,
  rowspan: /^\d{1,3}$/,
  start: /^\d{1,9}$/,
  width: /^\d{1,5}(px|%)?$/,
  height: /^\d{1,5}(px|%)?$/,
};

// A tag: < or </, its name, its attributes (quoted values may hold >), and
// a / before > for a self-closing one. Anything else starting with < and a
// letter, !, ? or / isn't HTML this draws.
const tagRE = /<(\/?)([a-zA-Z][a-zA-Z0-9-]*)((?:\s+[^\s"'>/=]+(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'=<>`]+))?)*)\s*(\/?)>/y;
const attrRE = /([^\s"'>/=]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>`]+)))?/g;

/**
 * html read for drawing, or null when it holds anything this doesn't draw
 * (see the file's header) or its tags don't each close.
 */
export function safeHTML(html: string): SafeNode[] | null {
  const root: SafeElement = { tag: "", attrs: {}, children: [] };
  const open: SafeElement[] = [root];
  let text = "";
  const flush = () => {
    if (text) open[open.length - 1].children.push(decode(text));
    text = "";
  };
  for (let i = 0; i < html.length; ) {
    const lt = html.indexOf("<", i);
    if (lt < 0) {
      text += html.slice(i);
      break;
    }
    text += html.slice(i, lt);
    tagRE.lastIndex = lt;
    const m = tagRE.exec(html);
    if (!m) {
      // A < that starts no tag (2 < 3) is text; a comment, a doctype or a
      // broken tag isn't drawn.
      if (/[a-zA-Z!?/]/.test(html.charAt(lt + 1))) return null;
      text += "<";
      i = lt + 1;
      continue;
    }
    i = tagRE.lastIndex;
    const [, closing, rawName, rawAttrs, selfClosing] = m;
    const name = rawName.toLowerCase();
    if (!Object.hasOwn(tags, name)) return null;
    flush();
    if (closing) {
      if (voids.has(name)) continue; // </br>, as browsers read it
      const top = open.pop();
      if (!top || top === root || top.tag !== name) return null;
      continue;
    }
    const el: SafeElement = { tag: name, attrs: attributes(name, rawAttrs), children: [] };
    open[open.length - 1].children.push(el);
    if (!voids.has(name) && !selfClosing) open.push(el);
  }
  flush();
  if (open.length !== 1) return null;
  return root.children;
}

// The attributes name keeps, with values it may have.
function attributes(name: string, raw: string): Record<string, string> {
  const keep = tags[name];
  const out: Record<string, string> = {};
  for (const a of raw.matchAll(attrRE)) {
    const key = a[1].toLowerCase();
    if (key !== "title" && !keep.includes(key)) continue;
    const value = decode(a[2] ?? a[3] ?? a[4] ?? "");
    if (values[key] && !values[key].test(value)) continue;
    out[key] = value;
  }
  return out;
}

const named: Record<string, string> = { amp: "&", lt: "<", gt: ">", quot: '"', apos: "'", nbsp: " ", copy: "©", reg: "®", trade: "™", hellip: "…", mdash: "—", ndash: "–", middot: "·", times: "×", rarr: "→", larr: "←", check: "✓" };

// Text with HTML's character references read.
function decode(s: string): string {
  return s.replace(/&(#x[0-9a-f]{1,6}|#\d{1,7}|[a-z]+);/gi, (m, ref: string) => {
    if (ref[0] !== "#") return named[ref.toLowerCase()] ?? m;
    const code = ref[1] === "x" || ref[1] === "X" ? parseInt(ref.slice(2), 16) : parseInt(ref.slice(1), 10);
    return code > 0 && code <= 0x10ffff ? String.fromCodePoint(code) : m;
  });
}

/** The tag html opens when it's one opening tag of an element that closes (<sub>), else null. */
export function openingTag(html: string): string | null {
  const m = /^<([a-zA-Z][a-zA-Z0-9-]*)\b[^>]*>$/.exec(html.trim());
  if (!m || html.trim().endsWith("/>")) return null;
  const name = m[1].toLowerCase();
  return voids.has(name) ? null : name;
}

/** The tag html closes when it's one closing tag (</sub>), else null. */
export function closingTag(html: string): string | null {
  const m = /^<\/([a-zA-Z][a-zA-Z0-9-]*)\s*>$/.exec(html.trim());
  return m ? m[1].toLowerCase() : null;
}

/**
 * The index of the node closing the element nodes[i] opens, among its
 * siblings (Markdown reads "<sub>2</sub>" as an opening tag, text and a
 * closing tag), else -1.
 */
export function closingIndex(nodes: readonly { type: string; value?: string }[], i: number): number {
  const html = (n: { type: string; value?: string }) => (n.type === "html" ? (n.value ?? "") : "");
  const name = openingTag(html(nodes[i]));
  if (!name) return -1;
  let depth = 1;
  for (let j = i + 1; j < nodes.length; j++) {
    const v = html(nodes[j]);
    if (!v) continue;
    if (openingTag(v) === name) depth++;
    else if (closingTag(v) === name && --depth === 0) return j;
  }
  return -1;
}
