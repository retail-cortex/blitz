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

// The service's checks, as the ?fake service (fake.ts) runs them: a fake
// must refuse what the service refuses, or a form that sends something
// the service turns down looks fine in ?fake. Each is a port of the Go
// it names, with the Go's words; where a port can only come close (a
// schedule, which the service reads with its own parser), it says so.
import { formProblems, resolve, setPath, type Component, type Form } from "../a2ui/model";
import { parseDuration } from "../workerForm";

/** The name rule for rules and guardrails (gates.ValidName): single hyphens or underscores, up to 64. */
export const validGateName = (name: string) => name.length <= 64 && /^[a-z0-9]+([-_][a-z0-9]+)*$/.test(name);

/** What keeps a rule from being saved (gates.Check). */
export function ruleProblems(r: { name: string; when: string; action: string; text: string }): string[] {
  const out: string[] = [];
  if (!validGateName(r.name)) out.push(`name ${JSON.stringify(r.name)}: lowercase letters, digits and single hyphens, up to 64`);
  if (!["", "before_agent", "after_agent"].includes(r.when)) out.push(`when ${JSON.stringify(r.when)}: before_agent, after_agent, or none for an instruction rule`);
  if (r.when !== "" && !["block", "warn", "revise"].includes(r.action)) out.push(`action ${JSON.stringify(r.action)}: block, warn or revise`);
  if (!r.text.trim()) out.push("the rule is empty: say what to check");
  return out;
}

/** The built-in guardrails (gates.Builtins): where each runs, its default action and the ones it takes. */
export const guardBuiltins = [
  { id: "prompt_injection", stage: "tool_result", defaultAction: "redact", actions: ["redact", "warn", "block"] },
  { id: "secrets", stage: "output", defaultAction: "redact", actions: ["redact", "warn", "block"] },
  { id: "limits", stage: "action", defaultAction: "block", actions: ["block"] },
];

/** The limits guardrail without a file (gates.BuiltinDefault): 200 model calls, $10, and the default commands (gates.DefaultsEntry). */
export const builtinLimits = () => ({ maxTurns: 200, maxCostUsd: 10, maxFilesChanged: 0, allowedDomains: [] as string[], commands: ["defaults"] });

/** What keeps a guardrail from being saved (gates.CheckGuardrail, checkGuardrail). */
export function guardrailProblems(g: { name: string; builtin: string; stage: string; action: string; text: string }): string[] {
  const out: string[] = [];
  const name = g.builtin || g.name;
  if (!validGateName(name)) out.push(`name ${JSON.stringify(name)}: lowercase letters, digits and single hyphens, up to 64`);
  if (!g.builtin && guardBuiltins.some((b) => b.id === name)) out.push(`name ${JSON.stringify(name)} is a built-in guardrail's`);
  if (g.builtin) {
    const b = guardBuiltins.find((x) => x.id === g.builtin);
    if (!b) return [...out, `builtin ${JSON.stringify(g.builtin)}: prompt_injection, secrets or limits`];
    const action = g.action || b.defaultAction;
    if (!b.actions.includes(action)) out.push(`action ${JSON.stringify(action)}: ${b.actions.join(", ")} for ${b.id}`);
    return out;
  }
  if (!["input", "tool_result", "output", "action"].includes(g.stage)) return [...out, `stage ${JSON.stringify(g.stage)}: input, tool_result, output or action`];
  const action = g.action || "block";
  if (action !== "block" && action !== "warn") return [...out, `action ${JSON.stringify(action)}: block or warn (redact is the secrets guardrail's)`];
  if (!g.text.trim()) out.push("the guardrail is empty: say what to catch");
  return out;
}

/** An effort as the service reads one (config.ParseEffort; xhigh is max), or null. */
export function parseEffort(s: string): string | null {
  const e = s.trim().toLowerCase();
  const v = e === "xhigh" ? "max" : e;
  return ["minimal", "low", "medium", "high", "max"].includes(v) ? v : null;
}

