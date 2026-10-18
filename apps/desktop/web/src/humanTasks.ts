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

// Workflows' human tasks waiting, in every workspace (spec_workflows_043
// WF-16, WF-18): one poll serves the status bar, the tasks dialog and the
// tasks window; new ones are announced as the user's [ui] task_alerts says.

import { useSyncExternalStore } from "react";
import { workspaces } from "./api";
import type { HumanTask } from "./gen/blitz/v1/workspace_pb";

/** How often the tasks are asked for while something shows them. */
const pollMs = 10_000;

/** How new tasks reach the user: a notification, the tasks at once, or nothing. */
export type TaskAlerts = "notify" | "open" | "off";

/** What the store holds: the tasks waiting, the setting, and whether the dialog shows. */
export interface TasksState {
  tasks: HumanTask[];
  alerts: TaskAlerts;
  shown: boolean;
  loaded: boolean;
}

let state: TasksState = { tasks: [], alerts: "notify", shown: false, loaded: false };
const listeners = new Set<() => void>();
const arrivals = new Set<(tasks: HumanTask[]) => void>();
let timer: ReturnType<typeof setInterval> | undefined;

/** The setting as the service says it, notify for anything else. */
export const alertsOf = (s: string): TaskAlerts => (s === "open" || s === "off" ? s : "notify");

/** The tasks in next that weren't in prev (by ID). */
export function arrived(prev: Pick<HumanTask, "id">[], next: HumanTask[]): HumanTask[] {
  const had = new Set(prev.map((t) => t.id));
  return next.filter((t) => !had.has(t.id));
}

function set(next: Partial<TasksState>) {
  state = { ...state, ...next };
  for (const f of listeners) f();
}

/** Asks for the tasks now (after answering one, say). */
export async function refreshTasks() {
  let r;
  try {
    r = await workspaces.listHumanTasks({ workspace: "" });
  } catch {
    return; // the service's state shows elsewhere
  }
  const tasks = r.tasks.filter((t) => t.kind === "human_task");
  const fresh = state.loaded ? arrived(state.tasks, tasks) : [];
  set({ tasks, alerts: alertsOf(r.alerts), loaded: true, shown: state.shown && tasks.length > 0 });
  if (fresh.length) for (const f of arrivals) f(fresh);
}

function subscribe(f: () => void) {
  listeners.add(f);
  if (listeners.size === 1) {
    void refreshTasks();
    timer = setInterval(refreshTasks, pollMs);
  }
  return () => {
    listeners.delete(f);
    if (listeners.size === 0) clearInterval(timer);
  };
}

/** The tasks, oldest first, and the dialog's state, kept up to date while in use. */
export function useTasks(): TasksState {
  return useSyncExternalStore(subscribe, () => state);
}

/** Calls f with tasks that have arrived since the last poll; returns what stops that. */
export function onArrived(f: (tasks: HumanTask[]) => void): () => void {
  arrivals.add(f);
  return () => arrivals.delete(f);
}

/** Shows the tasks dialog (the status bar's button, a notification, the tray). */
export const showTasks = () => set({ shown: true });
/** Hides the tasks dialog (Later). */
export const hideTasks = () => set({ shown: false });
/** Forgets a task answered, until the next poll. */
export const dropTask = (id: string) => set({ tasks: state.tasks.filter((t) => t.id !== id), shown: state.shown && state.tasks.length > 1 });
