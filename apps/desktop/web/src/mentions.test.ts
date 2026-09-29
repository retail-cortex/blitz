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
import { appendMention, insertMention, isImagePath, mentionAt, mentionText } from "./mentions";

describe("mentionAt", () => {
  it.each([
    ["@", 1, { start: 0, end: 1, query: "" }],
    ["look at @src/ma", 15, { start: 8, end: 15, query: "src/ma" }],
    ['see @"my do', 11, { start: 4, end: 11, query: "my do" }],
    ["line\n@x", 7, { start: 5, end: 7, query: "x" }],
    ["me@example", 10, null],
    ["@done and more", 14, null],
    ["@a b", 4, null],
    ["no mention", 10, null],
  ])("%j at %d", (text, caret, want) => {
    expect(mentionAt(text, caret)).toEqual(want);
  });
});

describe("insertMention", () => {
  it.each([
    ["@ma", 3, "src/main.go", "@src/main.go ", 13],
    ["read @ma please", 8, "src/main.go", "read @src/main.go please", 17],
    ["read @mafoo", 8, "src/main.go", "read @src/main.go ", 18],
    ["@my", 3, "docs/my file.md", '@"docs/my file.md" ', 19],
    ["@sr", 3, "src/", "@src/", 5],
    ["@sr", 3, "my src/", '@"my src/" ', 11],
  ])("%j", (text, caret, path, want, wantCaret) => {
    const token = mentionAt(text, caret)!;
    expect(insertMention(text, token, path)).toEqual({ text: want, caret: wantCaret });
  });
});

describe("appendMention", () => {
  it.each([
    ["", "a.go", "@a.go "],
    ["explain", "a.go", "explain @a.go "],
    ["explain ", "a b.go", 'explain @"a b.go" '],
    ["see @a.go ", "a.go", "see @a.go "],
    ["see @a.gox", "a.go", "see @a.gox @a.go "],
  ])("%j + %s", (draft, path, want) => {
    expect(appendMention(draft, path)).toBe(want);
  });
  it("quotes paths with spaces", () => expect(mentionText("a b")).toBe('@"a b"'));
});

describe("isImagePath", () => {
  it.each([
    ["a.png", true],
    ["b/C.JPEG", true],
    ["x.webp", true],
    ["x.svg", false],
    ["png", false],
    ["src/", false],
  ])("%s", (p, want) => expect(isImagePath(p)).toBe(want));
});
