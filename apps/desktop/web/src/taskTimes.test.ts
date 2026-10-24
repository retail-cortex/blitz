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

import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { TaskSchema } from "./gen/blitz/v1/turn_pb";
import { taskTimes } from "./taskTimes";

const task = (content: string, status: string) => create(TaskSchema, { content, status });

describe("taskTimes", () => {
  it("starts an item in progress, keeps its start, and ends it once done", () => {
    let times = taskTimes(new Map(), [task("a", "in_progress"), task("b", "pending")], 1000);
    expect([...times]).toEqual([["a", { start: 1000 }]]);
    times = taskTimes(times, [task("a", "in_progress"), task("b", "pending")], 5000);
    expect(times.get("a")).toEqual({ start: 1000 });
    times = taskTimes(times, [task("a", "done"), task("b", "in_progress")], 9000);
    expect(times.get("a")).toEqual({ start: 1000, end: 9000 });
    expect(times.get("b")).toEqual({ start: 9000 });
    times = taskTimes(times, [task("a", "done"), task("b", "done")], 12000);
    expect(times.get("a")).toEqual({ start: 1000, end: 9000 });
    expect(times.get("b")).toEqual({ start: 9000, end: 12000 });
  });
  it.each([
    ["done without being seen in progress: no time", [task("a", "done")]],
    ["dropped from the list", [task("b", "pending")]],
  ])("%s", (_, tasks) => {
    expect(taskTimes(new Map([["x", { start: 1 }]]), tasks, 2).size).toBe(0);
  });
});
