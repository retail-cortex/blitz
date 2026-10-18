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

// Workflow runs going on, in every workspace (spec_workflows_043 WF-19):
// one stream (WatchWorkflowRuns) serves the status bar, Studio's canvas
// and the alerts, wherever the run was started (the canvas, a chat, a
// worker), so a run goes on with Studio closed and its end is heard of
// from anywhere.

import { useSyncExternalStore } from "react";
import { workspaces } from "./api";
import type { WorkflowRun, WorkflowRunStep } from "./gen/blitz/v1/workspace_pb";

/** How often the runs are asked for when the service can't stream them, or the stream breaks. */
const pollMs = 10_000;

/** What the store holds: the runs going, waiting and lately ended, newest first. */
export interface RunsState {
  runs: WorkflowRun[];
  loaded: boolean;
}

let state: RunsState = { runs: [], loaded: false };
const listeners = new Set<() => void>();
const endings = new Set<(run: WorkflowRun) => void>();
let timer: ReturnType<typeof setTimeout> | undefined;
// When the store started listening: a run seen ended for the first time
// that ended after this ended while the app was open.
let since = 0;

/** Whether a run hasn't ended (it runs, or waits on a person or a timer). */
export const isGoing = (r: Pick<WorkflowRun, "status">) => r.status === "running" || r.status === "waiting";

/**
 * The runs in next that ended since prev: those prev had going, and those
 * new to the list that ended after after (a short run that started and
 * ended between two lists).
 */
export function ended(prev: Pick<WorkflowRun, "id" | "status">[], next: WorkflowRun[], after: number): WorkflowRun[] {
  const before = new Map(prev.map((r) => [r.id, r.status]));
  return next.filter((r) => {
    if (isGoing(r)) return false;
    const was = before.get(r.id);
    if (was === undefined) return (r.ended ? Number(r.ended.seconds) * 1000 : 0) > after;
    return was === "running" || was === "waiting";
  });
}

function set(next: Partial<RunsState>) {
  state = { ...state, ...next };
  for (const f of listeners) f();
}

function take(runs: WorkflowRun[]) {
  const done = state.loaded ? ended(state.runs, runs, since) : [];
  set({ runs, loaded: true });
  for (const r of done) for (const f of endings) f(r);
}

/** Asks for the runs now. */
export async function refreshRuns() {
  try {
    const cutoff = Date.now() - 10 * 60_000;
    const r = await workspaces.listWorkflowRuns({ workspace: "", limit: 20 });
    take(r.runs.filter((x) => isGoing(x) || (x.ended && Number(x.ended.seconds) * 1000 > cutoff)));
  } catch {
    // the service's state shows elsewhere
  }
}

let watching: AbortController | undefined;

// Follows the stream; an older service without it, or a broken stream, is
// asked every pollMs instead until the stream comes back.
async function watch(ctl: AbortController) {
  while (!ctl.signal.aborted) {
    try {
      for await (const r of workspaces.watchWorkflowRuns({ workspace: "" }, { signal: ctl.signal })) take(r.runs);
    } catch {
      // unimplemented, or dropped
    }
    if (ctl.signal.aborted) return;
    await refreshRuns();
    await new Promise((res) => (timer = setTimeout(res, pollMs)));
  }
}

function subscribe(f: () => void) {
  listeners.add(f);
  if (listeners.size === 1) {
    since = Date.now();
    watching = new AbortController();
    void watch(watching);
  }
  return () => {
    listeners.delete(f);
    if (listeners.size === 0) {
      watching?.abort();
      clearTimeout(timer);
    }
  };
}

/** The runs going and lately ended, kept up to date while in use. */
export function useWorkflowRuns(): RunsState {
  return useSyncExternalStore(subscribe, () => state);
}

/** Calls f with each run that ends (done, failed or stopped) while the store is in use; returns what stops that. */
export function onRunEnded(f: (run: WorkflowRun) => void): () => void {
  endings.add(f);
  return () => endings.delete(f);
}

/** The runs a canvas shows now: their end shows there, so the app doesn't announce it too. */
export const watchedRuns = new Set<string>();

// Failed runs the user has looked at (opened, or seen on the canvas): the
// status bar stops showing them.
const seen = new Set<string>();

/** Marks a run looked at: a failure of it leaves the status bar. */
export function seeRun(id: string) {
  if (seen.has(id)) return;
  seen.add(id);
  set({});
}

/** The failed runs not looked at yet. */
export const unseenFailures = (runs: WorkflowRun[]) => runs.filter((r) => r.status === "failed" && !seen.has(r.id));

/** A workflow's run going in a workspace (the newest), if one is. */
export function goingRun(runs: WorkflowRun[], workspace: string, workflow: string): WorkflowRun | undefined {
  return runs.find((r) => r.workspace === workspace && r.workflow === workflow && isGoing(r));
}

/** Each step's latest run in a receipt, by step ID (a step a loop ran again: its latest). */
export function latestSteps(steps: WorkflowRunStep[]): Record<string, WorkflowRunStep> {
  const out: Record<string, WorkflowRunStep> = {};
  for (const s of steps) out[s.id] = s;
  return out;
}

/** How far a run has got: the steps that ended (done, skipped) of those it has started. */
export function progress(run: Pick<WorkflowRun, "steps">): { done: number; started: number } {
  const latest = Object.values(latestSteps(run.steps));
  return { done: latest.filter((s) => s.status === "done" || s.status === "skipped").length, started: latest.length };
}

/** A step that failed in a run (its latest), if one did. */
export const failedStep = (run: Pick<WorkflowRun, "steps">) => [...run.steps].reverse().find((s) => s.status === "failed");

/** A timestamp's milliseconds (0 when unset). */
export const ms = (t: { seconds: bigint; nanos: number } | undefined) => (t ? Number(t.seconds) * 1000 + Math.floor(t.nanos / 1e6) : 0);

/** How long something took (or has taken, ending now), as 1h 5m, 3m 20s, 12s or 0.4s. */
export function took(start: number, end: number): string {
  if (!start) return "";
  const s = Math.max(0, (end - start) / 1000);
  if (s >= 3600) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  if (s >= 60) return `${Math.floor(s / 60)}m ${Math.round(s % 60)}s`;
  if (s >= 10) return `${Math.round(s)}s`;
  return `${s.toFixed(1)}s`;
}

/** A cost in US dollars, to the cent above one, finer below; "—" for none. */
export function cost(usd: number | undefined, priced = true): string {
  if (!usd) return priced ? "$0" : "—";
  const v = usd >= 1 ? usd.toFixed(2) : usd >= 0.01 ? usd.toFixed(3) : usd.toFixed(4);
  return `$${v}${priced ? "" : "+"}`;
}

/** A token count, short: 950, 12.3k, 1.2M. */
export function tokens(n: bigint | number | undefined): string {
  const v = Number(n ?? 0);
  if (v >= 1e6) return `${(v / 1e6).toFixed(1)}M`;
  if (v >= 1e4) return `${(v / 1e3).toFixed(1)}k`;
  return v.toLocaleString();
}
