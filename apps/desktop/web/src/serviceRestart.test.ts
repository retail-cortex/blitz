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

import { afterEach, describe, expect, it, vi } from "vitest";
import { markRestarting, restartDone, restarting, restartWait } from "./serviceRestart";

describe("the service restarting at the app's request (DSK-51d)", () => {
  afterEach(() => restartDone());

  it.each([
    { name: "nothing asked", mark: undefined, after: 0, want: "" },
    { name: "a restart, just asked", mark: "restart" as const, after: 0, want: "restart" },
    { name: "a start, just asked", mark: "start" as const, after: 1_000, want: "start" },
    { name: "a restart that never came", mark: "restart" as const, after: restartWait + 1, want: "" },
  ])("$name", ({ mark, after, want }) => {
    const now = Date.now();
    vi.spyOn(Date, "now").mockReturnValue(now);
    if (mark) markRestarting(mark);
    expect(restarting(now + after)).toBe(want);
    vi.restoreAllMocks();
  });

  it("ends once the service is up", () => {
    markRestarting();
    expect(restarting()).toBe("restart");
    restartDone();
    expect(restarting()).toBe("");
  });
});
