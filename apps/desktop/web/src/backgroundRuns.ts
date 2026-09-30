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

// The service's background runs (blitz --bg), in every workspace: the
// inbox (spec_parity_027 PAR-PAR-20). One poll serves every inbox button.

import { useSyncExternalStore } from "react";
import { sessions } from "./api";
import type { BackgroundRun } from "./gen/blitz/v1/session_pb";

/** How often the runs are asked for while something shows them. */
const pollMs = 4000;

let runs: BackgroundRun[] = [];
const listeners = new Set<() => void>();
let timer: ReturnType<typeof setInterval> | undefined;

/** Asks for the runs now (after starting or stopping one, say). */
export async function refreshRuns() {
  try {
    runs = (await sessions.listBackground({})).runs;
  } catch {
    return; // the service's state shows elsewhere
  }
  for (const f of listeners) f();
}

function subscribe(f: () => void) {
  listeners.add(f);
  if (listeners.size === 1) {
    refreshRuns();
    timer = setInterval(refreshRuns, pollMs);
  }
  return () => {
    listeners.delete(f);
    if (listeners.size === 0) clearInterval(timer);
  };
}

/** The background runs, newest first, kept up to date while in use. */
export function useRuns(): BackgroundRun[] {
  return useSyncExternalStore(subscribe, () => runs);
}

/** Whether a run hasn't ended. */
export function isActive(r: Pick<BackgroundRun, "state">): boolean {
  return r.state === "running" || r.state === "waiting";
}

/** How many runs go on, and how many of those wait for an answer. */
export function inboxCounts(list: Pick<BackgroundRun, "state">[]): { active: number; waiting: number } {
  const active = list.filter(isActive);
  return { active: active.length, waiting: active.filter((r) => r.state === "waiting").length };
}

/** The active run in a session, if any. */
export function runInSession(list: BackgroundRun[], sessionId: string): BackgroundRun | undefined {
  return list.find((r) => r.sessionId === sessionId && isActive(r));
}

/** How long a run has gone on (or went on), as 1h 5m, 3m 20s or 12s. */
export function runAge(started: Date, ended: Date | undefined, now: Date): string {
  const s = Math.max(0, Math.round(((ended ?? now).getTime() - started.getTime()) / 1000));
  if (s >= 3600) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  if (s >= 60) return `${Math.floor(s / 60)}m ${s % 60}s`;
  return `${s}s`;
}
