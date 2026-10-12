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

import { afterEach, describe, expect, it } from "vitest";
import { htmlPicture, isDataImage, pictureSource, pictureURL, setReadPicture } from "./pictures";

const png = "data:image/png;base64,iVBORw0KGgo=";

describe("pictures", () => {
  afterEach(() => setReadPicture(async () => Promise.reject(new Error("unset"))));

  it.each([
    [png, true],
    ["  data:image/jpeg;base64,/9j/4AAQ", true],
    ["data:image/svg+xml;charset=utf-8,%3Csvg%3E", true],
    ["data:image/webp,UklGR", true],
    ["data:text/html;base64,PHNjcmlwdD4=", false],
    ["data:image/x-icon;base64,AAAB", false],
    ["https://example.com/a.png", false],
  ])("%s is a data image: %s", (src, want) => {
    expect(isDataImage(src)).toBe(want);
  });

  it.each([
    [png, "docs/a.md", { kind: "data", url: png }],
    ["img/x.png", "docs/a.md", { kind: "file", path: "docs/img/x.png" }],
    ["/logo.svg", "docs/a.md", { kind: "file", path: "logo.svg" }],
    ["https://example.com/a.png", "docs/a.md", { kind: "web", url: "https://example.com/a.png" }],
    ["../../outside.png", "docs/a.md", { kind: "none" }],
    ["javascript:alert(1)", "docs/a.md", { kind: "none" }],
    ["img/x.png", null, { kind: "none" }],
    ["https://example.com/a.png", null, { kind: "web", url: "https://example.com/a.png" }],
    ["", "a.md", { kind: "none" }],
  ])("%s in %s comes from %j", (src, doc, want) => {
    expect(pictureSource(src, doc)).toEqual(want);
  });

  it.each([
    [`<img src="${png}" alt="A chart">`, { src: png, alt: "A chart" }],
    [`  <IMG SRC='${png}' width=320 height="200px" />\n`, { src: png, alt: "", width: "320", height: "200px" }],
    [`<img src="a.png" onerror="alert(1)" style="x" alt="a &amp; b">`, { src: "a.png", alt: "a & b" }],
    [`<img src="a.png" width="calc(1px)">`, { src: "a.png", alt: "" }],
    [`<img alt="no source">`, null],
    [`<img src="a.png"><script>alert(1)</script>`, null],
    [`<p><img src="a.png"></p>`, null],
    [`text <img src="a.png">`, null],
  ])("%s is the picture %j", (html, want) => {
    expect(htmlPicture(html)).toEqual(want);
  });

  it("shows data images as written, workspace images from memory, never the web", async () => {
    const reads: string[] = [];
    setReadPicture(async (dir, path) => {
      reads.push(`${dir}:${path}`);
      if (path.endsWith(".txt")) return { mime: "text/plain", data: new Uint8Array([104]) };
      if (path.endsWith("missing.png")) throw new Error("no such file");
      return { mime: "image/png", data: new Uint8Array([137, 80, 78, 71]) };
    });
    expect(await pictureURL("/ws", "a.md", png)).toBe(png);
    const url = await pictureURL("/ws", "docs/a.md", "x.png");
    expect(url).toMatch(/^blob:/);
    expect(await pictureURL("/ws", "docs/a.md", "./x.png")).toBe(url); // read once
    expect(await pictureURL("/ws", "docs/a.md", "notes.txt")).toBe("");
    expect(await pictureURL("/ws", "docs/a.md", "missing.png")).toBe("");
    expect(await pictureURL("/ws", "docs/a.md", "https://example.com/a.png")).toBe("");
    expect(reads).toEqual(["/ws:docs/x.png", "/ws:docs/notes.txt", "/ws:docs/missing.png"]);
  });

  it("keeps the latest pictures read", async () => {
    let n = 0;
    setReadPicture(async () => (n++, { mime: "image/png", data: new Uint8Array([1]) }));
    for (let i = 0; i < 70; i++) await pictureURL("/ws", "a.md", `p${i}.png`);
    await pictureURL("/ws", "a.md", "p0.png"); // forgotten: read again
    await pictureURL("/ws", "a.md", "p69.png"); // kept
    expect(n).toBe(71);
  });
});
