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
import { selectionText } from "./compose";

describe("selectionText", () => {
  it("says where the code is from, fenced with its language", () => {
    expect(selectionText("pkg/a.go", 3, 5, "go", "func A() {\n}\n")).toBe("`pkg/a.go` lines 3–5:\n```go\nfunc A() {\n}\n```");
  });
  it("names a single line", () => {
    expect(selectionText("a.ts", 7, 7, "typescript", "x()")).toBe("`a.ts` line 7:\n```typescript\nx()\n```");
  });
  it("fences code that holds a fence with a longer one", () => {
    expect(selectionText("README.md", 1, 3, "markdown", "```sh\nls\n```")).toContain("````markdown\n```sh");
  });
});
