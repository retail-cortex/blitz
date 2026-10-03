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
import { defaultView, playsVideo, previewKind, previewOnly } from "./previewKind";

describe("previewKind", () => {
  it.each([
    ["README.md", "markdown"],
    ["docs/guide.MARKDOWN", "markdown"],
    ["logo.svg", "svg"],
    ["a/b/shot.PNG", "image"],
    ["photo.jpeg", "image"],
    ["spec.pdf", "pdf"],
    ["audio/overview.wav", "audio"],
    ["talk.MP3", "audio"],
    ["voice.opus", "audio"],
    ["demo.MP4", "video"],
    ["old.avi", "video"],
    ["main.go", null],
    ["Makefile", null],
    ["v1.2/notes", null],
    [".md", "markdown"],
  ])("%s", (path, want) => expect(previewKind(path)).toBe(want));
  it("images, PDFs and sound show only as previews", () => {
    expect([previewOnly("image"), previewOnly("pdf"), previewOnly("audio"), previewOnly("markdown"), previewOnly(null)]).toEqual([true, true, true, false, false]);
  });
});

describe("defaultView", () => {
  it.each<[string, Parameters<typeof defaultView>[0], boolean, boolean, string]>([
    ["markdown opens rendered", "markdown", false, false, "preview"],
    ["markdown at a line opens its source", "markdown", true, false, "source"],
    ["an empty markdown file opens its source", "markdown", false, true, "source"],
    ["svg opens its source", "svg", false, false, "source"],
    ["images only preview", "image", true, true, "preview"],
    ["pdfs only preview", "pdf", false, false, "preview"],
    ["sound only plays", "audio", false, false, "preview"],
    ["other files are source", null, false, false, "source"],
  ])("%s", (_name, kind, atLine, empty, want) => {
    expect(defaultView(kind, { atLine, empty })).toBe(want);
  });
});

describe("playsVideo", () => {
  it.each([
    ["a.mp4", true],
    ["b.MOV", true],
    ["c.webm", true],
    ["d.avi", false],
    ["e.wmv", false],
  ])("%s", (p, want) => expect(playsVideo(p)).toBe(want));
});
