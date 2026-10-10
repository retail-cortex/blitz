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
import { fenceFile, servedExtension } from "./codeBlock";

describe("a code block's language", () => {
  it.each([
    ["go", "x.go", ".go"],
    ["TypeScript", "x.ts", ".ts"],
    ["tsx", "x.tsx", ".tsx"],
    ["javascript", "x.js", ".js"],
    ["jsx {title=a}", "x.jsx", ".jsx"],
    ["py", "x.py", ".py"],
    ["python", "x.py", ".py"],
    ["rust", "x.rs", ""], // rust-analyzer needs a crate
    ["r", "x.r", ""],
    ["mermaid", "x.mermaid", ""],
    ["", "x.txt", ""],
    [null, "x.txt", ""],
  ])("%s is %s, served as %j", (lang, file, ext) => {
    expect(fenceFile(lang)).toBe(file);
    expect(servedExtension(lang)).toBe(ext);
  });
});
