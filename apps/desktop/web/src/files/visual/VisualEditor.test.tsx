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

import type { Editor } from "@tiptap/core";
import { Window } from "happy-dom";
import { act } from "react";
import type { Root } from "react-dom/client";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import type { VisualSpot } from "./VisualEditor";

// The editor's first import (TipTap, CodeMirror, the language bundles)
// can pass the hook's 10 s on a loaded machine (the race suite's
// coverage run timed out twice).
const importTimeout = 60_000;

// A DOM for the editor, as the other tests stand one up (htmlPreview).
const win = new Window({ url: "http://localhost/" });
win.document.write("<!doctype html><html><head></head><body></body></html>"); // standards mode, as the app's page
let VisualEditor: typeof import("./VisualEditor").VisualEditor;
let createRoot: typeof import("react-dom/client").createRoot;
beforeAll(async () => {
  const w = win as unknown as Record<string, unknown>;
  for (const name of ["window", "document", "navigator", "Node", "Element", "HTMLElement", "HTMLButtonElement", "Text", "DocumentFragment", "MutationObserver", "ResizeObserver", "Range", "Selection", "Event", "KeyboardEvent", "MouseEvent", "InputEvent", "ClipboardEvent", "DragEvent", "CustomEvent", "FocusEvent", "DOMParser", "getComputedStyle"]) {
    const v = w[name];
    vi.stubGlobal(name, typeof v === "function" && /^[a-z]/.test(name) ? (v as (...a: unknown[]) => unknown).bind(win) : v);
  }
  vi.stubGlobal("getSelection", () => win.getSelection());
  vi.stubGlobal("requestAnimationFrame", (f: () => void) => setTimeout(f, 0));
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  ({ VisualEditor } = await import("./VisualEditor"));
  ({ createRoot } = await import("react-dom/client"));
}, importTimeout);
afterAll(async () => {
  // TipTap destroys an unmounted editor on a timer: after it, the DOM goes.
  await new Promise((r) => setTimeout(r, 100));
  vi.unstubAllGlobals();
  void win.happyDOM.close();
});

const readme = "# Shop\n\nA small shop server.\n\n## Usage\n\nRun it with care.\n";

let root: Root | undefined;
let host: HTMLDivElement | undefined;

afterEach(() => {
  act(() => root?.unmount());
  host?.remove();
  root = host = undefined;
});

// The editor of one tab, as EditorPane mounts it (keyed by path).
function mount(path: string, spot: VisualSpot | undefined, spots: Map<string, VisualSpot>, text = readme) {
  host ??= document.body.appendChild(document.createElement("div"));
  root ??= createRoot(host);
  act(() =>
    root!.render(<VisualEditor key={path} dir="/w" path={path} text={text} spot={spot} onSpot={(at) => spots.set(path, at)} onChange={() => {}} onSave={() => {}} />),
  );
}

// The editor's view, from its DOM.
function view() {
  // TipTap keeps its editor on the view's element.
  const dom = host!.querySelector(".ProseMirror") as (HTMLElement & { editor?: Editor }) | null;
  const editor = dom?.editor;
  if (!editor) throw new Error("no editor");
  return editor;
}

// Each tab has its own cursor and scroll in Visual (spec_037 VE-06b, #96).
describe("VisualEditor's place per tab", () => {
  it("starts a tab at the top, and puts a tab it left back where it was", () => {
    const spots = new Map<string, VisualSpot>();
    mount("README.md", undefined, spots);
    let editor = view();
    expect(editor.state.selection.from).toBe(1); // the start

    // The cursor moves into "Run it with care.", and the tab reports it.
    const at = editor.state.doc.textContent.indexOf("care");
    let pos = 0;
    editor.state.doc.descendants((n, p) => {
      if (n.isText && n.text?.includes("care")) pos = p + n.text.indexOf("care");
    });
    expect(at).toBeGreaterThan(0);
    act(() => editor.commands.setTextSelection(pos));
    expect(spots.get("README.md")).toMatchObject({ anchor: pos, head: pos });

    // Another tab: its own place, the top.
    mount("design.md", spots.get("design.md"), spots);
    expect(view().state.selection.from).toBe(1);

    // Back: the cursor where it was left.
    mount("README.md", spots.get("README.md"), spots);
    editor = view();
    expect(editor.state.selection.from).toBe(pos);
    expect(editor.can().undo()).toBe(false); // putting it back isn't an edit
  });

  it("keeps a place that's past the end of a shorter text inside it", () => {
    const spots = new Map<string, VisualSpot>();
    mount("README.md", { anchor: 9999, head: 9999, scroll: 0 }, spots);
    const editor = view();
    expect(editor.state.selection.from).toBeLessThanOrEqual(editor.state.doc.content.size);
  });
});

// The HTML a document may show is drawn, its source kept; the rest stays a
// source block (VE-05a, #115).
describe("VisualEditor's HTML", () => {
  const text = 'Line one<br />line two, H<sub>2</sub>O.\n\n<div align="center"><b>Centred</b></div>\n\n<script>alert(1)</script>\n';

  it("draws a paragraph's and a block's HTML, and keeps a script as source", () => {
    const changes: string[] = [];
    host ??= document.body.appendChild(document.createElement("div"));
    root ??= createRoot(host);
    act(() => root!.render(<VisualEditor dir="/w" path="html.md" text={text} onChange={(t) => changes.push(t)} onSave={() => {}} />));
    const doc = host.querySelector(".ProseMirror")!;
    const para = doc.querySelector("p")!;
    expect(para.querySelector(".raw-html br")).not.toBeNull();
    expect(para.querySelector(".raw-html sub")?.textContent).toBe("2");
    expect(para.querySelector("code.raw-inline")).toBeNull();
    const block = doc.querySelector(".visual-html .visual-html-view");
    expect(block?.querySelector('div[align="center"] b')?.textContent).toBe("Centred");
    expect(doc.querySelector("script")).toBeNull();
    const raws = [...doc.querySelectorAll(".raw-block")];
    expect(raws.filter((r) => !r.classList.contains("visual-html")).length).toBe(1); // the script, as source
    expect(changes).toEqual([]); // drawing isn't an edit
  });
});
