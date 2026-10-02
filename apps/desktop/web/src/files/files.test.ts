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
import { detectIndent } from "./indent";
import { parseFileRef, splitLine } from "./paths";
import { ancestors, emptyTree, moveTarget, parentOf, rows, setExpanded, shownFolders, validName, withChildren } from "./tree";

const folder = (path: string) => create(FileEntrySchema, { path, name: path.split("/").pop(), kind: FileKind.FOLDER });
const file = (path: string) => create(FileEntrySchema, { path, name: path.split("/").pop(), kind: FileKind.FILE });

describe("the tree", () => {
  let tree = withChildren(emptyTree, "", [folder("cmd"), folder("internal"), file("go.mod")]);
  tree = withChildren(tree, "internal", [folder("internal/cart"), file("internal/doc.go")]);
  tree = withChildren(tree, "internal/cart", [file("internal/cart/discount.go")]);

  it("shows open folders' entries under them", () => {
    expect(rows(tree).map((r) => r.entry.path)).toEqual(["cmd", "internal", "go.mod"]);
    const open = setExpanded(setExpanded(tree, "internal", true), "internal/cart", true);
    expect(rows(open).map((r) => `${r.depth}:${r.entry.path}`)).toEqual(["0:cmd", "0:internal", "1:internal/cart", "2:internal/cart/discount.go", "1:internal/doc.go", "0:go.mod"]);
    expect(shownFolders(open)).toEqual(["", "internal", "internal/cart"]);
  });
  it("closes a folder with the folders under it", () => {
    const open = setExpanded(setExpanded(tree, "internal", true), "internal/cart", true);
    expect([...setExpanded(open, "internal", false).expanded]).toEqual([]);
  });
  it("forgets folders that are gone", () => {
    const open = setExpanded(setExpanded(tree, "internal", true), "internal/cart", true);
    const after = withChildren(open, "internal", [file("internal/doc.go")]);
    expect(after.children.has("internal/cart")).toBe(false);
    expect([...after.expanded]).toEqual(["internal"]);
  });
  it("names paths", () => {
    expect(parentOf("a/b/c.go")).toBe("a/b");
    expect(parentOf("c.go")).toBe("");
    expect(ancestors("a/b/c.go")).toEqual(["a", "a/b"]);
    expect(["x.go", ".env"].every(validName)).toBe(true);
    expect(["", " ", ".", "..", "a/b", "a\\b"].some(validName)).toBe(false);
  });
  it.each([
    ["into a folder", "go.mod", "internal", "internal/go.mod"],
    ["out to the workspace", "internal/doc.go", "", "doc.go"],
    ["a folder into another", "internal/cart", "cmd", "cmd/cart"],
    ["into a folder with a name it starts with", "internal", "internal-old", "internal-old/internal"],
    ["onto itself", "internal", "internal", null],
    ["under itself", "internal", "internal/cart", null],
    ["where it already is", "internal/doc.go", "internal", null],
    ["the workspace", "", "cmd", null],
  ])("moves %s", (_, path, into, want) => {
    expect(moveTarget(path, into)).toBe(want);
  });
});

describe("file references", () => {
  const dir = "/Users/x/shop";
  it("finds paths, lines and columns", () => {
    expect(parseFileRef("internal/cart/discount.go", dir)).toEqual({ path: "internal/cart/discount.go" });
    expect(parseFileRef("discount.go:42", dir)).toEqual({ path: "discount.go", line: 42 });
    expect(parseFileRef("./a/b.ts:3:7", dir)).toEqual({ path: "a/b.ts", line: 3, column: 7 });
    expect(parseFileRef("/Users/x/shop/go.mod", dir)).toEqual({ path: "go.mod" });
  });
  it("leaves out what isn't a workspace path", () => {
    for (const s of ["go test ./...", "https://x.dev/a.go", "/etc/hosts", "../up.go", "ApplyCoupon", "~/x.go", "cmd/"]) expect(parseFileRef(s, dir)).toBeNull();
  });
  it("splits a line from Go to file's query", () => {
    expect(splitLine("disc:12")).toEqual({ query: "disc", line: 12, column: undefined });
    expect(splitLine("disc")).toEqual({ query: "disc" });
  });
});

describe("indentation", () => {
  it("detects tabs and spaces", () => {
    expect(detectIndent("func a() {\n\treturn\n}\n")).toBe("\t");
    expect(detectIndent("a:\n    b:\n        c: 1\n    d: 2\n")).toBe("    ");
    expect(detectIndent("{\n  \"a\": {\n    \"b\": 1\n  }\n}\n")).toBe("  ");
    expect(detectIndent("plain\ntext\n", "  ")).toBe("  ");
  });
});
