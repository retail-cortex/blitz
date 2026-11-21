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

import type { MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { AgentFileSchema, AgentScope, EvalResultSchema, WorkspaceProcessSchema, type AgentDefinition, type Connector } from "../gen/blitz/v1/workspace_pb";
import { agentProblems, guardBuiltins } from "./fakeChecks";
import { fakeParam, sleep } from "./fakeHelpers";

/** Agent files, by folder: the user's (~/.blitz/agents) and each workspace's (.agents/agents). */
export const fakeAgentFiles = new Map<string, Map<string, MessageInitShape<typeof AgentFileSchema>>>();
const builtinAgents = ["blitz", "qa", "planning-agent", "helios", "web-retriever", "model-judge", "agent-creator"];

/** Returns and lazily seeds the agent directory for a workspace/scope. */
export function agentFolder(workspace: string, scope: AgentScope): string {
  const dir = scope === AgentScope.USER ? "/Users/x/.blitz/agents" : `${workspace.replace(/\/+$/, "")}/.agents/agents`;
  if (!fakeAgentFiles.has(dir)) {
    const files = new Map<string, MessageInitShape<typeof AgentFileSchema>>();
    if (scope === AgentScope.USER) {
      files.set(`${dir}/writer.md`, {
        path: `${dir}/writer.md`,
        agent: { name: "writer", displayName: "Writer", description: "Drafts and edits prose: docs, release notes, emails", tools: ["read_file", "create_file", "edit"], agencyLevel: "medium", temperature: 0.8, prompt: "You are Writer. Write plainly.\n\n{agency_instructions}" },
      });
    } else {
      files.set(`${dir}/reviewer.md`, {
        path: `${dir}/reviewer.md`,
        agent: {
          name: "reviewer",
          displayName: "Code reviewer",
          description: "Reviews a diff for bugs, tests and readability",
          tools: ["read_file", "grep", "glob", "run_shell_command"],
          defaultModel: "anthropic/claude-sonnet-5",
          permissionMode: "plan",
          maxTurns: 30,
          effort: "high",
          maxTokens: 16384,
          prompt: "You are the reviewer. Read the change, run the tests, and report what is wrong, most serious first.\n\n{agency_instructions}",
        },
      });
      files.set(`${dir}/half-done.md`, { path: `${dir}/half-done.md`, problem: "failed to parse YAML frontmatter: yaml: line 3: mapping values are not allowed in this context" });
    }
    fakeAgentFiles.set(dir, files);
  }
  return dir;
}

/** Validates and saves an agent file in fakeAgentFiles. */
export function saveAgent(workspace: string, scope: AgentScope, previous: string, a: AgentDefinition) {
  const dir = agentFolder(workspace, scope);
  const files = fakeAgentFiles.get(dir)!;
  const problems: string[] = [];
  if (builtinAgents.includes(a.name)) problems.push(`name ${JSON.stringify(a.name)} is a built-in agent's`);
  problems.push(...agentProblems(a));
  if (a.name !== previous && [...files.values()].some((f) => f.agent?.name === a.name)) problems.push(`an agent named ${JSON.stringify(a.name)} already exists in ${dir}`);
  if (problems.length) return { problems };
  for (const [p, f] of files) if (previous && f.agent?.name === previous) files.delete(p);
  const file = { path: `${dir}/${a.name}.md`, agent: a };
  files.set(file.path, file);
  return { file };
}

/** Skill scenario fixture. */
export type FakeScenario = { name: string; description: string; prompt: string; executes: boolean; expectedSkills: string[]; threshold: number; outcome: string };

/** Studio project skill fixture. */
export type FakeSkill = { name: string; description: string; tools: string; agents: string[]; members: string[]; instructions: string; files: Record<string, string>; triggers?: string[]; scenarios?: FakeScenario[] };

/** Workspace-local skills in Studio. */
export const fakeProjectSkills: FakeSkill[] = [
  {
    name: "release-notes",
    description: "Write release notes from the git log",
    tools: "read_file run_shell_command",
    agents: [],
    members: [],
    instructions: "Read `git log` since the last tag, group the changes by what they mean for users, and write the notes in the style the reference shows.",
    triggers: ["write the release notes", "what changed since the last release"],
    scenarios: [
      { name: "notes-since-tag", description: "Notes from the log", prompt: "Write the release notes for v2.4", executes: true, expectedSkills: [], threshold: 0.8, outcome: "## What's new\n- Wishlists you can share.\n- Faster checkout." },
      { name: "not-for-bugs", description: "Bug reports aren't notes", prompt: "Why does checkout fail with a 500?", executes: false, expectedSkills: [], threshold: 0.7, outcome: "Looks at the error and the logs; no release notes." },
    ],
    files: { "references/style.md": "# Style\n\n- Start with what's new for users.\n- Short lines.\n", "scripts/count.py": '"""count: changes since a tag."""\n\nimport sys\n\nprint("3 entries since", sys.argv[1:] or ["the last tag"])\n', "scripts/tag.sh": '#!/bin/sh\n# tag: tags the release.\nset -eu\n\necho "tagging $1"\n', "references/brand.pdf": "%PDF-1.4", "references/logo.png": "" },
  },
  { name: "ship-it", description: "Cut a release: notes, tag and checks", tools: "", agents: ["reviewer"], members: ["release-notes", "pdf"], instructions: "Write the notes, then tag the release once the checks pass.", files: {} },
  { name: "tone", description: "Keep answers short and friendly", tools: "", agents: [], members: [], instructions: "Answer in two or three sentences.", files: {} },
];

/** User/plugin skills visible to Studio. */
export const fakeOtherSkills = [
  { name: "pdf", description: "Read and summarize PDF files", dir: "/home/me/.blitz/skills/pdf", kind: "simple", project: false, scripts: [], references: [], blocked: [] as string[], agents: [] as string[], members: [] as string[] },
  { name: "deploy", version: "1.2.0", description: "Deploy to staging and production", dir: "/home/me/.blitz/plugins/acme/skills/deploy", kind: "scripts", project: false, scripts: [], references: [], blocked: [] as string[], agents: [] as string[], members: [] as string[] },
];

/** Skills whose scripts have been reviewed and trusted by content hash. */
export const fakeTrusted = new Map<string, string>();

const fakeRequirements = (x: FakeSkill) =>
  (x.files["scripts/requirements.txt"] ?? x.files["requirements.txt"] ?? "")
    .split("\n")
    .map((l) => l.replace(/#.*/, "").trim())
    .filter(Boolean);

/** Builds SkillInfo summary for a FakeSkill. */
export const fakeSkillInfo = (workspace: string, x: FakeSkill) => {
  const refs = Object.keys(x.files).filter((p) => p.startsWith("references/"));
  const scripts = Object.keys(x.files)
    .filter((p) => /^scripts\/.+\.(py|ts|sh|cmd|bat)$/.test(p))
    .map((p) => ({ name: p.replace(/^scripts\/|\.(py|ts|sh|cmd|bat)$/g, ""), language: ({ py: "python", ts: "typescript", sh: "shell" } as Record<string, string>)[p.split(".").pop()!] ?? "cmd", source: p, allowed: true, reasons: [] as string[], found: true, deps: p.endsWith(".py") ? fakeRequirements(x) : ([] as string[]) }));
  const packageFiles = ["scripts/requirements.txt", "requirements.txt", "scripts/pyproject.toml", "pyproject.toml", "scripts/package.json", "package.json"].filter((f) => f in x.files);
  const kind = x.members.length ? "meta" : scripts.length ? "scripts" : refs.length ? "references" : "simple";
  const hash = "sha256:" + JSON.stringify(x.files).length;
  return { name: x.name, description: x.description, dir: `${workspace}/.agents/skills/${x.name}`, kind, project: true, references: refs, scripts, packageFiles, agents: x.agents, members: x.members, blocked: [] as string[], hash, needsReview: scripts.length > 0 && fakeTrusted.get(x.name) !== hash, triggers: x.triggers ?? [], scenarios: x.scenarios?.length ?? 0 };
};

/** Builds SkillFile message for a FakeSkill. */
export const fakeSkillFile = (workspace: string, x: FakeSkill) => ({
  skill: fakeSkillInfo(workspace, x),
  path: `${workspace}/.agents/skills/${x.name}/SKILL.md`,
  hash: "sha256:" + x.name.length,
  editable: true,
  spec: { name: x.name, description: x.description, tools: x.tools, agents: x.agents, members: x.members, instructions: x.instructions, triggers: x.triggers ?? [] },
});

/** Skill registries and installed registry skills. */
export const fakeRegistries: { name: string; url: string; hasToken: boolean }[] = [];

/** Catalog of skills offered by the fake Castor registry. */
export const fakeRegistrySkills = [
  { registry: "castor", name: "lit-review", description: "Reviews the literature on a question and keeps refs.bib in order", version: "1.2.0" },
  { registry: "castor", name: "stats-critic", description: "Checks a study's statistics: tests, power, multiple comparisons", version: "0.4.1" },
  { registry: "castor", name: "release-notes", description: "Drafts user-facing release notes from merged pull requests", version: "2.0.0" },
];

/** Skills installed from registries. */
export const fakeInstalledSkills: { name: string; registry: string; version: string; dir: string; installed: ReturnType<typeof timestampFromDate> }[] = [];

/** Studio rule fixture. */
export type FakeRule = { name: string; when: string; action: string; order: number; agents: string[]; description: string; paths: string[]; text: string };

/** Workspace rules in Studio. */
export const fakeRules: FakeRule[] = [
  { name: "clear-ask", when: "before_agent", action: "block", order: 10, agents: [], description: "Prompts say what to change", paths: [], text: "The prompt says which part of the code to change and what the result should be." },
  { name: "small-changes", when: "before_agent", action: "revise", order: 20, agents: [], description: "", paths: [], text: "The prompt asks for a focused change, not a rewrite." },
  { name: "tested", when: "after_agent", action: "revise", order: 10, agents: ["qa"], description: "Answers say how they were tested", paths: [], text: "The answer says how the change was tested." },
  { name: "go-style", when: "", action: "", order: 0, agents: [], description: "", paths: ["*.go"], text: "Run gofmt; wrap errors with %w." },
];

/** Builds the sorted ListRules response for a workspace. */
export const fakeRuleList = (workspace: string) =>
  [
    ...[...fakeRules].sort((a, b) => (a.when === b.when ? a.order - b.order || a.name.localeCompare(b.name) : (a.when === "before_agent" ? 0 : a.when === "after_agent" ? 1 : 2) - (b.when === "before_agent" ? 0 : b.when === "after_agent" ? 1 : 2))).map((r) => ({ ...r, path: `${workspace}/.agents/rules/${r.name}.md`, problem: "", editable: true })),
    { name: "commits", when: "", action: "", order: 0, agents: [], description: "", paths: [], text: "Write commit messages in the imperative.", path: `${workspace}/.claude/rules/commits.md`, problem: "", editable: false },
  ];

/** Eval case fixture. */
export type FakeCase = { id: string; name: string; prompt: string; output: string; type: string; threshold: number; scope: string };

/** Eval suite fixture. */
export type FakeEval = { name: string; description: string; agents: string[]; skills: string[]; near: string; allow: string[]; cases: FakeCase[] };

/** Eval suites in Studio. */
export const fakeEvals: FakeEval[] = [
  {
    name: "returns",
    description: "Return policy answers",
    agents: ["blitz", "reviewer"],
    skills: [],
    near: "judge",
    allow: [],
    cases: [
      { id: "window", name: "Return window", prompt: "What's the return window for an online order?", output: "30 days from delivery, with the receipt.", type: "near", threshold: 0.8, scope: "fail" },
      { id: "tone", name: "Friendly", prompt: "Can I return a jacket I wore once?", output: "Yes, within 30 days if it's unwashed, with the tags.", type: "near", threshold: 0.8, scope: "warn" },
      { id: "code", name: "", prompt: "Reply with only the return code format.", output: "RET-0000", type: "exact", threshold: 0, scope: "info" },
    ],
  },
  { name: "stock", description: "", agents: [], skills: [], near: "embedding", allow: ["mcp"], cases: [{ id: "stock", name: "Sizes", prompt: "Which sizes of the trail jacket are in stock?", output: "S, M and L.", type: "near", threshold: 0.75, scope: "fail" }] },
];

/** Completed eval runs keyed by eval name. */
export const fakeEvalRuns: Record<string, { id: string; eval: string; status: string; started: ReturnType<typeof timestampFromDate>; results: MessageInitShape<typeof EvalResultSchema>[]; costUsd: number; counts: Record<string, number> }[]> = {
  returns: [{ id: "run-1", eval: "returns", status: "passed", started: timestampFromDate(new Date(Date.now() - 26 * 3600e3)), results: [], costUsd: 0.0072, counts: { pass: 5, warn: 1 } }],
};

type FakeEvalRun = (typeof fakeEvalRuns)[string][number];

/** In-progress eval run tracked across watch/stop calls. */
export type FakeLiveEval = { run: FakeEvalRun; ended: boolean; stopped: boolean; waiters: (() => void)[] };

/** Currently running fake evals by run ID. */
export const fakeLiveEvals = new Map<string, FakeLiveEval>();

/** Returns active eval runs for an eval name. */
export const fakeLiveFor = (name: string) => [...fakeLiveEvals.values()].filter((l) => l.run.eval === name).map((l) => ({ ...l.run, results: [...l.run.results] }));

/** Builds the EvalFile message for ListEvals. */
export const fakeEvalFile = (workspace: string, e: FakeEval) => {
  const runs = fakeEvalRuns[e.name] ?? [];
  const last = fakeLiveFor(e.name)[0] ?? runs[runs.length - 1];
  return { ...e, path: `${workspace}/.agents/evals/${e.name}.md`, problem: "", lastRun: last && { ...last, results: [] } };
};

/** Starts an eval run that goes on by itself, a case every 0.7 s, until it ends or is stopped. */
export function startFakeEval(e: FakeEval): FakeLiveEval {
  const live: FakeLiveEval = { run: { id: `run-${Date.now()}`, eval: e.name, status: "running", started: timestampFromDate(new Date()), results: [], costUsd: 0, counts: {} }, ended: false, stopped: false, waiters: [] };
  const wake = () => live.waiters.splice(0).forEach((f) => f());
  fakeLiveEvals.set(live.run.id, live);
  void (async () => {
    run: for (const c of e.cases) {
      for (const agent of e.agents.length ? e.agents : ["blitz"]) {
        await new Promise((r) => setTimeout(r, 700));
        if (live.stopped) break run;
        const miss = c.id === "tone" && agent === "reviewer";
        const score = c.type === "exact" ? (miss ? 0 : 1) : miss ? 0.55 : 0.9;
        const status = score >= (c.threshold || 1) ? "pass" : c.scope;
        live.run.results.push({ case: c.id, name: c.name, agent, type: c.type, scope: c.scope, status, score, threshold: c.threshold, prompt: c.prompt, expected: c.output, output: miss ? "Returns? Sure, whenever you like!" : c.output, reason: c.type === "near" ? (miss ? "It doesn't give the 30 days." : "Same window and the receipt.") : "", refusals: c.id === "stock" ? ["create_file: notes.md"] : [], costUsd: c.type === "near" ? 0.0031 : 0.0009, duration: { seconds: BigInt(c.type === "near" ? 4 : 2), nanos: 300_000_000 } });
        wake();
      }
    }
    const results = live.run.results;
    const failed = results.some((r) => r.status === "fail" || r.status === "error");
    live.run = { ...live.run, status: live.stopped ? "stopped" : failed ? "failed" : "passed", costUsd: results.reduce((n, r) => n + (r.costUsd ?? 0), 0), counts: results.reduce((m: Record<string, number>, r) => ({ ...m, [r.status!]: (m[r.status!] ?? 0) + 1 }), {}) };
    (fakeEvalRuns[e.name] ??= []).push(live.run);
    live.ended = true;
    fakeLiveEvals.delete(live.run.id);
    wake();
  })();
  return live;
}

/** Follows a run: started with its results so far, each result after, then finished; until it ends or the caller leaves. */
export async function* followFakeEval(live: FakeLiveEval, signal: AbortSignal) {
  if (!live.ended) yield { event: { case: "started" as const, value: { ...live.run, results: [...live.run.results] } } };
  let sent = live.run.results.length;
  for (;;) {
    if (!live.ended)
      await new Promise<void>((r) => {
        live.waiters.push(r);
        signal.addEventListener("abort", () => r(), { once: true });
      });
    if (signal.aborted) return;
    while (sent < live.run.results.length && !live.ended) yield { event: { case: "result" as const, value: live.run.results[sent++] } };
    if (live.ended) {
      yield { event: { case: "finished" as const, value: live.run } };
      return;
    }
  }
}

/** Guardrail fixture. */
export type FakeGuard = { name: string; builtin: string; stage: string; action: string; agents: string[]; tools: string[]; description: string; pii: boolean; judge: boolean; off: boolean; limits?: { maxCostUsd: number; maxTurns: number; maxFilesChanged: number; allowedDomains: string[]; commands: string[] }; text: string };

/** Built-in guardrail descriptors. */
export const fakeBuiltinGuards = guardBuiltins;

/** Configured guardrails in Studio. */
export const fakeGuards: FakeGuard[] = [
  { name: "secrets", builtin: "secrets", stage: "output", action: "redact", agents: [], tools: [], description: "", pii: true, judge: false, off: false, text: "" },
  { name: "limits", builtin: "limits", stage: "action", action: "block", agents: [], tools: [], description: "", pii: false, judge: false, off: true, limits: { maxCostUsd: 2, maxTurns: 40, maxFilesChanged: 0, allowedDomains: [], commands: ["git push --force*", "rm -rf /*"] }, text: "" },
  { name: "no-branch-delete", builtin: "", stage: "action", action: "block", agents: [], tools: ["run_shell_command"], description: "Branches stay", pii: false, judge: false, off: false, text: "Deleting a git branch, locally or on the remote." },
];

/** Builds GuardrailFile response payload. */
export const fakeGuardFile = (workspace: string, g: FakeGuard) => ({ ...g, path: `${workspace}/.agents/guardrails/${g.name}.md`, problem: "", editable: true });

/** Connector fixture. */
export type FakeConnector = { name: string; category: string; scope: string; kind: string; description: string; settings: Record<string, string>; secretsSet: string[]; readOnly: boolean; tools: string[]; queries: { name: string; description: string; statement: string; params: { name: string; type: string; description: string }[] }[]; custom: boolean; text: string; trusted: boolean; enabled?: boolean };

/** Wraps an English string into a localized map. */
export const en = (s: string) => ({ en: s });
const allCaps = ["test", "try", "queries", "schema", "query-builder"];
const pgTools = [
  { id: "execute_sql", on: true, about: en("Runs any SQL the agent writes") },
  { id: "list_tables", on: true, about: en("Lists the tables and their columns") },
  { id: "list_schemas", about: en("Lists the schemas") },
  { id: "list_views", about: en("Lists the views") },
  { id: "list_indexes", about: en("Lists the indexes") },
  { id: "database_overview", about: en("Its version, size and connections") },
];
const field = (key: string, label: string, help: string, more: Record<string, unknown> = {}) => ({ key, label: en(label), help: en(help), type: "text", ...more });

/** Built-in database connector kinds. */
export const fakeConnectorKinds = [
  { id: "sqlite", category: "databases", name: en("SQLite"), hint: en("A SQLite file. sqlite-vec is built in: vec0 tables and nearest-neighbour queries work."), backend: "toolbox", capabilities: allCaps, canReadOnly: true, readOnlyHint: en("The file is opened read-only: SQLite refuses any write. Tools that write aren't offered."), queriesHint: en("SQL you write once, with parameters (? or $1…), offered as tools of their own. Safer than letting the agent write SQL."), queriesExample: "SELECT name FROM recipes WHERE cuisine = ?", origin: "builtin",
    fields: [field("database", "Database", "The file's path", { type: "path", required: true })], tools: [{ id: "execute_sql", on: true, about: en("Runs any SQL the agent writes") }, { id: "list_tables", on: true, about: en("Lists the tables and their columns") }] },
  { id: "postgres", category: "databases", name: en("PostgreSQL"), hint: en("Any PostgreSQL server, local or hosted."), backend: "toolbox", capabilities: allCaps, canReadOnly: true, readOnlyHint: en("Every session is read-only: PostgreSQL refuses any write, whatever the SQL. Tools that write aren't offered."), queriesHint: en("SQL you write once, with parameters ($1, $2…), offered as tools of their own. Safer than letting the agent write SQL."), queriesExample: "SELECT name, total FROM orders WHERE customer = $1", origin: "builtin",
    fields: [field("host", "Host", "localhost, or the server's address", { required: true, defaultValue: "localhost" }), field("port", "Port", "5432 unless it's another", { required: true, defaultValue: "5432" }), field("database", "Database", "Its name", { required: true }), field("user", "User", "A user that can only read is safest", { required: true }), field("password", "Password", "", { type: "secret", required: true })], tools: pgTools },
  { id: "bigquery", category: "databases", name: en("BigQuery"), hint: en("BigQuery on Google Cloud. Signs in with your Google credentials (gcloud auth application-default login)."), backend: "toolbox", capabilities: allCaps, canReadOnly: true, readOnlyHint: en("Writes are blocked: only queries that read run. Tools that write aren't offered."), queriesHint: en("SQL you write once, with parameters (@name), offered as tools of their own. Safer than letting the agent write SQL."), queriesExample: "SELECT sku, SUM(qty) AS sold FROM sales.orders WHERE day = @day GROUP BY sku", origin: "builtin",
    fields: [field("project", "Project", "The Google Cloud project", { required: true }), field("location", "Location", "US, EU, or a region (optional)"), field("allowedDatasets", "Datasets", "Only these, comma-separated (empty: every one)", { type: "list" }), field("maximumBytesBilled", "Most bytes billed", "A query that would bill more fails (empty: no limit)", { type: "number" })],
    tools: [{ id: "execute_sql", on: true, about: en("Runs any SQL the agent writes") }, { id: "list_datasets", on: true, about: en("Lists the datasets") }, { id: "list_tables", on: true, about: en("Lists the tables and their columns") }, { id: "table_info", on: true, about: en("A table's schema and details") }, { id: "dataset_info", about: en("A dataset's details") }] },
  { id: "alloydb-postgres", category: "databases", name: en("AlloyDB"), hint: en("AlloyDB for PostgreSQL on Google Cloud, through its connector. Signs in with your Google credentials (gcloud auth application-default login) unless you give a user and password."), backend: "toolbox", capabilities: allCaps, canReadOnly: true, readOnlyHint: en("Every session is read-only: AlloyDB refuses any write, whatever the SQL. Tools that write aren't offered."), queriesHint: en("SQL you write once, with parameters ($1, $2…), offered as tools of their own. Safer than letting the agent write SQL."), queriesExample: "SELECT name, total FROM orders WHERE customer = $1", origin: "builtin",
    fields: [field("project", "Project", "The Google Cloud project", { required: true }), field("region", "Region", "us-central1, for example", { required: true }), field("cluster", "Cluster", "The AlloyDB cluster", { required: true }), field("instance", "Instance", "The instance in it", { required: true }), field("ipType", "IP type", "public or private", { defaultValue: "public" }), field("database", "Database", "Its name", { required: true }), field("user", "User", "A user that can only read is safest"), field("password", "Password", "", { type: "secret" })], tools: pgTools },
];

/** Configured database connections across workspace and global scopes. */
export const fakeConnectors: FakeConnector[] = [
  { name: "shop", category: "databases", scope: "workspace", kind: "postgres", description: "The shop's orders and customers", settings: { host: "db.internal", port: "5432", database: "shop", user: "reader", password: "${DB_SHOP_PASSWORD}" }, secretsSet: ["password"], readOnly: true, tools: ["execute_sql", "list_tables"], queries: [{ name: "top_customers", description: "The customers who spent the most", statement: "SELECT name, sum(total) AS spent\nFROM orders JOIN customers USING (customer_id)\nGROUP BY name ORDER BY spent DESC LIMIT $1", params: [{ name: "limit", type: "integer", description: "How many" }] }, { name: "today", description: "Today's orders", statement: "SELECT id, customer, total FROM orders WHERE placed = current_date", params: [] }], custom: false, text: "", trusted: true },
  { name: "notes", category: "databases", scope: "workspace", kind: "sqlite", description: "Meeting notes, with embeddings", settings: { database: "data/notes.db" }, secretsSet: [], readOnly: false, tools: ["execute_sql", "list_tables"], queries: [], custom: false, text: "", trusted: false },
  { name: "sales", category: "databases", scope: "global", kind: "bigquery", description: "Sales across every store", settings: { project: "acme-analytics", location: "US", maximumBytesBilled: "1000000000" }, secretsSet: [], readOnly: true, tools: ["execute_sql", "list_datasets", "list_tables", "table_info"], queries: [], custom: false, text: "", trusted: true, enabled: false },
];

/** Formats a FakeConnector into a Connector proto message shape. */
export const fakeConnectorMsg = (workspace: string, d: FakeConnector) => ({ ...d, path: d.scope === "global" ? `~/.blitz/connectors/${d.category}/${d.name}.yaml` : `${workspace}/.agents/connectors/${d.category}/${d.name}.yaml`, problem: "", enabled: d.enabled !== false });

const fakeTables = [
  { name: "public.customers", about: "Everyone who ever ordered", columns: [{ name: "customer_id", type: "integer" }, { name: "name", type: "text" }, { name: "email", type: "text" }] },
  { name: "public.orders", about: "", columns: [{ name: "order_id", type: "integer" }, { name: "customer_id", type: "integer" }, { name: "total", type: "numeric" }, { name: "placed_at", type: "timestamp with time zone" }] },
];

/** Databases connector category metadata. */
export const fakeConnectorCategory = {
  id: "databases",
  name: { en: "Databases", es: "Bases de datos", "fr-CA": "Bases de données" },
  hint: en("Databases your agents can query: read-only unless you say otherwise."),
  icon: "database",
  server: "databases",
  prefix: "db",
  globalDir: "~/.blitz/connectors/databases",
  origin: "builtin",
};

/** Connector CRUD, test, schema, and query-draft helpers shared by Studio and Settings. */
export const fakeConnectorCalls = {
  save: (workspace: string, c: Connector, previousName: string, secretValues: Record<string, string>) => {
    if (!/^[a-z][a-z0-9_]{0,39}$/.test(c.name)) return { problems: [`name "${c.name}": lower case letters, digits and _, starting with a letter`] };
    const i = fakeConnectors.findIndex((d) => d.name === (previousName || c.name) && d.scope === c.scope && d.category === c.category);
    if (!previousName && i >= 0) return { problems: [`there's a connection named "${c.name}" already`] };
    const secretsSet = [...new Set([...(i >= 0 ? fakeConnectors[i].secretsSet : []), ...Object.keys(secretValues)])];
    if (c.kind === "postgres" && !secretsSet.includes("password")) return { problems: ["password is required"] };
    const next: FakeConnector = { name: c.name, category: c.category, scope: c.scope, kind: c.kind, description: c.description, settings: { ...c.settings }, secretsSet, readOnly: c.readOnly, tools: c.tools, queries: c.queries.map((q) => ({ name: q.name, description: q.description, statement: q.statement, params: q.params.map((p) => ({ ...p })) })), custom: c.custom, text: c.text, trusted: true, enabled: i >= 0 ? fakeConnectors[i].enabled : true };
    if (i >= 0) fakeConnectors[i] = next;
    else fakeConnectors.push(next);
    return { connection: fakeConnectorMsg(workspace, next) };
  },
  test: async (c: Connector) => {
    await new Promise((r) => setTimeout(r, 700));
    if (c.settings.host === "nowhere") return { problem: `failed to connect to host=nowhere user=${c.settings.user}: dial tcp: lookup nowhere: no such host` };
    const kind = fakeConnectorKinds.find((k) => k.id === c.kind);
    const tools = [...c.tools.filter((id) => !(c.readOnly && kind?.tools.find((t) => t.id === id && "writes" in t && t.writes))).map((id) => ({ name: `${c.name}_${id}`, description: `${id.replace(/_/g, " ")} on ${c.name}.` })), ...c.queries.map((q) => ({ name: `${c.name}_${q.name}`, description: q.description }))];
    return { tools: tools.map((t) => ({ ...t, readOnly: c.readOnly })) };
  },
  tryTool: async (c: Connector, tool: string) => {
    await new Promise((r) => setTimeout(r, 500));
    if (c.settings.host === "nowhere") return { problem: `failed to connect to host=nowhere user=${c.settings.user}: dial tcp: lookup nowhere: no such host` };
    return { output: tool.endsWith("top_customers") ? '{"name":"Ada Lovelace","spent":1820.5}\n{"name":"Grace Hopper","spent":1544}' : '{"n":42}' };
  },
  readSchema: async (c: Connector) => {
    await new Promise((r) => setTimeout(r, 500));
    if (c.settings.host === "nowhere") return { problem: "no tables in shop: failed to connect to host=nowhere" };
    return { tables: fakeTables };
  },
  draft: async (request: string) => {
    await new Promise((r) => setTimeout(r, 900));
    if (/refund/i.test(request)) return { tables: fakeTables.length, problem: "No table holds refunds: orders has a total, but nothing says one was refunded." };
    return {
      tables: fakeTables.length,
      notes: ['"Spent" is the sum of orders.total.', '"Last month" is the calendar month before today.'],
      query: {
        name: "top_customers_last_month",
        description: 'The customers who spent the most last month, with what they spent. Use it for "best customers" questions.',
        statement: "SELECT c.name, sum(o.total) AS spent\nFROM public.orders o JOIN public.customers c USING (customer_id)\nWHERE o.placed_at >= date_trunc('month', now()) - interval '1 month'\n  AND o.placed_at < date_trunc('month', now())\nGROUP BY c.name\nORDER BY spent DESC\nLIMIT $1",
        params: [{ name: "limit", type: "integer", description: "How many customers" }],
        readsOnly: true,
      },
    };
  },
};

/** Simulated background shell processes (?fake&processes). */
export const fakeProcesses: MessageInitShape<typeof WorkspaceProcessSchema>[] = fakeParam("processes")
  ? [
      { workspace: "/home/me/shop", process: { id: 3, command: "pnpm dev --port 5173 --host 127.0.0.1", running: true, runtimeMs: 2_520_000n, sessionId: "s-1", cpuPercent: 97.2, memoryBytes: 431_000_000n, measured: true, flags: ["cpu"] } },
      { workspace: "/home/me/shop", process: { id: 1, command: "python3 -m http.server 8000 --directory build/site", running: true, runtimeMs: 11_040_000n, sessionId: "s-0", cpuPercent: 0.1, memoryBytes: 19_400_000n, measured: true, flags: ["left"], idleMs: 7_500_000n } },
      { workspace: "/home/me/notes", process: { id: 2, command: "go test ./... -run TestSoak -count=200", running: true, runtimeMs: 312_000n, sessionId: "s-9", cpuPercent: 38.5, memoryBytes: 96_000_000n, measured: true } },
    ]
  : [];

/** Simulated output log for a background process. */
export const fakeProcessOutput = Array.from({ length: 60 }, (_, i) => `[vite] ${String(10 + Math.floor(i / 6)).padStart(2, "0")}:${String((i * 7) % 60).padStart(2, "0")} hmr update /src/cart/${["Totals", "Coupon", "LineItem"][i % 3]}.tsx`).join("\n") + "\n";

/** Streams updates whenever the fake background process list changes. */
export async function* watchFakeProcesses(workspace: string, signal?: AbortSignal) {
  let last = "";
  while (!signal?.aborted) {
    const processes = fakeProcesses.filter((x) => !workspace || x.workspace === workspace);
    const sig = processes.map((x) => `${x.workspace}#${x.process?.id}`).join("|");
    if (sig !== last) {
      last = sig;
      yield { processes, listed: true };
    }
    await sleep(500);
  }
}
