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


// Markdown in and out of the visual editor (spec_visual_editor_037 VE-03 –
// VE-05). Reading keeps each top-level block's source and the text before
// it; writing puts an untouched block's source back as it was and prints
// only changed blocks, in one house style. So a file opened and saved
// without a change is the same bytes, and an edit rewrites only what it
// touched.
import type { JSONContent } from "@tiptap/core";
import type { Node as PMNode } from "@tiptap/pm/model";
import type * as md from "mdast";
import { fromMarkdown } from "mdast-util-from-markdown";
import { gfmFromMarkdown } from "mdast-util-gfm";
import { gfm } from "micromark-extension-gfm";
import { sourceAttr } from "./schema";

/** A block's source, and the text between it and the block before. */
export interface Source {
  source: string;
  before: string;
  /** It was the file's first block. */
  first: boolean;
}

/** A file read for the visual editor. */
export interface Parsed {
  doc: JSONContent;
  sources: Map<string, Source>;
  /** The text after the last block. */
  trailing: string;
}

type Align = "left" | "right" | "center" | null;

// Front matter: YAML between --- lines, or TOML between +++ (Hugo), at the
// very top.
const frontMatter = /^(---|\+\+\+)\r?\n[\s\S]*?\r?\n\1[ \t]*(\r?\n|$)/;

/** Reads text as the visual editor's document. */
export function parseMarkdown(text: string): Parsed {
  const sources = new Map<string, Source>();
  const content: JSONContent[] = [];
  let end = 0;
  let n = 0;
  const add = (node: JSONContent, start: number, stop: number) => {
    const id = `s${n++}`;
    node.attrs = { ...node.attrs, [sourceAttr]: id };
    sources.set(id, { source: text.slice(start, stop), before: text.slice(end, start), first: content.length === 0 });
    content.push(node);
    end = stop;
  };
  const fm = frontMatter.exec(text);
  let offset = 0;
  if (fm) {
    const stop = fm[0].endsWith("\n") ? fm[0].length - (fm[0].endsWith("\r\n") ? 2 : 1) : fm[0].length;
    add({ type: "rawBlock", attrs: { text: text.slice(0, stop), kind: "frontmatter" } }, 0, stop);
    offset = stop;
  }
  const body = text.slice(offset);
  const tree = fromMarkdown(body, { extensions: [gfm()], mdastExtensions: [gfmFromMarkdown()] });
  const slice = (node: md.Node) => body.slice(node.position?.start.offset ?? 0, node.position?.end.offset ?? 0);
  for (const child of tree.children) {
    const start = offset + (child.position?.start.offset ?? 0);
    const stop = offset + (child.position?.end.offset ?? 0);
    add(block(child, slice), start, stop);
  }
  // A document has a block at least: an empty file, a paragraph to write in.
  return { doc: { type: "doc", content: content.length ? content : [{ type: "paragraph" }] }, sources, trailing: text.slice(end) };
}

// rawBlock kinds (the page names them from its catalogs), by mdast type.
const rawKinds: Record<string, string> = { html: "html", definition: "definition", footnoteDefinition: "footnote" };

