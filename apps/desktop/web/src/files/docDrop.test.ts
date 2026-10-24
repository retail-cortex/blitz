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
import { altText, insertMarkdown, spaced, insertsFor, inWorkspace, isImagePath, pastedName, relativeLink, saveFolder } from "./docDrop";
import { resolveLink } from "./mdlinks";
import type { UploadClient } from "./upload";

describe("relativeLink", () => {
  it.each([
    ["beside the document", "docs/a.md", "docs/pic.png", "pic.png"],
    ["in a folder below", "docs/a.md", "docs/images/pic.png", "images/pic.png"],
    ["up and across", "docs/guide/a.md", "assets/pic.png", "../../assets/pic.png"],
    ["from the top level", "README.md", "docs/pic.png", "docs/pic.png"],
    ["to the top level", "docs/a.md", "pic.png", "../pic.png"],
    ["a name with spaces", "a.md", "my pic (1).png", "my%20pic%20(1).png"],
    ["a folder named like the file", "pic/a.md", "pic", "../pic"],
  ])("%s", (_name, doc, target, want) => {
    expect(relativeLink(doc, target)).toBe(want);
    const back = resolveLink(want, doc);
    expect(back.kind === "path" && back.path).toBe(target); // the editor reads it back to the same file
  });
});

describe("the names", () => {
  it.each([
    ["team-photo_2.png", "team photo 2"],
    ["shots/Login screen.webp", "Login screen"],
    [".hidden", ".hidden"],
  ])("altText(%s)", (name, want) => expect(altText(name)).toBe(want));
  it("tells pictures by their extension", () => {
    expect(isImagePath("a/b.PNG")).toBe(true);
    expect(isImagePath("a/b.svg")).toBe(true);
    expect(isImagePath("a/b.pdf")).toBe(false);
    expect(isImagePath("png")).toBe(false);
  });
  it("names a pasted picture by the time", () => {
    expect(pastedName("image/png", new Date(2026, 9, 23, 9, 5))).toBe("pasted-2026-10-23-0905.png");
    expect(pastedName("image/jpeg", new Date(2026, 0, 2, 13, 30))).toBe("pasted-2026-01-02-1330.jpg");
  });
  it("writes Markdown, brackets escaped", () => {
    expect(
      insertMarkdown([
        { kind: "image", src: "a.png", text: "a [draft]" },
        { kind: "link", src: "b.pdf", text: "b.pdf" },
      ]),
    ).toBe("![a \\[draft\\]](a.png)\n[b.pdf](b.pdf)");
  });
});

describe("spaced", () => {
  it.each([
    ["after a word", "end.", "", " ![a](a.png)"],
    ["between words", "a", "b", " ![a](a.png) "],
    ["after a space", "a ", "b", "![a](a.png) "],
    ["at a line's start", "", "", "![a](a.png)"],
    ["before a newline", "a", "\n", " ![a](a.png)"],
  ])("%s", (_name, before, after, want) => expect(spaced(before, "![a](a.png)", after)).toBe(want));
});

describe("inWorkspace", () => {
  it.each([
    ["/home/me/shop", "/home/me/shop/docs/a.png", "docs/a.png"],
    ["/home/me/shop/", "/home/me/shop/a.png", "a.png"],
    ["C:\\Users\\me\\shop", "C:\\Users\\me\\shop\\img\\a.png", "img/a.png"],
    ["/home/me/shop", "/home/me/shopping/a.png", null],
    ["/home/me/shop", "/home/me/Desktop/a.png", null],
  ])("%s ∋ %s", (dir, abs, want) => expect(inWorkspace(dir, abs)).toBe(want));
});

describe("saving beside the document", () => {
  const tree: Record<string, { name: string; folder: boolean }[]> = {
    docs: [
      { name: "a.md", folder: false },
      { name: "pic.png", folder: false },
    ],
    notes: [
      { name: "b.md", folder: false },
      { name: "images", folder: true },
    ],
    "notes/images": [{ name: "shot.png", folder: false }],
  };
  const list = async (f: string) => tree[f] ?? [];

  it("uses the document's folder, or its images/ folder", async () => {
    expect(await saveFolder(list, "docs/a.md")).toEqual({ folder: "docs", taken: new Set(["a.md", "pic.png"]) });
    expect(await saveFolder(list, "notes/b.md")).toEqual({ folder: "notes/images", taken: new Set(["shot.png"]) });
  });

  it("links workspace files and saves the computer's", async () => {
    const uploads: string[] = [];
    const c: UploadClient = {
      uploadFile: async (r) => (uploads.push(r.path), { uploadId: "u", version: "v" }),
      createFolder: async () => ({}),
      deleteFile: async () => ({}),
    };
    const inTree = new File(["x"], "logo.svg");
    const fromDesktop = new File(["y"], "pic.png");
    const screenshot = new File(["z"], "image.png", { type: "image/png" });
    const report = new File(["r"], "report.pdf");
    const where = async (fs: File[]) => fs.map((f) => (f === inTree ? "/w/assets/logo.svg" : f === report ? "/elsewhere/report.pdf" : ""));
    const got = await insertsFor(c, list, "/w", "docs/a.md", { paths: ["docs/old.gif"], files: [inTree, fromDesktop, screenshot, report] }, where);
    expect(uploads[0]).toBe("docs/pic 2.png"); // pic.png is taken
    expect(uploads[1]).toMatch(/^docs\/pasted-\d{4}-\d\d-\d\d-\d{4}\.png$/);
    expect(uploads[2]).toBe("docs/report.pdf");
    expect(got.map((i) => `${i.kind} ${i.src} ${i.text}`)).toEqual([
      "image old.gif old",
      "image ../assets/logo.svg logo",
      "image pic%202.png pic 2",
      expect.stringMatching(/^image pasted-.*\.png pasted /),
      "link report.pdf report.pdf",
    ]);
  });
});