/** Whether a permission mode is one the service reads (api.ParsePermissionMode). */
export function validPermissionMode(s: string): boolean {
  const k = s.trim().toLowerCase().replace(/[-_ ]/g, "");
  return ["", "default", "ask", "manual", "acceptedits", "edits", "plan", "dontask", "deny", "auto", "bypass", "bypasspermissions"].includes(k);
}

/** An agent file's fields, as SaveAgentFile sends them. */
export interface AgentFields {
  name: string;
  description: string;
  agencyLevel: string;
  permissionMode: string;
  maxTurns: number;
  background: boolean;
  isolation: string;
  temperature?: number;
  topP?: number;
  maxTokens?: number;
  effort: string;
  thinkingBudget?: number;
}

/** What keeps an agent from being saved (agents.Save: the description, then AgentMetadata.Check, which stops at the first). */
export function agentProblems(a: AgentFields): string[] {
  const out: string[] = [];
  if (!a.description.trim()) out.push("description: say what the agent is for");
  const check = (): string => {
    if (!a.name) return "agent spec must declare a 'name' in frontmatter";
    if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(a.name)) return `name ${JSON.stringify(a.name)}: use at most 64 lowercase letters, digits, - and _, starting with a letter or digit`;
    if (a.agencyLevel && !["low", "medium", "high", "extreme"].includes(a.agencyLevel)) return `agency_level: ${JSON.stringify(a.agencyLevel)} (one of low, medium, high, extreme)`;
    if (a.permissionMode && !validPermissionMode(a.permissionMode)) return `permission_mode: unknown permission mode ${JSON.stringify(a.permissionMode)} (use default, accept-edits, auto, plan, dont-ask or bypass)`;
    if (a.maxTurns < 0) return "max_turns must not be negative";
    if (a.isolation && a.isolation !== "worktree") return `isolation: ${JSON.stringify(a.isolation)} (only worktree)`;
    if (a.isolation && !a.background) return "isolation: worktree needs background: true";
    if (a.temperature !== undefined && (a.temperature < 0 || a.temperature > 2)) return `temperature: ${a.temperature} (0 to 2)`;
    if (a.topP !== undefined && (a.topP <= 0 || a.topP > 1)) return `top_p: ${a.topP} (above 0, at most 1)`;
    if (a.maxTokens !== undefined && a.maxTokens < 1) return `max_tokens: ${a.maxTokens} (at least 1)`;
    if (a.thinkingBudget !== undefined && a.thinkingBudget < 0) return `thinking_budget: ${a.thinkingBudget} (0 or more)`;
    if (a.effort && parseEffort(a.effort) === null) return `effort: reasoning effort ${JSON.stringify(a.effort)}: use minimal, low, medium, high, max`;
    return "";
  };
  const c = check();
  return c ? [...out, c] : out;
}

/** The permission kinds a job may be given (workers' permissionKinds). */
export const jobKinds = ["shell", "write", "delete", "web", "mcp"];

/** Why a job's permission line isn't one (workers.ParsePermission), or "". */
export function permissionProblem(line: string): string {
  const i = line.indexOf(":");
  const kind = (i < 0 ? line : line.slice(0, i)).trim().toLowerCase();
  const pattern = i < 0 ? "" : line.slice(i + 1).trim();
  if (i < 0 || !jobKinds.includes(kind) || !pattern) return `permission ${JSON.stringify(line)}: use kind:pattern with kind shell, write, delete, web or mcp`;
  if ((kind === "write" || kind === "delete") && (pattern.startsWith("/") || `/${pattern}/`.includes("/../"))) return `permission ${JSON.stringify(line)}: paths are relative to the workspace and can't use ".."`;
  return "";
}

/** A job as CreateWorker and UpdateWorker send it. */
export interface JobFields {
  name: string;
  schedule: string;
  permissions: string[];
  maxTurns: number;
  maxCostUsd: number;
  timeout: string;
  catchUp: string;
  prompt: string;
  workflow: string;
  runsOn: string;
  onRequest: string;
  merge: string;
}

/**
 * What keeps a job from being saved (workers' load of a WORKER.md). The
 * schedule is the service's own parser's to read: the fake refuses only a
 * sentence it can't be ("when …").
 */
