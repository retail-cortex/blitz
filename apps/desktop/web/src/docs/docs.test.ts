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
import { docFiles, docPictures } from "./content";
import { docLink, docPicture, docTree, loadDocs, parsePage, pathOf, searchDocs, siteURL, siteURLOf, withRelease } from "./index";
import { docSections } from "./sections";

const { pages, tree } = loadDocs(docFiles);
const carried = new Set(pages.keys());

describe("the docs the app carries", () => {
  it("are the user sections, every page titled", () => {
    expect(pages.size).toBeGreaterThan(20);
    for (const p of ["", "getting-started", "tutorials", "guide", "guide/models", "products/desktop", "releases"]) expect(carried, p).toContain(p);
    expect([...carried].some((p) => p.startsWith("about") || p.startsWith("development"))).toBe(false);
    for (const page of pages.values()) expect(page.title, page.file).not.toBe("");
  });

  it("has every section the Help menu links to, in its order", () => {
    const order = tree.children.map((n) => n.page.path);
    for (const d of docSections) expect(carried, d.page).toContain(d.page);
    const at = docSections.map((d) => order.indexOf(d.page));
    expect(at).toEqual([...at].sort((a, b) => a - b));
  });

  it("lists the sections in the site's order", () => {
    expect(tree.page.path).toBe("");
    const sections = tree.children.map((n) => n.page.path);
    expect(sections).toContain("guide");
    expect(sections.indexOf("getting-started")).toBeLessThan(sections.indexOf("guide"));
    const guide = tree.children.find((n) => n.page.path === "guide")!;
    expect(guide.children.length).toBeGreaterThan(5);
  });

  // Every relative link in a page goes somewhere: a page the app carries,
  // or the website for one it doesn't (the site's build fails on a link
  // to no page at all).
  it.each([...pages.values()].map((p) => [p.file, p]))("%s: its links resolve", (_f, page) => {
    for (const [, href] of page.body.matchAll(/\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g)) {
      if (/^[a-z]+:/i.test(href) || href.startsWith("#")) continue;
      const to = docLink(page, href, carried);
      if (/\.md(#|$)/.test(href) && to.kind === "url") expect(to.href.startsWith(siteURL), `${page.file}: ${href}`).toBe(true);
      if (to.kind === "page") expect(carried.has(to.path), `${page.file}: ${href}`).toBe(true);
    }
  });

  it("has a dark twin for every desktop screenshot", () => {
    const shots = Object.keys(docPictures).filter((p) => p.startsWith("images/desktop/") && !p.endsWith("-dark.png"));
    expect(shots.length).toBeGreaterThan(0);
    for (const p of shots) expect(docPictures[p.replace(/\.png$/, "-dark.png")], p).toBeTruthy();
  });

  it("carries every picture a page shows", () => {
    expect(docPicture(pages.get("products/desktop")!, "../../images/desktop.png", docPictures)).toBeTruthy();
    for (const page of pages.values()) {
      // Pictures outside code: an example in a code block isn't shown.
      const text = page.body.replace(/```[\s\S]*?```/g, "").replace(/`[^`\n]*`/g, "");
      for (const [, src] of text.matchAll(/!\[[^\]]*\]\(([^)\s]+)\)/g)) {
        if (/^[a-z]+:/i.test(src)) continue;
        expect(docPicture(page, src, docPictures), `${page.file}: ${src}`).toBeTruthy();
      }
    }
  });

  it("finds pages by their words", () => {
    const hits = searchDocs([...pages.values()], "spec setup");
    expect(hits.length).toBeGreaterThan(0);
    expect(searchDocs([...pages.values()], "zzzz-no-such-word")).toEqual([]);
    expect(searchDocs([...pages.values()], " ")).toEqual([]);
  });
});

describe("pages and links", () => {
  const page = parsePage("guide/models.md", '---\ntitle: "Models"\nweight: 3\n---\n\n# Choosing\n\nText about [safety](safety.md#audit-log).\n\n## Prices\n');
  const known = new Set(["guide/models", "guide/safety", "guide", ""]);
  it("reads front matter and headings", () => {
    expect(page).toMatchObject({ path: "guide/models", title: "Models", weight: 3, headings: ["Choosing", "Prices"] });
    expect(page.body.startsWith("\n# Choosing")).toBe(true);
    expect(parsePage("x.md", "# Only a heading\n").title).toBe("Only a heading");
    expect(pathOf("_index.md")).toBe("");
    expect(pathOf("guide/_index.md")).toBe("guide");
  });
  it.each([
    ["safety.md#audit-log", { kind: "page", path: "guide/safety", anchor: "audit-log" }],
    ["_index.md", { kind: "page", path: "guide", anchor: undefined }],
    ["../_index.md", { kind: "page", path: "", anchor: undefined }],
    ["models.md#prices", { kind: "anchor", id: "prices" }],
    ["#prices", { kind: "anchor", id: "prices" }],
    ["../about/specs/spec_models_015.md", { kind: "url", href: `${siteURL}about/specs/spec_models_015/` }],
    ["../../../../etc.md", { kind: "url", href: siteURL }],
    ["https://example.com/x", { kind: "url", href: "https://example.com/x" }],
    ["/licenses/x.txt", { kind: "url", href: `${siteURL}licenses/x.txt` }],
    ["../../licenses/x.txt", { kind: "url", href: `${siteURL}licenses/x.txt` }],
    ["../safety/", { kind: "page", path: "guide/safety", anchor: undefined }],
  ])("%s", (href, want) => {
    expect(docLink(page, href, known)).toEqual(want);
  });
  it("addresses pages on the website", () => {
    expect(siteURLOf("guide/models", "prices")).toBe(`${siteURL}guide/models/#prices`);
    expect(siteURLOf("")).toBe(siteURL);
  });
  it("leaves web pictures alone", () => {
    expect(docPicture(page, "https://example.com/x.png", { "x.png": "u" })).toBeUndefined();
    expect(docPicture(page, "/images/a.png", { "images/a.png": "u" })).toBe("u");
    // Dark mode takes a picture's dark twin when there is one.
    const twins = { "images/a.png": "u", "images/a-dark.png": "d" };
    expect(docPicture(page, "/images/a.png", twins, true)).toBe("d");
    expect(docPicture(page, "/images/a.png", twins, false)).toBe("u");
    expect(docPicture(page, "/images/b.png", { "images/b.png": "b" }, true)).toBe("b");
  });
  it("orders a tree by weight, then title", () => {
    const t = docTree([parsePage("_index.md", "# Home"), parsePage("b/_index.md", "---\ntitle: B\nweight: 2\n---\n"), parsePage("a/_index.md", "---\ntitle: A\nweight: 2\n---\n"), parsePage("c/_index.md", "---\ntitle: C\nweight: 1\n---\n"), parsePage("a/x.md", "# X")]);
    expect(t.children.map((n) => n.page.title)).toEqual(["C", "A", "B"]);
    expect(t.children[1].children.map((n) => n.page.title)).toEqual(["X"]);
  });
});

describe("withRelease", () => {
  // Install snippets name the release with a shortcode (no pinned version
  // to bump); the app's copy is its own release's.
  const files = { "guide/github-action.md": "uses: retail-cortex/blitz/action@{{< release >}}\nand {{<release>}}" };
  it("fills in the app's release", () => {
    expect(withRelease(files, "0.6.2")["guide/github-action.md"]).toBe("uses: retail-cortex/blitz/action@v0.6.2\nand v0.6.2");
    expect(withRelease(files, "0.7.0-rc.1")["guide/github-action.md"]).toContain("action@v0.7.0-rc.1");
  });
  it("shows vX.Y.Z when the app isn't a release", () => {
    for (const v of ["dev", "0.6.2-3-gabc1234", "abc1234-dirty.5f3e2a1"]) expect(withRelease(files, v)["guide/github-action.md"]).toContain("action@vX.Y.Z");
  });
  it("leaves no shortcode in the pages the app carries", () => {
    const left = Object.entries(withRelease(docFiles, "0.6.2")).filter(([, text]) => /\{\{[<%]/.test(text.replace(/\{\{<\s*\/?\*/g, "")));
    expect(left.map(([f]) => f)).toEqual([]);
  });
});
