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


// A code block in the visual editor (spec_visual_editor_037 §4): a
// CodeMirror editor inside the document. Every change in it reaches the
// document in the same event, and every change to the block from the
// document (undo, a paste over it) reaches it; the document's history
// undoes both. The cursor moves in and out as if the block were text. A
// Mermaid block shows its diagram, its source a click away; a raw block
// (HTML, front matter) is its Markdown, labelled.
import { Compartment, EditorState as CMState, type Extension } from "@codemirror/state";
import { EditorView as CMView, type KeyBinding, type ViewUpdate } from "@codemirror/view";
import { redo, undo } from "@tiptap/pm/history";
import type { Node as PMNode } from "@tiptap/pm/model";
import { Selection, TextSelection } from "@tiptap/pm/state";
import type { EditorView, NodeView } from "@tiptap/pm/view";
import { t } from "../../i18n";
import { drawDiagram } from "../../mermaid";
import { indentUnit } from "@codemirror/language";
import { embeddedCode, languageSupport } from "../codemirror";
import { detectIndent } from "../indent";

// A language for a fence's info string, as a file name CodeMirror knows.
const fenceNames: Record<string, string> = { js: "x.js", javascript: "x.js", ts: "x.ts", typescript: "x.ts", tsx: "x.tsx", jsx: "x.jsx", py: "x.py", python: "x.py", go: "x.go", rust: "x.rs", rs: "x.rs", sh: "x.sh", bash: "x.sh", shell: "x.sh", zsh: "x.sh", json: "x.json", yaml: "x.yaml", yml: "x.yaml", toml: "x.toml", html: "x.html", css: "x.css", sql: "x.sql", java: "x.java", c: "x.c", cpp: "x.cpp", "c++": "x.cpp", proto: "x.proto", protobuf: "x.proto", dockerfile: "Dockerfile", md: "x.md", markdown: "x.md", diff: "x.diff", xml: "x.xml", kotlin: "x.kt", swift: "x.swift", ruby: "x.rb", php: "x.php" };

/** The file name whose language a fence's info string names. */
export function fenceFile(lang: string | null | undefined): string {
  const l = (lang ?? "").trim().split(/\s+/)[0].toLowerCase();
  return fenceNames[l] ?? (l ? `x.${l}` : "x.txt");
}

// The raw blocks the catalogs name; the rest are "other".
const rawKinds = new Set(["frontmatter", "html", "definition", "footnote"]);

/** Code blocks and raw blocks: which text they hold and how it changes. */
type Kind = "code" | "raw";

/** A CodeMirror editor for a code block or a raw block, in the document. */
export class CodeBlockView implements NodeView {
  dom: HTMLElement;
  private cm: CMView;
  private updating = false;
  private language = new Compartment();
  private label: HTMLElement;
  private diagram?: HTMLElement;
  private drawTimer?: ReturnType<typeof setTimeout>;
  private editing = false;

  constructor(
    private node: PMNode,
    private view: EditorView,
    private getPos: () => number | undefined,
    private kind: Kind,
  ) {
    this.dom = document.createElement("div");
    this.dom.className = kind === "raw" ? "code-block visual-code raw-block" : "code-block visual-code";
    const head = document.createElement("div");
    head.className = "code-head";
    head.contentEditable = "false";
    this.label = document.createElement("span");
    this.label.className = "t-label muted";
    head.append(this.label);
    if (kind === "code") {
      const copy = document.createElement("button");
      copy.type = "button";
      copy.className = "visual-code-button t-label";
      copy.textContent = t("desktop.code.copy");
      copy.onclick = () => void navigator.clipboard?.writeText(this.text());
      head.append(copy);
    }
    this.dom.append(head);
    this.cm = new CMView({
      state: CMState.create({ doc: this.text(), extensions: this.extensions() }),
    });
    this.dom.append(this.cm.dom);
    this.showLanguage();
    this.setLanguage();
    if (this.isMermaid()) this.addDiagram();
  }

  private text(): string {
    return this.kind === "raw" ? ((this.node.attrs.text as string) ?? "") : this.node.textContent;
  }

  private isMermaid() {
    return this.kind === "code" && ((this.node.attrs.language as string | null) ?? "").trim().toLowerCase() === "mermaid";
  }

