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

import { fromMarkdown } from "mdast-util-from-markdown";
import { mathFromMarkdown } from "mdast-util-math";
import { math } from "micromark-extension-math";
import { describe, expect, it } from "vitest";
import { isInlineMath, keepGitHubMath } from "./math";

// The inline math GitHub would read in s.
function inlineMath(s: string): string[] {
  const tree = fromMarkdown(s, { extensions: [math()], mdastExtensions: [mathFromMarkdown()] });
  keepGitHubMath(tree as Parameters<typeof keepGitHubMath>[0], s);
  const out: string[] = [];
  const walk = (n: { type: string; value?: string; children?: unknown[] }) => {
    if (n.type === "inlineMath") out.push(n.value ?? "");
    (n.children as (typeof n)[] | undefined)?.forEach(walk);
  };
  walk(tree);
  return out;
}

describe("math as GitHub reads it", () => {
  it.each([
    ["inline $x^2$ here", ["x^2"]],
    ["We use ($T$) and ($Sal$).", ["T", "Sal"]],
    ["costs $5 and $10 today", []],
    ["money $5, more $6.", []],
    ["$ x $ spaced", []],
    ["a $$b + c$$ d", ["b + c"]],
    ["escaped \\$5 and \\$6", []],
    ["$x$5 is followed by a digit", []],
    ["two $a$ and $b$", ["a", "b"]],
  ])("%s has %j", (s, want) => {
    expect(inlineMath(s)).toEqual(want);
  });

  it("reads the dollars around the math", () => {
    const s = "a $x$ b";
    expect(isInlineMath(s, 2, 5)).toBe(true);
    expect(isInlineMath("a $$ b", 2, 4)).toBe(true); // $$…$$ always
    expect(isInlineMath("$ x$", 0, 4)).toBe(false);
    expect(isInlineMath("$x $", 0, 4)).toBe(false);
  });
});
