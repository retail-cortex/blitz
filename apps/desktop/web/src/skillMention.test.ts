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
import { insertSkill, skillAt, skillOptions } from "./skillMention";

describe("\\skill completion", () => {
  it.each([
    ["\\rel", 4, { start: 0, end: 4, query: "rel" }],
    ["use \\", 5, { start: 4, end: 5, query: "" }],
    ["(\\notes", 7, { start: 1, end: 7, query: "notes" }],
    ["C:\\notes", 8, null],
    ["\\notes and more", 15, null],
    ["\\Notes", 6, null],
  ])("the token in %j at %i", (text, caret, want) => {
    expect(skillAt(text, caret)).toEqual(want);
  });

  it("offers names starting with it first, then those containing it", () => {
    const skills = [
      { name: "release-notes", description: "Write release notes" },
      { name: "notes", description: "Take notes" },
      { name: "pdf", description: "Read PDFs" },
    ];
    expect(skillOptions("no", skills).map((s) => s.name)).toEqual(["notes", "release-notes"]);
    expect(skillOptions("pdfs", skills).map((s) => s.name)).toEqual(["pdf"]);
    expect(skillOptions("", skills)).toHaveLength(3);
  });

  it("puts the name in, with a space unless one follows", () => {
    expect(insertSkill("use \\rel now", { start: 4, end: 8, query: "rel" }, "release-notes")).toEqual({ text: "use \\release-notes now", caret: 18 });
    expect(insertSkill("\\re", { start: 0, end: 3, query: "re" }, "release-notes")).toEqual({ text: "\\release-notes ", caret: 15 });
  });
});
