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

// Every stylesheet's blocks close: a block left open (an @container
// query missing its brace) quietly nests every rule after it, which then
// applies only where that query holds.
import { readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { describe, expect, it } from "vitest";

const dir = import.meta.dirname;

function stylesheets(d: string, rel = ""): string[] {
  return readdirSync(d, { withFileTypes: true }).flatMap((e) => {
    const r = rel ? `${rel}/${e.name}` : e.name;
    if (e.isDirectory()) return e.name === "gen" ? [] : stylesheets(join(d, e.name), r);
    return e.name.endsWith(".css") ? [r] : [];
  });
}

/** The line of each block still open at the end, comments ignored. */
function unclosed(text: string): number[] {
  const open: number[] = [];
  let line = 1;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (c === "/" && text[i + 1] === "*") {
      const end = text.indexOf("*/", i + 2);
      const comment = text.slice(i, end < 0 ? text.length : end + 2);
      line += comment.split("\n").length - 1;
      i += comment.length - 1;
    } else if (c === "\n") line++;
    else if (c === "{") open.push(line);
    else if (c === "}") open.pop();
  }
  return open;
}

describe("the stylesheets", () => {
  const files = stylesheets(dir);
  it("are found", () => {
    expect(files).toContain("app.css");
  });
  it.each(files)("%s closes every block", (file) => {
    expect(unclosed(readFileSync(join(dir, file), "utf8"))).toEqual([]);
  });
});

// The window moves by the title bar's free space (issue #106): only what
// can be clicked in it stops dragging, never the menu bar as a whole, and
// a view's header drags too.
// No view covers the top menu: a layer over the whole window lets clicks
// through (CodeMirror's tooltips); viewers start under it, as the
// drawer's views do (spec_066_reasoning RSN-15).
describe("the top menu", () => {
  it.each(stylesheets(dir))("%s leaves it usable", (file) => {
    const rules = readFileSync(join(dir, file), "utf8").match(/[^{}]+\{[^{}]*\}/g) ?? [];
    const covering = rules.filter((r) => /position:\s*fixed/.test(r) && /inset:\s*0\s*[;}]/.test(r) && !/pointer-events:\s*none/.test(r));
    expect(covering.map((r) => r.split("{")[0].trim())).toEqual([]);
  });
});

describe("the title bar", () => {
  const css = readFileSync(join(dir, "app.css"), "utf8");
  it("drags between the menus", () => {
    expect(readFileSync(join(dir, "MenuBar.tsx"), "utf8")).not.toMatch(/className="menubar[^"]*no-drag/);
    expect(css).toMatch(/\.drag-region button, \.drag-region input, \.drag-region \.menu-anchor \{ -webkit-app-region: no-drag; app-region: no-drag; \}/);
  });
  it("goes on in a view's header, whose controls stay clickable", () => {
    expect(css).toMatch(/\.drawer > \.drawer-head:not\(\.below\) \{ -webkit-app-region: drag; app-region: drag; \}/);
    expect(css).toMatch(/\.drawer > \.drawer-head:not\(\.below\) :is\(button, input, select, textarea, label, a, \[role="tab"\], \[tabindex\], \[contenteditable\], \.menu-anchor\) \{ -webkit-app-region: no-drag; app-region: no-drag; \}/);
  });
});

// A drop-down's arrow is .select's background-image (@blitz/webui's
// m3.css): a rule for .select that sets the background shorthand resets
// it, and the drop-down looks like a text field (glass.css's did,
// 2026-11-24); it sets background-color. Here and in @blitz/webui.
function arrowResets(css: string): string[] {
  const rules = css.replace(/\/\*[\s\S]*?\*\//g, "").match(/[^{}]+\{[^{}]*\}/g) ?? [];
  return rules
    .map((r) => ({ sel: r.split("{")[0].trim(), body: r.slice(r.indexOf("{")) }))
    .filter(({ sel }) => /\.select(?![\w-])/.test(sel) && !/\.select\s+option\b/.test(sel) && sel !== ".input, .select, textarea.input")
    .filter(({ body }) => /[{;\s]background\s*:/.test(body))
    .map(({ sel }) => sel);
}

describe("drop-downs", () => {
  const webui = dirname(createRequire(import.meta.url).resolve("@blitz/webui/glass.css"));
  const sheets = [...stylesheets(dir).map((f) => join(dir, f)), ...readdirSync(webui).filter((f) => f.endsWith(".css")).map((f) => join(webui, f))];
  it("are checked in @blitz/webui too", () => {
    expect(sheets.some((f) => f.endsWith("glass.css"))).toBe(true);
    expect(arrowResets(':root[data-glass="on"] :is(.input, .select) { background: var(--glass-strong); }')).toHaveLength(1);
    expect(arrowResets(':root[data-glass="on"] :is(.input, .select) { background-color: var(--glass-strong); }')).toEqual([]);
    expect(arrowResets(".select option { background: red; } .select-like { background: red; }")).toEqual([]);
  });
  it.each(sheets)("%s keeps their arrow", (file) => {
    expect(arrowResets(readFileSync(file, "utf8"))).toEqual([]);
  });
});
