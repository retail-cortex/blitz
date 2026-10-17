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
import { exportPath, pageHTML, paperFor, pdfPath, pictureURL, type ReadPicture, withMathFonts } from "./printPage";

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

it("names a Word export like the PDF", () => {
  expect(exportPath("docs/notes.md", ".docx")).toBe("docs/notes.docx");
  expect(exportPath("README.markdown", ".docx")).toBe("README.docx");
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

describe("withMathFonts", () => {
  const css = [
    '@font-face { font-family: KaTeX_Main; src: url("/assets/KaTeX_Main-Regular.woff2") format("woff2"), url("/assets/KaTeX_Main-Regular.woff") format("woff"); font-weight: normal; }',
    '@font-face { font-family: "KaTeX_Math"; src: url(KaTeX_Math-Italic.woff2) format("woff2"); }',
    '@font-face { font-family: Inter; src: url("/assets/Inter.woff2") format("woff2"); }',
    ".katex { font: normal 1.21em KaTeX_Main; }",
  ].join("\n");

  it("puts KaTeX's fonts in, as their WOFF2 alone, and nothing else", async () => {
    const asked: string[] = [];
    const out = await withMathFonts(css, "http://127.0.0.1:5123/index.html", async (url) => {
      asked.push(url);
      return new Uint8Array([119, 79, 70, 50]).buffer; // "wOF2"
    });
    expect(asked).toEqual(["http://127.0.0.1:5123/assets/KaTeX_Main-Regular.woff2", "http://127.0.0.1:5123/KaTeX_Math-Italic.woff2"]);
    expect(out).toContain('font-family: KaTeX_Main; src: url(data:font/woff2;base64,d09GMg==) format("woff2"); font-weight: normal; }');
    expect(out).toContain('font-family: "KaTeX_Math"; src: url(data:font/woff2;base64,d09GMg==) format("woff2"); }');
    expect(out).not.toContain("KaTeX_Main-Regular.woff\")");
    expect(out).toContain('url("/assets/Inter.woff2")'); // not KaTeX's
    expect(out).toContain(".katex { font: normal 1.21em KaTeX_Main; }");
  });

  it("keeps a font it can't read as it was", async () => {
    const out = await withMathFonts(css, "http://localhost:5173/", async () => Promise.reject(new Error("404")));
    expect(out).toBe(css);
  });
});

