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

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { EditorModel, Tab } from "./useEditor";
import { WebPreview } from "./WebPreview";

const tab = (url: string): Tab & { web: NonNullable<Tab["web"]> } => ({
  path: "web:w1",
  name: "localhost:3000",
  loading: false,
  version: "",
  size: 0,
  binary: false,
  tooLarge: false,
  agentRule: "",
  dirty: false,
  web: { url, title: "", loading: true, back: false, forward: false },
});
const model = { dir: "/home/me/shop", setWeb: () => {} } as unknown as EditorModel;

describe("WebPreview", () => {
  it("asks for an address, with nothing to move or add yet", () => {
    const html = renderToStaticMarkup(<WebPreview tab={tab("")} model={model} />);
    expect(html).toContain('class="editor-head web-bar"');
    expect(html).toMatch(/<input class="web-address"[^>]*value=""/);
    expect(html).toContain('class="editor-note muted"');
    expect(html).toMatch(/<div class="web-view" hidden=""><\/div>/);
    // Back, forward, reload and open in the browser wait for a page.
    expect(html.match(/disabled=""/g)?.length).toBeGreaterThanOrEqual(4);
  });
  it("shows the page's address", () => {
    const html = renderToStaticMarkup(<WebPreview tab={tab("http://localhost:3000/")} model={model} />);
    expect(html).toMatch(/<input class="web-address"[^>]*value="http:\/\/localhost:3000\/"/);
  });
});
