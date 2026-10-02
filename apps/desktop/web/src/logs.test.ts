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
import { canDelete, logTime } from "./logs";

describe("logTime", () => {
  it.each([
    [new Date(2026, 8, 28, 9, 5, 3, 7), "09:05:03.007"],
    [new Date(2026, 8, 28, 23, 59, 59, 999), "23:59:59.999"],
  ])("%s", (d, want) => {
    expect(logTime(d)).toBe(want);
  });
});

describe("canDelete", () => {
  const now = new Date(2026, 8, 28, 0, 30);
  it.each([
    ["2026-09-27", true],
    ["2025-12-31", true],
    ["2026-09-28", false], // today's is being written
    ["2026-09-29", false],
    ["", false], // the newest
    ["27/09/2026", false],
  ])("%s", (day, want) => {
    expect(canDelete(day, now)).toBe(want);
  });
});
