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
import { agencies, modes, withUnknown } from "./options";

describe("withUnknown", () => {
  it("leaves a menu as it is for a value it has, or none", () => {
    expect(withUnknown(agencies(), "high")).toEqual(agencies());
    expect(withUnknown(agencies(), "")).toEqual(agencies());
  });

  it("adds a value read from a file that isn't one of them, marked, so it can be put right", () => {
    // As a model wrote during setup: the agent didn't load, and Studio couldn't open it.
    const got = withUnknown(agencies(), "autonomous");
    expect(got.map((o) => o.value)).toEqual(["low", "medium", "high", "extreme", "autonomous"]);
    expect(got.at(-1)).toEqual({ value: "autonomous", label: "autonomous (not one of these)", detail: "autonomous isn't a value this takes: choose another, then save." });
    expect(withUnknown(modes(), "yolo").at(-1)?.label).toBe("yolo (not one of these)");
  });
});
