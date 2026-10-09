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


// The visual editor's document (spec_visual_editor_037 §3): TipTap's
// nodes for CommonMark with GitHub's extensions, and two of Blitz's own
// for what can't be edited visually (raw HTML, front matter, footnotes,
// reference links), kept as their Markdown. Every top-level block carries
// the ID of the source it was read from, so an untouched block is written
// back as it was (VE-04). No node views here: the editor adds them, and
// the tests build the same schema without a DOM.
import { Extension, Node, type Extensions } from "@tiptap/core";
import { Code } from "@tiptap/extension-code";
import { CodeBlock } from "@tiptap/extension-code-block";
import { Image } from "@tiptap/extension-image";
import { TaskItem, TaskList } from "@tiptap/extension-list";
import { Table, TableCell, TableHeader, TableRow } from "@tiptap/extension-table";
import { StarterKit } from "@tiptap/starter-kit";

/** The top-level blocks that remember their source. */
export const sourcedBlocks = ["paragraph", "heading", "blockquote", "bulletList", "orderedList", "taskList", "codeBlock", "horizontalRule", "table", "rawBlock"];

/** The attribute naming a block's source (nothing in the DOM, so pasted blocks have none). */
export const sourceAttr = "blitzSource";

// The source ID on every top-level block, and a list's looseness and a
// table's alignments, which the schema has no place for otherwise.
const SourceIDs = Extension.create({
  name: "blitzSourceIds",
  addGlobalAttributes() {
    return [
      { types: sourcedBlocks, attributes: { [sourceAttr]: { default: null, rendered: false, keepOnSplit: false } } },
      { types: ["bulletList", "orderedList", "taskList"], attributes: { spread: { default: false, rendered: false } } },
      { types: ["table"], attributes: { align: { default: null, rendered: false } } },
    ];
  },
});

/**
 * Markdown the editor keeps as text: a block (HTML, front matter, a
 * footnote); kind says which (html, frontmatter, definition, footnote,
 * or mdast's name for it), named from the catalogs.
 */
export const RawBlock = Node.create({
  name: "rawBlock",
  group: "block",
  atom: true,
  defining: true,
  addAttributes() {
    return { text: { default: "" }, kind: { default: "" } };
  },
  parseHTML: () => [{ tag: "div[data-raw-block]" }],
  renderHTML: ({ node }) => ["div", { "data-raw-block": "", class: "raw-block" }, ["pre", node.attrs.text as string]],
});

/** Markdown the editor keeps as text, inline (HTML, a reference link, a footnote's mark). */
export const RawInline = Node.create({
  name: "rawInline",
  group: "inline",
  inline: true,
  atom: true,
  addAttributes() {
    return { text: { default: "" } };
  },
  parseHTML: () => [{ tag: "code[data-raw-inline]" }],
  renderHTML: ({ node }) => ["code", { "data-raw-inline": "", class: "raw-inline" }, node.attrs.text as string],
});

/** The visual editor's extensions, without node views. */
export function visualExtensions(): Extensions {
  return [
    StarterKit.configure({
      code: false, // below: Markdown lets code be linked, bold, struck
      codeBlock: false, // Blitz's, with CodeMirror (the editor's node view)
      underline: false, // not Markdown
      link: { openOnClick: false, autolink: true, linkOnPaste: true, HTMLAttributes: { rel: null, target: null, class: null } },
      // An empty line after a last block that isn't a paragraph (a code
      // block, a diagram, a table), to type on; written only once typed in
      // (markdown.ts skips a new empty paragraph).
      trailingNode: { node: "paragraph", notAfter: ["paragraph"] },
    }),
    Code.extend({ excludes: "" }),
    CodeBlock.configure({ languageClassPrefix: "language-", defaultLanguage: null }),
    Image.configure({ inline: true, allowBase64: false }),
    TaskList,
    TaskItem.configure({ nested: true }),
    Table.configure({ resizable: false }),
    TableRow,
    TableHeader,
    TableCell,
    RawBlock,
    RawInline,
    SourceIDs,
  ];
}
