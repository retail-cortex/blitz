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
import { logTime } from "./logs";

describe("logTime", () => {
  it.each([
    [new Date(2026, 8, 28, 9, 5, 3, 7), "09:05:03.007"],
    [new Date(2026, 8, 28, 23, 59, 59, 999), "23:59:59.999"],
  ])("%s", (d, want) => {
    expect(logTime(d)).toBe(want);
  });
});
