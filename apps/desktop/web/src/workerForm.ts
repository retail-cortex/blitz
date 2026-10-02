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

// The worker form: its fields, the CreateWorker and UpdateWorker fields
// they make, and the form for a worker as written (GetWorkerSpec).
import { WorkerState } from "./gen/blitz/v1/worker_pb";
import { checkModelRef, emptyCatalog } from "./models";

/** The form's fields, as typed. */
export interface WorkerForm {
  name: string;
  description: string;
  schedule: string;
  timezone: string;
  agent: string;
  model: string;
  /** One "kind:pattern" a line. */
  permissions: string;
  maxTurns: string;
  maxCostUSD: string;
  timeout: string;
  catchUp: "none" | "once";
  prompt: string;
}

/** A blank form. */
export const emptyWorkerForm: WorkerForm = {
  name: "",
  description: "",
  schedule: "",
  timezone: "",
  agent: "",
  model: "",
  permissions: "",
  maxTurns: "",
  maxCostUSD: "",
  timeout: "",
  catchUp: "none",
  prompt: "",
};

/** Whether a name can be a worker's (as the service checks it). */
export const validWorkerName = (name: string) => /^[a-z0-9][a-z0-9_-]{0,63}$/.test(name);

/** A worker's name made from a title: "Nightly deps report" → "nightly-deps-report". */
export function workerName(title: string): string {
  return title
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^[-_]+|[-_]+$/g, "")
    .slice(0, 64);
}

/** What the form needs before it can be sent ("" when it's complete). */
export function missing(f: WorkerForm): "name" | "schedule" | "prompt" | "" {
  if (!validWorkerName(f.name.trim())) return "name";
  if (!f.schedule.trim()) return "schedule";
  if (!f.prompt.trim()) return "prompt";
  return "";
}

/** The CreateWorker request's fields for the form (numbers that don't parse are 0: unset). */
export function workerRequest(f: WorkerForm) {
  const num = (s: string) => {
    const n = Number(s.trim());
    return Number.isFinite(n) && n > 0 ? n : 0;
  };
  return {
    name: f.name.trim(),
    description: f.description.trim(),
    schedule: f.schedule.trim(),
    timezone: f.timezone.trim(),
    agent: f.agent,
    model: f.model.trim(),
    permissions: f.permissions
      .split("\n")
      .map((l) => l.trim())
      .filter(Boolean),
    maxTurns: Math.floor(num(f.maxTurns)),
    maxCostUsd: num(f.maxCostUSD),
    timeout: f.timeout.trim(),
    catchUp: f.catchUp === "once" ? "once" : "",
    prompt: f.prompt.trim(),
  };
}

/** A worker's fields as the service takes and gives them (CreateWorker, WorkerDefinition). */
export type WorkerFields = ReturnType<typeof workerRequest>;

/** The form for a worker as written, to edit it (0 and "" are unset). */
export function formFromWorker(d: WorkerFields): WorkerForm {
  return {
    name: d.name,
    description: d.description,
    schedule: d.schedule,
    timezone: d.timezone,
    agent: d.agent,
    model: d.model,
    permissions: d.permissions.join("\n"),
    maxTurns: d.maxTurns > 0 ? String(d.maxTurns) : "",
    maxCostUSD: d.maxCostUsd > 0 ? String(d.maxCostUsd) : "",
    timeout: d.timeout,
    catchUp: d.catchUp === "once" ? "once" : "none",
    prompt: d.prompt,
  };
}

/** Whether saving an edit pauses the worker: it's enabled, and a changed worker waits to be enabled again. */
export const savePauses = (state: WorkerState) => state === WorkerState.ENABLED;

/** Whether the form differs from the worker as loaded (so reloading it loses something). */
export function edited(f: WorkerForm, loaded: WorkerForm | null): boolean {
  if (!loaded) return false;
  return JSON.stringify(workerRequest(f)) !== JSON.stringify(workerRequest(loaded));
}

/** What the dialog's footer says: what the form lacks, or where it's saved (and, editing, whether saving pauses the worker). */
export function footerNote(f: WorkerForm, edit?: { path: string; pauses: boolean }): { key: string; path: string } {
  const lack = missing(f);
  if (lack) return { key: `desktop.newworker.needs.${lack}`, path: "" };
  if (edit) return { key: edit.pauses ? "desktop.editworker.pauses" : "desktop.editworker.where", path: edit.path };
  return { key: "desktop.newworker.where", path: `.agents/workers/${f.name.trim()}/WORKER.md` };
}

/** Whether the dialog's Create or Save can be pressed: the form is complete, nothing is being sent, and an edit's file is loaded and current. */
export function canSave(f: WorkerForm, s: { busy: boolean; editing: boolean; loaded: boolean; stale: boolean }): boolean {
  if (missing(f) || s.busy || checkModelRef(f.model, emptyCatalog).error) return false;
  return !s.editing || (s.loaded && !s.stale);
}
