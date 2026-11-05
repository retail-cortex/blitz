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
import { agentProblems, apiProblem, checkAnswer, configValueProblem, guardrailProblems, jobProblems, mcpProblem, providerProblem, ruleProblems, sessionSetting, setModelSetting, withKeepalives, type AgentFields, type JobFields } from "./fakeChecks";
import type { Form } from "../a2ui/model";

// The fake refuses what the service refuses: each case is one the Go
// side's tests refuse (or take) too.

describe("ruleProblems (gates.Check)", () => {
  const ok = { name: "no-secrets", when: "after_agent", action: "block", text: "No keys in replies." };
  it.each([
    ["a good rule", ok, 0],
    ["an instruction rule", { ...ok, when: "", action: "" }, 0],
    ["a when with a hyphen", { ...ok, when: "after-agent" }, 1],
    ["an action it doesn't take", { ...ok, action: "redact" }, 1],
    ["a name over 64", { ...ok, name: "a".repeat(65) }, 1],
    ["an empty rule", { ...ok, text: " " }, 1],
  ])("%s", (_, r, n) => expect(ruleProblems(r)).toHaveLength(n));
});

describe("guardrailProblems (gates.CheckGuardrail)", () => {
  const custom = { name: "no-branch-delete", builtin: "", stage: "action", action: "block", text: "Deleting a branch." };
  it.each([
    ["a custom one", custom, 0],
    ["a custom one named as a built-in", { ...custom, name: "limits" }, 1],
    ["a custom one that redacts", { ...custom, action: "redact" }, 1],
    ["a stage that isn't one", { ...custom, stage: "before" }, 1],
    ["a built-in with its action", { ...custom, name: "", builtin: "secrets", action: "warn", text: "" }, 0],
    ["a built-in with an action it doesn't take", { ...custom, name: "", builtin: "limits", action: "warn", text: "" }, 1],
  ])("%s", (_, g, n) => expect(guardrailProblems(g)).toHaveLength(n));
});

describe("agentProblems (agents.Save)", () => {
  const ok: AgentFields = { name: "reviewer", description: "Reviews changes", agencyLevel: "", permissionMode: "", maxTurns: 0, background: false, isolation: "", effort: "" };
  it.each<[string, Partial<AgentFields>, number]>([
    ["a good agent", {}, 0],
    ["no description", { description: "" }, 1],
    ["a name over 64", { name: "a".repeat(65) }, 1],
    ["a permission mode that isn't one", { permissionMode: "yolo" }, 1],
    ["accept-edits", { permissionMode: "accept-edits" }, 0],
    ["worktree without background", { isolation: "worktree" }, 1],
    ["a temperature over 2", { temperature: 2.5 }, 1],
    ["a top P of 0", { topP: 0 }, 1],
    ["xhigh effort", { effort: "xhigh" }, 0],
    ["an effort that isn't one", { effort: "huge" }, 1],
    ["an agency that isn't one", { agencyLevel: "total" }, 1],
  ])("%s", (_, f, n) => expect(agentProblems({ ...ok, ...f })).toHaveLength(n));
});

describe("jobProblems (workers' load)", () => {
  const ok: JobFields = { name: "deps", schedule: "Weekdays at 9:30", permissions: ["shell:go list -m -u all", "write:reports/"], maxTurns: 0, maxCostUsd: 0, timeout: "", catchUp: "", prompt: "Check the modules.", workflow: "", runsOn: "", onRequest: "", merge: "" };
  it.each<[string, Partial<JobFields>, number]>([
    ["a good job", {}, 0],
    ["a name with capitals", { name: "Deps" }, 1],
    ["no prompt nor workflow", { prompt: "" }, 1],
    ["a negative limit", { maxTurns: -1 }, 1],
    ["a timeout that isn't one", { timeout: "soon" }, 1],
    ["a kind that isn't one", { permissions: ["exec:make"] }, 1],
    ["a path out of the workspace", { permissions: ["write:../etc"] }, 1],
    ["a catch-up that isn't one", { catchUp: "all" }, 1],
  ])("%s", (_, f, n) => expect(jobProblems({ ...ok, ...f })).toHaveLength(n));
});

