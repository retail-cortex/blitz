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
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { inlineHtml, isRelative, resolveRef } from "./htmlPreview";

const dom = new Window();
beforeAll(() => vi.stubGlobal("DOMParser", dom.DOMParser));
afterAll(() => vi.unstubAllGlobals());

describe("an HTML file's preview", () => {
  it.each([
    ["style.css", true],
    ["./img/a.png", true],
    ["../shared/x.js", true],
    ["https://cdn.example/x.js", false],
    ["//cdn.example/x.js", false],
    ["data:image/png;base64,AA", false],
    ["/abs/x.css", false],
    ["#top", false],
    ["", false],
  ])("knows %s is beside the page: %s", (ref, want) => {
    expect(isRelative(ref)).toBe(want);
  });

  it("resolves a relative path from the page's folder, never out of the workspace", () => {
    expect(resolveRef("site/index.html", "css/a.css")).toBe("site/css/a.css");
    expect(resolveRef("site/pages/b.html", "../img/My%20Pic.png?v=2#x")).toBe("site/img/My Pic.png");
    expect(resolveRef("index.html", "./a.js")).toBe("a.js");
    expect(resolveRef("index.html", "../../etc/passwd")).toBe("");
  });

  it("puts the page's own stylesheets, scripts and images inline, and leaves the rest", async () => {
    const html = `<!DOCTYPE html><html><head><link rel="stylesheet" href="css/site.css"><link rel="stylesheet" href="https://cdn.example/x.css"><script src="app.js"></script><link rel="stylesheet" href="gone.css"></head><body><img src="img/logo.png"><img src="https://example.com/a.png"></body></html>`;
    const text = vi.fn(async (p: string) => {
      if (p === "site/css/site.css") return "body { color: red }";
      if (p === "site/app.js") return "window.ok = 1";
      throw new Error("no " + p);
    });
    const dataURL = vi.fn(async (p: string) => `data:image/png;base64,${btoa(p)}`);
    const out = await inlineHtml(html, "site/index.html", { text, dataURL });
    expect(out.startsWith("<!DOCTYPE html>")).toBe(true);
    expect(out).toContain("<style>body { color: red }</style>");
    expect(out).toContain('href="https://cdn.example/x.css"');
    expect(out).toContain("<script>window.ok = 1</script>");
    expect(out).toContain('href="gone.css"');
    expect(out).toContain(`src="data:image/png;base64,${btoa("site/img/logo.png")}"`);
    expect(out).toContain('src="https://example.com/a.png"');
    expect(text).not.toHaveBeenCalledWith(expect.stringContaining("cdn.example"));
  });
});
