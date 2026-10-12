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

// Math in Markdown (spec_files_029 FIL-67), as GitHub writes it: $…$
// inline, $$…$$ and ```math blocks, drawn by KaTeX. A single $ is math
// only as GitHub and Pandoc have it (no space inside either dollar, no
// digit after the closing one), so prices stay prices; the chat takes the
// $$ forms only. KaTeX (mathRender.ts, loaded the first time math shows)
// runs untrusted (no \href, \url or \includegraphics), and a formula it
// can't draw shows its source, marked.

/** KaTeX's options everywhere Blitz draws math. */
export const katexOptions = { throwOnError: false, strict: "ignore", trust: false, output: "htmlAndMathml" } as const;

/**
 * Whether the inline math source[start:end] (with its dollars) is math as
 * GitHub has it: $$…$$ always; $…$ when nothing inside touches a dollar
 * with a space and no digit follows the closing one.
 */
export function isInlineMath(source: string, start: number, end: number): boolean {
  const raw = source.slice(start, end);
  if (raw.startsWith("$$")) return true;
  const inner = raw.slice(1, -1);
  if (!inner || /^\s|\s$/.test(inner)) return false;
  return !/\d/.test(source.charAt(end));
}

type MdNode = { type: string; value?: string; children?: MdNode[]; position?: { start: { offset?: number }; end: { offset?: number } } };

/** Turns the inline math in tree (parsed from source) that isn't math as GitHub has it back into its text. */
export function keepGitHubMath(tree: MdNode, source: string) {
  const walk = (n: MdNode) => {
    n.children?.forEach((c, i, all) => {
      const s = c.position?.start.offset, e = c.position?.end.offset;
      if (c.type === "inlineMath" && s !== undefined && e !== undefined && !isInlineMath(source, s, e)) all[i] = { type: "text", value: source.slice(s, e), position: c.position };
      else walk(c);
    });
  };
  walk(tree);
}

/** The remark plugin that applies keepGitHubMath (after remark-math). */
export function githubMath() {
  return (tree: MdNode, file: { value: unknown }) => keepGitHubMath(tree, String(file.value));
}

