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

import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { KeySource, MCPServerConfigSchema, type ApiProviderSettings, type MCPServerConfig } from "../gen/blitz/v1/config_pb";
import { policyDefaults, type ModelSettings } from "./fakeChecks";
import { fakeInvalid, fakeParam, now } from "./fakeHelpers";

/** How a provider signs in, in the fake: an API key unless set. */
export interface FakeAuth {
  method: string;
  projectId: string;
  location: string;
  profile: string;
}

const configs = new Map<string, { text: string; provider: string; model: string; keys: Record<string, KeySource>; auth: Record<string, FakeAuth>; api?: ApiProviderSettings }>();

/** The models the fake custom gateway lists. */
export const fakeGatewayModels = ["claude-sonnet-5-5", "gemini-3-pro", "gpt-6-luna", "glm-5", "llama-4-maverick"];

/** Returns or initializes per-scope config settings. */
export function scopeConfig(workspace: string) {
  let c = configs.get(workspace);
  if (!c) {
    c = workspace
      ? { text: "", provider: "", model: "", keys: { gemini: KeySource.INHERITED, anthropic: KeySource.INHERITED, openai: KeySource.NONE }, auth: {} }
      : {
          text: '[llm]\nprovider = "gemini"\n\n[llm.gemini]\napi_key = "keychain:global/llm.gemini.api_key"\n\n[llm.anthropic]\napi_key = "sk-ant-plain"\n',
          provider: "gemini",
          model: "",
          // ?newuser: nothing set up yet; ?envkey: Anthropic's key in the service's environment.
          keys: fakeParam("newuser")
            ? { gemini: KeySource.NONE, anthropic: fakeParam("envkey") ? KeySource.ENVIRONMENT : KeySource.NONE, openai: KeySource.NONE }
            : { gemini: KeySource.KEYCHAIN, anthropic: fakeParam("envkey") ? KeySource.ENVIRONMENT : KeySource.PLAIN, openai: KeySource.NONE },
          auth: {},
        };
    configs.set(workspace, c);
  }
  return c;
}

const permissions = new Map<string, { rules: { effect: string; rule: string }[]; defaults: string }>([["", { rules: [{ effect: "deny", rule: "shell(git push)" }, { effect: "allow", rule: "shell(go test *)" }], defaults: "" }]]);

/** Returns or initializes per-scope permission rules. */
export function scopePermissions(workspace: string) {
  let p = permissions.get(workspace);
  if (!p) {
    p = { rules: [], defaults: "" };
    permissions.set(workspace, p);
  }
  return p;
}

/** Path shown for a scope's .env.toml file. */
export const configPath = (workspace: string) => (workspace ? `~/.blitz/workspaces/${workspace.split("/").pop()}-1a2b/.env.toml` : "~/.blitz/.env.toml");

/** Standard ConfigChange payload returned after editing config. */
export const change = (workspace: string) => ({ change: { path: configPath(workspace), modelError: "" } });

// Each workspace's [search] settings, as SetConfigValue left them.
const fakeSearchSettings = new Map<string, { enabled: boolean; sources: string[]; embeddingModel: string; enrich: boolean; enrichModel: string; enrichDailyLimit: number; includeIgnored: boolean }>();

/** Returns or initializes workspace search settings. */
export function searchSettingsOf(dir: string) {
  let s = fakeSearchSettings.get(dir);
  if (!s) fakeSearchSettings.set(dir, (s = { enabled: true, sources: ["files", "documents"], embeddingModel: "gemini/text-embedding-004", enrich: true, enrichModel: "", enrichDailyLimit: 200, includeIgnored: false }));
  return s;
}

/** Each model's own settings ([models."<name>"]), as UpdateModelSettings left them. */
export const fakeModelSettings = new Map<string, ModelSettings>([
  ["gpt-5", { temperature: 1 }],
  ["claude-opus-5-5", { reasoningEffort: "high" }],
]);

/** A model's settings as GetModelSettings gives them: its own, and the global ones that apply otherwise. */
export const fakeModelInfo = (ref: string) => {
  const model = ref.split("/").pop() ?? ref;
  return { model, provider: ref.includes("/") ? ref.split("/")[0] : "gemini", settings: { ...(fakeModelSettings.get(model) ?? {}) }, globalTemperature: 0.2, globalMaxTokens: 65536 };
};

