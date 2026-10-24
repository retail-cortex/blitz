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
import { chunkBytes, importDrop, keepBoth, readDrop, someNames, uploadFile, type DropItem, type UploadClient } from "./upload";

/** A service that keeps files in a map, as UploadFile does: a file appears with its last chunk. */
function fakeService() {
  const files = new Map<string, string>();
  const folders = new Set<string>();
  const pending = new Map<string, { path: string; data: Uint8Array[] }>();
  const calls: string[] = [];
  let next = 0;
  const c: UploadClient = {
    async uploadFile({ path, uploadId, data, done, replace }) {
      calls.push(`upload ${path} ${data.length}${done ? " done" : ""}`);
      const id = uploadId || `u${++next}`;
      const p = pending.get(id) ?? { path, data: [] };
      p.data.push(data);
      pending.set(id, p);
      if (!done) return { uploadId: id, version: "" };
      pending.delete(id);
      if (files.has(path) && !replace) throw new Error(`${path} exists`);
      const all = new Uint8Array(p.data.reduce((n, d) => n + d.length, 0));
      let at = 0;
      for (const d of p.data) {
        all.set(d, at);
        at += d.length;
      }
      files.set(path, new TextDecoder().decode(all));
      return { uploadId: id, version: `v${all.length}` };
    },
    async createFolder({ path }) {
      calls.push(`mkdir ${path}`);
      if (folders.has(path)) throw new Error(`${path} exists`);
      folders.add(path);
      return {};
    },
    async deleteFile({ path }) {
      calls.push(`rm ${path}`);
      folders.delete(path);
      for (const k of [...files.keys()]) if (k === path || k.startsWith(path + "/")) files.delete(k);
      return {};
    },
  };
  return { c, files, folders, calls };
}

const file = (name: string, text: string) => new File([text], name);
const item = (index: number, name: string, text: string): DropItem => ({ index, name, file: file(name, text), inside: [] });

describe("keepBoth", () => {
  it.each([
    ["a name that's free", "a.png", [], "a.png"],
    ["the next number", "a.png", ["a.png"], "a 2.png"],
    ["past numbers taken", "a.png", ["a.png", "a 2.png", "a 3.png"], "a 4.png"],
    ["a folder or a file without an extension", "docs", ["docs"], "docs 2"],
    ["a dotfile keeps its name whole", ".env", [".env"], ".env 2"],
  ])("%s", (_name, name, taken, want) => expect(keepBoth(name, new Set(taken))).toBe(want));
});

describe("someNames", () => {
  it.each([
    [["a"], "a"],
    [["a", "b", "c", "d"], "a, b, c, d"],
    [["a", "b", "c", "d", "e"], "a, b, c, d, …"],
  ])("%j", (names, want) => expect(someNames(names)).toBe(want));
});

describe("uploadFile", () => {
  it("sends a small file in one call", async () => {
    const s = fakeService();
    expect(await uploadFile(s.c, "/w", "a.txt", file("a.txt", "hello"), false)).toBe("v5");
    expect(s.calls).toEqual(["upload a.txt 5 done"]);
  });
  it("sends a big one in chunks, the same upload throughout", async () => {
    const s = fakeService();
    const big = new Blob([new Uint8Array(chunkBytes * 2 + 3)]);
    await uploadFile(s.c, "/w", "big.bin", big, false);
    expect(s.calls).toEqual([`upload big.bin ${chunkBytes}`, `upload big.bin ${chunkBytes}`, "upload big.bin 3 done"]);
    expect(s.files.get("big.bin")?.length).toBe(chunkBytes * 2 + 3);
  });
  it("sends an empty file", async () => {
    const s = fakeService();
    await uploadFile(s.c, "/w", "empty", new Blob([]), false);
    expect(s.files.get("empty")).toBe("");
  });
});

