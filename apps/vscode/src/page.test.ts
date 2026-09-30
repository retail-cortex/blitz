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
import { pageHtml } from "./page";

const index = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <script type="module" crossorigin src="./assets/index-abc.js"></script>
    <link rel="stylesheet" crossorigin href="./assets/index-def.css">
  </head>
  <body><div id="root"></div></body>
</html>`;

describe("pageHtml", () => {
  const html = pageHtml(
    index,
    { dir: "/w/app", api: "http://127.0.0.1:4321", token: "t</script>" },
    { asset: (p) => `vscode-resource://page/${p}`, cspSource: "vscode-src" },
    "N0NCE",
  );
  it("loads the page's files through the webview", () => {
    expect(html).toContain('src="vscode-resource://page/assets/index-abc.js"');
    expect(html).toContain('href="vscode-resource://page/assets/index-def.css"');
  });
  it("tells the page its settings first, safely", () => {
    expect(html.indexOf("window.blitzEditor")).toBeLessThan(html.indexOf("index-abc.js"));
    expect(html).toContain('"token":"t\\u003c/script\\u003e"');
    expect(html).not.toContain("t</script>");
  });
  it("allows only its own scripts and the proxy", () => {
    expect(html).toContain("script-src 'nonce-N0NCE' vscode-src");
    expect(html).toContain("connect-src http://127.0.0.1:4321");
    expect(html).toContain("default-src 'none'");
    expect(html.match(/<script nonce="N0NCE"/g)).toHaveLength(2);
  });
});
