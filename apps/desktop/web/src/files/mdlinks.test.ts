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
import { resolveLink, slug, slugger, type LinkTarget } from "./mdlinks";

describe("resolveLink", () => {
  it.each<[string, string, string | null, LinkTarget]>([
    ["web", "https://example.com/a", "docs/a.md", { kind: "url", href: "https://example.com/a" }],
    ["mail", "mailto:me@example.com", "docs/a.md", { kind: "url", href: "mailto:me@example.com" }],
    ["protocol-relative", "//example.com", "docs/a.md", { kind: "url", href: "//example.com" }],
    ["anchor", "#getting-started", "docs/a.md", { kind: "anchor", id: "getting-started" }],
    ["sibling", "b.md", "docs/a.md", { kind: "path", path: "docs/b.md", folder: false }],
    ["dot sibling", "./b.md", "docs/a.md", { kind: "path", path: "docs/b.md", folder: false }],
    ["parent with anchor", "../README.md#install", "docs/a.md", { kind: "path", path: "README.md", folder: false, anchor: "install" }],
    ["line", "../src/main.go#L10", "docs/a.md", { kind: "path", path: "src/main.go", folder: false, line: 10 }],
    ["line range", "main.go#L10-L20", "a.md", { kind: "path", path: "main.go", folder: false, line: 10 }],
    ["root-relative", "/pkg/x.go", "docs/deep/a.md", { kind: "path", path: "pkg/x.go", folder: false }],
    ["folder", "../pkg/", "docs/a.md", { kind: "path", path: "pkg", folder: true }],
    ["workspace root", "..", "docs/a.md", { kind: "path", path: "", folder: true }],
    ["escapes", "../../etc/passwd", "docs/a.md", { kind: "outside" }],
    ["escapes from root", "../x.md", "a.md", { kind: "outside" }],
    ["encoded", "my%20notes.md", "a.md", { kind: "path", path: "my notes.md", folder: false }],
    ["query dropped", "b.md?raw=1", "a.md", { kind: "path", path: "b.md", folder: false }],
    ["chat starts at the root", "docs/b.md", null, { kind: "path", path: "docs/b.md", folder: false }],
  ])("%s", (_name, href, doc, want) => {
    expect(resolveLink(href, doc)).toEqual(want);
  });
});

describe("slug", () => {
  it.each([
    ["Getting Started", "getting-started"],
    ["What's new in v2.0?", "whats-new-in-v20"],
    ["  `code` & more ", "code--more"],
    ["Café déjà", "café-déjà"],
  ])("%s", (text, want) => {
    expect(slug(text)).toBe(want);
  });

  it("numbers repeats", () => {
    const s = slugger();
    expect([s("Usage"), s("Usage"), s("Other"), s("Usage")]).toEqual(["usage", "usage-1", "other", "usage-2"]);
  });
});
