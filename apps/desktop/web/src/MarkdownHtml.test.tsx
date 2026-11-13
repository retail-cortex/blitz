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

import { Window } from "happy-dom";
import { act } from "react";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

// A DOM to render in, as the visual editor's tests stand one up.
const win = new Window({ url: "http://localhost/" });
let Markdown: typeof import("./Markdown").Markdown;
let MarkdownDocProvider: typeof import("./Markdown").MarkdownDocProvider;
let createRoot: typeof import("react-dom/client").createRoot;
beforeAll(async () => {
  const w = win as unknown as Record<string, unknown>;
  for (const name of ["window", "document", "navigator", "Node", "Element", "HTMLElement", "Text", "DocumentFragment", "MutationObserver", "Event", "getComputedStyle"]) {
    const v = w[name];
    vi.stubGlobal(name, typeof v === "function" && /^[a-z]/.test(name) ? (v as (...a: unknown[]) => unknown).bind(win) : v);
  }
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  ({ Markdown, MarkdownDocProvider } = await import("./Markdown"));
  ({ createRoot } = await import("react-dom/client"));
}, 60_000);
afterAll(() => {
  vi.unstubAllGlobals();
  void win.happyDOM.close();
});

// text rendered as a document's Markdown (doc) or a reply's, as HTML.
function render(text: string, doc = true): string {
  const host = document.body.appendChild(document.createElement("div"));
  const root = createRoot(host);
  act(() => root.render(doc ? <MarkdownDocProvider value={{ dir: "/w", path: "README.md" }}><Markdown text={text} /></MarkdownDocProvider> : <Markdown text={text} />));
  const html = host.innerHTML;
  act(() => root.unmount());
  host.remove();
  return html;
}
const doc = (text: string) => render(text);

// A document's formatting HTML is drawn as Preview's elements; a reply's,
// and anything else, isn't (spec_037_visual_editor VE-05a, DSK-24, #115).
describe("Markdown's HTML", () => {
  it("draws a document's formatting HTML", () => {
    const html = doc('Line one<br />line two, H<sub>2</sub>O.\n\n<div align="center"><b>Centred</b></div>\n\n| a | b |\n|---|---|\n| x<br>y | z |\n');
    expect(html).toMatch(/Line one<br>line two, H<sub>2<\/sub>O\./);
    expect(html).toContain('<div align="center"><b>Centred</b></div>');
    expect(html).toMatch(/<td[^>]*>x<br>y<\/td>/);
  });

  it("keeps a link a link the app opens, and drops what isn't kept", () => {
    const html = doc('<a href="https://example.com" onclick="x()">site</a> and <span style="color:red">red</span>\n');
    expect(html).not.toContain("onclick");
    expect(html).not.toContain("style=");
    expect(html).toContain("<span>red</span>");
    expect(html).toContain("site");
  });

  it("doesn't draw a script, an unknown element or a reply's HTML", () => {
    const html = doc("<script>alert(1)</script>\n\n<blink>x</blink> y\n");
    expect(html).not.toContain("<script");
    expect(html).not.toContain("<blink");
    expect(render("a<br />b <sub>2</sub>", false)).not.toMatch(/<br|<sub/);
  });
});
