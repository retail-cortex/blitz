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
// The table viewer's rules, apart from its view (spec_files_029): sorting,
// paging and a row to open at.

/** How a table is sorted: by a column (0-based), ascending or not. */
export interface Sort {
  column: number;
  desc: boolean;
}

/** A header's click: ascending, then descending, then file order. */
export function nextSort(current: Sort | null, column: number): Sort | null {
  if (!current || current.column !== column) return { column, desc: false };
  return current.desc ? null : { column, desc: true };
}

/** Rows a page holds. */
export const pageSize = 100;

/** The page's first match (0-based offset) that holds row (1-based) when nothing filters. */
export function offsetFor(row: number): number {
  return Math.max(0, Math.floor((row - 1) / pageSize) * pageSize);
}

/** The table row a file line is (line 1 is the header). */
export function rowOfLine(line?: number): number | undefined {
  return line && line > 1 ? line - 1 : undefined;
}

/** "101–200" for a page of n rows from offset. */
export function pageRange(offset: number, n: number): string {
  return n === 0 ? "0" : `${(offset + 1).toLocaleString()}–${(offset + n).toLocaleString()}`;
}

/** The filters that say something, as the API takes them. */
export function activeFilters(filters: Record<number, string>): { column: number; expr: string }[] {
  return Object.entries(filters)
    .filter(([, e]) => e.trim() !== "")
    .map(([c, e]) => ({ column: Number(c), expr: e.trim() }));
}