export function jobProblems(w: JobFields): string[] {
  const out: string[] = [];
  if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(w.name)) out.push(`directory name ${JSON.stringify(w.name)} isn't a worker name (lowercase letters, digits, - and _)`);
  if (!w.prompt.trim() && !w.workflow.trim()) out.push("the workflow (the text after the frontmatter) is empty");
  if (!w.schedule.trim() || /^when/i.test(w.schedule.trim())) out.push(`schedule ${JSON.stringify(w.schedule)}: use a cron expression, a descriptor or plain text like "Daily at 6 AM"`);
  for (const p of w.permissions) {
    const why = permissionProblem(p);
    if (why) out.push(why);
  }
  if (w.timeout) {
    const d = parseDuration(w.timeout);
    if (d === null || d <= 0) out.push(`limits.timeout ${JSON.stringify(w.timeout)} isn't a duration like 20m`);
  }
  if (w.maxTurns < 0 || w.maxCostUsd < 0) out.push("limits can't be negative");
  if (!["", "here", "service"].includes(w.runsOn)) out.push(`runs_on ${JSON.stringify(w.runsOn)}: use "here" or "service"`);
  if (!["", "main", "never"].includes(w.merge)) out.push(`merge ${JSON.stringify(w.merge)}: use "main" or "never"`);
  if (!["", "refuse", "pause"].includes(w.onRequest)) out.push(`on_request ${JSON.stringify(w.onRequest)}: use "refuse" or "pause"`);
  if (!["", "none", "once"].includes(w.catchUp)) out.push(`catch_up ${JSON.stringify(w.catchUp)}: use "none" or "once"`);
  return out;
}

/** Why an MCP server can't be saved (config.AddMCPServer, ReplaceMCPServer), or "". */
export function mcpProblem(s: { name: string; command: string; url: string }): string {
  return !s.name || !s.command === !s.url ? "an MCP server needs a name and either a command or a URL" : "";
}

/** The providers (config.Providers), and those that take an API key and how each other signs in. */
export const providers = ["gemini", "anthropic", "openai", "ollama", "bedrock", "azure", "vertex-anthropic", "api"];
const keyed = ["gemini", "anthropic", "openai"];
const authMethods: Record<string, string[]> = { gemini: ["adc", "oauth"], anthropic: ["oauth", "adc"], bedrock: ["oauth"], azure: ["oauth", "entra"] };

/** Why a sign-in method isn't one the provider takes (ProviderAuth.check), or "". */
export function authProblem(provider: string, method: string, profile = ""): string {
  if (!method || method === "api_key") return "";
  const m = authMethods[provider] ?? [];
  if (m.includes(method)) return /[/\\]/.test(profile) ? `${JSON.stringify(profile)} isn't a profile name` : "";
  return m.length ? `${provider} signs in with api_key or ${m.join(" or ")}, not ${JSON.stringify(method)}` : `${provider} signs in with an API key only, not ${JSON.stringify(method)}`;
}

/** The api provider's settings as SetProvider sends them (ApiProviderSettings). */
export interface APISettings {
  baseUrl: string;
  headers: { name: string; value: string }[];
  tokenScript: string;
  tokenScriptLanguage: string;
}

const token = "{{TOKEN_SCRIPT}}";

/**
 * Why the api provider's settings can't build a model (APIProviderSettings.edits
 * and APIConfig.Check, spec_015_models MDL-100), or "". stored are the
 * headers already in the file, whose values a form may leave empty.
 */