describe("mcpProblem", () => {
  it.each([
    ["a command", { name: "fs", command: "npx", url: "" }, false],
    ["a URL", { name: "jira", command: "", url: "https://x" }, false],
    ["both", { name: "x", command: "npx", url: "https://x" }, true],
    ["neither", { name: "x", command: "", url: "" }, true],
  ])("%s", (_, m, bad) => expect(!!mcpProblem(m)).toBe(bad));
});

describe("providerProblem (config.SetProvider, MDL-100)", () => {
  const api = { baseUrl: "https://gw.example/v1", headers: [{ name: "Authorization", value: "Bearer {{TOKEN_SCRIPT}}" }], tokenScript: "print-token", tokenScriptLanguage: "" };
  it.each([
    ["gemini signs in with a Google account", { provider: "gemini", key: "", auth: { method: "oauth", profile: "" } }, ""],
    ["openai takes no sign-in", { provider: "openai", key: "", auth: { method: "oauth", profile: "" } }, "openai signs in with an API key only"],
    ["a key for ollama", { provider: "ollama", key: "k" }, "choose a provider that takes an API key"],
    ["the api provider", { provider: "api", key: "", api }, ""],
    ["api settings for another provider", { provider: "openai", key: "", api }, "need provider api"],
    ["a placeholder in the host", { provider: "api", key: "", api: { ...api, baseUrl: "https://{{TOKEN_SCRIPT}}.example" } }, "not its host"],
    ["a script nothing uses", { provider: "api", key: "", api: { ...api, headers: [{ name: "X-Key", value: "k" }] } }, "no header or base_url uses"],
    ["no headers", { provider: "api", key: "", api: { ...api, headers: [] } }, "needs at least one header"],
  ])("%s", (_, c, want) => {
    const got = providerProblem(c);
    if (want) expect(got).toContain(want);
    else expect(got).toBe("");
  });
  it("keeps a header's stored value when the form leaves it empty", () => {
    expect(apiProblem({ ...api, headers: [{ name: "Authorization", value: "" }] }, { Authorization: "Bearer {{TOKEN_SCRIPT}}" })).toBe("");
    expect(apiProblem({ ...api, headers: [{ name: "Authorization", value: "" }] })).toContain("needs a value");
  });
});

describe("configValueProblem (config.SetValue)", () => {
  it.each([
    ["an unknown key", "health.enabled", "true", true],
    ["a misspelt key", "workers.policy.max_turn", "5", true],
    ["a count", "workers.policy.max_turns", "200", false],
    ["a count that isn't one", "workers.policy.max_turns", "abc", true],
    ["dollars", "workers.policy.max_cost_usd", "12.5", false],
    ["a length of time", "workers.policy.max_timeout", "2h", false],
    ["a length of time that isn't one", "workers.policy.max_timeout", "2 hours", true],
    ["empty clears any", "workers.policy.max_timeout", "", false],
    ["a kind that isn't one", "workers.policy.allow", "shell, exec", true],
    ["a health cost", "health.max_cost_usd", "1", false],
    ["a provider that isn't one", "llm.provider", "acme", true],
    ["a sign-in a provider doesn't take", "llm.openai.auth", "oauth", true],
  ])("%s", (_, key, value, bad) => expect(!!configValueProblem(key, value)).toBe(bad));
});

describe("setModelSetting (ModelSettings.Set)", () => {
  it.each([
    ["a temperature", "temperature", "0.7", { temperature: 0.7 }],
    ["a temperature of 0", "temperature", "0", { temperature: 0 }],
    ["clearing it", "temperature", "", {}],
    ["a temperature over 2", "temperature", "3", "temperature must be a number in [0, 2]"],
    ["a top P of 0", "top_p", "0", "top_p must be a number in (0, 1]"],
    ["max tokens", "max_tokens", "1024", { maxTokens: 1024 }],
    ["xhigh effort", "reasoning_effort", "xhigh", { reasoningEffort: "max" }],
    ["a key it doesn't know", "temp", "1", 'unknown setting "temp" (known: temperature, max_tokens, top_p, seed, reasoning_effort, thinking_budget)'],
  ])("%s", (_, key, text, want) => expect(setModelSetting({}, key, text)).toEqual(want));
});

