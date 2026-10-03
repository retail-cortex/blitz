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

import { describe, expect, it } from "vitest";
import { pageHTML, paperFor, pdfPath, pictureURL, type ReadPicture } from "./printPage";

describe("paperFor", () => {
  it.each([
    ["en-US", "Letter"],
    ["en", "Letter"],
    ["fr-CA", "Letter"],
    ["en-GB", "A4"],
    ["fr", "A4"],
    ["es-MX", "Letter"],
    ["not a locale!", "A4"],
  ])("%s", (locale, want) => expect(paperFor(locale)).toBe(want));
});

describe("pdfPath", () => {
  it.each([
    ["notes/week1.md", "notes/week1.pdf"],
    ["README.MARKDOWN", "README.pdf"],
  ])("%s", (path, want) => expect(pdfPath(path)).toBe(want));
});

describe("pageHTML", () => {
  it("puts the preview under the window's CSS, the user's, then the print rules", () => {
    const html = pageHTML({ title: "A <b>title</b>", body: "<div class=markdown>hi</div>", css: ".a{}", userCss: ".b{}</style><script>" });
    expect(html).toMatch(/^<!doctype html><html data-theme="light">/);
    expect(html).toContain("<title>A &lt;b&gt;title&lt;/b&gt;</title>");
    expect(html.indexOf(".a{}")).toBeLessThan(html.indexOf(".b{}"));
    expect(html.indexOf(".b{}")).toBeLessThan(html.indexOf("break-inside"));
    expect(html).not.toContain("</style><script>");
    expect(html).toContain('<div class="preview preview-markdown"><div class=markdown>hi</div></div>');
  });
});

describe("pictureURL", () => {
  const read: ReadPicture = async (path) => {
    if (path === "notes/img/chart.png") return { mime: "image/png", data: new Uint8Array([1, 2, 3]) };
    if (path === "notes/doc.pdf") return { mime: "application/pdf", data: new Uint8Array([1]) };
    throw new Error("no such file");
  };
  it.each([
    ["img/chart.png", "data:image/png;base64,AQID"],
    ["https://example.com/a.png", "https://example.com/a.png"],
    ["data:image/gif;base64,R0lG", "data:image/gif;base64,R0lG"],
    ["javascript:alert(1)", ""],
    ["../../../etc/x.png", ""],
    ["missing.png", ""],
    ["doc.pdf", ""],
    ["img/", ""],
  ])("%s", async (src, want) => expect(await pictureURL(src, "notes/week1.md", read)).toBe(want));
});
