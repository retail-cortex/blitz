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

import { createRouterTransport } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import { setTransport } from "./api";
import { type HumanTask, WorkspaceService } from "./gen/blitz/v1/workspace_pb";
import { alertsOf, arrived, onArrived, subscribeTasks, tasksNow } from "./humanTasks";

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

describe("the tasks stream", () => {
  it("skips the page server's keepalives: the list stays and nothing re-announces", async () => {
    const waiting = { id: "t1", kind: "human_task", workspace: "/w" };
    // The stream ends after its last message; the store then asks for
    // the list, so by then it has read every message.
    let end!: () => void;
    const ended = new Promise<void>((r) => (end = r));
    setTransport(
      createRouterTransport(({ service }) => {
        service(WorkspaceService, {
          async *watchHumanTasks() {
            yield { tasks: [waiting], alerts: "open", listed: true };
            yield {}; // a keepalive (pkg/pageserver)
            yield { tasks: [waiting, { id: "t2", kind: "human_task", workspace: "/w" }], alerts: "open", listed: true };
            yield {};
          },
          listHumanTasks: () => {
            end();
            throw new Error("not now");
          },
        });
      }),
    );
    const heard: string[][] = [];
    const stopArrived = onArrived((ts) => heard.push(ts.map((t) => t.id)));
    const stop = subscribeTasks(() => {});
    try {
      await ended;
      expect(tasksNow().tasks.map((t) => t.id)).toEqual(["t1", "t2"]);
      expect(tasksNow().alerts).toBe("open");
      expect(heard).toEqual([["t2"]]);
    } finally {
      stop();
      stopArrived();
    }
  });
});
