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
import { pause } from "./pause";

describe("pause", () => {
  afterEach(() => vi.restoreAllMocks());

  it.each([
    { name: "the time passing", abort: false },
    { name: "an abort", abort: true },
  ])("ends with $name and leaves no listener", async ({ abort }) => {
    const ctl = new AbortController();
    const add = vi.spyOn(ctl.signal, "addEventListener");
    const remove = vi.spyOn(ctl.signal, "removeEventListener");
    const waited = pause(abort ? 60_000 : 1, ctl.signal);
    if (abort) ctl.abort();
    await waited;
    expect(add).toHaveBeenCalledTimes(1);
    expect(remove).toHaveBeenCalledWith("abort", add.mock.calls[0][1]);
  });

  it("ends at once when already aborted", async () => {
    const ctl = new AbortController();
    ctl.abort();
    await expect(pause(60_000, ctl.signal)).resolves.toBeUndefined();
  });
});