function block(node: md.Node, slice: (n: md.Node) => string): JSONContent {
  const kids = (n: md.Parent) => n.children.map((c) => block(c as md.Node, slice));
  switch (node.type) {
    case "paragraph":
      return { type: "paragraph", content: inline((node as md.Paragraph).children, [], slice) };
    case "heading": {
      const h = node as md.Heading;
      return { type: "heading", attrs: { level: h.depth }, content: inline(h.children, [], slice) };
    }
    case "blockquote": {
      const content = kids(node as md.Blockquote);
      return { type: "blockquote", content: content.length ? content : [{ type: "paragraph" }] };
    }
    case "list": {
      const l = node as md.List;
      const task = l.children.length > 0 && l.children.every((i) => i.checked === true || i.checked === false);
      const spread = !!l.spread || l.children.some((i) => i.spread);
      const items = l.children.map((i) => {
        const content = i.children.map((c) => block(c, slice));
        if (content[0]?.type !== "paragraph") content.unshift({ type: "paragraph" });
        return task ? { type: "taskItem", attrs: { checked: !!i.checked }, content } : { type: "listItem", content };
      });
      if (task) return { type: "taskList", attrs: { spread }, content: items };
      if (l.ordered) return { type: "orderedList", attrs: { start: l.start ?? 1, spread }, content: items };
      return { type: "bulletList", attrs: { spread }, content: items };
    }
    case "code": {
      const c = node as md.Code;
      return { type: "codeBlock", attrs: { language: c.lang || null }, content: c.value ? [{ type: "text", text: c.value }] : undefined };
    }
    case "thematicBreak":
      return { type: "horizontalRule" };
    case "table": {
      const t = node as md.Table;
      return {
        type: "table",
        attrs: { align: (t.align ?? []) as Align[] },
        content: t.children.map((row, r) => ({
          type: "tableRow",
          content: row.children.map((cell) => ({
            type: r === 0 ? "tableHeader" : "tableCell",
            content: [{ type: "paragraph", content: inline(cell.children, [], slice) }],
          })),
        })),
      };
    }
  }
  return { type: "rawBlock", attrs: { text: slice(node), kind: rawKinds[node.type] ?? node.type } };
}

type Mark = { type: string; attrs?: Record<string, unknown> };

function inline(nodes: md.PhrasingContent[] | md.RootContent[], marks: Mark[], slice: (n: md.Node) => string): JSONContent[] {
  const out: JSONContent[] = [];
  const text = (t: string, m: Mark[]) => t && out.push(m.length ? { type: "text", text: t, marks: m } : { type: "text", text: t });
  for (const node of nodes as md.Node[]) {
    switch (node.type) {
      case "text":
        text((node as md.Text).value, marks);
        break;
      case "emphasis":
        out.push(...inline((node as md.Emphasis).children, [...marks, { type: "italic" }], slice));
        break;
      case "strong":
        out.push(...inline((node as md.Strong).children, [...marks, { type: "bold" }], slice));
        break;
      case "delete":
        out.push(...inline((node as md.Delete).children, [...marks, { type: "strike" }], slice));
        break;
      case "inlineCode":
        text((node as md.InlineCode).value, [...marks, { type: "code" }]);
        break;
      case "link": {
        const l = node as md.Link;
        out.push(...inline(l.children, [...marks, { type: "link", attrs: { href: l.url, title: l.title ?? null } }], slice));
        break;
      }
      case "image": {
        const i = node as md.Image;
        out.push({ type: "image", attrs: { src: i.url, alt: i.alt ?? "", title: i.title ?? null } });
        break;
      }
      case "break":
        out.push({ type: "hardBreak" });
        break;
      default: // inline HTML, reference links and images, footnote marks
        out.push({ type: "rawInline", attrs: { text: slice(node) } });
    }
  }
  return out;
}

/** A block's content, without its source ID: what says whether it changed. */
export function fingerprint(node: PMNode): string {
  const json = node.toJSON() as JSONContent;
  if (json.attrs) {
    const rest = { ...json.attrs };
    delete rest[sourceAttr];
    json.attrs = rest;
  }
  return JSON.stringify(json);
}

/** Each sourced block's fingerprint as the editor first holds it. */
export function fingerprints(doc: PMNode): Map<string, string> {
  const out = new Map<string, string>();
  doc.forEach((node) => {
    const id = node.attrs[sourceAttr] as string | null;
    if (id && !out.has(id)) out.set(id, fingerprint(node));
  });
  return out;
}

