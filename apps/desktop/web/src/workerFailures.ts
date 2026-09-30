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

// Worker runs that failed or hit a limit since the workspace's Workers view
// was last shown (BL-WK-11): the workspace dropdown and the Workers button
// badge them. One poll serves every open workspace.

import { timestampDate } from "@bufbuild/protobuf/wkt";
import { useSyncExternalStore } from "react";
import { workers } from "./api";
import { RunStatus, type Worker } from "./gen/blitz/v1/worker_pb";

/** How often the open workspaces' workers are asked for. */
const pollMs = 60_000;

/** How many workers' latest runs failed or hit a limit after seen (ms). */
export function unseenFailures(list: Pick<Worker, "lastRun">[], seen: number): number {
  return list.filter((w) => {
    const r = w.lastRun;
    if (!r || (r.status !== RunStatus.FAILED && r.status !== RunStatus.LIMITED) || !r.started) return false;
    return timestampDate(r.started).getTime() > seen;
  }).length;
}

type Dirs = Record<string, number>; // dir -> when its Workers view was last shown
let wanted: Dirs = {};
let counts: Record<string, number> = {};
const listeners = new Set<() => void>();
let timer: ReturnType<typeof setInterval> | undefined;

async function poll() {
  const next: Record<string, number> = {};
  for (const [dir, seen] of Object.entries(wanted)) {
    try {
      next[dir] = unseenFailures((await workers.listWorkers({ workspace: dir })).workers, seen);
    } catch {
      next[dir] = 0; // workers off, or the service away: nothing to show
    }
  }
  counts = next;
  for (const f of listeners) f();
}

/** Sets which workspaces to watch, with when each last showed its workers, and asks now. */
export function watchWorkerFailures(dirs: Dirs) {
  const same = Object.keys(dirs).length === Object.keys(wanted).length && Object.entries(dirs).every(([d, s]) => wanted[d] === s);
  wanted = dirs;
  if (!same) void poll();
}

function subscribe(f: () => void) {
  listeners.add(f);
  if (listeners.size === 1) timer = setInterval(poll, pollMs);
  return () => {
    listeners.delete(f);
    if (listeners.size === 0) clearInterval(timer);
  };
}

/** Each watched workspace's count of unseen failed runs. */
export function useWorkerFailures(): Record<string, number> {
  return useSyncExternalStore(subscribe, () => counts);
}