describe("importDrop", () => {
  const folderItem: DropItem = {
    index: 1,
    name: "pics",
    inside: [{ path: "a.png", file: file("a.png", "A") }, { path: "old/" }, { path: "old/b.png", file: file("b.png", "B") }],
  };

  it("puts files and folders in the folder", async () => {
    const s = fakeService();
    const r = await importDrop(s.c, "/w", "docs", [item(0, "n.md", "note"), folderItem], new Set(), "skip");
    expect(r.added).toEqual(["docs/n.md", "docs/pics"]);
    expect(r.done.map((d) => d.index)).toEqual([0, 1]);
    expect(Object.fromEntries(s.files)).toEqual({ "docs/n.md": "note", "docs/pics/a.png": "A", "docs/pics/old/b.png": "B" });
    expect([...s.folders]).toEqual(["docs/pics", "docs/pics/old"]);
  });

  it.each([
    ["skip leaves what's there", "skip" as const, [], { "n.md": "old" }],
    ["keep both puts it beside", "keep" as const, ["n 2.md"], { "n.md": "old", "n 2.md": "new" }],
    ["replace replaces it", "replace" as const, ["n.md"], { "n.md": "new" }],
  ])("a name already there: %s", async (_name, onConflict, added, want) => {
    const s = fakeService();
    s.files.set("n.md", "old");
    const r = await importDrop(s.c, "/w", "", [item(0, "n.md", "new")], new Set(["n.md"]), onConflict);
    expect(r.added).toEqual(added);
    expect(Object.fromEntries(s.files)).toEqual(want);
  });

  it("replaces a folder with everything in it", async () => {
    const s = fakeService();
    s.folders.add("pics");
    s.files.set("pics/stale.png", "S");
    await importDrop(s.c, "/w", "", [folderItem], new Set(["pics"]), "replace");
    expect(s.files.has("pics/stale.png")).toBe(false);
    expect(s.files.get("pics/a.png")).toBe("A");
  });

  it("keeps two dropped items with one name apart", async () => {
    const s = fakeService();
    const r = await importDrop(s.c, "/w", "", [item(0, "a.txt", "1"), item(1, "a.txt", "2")], new Set(), "keep");
    expect(r.added).toEqual(["a.txt", "a 2.txt"]);
  });

  it("stops at a failure, reporting what was done", async () => {
    const s = fakeService();
    s.folders.add("pics"); // createFolder fails: it exists, and the conflict wasn't known
    await expect(importDrop(s.c, "/w", "", [item(0, "a.txt", "1"), folderItem], new Set(), "skip")).rejects.toThrow("pics exists");
    expect(s.files.get("a.txt")).toBe("1");
  });
});

describe("readDrop", () => {
  // Entries as the browser's File System API gives them.
  const fileEntry = (f: File) => ({ isFile: true, isDirectory: false, name: f.name, file: (ok: (f: File) => void) => ok(f) }) as unknown as FileSystemEntry;
  const dirEntry = (name: string, children: FileSystemEntry[]) =>
    ({
      isFile: false,
      isDirectory: true,
      name,
      createReader: () => {
        // Two batches, then none: readEntries is called until it's empty.
        const batches = [children.slice(0, 1), children.slice(1), []];
        return { readEntries: (ok: (e: FileSystemEntry[]) => void) => ok(batches.shift() ?? []) };
      },
    }) as unknown as FileSystemEntry;

  it("walks folders, keeping each item's place", async () => {
    const got = await readDrop([fileEntry(file("top.txt", "t")), dirEntry("pics", [fileEntry(file("a.png", "A")), dirEntry("sub", [fileEntry(file("b.png", "B"))])])]);
    expect(got.map((g) => [g.index, g.name, g.inside.map((x) => x.path)])).toEqual([
      [0, "top.txt", []],
      [1, "pics", ["a.png", "sub/", "sub/b.png"]],
    ]);
  });
  it("takes plain files where there are no entries", async () => {
    const got = await readDrop([file("a.txt", "a")]);
    expect(got).toEqual([{ index: 0, name: "a.txt", file: expect.any(File), inside: [] }]);
  });
});
