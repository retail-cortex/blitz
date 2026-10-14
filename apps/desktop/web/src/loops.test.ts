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
import { formatInterval, parseInterval } from "./loops";

describe("intervals", () => {
  it.each([
    ["10", 600_000],
    ["10m", 600_000],
    ["1h30m", 5_400_000],
    ["90s", 90_000],
    ["30s", undefined], // under a minute
    ["0", undefined],
    ["soon", undefined],
    ["", undefined],
  ])("%s is %s ms", (s, ms) => {
    expect(parseInterval(s)).toBe(ms);
  });
  it.each([
    [600_000, "10m"],
    [5_400_000, "1h30m"],
    [90_000, "1m30s"],
    [0, "0s"],
  ])("%i ms is written %s", (ms, s) => {
    expect(formatInterval(ms)).toBe(s);
  });
});
