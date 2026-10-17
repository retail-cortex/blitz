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

import { Window } from "happy-dom";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { pageMarkdown, pagePictures } from "./webMarkdown";
import { pageHTML, pictureName, picturesFolder, saveName, savePath } from "./webSave";

// The Markdown is made with the window's DOM: happy-dom's here (the tests
// otherwise run without one).
const dom = new Window();
beforeAll(() => {
  vi.stubGlobal("window", dom);
  vi.stubGlobal("document", dom.document);
  vi.stubGlobal("DOMParser", dom.DOMParser);
});
afterAll(() => {
  vi.unstubAllGlobals();
  void dom.happyDOM.close();
});

const when = new Date(2026, 9, 16, 23, 30); // late in the day, local time

describe("saveName", () => {
  it.each([
    { url: "http://localhost:3000/", title: "Shop · Cart", want: "Shop · Cart" },
    { url: "http://localhost:3000/", title: "  a/b: c?  ", want: "a-b- c" },
    { url: "https://example.com/docs/intro/", title: "", want: "example.com-docs-intro" },
    { url: "not a url", title: " ", want: "page" },
    { url: "", title: "...", want: "page" },
  ])("$title $url", ({ url, title, want }) => expect(saveName(url, title)).toBe(want));
  it("keeps names short", () => expect(saveName("", "x".repeat(200))).toHaveLength(80));
});

describe("savePath", () => {
  it.each([
    { folder: "", name: "Cart", format: "md" as const, want: { path: "Cart.md" } },
    { folder: "docs/web/", name: "Cart", format: "html" as const, want: { path: "docs/web/Cart.html" } },
    { folder: " ./docs ", name: "Cart.pdf", format: "pdf" as const, want: { path: "docs/Cart.pdf" } },
    { folder: "docs\\web", name: "Cart.md", format: "md" as const, want: { path: "docs/web/Cart.md" } },
    { folder: "../out", name: "Cart", format: "md" as const, want: { error: "desktop.web.save.outside" } },
    { folder: "/etc", name: "Cart", format: "md" as const, want: { error: "desktop.web.save.outside" } },
    { folder: "C:\\x", name: "Cart", format: "md" as const, want: { error: "desktop.web.save.outside" } },
    { folder: "docs", name: " ", format: "md" as const, want: { error: "desktop.web.save.bad_name" } },
    { folder: "docs", name: "a/b", format: "md" as const, want: { error: "desktop.web.save.bad_name" } },
  ])("$folder $name", ({ folder, name, format, want }) => expect(savePath(folder, name, format)).toEqual(want));
});

describe("pageHTML", () => {
  it("adds a base and a note of where it came from", () => {
    const out = pageHTML('<html><head lang="en"><title>T</title></head><body></body></html>', "http://localhost:3000/a?b=1&c=2", when);
    expect(out).toBe('<!-- Saved by Blitz from http://localhost:3000/a?b=1&c=2 on 2026-10-16 -->\n<html><head lang="en"><base href="http://localhost:3000/a?b=1&amp;c=2"><title>T</title></head><body></body></html>');
  });
  it("keeps a page's own base, and copes without a head", () => {
    expect(pageHTML('<head><base href="/x/"></head>', "http://h/", when)).toBe('<!-- Saved by Blitz from http://h/ on 2026-10-16 -->\n<head><base href="/x/"></head>');
    expect(pageHTML("<p>hi</p>", "http://h/--x", when)).toBe('<!-- Saved by Blitz from http://h/%2D%2Dx on 2026-10-16 -->\n<base href="http://h/--x">\n<p>hi</p>');
  });
});

describe("pageMarkdown", () => {
  it("writes the main content as the visual editor writes Markdown", () => {
    const html = `<html><head><title>Cart</title><style>p{}</style></head><body>
      <nav><a href="/">Home</a></nav>
      <main><h2>Your cart</h2><p>Three items, <b>15% off</b>. <a href="/terms">Terms</a></p>
      <ul><li>Apples</li><li>Pears</li></ul><img src="logo.png" alt="Logo"><script>evil()</script><button>Pay</button></main>
    </body></html>`;
    const md = pageMarkdown(html, "http://localhost:3000/cart/", "Cart", when);
    expect(md).toContain("# Cart\n\nSaved from <http://localhost:3000/cart/> on 2026-10-16.\n\n");
    expect(md).toContain("## Your cart");
    expect(md).toContain("**15% off**");
    expect(md).toContain("[Terms](http://localhost:3000/terms)");
    expect(md).toMatch(/- Apples\n- Pears/);
    expect(md).toContain("![Logo](http://localhost:3000/cart/logo.png)");
    for (const gone of ["evil", "Pay", "Home", "p{}"]) expect(md).not.toContain(gone);
  });
  it("keeps the page's own first heading, the note under it, and takes the body without a main", () => {
    const md = pageMarkdown("<body><h1>Docs</h1><p>Hi</p></body>", "https://example.com/", "Docs", when);
    expect(md).toBe("# Docs\n\nSaved from <https://example.com/> on 2026-10-16.\n\nHi\n");
  });
});

describe("the page's pictures", () => {
  const html = `<body><nav><img src="/nav.png"></nav><main><img src="a.png" alt="A"><img src="/b.svg"><img src="a.png"><img src="data:image/gif;base64,R0lG"></main></body>`;
  it("are the web pictures of the main content, each once", () => {
    expect(pagePictures(html, "http://localhost:3000/shop/")).toEqual(["http://localhost:3000/shop/a.png", "http://localhost:3000/b.svg"]);
  });
  it("show from beside the Markdown when saved there, else from the site", () => {
    const md = pageMarkdown(html, "http://localhost:3000/shop/", "Shop", when, new Map([["http://localhost:3000/shop/a.png", "Shop · Cart_files/a.png"]]));
    expect(md).toContain("![A](<Shop · Cart_files/a.png>)");
    expect(md).toContain("](http://localhost:3000/b.svg)");
  });
  it("go in a folder named after the file", () => {
    expect(picturesFolder("docs/web/Shop · Cart.md")).toBe("docs/web/Shop · Cart_files");
    expect(picturesFolder("cart.md")).toBe("cart_files");
  });
  it("are named after their addresses, once each", () => {
    const taken = new Set<string>();
    expect(pictureName("http://h/img/logo.png?v=2", "png", taken)).toBe("logo.png");
    expect(pictureName("http://h/other/Logo.png", "png", taken)).toBe("Logo-2.png");
    expect(pictureName("http://h/photo%20of%20me.jpeg", "jpg", taken)).toBe("photo-of-me.jpg");
    expect(pictureName("http://h/", "svg", taken)).toBe("picture.svg");
    expect(pictureName("not a url", "gif", taken)).toBe("picture.gif");
  });
});