/** The engine's note while a local model would review its own actions (spec_005 APR-14c). */
export function fakeModeNote(provider: string) {
  return provider === "ollama" ? "Auto mode is off while a model on this computer (Ollama) does the work: it would review its own actions. Blitz asks you instead. To use auto mode, set a reviewer model ([permissions.auto] model, such as gemini/gemini-3.8-flash) and open the workspace again." : "";
}

/** Media types accepted by the fake provider. */
export function fakeAccepted(provider: string) {
  const media = (kind: string, types: [string, string[]][], maxBytes: number) => ({ kind, mimeTypes: types.map(([m]) => m), extensions: types.flatMap(([, e]) => e), maxBytes: BigInt(maxBytes) });
  const images = media("image", [["image/png", [".png"]], ["image/jpeg", [".jpg", ".jpeg"]], ["image/gif", [".gif"]], ["image/webp", [".webp"]], ["image/bmp", [".bmp"]]], 20 << 20);
  const pdf = media("document", [["application/pdf", [".pdf"]]], 32 << 20);
  const text = media("text", [["text/plain", [".txt", ".md", ".csv", ".json"]]], 1 << 20);
  if (provider !== "gemini") return [images, pdf, text];
  return [
    images,
    pdf,
    text,
    media("audio", [["audio/mpeg", [".mp3"]], ["audio/wav", [".wav"]], ["audio/mp4", [".m4a"]]], 2 ** 31),
    media("video", [["video/mp4", [".mp4"]], ["video/quicktime", [".mov"]], ["video/webm", [".webm"]]], 2 ** 31),
  ];
}

/** Secret names by scope ("" global). */
export const fakeSecrets = new Map<string, string[]>([["", ["github_token", "jira_api_key"]]]);

const fakeWorkspaceMCP = new Map<string, MCPServerConfig[]>();

/** Global agents switched off per workspace (STU-15). */
export const fakeAgentsOff = new Map<string, Set<string>>();

/** Global MCP servers switched off per workspace. */
export const fakeMCPOff = new Map<string, Set<string>>();

/** Returns or initializes workspace-local MCP server configs. */
export function workspaceMCP(workspace: string): MCPServerConfig[] {
  let list = fakeWorkspaceMCP.get(workspace);
  if (!list) {
    list = [create(MCPServerConfigSchema, { name: "figma", url: "https://mcp.figma.example/mcp", autoApprove: false })];
    fakeWorkspaceMCP.set(workspace, list);
  }
  return list;
}

/** Config path shown for MCP configuration at a scope. */
export const mcpPath = (workspace: string) => (workspace ? `~/.blitz/workspaces/${workspace.split("/").pop()}-1a2b3c4d/.env.toml` : "~/.blitz/.env.toml");

/** Global MCP server definitions. */
export const fakeMCP: MCPServerConfig[] = [
  // An allow-list and a prefix, which an edit must keep (the form sends them back).
  create(MCPServerConfigSchema, { name: "github", command: "npx", args: ["-y", "@modelcontextprotocol/server-github"], env: { GITHUB_TOKEN: "ghp_…" }, tools: ["search_issues", "get_pull_request", "create_issue"], prefix: "gh" }),
  create(MCPServerConfigSchema, { name: "jira", url: "https://mcp.atlassian.example/v1/mcp", headers: { "X-Team": "core" } }),
  create(MCPServerConfigSchema, { name: "postgres", command: "postgres-mcp", args: ["--ro"], disabled: true }),
];

/** Global worker policy defaults and limits. */
export const fakePolicy = { ...policyDefaults, allow: ["shell", "write", "web"], maxConcurrent: 3, runningMaxConcurrent: 2, path: "/home/me/.blitz/.env.toml" };

/** Personality shape stored in the fake. */
export type FakePersonality = { name: string; description: string; voice: string; source: string; text: string };

