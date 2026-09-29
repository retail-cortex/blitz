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
import { emptyWorkerForm, missing, validWorkerName, workerName, workerRequest } from "./workerForm";

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
