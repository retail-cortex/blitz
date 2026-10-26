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

// The agents' background processes still running, in every open
// workspace (spec_processes_052): one stream (WatchProcesses) serves the
// status bar's count and the Processes view, with what the view shows of
// each (its running time, usage and flags) and which to stop.

import { useSyncExternalStore } from "react";
import { workspaces } from "./api";
import type { WorkspaceProcess } from "./gen/blitz/v1/workspace_pb";
import { t, tn } from "./i18n";

/** How long to wait before following the stream again after it breaks (or an older service lacks it). */
const retryMs = 10_000;

/** How many of a process's last lines of output the view shows. */
export const outputLines = 40;

/** What the store holds: the processes running, and when the list came (to count their time from). */
export interface ProcessesState {
  list: WorkspaceProcess[];
  receivedAt: number;
}

let state: ProcessesState = { list: [], receivedAt: 0 };
const listeners = new Set<() => void>();
let watching: AbortController | undefined;
let timer: ReturnType<typeof setTimeout> | undefined;

function take(list: WorkspaceProcess[]) {
  state = { list, receivedAt: Date.now() };
  for (const f of listeners) f();
}

/**
 * Follows the service's process stream while something shows the
 * processes: it sends the list at once, then again on each change. A
 * broken stream, or an older service without it, is tried again after
 * retryMs; nothing is polled.
 */
async function watch(ctl: AbortController) {
  while (!ctl.signal.aborted) {
    try {
      for await (const r of workspaces.watchProcesses({ workspace: "" }, { signal: ctl.signal })) {
        // The page server's keepalives are empty messages: not a list.
        if (r.listed) take(r.processes);
      }
    } catch {
      // An older service (unimplemented) or a dropped stream.
    }
    if (ctl.signal.aborted) return;
    // Ends with the abort too, so a loop waiting to retry doesn't hang.
    await new Promise<void>((res) => {
      timer = setTimeout(res, retryMs);
      ctl.signal.addEventListener("abort", () => res(), { once: true });
    });
  }
}

function subscribe(f: () => void) {
  listeners.add(f);
  if (listeners.size === 1) {
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

/** The processes running in every open workspace, kept up to date while in use. */
export function useProcesses(): ProcessesState {
  return useSyncExternalStore(subscribe, () => state);
}

/** Stops a process, whichever session started it (the user's own stop). */
export async function stopProcess(p: WorkspaceProcess) {
  await workspaces.killProcess({ workspace: p.workspace, id: p.process?.id ?? 0, anySession: true });
}

/** A process's last lines of output, as captured. */
export async function processOutput(p: WorkspaceProcess): Promise<string> {
  const r = await workspaces.getProcessOutput({ workspace: p.workspace, id: p.process?.id ?? 0, anySession: true });
  return lastLines(r.output, outputLines);
}

/** A process's key in a list: its workspace and its number there. */
export const processKey = (p: WorkspaceProcess) => `${p.workspace}#${p.process?.id ?? 0}`;

/** The processes by workspace, as the menu and the drawer list them: dir's first, then the others by folder. */
export function byWorkspace(list: WorkspaceProcess[], dir: string): { workspace: string; processes: WorkspaceProcess[] }[] {
  const groups = new Map<string, WorkspaceProcess[]>();
  for (const p of list) groups.set(p.workspace, [...(groups.get(p.workspace) ?? []), p]);
  return [...groups.keys()]
    .sort((a, b) => (a === dir ? -1 : b === dir ? 1 : a.localeCompare(b)))
    .map((workspace) => ({ workspace, processes: groups.get(workspace) ?? [] }));
}

/** A process on one line, under its workspace: 42m 0s · CPU 97% · 411.0 MB · Busy (its flags unless withFlags is false). */
export function processDetail(p: WorkspaceProcess, receivedAt: number, now: number, withFlags = true): string {
  const proc = p.process;
  if (!proc) return "";
  const parts = [duration(runningFor(proc.runtimeMs, receivedAt, now))];
  if (proc.measured) parts.push(t("desktop.processes.cpu", { cpu: cpu(proc.cpuPercent) }), memory(proc.memoryBytes));
  if (withFlags) parts.push(...proc.flags.map((f) => flagLabel(f, proc.idleMs)));
  return parts.join(" · ");
}

/** Whether a process looks bad (busy, memory, left running). */
export const isFlagged = (p: WorkspaceProcess) => (p.process?.flags.length ?? 0) > 0;

/** The processes Stop flagged stops: those with any flag. */
export const flaggedOf = (list: WorkspaceProcess[]) => list.filter(isFlagged);

/** How long a process has run now: what it had at receivedAt, plus the time since. */
export function runningFor(runtimeMs: bigint | number, receivedAt: number, now: number): number {
  return Number(runtimeMs) + Math.max(0, now - receivedAt);
}

/** A length of time, as 1h 5m, 3m 20s or 12s (as the inbox writes a run's age). */
export function duration(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s >= 86_400) return `${Math.floor(s / 86_400)}d ${Math.floor((s % 86_400) / 3600)}h`;
  if (s >= 3600) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  if (s >= 60) return `${Math.floor(s / 60)}m ${s % 60}s`;
  return `${s}s`;
}

/** Resident memory, as 512 KB, 12.3 MB or 1.5 GB. */
export function memory(bytes: bigint | number): string {
  let v = Number(bytes);
  for (const unit of ["B", "KB", "MB", "GB"]) {
    if (v < 1024 || unit === "GB") return `${unit === "B" ? v : v.toFixed(unit === "KB" ? 0 : 1)} ${unit}`;
    v /= 1024;
  }
  return `${v} B`;
}

/** Percent of one core, as 3% or 0.4% (more than 100 when it uses several). */
export const cpu = (percent: number) => `${percent > 0 && percent < 1 ? percent.toFixed(1) : Math.round(percent)}%`;

/** A flag's badge: Busy, Memory, or Left running (with how long its session has been idle). */
export function flagLabel(flag: string, idleMs: bigint | number): string {
  switch (flag) {
    case "cpu":
      return t("desktop.processes.flag.cpu");
    case "memory":
      return t("desktop.processes.flag.memory");
    case "left":
      return Number(idleMs) > 0 ? t("desktop.processes.flag.left_idle", { idle: duration(Number(idleMs)) }) : t("desktop.processes.flag.left");
    default:
      return flag;
  }
}

/** The status bar's label: how many processes run. */
export const countLabel = (n: number, flagged = 0) =>
  flagged > 0 ? tn("desktop.status.processes_flagged", n, { flagged }) : tn("desktop.status.processes", n);

/** The last n lines of text (without a final newline's empty line). */
export function lastLines(text: string, n: number): string {
  const lines = text.replace(/\n$/, "").split("\n");
  return lines.slice(-n).join("\n");
}
