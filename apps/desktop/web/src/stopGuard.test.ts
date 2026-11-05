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

import { describe, expect, it, vi } from "vitest";

const listRunningWorkerRuns = vi.fn();
vi.mock("./api", () => ({ workers: { listRunningWorkerRuns: () => listRunningWorkerRuns() } }));

const { runningJobs } = await import("./stopGuard");

// Stopping the service stops the jobs' runs too, so the guard asks about
// them as it does about workflows' (N26 desktop-4#35).
describe("the jobs a stop would stop", () => {
  it.each([
    { name: "each job once", runs: [{ worker: "nightly" }, { worker: "deps" }, { worker: "nightly" }], want: ["nightly", "deps"] },
    { name: "none going", runs: [], want: [] },
  ])("$name", async ({ runs, want }) => {
    listRunningWorkerRuns.mockResolvedValueOnce({ runs });
    expect(await runningJobs()).toEqual(want);
  });
  it("none when the service can't say", async () => {
    listRunningWorkerRuns.mockRejectedValueOnce(new Error("unreachable"));
    expect(await runningJobs()).toEqual([]);
  });
});
