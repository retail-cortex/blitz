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
import { sampleParams, tableRows, templated } from "./dataStep";

describe("a data step's Try it", () => {
  it("asks for the templates' samples and types what's typed", () => {
    expect(templated("{{ input }}")).toBe(true);
    expect(templated("42")).toBe(false);
    expect(sampleParams([["n", "3"], ["who", "Ada"], ["paid", "true"]])).toEqual({ n: 3, who: "Ada", paid: true });
  });

  it("shows a table's rows as text", () => {
    expect(tableRows('[[1, "Ada", null, {"a": 1}], [2.5, true]]')).toEqual([
      ["1", "Ada", "", '{"a":1}'],
      ["2.5", "true"],
    ]);
    expect(tableRows("")).toEqual([]);
    expect(tableRows("{")).toEqual([]);
    expect(tableRows('{"rows": 1}')).toEqual([]);
    expect(tableRows("[1]")).toEqual([[]]);
  });
});