export function apiProblem(a: APISettings, stored: Record<string, string> = {}): string {
  const headers: Record<string, string> = {};
  for (const h of a.headers) {
    const name = h.name.trim();
    if (name in headers) return `[llm.api] header ${JSON.stringify(name)} is given twice`;
    if (h.value) headers[name] = h.value;
    else if (stored[name]) headers[name] = stored[name];
    else return `[llm.api] header ${JSON.stringify(name)} needs a value`;
  }
  const base = a.baseUrl.trim();
  if (!base) return "[llm.api] needs a base_url";
  let u: URL | undefined;
  try {
    u = new URL(base.replaceAll(token, "blitztokenscriptplaceholder"));
  } catch {
    u = undefined;
  }
  if (!u || (u.protocol !== "http:" && u.protocol !== "https:") || !u.host) return `[llm.api] base_url ${JSON.stringify(base)} isn't an absolute http or https URL`;
  if (u.host.includes("blitztokenscriptplaceholder") || u.username.includes("blitztokenscriptplaceholder")) return `[llm.api] base_url can use ${token} only in its path or query, not its host`;
  const names = Object.keys(headers).sort();
  if (!names.length) return "[llm.api] needs at least one header (headers)";
  const seen = new Map<string, string>();
  for (const name of names) {
    if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(name)) return `[llm.api] header ${JSON.stringify(name)} isn't a valid HTTP header name`;
    const other = seen.get(name.toLowerCase());
    if (other !== undefined) return `[llm.api] headers ${JSON.stringify(other)} and ${JSON.stringify(name)} are the same header`;
    seen.set(name.toLowerCase(), name);
    if (/[\r\n\0]/.test(headers[name])) return `[llm.api] header ${JSON.stringify(name)}'s value has a line break`;
  }
  const uses = base.includes(token) || Object.values(headers).some((v) => v.includes(token));
  const script = !!a.tokenScript.trim();
  if (uses && !script) return `[llm.api] uses ${token} but has no token_script`;
  if (script && !uses) return `[llm.api] has a token_script but no header or base_url uses ${token}`;
  const lang = a.tokenScriptLanguage.trim() || "shell";
  if (!["shell", "python", "typescript"].includes(lang)) return `[llm.api] token_script_language ${JSON.stringify(a.tokenScriptLanguage)} isn't one of shell, python, typescript`;
  return "";
}

/** Why SetProvider refuses the choice (config.SetProvider), or "". */
export function providerProblem(c: { provider: string; key: string; auth?: { method: string; profile: string }; api?: APISettings }, stored: Record<string, string> = {}): string {
  if (c.provider && !providers.includes(c.provider)) return `unknown provider ${JSON.stringify(c.provider)} (${providers.slice(0, -1).join(", ")} or ${providers.at(-1)})`;
  if ((c.key && !keyed.includes(c.provider)) || (c.auth && !keyed.includes(c.provider) && !(authMethods[c.provider] ?? []).length)) return `choose a provider that takes an API key (${keyed.join(", ")}) for the key or sign-in`;
  if (c.auth) {
    const why = authProblem(c.provider, c.auth.method.trim(), c.auth.profile.trim());
    if (why) return why;
    if (c.key && c.auth.method && c.auth.method !== "api_key") return `an API key is for signing in with api_key, not ${c.auth.method}`;
  }
  if (c.api) {
    if (c.provider !== "api") return "the api provider's settings need provider api";
    return apiProblem(c.api, stored);
  }
  return "";
}

/** Whether a browser profile's name is one (config's profileName). */
export const validProfileName = (name: string) => /^[a-z0-9][a-z0-9_-]{0,31}$/.test(name);

/** Whether a key can name a secret (secrets.ValidKey). */
export const validSecretKey = (key: string) => /^[A-Za-z0-9_.-]{1,64}$/.test(key);

/** The settings SetConfigValue sets as strings (config's settableValues). */
const settable = [
  "llm.provider",
  "blitz.default_model",
  "llm.gemini.model",
  "llm.gemini.auth",
  "llm.gemini.project_id",
  "llm.gemini.location",
  "llm.anthropic.model",
  "llm.anthropic.auth",
  "llm.bedrock.auth",
  "llm.azure.auth",
  "llm.anthropic.profile",
  "llm.anthropic.project_id",
  "llm.anthropic.location",
  "llm.anthropic.base_url",
  "llm.openai.model",
  "llm.openai.base_url",
  "audio.model",
  "audio.voice",
  "blitz.default_agent",
  "blitz.agency_level",
  "blitz.permission_mode",
  "health.model",
  "ui.style",
  "ui.locale",
  "ui.notify",
  "ui.task_alerts",
  "ui.theme",
  "ui.editor",
  "search.embedding_model",
  "search.enrich_model",
];

