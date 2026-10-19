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
import { actionMessage, type FormAction } from "./a2ui/model";
import { workspaces } from "./api";
import type { HumanTask, ListHumanTasksResponse, WatchHumanTasksResponse } from "./gen/blitz/v1/workspace_pb";

/** How often the tasks are asked for when the service can't stream them, or the stream breaks. */
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
let timer: ReturnType<typeof setTimeout> | undefined;

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

/** Takes a list the service sent: the tasks, the setting, and which have arrived. */
function take(r: ListHumanTasksResponse | WatchHumanTasksResponse) {
  const tasks = r.tasks.filter((t) => t.kind === "human_task");
  const fresh = state.loaded ? arrived(state.tasks, tasks) : [];
  set({ tasks, alerts: alertsOf(r.alerts), loaded: true, shown: state.shown && tasks.length > 0 });
  if (fresh.length) for (const f of arrivals) f(fresh);
}

/** Asks for the tasks now (after answering one, say). */
export async function refreshTasks() {
  try {
    take(await workspaces.listHumanTasks({ workspace: "" }));
  } catch {
    // the service's state shows elsewhere
  }
}

let watching: AbortController | undefined;

/**
 * Follows the service's task stream (WatchHumanTasks) while something
 * shows the tasks; an older service without it, or a broken stream, is
 * asked every pollMs instead until the stream comes back.
 */
async function watch(ctl: AbortController) {
  while (!ctl.signal.aborted) {
    try {
      for await (const r of workspaces.watchHumanTasks({ workspace: "" }, { signal: ctl.signal })) take(r);
    } catch {
      // An older service (unimplemented) or a dropped stream: ask now,
      // and try the stream again after a while.
    }
    if (ctl.signal.aborted) return;
    await refreshTasks();
    await new Promise((res) => (timer = setTimeout(res, pollMs)));
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

/**
 * Answers a human task with the button pressed (A2UI's action message, and
 * the data model then): the service checks it against the form and the
 * run goes on in the background; a refused answer is an error saying why.
 */
export async function answerTask(task: Pick<HumanTask, "id" | "workspace">, action: FormAction) {
  const r = await workspaces.answerHumanTask({ workspace: task.workspace, taskId: task.id, actionJson: JSON.stringify(actionMessage(task.id, action)), dataJson: JSON.stringify(action.data) });
  dropTask(task.id);
  return r;
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
