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

// Words marked in a document's preview: a search hit's (spec_search_035)
// and Find in file's (spec_files_029). The CSS Custom Highlight API marks
// them, which leaves the rendered document as it is.

import { blockAt, matchSpans } from "../search";

/** A set of marks: the highlights' names, styled in app.css. */
export interface Marks {
  all: string;
  current: string;
}

/** A search hit's marks. */
export const hitMarks: Marks = { all: "search-found", current: "search-found-current" };
/** Find in file's marks. */
export const findMarks: Marks = { all: "file-found", current: "file-found-current" };

/** Where terms are in box's text, in order (toWordEnd: see matchSpans). */
export function findRanges(box: HTMLElement, terms: string[], toWordEnd: boolean): Range[] {
  const ranges: Range[] = [];
  const walker = document.createTreeWalker(box, NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    for (const [start, end] of matchSpans(n.nodeValue ?? "", terms, toWordEnd)) {
      const r = document.createRange();
      r.setStart(n, start);
      r.setEnd(n, end);
      ranges.push(r);
    }
  }
  return ranges;
}

/** Marks ranges, here the strongest, and scrolls to it. */
export function paint(marks: Marks, ranges: Range[], here: Range | undefined, fallback?: HTMLElement) {
  (here?.startContainer.parentElement ?? fallback)?.scrollIntoView({ block: "center" });
  if (typeof Highlight === "undefined" || !CSS.highlights) return;
  CSS.highlights.set(marks.all, new Highlight(...ranges.filter((r) => r !== here)));
  if (here) CSS.highlights.set(marks.current, new Highlight(here));
  else CSS.highlights.delete(marks.current);
}

/** Takes the marks away. */
export function unpaint(marks: Marks) {
  if (typeof Highlight === "undefined" || !CSS.highlights) return;
  CSS.highlights.delete(marks.all);
  CSS.highlights.delete(marks.current);
}

/**
 * Marks terms in box's text and scrolls to the first in the block that
 * holds the source line (its elements carry data-line), or after it.
 * Returns what takes the marks away.
 */
export function showFound(box: HTMLElement, line: number, terms: string[]): () => void {
  const blocks = [...box.querySelectorAll<HTMLElement>("[data-line]")];
  const at = blockAt(
    blocks.map((b) => Number(b.dataset.line)),
    line,
  );
  const block = at >= 0 ? blocks[at] : box;
  const ranges = findRanges(box, terms, true);
  const here =
    ranges.find((r) => block.contains(r.startContainer)) ?? ranges.find((r) => block.compareDocumentPosition(r.startContainer) & Node.DOCUMENT_POSITION_FOLLOWING);
  paint(hitMarks, ranges, here, block);
  return () => unpaint(hitMarks);
}
