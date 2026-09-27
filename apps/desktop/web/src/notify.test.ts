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
import { finishedAfterMs, shouldNotify } from "./notify";

describe("shouldNotify", () => {
  const base = { enabled: true, focused: false, shown: true, elapsedMs: finishedAfterMs };
  it("notifies only when the user is looking elsewhere", () => {
    expect(shouldNotify("waiting", base)).toBe(true);
    expect(shouldNotify("waiting", { ...base, focused: true })).toBe(false);
    expect(shouldNotify("waiting", { ...base, focused: true, shown: false })).toBe(true); // another workspace
    expect(shouldNotify("waiting", { ...base, enabled: false })).toBe(false);
  });
  it("skips quick turns, never a wait", () => {
    expect(shouldNotify("finished", { ...base, elapsedMs: 2000 })).toBe(false);
    expect(shouldNotify("finished", base)).toBe(true);
    expect(shouldNotify("waiting", { ...base, elapsedMs: 0 })).toBe(true);
  });
});
