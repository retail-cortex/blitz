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

import { beforeAll, describe, expect, it, vi } from "vitest";
import { config, workers, workspaces } from "../api";

// ?fake as strict as the service: what the service refuses, the fake
// refuses; what it keeps, the fake gives back. The fake reads its
// switches from the page's address, which the tests give it.
beforeAll(async () => {
  vi.stubGlobal("location", new URL("http://localhost/?fake"));
  (await import("./fake")).installFake();
});

describe("the fake service", () => {
  it("refuses a setting the service doesn't set, and a value not of its type", async () => {
    await expect(config.setConfigValue({ workspace: "", key: "health.enabled", value: "true" })).rejects.toThrow("isn't one of the settings set this way");
    await expect(config.setConfigValue({ workspace: "", key: "workers.policy.max_turns", value: "abc" })).rejects.toThrow("whole number");
  });

  it("puts back the shipped limits when a field is cleared, with no caps", async () => {
    await config.setConfigValue({ workspace: "", key: "workers.policy.max_timeout", value: "3h" });
    await config.setConfigValue({ workspace: "", key: "workers.policy.max_timeout", value: "" });
    await config.setConfigValue({ workspace: "", key: "workers.policy.default_max_turns", value: "" });
    const { policy } = await workers.listAllWorkers({});
    expect(policy?.maxTimeout).toBe("");
    expect(policy?.defaultMaxTurns).toBe(50);
    expect(policy?.maxTurns).toBe(0);
  });

  it("keeps a model's settings and gives them back", async () => {
    const before = await workspaces.getModelSettings({ workspace: "/w", ref: "gemini/gemini-4-argon" });
    expect(before.model?.settings?.temperature).toBeUndefined();
    const r = await workspaces.updateModelSettings({ workspace: "/w", ref: "gemini/gemini-4-argon", changes: [{ key: "temperature", value: "0" }] });
    expect(r.model?.settings?.temperature).toBe(0);
    await expect(workspaces.updateModelSettings({ workspace: "/w", ref: "gemini/gemini-4-argon", changes: [{ key: "temp", value: "1" }] })).rejects.toThrow("unknown setting");
  });

  it("removes a permission rule from its effect's list only", async () => {
    await config.addPermission({ workspace: "", effect: "allow", rule: "shell(make test)" });
    await config.addPermission({ workspace: "", effect: "deny", rule: "shell(make test)" });
    const r = await config.removePermission({ workspace: "", rule: "shell(make test)", effect: "allow" });
    expect(r.removed).toBe(1);
    const left = (await config.describePermissions({ workspace: "" })).rules.filter((x) => x.rule === "shell(make test)");
    expect(left.map((x) => x.effect)).toEqual(["deny"]);
  });

  it("refuses a rule and a guardrail the service refuses", async () => {
    expect((await workspaces.saveRule({ workspace: "/w", previousName: "", rule: { name: "late", when: "after-agent", action: "block", text: "x" } })).problems).toHaveLength(1);
    expect((await workspaces.saveGuardrail({ workspace: "/w", previousName: "", guardrail: { name: "limits", stage: "action", action: "block", text: "x" } })).problems).toEqual(['name "limits" is a built-in guardrail\'s']);
  });

  it("finds a global job to edit, and refuses a negative limit", async () => {
    const { home } = await workers.listAllWorkers({});
    const global = (await workers.listWorkers({ workspace: home })).workers[0];
    const { worker, hash } = await workers.getWorkerSpec({ workspace: home, name: global.name });
    expect(worker?.name).toBe(global.name);
    expect((await workers.updateWorker({ workspace: home, hash, worker: { ...worker!, maxTurns: -1 } })).problems).toEqual(["limits can't be negative"]);
  });

  it("checks a human task's answer against its form", async () => {
    const task = (await workspaces.listHumanTasks({ workspace: "" })).tasks[0];
    const bad = JSON.stringify({ version: "v0.9", action: { name: "approve", surfaceId: task.id, sourceComponentId: "nope", context: {} } });
    await expect(workspaces.answerHumanTask({ workspace: task.workspace, taskId: task.id, actionJson: bad })).rejects.toThrow("isn't one of the form's buttons");
  });

  it("sends the page server's keepalives on a quiet stream", async () => {
    const ctl = new AbortController();
    const got: unknown[] = [];
    for await (const m of workspaces.watchHumanTasks({ workspace: "" }, { signal: ctl.signal })) {
      got.push(m);
      if (got.length === 3) break;
    }
    ctl.abort();
    expect(got.slice(1).some((m) => (m as { tasks: unknown[] }).tasks.length === 0)).toBe(true);
  });
});
