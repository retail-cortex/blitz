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
import { formatBytes } from "./bytes";

describe("formatBytes", () => {
  it.each([
    ["a few bytes are 1 KB", 100, "1 KB"],
    ["kilobytes, whole", 12_800, "13 KB"],
    ["megabytes, one decimal", 3 << 20, "3.0 MB"],
    ["a bigint", 5n << 20n, "5.0 MB"],
  ] as const)("%s", (_, n, want) => {
    expect(formatBytes(n)).toBe(want);
  });
});