  private extensions(): Extension {
    const keys: KeyBinding[] = [
      { key: "ArrowUp", run: () => this.escape("line", -1) },
      { key: "ArrowLeft", run: () => this.escape("char", -1) },
      { key: "ArrowDown", run: () => this.escape("line", 1) },
      { key: "ArrowRight", run: () => this.escape("char", 1) },
      { key: "Mod-Enter", run: () => this.exit() },
      { key: "Ctrl-Enter", run: () => this.exit() },
      { key: "Backspace", run: () => this.backspace() },
      { key: "Mod-z", run: () => undo(this.view.state, this.view.dispatch) },
      { key: "Mod-y", run: () => redo(this.view.state, this.view.dispatch) },
      { key: "Shift-Mod-z", run: () => redo(this.view.state, this.view.dispatch) },
      { key: "Mod-s", run: () => (this.view.dom.dispatchEvent(new CustomEvent("visual-save", { bubbles: true })), true) },
    ];
    return [
      embeddedCode(this.language.of([]), keys),
      indentUnit.of(this.text().includes("\n\t") || /^go$/i.test(((this.node.attrs.language as string | null) ?? "").trim()) ? "\t" : detectIndent(this.text())),
      CMView.editorAttributes.of({ class: "visual-code-editor" }), // CodeMirror owns its element's classes
      CMView.updateListener.of((u) => this.forward(u)),
      CMState.readOnly.of(!this.view.editable),
    ];
  }

  private showLanguage() {
    const lang = (this.node.attrs.language as string | null) ?? "";
    this.label.textContent = this.kind === "raw" ? t(`desktop.visual.raw.${rawKinds.has(this.node.attrs.kind as string) ? this.node.attrs.kind : "other"}`) : lang || "text";
  }

  private async setLanguage() {
    const name = this.kind === "raw" ? (this.node.attrs.kind === "html" ? "x.html" : this.node.attrs.kind === "frontmatter" ? "x.yaml" : "x.md") : fenceFile(this.node.attrs.language as string | null);
    const support = await languageSupport(name);
    this.cm.dispatch({ effects: this.language.reconfigure(support) });
  }

  // A Mermaid block: the diagram below its source, which shows on Edit.
  private addDiagram() {
    this.diagram = document.createElement("div");
    this.diagram.className = "mermaid-diagram visual-diagram";
    this.diagram.contentEditable = "false";
    const edit = document.createElement("button");
    edit.type = "button";
    edit.className = "visual-code-button t-label";
    edit.textContent = t("desktop.visual.edit_diagram");
    edit.onclick = () => {
      this.editing = !this.editing;
      this.dom.classList.toggle("editing", this.editing);
      edit.textContent = t(this.editing ? "desktop.visual.done_diagram" : "desktop.visual.edit_diagram");
      if (this.editing) this.cm.focus();
    };
    this.dom.querySelector(".code-head")?.append(edit);
    this.dom.classList.add("visual-mermaid");
    this.dom.append(this.diagram);
    this.draw(0);
  }

  private draw(delay = 300) {
    clearTimeout(this.drawTimer);
    this.drawTimer = setTimeout(async () => {
      if (!this.diagram) return;
      const theme = document.documentElement.dataset.theme === "dark" ? "dark" : "light";
      try {
        this.diagram.innerHTML = await drawDiagram(this.text(), theme); // sanitised by Mermaid (FIL-55)
        this.diagram.classList.remove("error-text");
      } catch (e) {
        this.diagram.textContent = String((e as Error).message ?? e);
        this.diagram.classList.add("error-text");
      }
    }, delay);
  }

