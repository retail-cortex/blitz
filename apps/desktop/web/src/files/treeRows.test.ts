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

import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { FileEntrySchema, FileKind } from "../gen/blitz/v1/file_pb";
import { copyName, emptyTree, nestParents, nextMatch, problemsUp, rangeOf, rowLabel, rows, setExpanded, sortEntries, topmost, validNewName, withChildren } from "./tree";

const folder = (path: string) => create(FileEntrySchema, { path, name: path.split("/").pop(), kind: FileKind.FOLDER });
const file = (path: string) => create(FileEntrySchema, { path, name: path.split("/").pop(), kind: FileKind.FILE });
const names = (es: { name: string }[]) => es.map((e) => e.name);

describe("sorting (FIL-48)", () => {
  const entries = [file("b.md"), folder("src"), file("a.go"), file("a10.go"), file("a2.go"), folder("Docs")];
  it.each([
    ["folders", ["Docs", "src", "a.go", "a2.go", "a10.go", "b.md"]],
    ["mixed", ["a.go", "a2.go", "a10.go", "b.md", "Docs", "src"]],
    ["type", ["Docs", "src", "a.go", "a2.go", "a10.go", "b.md"]],
  ] as const)("%s", (sort, want) => {
    expect(names(sortEntries(entries, sort))).toEqual(want);
  });
  it("by type groups extensions", () => {
    expect(names(sortEntries([file("z.go"), file("a.md"), file("b.go")], "type"))).toEqual(["b.go", "z.go", "a.md"]);
  });
});

describe("nesting (FIL-48)", () => {
  const entries = [file("app.ts"), file("app.test.ts"), file("app.d.ts"), file("package.json"), file("pnpm-lock.yaml"), file("main.go"), file("main_test.go"), file("lonely.test.ts"), folder("x.test.ts")];
  it("nests tests, declarations and locks under their file", () => {
    expect(Object.fromEntries(nestParents(entries))).toEqual({
      "app.test.ts": "app.ts",
      "app.d.ts": "app.ts",
      "pnpm-lock.yaml": "package.json",
      "main_test.go": "main.go",
    });
  });
  it("shows nested files under their file when it's open", () => {
    const tree = withChildren(emptyTree, "", entries);
    const shut = rows(tree, "", 0, { nest: true });
    expect(shut.map((r) => r.entry.name)).toEqual(["x.test.ts", "app.ts", "lonely.test.ts", "main.go", "package.json"]);
    expect(shut.find((r) => r.entry.name === "app.ts")?.nests).toBe(2);
    const open = rows(setExpanded(tree, "app.ts", true), "", 0, { nest: true });
    expect(open.slice(1, 4).map((r) => `${r.depth}:${r.entry.name}`)).toEqual(["0:app.ts", "1:app.d.ts", "1:app.test.ts"]);
    expect(rows(tree).length).toBe(entries.length);
  });
});

describe("compact folders (FIL-47)", () => {
  let tree = withChildren(emptyTree, "", [folder("src"), file("go.mod")]);
  tree = withChildren(tree, "src", [folder("src/main")]);
  tree = withChildren(tree, "src/main", [folder("src/main/java")]);
  tree = withChildren(tree, "src/main/java", [file("src/main/java/App.java"), file("src/main/java/Util.java")]);
  const open = ["src", "src/main", "src/main/java"].reduce((t, p) => setExpanded(t, p, true), tree);
  it("shows a single-child chain on one row", () => {
    const shown = rows(open, "", 0, { compact: true });
    expect(shown.map((r) => `${r.depth}:${rowLabel(r)}`)).toEqual(["0:src/main/java", "1:App.java", "1:Util.java", "0:go.mod"]);
    expect(shown[0].entry.path).toBe("src/main/java");
    expect(shown[0].chain?.map((e) => e.path)).toEqual(["src", "src/main", "src/main/java"]);
  });
  it("stops at a closed folder, and isn't on unless asked", () => {
    const part = setExpanded(open, "src/main/java", false);
    expect(rows(part, "", 0, { compact: true }).map(rowLabel)).toEqual(["src/main/java", "go.mod"]);
    expect(rows(open).map(rowLabel).slice(0, 3)).toEqual(["src", "main", "java"]);
  });
});

describe("names and selections", () => {
  it("new names may make folders (FIL-49)", () => {
    expect(validNewName("a/b/c.md")).toBe(true);
    expect(validNewName("notes.md")).toBe(true);
    for (const bad of ["a//b", "../x", "a/./b", "/a", "a/"]) expect(validNewName(bad)).toBe(false);
  });
  it("type-to-find goes to the next match, round (FIL-73)", () => {
    const shown = rows(withChildren(emptyTree, "", [file("alpha"), file("beta"), file("apple")]));
    expect(nextMatch(shown, 0, "a")).toBe(1);
    expect(nextMatch(shown, 1, "a")).toBe(0);
    expect(nextMatch(shown, 0, "BE")).toBe(2);
    expect(nextMatch(shown, 0, "z")).toBe(-1);
    expect(nextMatch(shown, 0, "")).toBe(-1);
  });
  it("ranges and topmost paths (FIL-74)", () => {
    const shown = rows(withChildren(emptyTree, "", [file("a"), file("b"), file("c"), file("d")]));
    expect(rangeOf(shown, "c", "a")).toEqual(["a", "b", "c"]);
    expect(rangeOf(shown, "x", "b")).toEqual(["b"]);
    expect(rangeOf(shown, "a", "x")).toEqual([]);
    expect(topmost(["src/a.go", "src", "docs/x.md", "src/sub/b.go", "docs/x.md"])).toEqual(["docs/x.md", "src"]);
  });
  it("copies' names (FIL-38)", () => {
    expect(copyName("a.md", new Set())).toBe("a.md");
    expect(copyName("a.md", new Set(["a.md"]))).toBe("a copy.md");
    expect(copyName("a.md", new Set(["a.md", "a copy.md"]))).toBe("a copy 2.md");
    expect(copyName("src", new Set(["src"]))).toBe("src copy");
    expect(copyName(".env", new Set([".env"]))).toBe(".env copy");
  });
  it("problems colour their folders (FIL-46)", () => {
    const got = problemsUp(new Map([["src/a/x.go", "warning"], ["src/b.go", "error"], ["docs/y.md", "warning"]]));
    expect(Object.fromEntries(got)).toEqual({
      "src/a/x.go": "warning",
      "src/b.go": "error",
      "docs/y.md": "warning",
      src: "error",
      "src/a": "warning",
      docs: "warning",
    });
  });
});
