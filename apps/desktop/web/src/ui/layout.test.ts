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
import { panelWidth } from "./layout";

describe("panelWidth", () => {
  it.each([
    { name: "the saved width", saved: 500, fallback: 420, min: 340, max: 800, want: 500 },
    { name: "the fallback when unset", saved: 0, fallback: 420, min: 340, max: 800, want: 420 },
    { name: "a wide display's width on a narrow one", saved: 1400, fallback: 420, min: 340, max: 700, want: 700 },
    { name: "never below the minimum", saved: 100, fallback: 420, min: 340, max: 800, want: 340 },
    { name: "a window too small for the maximum", saved: 500, fallback: 420, min: 340, max: 200, want: 340 },
  ])("$name", ({ saved, fallback, min, max, want }) => expect(panelWidth(saved, fallback, min, max)).toBe(want));
});