describe("sessionSetting (Workspace.Set)", () => {
  it.each([
    ["auto effort is unset", "effort", "auto", { key: "effort", value: "" }],
    ["an effort", "reasoning_effort", "high", { key: "effort", value: "high" }],
    ["an agency", "agency_level", "Low", { key: "agency", value: "low" }],
    ["an agency that isn't one", "agency", "total", { error: "agency level must be low, medium, high or extreme", reason: "INVALID_AGENCY" }],
    ["a key it doesn't know", "colour", "red", { error: 'unknown setting "colour"', reason: "UNKNOWN_SETTING" }],
  ])("%s", (_, key, value, want) => expect(sessionSetting(key, value)).toEqual(want));
});

describe("checkAnswer (Form.Answer)", () => {
  const form: Form = {
    components: [
      { id: "root", component: "Column", children: ["items", "approve"] },
      { id: "items", component: "List", children: { path: "/items", componentId: "item" } },
      { id: "item", component: "TextField", value: { path: "name" }, checks: [{ call: "required", args: { value: { path: "name" } }, message: "name it" }] },
      { id: "approve", component: "Button", child: "l", action: { event: { name: "approve", context: { note: { path: "/note" } } } } },
    ],
    data: { note: "", items: [{ name: "kettle" }] },
  };
  const action = (a: Record<string, unknown>) => JSON.stringify({ version: "v0.9", action: { name: "approve", surfaceId: "t1", sourceComponentId: "approve", context: { note: "ok" }, ...a } });
  it.each([
    ["an answer the form takes", action({}), "", { answer: { action: "approve", note: "ok" } }],
    ["not an action", JSON.stringify({ version: "v0.9" }), "", { problems: ["not an action message"] }],
    ["another task's", action({ surfaceId: "t2" }), "", { problems: ["it answers t2, not this task"] }],
    ["a button that isn't one", action({ sourceComponentId: "item" }), "", { problems: ['"item" isn\'t one of the form\'s buttons'] }],
    ["another action", action({ name: "reject" }), "", { problems: ['approve: its action isn\'t "reject"'] }],
    ["a context the button doesn't send", action({ context: { note: "ok", extra: 1 } }), "", { problems: ['approve: its action sends no "extra"'] }],
    ["a data model whose item fails", action({}), JSON.stringify({ note: "ok", items: [{ name: "" }] }), { problems: ["item: name it"] }],
    ["a context that isn't the data's", action({}), JSON.stringify({ note: "other", items: [{ name: "kettle" }] }), { problems: ["approve: note isn't what the form holds"] }],
  ])("%s", (_, a, data, want) => expect(checkAnswer(form, "t1", a, data)).toEqual(want));
});

describe("withKeepalives", () => {
  it("sends an empty message while the stream is quiet, then what it sends", async () => {
    async function* slow() {
      await new Promise((r) => setTimeout(r, 40));
      yield { listed: true };
    }
    const got: unknown[] = [];
    for await (const m of withKeepalives(slow(), undefined, 15)) got.push(m);
    expect(got.length).toBeGreaterThan(1);
    expect(got.slice(0, -1).every((m) => JSON.stringify(m) === "{}")).toBe(true);
    expect(got.at(-1)).toEqual({ listed: true });
  });
  it("stops when the call is aborted", async () => {
    const ctl = new AbortController();
    async function* never() {
      await new Promise(() => {});
      yield 1;
    }
    const got: unknown[] = [];
    for await (const m of withKeepalives(never(), ctl.signal, 5)) {
      got.push(m);
      if (got.length === 2) ctl.abort();
    }
    expect(got).toHaveLength(2);
  });
});
