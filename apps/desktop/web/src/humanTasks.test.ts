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
import type { HumanTask } from "./gen/blitz/v1/workspace_pb";
import { alertsOf, arrived } from "./humanTasks";

const task = (id: string) => ({ id }) as HumanTask;

describe("humanTasks", () => {
  it.each([
    ["notify", "notify"],
    ["open", "open"],
    ["off", "off"],
    ["", "notify"],
    ["loud", "notify"],
  ])("reads the setting %j as %s", (s, want) => expect(alertsOf(s)).toBe(want));

  it.each([
    { name: "none before", prev: [], next: ["a"], want: ["a"] },
    { name: "one new", prev: ["a"], next: ["a", "b"], want: ["b"] },
    { name: "one answered", prev: ["a", "b"], next: ["b"], want: [] },
  ])("finds the tasks arrived: $name", ({ prev, next, want }) => expect(arrived(prev.map(task), next.map(task)).map((t) => t.id)).toEqual(want));
});
