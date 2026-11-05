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
import { restartAfterFix, sandboxDetail } from "./SandboxNotice";

describe("sandboxDetail", () => {
  it("says what each state means", () => {
    expect(sandboxDetail(null)).toBe("Checking…");
    expect(sandboxDetail({ state: "ready", bwrap: "/usr/bin/bwrap" })).toContain("/usr/bin/bwrap");
    expect(sandboxDetail({ state: "no_bwrap" })).toContain("sudo apt install bubblewrap");
    expect(sandboxDetail({ state: "restricted", detail: "uid map" })).toContain("uid map");
    expect(sandboxDetail({ state: "restricted", detail: "uid map" })).toContain("Allow");
    expect(sandboxDetail({ state: "broken", detail: "seccomp" })).toContain('sandbox.shell = "auto"');
    expect(sandboxDetail({ state: "broken" })).toContain("()");
  });
});

// N26 desktop-1#19: a service not back in time was reported as fixed.
describe("restartAfterFix", () => {
  const ok = async () => {};
  it.each([
    { name: "back", restart: ok, settled: true, want: "" },
    { name: "not back in time", restart: ok, settled: false, want: "didn't start within 15 seconds" },
    {
      name: "restart failed",
      restart: async () => {
        throw new Error("no service");
      },
      settled: true,
      want: "no service",
    },
  ])("$name", async ({ restart, settled, want }) => {
    const got = await restartAfterFix(restart, async () => ({ settled }));
    if (want) expect(got).toContain(want);
    else expect(got).toBe("");
  });
});