  // The editor's changes, to the document, in the same event (VE-22).
  private forward(update: ViewUpdate) {
    if (this.updating || !this.cm.hasFocus) return;
    const pos = this.getPos();
    if (pos === undefined) return;
    if (this.kind === "raw") {
      if (!update.docChanged) return;
      this.view.dispatch(this.view.state.tr.setNodeMarkup(pos, undefined, { ...this.node.attrs, text: update.state.doc.toString() }));
      return;
    }
    let offset = pos + 1;
    const { main } = update.state.selection;
    const selFrom = offset + main.from;
    const selTo = offset + main.to;
    const pmSel = this.view.state.selection;
    if (!update.docChanged && pmSel.from === selFrom && pmSel.to === selTo) return;
    const tr = this.view.state.tr;
    update.changes.iterChanges((fromA, toA, fromB, toB, text) => {
      if (text.length) tr.replaceWith(offset + fromA, offset + toA, this.view.state.schema.text(text.toString()));
      else tr.delete(offset + fromA, offset + toA);
      offset += toB - fromB - (toA - fromA);
    });
    tr.setSelection(TextSelection.create(tr.doc, selFrom, selTo));
    this.view.dispatch(tr);
    if (update.docChanged && this.isMermaid()) this.draw();
  }

  // The cursor leaves the block at its edge, for the block beside it.
  private escape(unit: "line" | "char", dir: -1 | 1): boolean {
    const state = this.cm.state;
    let main: { from: number; to: number; empty: boolean } = state.selection.main;
    if (!main.empty) return false;
    if (unit === "line") main = { ...state.doc.lineAt(state.selection.main.head), empty: true };
    if (dir < 0 ? main.from > 0 : main.to < state.doc.length) return false;
    const pos = this.getPos();
    if (pos === undefined) return false;
    const target = pos + (dir < 0 ? 0 : this.node.nodeSize);
    const selection = Selection.near(this.view.state.doc.resolve(target), dir);
    this.view.dispatch(this.view.state.tr.setSelection(selection).scrollIntoView());
    this.view.focus();
    return true;
  }

  // ⌘Enter: a new paragraph after the block, the cursor in it.
  private exit(): boolean {
    const pos = this.getPos();
    if (pos === undefined) return false;
    const after = pos + this.node.nodeSize;
    const tr = this.view.state.tr.insert(after, this.view.state.schema.nodes.paragraph.create());
    tr.setSelection(TextSelection.create(tr.doc, after + 1));
    this.view.dispatch(tr.scrollIntoView());
    this.view.focus();
    return true;
  }

  // Backspace in an empty block makes it a paragraph.
  private backspace(): boolean {
    if (this.cm.state.doc.length > 0) return false;
    const pos = this.getPos();
    if (pos === undefined) return false;
    const tr = this.view.state.tr.replaceWith(pos, pos + this.node.nodeSize, this.view.state.schema.nodes.paragraph.create());
    tr.setSelection(TextSelection.create(tr.doc, pos + 1));
    this.view.dispatch(tr);
    this.view.focus();
    return true;
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false;
    const languageChanged = node.attrs.language !== this.node.attrs.language || node.attrs.kind !== this.node.attrs.kind;
    this.node = node;
    if (languageChanged) {
      this.showLanguage();
      void this.setLanguage();
    }
    if (this.updating) return true;
    const next = this.text();
    const cur = this.cm.state.doc.toString();
    if (next !== cur) {
      let start = 0;
      let curEnd = cur.length;
      let nextEnd = next.length;
      while (start < curEnd && cur.charCodeAt(start) === next.charCodeAt(start)) start++;
      while (curEnd > start && nextEnd > start && cur.charCodeAt(curEnd - 1) === next.charCodeAt(nextEnd - 1)) {
        curEnd--;
        nextEnd--;
      }
      this.updating = true;
      this.cm.dispatch({ changes: { from: start, to: curEnd, insert: next.slice(start, nextEnd) } });
      this.updating = false;
      if (this.isMermaid()) this.draw();
    }
    return true;
  }

  // The document's cursor, put into the block.
  setSelection(anchor: number, head: number) {
    this.cm.focus();
    this.updating = true;
    this.cm.dispatch({ selection: { anchor, head } });
    this.updating = false;
  }

  selectNode() {
    this.dom.classList.add("ProseMirror-selectednode");
    this.cm.focus();
  }

  deselectNode() {
    this.dom.classList.remove("ProseMirror-selectednode");
  }

  // The block handles its own events and DOM.
  stopEvent() {
    return true;
  }

  ignoreMutation() {
    return true;
  }

  destroy() {
    clearTimeout(this.drawTimer);
    this.cm.destroy();
  }
}