const fakeBuiltinPersonalities: FakePersonality[] = [
  { name: "default", description: "Blitz's own: clear, capable, neutral", voice: "", source: "built-in", text: "" },
  { name: "calm", description: "Steady and reassuring, one step at a time", voice: "Vindemiatrix", source: "built-in", text: "Be calm and steady. Short sentences, one step at a time." },
  { name: "cheerful", description: "Upbeat and a little playful; celebrates wins", voice: "Puck", source: "built-in", text: "Be cheerful and upbeat, a little playful. Celebrate wins." },
  { name: "direct", description: "No pleasantries; the answer first, then the risk", voice: "Kore", source: "built-in", text: "Be direct. Lead with the answer, then the risk, then what to do." },
  { name: "friendly", description: "Warm and encouraging, in plain words", voice: "Achird", source: "built-in", text: "Be warm and encouraging, in plain words." },
  { name: "mentor", description: "Explains the why, one idea at a time", voice: "Sadaltager", source: "built-in", text: "Be a patient mentor: explain why, one idea at a time." },
  { name: "reviewer", description: "Constructively sceptical; asks for evidence", voice: "Charon", source: "built-in", text: "Be a constructive sceptic, like a good code reviewer." },
];

/** User-owned custom personalities. */
export const fakeOwnPersonalities = new Map<string, FakePersonality>([["coach", { name: "coach", description: "Cheers you on", voice: "Puck", source: "own", text: "Cheer the person on like a coach at the big game." }]]);

/** Mutable default personality and per-workspace personality overrides. */
export const fakePersonalityState = { defaultName: "default" };

/** User profile settings ([profile]). */
export const fakeProfile = { name: "", gender: "", pronouns: "" };

/** Per-workspace selected personality name. */
export const fakePersonalityOf = new Map<string, string>();

/** Merged built-in and user personalities list. */
export const fakePersonalities = () => [...fakeBuiltinPersonalities.filter((p) => !fakeOwnPersonalities.has(p.name)), ...[...fakeOwnPersonalities.values()].sort((a, b) => a.name.localeCompare(b.name))];

/** Looks up a personality by name or throws INVALID_SETTING. */
export const fakePersonality = (name: string) => {
  const p = fakePersonalities().find((x) => x.name === name.trim().toLowerCase());
  if (!p) throw fakeInvalid(`no personality ${JSON.stringify(name)} (/personality lists them)`, "INVALID_SETTING");
  return p;
};

/** Effective personality name for a workspace. */
export const fakePersonalityIn = (workspace: string) => (fakePersonalities().some((p) => p.name === (fakePersonalityOf.get(workspace) ?? fakePersonalityState.defaultName)) ? (fakePersonalityOf.get(workspace) ?? fakePersonalityState.defaultName) : "default");

/** Browser profiles and workspace assignments. */
export const fakeProfiles: { name: string; created: ReturnType<typeof now>; allowDomains: string[] }[] = [{ name: "work", created: now(), allowDomains: ["mail.example.com"] }];

/** Per-workspace assigned browser profile name. */
export const fakeProfileOf = new Map<string, string>();

/** Organization share submissions state. */
export const fakeSubmissions: { id: bigint; kind: string; name: string; note: string; state: string; verdicts: { check: string; score: number; passed: boolean; reason: string; findings: string[] }[]; reason: string; publishedVersion: string }[] = [
  {
    id: 1n,
    kind: "skill",
    name: "fetch",
    note: "",
    state: "rejected",
    verdicts: [{ check: "security", score: 0.1, passed: false, reason: "it calls curl, which the rubric forbids", findings: ["skills/fetch/scripts/get.sh calls the network"] }],
    reason: "security: it calls curl, which the rubric forbids",
    publishedVersion: "",
  },
];

/** Organization catalog items (agents, skills, workflows, rules, guardrails). */
export const fakeOrgItems = [
  { kind: "agent", name: "reviewer", path: "", lock: "locked", groups: [], overridden: false, decidedBy: "the organization's policy", version: "2.1.0", state: "required", on: true, description: "Reviews pull requests", locks: ["skill sql-review 2.0.0"] },
  { kind: "agent", name: "analyst", path: "", lock: "default", groups: [], overridden: false, decidedBy: "the policy for Data", version: "1.0.3", state: "available", on: false, description: "Answers questions from the warehouse", locks: [] },
  { kind: "skill", name: "sql-review", path: "", lock: "default", groups: [], overridden: false, decidedBy: "the policy for Data", version: "2.0.0", state: "available", on: true, description: "Reviews SQL for safety and cost", locks: [] },
  { kind: "workflow", name: "release-notes", path: "", lock: "default", groups: [], overridden: false, decidedBy: "the organization's policy", version: "1.2.0", state: "available", on: false, description: "Drafts release notes from merged PRs", locks: [] },
  { kind: "rule", name: "no-customer-data", path: "rules/no-customer-data.md", lock: "locked", groups: [], overridden: false, decidedBy: "the organization's policy (locked)", version: "1.0.0", state: "required", on: true, description: "", locks: [] },
  { kind: "rule", name: "house-style", path: "rules/house-style.md", lock: "locked", groups: [], overridden: false, decidedBy: "the policy for Data", version: "1.1.0", state: "required", on: true, description: "", locks: [] },
  { kind: "guardrail", name: "pii", path: "guardrails/pii.md", lock: "locked", groups: [], overridden: false, decidedBy: "the policy an admin set for you", version: "1.0.0", state: "required", on: true, description: "", locks: [] },
];