/** The settings SetConfigValue reads as a type (config's typedValues), by kind. */
const typed: Record<string, string> = {
  "search.enabled": "bool",
  "search.enrich": "bool",
  "search.enrich_daily_limit": "int",
  "search.include_ignored": "bool",
  "search.sources": "sources",
  "workers.policy.allow": "kinds",
  "workers.policy.max_concurrent": "int",
  "workers.policy.default_max_turns": "int",
  "workers.policy.max_turns": "int",
  "workers.policy.default_max_cost_usd": "usd",
  "workers.policy.max_cost_usd": "usd",
  "workers.policy.default_timeout": "duration",
  "workers.policy.max_timeout": "duration",
  "health.schedule": "schedule",
  "harness.reflect_schedule": "schedule",
  "health.max_cost_usd": "usd",
  "harness.max_cost_usd": "usd",
  "harness.reflect_threshold": "int",
  "browser.show": "show",
  "browser.open": "open",
  "web.browser_fallback": "bool",
};

/** Why SetConfigValue refuses key = value (config.SetValue and typedValue), or "". "" always clears a setting. */
export function configValueProblem(key: string, value: string): string {
  const kind = typed[key];
  if (!settable.includes(key) && !kind) return `${JSON.stringify(key)} isn't one of the settings set this way (${[...settable, ...Object.keys(typed)].sort().join(", ")}); edit the file for the rest`;
  if (key === "llm.provider" && value && !providers.includes(value)) return `unknown provider ${JSON.stringify(value)} (${providers.slice(0, -1).join(", ")} or ${providers.at(-1)})`;
  const auth = /^llm\.(.+)\.auth$/.exec(key);
  if (auth) {
    const why = authProblem(auth[1], value);
    if (why) return why;
  }
  if (!kind || value === "") return "";
  switch (kind) {
    case "bool":
      return ["true", "false", "1", "0", "t", "f", "TRUE", "FALSE", "True", "False", "T", "F"].includes(value) ? "" : `${key} is true or false, not ${JSON.stringify(value)}`;
    case "int":
      return /^[+-]?\d+$/.test(value) && Number(value) >= 0 ? "" : `${key} is a whole number, 0 or more, not ${JSON.stringify(value)}`;
    case "usd": {
      const n = /^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/.test(value) ? Number(value) : NaN;
      return Number.isFinite(n) && n >= 0 ? "" : `${key} is an amount in US dollars, 0 or more, not ${JSON.stringify(value)}`;
    }
    case "duration": {
      const d = parseDuration(value);
      return d !== null && d > 0 ? "" : `${key} is a length of time like 30m or 2h, not ${JSON.stringify(value)}`;
    }
    case "schedule":
      return /^when/i.test(value.trim()) ? `${key}: can't read ${JSON.stringify(value)} as a schedule` : "";
    case "show":
      return ["ask", "watch", "silent"].includes(value) ? "" : `${key} is ask, watch, silent, not ${JSON.stringify(value)}`;
    case "open":
      return ["ask", "embedded", "system"].includes(value) ? "" : `${key} is ask, embedded, system, not ${JSON.stringify(value)}`;
    case "kinds":
      for (const s of value.split(",").map((x) => x.trim())) if (s && s !== "none" && !jobKinds.includes(s)) return `${JSON.stringify(s)} isn't a permission kind (${jobKinds.join(", ")})`;
      return "";
    case "sources": {
      const list = value
        .split(",")
        .map((x) => x.trim())
        .filter(Boolean);
      for (const s of list) if (!["files", "documents", "chats", "notes"].includes(s)) return `${JSON.stringify(s)} isn't a search source (files, documents, chats, notes)`;
      return list.length ? "" : `${key} needs at least one source`;
    }
  }
  return "";
}

