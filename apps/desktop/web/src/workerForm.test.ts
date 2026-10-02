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
import { WorkerState } from "./gen/blitz/v1/worker_pb";
import { canSave, edited, emptyWorkerForm, footerNote, formFromWorker, missing, savePauses, validWorkerName, workerName, workerRequest, type WorkerFields } from "./workerForm";

describe("workerName", () => {
  it.each([
    ["Nightly deps report", "nightly-deps-report"],
    ["  Café  résumé! ", "cafe-resume"],
    ["--x--", "x"],
    ["a".repeat(80), "a".repeat(64)],
  ])("%s", (title, want) => {
    expect(workerName(title)).toBe(want);
    expect(validWorkerName(want)).toBe(true);
  });
  it.each([["My Worker"], ["-x"], [""], ["a/b"]])("%s isn't a name", (n) => expect(validWorkerName(n)).toBe(false));
});

describe("missing", () => {
  const f = { ...emptyWorkerForm, name: "deps", schedule: "@daily", prompt: "Go." };
  it.each([
    [f, ""],
    [{ ...f, name: "Deps" }, "name"],
    [{ ...f, schedule: " " }, "schedule"],
    [{ ...f, prompt: "" }, "prompt"],
  ])("%j", (form, want) => expect(missing(form)).toBe(want));
});

describe("workerRequest", () => {
  it("trims, splits permissions by line, and leaves bad numbers unset", () => {
    const r = workerRequest({
      ...emptyWorkerForm,
      name: " deps ",
      schedule: " Weekdays at 9:30 ",
      permissions: "shell:go list -m -u all\n\n  write:reports/*  \n",
      maxTurns: "12.7",
      maxCostUSD: "abc",
      timeout: " 20m ",
      catchUp: "once",
      prompt: "\nReport.\n",
    });
    expect(r).toMatchObject({
      name: "deps",
      schedule: "Weekdays at 9:30",
      permissions: ["shell:go list -m -u all", "write:reports/*"],
      maxTurns: 12,
      maxCostUsd: 0,
      timeout: "20m",
      catchUp: "once",
      prompt: "Report.",
    });
    expect(workerRequest(emptyWorkerForm).catchUp).toBe("");
  });
});

describe("formFromWorker", () => {
  const written: WorkerFields = {
    name: "deps",
    description: "Nightly deps",
    schedule: "Daily at 6 AM",
    timezone: "Europe/Paris",
    agent: "qa",
    model: "gemini/gemini-3.8-flash",
    permissions: ["shell:go list -m -u all", "write:reports/*"],
    maxTurns: 30,
    maxCostUsd: 0.5,
    timeout: "20m",
    catchUp: "once",
    prompt: "Report.",
  };
  it("round-trips through the form", () => {
    expect(workerRequest(formFromWorker(written))).toEqual(written);
  });
  it.each([
    ["unset numbers stay empty", { maxTurns: 0, maxCostUsd: 0 }, { maxTurns: "", maxCostUSD: "" }],
    ["no catch-up is Skip", { catchUp: "" }, { catchUp: "none" }],
    ["permissions are a line each", { permissions: ["a:1", "b:2"] }, { permissions: "a:1\nb:2" }],
  ])("%s", (_, change, want) => {
    expect(formFromWorker({ ...written, ...change })).toMatchObject(want);
  });
});

describe("savePauses", () => {
  it.each([
    [WorkerState.ENABLED, true],
    [WorkerState.CHANGED, false],
    [WorkerState.DISABLED, false],
    [WorkerState.NEW, false],
    [WorkerState.INVALID, false],
  ])("%s", (state, want) => expect(savePauses(state)).toBe(want));
});

describe("edited", () => {
  const loaded = { ...emptyWorkerForm, name: "deps", schedule: "@daily", prompt: "Go." };
  it.each([
    ["nothing loaded", loaded, null, false],
    ["unchanged", { ...loaded }, loaded, false],
    ["only whitespace", { ...loaded, prompt: " Go. \n" }, loaded, false],
    ["a new schedule", { ...loaded, schedule: "@hourly" }, loaded, true],
  ])("%s", (_, f, base, want) => expect(edited(f, base)).toBe(want));
});

describe("footerNote", () => {
  const f = { ...emptyWorkerForm, name: "deps", schedule: "@daily", prompt: "Go." };
  const path = "/w/.agents/workers/deps/WORKER.md";
  it.each([
    ["what's missing first", { ...f, prompt: "" }, undefined, { key: "desktop.newworker.needs.prompt", path: "" }],
    ["where a new one goes", f, undefined, { key: "desktop.newworker.where", path: ".agents/workers/deps/WORKER.md" }],
    ["an edit's file", f, { path, pauses: false }, { key: "desktop.editworker.where", path }],
    ["that saving pauses it", f, { path, pauses: true }, { key: "desktop.editworker.pauses", path }],
  ])("%s", (_, form, edit, want) => expect(footerNote(form, edit)).toEqual(want));
});

describe("canSave", () => {
  const f = { ...emptyWorkerForm, name: "deps", schedule: "@daily", prompt: "Go." };
  const s = { busy: false, editing: false, loaded: false, stale: false };
  it.each([
    ["a complete new worker", f, s, true],
    ["an incomplete form", { ...f, schedule: "" }, s, false],
    ["while sending", f, { ...s, busy: true }, false],
    ["an edit not loaded yet", f, { ...s, editing: true }, false],
    ["a loaded edit", f, { ...s, editing: true, loaded: true }, true],
    ["an edit changed on disk", f, { ...s, editing: true, loaded: true, stale: true }, false],
    ["a model that can't be one", { ...f, model: "gemini 3" }, s, false],
    ["a model of no provider's list (a warning only)", { ...f, model: "gemini-nope" }, s, true],
  ])("%s", (_, form, state, want) => expect(canSave(form, state)).toBe(want));
});
