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

import { mkdir, mkdtemp, realpath, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { inside, isPage, pageURL, Pages, typeOf } from "./pageserve";

describe("inside", () => {
  const root = "/w";
  it.each([
    ["index.html", "/w/index.html"],
    ["a/b.js", "/w/a/b.js"],
    ["a/../b.js", "/w/b.js"],
    ["../etc/passwd", "/w/etc/passwd"],
    ["%2e%2e/%2e%2e/etc/passwd", "/w/etc/passwd"],
    ["a%20b.html", "/w/a b.html"],
    ["", "/w"],
  ])("%s", (p, want) => expect(inside(root, p)).toBe(want));
  it.each(["%zz", "a%00b", "a\\..\\b"])("refuses %s", (p) => expect(inside(root, p)).toBeUndefined());
});

describe("names", () => {
  it.each([
    ["page.html", true],
    ["PAGE.HTM", true],
    ["page.html.txt", false],
    ["style.css", false],
  ])("%s is a page: %s", (p, want) => expect(isPage(p)).toBe(want));
  it("types", () => {
    expect(typeOf("a.JS")).toBe("text/javascript; charset=utf-8");
    expect(typeOf("a.unknown")).toBe("application/octet-stream");
  });
  it("encodes each part of a path", () => expect(pageURL("http://h/t", "my site/a#1.html")).toBe("http://h/t/my%20site/a%231.html"));
});

describe("Pages", () => {
  let base = "";
  let ws = "";
  let outside = "";
  const pages = new Pages();
  beforeAll(async () => {
    base = await realpath(await mkdtemp(path.join(tmpdir(), "pageserve-")));
    ws = path.join(base, "ws");
    outside = path.join(base, "secret.txt");
    await mkdir(path.join(ws, "site", "js"), { recursive: true });
    await writeFile(path.join(ws, "site", "index.html"), "<script src=js/app.js></script>");
    await writeFile(path.join(ws, "site", "js", "app.js"), "console.log(1)");
    await writeFile(path.join(ws, "notes.txt"), "notes");
    await writeFile(outside, "secret");
    await symlink(outside, path.join(ws, "site", "leak.txt"));
    await symlink(outside, path.join(ws, "leak.html"));
  });
  afterAll(async () => {
    pages.close();
    await rm(base, { recursive: true, force: true });
  });

  it("serves a page and what it loads, under the workspace's token", async () => {
    const url = await pages.url(ws, "site/index.html");
    expect(url).toMatch(/^http:\/\/127\.0\.0\.1:\d+\/[\w-]{24}\/site\/index\.html$/);
    expect(await pages.url(ws + "/", "site/index.html")).toBe(url);
    let res = await fetch(url);
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("text/html; charset=utf-8");
    expect(res.headers.get("x-content-type-options")).toBe("nosniff");
    expect(await res.text()).toContain("app.js");
    res = await fetch(new URL("js/app.js", url));
    expect(res.headers.get("content-type")).toBe("text/javascript; charset=utf-8");
    expect(await res.text()).toBe("console.log(1)");
    res = await fetch(new URL("../notes.txt", url));
    expect(await res.text()).toBe("notes");
    res = await fetch(url.replace(/index\.html$/, ""));
    expect(await res.text()).toContain("app.js");
    res = await fetch(url, { method: "HEAD" });
    expect(res.status).toBe(200);
  });

  it("serves nothing else", async () => {
    const url = await pages.url(ws, "site/index.html");
    const token = new URL(url).pathname.split("/")[1];
    const at = (p: string) => `${pages.address()}/${token}/${p}`;
    for (const u of [at("site/leak.txt"), at("%2e%2e/secret.txt"), at("site/none.js"), `${pages.address()}/wrong/site/index.html`, at("")]) {
      expect((await fetch(u)).status, u).toBe(404);
    }
    expect((await fetch(url, { method: "POST" })).status).toBe(405);
  });

  it("refuses what isn't a page in a workspace", async () => {
    await expect(pages.url("relative", "a.html")).rejects.toThrow("absolute");
    await expect(pages.url(ws, "notes.txt")).rejects.toThrow("only pages");
    await expect(pages.url(ws, "none.html")).rejects.toThrow("no such page");
    await expect(pages.url(ws, "leak.html")).rejects.toThrow("no such page");
    await expect(pages.url(ws, "../ws/site/../../secret.html")).rejects.toThrow("no such page");
    await expect(pages.url(path.join(ws, "notes.txt"), "a.html")).rejects.toThrow();
  });
});
