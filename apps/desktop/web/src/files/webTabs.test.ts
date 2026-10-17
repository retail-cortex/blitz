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
import { consoleBlock, covered, isLocalLink, isWebPath, maxBlockHTML, newWebId, pageBlock, webId, webPath, webTabName } from "./webTabs";

describe("web tabs", () => {
  it("are paths of their own, by their preview's ID", () => {
    const id = newWebId();
    expect(id).toMatch(/^[\w-]{1,64}$/);
    expect(newWebId()).not.toBe(id);
    expect(isWebPath(webPath(id))).toBe(true);
    expect(webId(webPath(id))).toBe(id);
    expect(isWebPath("src/web.ts")).toBe(false);
  });
  it.each([
    { url: "http://localhost:3000/", title: "  My app ", want: "My app" },
    { url: "http://localhost:3000/", title: "", want: "localhost:3000" },
    { url: "https://example.com/docs/a", title: "", want: "example.com/docs/a" },
    { url: "", title: "", want: "" },
    { url: "not a url", title: "", want: "not a url" },
  ])("are named $want", ({ url, title, want }) => expect(webTabName(url, title)).toBe(want));
});

describe("isLocalLink", () => {
  it.each([
    { href: "http://localhost:3000/", want: true },
    { href: "https://localhost/", want: true },
    { href: "http://127.0.0.1:8000/x", want: true },
    { href: "http://[::1]:5173/", want: true },
    { href: "http://app.localhost:3000/", want: true },
    { href: "https://example.com/", want: false },
    { href: "http://localhost.example.com/", want: false },
    { href: "file:///tmp/index.html", want: false },
    { href: "mailto:me@localhost", want: false },
    { href: "src/app.ts", want: false },
  ])("$href", ({ href, want }) => expect(isLocalLink(href)).toBe(want));
});

describe("covered", () => {
  const box = { left: 100, top: 100, right: 300, bottom: 300 };
  it("is whether something overlaps the box", () => {
    expect(covered(box, [])).toBe(false);
    expect(covered(box, [{ left: 0, top: 0, right: 50, bottom: 50 }])).toBe(false);
    expect(covered(box, [{ left: 300, top: 100, right: 400, bottom: 300 }])).toBe(false); // touching
    expect(covered(box, [{ left: 250, top: 0, right: 400, bottom: 120 }])).toBe(true);
    expect(covered(box, [{ left: 0, top: 0, right: 1000, bottom: 1000 }])).toBe(true); // a scrim
  });
});

describe("Add to chat", () => {
  const c = { url: "http://localhost:3000/", title: "Home", html: "<p>page</p>", selection: "<b>sel</b>" };
  it("sends the page, or the selection", () => {
    expect(pageBlock(c, "page")).toEqual({ kind: "web_page", url: c.url, title: "Home", html: "<p>page</p>", text: "" });
    expect(pageBlock(c, "selection")).toEqual({ kind: "web_selection", url: c.url, title: "Home", html: "<b>sel</b>", text: "" });
    expect(pageBlock({ ...c, html: "x".repeat(maxBlockHTML + 5) }, "page").html).toHaveLength(maxBlockHTML);
  });
  it("sends the console's errors as text", () => {
    expect(consoleBlock(c.url, "Home", ["a", "b"])).toEqual({ kind: "console", url: c.url, title: "Home", html: "", text: "a\nb" });
  });
});
