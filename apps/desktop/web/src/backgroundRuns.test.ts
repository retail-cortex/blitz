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
import { inboxCounts, isActive, runAge, runInSession } from "./backgroundRuns";
import type { BackgroundRun } from "./gen/blitz/v1/session_pb";

const run = (id: string, sessionId: string, state: string) => ({ id, sessionId, state }) as BackgroundRun;

describe("inbox", () => {
  it("counts the runs going on and those waiting", () => {
    expect(inboxCounts([run("bg-1", "a", "running"), run("bg-2", "b", "waiting"), run("bg-3", "c", "done"), run("bg-4", "d", "failed")])).toEqual({ active: 2, waiting: 1 });
    expect(inboxCounts([])).toEqual({ active: 0, waiting: 0 });
  });
  it.each([
    ["running", true],
    ["waiting", true],
    ["done", false],
    ["stopped", false],
    ["failed", false],
  ])("%s is active: %s", (state, want) => expect(isActive({ state })).toBe(want));
  it("finds a session's active run", () => {
    const list = [run("bg-2", "s", "done"), run("bg-1", "s", "waiting")];
    expect(runInSession(list, "s")?.id).toBe("bg-1");
    expect(runInSession(list, "other")).toBeUndefined();
  });
  it.each([
    [12, "12s"],
    [200, "3m 20s"],
    [3900, "1h 5m"],
  ])("%ss is %s", (secs, want) => {
    const start = new Date(0);
    expect(runAge(start, undefined, new Date(secs * 1000))).toBe(want);
    expect(runAge(start, new Date(secs * 1000), new Date(99999999))).toBe(want);
  });
});
