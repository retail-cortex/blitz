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
import { blockAt, defaultSources, findTerms, hitTarget, indexCounts, markTerms, matchSpans, queryTerms, scanPending, startSources, toggleSource } from "./search";

describe("hitTarget", () => {
  it.each([
    [{ source: "files", ref: "a/b.go", line: 12 }, { kind: "file", path: "a/b.go", line: 12 }],
    [{ source: "documents", ref: "Report.PDF", line: 40 }, { kind: "file", path: "Report.PDF", line: undefined }],
    [{ source: "documents", ref: "model.ipynb", line: 7 }, { kind: "file", path: "model.ipynb", line: 7 }],
    [{ source: "chats", ref: "session-1", line: 3 }, { kind: "chat", id: "session-1" }],
    [{ source: "notes", ref: ".blitz/plans/s-1.md", line: 0 }, { kind: "file", path: ".blitz/plans/s-1.md", line: undefined }],
    [{ source: "notes", ref: "20261003-fact", line: 1 }, { kind: "none" }],
    [{ source: "email", ref: "x", line: 1 }, { kind: "none" }],
  ])("%o opens %o", (hit, want) => expect(hitTarget(hit)).toEqual(want));
});

describe("queryTerms and markTerms", () => {
  it("splits words and phrases, longest first", () => {
    expect(queryTerms('Zeppelin "rigid airship"  a')).toEqual(["rigid airship", "zeppelin", "a"]);
    expect(queryTerms("   ")).toEqual([]);
  });
  it("marks every match, whatever its case", () => {
    expect(markTerms("A zeppelin; ZEPPELINS.", ["zeppelin"])).toEqual([
      { text: "A ", mark: false },
      { text: "zeppelin", mark: true },
      { text: "; ", mark: false },
      { text: "ZEPPELIN", mark: true },
      { text: "S.", mark: false },
    ]);
  });
  it("escapes what a term holds and keeps text without terms whole", () => {
    expect(markTerms("f(x) + g(x)", ["f(x)"]).filter((p) => p.mark)).toEqual([{ text: "f(x)", mark: true }]);
    expect(markTerms("plain", [])).toEqual([{ text: "plain", mark: false }]);
    expect(markTerms("", ["x"])).toEqual([]);
  });
});

describe("toggleSource", () => {
  it("adds in order and removes, but never the last", () => {
    expect(toggleSource(defaultSources, "chats")).toEqual(["files", "documents", "chats"]);
    expect(toggleSource(["chats", "files"], "documents")).toEqual(["files", "documents", "chats"]);
    expect(toggleSource(defaultSources, "files")).toEqual(["documents"]);
    expect(toggleSource(["notes"], "notes")).toEqual(["notes"]);
  });
});

describe("where a hit opens", () => {
  it("marks the query's words, long ones cut as the stemmer does", () => {
    expect(findTerms('Indexing "rigid airship" go indexing')).toEqual(["rigid airship", "index", "go"]);
    expect(findTerms("")).toEqual([]);
  });
  it("finds the spans of the words", () => {
    expect(matchSpans("Index the index", ["index"])).toEqual([
      [0, 5],
      [10, 15],
    ]);
    expect(matchSpans("Rounding is rounded.", ["round"])).toEqual([
      [0, 8],
      [12, 19],
    ]);
    expect(matchSpans("roundround", ["round"])).toEqual([[0, 10]]);
    expect(matchSpans("Rounding is rounded.", ["round"], false)).toEqual([
      [0, 5],
      [12, 17],
    ]);
    expect(matchSpans("nothing", ["zeppelin"])).toEqual([]);
  });
  it.each([
    [[1, 4, 9], 1, 0],
    [[1, 4, 9], 6, 1],
    [[1, 4, 9], 30, 2],
    [[3, 4], 1, -1],
    [[], 5, -1],
  ])("blocks starting at %o hold line %i in block %i", (starts, line, want) => expect(blockAt(starts, line)).toBe(want));
});

// N26 desktop-1#21: the dialog always started on files and PDFs, whatever
// the workspace's Search by default in said.
describe("the sources a search starts with", () => {
  it.each([
    { name: "none set", settings: undefined, want: ["files", "documents"] },
    { name: "empty", settings: { sources: [] }, want: ["files", "documents"] },
    { name: "the workspace's", settings: { sources: ["files", "chats", "notes"] }, want: ["files", "chats", "notes"] },
    { name: "unknown left out", settings: { sources: ["web", "notes"] }, want: ["notes"] },
  ])("$name", ({ settings, want }) => expect(startSources(settings)).toEqual(want));
});

// N26 desktop-1#20, #22: the dialog asked forever with search off, and
// its counts left out what couldn't be read.
describe("the index's state", () => {
  const s = { enabled: true, scanning: false, lastScan: {}, items: { files: 3 }, embeddingModel: "", embedded: 0, enriched: 0, unreadable: 2 };
  it("is asked again only while a scan is to come", () => {
    expect(scanPending(s)).toBe(false);
    expect(scanPending({ ...s, scanning: true })).toBe(true);
    expect(scanPending({ ...s, lastScan: undefined })).toBe(true);
    expect(scanPending({ ...s, enabled: false, lastScan: undefined })).toBe(false);
    expect(scanPending(undefined)).toBe(false);
  });
  it("says what it holds, unreadable items too", () => {
    expect(indexCounts(s)).toContain("3");
    expect(indexCounts(s).split(" · ")).toHaveLength(5);
  });
});
