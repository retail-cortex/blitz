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
import { sliderCommit } from "./workspaceSettings";

describe("sliderCommit", () => {
  it.each([
    ["a Tab or click at Default saves nothing", false, 0, undefined, undefined],
    ["a blur with no move saves nothing", false, 0.7, 0.7, undefined],
    ["moved back to the set value saves nothing", true, 0.7, 0.7, undefined],
    ["moved from Default saves the value", true, 0, undefined, "0"],
    ["moved to another value saves it", true, 1.2, 0.7, "1.2"],
  ] as const)("%s", (_, moved, shown, value, want) => {
    expect(sliderCommit(moved, shown, value)).toBe(want);
  });
});
