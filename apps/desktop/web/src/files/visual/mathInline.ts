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

// Inline math in the visual editor (spec_files_029 FIL-67): drawn by
// KaTeX; selected (a click, or the arrows onto it), its TeX opens in a
// field below it, drawn as you type. Enter or leaving the field keeps it,
// Escape puts it back, an empty field removes it.
import type { Node as PMNode } from "@tiptap/pm/model";
import { TextSelection } from "@tiptap/pm/state";
import type { EditorView, NodeView } from "@tiptap/pm/view";
import { t } from "../../i18n";

// The open field's close, so one opens at a time.
let closeField: ((keep: boolean) => void) | undefined;

type Close = (keep: boolean, quiet?: boolean) => void;

/** Inline math, drawn, with its field. */
export class MathInlineView implements NodeView {
  dom: HTMLElement;
  private close?: Close;

  constructor(
    private node: PMNode,
    private view: EditorView,
    private getPos: () => number | undefined,
  ) {
    this.dom = document.createElement("span");
    this.dom.className = "math-inline-view";
    this.dom.contentEditable = "false";
    this.draw(this.tex());
  }

  private tex(): string {
    return (this.node.attrs.text as string) ?? "";
  }

  private draw(tex: string) {
    const el = this.dom;
    void import("../mathRender").then(({ renderMath }) => renderMath(el, tex, false));
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false;
    const changed = node.attrs.text !== this.node.attrs.text;
    this.node = node;
    if (changed) this.draw(this.tex());
    return true;
  }

  selectNode() {
    this.dom.classList.add("ProseMirror-selectednode");
    if (this.view.editable) this.openField();
  }

  deselectNode() {
    this.dom.classList.remove("ProseMirror-selectednode");
  }

  // The field under the math: its TeX, drawn as it changes.
  private openField() {
    closeField?.(true);
    const at = this.dom.getBoundingClientRect();
    const box = document.createElement("div");
    box.className = "menu floating visual-popover visual-math-field";
    box.style.top = `${at.bottom + 6}px`;
    box.style.left = `${Math.max(8, at.left)}px`;
    const input = document.createElement("input");
    input.className = "input";
    input.spellcheck = false;
    input.value = this.tex();
    input.setAttribute("aria-label", t("desktop.visual.math_inline"));
    box.append(input);
    const before = this.tex();
    let done = false;
    // quiet: the view is going (no change can be made to the document now).
    const close: Close = (keep, quiet = false) => {
      if (done) return;
      done = true;
      if (closeField === close) closeField = undefined;
      this.close = undefined;
      box.remove();
      document.removeEventListener("mousedown", outside, true);
      if (quiet) return;
      const pos = this.getPos();
      if (pos === undefined) return;
      const tex = keep ? input.value.trim() : before;
      const tr = this.view.state.tr;
      if (!tex) tr.delete(pos, pos + this.node.nodeSize);
      else if (tex !== this.tex() || !keep) tr.setNodeMarkup(pos, undefined, { ...this.node.attrs, text: tex });
      tr.setSelection(TextSelection.create(tr.doc, Math.min(tr.mapping.map(pos + this.node.nodeSize), tr.doc.content.size)));
      this.view.dispatch(tr);
      this.view.focus();
    };
    const outside = (e: MouseEvent) => {
      if (!box.contains(e.target as Node)) close(true);
    };
    input.oninput = () => this.draw(input.value || " ");
    input.onkeydown = (e) => {
      if (e.key === "Enter") {
        e.preventDefault();
        close(true);
      } else if (e.key === "Escape") {
        e.preventDefault();
        this.draw(before);
        close(false);
      }
      e.stopPropagation(); // not the document's keys
    };
    document.body.append(box);
    document.addEventListener("mousedown", outside, true);
    closeField = close;
    this.close = close;
    input.focus();
    input.select();
  }

  stopEvent() {
    return false;
  }

  ignoreMutation() {
    return true;
  }

  destroy() {
    this.close?.(true, true);
  }
}