/** The document as Markdown: untouched blocks as they were read, the rest printed. */
export function serializeMarkdown(doc: PMNode, parsed: Pick<Parsed, "sources" | "trailing">, prints: Map<string, string>): string {
  let out = "";
  doc.forEach((node) => {
    const id = node.attrs[sourceAttr] as string | null;
    const src = id ? parsed.sources.get(id) : undefined;
    // A new empty paragraph writes nothing: Markdown has no empty paragraph.
    if (!src && node.type.name === "paragraph" && node.content.size === 0) return;
    const same = !!src && prints.get(id!) === fingerprint(node);
    let before = src ? src.before : "\n\n";
    if (out === "") before = src?.first ? src.before : "";
    out += before + (same ? src!.source : printBlock(node));
  });
  // What followed the last block, as it was; a new document ends its last line.
  return out + (parsed.sources.size > 0 || parsed.trailing ? parsed.trailing : out ? "\n" : "");
}

// The house style, for blocks that changed.

function printBlock(node: PMNode): string {
  switch (node.type.name) {
    case "paragraph":
      return printInline(node);
    case "heading":
      return "#".repeat(node.attrs.level as number) + " " + printInline(node);
    case "blockquote":
      return printBlocks(node, false)
        .split("\n")
        .map((l) => (l ? "> " + l : ">"))
        .join("\n");
    case "bulletList":
    case "orderedList":
    case "taskList":
      return printList(node);
    case "codeBlock": {
      const text = node.textContent;
      const longest = Math.max(2, ...[...text.matchAll(/`+/g)].map((m) => m[0].length));
      const fence = "`".repeat(longest + 1);
      return `${fence}${(node.attrs.language as string | null) ?? ""}\n${text}\n${fence}`;
    }
    case "horizontalRule":
      return "---";
    case "table":
      return printTable(node);
    case "rawBlock":
      return node.attrs.text as string;
  }
  return printInline(node);
}

// A container's blocks, a blank line apart (a list after a paragraph in a
// tight list item: one line).
function printBlocks(node: PMNode, tight: boolean): string {
  const parts: string[] = [];
  node.forEach((child, _o, i) => {
    const text = printBlock(child);
    if (i === 0) parts.push(text);
    else parts.push((tight && isList(child) ? "\n" : "\n\n") + text);
  });
  return parts.join("");
}

const isList = (n: PMNode) => n.type.name === "bulletList" || n.type.name === "orderedList" || n.type.name === "taskList";

function printList(node: PMNode): string {
  const spread = !!node.attrs.spread;
  const start = (node.attrs.start as number | undefined) ?? 1;
  const items: string[] = [];
  node.forEach((item, _o, i) => {
    let marker = "- ";
    if (node.type.name === "orderedList") marker = `${start + i}. `;
    if (node.type.name === "taskList") marker = item.attrs.checked ? "- [x] " : "- [ ] ";
    const indent = " ".repeat(node.type.name === "taskList" ? 2 : marker.length);
    const body = printBlocks(item, !spread)
      .split("\n")
      .map((l, j) => (j === 0 ? marker + l : l ? indent + l : ""))
      .join("\n");
    items.push(body);
  });
  return items.join(spread ? "\n\n" : "\n");
}

function printTable(node: PMNode): string {
  const rows: string[][] = [];
  node.forEach((row) => {
    const cells: string[] = [];
    row.forEach((cell) => {
      const parts: string[] = [];
      cell.forEach((p) => parts.push(printInline(p, true)));
      cells.push(parts.join("<br>"));
    });
    rows.push(cells);
  });
  const cols = Math.max(...rows.map((r) => r.length));
  const align = ((node.attrs.align as Align[] | null) ?? []).slice(0, cols);
  const width = Array.from({ length: cols }, (_, c) => Math.max(3, ...rows.map((r) => [...(r[c] ?? "")].length)));
  const line = (cells: string[]) => "| " + width.map((w, c) => (cells[c] ?? "").padEnd(w)).join(" | ") + " |";
  const rule =
    "| " +
    width
      .map((w, c) => {
        const a = align[c];
        if (a === "center") return ":" + "-".repeat(w - 2) + ":";
        if (a === "left") return ":" + "-".repeat(w - 1);
        if (a === "right") return "-".repeat(w - 1) + ":";
        return "-".repeat(w);
      })
      .join(" | ") +
    " |";
  return [line(rows[0] ?? []), rule, ...rows.slice(1).map(line)].join("\n");
}

// The order marks open in, outermost first.
const markOrder = ["link", "bold", "italic", "strike", "code"];

function printInline(node: PMNode, inTable = false): string {
  let out = "";
  let open: { name: string; href?: string; title?: string | null }[] = [];
  const close = (to: number) => {
    while (open.length > to) {
      const m = open.pop()!;
      if (m.name === "link") out += `](${linkTarget(m.href ?? "", m.title)})`;
      else out += delimiter(m.name);
    }
  };
  node.forEach((child) => {
    const marks = [...child.marks].sort((a, b) => markOrder.indexOf(a.type.name) - markOrder.indexOf(b.type.name));
    // Keep the marks still in force from the outside; close the rest.
    let keep = 0;
    while (keep < open.length && keep < marks.length && sameMark(open[keep], marks[keep])) keep++;
    close(keep);
    for (const m of marks.slice(keep)) {
      if (m.type.name === "link") out += "[";
      else if (m.type.name !== "code") out += delimiter(m.type.name);
      open.push({ name: m.type.name, href: m.attrs.href as string, title: m.attrs.title as string | null });
    }
    const code = marks.some((m) => m.type.name === "code");
    switch (child.type.name) {
      case "text":
        out += code ? codeSpan(child.text ?? "") : escapeText(child.text ?? "", inTable);
        if (code) open = open.filter((m) => m.name !== "code");
        break;
      case "hardBreak":
        out += inTable ? "<br>" : "\\\n";
        break;
      case "image":
        out += `![${escapeText((child.attrs.alt as string) ?? "", inTable)}](${linkTarget(child.attrs.src as string, child.attrs.title as string | null)})`;
        break;
      case "rawInline":
        out += child.attrs.text as string;
        break;
    }
  });
  close(0);
  return escapeLineStarts(out);
}

function sameMark(o: { name: string; href?: string; title?: string | null }, m: { type: { name: string }; attrs: Record<string, unknown> }) {
  return o.name === m.type.name && (o.name !== "link" || (o.href === m.attrs.href && (o.title ?? null) === (m.attrs.title ?? null)));
}

function delimiter(mark: string): string {
  return mark === "bold" ? "**" : mark === "italic" ? "*" : mark === "strike" ? "~~" : "";
}

function codeSpan(text: string): string {
  const longest = Math.max(0, ...[...text.matchAll(/`+/g)].map((m) => m[0].length));
  const fence = "`".repeat(longest + 1);
  const pad = text.startsWith("`") || text.endsWith("`") || (text.startsWith(" ") && text.endsWith(" ") && text.trim()) ? " " : "";
  return fence + pad + text + pad + fence;
}

function linkTarget(href: string, title: string | null | undefined): string {
  const url = /[\s()<>]/.test(href) ? `<${href.replace(/[<>]/g, (c) => encodeURIComponent(c))}>` : href;
  return title ? `${url} "${title.replace(/"/g, '\\"')}"` : url;
}

/** Text escaped so Markdown reads it back as the same text. */
export function escapeText(text: string, inTable = false): string {
  let out = text.replace(/[\\`*[\]]/g, "\\$&");
  out = out.replace(/(^|[^\p{L}\p{N}])_|_(?=$|[^\p{L}\p{N}])/gu, (m) => m.replace("_", "\\_"));
  out = out.replace(/<(?=[A-Za-z/!?])/g, "\\<");
  out = out.replace(/~~/g, "\\~\\~");
  if (inTable) out = out.replace(/\|/g, "\\|");
  return out;
}

// What would start a block at a line's start is escaped there.
function escapeLineStarts(text: string): string {
  return text
    .split("\n")
    .map((l) => l.replace(/^(\s*)(#{1,6}(?=\s|$)|>|[-+](?=\s)|=+\s*$|(\d+)([.)])(?=\s))/, (_m, sp: string, all: string, num: string | undefined, dot: string | undefined) => (num !== undefined ? `${sp}${num}\\${dot}` : `${sp}\\${all}`)))
    .join("\n");
}
