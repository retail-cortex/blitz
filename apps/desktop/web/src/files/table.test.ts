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
import { activeFilters, nextSort, offsetFor, pageRange, rowOfLine } from "./table";

describe("table viewer rules", () => {
  it("cycles a column's sort: ascending, descending, off; another column starts ascending", () => {
    expect(nextSort(null, 2)).toEqual({ column: 2, desc: false });
    expect(nextSort({ column: 2, desc: false }, 2)).toEqual({ column: 2, desc: true });
    expect(nextSort({ column: 2, desc: true }, 2)).toBeNull();
    expect(nextSort({ column: 2, desc: true }, 0)).toEqual({ column: 0, desc: false });
  });
  it("opens a search hit's line at the page that holds its row", () => {
    expect(rowOfLine(undefined)).toBeUndefined();
    expect(rowOfLine(1)).toBeUndefined();
    expect(rowOfLine(2)).toBe(1);
    expect(offsetFor(1)).toBe(0);
    expect(offsetFor(100)).toBe(0);
    expect(offsetFor(101)).toBe(100);
    expect(offsetFor(0)).toBe(0);
  });
  it("names a page's range and keeps only the filters that say something", () => {
    expect(pageRange(100, 100)).toBe("101–200");
    expect(pageRange(0, 0)).toBe("0");
    expect(activeFilters({ 0: " >5 ", 2: "  ", 3: "north" })).toEqual([
      { column: 0, expr: ">5" },
      { column: 3, expr: "north" },
    ]);
  });
});
