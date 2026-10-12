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

// KaTeX for math in Markdown (math.ts), in a chunk of its own: the chat
// and Preview load it the first time a text has math, and the visual
// editor with its own.
import katex from "katex";
import "katex/dist/katex.min.css";
import type { Options } from "react-markdown";
import rehypeKatex from "rehype-katex";
import { katexOptions } from "./math";

type Pluggable = NonNullable<Options["rehypePlugins"]>[number];

/** The rehype plugin that draws math, with Blitz's KaTeX options. */
export const rehypeMath: Pluggable = [rehypeKatex, katexOptions];

/** tex drawn into el: a display block or inline; a formula KaTeX can't draw shows its source, marked. */
export function renderMath(el: HTMLElement, tex: string, display: boolean) {
  try {
    katex.render(tex, el, { ...katexOptions, displayMode: display, throwOnError: true });
    el.classList.remove("math-error");
    el.removeAttribute("title");
  } catch (e) {
    el.textContent = display ? tex : `$${tex}$`;
    el.classList.add("math-error");
    el.title = e instanceof Error ? e.message : String(e);
  }
}
