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

import { useEffect, useState } from "react";
import type { Task } from "./gen/blitz/v1/turn_pb";

/** When an item of the agent's task list started, and once done, ended. */
export type TaskTime = { start: number; end?: number };

/**
 * The task list's times, by item, after the list changed at now: an item
 * starts when it's first seen in progress and ends when it's first seen
 * done. The list only arrives live, during a turn, so these are when it
 * happened (to within an event). Items no longer listed are dropped.
 */
export function taskTimes(prev: ReadonlyMap<string, TaskTime>, tasks: Task[], now: number): Map<string, TaskTime> {
  const next = new Map<string, TaskTime>();
  for (const task of tasks) {
    const was = prev.get(task.content);
    if (task.status === "in_progress") next.set(task.content, { start: was?.start ?? now });
    else if (task.status === "done" && was) next.set(task.content, { start: was.start, end: was.end ?? now });
  }
  return next;
}

/** The time now, every second while active (a clock that ticks). */
export function useNow(active: boolean): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [active]);
  return now;
}