/** The job limits' policy as the shipped settings have it (config.DefaultConfig's [workers.policy]): defaults, and no caps (WK-73). */
export const policyDefaults = { allow: ["shell", "write", "delete", "web", "mcp"], defaultMaxTurns: 50, defaultMaxCostUsd: 1, defaultTimeout: "30m", maxTurns: 0, maxCostUsd: 0, maxTimeout: "", maxConcurrent: 2 };

/** The settings a model can have (config.ModelSettingKeys). */
export const modelSettingKeys = ["temperature", "max_tokens", "top_p", "seed", "reasoning_effort", "thinking_budget"];

/** A model's settings, as ModelSettings holds them. */
export type ModelSettings = { temperature?: number; maxTokens?: number; topP?: number; seed?: number; reasoningEffort?: string; thinkingBudget?: number };

/** settings with key set from text ("" clears it), or why it can't be (config.ModelSettings.Set). */
export function setModelSetting(settings: ModelSettings, key: string, text: string): ModelSettings | string {
  const v = text.trim();
  const out = { ...settings };
  const float = (field: "temperature" | "topP", lo: number, hi: number, loIn: boolean) => {
    if (!v) return (delete out[field], out);
    const n = /^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/.test(v) ? Number(v) : NaN;
    if (!Number.isFinite(n) || n > hi || n < lo || (!loIn && n === lo)) return `${key} must be a number in ${loIn ? "[" : "("}${lo}, ${hi}]`;
    out[field] = n;
    return out;
  };
  const int = (field: "maxTokens" | "seed" | "thinkingBudget", lo: number) => {
    if (!v) return (delete out[field], out);
    const n = /^[+-]?\d+$/.test(v) ? Number(v) : NaN;
    if (!Number.isFinite(n) || n < lo || n > 2147483647) return `${key} must be a whole number from ${lo} to 2147483647`;
    out[field] = n;
    return out;
  };
  switch (key) {
    case "temperature":
      return float("temperature", 0, 2, true);
    case "top_p":
      return float("topP", 0, 1, false);
    case "max_tokens":
      return int("maxTokens", 1);
    case "seed":
      return int("seed", -2147483648);
    case "thinking_budget":
      return int("thinkingBudget", 0);
    case "reasoning_effort": {
      const t = v.replace(/^"|"$/g, "");
      if (!t) return (delete out.reasoningEffort, out);
      const e = parseEffort(t);
      if (!e) return `reasoning effort ${JSON.stringify(t)}: use minimal, low, medium, high, max`;
      out.reasoningEffort = e;
      return out;
    }
  }
  return `unknown setting ${JSON.stringify(key)} (known: ${modelSettingKeys.join(", ")})`;
}

/** A session setting SetSetting changes (engine's Workspace.Set): its canonical key and value, or why not and the service's reason. */
export function sessionSetting(key: string, value: string): { key: string; value: string } | { error: string; reason: string } {
  const k = key.trim().toLowerCase();
  switch (k) {
    case "effort":
    case "reasoning_effort": {
      const v = value.trim().toLowerCase();
      if (!v || v === "auto" || v === "default") return { key: "effort", value: "" };
      const e = parseEffort(v);
      return e ? { key: "effort", value: e } : { error: `reasoning effort ${JSON.stringify(value)}: use minimal, low, medium, high, max`, reason: "INVALID_SETTING" };
    }
    case "style":
    case "output_style":
      return { key: "style", value };
    case "agency":
    case "agency_level":
      return ["low", "medium", "high", "extreme"].includes(value.toLowerCase()) ? { key: "agency", value: value.toLowerCase() } : { error: "agency level must be low, medium, high or extreme", reason: "INVALID_AGENCY" };
  }
  return { error: `unknown setting ${JSON.stringify(k)}`, reason: "UNKNOWN_SETTING" };
}

/** Two JSON values alike, numbers by value (a2ui's same). */
const same = (a: unknown, b: unknown) => JSON.stringify(sorted(a)) === JSON.stringify(sorted(b));
function sorted(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(sorted);
  if (v && typeof v === "object")
    return Object.fromEntries(
      Object.keys(v)
        .sort()
        .map((k) => [k, sorted((v as Record<string, unknown>)[k])]),
    );
  return v;
}

