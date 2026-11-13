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
import { closingIndex, safeHTML } from "./safeHtml";

// The HTML a document may show drawn (spec_037_visual_editor VE-05a, #115).
describe("safeHTML", () => {
  it.each([
    ["a line break", "<br />", [{ tag: "br", attrs: {}, children: [] }]],
    ["text and entities", "a &amp; b &#x2713;", ["a & b ✓"]],
    ["a < that starts no tag", "2 < 3", ["2 < 3"]],
    ["nested formatting", "<b>bold <i>both</i></b>", [{ tag: "b", attrs: {}, children: ["bold ", { tag: "i", attrs: {}, children: ["both"] }] }]],
    ["the attributes kept, the rest dropped", `<div align="center" style="color:red" onclick="x()" title='t'>c</div>`, [{ tag: "div", attrs: { align: "center", title: "t" }, children: ["c"] }]],
    ["an attribute's value that isn't one it may have", `<div align="javascript:x">c</div>`, [{ tag: "div", attrs: {}, children: ["c"] }]],
    ["a picture", `<img src="a.png" alt="A" width="40" height="x">`, [{ tag: "img", attrs: { src: "a.png", alt: "A", width: "40" }, children: [] }]],
    ["a > in a quoted value", `<abbr title="a > b">x</abbr>`, [{ tag: "abbr", attrs: { title: "a > b" }, children: ["x"] }]],
    ["upper case", "<BR>", [{ tag: "br", attrs: {}, children: [] }]],
  ])("draws %s", (_name, html, want) => {
    expect(safeHTML(html)).toEqual(want);
  });

  it.each([
    ["a script", "<script>alert(1)</script>"],
    ["a style", "<style>body{}</style>"],
    ["a frame", `<iframe src="https://x"></iframe>`],
    ["an unknown element", "<blink>x</blink>"],
    ["a comment", "<!-- note -->"],
    ["a tag not closed", "<details><summary>s</summary>"],
    ["a closing tag not opened", "</div>"],
    ["tags crossed", "<b><i>x</b></i>"],
    ["a broken tag", "<div"],
    ["a form", "<form><input></form>"],
  ])("doesn't draw %s", (_name, html) => {
    expect(safeHTML(html)).toBeNull();
  });
});

describe("closingIndex", () => {
  const html = (value: string) => ({ type: "html", value });
  const text = (value: string) => ({ type: "text", value });

  it.each([
    ["an element's closing tag", [html("<sub>"), text("2"), html("</sub>")], 0, 2],
    ["past one nested in it", [html("<span>"), html("<span>"), html("</span>"), html("</span>")], 0, 3],
    ["none for a void element", [html("<br>"), text("a")], 0, -1],
    ["none for a self-closing tag", [html("<span/>"), html("</span>")], 0, -1],
    ["none when it doesn't close", [html("<sub>"), text("2")], 0, -1],
    ["none for text", [text("<sub>"), html("</sub>")], 0, -1],
  ])("finds %s", (_name, nodes, i, want) => {
    expect(closingIndex(nodes, i)).toBe(want);
  });
});
