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
import { applyPatch, parsePatch } from "./patch";

const text = "one\ntwo\nthree\nfour\nfive\nsix\nseven\n";

describe("parsePatch", () => {
  it("reads the files and hunks", () => {
    const [p, q] = parsePatch("--- a/x.go\n+++ b/x.go\n@@ -2,2 +2,2 @@\n two\n-three\n+THREE\n--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+hi\n");
    expect(p).toEqual({ oldPath: "x.go", newPath: "x.go", hunks: [{ oldStart: 2, lines: [" two", "-three", "+THREE"] }] });
    expect(q.oldPath).toBeUndefined();
    expect(q.newPath).toBe("new.txt");
  });
  it("reads a deletion", () => {
    const [p] = parsePatch("--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n");
    expect(p.newPath).toBeUndefined();
  });
});

describe("applyPatch", () => {
  it.each([
    ["a change", "@@ -2,3 +2,3 @@\n two\n-three\n+THREE\n four\n", "one\ntwo\nTHREE\nfour\nfive\nsix\nseven\n"],
    ["two hunks", "@@ -1,2 +1,2 @@\n-one\n+ONE\n two\n@@ -6,2 +6,3 @@\n six\n seven\n+eight\n", "ONE\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n"],
    ["a hunk that moved", "@@ -1,2 +1,2 @@\n four\n-five\n+FIVE\n", "one\ntwo\nthree\nfour\nFIVE\nsix\nseven\n"],
    ["a deletion of lines", "@@ -3,2 +3,0 @@\n-three\n-four\n", "one\ntwo\nfive\nsix\nseven\n"],
  ])("%s", (_, hunks, want) => {
    const [p] = parsePatch(`--- a/f\n+++ b/f\n${hunks}`);
    expect(applyPatch(text, p)).toBe(want);
  });
  it("makes a new file", () => {
    const [p] = parsePatch("--- /dev/null\n+++ b/n.txt\n@@ -0,0 +1,2 @@\n+a\n+b\n");
    expect(applyPatch("", p)).toBe("a\nb\n");
  });
  it("refuses a change that no longer fits", () => {
    const [p] = parsePatch("--- a/f\n+++ b/f\n@@ -2,1 +2,1 @@\n-deux\n+DEUX\n");
    expect(() => applyPatch(text, p)).toThrow(/doesn't fit/);
  });
});