/** Organization job notice preferences. */
export const fakeNotices = { email: true, webhook: "", everyRun: false, emailAvailable: true };

/** Shared organization workspaces the user belongs to. */
export const fakeOrgWorkspaces: { handle: string; name: string; role: string; sync: boolean; dir: string; cloned: boolean }[] = [
  { handle: "plans", name: "Plans", role: "editor", sync: true, dir: "/home/me/Blitz/Acme Assist/plans", cloned: true },
  { handle: "q4-launch", name: "Q4 Launch", role: "owner", sync: true, dir: "/home/me/Blitz/Acme Assist/q4-launch", cloned: false },
];

/** Artifact URN prefix for fake service job runs. */
export const fakeServiceSet = "urn:blitz:artifacts:9b2f0c4e-1d2a-4b7e-9f00-1c2d3e4f5a6b:51aa7e10-0b6c-4c2a-8d1e-2f3a4b5c6d7e";

/** Service jobs pushed from local workspaces. */
export const fakeServicePushed = new Set<string>();

/** Primary organization service job fixture. */
export const fakeServiceJob = { id: "51aa7e10-0b6c-4c2a-8d1e-2f3a4b5c6d7e", urn: "urn:blitz:jobs:51aa7e10-0b6c-4c2a-8d1e-2f3a4b5c6d7e", name: "sales-digest", schedule: "Mondays at 06:00", enabled: true };

/** Service runs for sales-digest, newest first. */
export const fakeServiceRuns: { id: string; trigger: string; status: string; output?: string; note?: string; costUsd: number; created: ReturnType<typeof now>; branch?: string; merged?: boolean; mergeNote?: string; artifacts: string; pending?: { tool: string; kind: string; detail: string } }[] = [
  {
    id: "7a7a7a7a-7b1c-4d2e-a3f4-5b6c7d8e9f00",
    trigger: "schedule",
    status: "paused",
    output: "The numbers are ready. To publish them, I need to upload the chart to the team drive.",
    costUsd: 0.0412,
    created: timestampFromDate(new Date(Date.now() - 2 * 3600e3)),
    artifacts: `${fakeServiceSet}:7a7a7a7a-7b1c-4d2e-a3f4-5b6c7d8e9f00`,
    pending: { tool: "run_shell_command", kind: "shell", detail: "gsutil cp out/sales-by-region.png gs://acme-team/" },
  },
  {
    id: "e8030f9a-7b1c-4d2e-a3f4-5b6c7d8e9f01",
    trigger: "schedule",
    status: "succeeded",
    output: "Sales were up 4% on the week; the West led. The table and chart are in the run's files.",
    costUsd: 0.0831,
    created: timestampFromDate(new Date(Date.now() - 26 * 3600e3)),
    branch: "blitz/jobs/sales-digest/e8030f9a-7b1c-4d2e-a3f4-5b6c7d8e9f01",
    mergeNote: "its job keeps its changes on their branch (merge: never)",
    artifacts: `${fakeServiceSet}:e8030f9a-7b1c-4d2e-a3f4-5b6c7d8e9f01`,
  },
  {
    id: "0c0ffee0-7b1c-4d2e-a3f4-5b6c7d8e9f02",
    trigger: "schedule",
    status: "failed",
    note: "its owner is no longer a Runner of its workspace",
    costUsd: 0,
    created: timestampFromDate(new Date(Date.now() - 194 * 3600e3)),
    artifacts: `${fakeServiceSet}:0c0ffee0-7b1c-4d2e-a3f4-5b6c7d8e9f02`,
  },
];
