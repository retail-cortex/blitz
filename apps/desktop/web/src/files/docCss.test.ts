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

import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { applyDocCss, docCssFor, docCssTemplate, docCssToSave, docVariables } from "./docCss";

describe("document css", () => {
  it("lists every variable document.css has", () => {
    const sheet = readFileSync("src/files/document.css", "utf8");
    const defined = new Set([...sheet.matchAll(/(--doc-[\w-]+)\s*:/g)].map((m) => m[1]));
    expect(new Set(docVariables.map(([n]) => n))).toEqual(defined);
  });

  it("starts each theme from a template that saves as nothing", () => {
    for (const theme of ["light", "dark"] as const) {
      const tpl = docCssTemplate(theme);
      expect(tpl).toContain(`the ${theme} theme`);
      for (const [name] of docVariables) expect(tpl).toContain(`/* ${name}: ;`);
      expect(docCssToSave(theme, tpl)).toBe("");
      expect(docCssToSave(theme, `\n${tpl}\n\n`)).toBe("");
      expect(docCssToSave(theme, tpl + ".x{}")).toBe(tpl + ".x{}");
    }
  });

  it.each([
    ["light", "L"],
    ["dark", "D"],
  ] as const)("picks the %s theme's", (theme, want) => {
    expect(docCssFor({ markdown_css_light: "L", markdown_css_dark: "D" }, theme)).toBe(want);
  });

  it("keeps one style element, removed when empty", () => {
    const els = new Map<string, { id: string; textContent: string; remove: () => void }>();
    const doc = {
      getElementById: (id: string) => els.get(id) ?? null,
      createElement: () => ({ id: "", textContent: "", remove() { els.delete(this.id); } }),
      head: { appendChild: (el: { id: string; textContent: string; remove: () => void }) => els.set(el.id, el) },
    } as unknown as Document;
    applyDocCss("a{}", doc);
    applyDocCss("b{}", doc);
    expect(els.size).toBe(1);
    expect(els.get("user-doc-css")?.textContent).toBe("b{}");
    applyDocCss("", doc);
    expect(els.size).toBe(0);
  });
});