/**
 * A human task's answer checked as the service checks it
 * (a2ui.ParseAction, engine's CheckAnswer and Form.Answer): the answer a
 * run gets, or the problems as lines ("component: message").
 */
export function checkAnswer(form: Form, taskId: string, actionJson: string, dataJson: string): { answer: Record<string, unknown> } | { problems: string[] } {
  const fail = (component: string, message: string) => ({ problems: [component ? `${component}: ${message}` : message] });
  let m: { version?: unknown; action?: { name?: string; surfaceId?: string; sourceComponentId?: string; context?: Record<string, unknown> } };
  try {
    m = JSON.parse(actionJson);
  } catch (e) {
    return fail("", `not an A2UI message: ${(e as Error).message}`);
  }
  if (m?.version !== "v0.9") return fail("", `A2UI version ${JSON.stringify(m?.version ?? "")}, not v0.9`);
  const a = m.action;
  if (!a) return fail("", "not an action message");
  if (!a.name || !a.sourceComponentId) return fail("", "an action needs its name and the component it came from");
  const context = a.context ?? {};
  if (a.surfaceId && a.surfaceId !== taskId) return fail("", `it answers ${a.surfaceId}, not this task`);
  let data: Record<string, unknown> | undefined;
  if (dataJson.trim()) {
    try {
      data = JSON.parse(dataJson);
    } catch {
      return fail("", "its data model isn't a JSON object");
    }
    if (!data || typeof data !== "object" || Array.isArray(data)) return fail("", "its data model isn't a JSON object");
  }
  const btn = form.components.find((c) => c?.id === a.sourceComponentId) as Component | undefined;
  if (!btn || btn.component !== "Button") return fail("", `${JSON.stringify(a.sourceComponentId)} isn't one of the form's buttons`);
  const event = (btn.action as { event?: { name?: string; context?: Record<string, unknown> } } | undefined)?.event;
  if (!event || event.name !== a.name) return fail(a.sourceComponentId, `its action isn't ${JSON.stringify(a.name)}`);
  const bindings = event.context ?? {};
  for (const k of Object.keys(context)) if (!(k in bindings)) return fail(a.sourceComponentId, `its action sends no ${JSON.stringify(k)}`);
  if (!data) {
    data = structuredClone(form.data ?? {});
    for (const [k, b] of Object.entries(bindings)) {
      const p = b && typeof b === "object" && typeof (b as { path?: unknown }).path === "string" ? (b as { path: string }).path : undefined;
      if (p !== undefined && k in context) data = setPath(data, p, context[k]);
    }
  }
  const problems: string[] = [];
  for (const [k, b] of Object.entries(bindings)) if (!same(resolve(b, data) ?? null, context[k] ?? null)) problems.push(`${a.sourceComponentId}: ${k} isn't what the form holds`);
  for (const [id, msgs] of formProblems(form, data)) for (const msg of msgs) problems.push(`${id}: ${msg}`);
  if (problems.length) return { problems };
  return { answer: { action: a.name, ...context } };
}

/**
 * A server stream as the page server passes it on (pkg/pageserver's
 * proxy): an empty message after each quiet spell (quietMs), so a client
 * that takes every message as the whole state is caught in ?fake too. It
 * ends when the call is aborted or the stream ends.
 */
export async function* withKeepalives<T>(src: AsyncIterable<T>, signal?: AbortSignal, quietMs = 250): AsyncGenerator<T> {
  const it = src[Symbol.asyncIterator]();
  let next = it.next();
  try {
    for (;;) {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const quiet = new Promise<"quiet">((r) => (timer = setTimeout(() => r("quiet"), quietMs)));
      const got = await Promise.race([next, quiet]);
      clearTimeout(timer);
      if (signal?.aborted) return;
      if (got === "quiet") {
        yield {} as T;
        continue;
      }
      if (got.done) return;
      yield got.value;
      next = it.next();
    }
  } finally {
    void it.return?.();
  }
}
