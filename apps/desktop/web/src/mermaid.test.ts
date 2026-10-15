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
import { mermaidSources, svgSize } from "./mermaid";

describe("mermaidSources", () => {
  it("finds each ```mermaid block's text as the Word writer reads it", () => {
    const md = "# Doc\n\n```mermaid\ngraph LR\n  a --> b\n```\n\n```go\nx := 1\n```\n\n~~~~ Mermaid\nsequenceDiagram\n  A->>B: hi\n\n~~~~\n\n  ```mermaid\n  graph TD\n    x\n  ```\n";
    expect(mermaidSources(md)).toEqual(["graph LR\n  a --> b", "sequenceDiagram\n  A->>B: hi", "graph TD\n  x"]);
  });
  it("finds none in plain text", () => expect(mermaidSources("no diagrams\n```\nmermaid\n```")).toEqual([]));
});

describe("svgSize", () => {
  it.each([
    ['<svg viewBox="0 0 320 180" style="max-width: 320px">', 320, 180],
    ['<svg width="200" height="100">', 200, 100],
    ["<svg>", 600, 400],
  ])("%s", (svg, width, height) => expect(svgSize(svg)).toEqual({ width, height }));
});
