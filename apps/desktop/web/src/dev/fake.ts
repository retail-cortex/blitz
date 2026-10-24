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

// A fake Blitz service for working on the page without one (?fake in a
// development build). It keeps a little state in memory and scripts turns
// so every state of the conversation can be seen: streamed Markdown,
// thinking, tool calls, a task list, an approval and a plan review.
import { clone, create, type JsonObject, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { setTransport } from "../api";
import { BackgroundRunSchema, MessageSchema, RunTurnResponseSchema, SessionInfoSchema, SessionService, WatchBackgroundResponseSchema, type BackgroundRun, type Message, type SessionInfo } from "../gen/blitz/v1/session_pb";
import { ActionKind, Decision, ErrorInfoSchema, UsageSchema, type Usage } from "../gen/blitz/v1/turn_pb";

type Out = MessageInitShape<typeof RunTurnResponseSchema>;
import { RunStatus, WorkerService, WorkerState } from "../gen/blitz/v1/worker_pb";
import { ConfigService, KeySource, MCPServerConfigSchema, type MCPServerConfig } from "../gen/blitz/v1/config_pb";
import { FileKind, FileService, GitAction, RevisionDiff } from "../gen/blitz/v1/file_pb";
import { HealthCheckProgressSchema, HealthVerdictSchema, WorkspaceHealthSchema, type HealthItem, type WorkspaceHealth } from "../gen/blitz/v1/workspace_pb";
import { AgentFileSchema, AgentScope, EvalResultSchema, ConflictChoice, GitOperation, PullOutcome, SuggestionKind, SuggestionSchema, WorkflowRunEventSchema, WorkflowRunSchema, WorkflowRunStepSchema, WorkspaceService, type AgentDefinition, type Connector, type WorkflowRun } from "../gen/blitz/v1/workspace_pb";

// Whether the fake workspace is a git repository (?norepo: not yet).
// The providers the fake has signed in to, and each one's sign-in.
const fakeSignedIn = new Set<string>();
const fakeTools: Record<string, string> = { google: "gcloud", anthropic: "ant", aws: "aws", azure: "az" };
function fakeSignIn(provider: string) {
  const found = provider === "google" || provider === "aws";
  return {
    provider,
    tool: fakeTools[provider],
    toolFound: found,
    install: "https://example.com/install",
    signedIn: fakeSignedIn.has(provider),
    detail: fakeSignedIn.has(provider) ? "you@example.com (authorized_user)" : found ? "no Application Default Credentials" : "",
  };
}

// Whether the fake commit dialog has files staged (after "Stage all changes").
let fakeStaged = false;
let fakeRepo = !new URLSearchParams(location.search).has("norepo");
// The branch and its remote: 2 commits to pull, 1 to push; pulling stops
// on a conflict in discount.go (RemoteSync.tsx). ?fake&nopush: no upstream yet.
const fakeRemote = {
  upstream: new URLSearchParams(location.search).has("nopush") ? "" : "origin/fix/coupon-rounding",
  ahead: 1,
  behind: 2,
  operation: GitOperation.UNSPECIFIED,
  conflicts: [] as string[],
  merging: "",
};
// The branches (Branches.tsx): switching changes the branch the status shows;
// merging "spike/lazy-totals" stops on a conflict, any other branch fast-forwards.
let fakeBranch = "fix/coupon-rounding";
const fakeBranches = [
  { name: "fix/coupon-rounding", subject: "Round the total, not each line", upstream: "origin/fix/coupon-rounding" },
  { name: "main", subject: "Release 1.4.0", upstream: "origin/main" },
  { name: "spike/lazy-totals", subject: "Compute totals on read", upstream: "" },
  { name: "origin/feature/gift-cards", subject: "Gift card balance lookup", upstream: "", remote: true },
];
/** A failed-precondition error with the service's reason, as the real one sends. */
function fakeError(msg: string, reason: string): ConnectError {
  const err = new ConnectError(msg, Code.FailedPrecondition);
  err.details.push({ desc: ErrorInfoSchema, value: { reason } });
  return err;
}
const fakeGitStatus = () =>
  fakeRepo ? { git: true, repo: true, branch: fakeBranch, changed: 2, remote: true, ...fakeRemote, conflicts: [...fakeRemote.conflicts] } : { git: true, repo: false };

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const now = () => timestampFromDate(new Date());

// Each workspace's [search] settings, as SetConfigValue left them.
const fakeSearchSettings = new Map<string, { enabled: boolean; sources: string[]; embeddingModel: string; enrich: boolean; enrichModel: string; enrichDailyLimit: number; includeIgnored: boolean }>();
function searchSettingsOf(dir: string) {
  let s = fakeSearchSettings.get(dir);
  if (!s) fakeSearchSettings.set(dir, (s = { enabled: true, sources: ["files", "documents"], embeddingModel: "gemini/text-embedding-004", enrich: true, enrichModel: "", enrichDailyLimit: 200, includeIgnored: false }));
  return s;
}

// A long document the search finds a passage deep in, for the preview's
// marks (files/find.ts).
const fakePricing = [
  "# Pricing",
  "",
  "How prices, discounts and taxes work, for support staff. Totals are rounded once, at the end.",
  "",
  ...Array.from({ length: 24 }, (_, i) => [`## Discount rule ${i + 1}`, "", `A coupon of kind ${i + 1} takes its percentage off the items it names, never off shipping.`, ""]).flat(),
  "## Rounding",
  "",
  "The cart total is rounded half to even, per line.",
  "",
  "## Taxes",
  "",
  "Taxes are added after discounts.",
  "",
].join("\n");
const pricingRounding = fakePricing.split("\n").indexOf("## Rounding") + 1;

// What the fake workspace's search finds (SearchWorkspace), by source.
const fakeSearchCorpus = [
  { source: "files", ref: "internal/cart/cart.go", title: "internal/cart/cart.go", line: 42, section: "", snippet: "// Total rounds each line to cents before summing.\nfunc (c *Cart) Total() Money {\n\treturn c.sum(math.Round)", score: 0.9, summary: "The shopping cart: lines, totals and rounding to cents.", tags: ["checkout", "money", "rounding"] },
  { source: "files", ref: "internal/cart/cart_test.go", title: "internal/cart/cart_test.go", line: 18, section: "", snippet: "func TestCartTotalRounds(t *testing.T) {\n\tc := New()\n\tc.Add(Item{Price: 0.105, Qty: 3})", score: 0.7 },
  { source: "files", ref: "docs/pricing.md", title: "docs/pricing.md", line: pricingRounding, section: "", snippet: "## Rounding\n\nThe cart total is rounded half to even, per line.", score: 0.6, summary: "How prices, discounts and rounding work, for support staff.", tags: ["pricing", "discounts", "rounding"] },
  { source: "documents", ref: "specs/payments.pdf", title: "specs/payments.pdf", line: 88, section: "page 4", snippet: "Totals must be rounded per line item, not on the sum,\nto match the invoice.", score: 0.5 },
  { source: "chats", ref: "session-2", title: "Fix the cart rounding bug", line: 3, section: "", snippet: "The cart total is off by a cent when three items are rounded.", score: 0.4 },
  { source: "notes", ref: ".blitz/plans/session-2-1.md", title: ".blitz/plans/session-2-1.md", line: 1, section: "", snippet: "Plan: round each cart line before summing; add a test.", score: 0.3 },
];

const msg = (role: string, text: string, kind = ""): Message => create(MessageSchema, { role, text, kind, time: now() });

const intro = `Here's how **shop** is organised:

| Directory | What's there |
|---|---|
| \`cmd/shop\` | The server's \`main\`: flags, config, graceful shutdown |
| \`internal/cart\` | Cart pricing, with the discount rules |
| \`internal/store\` | Postgres access through \`sqlc\` |

Build and test with:

\`\`\`sh
make build
go test ./...
\`\`\`

The discount code lives in [\`internal/cart/discount.go\`](https://github.com/example/shop). Two things stand out:

1. \`ApplyCoupon\` rounds *before* summing line items, so totals can be off by a cent.
2. There's no test for expired coupons.`;

interface State {
  sessions: SessionInfo[];
  active: string;
  settings: { agent: string; model: string; provider: string; effort: string; mode: string; agency: string; locale: string; style: string; speechModel?: string };
  pending: Map<string, (answer: string) => void>;
  usage: Usage;
  // The project settings' trust state: the "shop" workspace has some.
  projectTrust?: string;
  // Background tasks, by ID: they finish a few seconds after they start.
  tasks: Map<string, { agent: string; prompt: string; sessionId: string; started: number; stopped?: number; answered?: number }>;
}

const states = new Map<string, State>();

// Agent files, by folder: the user's (~/.blitz/agents) and each
// workspace's (.agents/agents); one doesn't parse.
const fakeAgentFiles = new Map<string, Map<string, MessageInitShape<typeof AgentFileSchema>>>();
const builtinAgents = ["blitz", "qa", "planning-agent", "helios", "web-retriever", "model-judge", "agent-creator"];
function agentFolder(workspace: string, scope: AgentScope): string {
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
function saveAgent(workspace: string, scope: AgentScope, previous: string, a: AgentDefinition) {
  const dir = agentFolder(workspace, scope);
  const files = fakeAgentFiles.get(dir)!;
  const problems: string[] = [];
  if (!/^[a-z0-9][a-z0-9_-]*$/.test(a.name)) problems.push(`name ${JSON.stringify(a.name)}: lowercase letters, digits, - and _`);
  if (builtinAgents.includes(a.name)) problems.push(`agent name ${JSON.stringify(a.name)} is reserved by a built-in agent`);
  if (a.name !== previous && [...files.values()].some((f) => f.agent?.name === a.name)) problems.push(`an agent named ${JSON.stringify(a.name)} already exists in ${dir}`);
  if (problems.length) return { problems };
  for (const [p, f] of files) if (previous && f.agent?.name === previous) files.delete(p);
  const file = { path: `${dir}/${a.name}.md`, agent: a };
  files.set(file.path, file);
  return { file };
}

// The days the fake service has a log for (DeleteLogDay removes them).
let fakeLogDays = ["2026-09-28", "2026-09-27"];

function state(dir: string): State {
  let s = states.get(dir);
  if (!s) {
    const first = create(SessionInfoSchema, {
      id: "session-1",
      title: dir.endsWith("shop") ? "Understand the cart code" : "",
      workspace: dir,
      created: now(),
      updated: now(),
      messages: dir.endsWith("shop") ? [msg("user", "Explain how this project is organised, and how to build and test it."), msg("model", intro)] : [],
    });
    first.messageCount = first.messages.length;
    const other = create(SessionInfoSchema, { id: "session-0", title: "Fix the flaky checkout test", workspace: dir, messageCount: 14, updated: now() });
    s = {
      sessions: [
        first,
        other,
        create(SessionInfoSchema, { id: "session-w1", title: "⏰ nightly-deps 2026-10-04 02:00", origin: "worker", agent: "blitz", workspace: dir, messageCount: 6, updated: now() }),
        create(SessionInfoSchema, { id: "session-w2", title: "⏰ nightly-deps 2026-10-03 02:00", origin: "worker", agent: "blitz", workspace: dir, messageCount: 4, updated: now() }),
        create(SessionInfoSchema, { id: "session-1b", title: "Add the cart API docs", workspace: dir, messageCount: 9, updated: now() }),
      ],
      active: first.id,
      settings: { agent: "blitz", model: "gemini-4-argon", provider: "gemini", effort: "", mode: "auto", agency: "high", locale: "en-US", style: "default" },
      pending: new Map(),
      tasks: new Map(),
      usage: create(UsageSchema, { calls: 3, input: 18234n, output: 2210n, lastPrompt: 12876n, costUsd: 0.0123, priced: true }),
    };
    states.set(dir, s);
  }
  return s;
}

const active = (s: State) => s.sessions.find((x) => x.id === s.active)!;

// A background task as the service reports it: after 3 s it asks to run
// the tests and waits; 3 s after the answer it's done (or stopped).
function fakeTask(s: State, id: string) {
  const t = s.tasks.get(id)!;
  const end = t.stopped ?? (t.answered && Date.now() - t.answered > 3000 ? t.answered + 3000 : 0);
  const state = t.stopped ? "stopped" : end ? "done" : !t.answered && Date.now() - t.started > 3000 ? "waiting" : "running";
  const ms = (end || Date.now()) - t.started;
  return {
    id,
    agent: t.agent,
    prompt: t.prompt,
    sessionId: t.sessionId,
    state,
    started: timestampFromDate(new Date(t.started)),
    ended: end ? timestampFromDate(new Date(end)) : undefined,
    result: state === "done" ? "No other rounding bugs: `SplitTotal` and `Refund` already work in cents." : "",
    error: t.stopped ? "stopped" : "",
    usage: create(UsageSchema, { calls: Math.ceil(ms / 2000), costUsd: 0.0009 * Math.ceil(ms / 2000), priced: true }),
  };
}

// The shop workspace's .blitz/settings.toml: a deny rule, and a hook, an
// MCP server and an allow rule waiting for trust (spec_project_config_031).
function fakeProject(s: State) {
  if (!s.sessions[0].workspace.endsWith("shop")) return { state: "none", files: [], applied: [], pending: [], ignored: [], problems: [] };
  const trust = s.projectTrust ?? "new";
  const f = ".blitz/settings.toml";
  return {
    files: [f],
    state: trust,
    hash: "sha256:fake",
    loaded: trust === "trusted",
    applied: [{ file: f, kind: "deny", value: "shell(rm -rf *)" }],
    pending: [
      { file: f, kind: "hook", key: "pre_tool", value: "./scripts/check.sh" },
      { file: f, kind: "mcp", key: "db", value: "npx @acme/db-mcp" },
      { file: f, kind: "allow", value: "shell(make test)" },
    ],
    ignored: [{ file: f, kind: "setting", key: "llm.openai.base_url", reason: "never" }],
    problems: [],
  };
}

// The welcome tiles (GetSuggestions): ideas are "being written" for the
// first 4 s after a workspace first asks; ?noharness offers the setup,
// ?stale the update of the skills and agents, ?reflect and ?review the
// reflection's tiles.
const suggestionsAsked = new Map<string, number>();
// The session's goal (/goal), the same for every workspace.
let fakeGoal: { condition: string; continues: number; max: number; last: string } | undefined;
function fakeSuggestions(dir: string) {
  if (!suggestionsAsked.has(dir)) suggestionsAsked.set(dir, Date.now());
  const pending = Date.now() - suggestionsAsked.get(dir)! < 4000;
  const last = state(dir).sessions.find((x) => x.messages.length > 0);
  const out: MessageInitShape<typeof SuggestionSchema>[] = [];
  if (new URLSearchParams(location.search).has("stale")) out.push({ kind: SuggestionKind.HARNESS_UPDATE });
  if (new URLSearchParams(location.search).has("reflect")) out.push({ kind: SuggestionKind.REFLECT, count: 7 });
  if (new URLSearchParams(location.search).has("review")) out.push({ kind: SuggestionKind.REFLECT_REVIEW, count: 3 });
  if (last) out.push({ kind: SuggestionKind.CONTINUE, title: last.title, sessionId: last.id });
  out.push({ kind: SuggestionKind.CHANGES, count: Object.keys(fakeGit).length });
  if (!pending) {
    out.push(
      { kind: SuggestionKind.IDEA, title: "Add tests for the coupon rounding fix", prompt: "Add table-driven tests for ApplyCoupon's rounding, including the cases from the last bug." },
      { kind: SuggestionKind.IDEA, title: "Wire the discount rules into checkout", prompt: "Use the discount rules in internal/cart from the checkout handler, and test it." },
    );
  }
  return { suggestions: out, harnessMissing: new URLSearchParams(location.search).has("noharness"), pending };
}

let nextRequest = 0;

async function* runTurn(dir: string, text: string, plan: boolean): AsyncGenerator<Out> {
  const s = state(dir);
  const sess = active(s);
  sess.messages.push(msg("user", plan ? `/plan ${text}` : text));
  if (!sess.title) sess.title = text.slice(0, 50);
  const ev = (kind: unknown, author = "blitz") => ({ event: { author, kind } }) as Out;
  const before = create(UsageSchema, s.usage);
  yield ev({ case: "accepted", value: {} }, "");
  await sleep(300);
  if (/choose/i.test(text)) {
    // A multi-select question (ask_user_question with multi_select).
    const id = `q${++nextRequest}`;
    const answer = new Promise<string>((r) => s.pending.set(id, r));
    yield ev({
      case: "question",
      value: { requestId: id, question: "Which packages should I check for the rounding bug?", options: ["internal/cart", "internal/store", "cmd/shop"], multiSelect: true },
    });
    const a = await answer;
    const text = `Checking ${a.split("\n").join(", ")}.`;
    sess.messages.push(msg("model", text));
    yield ev({ case: "text", value: { text } });
    yield ev({ case: "finished", value: { before, after: s.usage } });
    return;
  }
  if (/reconnect/i.test(text)) {
    // The model's connection drops and is retried (a transient notice, a
    // snackbar), then the agent asks with answers too long for one line.
    yield ev({ case: "notice", value: { text: "Lost the connection to the model (unexpected EOF). Reconnecting…", transient: true } });
    await sleep(1200);
    const id = `q${++nextRequest}`;
    const answer = new Promise<string>((r) => s.pending.set(id, r));
    yield ev({
      case: "question",
      value: {
        requestId: id,
        question: "How should recipe version history behave?",
        options: [
          "Store full snapshots of Recipes only (title, ingredients, steps, tags, allergens, calories) on every update, with ability to view diffs and restore a previous version",
          "Store snapshots across Recipes, Recipe Books, and Shopping Lists on every update",
          "View-only version history for Recipes (audit log of changes over time, without restore)",
        ],
      },
    });
    const text = `Going with: ${await answer}`;
    sess.messages.push(msg("model", text));
    yield ev({ case: "text", value: { text } });
    yield ev({ case: "finished", value: { before, after: s.usage } });
    return;
  }
  if (/burst/i.test(text)) {
    // Parallel tool calls that each need approval: their requests arrive
    // together, and the turn ends only once every one is answered.
    const files = ["AGENTS.md", "CLAUDE.md", "GEMINI.md"];
    const ids = files.map(() => `a${++nextRequest}`);
    const answers = ids.map((id) => new Promise<string>((r) => s.pending.set(id, r)));
    for (const [i, path] of files.entries()) yield ev({ case: "toolCall", value: { id: `p${i}`, name: "create_file", args: { path } } });
    for (const [i, path] of files.entries()) {
      yield ev({ case: "approvalRequest", value: { requestId: ids[i], tool: "create_file", kind: ActionKind.WRITE, detail: `create ${path}` } });
    }
    const got = await Promise.all(answers);
    for (const [i, path] of files.entries()) {
      yield ev({ case: "toolResult", value: { id: `p${i}`, name: "create_file", result: got[i] === "deny" ? { error: "the user denied it" } : { success: true, path } } });
    }
    const text = "Created the agent instruction files.";
    sess.messages.push(msg("model", text));
    yield ev({ case: "text", value: { text } });
    yield ev({ case: "finished", value: { before, after: s.usage } });
    return;
  }
  for (const part of ["The user wants the rounding fixed. ", "I'll read the discount code first, then ", "write a failing test."]) {
    yield ev({ case: "text", value: { text: part, partial: true, thought: true } });
    await sleep(120);
  }
  yield ev({ case: "text", value: { text: "", repeat: true, thought: true } });
  const tasks = (done: number) => ({
    case: "tasks" as const,
    value: {
      items: ["Read the discount code", "Write a failing test for rounding", "Fix the rounding", "Run the tests"].map((content, i) => ({
        content,
        status: i < done ? "done" : i === done ? "in_progress" : "pending",
      })),
    },
  });
  yield ev(tasks(0));
  const call = (id: string, name: string, args: JsonObject) => ev({ case: "toolCall", value: { id, name, args } });
  const result = (id: string, name: string, res: JsonObject) => ev({ case: "toolResult", value: { id, name, result: res } });
  yield call("1", "read_file", { path: "internal/cart/discount.go" });
  await sleep(250);
  yield result("1", "read_file", { content: "package cart\n…", lines: 84 });
  yield call("2", "grep", { pattern: "ApplyCoupon", path: "internal" });
  await sleep(200);
  yield result("2", "grep", { matches: 3 });
  const taskId = `task-${s.tasks.size + 1}`;
  s.tasks.set(taskId, { agent: "qa", prompt: "Review the cart package for other rounding bugs", sessionId: sess.id, started: Date.now() });
  yield call("2b", "invoke_agent", { agent_name: "qa", prompt: "Review the cart package for other rounding bugs", background: true });
  yield result("2b", "invoke_agent", { agent_name: "qa", task_id: taskId, status: "running", response: `Started in the background as ${taskId}.` });
  yield ev(tasks(1));

  if (plan || s.settings.mode === "plan") {
    const id = `q${++nextRequest}`;
    const answer = new Promise<string>((r) => s.pending.set(id, r));
    yield ev({
      case: "question",
      value: {
        requestId: id,
        question:
          "The agent's plan:\n\n## Fix coupon rounding\n\n1. Add `TestApplyCouponRounding` in `internal/cart/discount_test.go` with three line items that round differently.\n2. Change `ApplyCoupon` to sum in cents, then apply the discount once.\n3. Run `go test ./internal/cart/...`.\n\n**Risk:** stored totals in old orders keep the old rounding.\n\nCarry it out? Or type what to change.",
        options: ["Yes, carry it out", "Yes, and accept its file edits without asking", "No, keep planning"],
      },
    });
    const a = await answer;
    if (a === "No, keep planning") {
      yield ev({ case: "text", value: { text: "Understood: I'll wait for your next message." } });
      yield ev({ case: "finished", value: { before, after: s.usage } });
      return;
    }
    yield ev({ case: "text", value: { text: "Plan approved.\n\n" } });
  }

  const id = `a${++nextRequest}`;
  const decision = new Promise<string>((r) => s.pending.set(id, r));
  yield call("3", "create_file", { path: "internal/cart/discount_test.go" });
  yield ev({
    case: "approvalRequest",
    value: {
      requestId: id,
      tool: "create_file",
      kind: ActionKind.WRITE,
      detail: "create internal/cart/discount_test.go",
      scopeLabel: "edits to internal/cart",
      diff: "--- /dev/null\n+++ b/internal/cart/discount_test.go\n@@ -0,0 +1,14 @@\n+package cart\n+\n+import \"testing\"\n+\n+func TestApplyCouponRounding(t *testing.T) {\n+\titems := []Item{{Cents: 333}, {Cents: 333}, {Cents: 334}}\n+\tgot := ApplyCoupon(items, Coupon{Percent: 15})\n+\tif got != 850 {\n+\t\tt.Fatalf(\"total %d, want 850\", got)\n+\t}\n+}\n",
    },
  });
  const d = await decision;
  if (d === "deny") {
    yield result("3", "create_file", { error: "the user denied it" });
    yield ev({ case: "text", value: { text: "OK, I won't create the test. Tell me how you'd like to proceed." } });
  } else {
    yield result("3", "create_file", { success: true });
    yield ev(tasks(2));
    yield call("4", "replace_in_file", { path: "internal/cart/discount.go" });
    await sleep(250);
    yield result("4", "replace_in_file", { success: true });
    yield ev(tasks(3));
    yield call("5", "run_shell_command", { command: "go test ./internal/cart/..." });
    await sleep(500);
    yield result("5", "run_shell_command", { exit_code: 0, output: "ok  shop/internal/cart 0.21s" });
    yield ev(tasks(4));
    const answer = "Fixed. `ApplyCoupon` now sums line items in cents and applies the discount once, so totals no longer drift by a cent.\n\n- **Changed:** `internal/cart/discount.go`\n- **Added:** `TestApplyCouponRounding`\n- **Verified:** `go test ./internal/cart/...` passes.";
    for (const word of answer.split(/(?<= )/)) {
      yield ev({ case: "text", value: { text: word, partial: true } });
      await sleep(15);
    }
    yield ev({ case: "text", value: { text: answer, repeat: true } });
    sess.messages.push(msg("model", answer));
  }
  s.usage = create(UsageSchema, { ...s.usage, calls: s.usage.calls + 4, input: s.usage.input + 9120n, output: s.usage.output + 812n, lastPrompt: 14210n, costUsd: s.usage.costUsd + 0.0061 });
  sess.messageCount = sess.messages.length;
  yield ev({ case: "finished", value: { before, after: s.usage } });
}

function notFound(what: string): never {
  const err = new ConnectError(`${what} not found`, Code.NotFound);
  err.details.push({ desc: ErrorInfoSchema, value: { reason: "NOT_FOUND" } });
  throw err;
}

// The settings: per scope ("" global), the file and where each key is.
/** How a provider signs in, in the fake: an API key unless set. */
interface FakeAuth {
  method: string;
  projectId: string;
  location: string;
  profile: string;
}

const configs = new Map<string, { text: string; provider: string; model: string; keys: Record<string, KeySource>; auth: Record<string, FakeAuth> }>();

function scopeConfig(workspace: string) {
  let c = configs.get(workspace);
  if (!c) {
    c = workspace
      ? { text: "", provider: "", model: "", keys: { gemini: KeySource.INHERITED, anthropic: KeySource.INHERITED, openai: KeySource.NONE }, auth: {} }
      : {
          text: '[llm]\nprovider = "gemini"\n\n[llm.gemini]\napi_key = "keychain:global/llm.gemini.api_key"\n\n[llm.anthropic]\napi_key = "sk-ant-plain"\n',
          provider: "gemini",
          model: "",
          keys: { gemini: KeySource.KEYCHAIN, anthropic: KeySource.PLAIN, openai: KeySource.NONE },
          auth: {},
        };
    configs.set(workspace, c);
  }
  return c;
}
/** Each scope's permission rules, in the fake. */
const permissions = new Map<string, { rules: { effect: string; rule: string }[]; defaults: string }>([["", { rules: [{ effect: "deny", rule: "shell(git push)" }, { effect: "allow", rule: "shell(go test *)" }], defaults: "" }]]);
function scopePermissions(workspace: string) {
  let p = permissions.get(workspace);
  if (!p) {
    p = { rules: [], defaults: "" };
    permissions.set(workspace, p);
  }
  return p;
}
const configPath = (workspace: string) => (workspace ? `~/.blitz/workspaces/${workspace.split("/").pop()}-1a2b/.env.toml` : "~/.blitz/.env.toml");
const change = (workspace: string) => ({ change: { path: configPath(workspace), modelError: "" } });

// The workspace's files: a small Go project, some of it changed.
// Workers made with the New worker dialog.
// The workspace's workers, and each one's WORKER.md as written (spec).
type FakeWorker = Record<string, unknown> & { name: string; hash: string; state: WorkerState; path: string; spec: Record<string, unknown> & { schedule: string } };
const fakeSpec = (name: string, o: Record<string, unknown> = {}) => ({ name, description: "", schedule: "", timezone: "", agent: "", model: "", permissions: [] as string[], maxTurns: 0, maxCostUsd: 0, timeout: "", catchUp: "", prompt: "", ...o });
// The home workspace the global workers run in (~/.blitz/home), and theirs.
const fakeHome = "/home/me/.blitz/home";
const fakeGlobal: FakeWorker[] = [
  { name: "daily-news", description: "Summarises the morning's news in ~/notes/news.md.", state: WorkerState.ENABLED, schedule: "Daily at 7 AM", cron: "0 7 * * *", timezone: "America/Chicago", hash: "77ab", path: "/home/me/.blitz/workers/daily-news/WORKER.md", limits: { maxTurns: 10, maxCostUsd: 0.2 }, spec: fakeSpec("daily-news", { schedule: "Daily at 7 AM", timezone: "America/Chicago", prompt: "Summarise the morning's news." }) },
];
// Other workspaces with workers on record (Settings › Workers): one not
// in the app's workspaces, one whose folder is gone.
const fakeElsewhere = [
  { workspace: "/home/me/notes", home: false, missing: false, problem: "", workers: [{ workspace: "/home/me/notes", name: "weekly-notes", state: WorkerState.ENABLED, schedule: "Fridays at 17:00", problems: [], permissions: [] }] },
  { workspace: "/home/me/old-site", home: false, missing: true, problem: "", workers: [{ workspace: "/home/me/old-site", name: "backup", state: WorkerState.ENABLED, problems: [], permissions: [] }] },
];
// The global limits on every worker ([workers.policy]); the service still
// runs two at once until it restarts.
const fakePolicy = { allow: ["shell", "write", "web"], defaultMaxTurns: 50, defaultMaxCostUsd: 1, defaultTimeout: "30m", maxTurns: 200, maxCostUsd: 10, maxTimeout: "2h", maxConcurrent: 3, runningMaxConcurrent: 2, path: "/home/me/.blitz/.env.toml" };
// Studio's skills: the workspace's own, with their files, and those it
// sees elsewhere (yours, a plugin's, built in) to copy or reference.
type FakeSkill = { name: string; description: string; tools: string; agents: string[]; members: string[]; instructions: string; files: Record<string, string>; triggers?: string[]; scenarios?: FakeScenario[] };
// A skill's scenarios (SK-111): Castor's behaviour tests.
type FakeScenario = { name: string; description: string; prompt: string; executes: boolean; expectedSkills: string[]; threshold: number; outcome: string };
const fakeProjectSkills: FakeSkill[] = [
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
const fakeOtherSkills = [
  { name: "pdf", description: "Read and summarize PDF files", dir: "/home/me/.blitz/skills/pdf", kind: "simple", project: false, scripts: [], references: [], blocked: [] as string[], agents: [] as string[], members: [] as string[] },
  { name: "deploy", version: "1.2.0", description: "Deploy to staging and production", dir: "/home/me/.blitz/plugins/acme/skills/deploy", kind: "scripts", project: false, scripts: [], references: [], blocked: [] as string[], agents: [] as string[], members: [] as string[] },
];
const fakeSkillInfo = (workspace: string, x: FakeSkill) => {
  const refs = Object.keys(x.files).filter((p) => p.startsWith("references/"));
  const scripts = Object.keys(x.files)
    .filter((p) => /^scripts\/.+\.(py|ts|sh|cmd|bat)$/.test(p))
    .map((p) => ({ name: p.replace(/^scripts\/|\.(py|ts|sh|cmd|bat)$/g, ""), language: ({ py: "python", ts: "typescript", sh: "shell" } as Record<string, string>)[p.split(".").pop()!] ?? "cmd", source: p, allowed: true, reasons: [] as string[], found: true, deps: p.endsWith(".py") ? fakeRequirements(x) : ([] as string[]) }));
  const packageFiles = ["scripts/requirements.txt", "requirements.txt", "scripts/pyproject.toml", "pyproject.toml", "scripts/package.json", "package.json"].filter((f) => f in x.files);
  const kind = x.members.length ? "meta" : scripts.length ? "scripts" : refs.length ? "references" : "simple";
  // A skill's scripts wait for review until trusted at their content (SK-108).
  const hash = "sha256:" + JSON.stringify(x.files).length;
  return { name: x.name, description: x.description, dir: `${workspace}/.agents/skills/${x.name}`, kind, project: true, references: refs, scripts, packageFiles, agents: x.agents, members: x.members, blocked: [] as string[], hash, needsReview: scripts.length > 0 && fakeTrusted.get(x.name) !== hash, triggers: x.triggers ?? [], scenarios: x.scenarios?.length ?? 0 };
};
// A skill's Python packages: its requirements.txt's lines.
const fakeRequirements = (x: FakeSkill) =>
  (x.files["scripts/requirements.txt"] ?? x.files["requirements.txt"] ?? "")
    .split("\n")
    .map((l) => l.replace(/#.*/, "").trim())
    .filter(Boolean);
// The skills whose scripts were trusted, at which content.
const fakeTrusted = new Map<string, string>();
const fakeSkillFile = (workspace: string, x: FakeSkill) => ({
  skill: fakeSkillInfo(workspace, x),
  path: `${workspace}/.agents/skills/${x.name}/SKILL.md`,
  hash: "sha256:" + x.name.length,
  editable: true,
  spec: { name: x.name, description: x.description, tools: x.tools, agents: x.agents, members: x.members, instructions: x.instructions, triggers: x.triggers ?? [] },
});
// Studio's rules: two gates before the agent, one after, an instruction
// rule, and one from .claude/rules (read-only there).
type FakeRule = { name: string; when: string; action: string; order: number; agents: string[]; description: string; paths: string[]; text: string };
const fakeRules: FakeRule[] = [
  { name: "clear-ask", when: "before_agent", action: "block", order: 10, agents: [], description: "Prompts say what to change", paths: [], text: "The prompt says which part of the code to change and what the result should be." },
  { name: "small-changes", when: "before_agent", action: "revise", order: 20, agents: [], description: "", paths: [], text: "The prompt asks for a focused change, not a rewrite." },
  { name: "tested", when: "after_agent", action: "revise", order: 10, agents: ["qa"], description: "Answers say how they were tested", paths: [], text: "The answer says how the change was tested." },
  { name: "go-style", when: "", action: "", order: 0, agents: [], description: "", paths: ["*.go"], text: "Run gofmt; wrap errors with %w." },
];
const fakeRuleList = (workspace: string) =>
  [
    ...[...fakeRules].sort((a, b) => (a.when === b.when ? a.order - b.order || a.name.localeCompare(b.name) : (a.when === "before_agent" ? 0 : a.when === "after_agent" ? 1 : 2) - (b.when === "before_agent" ? 0 : b.when === "after_agent" ? 1 : 2))).map((r) => ({ ...r, path: `${workspace}/.agents/rules/${r.name}.md`, problem: "", editable: true })),
    { name: "commits", when: "", action: "", order: 0, agents: [], description: "", paths: [], text: "Write commit messages in the imperative.", path: `${workspace}/.claude/rules/commits.md`, problem: "", editable: false },
  ];
// Studio's evals: returns answers (one agent misses a case) and stock
// lookups, never run.
type FakeCase = { id: string; name: string; prompt: string; output: string; type: string; threshold: number; scope: string };
type FakeEval = { name: string; description: string; agents: string[]; skills: string[]; near: string; allow: string[]; cases: FakeCase[] };
const fakeEvals: FakeEval[] = [
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
const fakeEvalRuns: Record<string, { id: string; eval: string; status: string; started: ReturnType<typeof timestampFromDate>; results: MessageInitShape<typeof EvalResultSchema>[]; costUsd: number; counts: Record<string, number> }[]> = {
  returns: [{ id: "run-1", eval: "returns", status: "passed", started: timestampFromDate(new Date(Date.now() - 26 * 3600e3)), results: [], costUsd: 0.0072, counts: { pass: 5, warn: 1 } }],
};
type FakeEvalRun = (typeof fakeEvalRuns)[string][number];
// Eval runs going on, by ID: they go on in the "service" when the page leaves (EV-09a).
type FakeLiveEval = { run: FakeEvalRun; ended: boolean; stopped: boolean; waiters: (() => void)[] };
const fakeLiveEvals = new Map<string, FakeLiveEval>();
const fakeLiveFor = (name: string) => [...fakeLiveEvals.values()].filter((l) => l.run.eval === name).map((l) => ({ ...l.run, results: [...l.run.results] }));
const fakeEvalFile = (workspace: string, e: FakeEval) => {
  const runs = fakeEvalRuns[e.name] ?? [];
  const last = fakeLiveFor(e.name)[0] ?? runs[runs.length - 1];
  return { ...e, path: `${workspace}/.agents/evals/${e.name}.md`, problem: "", lastRun: last && { ...last, results: [] } };
};
/** Starts an eval run that goes on by itself, a case every 0.7 s, until it ends or is stopped. */
function startFakeEval(e: FakeEval): FakeLiveEval {
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
async function* followFakeEval(live: FakeLiveEval, signal: AbortSignal) {
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

// Studio's guardrails: prompt injection with its defaults, secrets tuned,
// limits switched off, and one of the workspace's own.
type FakeGuard = { name: string; builtin: string; stage: string; action: string; agents: string[]; tools: string[]; description: string; pii: boolean; judge: boolean; off: boolean; limits?: { maxCostUsd: number; maxTurns: number; maxFilesChanged: number; allowedDomains: string[]; commands: string[] }; text: string };
const fakeBuiltinGuards = [
  { id: "prompt_injection", stage: "tool_result", defaultAction: "warn", actions: ["warn", "block"] },
  { id: "secrets", stage: "output", defaultAction: "redact", actions: ["redact", "warn", "block"] },
  { id: "limits", stage: "action", defaultAction: "block", actions: ["block"] },
];
const fakeGuards: FakeGuard[] = [
  { name: "secrets", builtin: "secrets", stage: "output", action: "redact", agents: [], tools: [], description: "", pii: true, judge: false, off: false, text: "" },
  { name: "limits", builtin: "limits", stage: "action", action: "block", agents: [], tools: [], description: "", pii: false, judge: false, off: true, limits: { maxCostUsd: 2, maxTurns: 40, maxFilesChanged: 0, allowedDomains: [], commands: ["git push --force*", "rm -rf /*"] }, text: "" },
  { name: "no-branch-delete", builtin: "", stage: "action", action: "block", agents: [], tools: ["run_shell_command"], description: "Branches stay", pii: false, judge: false, off: false, text: "Deleting a git branch, locally or on the remote." },
];
const fakeGuardFile = (workspace: string, g: FakeGuard) => ({ ...g, path: `${workspace}/.agents/guardrails/${g.name}.md`, problem: "", editable: true });
// Studio's connectors (spec_connectors_047): the databases category and
// its four kinds (English only: the real descriptors carry Spanish and
// French); a read-only Postgres with a query of its own, a SQLite file
// that came with the workspace (not trusted yet), and BigQuery for every
// workspace.
type FakeConnector = { name: string; category: string; scope: string; kind: string; description: string; settings: Record<string, string>; secretsSet: string[]; readOnly: boolean; tools: string[]; queries: { name: string; description: string; statement: string; params: { name: string; type: string; description: string }[] }[]; custom: boolean; text: string; trusted: boolean; enabled?: boolean };
const en = (s: string) => ({ en: s });
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
const fakeConnectorKinds = [
  { id: "alloydb-postgres", category: "databases", name: en("AlloyDB"), hint: en("AlloyDB for PostgreSQL on Google Cloud, through its connector. Signs in with your Google credentials (gcloud auth application-default login) unless you give a user and password."), backend: "toolbox", capabilities: allCaps, canReadOnly: true, readOnlyHint: en("Every session is read-only: AlloyDB refuses any write, whatever the SQL. Tools that write aren't offered."), queriesHint: en("SQL you write once, with parameters ($1, $2…), offered as tools of their own. Safer than letting the agent write SQL."), queriesExample: "SELECT name, total FROM orders WHERE customer = $1", placeholders: "$1", origin: "builtin",
    fields: [field("project", "Project", "The Google Cloud project", { required: true }), field("region", "Region", "us-central1, for example", { required: true }), field("cluster", "Cluster", "The AlloyDB cluster", { required: true }), field("instance", "Instance", "The instance in it", { required: true }), field("ipType", "IP type", "public or private", { defaultValue: "public" }), field("database", "Database", "Its name", { required: true }), field("user", "User", "A user that can only read is safest"), field("password", "Password", "", { type: "secret" })], tools: pgTools },
  { id: "bigquery", category: "databases", name: en("BigQuery"), hint: en("BigQuery on Google Cloud. Signs in with your Google credentials (gcloud auth application-default login)."), backend: "toolbox", capabilities: allCaps, canReadOnly: true, readOnlyHint: en("Writes are blocked: only queries that read run. Tools that write aren't offered."), queriesHint: en("SQL you write once, with parameters (@name), offered as tools of their own. Safer than letting the agent write SQL."), queriesExample: "SELECT sku, SUM(qty) AS sold FROM sales.orders WHERE day = @day GROUP BY sku", placeholders: "@name", origin: "builtin",
    fields: [field("project", "Project", "The Google Cloud project", { required: true }), field("location", "Location", "US, EU, or a region (optional)"), field("allowedDatasets", "Datasets", "Only these, comma-separated (empty: every one)", { type: "list" }), field("maximumBytesBilled", "Most bytes billed", "A query that would bill more fails (empty: no limit)", { type: "number" })],
    tools: [{ id: "execute_sql", on: true, about: en("Runs any SQL the agent writes") }, { id: "list_datasets", on: true, about: en("Lists the datasets") }, { id: "list_tables", on: true, about: en("Lists the tables and their columns") }, { id: "table_info", on: true, about: en("A table's schema and details") }, { id: "dataset_info", about: en("A dataset's details") }] },
  { id: "postgres", category: "databases", name: en("PostgreSQL"), hint: en("Any PostgreSQL server, local or hosted."), backend: "toolbox", capabilities: allCaps, canReadOnly: true, readOnlyHint: en("Every session is read-only: PostgreSQL refuses any write, whatever the SQL. Tools that write aren't offered."), queriesHint: en("SQL you write once, with parameters ($1, $2…), offered as tools of their own. Safer than letting the agent write SQL."), queriesExample: "SELECT name, total FROM orders WHERE customer = $1", placeholders: "$1", origin: "builtin",
    fields: [field("host", "Host", "localhost, or the server's address", { required: true, defaultValue: "localhost" }), field("port", "Port", "5432 unless it's another", { required: true, defaultValue: "5432" }), field("database", "Database", "Its name", { required: true }), field("user", "User", "A user that can only read is safest", { required: true }), field("password", "Password", "", { type: "secret", required: true })], tools: pgTools },
  { id: "sqlite", category: "databases", name: en("SQLite"), hint: en("A SQLite file. sqlite-vec is built in: vec0 tables and nearest-neighbour queries work."), backend: "toolbox", capabilities: allCaps, canReadOnly: true, readOnlyHint: en("The file is opened read-only: SQLite refuses any write. Tools that write aren't offered."), queriesHint: en("SQL you write once, with parameters (? or $1…), offered as tools of their own. Safer than letting the agent write SQL."), queriesExample: "SELECT name FROM recipes WHERE cuisine = ?", placeholders: "$1", origin: "builtin",
    fields: [field("database", "Database", "The file's path", { type: "path", required: true })], tools: [{ id: "execute_sql", writes: true, on: true, about: en("Runs any SQL the agent writes") }, { id: "list_tables", on: true, about: en("Lists the tables and their columns") }] },
];
const fakeConnectors: FakeConnector[] = [
  { name: "shop", category: "databases", scope: "workspace", kind: "postgres", description: "The shop's orders and customers", settings: { host: "db.internal", port: "5432", database: "shop", user: "reader", password: "${DB_SHOP_PASSWORD}" }, secretsSet: ["password"], readOnly: true, tools: ["execute_sql", "list_tables"], queries: [{ name: "top_customers", description: "The customers who spent the most", statement: "SELECT name, sum(total) AS spent\nFROM orders JOIN customers USING (customer_id)\nGROUP BY name ORDER BY spent DESC LIMIT $1", params: [{ name: "limit", type: "integer", description: "How many" }] }], custom: false, text: "", trusted: true },
  { name: "notes", category: "databases", scope: "workspace", kind: "sqlite", description: "Meeting notes, with embeddings", settings: { database: "data/notes.db" }, secretsSet: [], readOnly: false, tools: ["execute_sql", "list_tables"], queries: [], custom: false, text: "", trusted: false },
  { name: "sales", category: "databases", scope: "global", kind: "bigquery", description: "Sales across every store", settings: { project: "acme-analytics", location: "US", maximumBytesBilled: "1000000000" }, secretsSet: [], readOnly: true, tools: ["execute_sql", "list_datasets", "list_tables", "table_info"], queries: [], custom: false, text: "", trusted: true, enabled: false },
];
const fakeConnectorMsg = (workspace: string, d: FakeConnector) => ({ ...d, path: d.scope === "global" ? `~/.blitz/connectors/${d.category}/${d.name}.yaml` : `${workspace}/.agents/connectors/${d.category}/${d.name}.yaml`, problem: "", enabled: d.enabled !== false });
const fakeTables = [
  { name: "public.customers", about: "Everyone who ever ordered", columns: [{ name: "customer_id", type: "integer" }, { name: "name", type: "text" }, { name: "email", type: "text" }] },
  { name: "public.orders", about: "", columns: [{ name: "order_id", type: "integer" }, { name: "customer_id", type: "integer" }, { name: "total", type: "numeric" }, { name: "placed_at", type: "timestamp with time zone" }] },
];
// The Databases category, as both Studio and Settings list it.
const fakeConnectorCategory = {
  id: "databases",
  name: { en: "Databases", es: "Bases de datos", "fr-CA": "Bases de données" },
  hint: en("Databases your agents can query, served inside Blitz by MCP Toolbox for Databases. Read-only unless you say otherwise."),
  icon: "database",
  server: "databases",
  prefix: "db",
  globalDir: "~/.blitz/connectors/databases",
  origin: "builtin",
};
// The connector calls Studio and Settings share: the workspace's
// connections and the global ones (spec_connectors_047 CON-18).
const fakeConnectorCalls = {
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
      notes: ["\"Spent\" is the sum of orders.total.", "\"Last month\" is the calendar month before today."],
      query: {
        name: "top_customers_last_month",
        description: "The customers who spent the most last month, with what they spent. Use it for \"best customers\" questions.",
        statement: "SELECT c.name, sum(o.total) AS spent\nFROM public.orders o JOIN public.customers c USING (customer_id)\nWHERE o.placed_at >= date_trunc('month', now()) - interval '1 month'\n  AND o.placed_at < date_trunc('month', now())\nGROUP BY c.name\nORDER BY spent DESC\nLIMIT $1",
        params: [{ name: "limit", type: "integer", description: "How many customers" }],
        readsOnly: true,
      },
    };
  },
};
// Worker runs going now (?fake&noruns: none).
const fakeLive: { id: string; workspace: string; worker: string; status: RunStatus; started: ReturnType<typeof now>; manual: boolean; sessionId: string; error?: { reason: string; message: string } }[] = new URLSearchParams(location.search).has("noruns")
  ? []
  : [
      { id: "live-1", workspace: "/home/me/shop", worker: "nightly-deps", status: RunStatus.RUNNING, started: now(), manual: false, sessionId: "worker-nightly-3" },
      { id: "live-2", workspace: fakeHome, worker: "daily-news", status: RunStatus.RUNNING, started: now(), manual: true, sessionId: "worker-news-1" },
    ];
const fakeWorkers: FakeWorker[] = [
  {
    name: "nightly-deps",
    description: "Checks for outdated dependencies every night and opens a summary.",
    state: WorkerState.ENABLED,
    schedule: "every day at 02:00",
    cron: "0 2 * * *",
    timezone: "America/Chicago",
    nextRun: timestampFromDate(new Date(Date.now() + 5 * 3600e3)),
    permissions: ["shell(go list -m -u all)"],
    limits: { maxTurns: 20, maxCostUsd: 0.5, timeout: { seconds: 900n } },
    hash: "9f2c41ab",
    path: "workers/nightly-deps/WORKER.md",
    // Last night's run failed: the workspace is badged until its workers are seen.
    lastRun: { id: "run-9", worker: "nightly-deps", status: RunStatus.FAILED, started: timestampFromDate(new Date(Date.now() - 8 * 3600e3)) },
    spec: fakeSpec("nightly-deps", {
      description: "Checks for outdated dependencies every night and opens a summary.",
      schedule: "every day at 02:00",
      timezone: "America/Chicago",
      permissions: ["shell:go list -m -u all", "write:reports/*"],
      maxTurns: 20,
      maxCostUsd: 0.5,
      timeout: "15m",
      prompt: "List outdated Go modules with `go list -m -u all` and write reports/deps.md.",
    }),
  },
  { name: "weekly-report", state: WorkerState.NEW, schedule: "Mondays at 09:00", cron: "0 9 * * 1", timezone: "America/Chicago", hash: "11aa", path: "workers/weekly-report/WORKER.md", limits: { maxTurns: 10, maxCostUsd: 0.2 }, spec: fakeSpec("weekly-report", { schedule: "Mondays at 09:00", timezone: "America/Chicago", maxTurns: 10, maxCostUsd: 0.2, prompt: "Summarize last week's commits in reports/week.md." }) },
  {
    name: "broken-digest",
    state: WorkerState.INVALID,
    schedule: "whenever",
    timezone: "",
    hash: "77bb",
    path: "workers/broken-digest/WORKER.md",
    problems: ['schedule "whenever": use a cron expression, a descriptor or plain text like "Daily at 6 AM"'],
    spec: fakeSpec("broken-digest", { schedule: "whenever", prompt: "Write a digest of open issues." }),
  },
];

// What a model of provider takes, as the service says (runtime.AcceptedMedia).
function fakeAccepted(provider: string) {
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

// A sales table for the table viewer (ReadTable reads it below).
const fakeSales = ["region,product,units,price,date", ...Array.from({ length: 240 }, (_, i) => `${["north", "south", "east", "west"][i % 4]},${["coupon", "cart", "gift card"][i % 3]},${(i * 7) % 50},${((i * 13) % 997) / 10},2026-0${1 + (i % 9)}-1${i % 10}`)].join("\n") + "\n";

// Files being uploaded (dropped from the computer), by upload id.
const fakeUploads = new Map<string, string>();
let fakeUploadSeq = 0;

const fakeFiles = new Map<string, string>([
  ["go.mod", "module example.com/shop\n\ngo 1.27\n"],
  ["data/sales.csv", fakeSales],
  ["docs/pricing.md", fakePricing],
  // A page with its own stylesheet and script, previewed in a sandboxed frame.
  ["site/index.html", '<!doctype html>\n<html>\n<head>\n  <title>Shop</title>\n  <link rel="stylesheet" href="site.css">\n</head>\n<body>\n  <h1>The shop</h1>\n  <p id="when">Loading…</p>\n  <script src="app.js"></script>\n</body>\n</html>\n'],
  ["site/site.css", "body { font-family: Georgia, serif; margin: 40px; color: #234; }\nh1 { color: #b04a1a; border-bottom: 2px solid #b04a1a; }\n"],
  ["site/app.js", 'document.getElementById("when").textContent = "Script ran: " + (2 + 2);\n'],
  [
    "README.md",
    "# Shop\n\nA small shop server. Run it with `go run ./cmd/shop`. See [the design](docs/design.md#requests), [main.go](cmd/shop/main.go#L10), [the cart package](internal/cart/) and [Usage](#usage).\n\n## Usage\n\n| Path | What |\n| --- | --- |\n| `/cart` | the cart |\n\n- [x] discounts\n- [ ] checkout\n\n```mermaid\nflowchart LR\n  client[Browser] --> cart[\"/cart handler\"] --> store[(SQLite)]\n```\n",
  ],
  ["docs/design.md", "# Design\n\nBack to [the README](../README.md).\n\n## Requests\n\n```mermaid\nsequenceDiagram\n  Browser->>Shop: POST /cart\n  Shop->>Store: ListItems\n  Store-->>Shop: items\n  Shop-->>Browser: total\n```\n\n> Coupons apply once, rounded in cents.\n"],
  ["docs/logo.svg", '<svg xmlns="http://www.w3.org/2000/svg" width="120" height="120"><circle cx="60" cy="60" r="50" fill="#1a73e8"/></svg>\n'],
  ["docs/shot.png", "\u0000png"],
  ["docs/spec.pdf", "\u0000pdf"],
  ["docs/overview.wav", "\u0000wav"],
  ["cmd/shop/main.go", 'package main\n\nimport (\n\t"log"\n\t"net/http"\n\n\t"example.com/shop/internal/cart"\n)\n\nfunc main() {\n\thttp.HandleFunc("/cart", cart.Handler)\n\tlog.Fatal(http.ListenAndServe(":8080", nil))\n}\n'],
  [
    "internal/cart/discount.go",
    "package cart\n\n// ApplyCoupon returns the total of items with the coupon's percentage off,\n// rounded once, in cents.\nfunc ApplyCoupon(items []Item, c Coupon) int {\n\ttotal := 0\n\tfor _, it := range items {\n\t\ttotal += it.Cents\n\t}\n\treturn round(total * (100 - c.Percent) / 100)\n}\n\nfunc round(cents int) int {\n\tif cents < 0 {\n\t\treturn 0\n\t}\n\treturn cents\n}\n",
  ],
  ["internal/cart/discount_test.go", "package cart\n\nimport \"testing\"\n\nfunc TestApplyCouponRounding(t *testing.T) {\n}\n"],
  ["internal/cart/cart.go", "package cart\n\ntype Item struct {\n\tName  string\n\tCents int\n}\n\ntype Coupon struct{ Percent int }\n"],
  ["internal/store/queries.sql", "-- name: ListItems :many\nSELECT id, name, cents FROM items ORDER BY name;\n"],
  ["web/app.ts", "export function total(items: { cents: number }[]): number {\n  return items.reduce((a, b) => a + b.cents, 0);\n}\n"],
  [".env", "STRIPE_KEY=sk_test_fake\n"],
  [".gitignore", "bin/\n*.log\n"],
  ["AGENTS.md", "# Shop\n\nRun `go test ./...` before committing.\n"],
  ["bin/shop", "\u0000binary"],
]);
const fakeGit: Record<string, string> = { "internal/cart/discount.go": "modified", "internal/cart/discount_test.go": "untracked", "web/app.ts": "added" };
// Skill registries in the fake: Castor's offer, and what was installed.
// The user's secrets' names, by scope ("" global).
const fakeSecrets = new Map<string, string[]>([["", ["github_token", "jira_api_key"]]]);

// Settings › MCP servers: the global servers, edited in place.
const fakeMCP: MCPServerConfig[] = [
  create(MCPServerConfigSchema, { name: "github", command: "npx", args: ["-y", "@modelcontextprotocol/server-github"], env: { GITHUB_TOKEN: "ghp_…" } }),
  create(MCPServerConfigSchema, { name: "jira", url: "https://mcp.atlassian.example/v1/mcp", headers: { "X-Team": "core" } }),
  create(MCPServerConfigSchema, { name: "postgres", command: "postgres-mcp", args: ["--ro"], disabled: true }),
];

const fakeRegistries: { name: string; url: string; hasToken: boolean }[] = [];
const fakeRegistrySkills = [
  { registry: "castor", name: "lit-review", description: "Reviews the literature on a question and keeps refs.bib in order", version: "1.2.0", castorId: "sk_lit" },
  { registry: "castor", name: "stats-critic", description: "Checks a study's statistics: tests, power, multiple comparisons", version: "0.4.1", castorId: "sk_stats" },
  { registry: "castor", name: "release-notes", description: "Drafts user-facing release notes from merged pull requests", version: "2.0.0", castorId: "sk_notes" },
];
const fakeInstalledSkills: { name: string; registry: string; version: string; dir: string; installed: ReturnType<typeof now> }[] = [];
// What .gitignore ignores in the fake: bin/, *.log, and the lines Add to
// .gitignore wrote.
const fakeIgnored = (p: string) =>
  p.startsWith("bin") || p.endsWith(".log") || (fakeFiles.get(".gitignore") ?? "").split("\n").some((l) => l === `/${p}` || l === `/${p}/`);
// A history in the fake: six commits, with a branch merged in.
const fakeRevisions = (p: string) => {
  const h = (n: number) => n.toString(16).padStart(2, "0").repeat(20);
  const day = (d: number, hours = 0) => timestampFromDate(new Date(Date.now() - d * 86400000 - hours * 3600000));
  const rows = [
    { n: 6, parents: [5, 4], subject: "Merge branch 'coupon-rounding'", author: "Ana Ruiz", email: "ana@example.com", when: day(0, 2), added: 0, removed: 0 },
    { n: 5, parents: [3], subject: "Read the coupon from the request", author: "Sam Lee", email: "sam@example.com", when: day(0, 5), added: 14, removed: 3 },
    { n: 4, parents: [3], subject: "Round coupon discounts half up", author: "Ana Ruiz", email: "ana@example.com", when: day(1), added: 6, removed: 2 },
    { n: 3, parents: [2], subject: "Add the cart's tests", author: "Kim Park", email: "kim@example.com", when: day(4), added: 120, removed: 0 },
    { n: 2, parents: [1], subject: "Price items in cents", author: "Sam Lee", email: "sam@example.com", when: day(12), added: 40, removed: 38 },
    { n: 1, parents: [], subject: `Add ${p.split("/").pop() || "the shop"}`, author: "Ana Ruiz", email: "ana@example.com", when: day(40), added: 210, removed: 0 },
  ];
  return rows.map((r) => ({ commit: h(r.n), short: h(r.n).slice(0, 7), parents: r.parents.map(h), subject: r.subject, author: r.author, email: r.email, when: r.when, path: p, added: r.added, removed: r.removed, files: r.added + r.removed ? 2 : 0 }));
};
const fakeOldText = (text: string) => text.split("\n").slice(0, 3).join("\n") + "\n";
const fakeInstructionFiles = ["AGENTS.md", "CLAUDE.md", "GEMINI.md", "BLITZ.md"];
const fakeHidden = (p: string) => {
  const name = p.split("/").pop()!;
  return p === ".env" ? "blocked" : name.startsWith(".") ? "dot" : fakeInstructionFiles.includes(name) ? "agent" : fakeIgnored(p) ? "ignored" : "";
};
const fakeVersion = (text: string) => String(text.length) + ":" + [...text].reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7).toString(16);

// Background runs (blitz --bg), made on first use in the first workspace
// open: one waits for an approval, one runs, one is done.
let fakeRuns: BackgroundRun[] | undefined;
const runStopped = new Set<string>();
function backgroundRuns(): BackgroundRun[] {
  if (fakeRuns) return fakeRuns;
  const dir = [...states.keys()][0];
  if (!dir) return []; // made once a workspace is open
  const s = state(dir);
  const ago = (min: number) => timestampFromDate(new Date(Date.now() - min * 60000));
  const run = (id: string, prompt: string, st: string, started: number, extra: Partial<Pick<BackgroundRun, "waiting" | "costUsd" | "ended">> = {}) => {
    const sess = create(SessionInfoSchema, { id: `session-${id}`, title: prompt, workspace: dir, created: ago(started), updated: now(), messages: [msg("user", prompt)] });
    sess.messageCount = 1;
    s.sessions.push(sess);
    return Object.assign(create(BackgroundRunSchema, { id, workspace: dir, sessionId: sess.id, prompt, state: st, started: ago(started) }), extra);
  };
  fakeRuns = [
    run("bg-3", "Deploy the staging build and check the health endpoint", "waiting", 4, { waiting: 1, costUsd: 0.21 }),
    run("bg-2", "Upgrade the Go modules and fix what breaks", "running", 12, { costUsd: 0.48 }),
    run("bg-1", "Write the release notes for 0.4", "done", 95, { ended: ago(80), costUsd: 0.07 }),
  ];
  return fakeRuns;
}

// Studio's workflows: a research brief, and a draft not finished yet.
type FakeFlow = {
  name: string;
  description: string;
  nodes: { id: string; type: string; title: string; x: number; y: number; agent: string; skills: string[]; instructions: string; rules: string[]; skill: string; script: string; args: string[]; path: string; fields?: { name: string; type: string; description: string }[]; url?: string; method?: string; server?: string; tool?: string; argumentsJson?: string; headers?: { name: string; value: string }[]; auth?: { type: string; token?: string } }[];
  edges: { from: string; to: string; when?: string; if?: string; default?: boolean; error?: boolean; label?: string }[];
  mode?: string;
};
const flowNode = (id: string, type: string, title: string, x: number, y: number, extra: Partial<FakeFlow["nodes"][number]> = {}) => ({ id, type, title, x, y, agent: "", skills: [], instructions: "", rules: [], skill: "", script: "", args: [], path: "", ...extra });
const fakeFlows: FakeFlow[] = [
  {
    name: "research-brief",
    description: "A one-page brief from the notes and the web",
    nodes: [
      flowNode("notes", "read_file", "Read the notes", 0, 0, { path: "notes/coupons.md" }),
      flowNode("look-up", "browser", "Look it up", 0, 160, { instructions: "Find how other shops round coupon discounts." }),
      flowNode("gather", "accumulate", "Gather", 280, 80),
      flowNode("draft", "agent", "Draft the brief", 560, 80, { agent: "reviewer", instructions: "Write a one-page brief: the key points first, sources cited.", rules: ["tested"] }),
      flowNode("save", "save_file", "Save the brief", 840, 80, { path: "briefs/coupons.md" }),
    ],
    edges: [
      { from: "notes", to: "gather" },
      { from: "look-up", to: "gather" },
      { from: "gather", to: "draft" },
      { from: "draft", to: "save" },
    ],
  },
  {
    name: "refund-triage",
    description: "Big refunds to a person, the rest straight through",
    nodes: [
      flowNode("ticket", "read_file", "Read the ticket", 0, 80, { path: "tickets/latest.md" }),
      flowNode("pull", "extract", "Pull out the refund", 260, 80, { fields: [{ name: "amount", type: "number", description: "" }, { name: "order", type: "string", description: "" }] }),
      flowNode("size", "exclusive", "How big?", 520, 80),
      flowNode("review", "agent", "Review it", 720, 0, { agent: "reviewer", instructions: "Check order {{ steps.pull.data.order }} before refunding {{ steps.pull.data.amount }}." }),
      flowNode("refund", "agent", "Refund it", 720, 160, { instructions: "Refund order {{ steps.pull.data.order }}." }),
      flowNode("log", "save_file", "Log it", 980, 80, { path: "refunds/log.md" }),
    ],
    edges: [
      { from: "ticket", to: "pull" },
      { from: "pull", to: "size" },
      { from: "size", to: "review", when: "output.data.amount > 100", label: "over $100" },
      { from: "size", to: "refund", default: true },
      { from: "review", to: "log" },
      { from: "refund", to: "log" },
      { from: "pull", to: "log", error: true },
    ],
  },
  {
    name: "order-status",
    description: "Look the order up, ask shipping, file it",
    nodes: [
      flowNode("get", "api_call", "Get the order", 0, 80, { url: "https://api.shop.example/orders/{{ input }}", method: "GET", headers: [{ name: "Accept", value: "application/json" }], auth: { type: "bearer", token: "{secrets:shop_token}" } } as Partial<FakeFlow["nodes"][number]>),
      flowNode("ship", "a2a_call", "Ask shipping", 280, 0, { url: "https://agents.shipping.example", instructions: "When does order {{ input }} ship?" }),
      flowNode("issue", "mcp_call", "File an issue", 280, 160, { server: "github", tool: "create_issue", argumentsJson: '{"title": "Order {{ input }}"}' }),
    ],
    edges: [
      { from: "get", to: "ship" },
      { from: "get", to: "issue", error: true },
    ],
  },
  { name: "weekly-digest", description: "", nodes: [flowNode("sum-up", "summarize", "", 0, 80), flowNode("mail", "email", "", 280, 80)], edges: [{ from: "sum-up", to: "mail" }] },
];
const fakeFlowProblem = (f: FakeFlow) => {
  const p = f.nodes.find((n) => n.type === "email" || n.type === "data");
  if (p) return `step ${p.id}: ${p.type} steps don't run yet`;
  const q = f.nodes.find((n) => (n.type === "agent" || n.type === "refine" || n.type === "browser") && !n.instructions.trim());
  return q ? `step ${q.id}: say what it's to do` : "";
};
const fakeFlowFile = (workspace: string, f: FakeFlow) => ({ ...f, path: `${workspace}/.agents/workflows/${f.name}.flow.yaml`, problem: fakeFlowProblem(f) });
// A form as A2UI's server-to-client messages, as the service sends a task's.
const fakeMessages = (surfaceId: string, form: { components: unknown[]; data: unknown }) =>
  JSON.stringify([
    { version: "v0.9", createSurface: { surfaceId, catalogId: "https://a2ui.org/specification/v0_9/catalogs/basic/catalog.json", sendDataModel: true } },
    { version: "v0.9", updateComponents: { surfaceId, components: form.components } },
    { version: "v0.9", updateDataModel: { surfaceId, path: "/", value: form.data } },
  ]);
// Workflows' human tasks waiting: a refund to approve (?fake&notasks for none).
const fakeTasks = new URLSearchParams(location.search).has("notasks")
  ? []
  : [
      {
        id: "run-7/approve",
        run: "run-7",
        workspace: "/Users/me/shop",
        workflow: "refund-triage",
        step: "approve",
        title: "Approve the refund",
        kind: "human_task",
        message: "Order 4417 asks for a $240 refund: over the $100 limit.",
        // As the service sends it: A2UI's messages for the task's surface.
        a2uiJson: fakeMessages("run-7/approve", {
          components: [
            { id: "root", component: "Column", children: ["amount", "note", "buttons"] },
            { id: "amount", component: "TextField", label: "Refund ($)", value: { path: "/amount" }, variant: "number" },
            { id: "note", component: "TextField", label: "Note for the customer", value: { path: "/note" }, variant: "longText" },
            { id: "buttons", component: "Row", justify: "end", children: ["reject", "approve"] },
            { id: "reject_label", component: "Text", text: "Reject" },
            { id: "reject", component: "Button", child: "reject_label", action: { event: { name: "reject", context: { note: { path: "/note" } } } } },
            { id: "approve_label", component: "Text", text: "Approve" },
            { id: "approve", component: "Button", variant: "primary", child: "approve_label", action: { event: { name: "approve", context: { amount: { path: "/amount" }, note: { path: "/note" } } } } },
          ],
          data: { amount: 240, note: "" },
        }),
        created: now(),
        sessionId: "session-flow-7",
      },
    ];
let fakeAlerts = "notify";
// The task stream: the list now, then whenever it changes (as the service's).
async function* watchFakeTasks(workspace: string) {
  let last = "";
  for (;;) {
    const tasks = fakeTasks.filter((x) => !workspace || x.workspace === workspace);
    const sig = fakeAlerts + tasks.map((x) => x.id).join("|");
    if (sig !== last) {
      last = sig;
      yield { alerts: fakeAlerts, tasks };
    }
    await sleep(500);
  }
}
// Background runs of workflows: each drives itself (as the service's
// do, watched or not), its events kept for watchers and its receipt
// (WF-19) updated as it goes.
const fakeFlowRuns = new Map<string, FakeFlow>();
const flowLogs = new Map<string, { events: MessageInitShape<typeof WatchBackgroundResponseSchema>[]; done: boolean }>();
let receiptsVersion = 0;
const ago = (min: number) => timestampFromDate(new Date(Date.now() - min * 60000));
const fakeUsage = (calls: number, input: number, output: number, usd: number) => create(UsageSchema, { calls, input: BigInt(input), output: BigInt(output), costUsd: usd, priced: true });
// Past runs of the research brief: one done, one failed, one a worker's.
const fakeReceipts: WorkflowRun[] = [
  create(WorkflowRunSchema, {
    id: "flow-past-2",
    workspace: "",
    workflow: "research-brief",
    origin: "worker",
    started: ago(60 * 20),
    ended: ago(60 * 20 - 2),
    status: "failed",
    error: "The workflow research-brief stopped at Look it up: the page didn't load (timeout after 30s)",
    usage: fakeUsage(1, 1800, 120, 0.0021),
    models: ["gemini-4-argon"],
    steps: [
      { id: "notes", title: "Read the notes", type: "read_file", status: "done", started: ago(60 * 20), ended: ago(60 * 20), attempts: 1, usage: fakeUsage(0, 0, 0, 0) },
      { id: "look-up", title: "Look it up", type: "browser", status: "failed", started: ago(60 * 20), ended: ago(60 * 20 - 2), attempts: 3, agent: "blitz", models: ["gemini-4-argon"], usage: fakeUsage(1, 1800, 120, 0.0021), error: "the page didn't load (timeout after 30s)" },
    ],
  }),
  create(WorkflowRunSchema, {
    id: "flow-past-1",
    workspace: "",
    workflow: "research-brief",
    origin: "background",
    started: ago(60 * 26),
    ended: ago(60 * 26 - 3),
    status: "done",
    output: "# Coupon rounding\n\nRound each line half up…",
    usage: fakeUsage(3, 9400, 1210, 0.0164),
    models: ["gemini-4-argon", "gemini-4-flash"],
    steps: [
      { id: "notes", title: "Read the notes", type: "read_file", status: "done", started: ago(60 * 26), ended: ago(60 * 26), attempts: 1, usage: fakeUsage(0, 0, 0, 0) },
      { id: "look-up", title: "Look it up", type: "browser", status: "done", started: ago(60 * 26), ended: ago(60 * 26 - 1), attempts: 1, agent: "blitz", models: ["gemini-4-argon"], usage: fakeUsage(1, 3100, 410, 0.0052) },
      { id: "gather", title: "Gather", type: "accumulate", status: "done", started: ago(60 * 26 - 1), ended: ago(60 * 26 - 1), attempts: 1, usage: fakeUsage(0, 0, 0, 0) },
      { id: "draft", title: "Draft the brief", type: "agent", status: "done", started: ago(60 * 26 - 1), ended: ago(60 * 26 - 3), attempts: 1, agent: "reviewer", models: ["gemini-4-argon", "gemini-4-flash"], usage: fakeUsage(2, 6300, 800, 0.0112) },
      { id: "save", title: "Save the brief", type: "save_file", status: "done", started: ago(60 * 26 - 3), ended: ago(60 * 26 - 3), attempts: 1, usage: fakeUsage(0, 0, 0, 0) },
    ],
  }),
];
const fakeOutputs: Record<string, string> = {
  notes: "Coupons round half up since 0.4; refunds round down.",
  "look-up": "Most shops round each line, then the total.",
  gather: "## Read the notes\n\nCoupons round half up…\n\n## Look it up\n\nMost shops round each line…",
  draft: "# Coupon rounding\n\nRound each line half up, then total; refunds round down. Sources: notes, three shops' docs.",
  save: "",
};
const fakeSpend: Record<string, [number, number, number, string[]]> = { "look-up": [3100, 410, 0.0052, ["gemini-4-argon"]], draft: [6300, 800, 0.0112, ["gemini-4-argon", "gemini-4-flash"]] };

// The workspace dashboard (spec_health_044): the fake's items with facts,
// most of them judged, a check that judges what changed item by item.
const fakeDaily = (seed: number) => Array.from({ length: 30 }, (_, i) => ((i * 7 + seed * 3) % 5 === 0 ? 0 : (i + seed) % 4));
const fakeVerdict = (overall: number, lines: [string, number, string][], summary: string, fixes: string[], current = true) =>
  create(HealthVerdictSchema, {
    model: "gemini/gemini-4-flash",
    at: ago(60 * 9),
    overall,
    grade: overall >= 4.5 ? "A" : overall >= 3.5 ? "B" : overall >= 2.5 ? "C" : "D",
    summary,
    fixes,
    costUsd: 0.0021,
    current,
    scores: lines.map(([line, score, reason]) => ({ line, score, reason })),
  });
const fakeHealth: WorkspaceHealth = create(WorkspaceHealthSchema, {
  at: ago(0),
  overall: 3.9,
  previousOverall: 3.6,
  byKind: { agent: 4.2, skill: 3.4, rule: 4.6, guardrail: 4.8, eval: 3.1, workflow: 3.3, worker: 4.0 },
  lastCheck: ago(60 * 9),
  lastCostUsd: 0.034,
  model: "gemini/gemini-4-flash",
  schedule: "every day at 02:00",
  next: timestampFromDate(new Date(Date.now() + 5 * 3600_000)),
  maxCostUsd: 1,
  days: Array.from({ length: 30 }, (_, i) => {
    const d = new Date(Date.now() - (29 - i) * 86400_000);
    const done = (i * 5) % 7, failed = i % 6 === 0 ? 2 : i % 9 === 0 ? 1 : 0;
    return { date: d.toISOString().slice(0, 10), done, failed, stopped: i % 13 === 0 ? 1 : 0, costUsd: done * 0.011 + failed * 0.004 };
  }),
  signals: [
    { kind: "denial", about: "run_shell_command", count: 3, last: ago(120), detail: "pushing to main isn't part of the task" },
    { kind: "rule", about: "cites-sources", count: 2, last: ago(300), detail: "the brief quotes figures without a source" },
  ],
  items: [
    { kind: "agent", name: "writer", path: "/home/me/shop/.agents/agents/writer.md", detail: "Writes briefs and release notes", uses: 14, daily: fakeDaily(1), history: [3.6, 4.0, 4.4],
      evals: { runs: 4, last: ago(60 * 30), passed: 5, cases: 6 },
      verdict: fakeVerdict(4.4, [["Its instructions are clear and complete: what it's for, how it works, when it's done", 5, "Purpose, tone and the finish line are all stated."], ["Its tools fit what it's for, and no more", 4, "web_fetch is listed but no task needs it."], ["Its evals cover what it does", 4, "Six cases; none for release notes."], ["Its runs succeed", 5, "Every run in 30 days finished."]], "A clear, well-scoped writer; release notes lack an eval.", ["Add an eval case: \"Write release notes for v1.2 from CHANGELOG.md\" expecting a bulleted list of user-facing changes.", "Remove web_fetch from its tools."]) },
    { kind: "agent", name: "reviewer", path: "/home/me/shop/.agents/agents/reviewer.md", detail: "Reviews pull requests", flags: ["stale"], uses: 6, daily: fakeDaily(2), history: [4.1, 3.9],
      evals: { runs: 2, last: ago(60 * 24 * 6), passed: 3, cases: 4 },
      verdict: fakeVerdict(3.9, [["Its instructions are clear and complete: what it's for, how it works, when it's done", 4, "Clear, but it doesn't say what to do with a failing build."], ["Its tools fit what it's for, and no more", 4, "Fine."], ["Its evals cover what it does", 3, "Out of date since its last change."], ["Its runs succeed", 4, "One failure in six."]], "Solid; rerun its evals after the last edit.", ["Run its evals again: it changed after they last ran."]) },
    { kind: "agent", name: "triage", path: "/home/me/shop/.agents/agents/triage.md", detail: "Sorts incoming issues", flags: ["uncovered"], uses: 2, daily: fakeDaily(3), history: [],
      verdict: fakeVerdict(2.8, [["Its instructions are clear and complete: what it's for, how it works, when it's done", 3, "It lists labels but not when to use each."], ["Its tools fit what it's for, and no more", 3, "It can write files but only needs to read."], ["Its evals cover what it does", 1, "No eval checks it."], ["Its runs succeed", 4, "Both runs finished."]], "Useful but unproven: no evals and loose tools.", ["Add an eval with three issues and the label each should get.", "Replace write_file with read_file in its tools."]) },
    { kind: "agent", name: "broken", path: "/home/me/shop/.agents/agents/broken.md", problem: "failed to parse YAML frontmatter: line 3: did not find expected key", daily: fakeDaily(0) },
    { kind: "skill", name: "pdf", path: "/home/me/shop/.agents/skills/pdf", detail: "scripts", uses: 9, daily: fakeDaily(4), history: [3.2, 3.8],
      verdict: fakeVerdict(3.8, [["Its description says when to use it (it's what makes an agent choose it)", 4, "Says PDFs; could name forms and tables."], ["Its instructions can be followed step by step", 4, "Clear steps."], ["Its scripts run and pass the policy (or it needs none)", 3, "render.py needs the network, which the policy denies."], ["Something uses it", 4, "Used nine times."]], "Works; its render script hits the network policy.", ["Make render.py work offline, or ask for network in its front matter."]) },
    { kind: "skill", name: "changelog", path: "/home/me/shop/.agents/skills/changelog", detail: "simple", flags: ["unused"], daily: fakeDaily(0),
      verdict: fakeVerdict(3.0, [["Its description says when to use it (it's what makes an agent choose it)", 2, "\"Changelog helper\" doesn't say when."], ["Its instructions can be followed step by step", 4, "Fine."], ["Its scripts run and pass the policy (or it needs none)", 5, "None needed."], ["Something uses it", 1, "Never used."]], "Never chosen: its description doesn't say when.", ["Rewrite its description: \"Use when asked to update CHANGELOG.md or write release notes.\""]) },
    { kind: "rule", name: "cites-sources", path: "/home/me/shop/.agents/rules/cites-sources.md", detail: "after_agent", uses: 11, fired: 2, daily: fakeDaily(5), history: [4.6],
      verdict: fakeVerdict(4.6, [["It can be judged pass or fail from a prompt or an answer alone", 5, "Yes."], ["It isn't redundant with another rule or guardrail", 4, "Fine."], ["It fires when it should, and only then", 5, "Both firings were right."]], "A sharp, well-aimed rule.", []) },
    { kind: "guardrail", name: "secrets", detail: "secrets · output", uses: 40, fired: 1, daily: fakeDaily(6), history: [4.8],
      verdict: fakeVerdict(4.8, [["What it checks is clear and can be decided", 5, "Built-in."], ["Its action fits the risk (block, redact or warn)", 5, "Redacts."], ["It fires when it should, and only then", 4, "One redaction, correct."]], "Doing its job.", []) },
    { kind: "guardrail", name: "limits", detail: "limits · action", flags: ["off"], daily: fakeDaily(0) },
    { kind: "eval", name: "writing", path: "/home/me/shop/.agents/evals/writing.md", detail: "The writer's briefs", uses: 4, daily: fakeDaily(7), evals: { runs: 4, last: ago(60 * 30), passed: 5, cases: 6 }, history: [3.0, 3.1],
      verdict: fakeVerdict(3.1, [["Its cases cover the agent's job, the hard cases too", 3, "Only briefs; no long inputs."], ["Its expected outputs and thresholds are sensible", 3, "0.95 is strict for Near."], ["It tells good from bad (not always passing or always failing)", 4, "Mixed results."]], "Covers the basics; thresholds are tight.", ["Lower the Near threshold of case brief-2 to 0.8.", "Add a case with a 20-page input."]) },
    { kind: "workflow", name: "research-brief", path: "/home/me/shop/.agents/workflows/research-brief.flow.yaml", detail: "A one-page brief from the notes and the web", flags: ["failing"], uses: 7, daily: fakeDaily(8), history: [3.6, 3.3],
      runs: { runs: 7, done: 3, failed: 4, stopped: 0, costUsd: 0.146, medianMs: 41000n, last: ago(90), failingStep: "Look it up" },
      verdict: fakeVerdict(2.9, [["Its steps are well defined and in a sensible order", 4, "Good order."], ["It handles failure (retries, error links, timeouts)", 2, "The browser step has no timeout or retry."], ["Its cost and duration fit what it does", 3, "Fine."], ["Its runs succeed", 2, "Four of seven failed at Look it up."], ["Its permission mode fits what it does", 4, "Auto suits it."]], "Fails at the web step: give it a timeout and a retry.", ["Give Look it up a 60s timeout and 2 retries.", "Add an error link from Look it up to Draft so a brief is written from the notes alone."]) },
    { kind: "workflow", name: "refund-triage", path: "/home/me/shop/.agents/workflows/refund-triage.flow.yaml", detail: "Big refunds wait for a person", uses: 12, daily: fakeDaily(9), history: [4.0],
      runs: { runs: 12, done: 11, failed: 1, stopped: 0, costUsd: 0.082, medianMs: 9000n, last: ago(30) },
      verdict: fakeVerdict(4.0, [["Its steps are well defined and in a sensible order", 4, "Clear."], ["It handles failure (retries, error links, timeouts)", 4, "Has an error path."], ["Its cost and duration fit what it does", 4, "Cheap."], ["Its runs succeed", 4, "11 of 12."], ["Its permission mode fits what it does", 4, "Fine."]], "Reliable.", []) },
    { kind: "workflow", name: "weekly-digest", path: "/home/me/shop/.agents/workflows/weekly-digest.flow.yaml", problem: "step mail: email steps don't run yet", daily: fakeDaily(0) },
    { kind: "worker", name: "nightly-brief", path: "/home/me/shop/.agents/workers/nightly-brief/WORKER.md", detail: "research-brief", uses: 6, daily: fakeDaily(10), history: [4.0],
      runs: { runs: 6, done: 4, failed: 2, stopped: 0, costUsd: 0.31, medianMs: 52000n, last: ago(600) },
      verdict: fakeVerdict(4.0, [["Its permissions are as tight as its work allows", 4, "Writes only briefs/."], ["Its schedule and limits fit its work", 4, "Nightly, $0.50 cap."], ["Its runs succeed", 4, "Four of six."]], "Well bounded.", []) },
  ],
});

let fakeChecking = false;
let fakeProgress: { item: string; index: number; total: number; costUsd: number } | undefined;
async function* watchFakeHealth(): AsyncGenerator<{ health: WorkspaceHealth }> {
  let last = "";
  for (;;) {
    const sig = JSON.stringify([fakeHealth.items.map((i) => i.verdict?.overall ?? 0), fakeChecking, fakeHealth.lastCheck?.seconds.toString()]);
    if (sig !== last) {
      last = sig;
      yield { health: clone(WorkspaceHealthSchema, { ...fakeHealth, checking: fakeChecking }) };
    }
    await new Promise((r) => setTimeout(r, 400));
  }
}
const fakeDue = (scope?: { kinds: string[]; items: string[]; force: boolean }): HealthItem[] =>
  fakeHealth.items.filter((it) => !it.problem && (!scope?.kinds.length || scope.kinds.includes(it.kind)) && (!scope?.items.length || scope.items.includes(`${it.kind}/${it.name}`)) && (scope?.force || !it.verdict || !it.verdict.current || it.name === "reviewer"));
let fakeStopCheck = false;
async function* fakeCheck(scope?: { kinds: string[]; items: string[]; force: boolean }) {
  const due = fakeDue(scope);
  fakeChecking = true;
  fakeStopCheck = false;
  let cost = 0;
  let judged = 0;
  try {
    for (const [i, it] of due.entries()) {
      if (fakeStopCheck) break;
      yield { item: `${it.kind}/${it.name}`, index: i + 1, total: due.length, costUsd: cost };
      await new Promise((r) => setTimeout(r, 700));
      cost += 0.0019;
      const v = it.verdict ? clone(HealthVerdictSchema, it.verdict) : fakeVerdict(3.5, [["Overall", 4, "Judged in the fake."]], "Judged in the fake.", []);
      v.at = ago(0);
      v.current = true;
      if (it.name === "reviewer") v.overall = Math.min(5, v.overall + 0.3);
      v.grade = v.overall >= 4.5 ? "A" : v.overall >= 3.5 ? "B" : v.overall >= 2.5 ? "C" : "D";
      it.verdict = v;
      it.history = [...it.history, v.overall];
      judged++;
      yield { item: `${it.kind}/${it.name}`, index: i + 1, total: due.length, verdict: v, costUsd: cost };
    }
  } finally {
    fakeChecking = false;
    fakeHealth.lastCheck = ago(0);
    fakeHealth.lastCostUsd = cost;
  }
  yield { done: true, judged, costUsd: cost };
}

// Runs a workflow in the fake: its steps one after another, the agent
// steps streaming their text, save_file asking for approval.
async function driveFlow(r: BackgroundRun, f: FakeFlow, s: ReturnType<typeof state>) {
  const log = { events: [] as MessageInitShape<typeof WatchBackgroundResponseSchema>[], done: false };
  flowLogs.set(r.id, log);
  const ev = (kind: unknown) => log.events.push({ event: { author: "blitz", kind } } as MessageInitShape<typeof WatchBackgroundResponseSchema>);
  // Its mode (WF-21): the workflow's, auto unless it says, held back by a stricter workspace.
  const rank: Record<string, number> = { plan: 0, "dont-ask": 1, default: 2, "accept-edits": 3, auto: 3, bypass: 4 };
  const want = f.mode || "auto";
  const mode = (rank[want] ?? 2) > (rank[s.settings.mode] ?? 2) ? s.settings.mode : want;
  const rec = create(WorkflowRunSchema, { id: `flow-${Date.now()}`, workspace: r.workspace, workflow: f.name, origin: "background", sessionId: r.sessionId, input: r.prompt.split(" ").slice(1).join(" "), started: now(), status: "running", backgroundId: r.id, usage: fakeUsage(0, 0, 0, 0), mode });
  rec.events.push(create(WorkflowRunEventSchema, { at: now(), kind: "run", text: "started" }));
  if (mode !== want) rec.events.push(create(WorkflowRunEventSchema, { at: now(), kind: "run", text: `held: ${want}: ${mode}` }));
  fakeReceipts.unshift(rec);
  const bump = () => {
    let calls = 0, input = 0n, output = 0n, usd = 0;
    for (const st of rec.steps) {
      calls += st.usage?.calls ?? 0;
      input += st.usage?.input ?? 0n;
      output += st.usage?.output ?? 0n;
      usd += st.usage?.costUsd ?? 0;
    }
    rec.usage = create(UsageSchema, { calls, input, output, costUsd: usd, priced: true });
    rec.models = [...new Set(rec.steps.flatMap((x) => x.models))];
    receiptsVersion++;
  };
  const note = (step: string, kind: string, text: string, error = false) => rec.events.push(create(WorkflowRunEventSchema, { at: now(), step, kind, text, error }));
  const end = (status: string, error = "") => {
    rec.status = status;
    rec.ended = now();
    rec.error = error;
    rec.backgroundId = "";
    rec.approvals = 0;
    note("", "run", error ? `${status}: ${error}` : status, !!error);
    r.state = status === "done" ? "done" : status;
    r.ended = now();
    log.done = true;
    bump();
  };
  const order = ["notes", "look-up", "gather", "draft", "save"].filter((id) => f.nodes.some((n) => n.id === id));
  for (const [i, id] of order.entries()) {
    if (runStopped.has(r.id)) return end("stopped");
    const n = f.nodes.find((x) => x.id === id)!;
    const step = { workflow: f.name, id, title: n.title, type: n.type, index: i + 1, count: order.length, run: rec.id };
    ev({ case: "flowStep", value: { ...step, status: "running" } });
    const spend = fakeSpend[id];
    const row = create(WorkflowRunStepSchema, { id, title: n.title, type: n.type, status: "running", started: now(), attempts: 1, agent: spend ? n.agent || "blitz" : "", models: spend?.[3] ?? [], usage: fakeUsage(0, 0, 0, 0) });
    rec.steps.push(row);
    note(id, "step", "running");
    bump();
    if (spend) {
      // Streams its text a few words at a time, spending as it goes.
      const words = (fakeOutputs[id] ?? "").split(" ");
      for (const [k, w] of words.entries()) {
        if (runStopped.has(r.id)) break;
        row.stream += (k ? " " : "") + w;
        row.usage = fakeUsage(1, Math.round((spend[0] * (k + 1)) / words.length), Math.round((spend[1] * (k + 1)) / words.length), (spend[2] * (k + 1)) / words.length);
        if (k === 2) note(id, "tool", "web_fetch");
        bump();
        await sleep(160);
      }
    } else await sleep(700);
    if (runStopped.has(r.id)) {
      row.status = "failed";
      row.ended = now();
      row.error = "stopped";
      return end("stopped");
    }
    if (n.type === "save_file" && mode !== "auto" && mode !== "accept-edits" && mode !== "bypass") {
      const req = `${r.id}-save`;
      const answer = new Promise<string>((res) => s.pending.set(req, res));
      r.state = "waiting";
      r.waiting = 1;
      rec.approvals = 1;
      bump();
      ev({ case: "approvalRequest", value: { requestId: req, tool: "save_file", kind: ActionKind.WRITE, detail: n.path, scopeLabel: n.path } });
      const a = await answer;
      s.pending.delete(req);
      r.state = "running";
      r.waiting = 0;
      rec.approvals = 0;
      if (a === "stop") {
        row.status = "failed";
        row.ended = now();
        row.error = "stopped";
        return end("stopped");
      }
      if (a === "deny") {
        const error = `${n.path}: not approved`;
        row.status = "failed";
        row.ended = now();
        row.error = error;
        note(id, "step", `failed: ${error}`, true);
        ev({ case: "flowStep", value: { ...step, status: "failed", error } });
        ev({ case: "finished", value: { output: "", before: s.usage, after: s.usage, error: create(ErrorInfoSchema, { reason: "NOT_APPROVED", message: `The workflow ${f.name} stopped at ${n.title}: ${error}` }) } });
        return end("failed", `The workflow ${f.name} stopped at ${n.title}: ${error}`);
      }
    }
    row.status = "done";
    row.ended = now();
    row.output = fakeOutputs[id] ?? "";
    note(id, "step", "done");
    ev({ case: "flowStep", value: { ...step, status: "done", output: fakeOutputs[id] ?? "" } });
    bump();
  }
  rec.output = fakeOutputs.draft;
  ev({ case: "finished", value: { output: fakeOutputs.draft, before: s.usage, after: s.usage } });
  end("done");
}

// A receipt as a list shows it: without its events and its steps' text.
const summary = (r: WorkflowRun) => {
  const c = clone(WorkflowRunSchema, r);
  c.events = [];
  for (const x of c.steps) x.stream = x.output = "";
  return c;
};
const receiptsFor = (workspace: string) => fakeReceipts.map((r) => (r.workspace ? r : Object.assign(r, { workspace: [...states.keys()][0] ?? workspace })));

async function* watchFakeRuns(workspace: string) {
  let last = -1;
  for (;;) {
    if (receiptsVersion !== last) {
      last = receiptsVersion;
      const cutoff = Date.now() - 10 * 60_000;
      yield { runs: receiptsFor(workspace).filter((r) => (!workspace || r.workspace === workspace) && (r.status === "running" || r.status === "waiting" || (r.ended && Number(r.ended.seconds) * 1000 > cutoff))).map(summary) };
    }
    await sleep(300);
  }
}

async function* watchFakeRun(id: string) {
  let last = -1;
  for (;;) {
    const r = fakeReceipts.find((x) => x.id === id);
    if (!r) throw new ConnectError(`no workflow run ${id}`, Code.NotFound);
    if (receiptsVersion !== last) {
      last = receiptsVersion;
      yield { run: clone(WorkflowRunSchema, r) };
    }
    if (r.status !== "running") return;
    await sleep(200);
  }
}

async function* watchRun(id: string): AsyncGenerator<MessageInitShape<typeof WatchBackgroundResponseSchema>> {
  const r = backgroundRuns().find((x) => x.id === id);
  if (!r) throw new ConnectError(`no background run ${id}`, Code.NotFound);
  const s = state(r.workspace);
  const ev = (kind: unknown) => ({ event: { author: "blitz", kind } }) as MessageInitShape<typeof WatchBackgroundResponseSchema>;
  const log = flowLogs.get(id);
  if (log) {
    // From the start, then as it goes (answered approvals left out).
    for (let sent = 0; ; ) {
      while (sent < log.events.length) {
        const e = log.events[sent++];
        const k = (e.event as { kind?: { case?: string; value?: { requestId?: string } } }).kind;
        if (k?.case === "approvalRequest" && !s.pending.has(k.value?.requestId ?? "")) continue;
        yield e;
      }
      if (log.done) return;
      await sleep(150);
    }
  }
  const ended = () => ev({ case: "finished", value: { output: "", before: s.usage, after: s.usage } });
  if (r.state === "done") {
    yield ev({ case: "text", value: { text: "The release notes are in docs/releases/0.4.md." } });
    yield ended();
    return;
  }
  yield ev({ case: "toolCall", value: { id: "b1", name: "run_shell_command", args: { command: "make build-staging" } } });
  yield ev({ case: "toolResult", value: { id: "b1", name: "run_shell_command", result: { exit_code: 0, output: "built shop-staging" } } });
  if (r.state === "waiting") {
    const req = `${id}-req`;
    const answer = new Promise<string>((res) => s.pending.set(req, res));
    yield ev({ case: "approvalRequest", value: { requestId: req, tool: "run_shell_command", kind: ActionKind.COMMAND, detail: "kubectl rollout restart deploy/shop -n staging" } });
    const a = await answer;
    r.state = "running";
    r.waiting = 0;
    yield ev({ case: "text", value: { text: a === "deny" ? "Not deploying, then." : "Deployed; /healthz answers 200." } });
    r.state = "done";
    r.ended = now();
    yield ended();
    return;
  }
  while (!runStopped.has(id)) await sleep(200);
  yield ev({ case: "finished", value: { output: "", before: s.usage, after: s.usage, error: create(ErrorInfoSchema, { reason: "CANCELLED", message: "stopped" }) } });
}

/** Replaces the service with the in-page fake (development: ?fake). */
export function installFake() {
  setTransport(
    createRouterTransport(({ service }) => {
      service(SessionService, {
        // The slash commands' (slashCommands.ts): enough to see each answer.
        exportSession: ({ workspace }) => ({ markdown: `# ${active(state(workspace)).title || "Chat"}\n` }),
        forkSession: ({ workspace }) => ({ session: active(state(workspace)) }),
        getGoal: () => ({ goal: fakeGoal }),
        setGoal: ({ condition }) => ((fakeGoal = { condition, continues: 0, max: 10, last: "" }), { goal: fakeGoal }),
        clearGoal: () => ((fakeGoal = undefined), {}),
        getActiveSession: ({ workspace }) => ({ session: active(state(workspace)) }),
        listSessions: ({ workspace }) => ({ sessions: state(workspace).sessions.map((s) => ({ ...s, messages: [] })) }),
        newSession: ({ workspace }) => {
          const s = state(workspace);
          const n = create(SessionInfoSchema, { id: `session-${s.sessions.length + 1}`, workspace, created: now(), updated: now() });
          s.sessions.unshift(n);
          s.active = n.id;
          return { session: n };
        },
        listBackground: () => ({ runs: backgroundRuns() }),
        // A workflow's run from Studio (/name input); other prompts aren't faked.
        startBackground: ({ workspace, turn }) => {
          const name = (turn?.text ?? "").replace(/^\//, "").split(" ")[0];
          const f = fakeFlows.find((x) => x.name === name);
          if (!f) throw new ConnectError("the fake runs only workflows in the background", Code.Unimplemented);
          const run = create(BackgroundRunSchema, { id: `bg-${backgroundRuns().length + 1}`, workspace, sessionId: `session-flow-${Date.now()}`, prompt: turn!.text, state: "running", started: now() });
          backgroundRuns().unshift(run);
          fakeFlowRuns.set(run.id, f);
          void driveFlow(run, f, state(workspace));
          return { run };
        },
        watchBackground: ({ id }) => watchRun(id),
        stopBackground: ({ id }) => {
          const r = backgroundRuns().find((x) => x.id === id);
          if (!r) throw new ConnectError(`no background run ${id}`, Code.NotFound);
          if (r.state === "running" || r.state === "waiting") {
            runStopped.add(id);
            r.state = "stopped";
            r.waiting = 0;
            r.ended = now();
          }
          return { run: r };
        },
        loadSession: ({ workspace, ref }) => {
          const s = state(workspace);
          if (ref.startsWith("worker-") && !s.sessions.some((x) => x.id === ref)) {
            // A worker run's session: its report.
            const run = create(SessionInfoSchema, { id: ref, title: "⏰ nightly-deps", origin: "worker", workspace, created: now(), updated: now(), messages: [msg("user", "Check for outdated dependencies."), msg("model", "Two modules are behind: golang.org/x/net and google.golang.org/grpc. The report is in reports/deps.md.")] });
            run.messageCount = 2;
            s.sessions.push(run);
          }
          if (!s.sessions.some((x) => x.id === ref)) notFound(ref);
          s.active = ref;
          return { session: active(s) };
        },
        deleteSession: ({ workspace, sessionId }) => {
          const s = state(workspace);
          if (sessionId === s.active) throw new ConnectError("the session is open: switch to another first", Code.FailedPrecondition);
          if (!s.sessions.some((x) => x.id === sessionId)) throw new ConnectError("no such session", Code.NotFound);
          s.sessions = s.sessions.filter((x) => x.id !== sessionId);
          return {};
        },
        renameSession: ({ workspace, title }) => {
          const sess = active(state(workspace));
          sess.title = title;
          return { session: sess };
        },
        runTurn: (req) => {
          (globalThis as { __lastTurn?: unknown }).__lastTurn = req.turn; // for checks
          return runTurn(req.workspace, req.turn?.text ?? "", !!req.turn?.plan);
        },
        steer: ({ workspace, text }) => {
          active(state(workspace)).messages.push(msg("user", text, "steer"));
          return {};
        },
        approve: ({ workspace, requestId, decision }) => {
          const task = state(workspace).tasks.get(requestId.replace(/-req$/, ""));
          if (task) task.answered = Date.now();
          state(workspace).pending.get(requestId)?.(decision === Decision.DENY ? "deny" : "allow");
          return {};
        },
        answer: ({ workspace, requestId, answer }) => {
          state(workspace).pending.get(requestId)?.(answer);
          return {};
        },
        getUsage: ({ workspace }) => ({ usage: state(workspace).usage }),
        compact: () => ({ eventsCompacted: 12, summaryChars: 1840 }),
        rewind: ({ workspace, index, mode }) => {
          const sess = active(state(workspace));
          const prompt = sess.messages[index]?.text ?? "";
          if (mode === "both" || mode === "conversation") {
            sess.messages = sess.messages.slice(0, index);
            sess.messageCount = sess.messages.length;
          }
          return { mode, prompt, restored: mode === "conversation" ? [] : ["internal/cart/discount.go"], compacted: { eventsCompacted: 6, summaryChars: 900 } };
        },
        listRewindPoints: () => ({ points: [] }),
        searchSession: ({ terms }) => ({ found: terms === "zzz" ? 0 : 2, prompt: `Look at these passages about ${terms}…` }),
        saveSnapshot: ({ name }) => ({ snapshot: { id: "snap-1", snapshot: name } }),
      });
      service(WorkspaceService, {
        // The slash commands' (slashCommands.ts): enough to see each answer.
        getSandbox: () => ({ summary: ["Shell commands: sandboxed (sandbox-exec)", "Network: allowed for listed hosts", "Writable: the workspace and the temp folder"] }),
        listMCPServers: () => ({
          servers: [
            ...fakeMCP.map((m) => ({ name: m.name, target: m.url || [m.command, ...(m.args ?? [])].join(" "), autoApprove: !!m.autoApprove, disabled: !!m.disabled, state: m.disabled ? "disabled" : m.name === "github" ? "connected" : m.name === "jira" ? "failed" : "idle", tools: m.name === "github" ? 26 : 0, error: m.name === "jira" && !m.disabled ? "401 Unauthorized: sign in with blitz mcp login jira" : "" })),
            { name: "db", target: "npx @acme/db-mcp", state: "connected", tools: 4 },
          ],
        }),
        restartMCPServer: () => ({}),
        draftManifest: ({ name, current }) => ({
          text: `# ${name || "Reviewer"}\n\nYou review changes for correctness first, then clarity.\n\n## How you work\n- Read the diff and the files it touches.\n- List real bugs with the line and why.\n- Suggest the smallest fix.\n${current ? "\n(Improved from the instructions there.)" : ""}`,
          changes: ["Said what the agent is for and how it works", "Added a short checklist"],
        }),
        pinModel: ({ agent, ref }) => ({ agent, model: ref }),
        unpinModel: ({ agent, workspace }) => ({ agent, model: state(workspace).settings.model }),
        reloadMemory: () => ({ paths: ["/home/me/blitz/AGENTS.md", "/home/me/blitz/.agents/AGENTS.md"], files: ["AGENTS.md"] }),
        addMemory: () => ({ path: "/home/me/blitz/BLITZ.md" }),
        listNotes: () => ({ notes: [{ name: "answer-style", kind: "feedback", text: "Keep answers short; show the command first." }] }),
        forgetNote: () => ({}),
        listHooks: () => ({ hooks: [{ event: "PreToolUse", type: "command", match: "run_shell_command", runs: "./scripts/guard.sh", source: "your settings", failures: [] }] }),
        listEnvs: () => ({ envs: [{ key: "a1b2c3", deps: ["pandas", "matplotlib"], skills: ["data-report"], size: 182_000_000n, ready: true }] }),
        pruneEnvs: () => ({ removed: 0, freed: 0n, failed: [] }),
        removeEnv: () => ({}),
        listSkills: ({ workspace, query }) => ({ skills: [...fakeOtherSkills, ...fakeProjectSkills.map((x) => fakeSkillInfo(workspace, x))].filter((x) => !query || x.name.includes(query)) }),
        getSkill: ({ name }) => ({ skill: { name, description: "Read and summarize PDF files", tools: [{ name: "read_file" }] } }),
        // Workflows' human tasks, answered by their forms.
        listHumanTasks: ({ workspace }) => ({ alerts: fakeAlerts, tasks: fakeTasks.filter((x) => !workspace || x.workspace === workspace) }),
        watchHumanTasks: ({ workspace }) => watchFakeTasks(workspace),
        listWorkflowRuns: ({ workspace, workflow, limit }) => ({ runs: receiptsFor(workspace).filter((r) => (!workspace || r.workspace === workspace) && (!workflow || r.workflow === workflow)).slice(0, limit || 50).map(summary) }),
        getWorkflowRun: ({ id }) => {
          const r = fakeReceipts.find((x) => x.id === id);
          if (!r) throw new ConnectError(`no workflow run ${id}`, Code.NotFound);
          return { run: r };
        },
        watchWorkflowRun: ({ id }) => watchFakeRun(id),
        watchWorkflowRuns: ({ workspace }) => watchFakeRuns(workspace),
        getWorkspaceHealth: () => ({ health: clone(WorkspaceHealthSchema, { ...fakeHealth, checking: fakeChecking, progress: fakeChecking && fakeProgress ? create(HealthCheckProgressSchema, fakeProgress) : undefined }) }),
        // The composer: one question, then a three-step draft.
        startCompose: ({ request }) => ({
          session: { id: "compose-fake", kind: "workflow", asking: true, costUsd: 0.0012, turns: [{ role: "user", text: request }, { role: "composer", text: "Where are the notes kept?", options: ["notes/today.md", "A folder of notes", "In the chat"] }] },
        }),
        answerCompose: ({ answer }) => {
          const file = "description: A daily brief from the notes\nnodes:\n  - {id: notes, type: read_file, title: Read the notes, path: notes/today.md, position: {x: 0, y: 80}}\n  - {id: draft, type: agent, title: Draft the brief, instructions: Write a short brief for the team., position: {x: 280, y: 80}}\n  - {id: save, type: save_file, title: Save it, path: briefs/today.md, position: {x: 560, y: 80}}\nedges:\n  - {from: notes, to: draft}\n  - {from: draft, to: save}\n";
          return {
            session: {
              id: "compose-fake",
              kind: "workflow",
              asking: false,
              costUsd: 0.0031,
              turns: [{ role: "user", text: "A daily brief" }, { role: "composer", text: "Where are the notes kept?", options: [] }, { role: "user", text: answer }, { role: "composer", text: "Reads today's notes, drafts a short brief and saves it in briefs/." }],
              draft: {
                name: "daily-brief",
                summary: "Reads today's notes, drafts a short brief and saves it in briefs/.",
                file,
                problems: [],
                workflow: {
                  name: "daily-brief",
                  description: "A daily brief from the notes",
                  nodes: [
                    flowNode("notes", "read_file", "Read the notes", 0, 80, { path: "notes/today.md" }),
                    flowNode("draft", "agent", "Draft the brief", 280, 80, { instructions: "Write a short brief for the team." }),
                    flowNode("save", "save_file", "Save it", 560, 80, { path: "briefs/today.md" }),
                  ],
                  edges: [
                    { from: "notes", to: "draft" },
                    { from: "draft", to: "save" },
                  ],
                },
              },
            },
          };
        },
        dropCompose: () => ({}),
        startHealthCheck: ({ scope }) => {
          if (fakeChecking) throw new ConnectError("a health check of this workspace is running", Code.FailedPrecondition);
          void (async () => {
            for await (const r of fakeCheck(scope)) {
              if (r.done) {
                fakeHealth.lastJudged = r.judged ?? 0;
                fakeHealth.lastErrors = [];
              } else if (!("verdict" in r)) fakeProgress = { item: r.item ?? "", index: r.index ?? 0, total: r.total ?? 0, costUsd: r.costUsd ?? 0 };
            }
          })();
          return {};
        },
        watchWorkspaceHealth: () => watchFakeHealth(),
        estimateHealthCheck: ({ scope }) => {
          const n = fakeDue(scope).length;
          return { items: n, costUsd: n * 0.0019, priced: true };
        },
        checkWorkspaceHealth: ({ scope }) => fakeCheck(scope),
        stopHealthCheck: () => {
          fakeStopCheck = true;
          return {};
        },
        stopWorkflowRun: ({ id }) => {
          const r = fakeReceipts.find((x) => x.id === id && x.status === "running");
          if (!r) throw new ConnectError("the workflow run isn't running", Code.NotFound);
          runStopped.add(r.backgroundId);
          const bg = backgroundRuns().find((x) => x.id === r.backgroundId);
          if (bg) state(bg.workspace).pending.get(`${bg.id}-save`)?.("stop");
          return {};
        },
        answerHumanTask: ({ taskId, actionJson }) => {
          const i = fakeTasks.findIndex((x) => x.id === taskId);
          if (i < 0) throw new ConnectError("no such task", Code.NotFound);
          // The service checks the answer against the form; the fake, that it's an action.
          if (!(JSON.parse(actionJson || "{}") as { action?: unknown }).action) throw new ConnectError("the answer isn't accepted: not an action message", Code.InvalidArgument);
          fakeTasks.splice(i, 1);
          return {};
        },
        // Studio › Workflows: listed, saved and deleted; runs are background runs.
        listWorkflows: ({ workspace }) => ({ dir: `${workspace}/.agents/workflows`, workflows: fakeFlows.map((f) => fakeFlowFile(workspace, f)) }),
        saveWorkflow: ({ workspace, previousName, workflow }) => {
          const w = workflow!;
          if (!/^[a-z0-9]+([-_][a-z0-9]+)*$/.test(w.name)) return { problems: [`name "${w.name}": lowercase letters, digits and single hyphens, up to 64`] };
          const next: FakeFlow = {
            name: w.name,
            description: w.description,
            mode: w.mode,
            nodes: w.nodes.map((n) => ({ ...n, skills: [...n.skills], rules: [...n.rules], args: [...n.args], fields: n.fields.map((f) => ({ ...f })) })),
            edges: w.edges.map((e) => ({ from: e.from, to: e.to, when: e.when, if: e.if, default: e.default, error: e.error, label: e.label })),
          };
          const i = fakeFlows.findIndex((x) => x.name === (previousName || w.name));
          if (i >= 0) fakeFlows[i] = next;
          else fakeFlows.push(next);
          return { workflow: fakeFlowFile(workspace, next) };
        },
        deleteWorkflow: ({ name }) => {
          const i = fakeFlows.findIndex((x) => x.name === name);
          if (i < 0) throw new ConnectError(`no workflow ${name}`, Code.NotFound);
          fakeFlows.splice(i, 1);
          return {};
        },
        // Studio › Evals: two evals, their runs, and a run streamed case by case.
        listEvals: ({ workspace }) => ({ dir: `${workspace}/.agents/evals`, evals: fakeEvals.map((e) => fakeEvalFile(workspace, e)) }),
        saveEval: ({ workspace, previousName, eval: spec }) => {
          const e = spec!;
          if (!/^[a-z0-9]+([-_][a-z0-9]+)*$/.test(e.name)) return { problems: [`name "${e.name}": lowercase letters, digits and single hyphens, up to 64`] };
          const blank = e.cases.find((c) => !c.prompt.trim());
          if (blank) return { problems: [`case ${blank.id}: it needs a prompt`] };
          const next: FakeEval = { name: e.name, description: e.description, agents: e.agents, skills: e.skills, near: e.near || "judge", allow: e.allow, cases: e.cases.map((c) => ({ ...c, threshold: c.type === "near" ? c.threshold || 0.8 : 0 })) };
          const i = fakeEvals.findIndex((x) => x.name === (previousName || e.name));
          if (i >= 0) fakeEvals[i] = next;
          else fakeEvals.push(next);
          return { eval: fakeEvalFile(workspace, next) };
        },
        deleteEval: ({ name }) => {
          const i = fakeEvals.findIndex((x) => x.name === name);
          if (i < 0) throw new ConnectError(`no eval ${name}`, Code.NotFound);
          fakeEvals.splice(i, 1);
          return {};
        },
        listEvalRuns: ({ name }) => ({ runs: [...fakeLiveFor(name), ...(fakeEvalRuns[name] ?? []).slice().reverse()] }),
        draftEvalCases: async ({ agent, existing }) => {
          await new Promise((r) => setTimeout(r, 900));
          const taken = new Set(existing.map((c) => c.id));
          const id = (s: string) => (taken.has(s) ? `${s}-2` : s);
          return {
            cases: [
              { id: id("plan-steps"), name: "Plans in steps", prompt: "Plan adding a wishlist to the shop.", output: "A numbered plan: data model, API, page, tests, rollout.", type: "near", threshold: 0.75, scope: "fail" },
              { id: id("asks-first"), name: "Asks when unclear", prompt: "Make it faster.", output: "Asks what should be faster before planning.", type: "near", threshold: 0.8, scope: "fail" },
              { id: id("no-code"), name: "Doesn't write code", prompt: "Write the wishlist API.", output: "Plans it, and leaves the code to a coding agent.", type: "near", threshold: 0.7, scope: "warn" },
              { id: id("format"), name: "", prompt: `Reply with only the word READY if you're ${agent}.`, output: "READY", type: "exact", threshold: 0, scope: "info" },
            ],
          };
        },
        runEval: async function* ({ name, background }, ctx) {
          // A skill's scenarios run as the eval "skill:<name>" (SK-111).
          const sk = name.startsWith("skill:") ? fakeProjectSkills.find((k) => k.name === name.slice(6)) : undefined;
          const e = sk ? ({ name, description: "", agents: [], skills: [], near: "judge", allow: [], cases: (sk.scenarios ?? []).map((sc) => ({ id: sc.name, name: sc.description, prompt: sc.prompt, output: sc.outcome, type: "near", threshold: sc.threshold, scope: "fail" })) } as FakeEval) : fakeEvals.find((x) => x.name === name);
          if (!e) throw new ConnectError(`no eval ${name}`, Code.NotFound);
          const live = startFakeEval(e);
          // Without background, it stops when the caller leaves (blitz eval).
          if (!background) ctx.signal.addEventListener("abort", () => (live.stopped = true), { once: true });
          yield* followFakeEval(live, ctx.signal);
        },
        watchEvalRun: async function* ({ id }, ctx) {
          const live = fakeLiveEvals.get(id);
          if (live) return yield* followFakeEval(live, ctx.signal);
          const run = Object.values(fakeEvalRuns).flat().find((r) => r.id === id);
          if (!run) throw new ConnectError(`no eval run ${id}`, Code.NotFound);
          yield { event: { case: "finished" as const, value: run } };
        },
        stopEvalRun: ({ id }) => {
          const live = fakeLiveEvals.get(id);
          if (!live) throw new ConnectError(`no eval run ${id}`, Code.NotFound);
          live.stopped = true;
          return {};
        },
        // Studio › Guardrails: the built-ins on or off, and the workspace's own.
        listGuardrails: ({ workspace }) => ({
          dir: `${workspace}/.agents/guardrails`,
          builtins: fakeBuiltinGuards.map((b) => {
            const g = fakeGuards.find((x) => x.builtin === b.id);
            return { ...b, file: g ? fakeGuardFile(workspace, g) : { name: b.id, builtin: b.id, stage: b.stage, action: b.defaultAction, agents: [], tools: [], description: "", pii: false, judge: false, text: "", off: false, path: "", problem: "", editable: true, limits: b.id === "limits" ? { maxCostUsd: 0, maxTurns: 0, maxFilesChanged: 0, allowedDomains: [], commands: ["git push*--force*", "git push* -f*", "rm -rf /", "rm -rf ~", "rm -rf ~/"] } : undefined } };
          }),
          guardrails: fakeGuards.filter((g) => !g.builtin).map((g) => fakeGuardFile(workspace, g)),
        }),
        saveGuardrail: ({ workspace, previousName, guardrail }) => {
          const g = guardrail!;
          const name = g.builtin || g.name;
          if (!g.builtin && !/^[a-z0-9]+([-_][a-z0-9]+)*$/.test(name)) return { problems: [`name "${name}": lowercase letters, digits and single hyphens, up to 64`] };
          if (!g.builtin && !g.text.trim()) return { problems: ["the guardrail is empty: say what to catch"] };
          const next = { name, builtin: g.builtin, stage: g.builtin ? fakeBuiltinGuards.find((b) => b.id === g.builtin)!.stage : g.stage, action: g.action, agents: g.agents, tools: g.tools, description: g.description, pii: g.pii, judge: g.judge, limits: g.limits, text: g.text, off: g.off };
          const i = fakeGuards.findIndex((x) => x.name === (g.builtin || previousName || name));
          if (i >= 0) fakeGuards[i] = next;
          else fakeGuards.push(next);
          return { guardrail: fakeGuardFile(workspace, next) };
        },
        deleteGuardrail: ({ name }) => {
          const i = fakeGuards.findIndex((x) => x.name === name);
          if (i < 0) throw new ConnectError(`no guardrail ${name}`, Code.NotFound);
          fakeGuards.splice(i, 1);
          return {};
        },
        tryGuardrail: async ({ guardrail, sample }) => {
          await new Promise((r) => setTimeout(r, 600));
          if (guardrail?.builtin === "secrets") return /@|AKIA/.test(sample) ? { pass: false, reason: `It would be shown as: ${sample.replace(/\S+@\S+/g, "[EMAIL REDACTED]")}` } : { pass: true, reason: "No secrets or personal data found." };
          if (guardrail?.builtin === "prompt_injection") return /ignore|you are now/i.test(sample) ? { pass: false, reason: "it contains text aimed at the agent" } : { pass: true, reason: "Nothing in it looks aimed at the agent." };
          return /delete|-D|drop/i.test(sample) ? { pass: false, reason: "It deletes a branch." } : { pass: true, reason: "Nothing here is deleted." };
        },
        // Studio › Connectors.
        listConnectors: ({ workspace }) => ({
          categories: [
            {
              ...fakeConnectorCategory,
              workspaceDir: `${workspace}/.agents/connectors/databases`,
              state: "connected",
              toolCount: fakeConnectors.filter((d) => d.enabled !== false).reduce((n, d) => n + d.tools.length + d.queries.length, 0),
            },
          ],
          kinds: [...fakeConnectorKinds, { id: "mysql", category: "databases", name: en("MySQL"), backend: "toolbox", origin: "workspace", path: `${workspace}/.agents/connectors/kinds/mysql.yaml`, problem: 'source "mysql": not in Blitz\'s Toolbox' }],
          connections: fakeConnectors.map((d) => fakeConnectorMsg(workspace, d)),
          workspaceDir: `${workspace}/.agents/connectors`,
          globalDir: "~/.blitz/connectors",
        }),
        saveConnector: ({ workspace, connection, previousName, secretValues }) => {
          if (connection!.scope === "global") throw new ConnectError("a global connection is changed in Settings, not in a workspace", Code.FailedPrecondition);
          return fakeConnectorCalls.save(workspace, { ...connection!, scope: "workspace" }, previousName, secretValues);
        },
        deleteConnector: ({ category, scope, name }) => {
          if (scope === "global") throw new ConnectError("a global connection is changed in Settings, not in a workspace", Code.FailedPrecondition);
          const i = fakeConnectors.findIndex((d) => d.name === name && d.scope === scope && d.category === category);
          if (i < 0) throw new ConnectError(`no connection ${name}`, Code.NotFound);
          fakeConnectors.splice(i, 1);
          return {};
        },
        setConnectorEnabled: ({ category, scope, name, enabled }) => {
          const d = fakeConnectors.find((d) => d.name === name && d.scope === scope && d.category === category);
          if (!d) throw new ConnectError(`no connection ${name}`, Code.NotFound);
          d.enabled = enabled;
          return {};
        },
        makeConnectorGlobal: ({ category, name }) => {
          const d = fakeConnectors.find((d) => d.name === name && d.scope === "workspace" && d.category === category);
          if (!d) throw new ConnectError(`no connection ${name}`, Code.NotFound);
          if (!d.trusted) throw new ConnectError("save and trust it first", Code.FailedPrecondition);
          if (fakeConnectors.some((x) => x.name === name && x.scope === "global" && x.category === category)) return { problems: [`there's a global connection named "${name}" already`] };
          d.scope = "global";
          return { connection: fakeConnectorMsg("", d) };
        },
        testConnector: ({ connection }) => fakeConnectorCalls.test(connection!),
        tryConnectorTool: ({ connection, tool }) => fakeConnectorCalls.tryTool(connection!, tool),
        readConnectorSchema: ({ connection }) => fakeConnectorCalls.readSchema(connection!),
        draftConnectorQuery: ({ request }) => fakeConnectorCalls.draft(request),
        // Studio › Rules: gates before and after the agent, and instruction rules.
        listRules: ({ workspace }) => ({ dir: `${workspace}/.agents/rules`, rules: fakeRuleList(workspace) }),
        saveRule: ({ workspace, previousName, rule }) => {
          const r = rule!;
          if (!/^[a-z0-9]+([-_][a-z0-9]+)*$/.test(r.name)) return { problems: [`name "${r.name}": lowercase letters, digits and single hyphens, up to 64`] };
          if (!r.text.trim()) return { problems: ["the rule is empty: say what to check"] };
          const i = fakeRules.findIndex((x) => x.name === previousName);
          const next = { name: r.name, when: r.when, action: r.action, order: r.order, agents: r.agents, description: r.description, paths: r.paths, text: r.text };
          if (i >= 0) fakeRules[i] = next;
          else fakeRules.push(next);
          return { rule: fakeRuleList(workspace).find((x) => x.name === r.name) };
        },
        deleteRule: ({ name }) => {
          const i = fakeRules.findIndex((x) => x.name === name);
          if (i < 0) throw new ConnectError(`no rule ${name}`, Code.NotFound);
          fakeRules.splice(i, 1);
          return {};
        },
        reorderRules: ({ names }) => {
          names.forEach((n, i) => {
            const r = fakeRules.find((x) => x.name === n);
            if (r) r.order = (i + 1) * 10;
          });
          return {};
        },
        tryRule: async ({ sample }) => (await new Promise((r) => setTimeout(r, 700)), /test/i.test(sample) ? { pass: true, reason: "It names the tests it ran." } : { pass: false, reason: "It doesn't say how the change was tested." }),
        // Studio › Skills: the workspace's own skills, their files, copies.
        getSkillFile: ({ workspace, name }) => {
          const x = fakeProjectSkills.find((k) => k.name === name);
          if (x) return { file: fakeSkillFile(workspace, x) };
          const o = fakeOtherSkills.find((k) => k.name === name);
          if (!o) throw new ConnectError(`no skill ${name}`, Code.NotFound);
          return { file: { skill: o, path: `${o.dir}/SKILL.md`, spec: { name: o.name, description: o.description, instructions: "Read the file, then summarize it." } } };
        },
        saveSkill: ({ workspace, previousName, skill }) => {
          const sp = skill!;
          if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(sp.name)) return { problems: [`name "${sp.name}": lowercase letters, digits and single hyphens, up to 64`] };
          if (!sp.description.trim()) return { problems: ["description: say what the skill is for and when to use it"] };
          if (sp.name !== previousName && fakeProjectSkills.some((k) => k.name === sp.name)) return { problems: [`name "${sp.name}": the workspace has a skill by that name`] };
          let x = fakeProjectSkills.find((k) => k.name === previousName);
          if (!x) fakeProjectSkills.push((x = { name: sp.name, description: "", tools: "", agents: [], members: [], instructions: "", files: {} }));
          Object.assign(x, { name: sp.name, description: sp.description, tools: sp.tools, agents: sp.agents, members: sp.members, instructions: sp.instructions, triggers: sp.triggers });
          return { file: fakeSkillFile(workspace, x) };
        },
        listSkillScenarios: ({ workspace, skill }) => ({ scenarios: (fakeProjectSkills.find((k) => k.name === skill)?.scenarios ?? []).map((sc) => ({ ...sc, path: `${workspace}/.agents/skills/${skill}/scenarios/${sc.name}.md`, problem: "" })) }),
        saveSkillScenario: ({ workspace, skill, previousName, scenario }) => {
          const x = fakeProjectSkills.find((k) => k.name === skill);
          if (!x) throw new ConnectError(`no skill ${skill}`, Code.NotFound);
          const sc = scenario!;
          const probs = [!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(sc.name) && `name "${sc.name}": lowercase letters, digits and single hyphens, up to 64`, !sc.prompt.trim() && "prompt: what to ask the agent", !sc.outcome.trim() && "outcome: the answer expected"].filter(Boolean) as string[];
          if (probs.length) return { problems: probs };
          const list = (x.scenarios ??= []);
          const next = { name: sc.name, description: sc.description, prompt: sc.prompt, executes: sc.executes, expectedSkills: sc.expectedSkills, threshold: sc.threshold, outcome: sc.outcome };
          const i = list.findIndex((k) => k.name === (previousName || sc.name));
          if (i >= 0) list[i] = next;
          else list.push(next);
          list.sort((a, b) => a.name.localeCompare(b.name));
          return { scenario: { ...next, path: `${workspace}/.agents/skills/${skill}/scenarios/${sc.name}.md`, problem: "" } };
        },
        deleteSkillScenario: ({ skill, name }) => {
          const x = fakeProjectSkills.find((k) => k.name === skill);
          const i = x?.scenarios?.findIndex((k) => k.name === name) ?? -1;
          if (!x || i < 0) throw new ConnectError(`no scenario ${name}`, Code.NotFound);
          x.scenarios!.splice(i, 1);
          return {};
        },
        // Tried once: it uses the skill when the prompt is near one of its trigger phrases (or it's given it).
        trySkill: async ({ skill, agent, prompt, force }) => {
          await new Promise((r) => setTimeout(r, 900));
          const x = fakeProjectSkills.find((k) => k.name === skill);
          const words = prompt.toLowerCase().split(/\W+/);
          const near = (x?.triggers ?? []).some((p) => p.toLowerCase().split(/\W+/).filter((w) => w.length > 3).filter((w) => words.some((v) => v.startsWith(w.slice(0, 5)))).length >= 2);
          const triggered = force || near;
          return { agent: agent || x?.agents[0] || "blitz", output: triggered ? "## What's new in v2.4\n- Wishlists you can share with a link.\n- Checkout is a step shorter." : "I can help with that: what would you like to know?", skillsUsed: triggered ? [skill] : [], triggered, forced: force, refusals: [], costUsd: 0.0024, duration: { seconds: 3n, nanos: 0 } };
        },
        deleteSkill: ({ name }) => {
          const i = fakeProjectSkills.findIndex((k) => k.name === name);
          if (i < 0) throw new ConnectError(`no skill ${name}`, Code.NotFound);
          fakeProjectSkills.splice(i, 1);
          return {};
        },
        readSkillFile: ({ name, path }) => ({ content: new TextEncoder().encode(fakeProjectSkills.find((k) => k.name === name)?.files[path] ?? "") }),
        writeSkillFile: ({ workspace, name, path, content }) => {
          const x = fakeProjectSkills.find((k) => k.name === name)!;
          if (!/^((references|scripts)\/[^.]|(requirements\.txt|pyproject\.toml|package\.json)$)/.test(path) || path.includes("..")) throw new ConnectError(`"${path}": a skill's files go in references/ or scripts/`, Code.InvalidArgument);
          x.files[path] = new TextDecoder().decode(content);
          return { file: fakeSkillFile(workspace, x) };
        },
        deleteSkillFile: ({ workspace, name, path }) => {
          const x = fakeProjectSkills.find((k) => k.name === name)!;
          delete x.files[path];
          return { file: fakeSkillFile(workspace, x) };
        },
        copySkill: ({ workspace, from, name }) => {
          if (fakeProjectSkills.some((k) => k.name === name)) return { problems: [`name "${name}": the workspace has a skill by that name`] };
          const o = [...fakeOtherSkills, ...fakeProjectSkills].find((k) => k.name === from)!;
          const x = { name, description: o.description, tools: "", agents: [], members: [], instructions: "Read the file, then summarize it.", files: {} };
          fakeProjectSkills.push(x);
          return { file: fakeSkillFile(workspace, x) };
        },
        runSkillScript: async ({ script, args }) => (await new Promise((r) => setTimeout(r, 900)),  ({ exitCode: 0, stdout: `${script}: ${args.length ? args.join(" ") : "no arguments"}\n3 entries since v0.7.0\n`, stderr: "", sandbox: "seatbelt" })),
        getSettings: ({ workspace }) => {
          const s = state(workspace).settings;
          return { agent: s.agent, model: s.model, provider: s.provider, effort: s.effort, permissionMode: s.mode, agency: s.agency, locale: s.locale, style: s.style, imagesEnabled: true, speechModel: s.speechModel ?? "", acceptedMedia: fakeAccepted(s.provider) };
        },
        // ?nomodel: the model can't be reached (the chat's warning card).
        getModel: ({ workspace }) => ({
          name: state(workspace).settings.model,
          provider: state(workspace).settings.provider,
          unavailable: new URLSearchParams(location.search).has("nomodel") ? "the Gemini API key is missing (set GEMINI_API_KEY)" : "",
        }),
        setModel: ({ workspace, ref }) => {
          const [provider, name] = ref.includes("/") ? ref.split("/", 2) : [state(workspace).settings.provider, ref];
          Object.assign(state(workspace).settings, { provider, model: name });
          return {};
        },
        listAgents: ({ workspace }) => ({
          agents: [
            { name: "blitz", displayName: "Blitz", description: "General coding agent", active: state(workspace).settings.agent === "blitz" },
            { name: "qa", displayName: "QA", description: "Testing specialist", active: state(workspace).settings.agent === "qa", pinnedModel: "anthropic/claude-sonnet-5" },
            { name: "planning-agent", displayName: "Planner", description: "Plans before changing", active: false },
          ],
        }),
        setAgent: ({ workspace, name }) => {
          state(workspace).settings.agent = name;
          return {};
        },
        listAgentFiles: ({ workspace, scope }) => {
          const dir = agentFolder(workspace, scope);
          return { dir, files: [...fakeAgentFiles.get(dir)!.values()].sort((a, b) => (a.path ?? "").localeCompare(b.path ?? "")) };
        },
        saveAgentFile: ({ workspace, scope, previousName, agent }) => saveAgent(workspace, scope, previousName, agent!),
        // The active agent's tools, or with all every tool an agent can be given.
        listTools: ({ workspace, all }) => {
          const mine = ["list_files", "glob", "read_file", "view_image", "grep", "create_file", "edit", "replace_in_file", "delete_snippet", "run_shell_command", "manage_background_process", "ask_user_question", "invoke_agent", "list_agents", "todo_write"];
          const more = ["web_fetch", "web_search", "notebook_edit", "browser_navigate", "browser_screenshot", "lsp_definition", "lsp_references"];
          return {
            agent: all ? "" : state(workspace).settings.agent,
            tools: (all ? [...mine, ...more] : mine).map((name) => ({ name, description: name.replace(/_/g, " "), planAllowed: !/create|edit|replace|delete|shell|notebook/.test(name) })),
          };
        },
        deleteAgentFile: ({ workspace, scope, name, path: byPath }) => {
          const files = fakeAgentFiles.get(agentFolder(workspace, scope))!;
          const path = byPath ? (files.has(byPath) ? byPath : undefined) : [...files].find(([, f]) => f.agent?.name === name)?.[0];
          if (!path) throw new ConnectError(`no agent ${name || byPath}`, Code.NotFound);
          files.delete(path);
          return {};
        },
        setSetting: ({ workspace, key, value }) => {
          const s = state(workspace).settings;
          if (key === "effort") s.effort = value === "auto" ? "" : value;
          if (key === "agency") s.agency = value;
          if (key === "style") s.style = value;
          return { key };
        },
        setPermissionMode: ({ workspace, mode }) => {
          if (mode === "bypass") {
            const err = new ConnectError("bypass needs the OS sandbox", Code.FailedPrecondition);
            err.details.push({ desc: ErrorInfoSchema, value: { reason: "BYPASS_NEEDS_SANDBOX" } });
            throw err;
          }
          state(workspace).settings.mode = mode;
          return { mode };
        },
        getModelSettings: ({ ref }) =>
          ref
            ? { model: { model: ref.split("/").pop(), provider: "gemini", settings: { temperature: 0.3 }, globalTemperature: 0.2, globalMaxTokens: 65536 } }
            : { all: { "gpt-5": { temperature: 1 }, "claude-opus-5-5": { reasoningEffort: "high" } } },
        updateModelSettings: ({ ref }) => ({ model: { model: ref, settings: {} }, unsupported: [] }),
        listStyles: ({ workspace }) => ({
          styles: [
            { name: "concise", description: "Short answers; details only when asked", source: "built-in" },
            { name: "default", description: "No style: the agent's own way of answering", source: "built-in" },
            { name: "explanatory", description: "Explains choices and the codebase as it works, to learn from", source: "built-in" },
          ].map((st) => ({ ...st, active: st.name === state(workspace).settings.style })),
        }),
        listLocales: () => ({ locales: [{ tag: "en-US", name: "English" }, { tag: "es", name: "Español" }, { tag: "fr-CA", name: "Français (Canada)" }] }),
        setLocale: ({ input }) => ({ tag: input }),
        listPermissionRules: () => ({
          rules: [
            { effect: "allow", rule: "shell(go test *)", source: "config" },
            { effect: "ask", rule: "shell(git push *)", source: "config" },
            { effect: "deny", rule: "read(.env*)", source: "session" },
          ],
        }),
        addPermissionRule: ({ rule }) => ({ rule }),
        removePermissionRule: ({ rule }) => ({ rule }),
        listApprovals: () => ({ approvals: [{ key: "k1", kind: "write", subject: "internal/cart", always: false }] }),
        revokeApprovals: () => ({ revoked: 1 }),
        searchWorkspace: ({ query, sources }) => {
          const words = query.toLowerCase().split(/\s+/).filter(Boolean);
          const want = sources.length ? sources : ["files", "documents"];
          return { sources: want, hits: fakeSearchCorpus.filter((h) => want.includes(h.source) && words.every((w) => `${h.ref} ${h.title} ${h.snippet}`.toLowerCase().includes(w))) };
        },
        getSearchStatus: ({ workspace }) => {
          const settings = searchSettingsOf(workspace);
          const on = settings.enabled;
          return {
            status: {
              enabled: on,
              items: (on ? { files: 42, documents: 3, chats: 7, notes: 2 } : {}) as Record<string, number>,
              scanning: false,
              lastScan: on ? now() : undefined,
              unreadable: on ? 1 : 0,
              error: "",
              embeddingModel: on ? settings.embeddingModel : "",
              embedded: on && settings.embeddingModel ? 318 : 0,
              embedError: "",
              enriched: on && settings.enrich ? 37 : 0,
              enrichError: "",
              problem: "",
              settings,
            },
          };
        },
        reindex: () => ({}),
        getProjectSettings: ({ workspace }) => ({ settings: fakeProject(state(workspace)) }),
        listTasks: ({ workspace, sessionIds }) => ({
          tasks: [...state(workspace).tasks.keys()].map((id) => fakeTask(state(workspace), id)).filter((t) => sessionIds.includes(t.sessionId)),
        }),
        getTask: ({ workspace, id }) => {
          if (!state(workspace).tasks.has(id)) notFound(id);
          const t = fakeTask(state(workspace), id);
          const events = ["→ read_file {\"path\":\"internal/cart/cart.go\"}", "← read_file", "→ grep {\"pattern\":\"math.Round\"}", "← grep"];
          return { task: t, events: t.state === "running" ? events.slice(0, 2) : events };
        },
        watchTasks: async function* ({ workspace, sessionIds }, ctx) {
          yield { event: { kind: { case: "ready", value: true } } };
          const asked = new Set<string>();
          const ended = new Set<string>();
          while (!ctx.signal.aborted) {
            await sleep(500);
            const s = state(workspace);
            for (const id of s.tasks.keys()) {
              const t = fakeTask(s, id);
              if (!sessionIds.includes(t.sessionId)) continue;
              if (t.state === "waiting" && !asked.has(id)) {
                asked.add(id);
                yield {
                  event: {
                    kind: {
                      case: "approvalRequest",
                      value: { requestId: `${id}-req`, tool: "run_shell_command", kind: ActionKind.COMMAND, detail: "go test ./internal/cart/...", scopeLabel: "this exact command in this workspace", agent: t.agent, taskId: id },
                    },
                  },
                };
              }
              if (asked.has(id) && t.state !== "waiting" && !ended.has(`${id}-req`)) {
                ended.add(`${id}-req`);
                yield { event: { kind: { case: "resolved", value: `${id}-req` } } };
              }
              if (["done", "stopped"].includes(t.state) && !ended.has(id)) {
                ended.add(id);
                yield { event: { kind: { case: "task", value: t } } };
              }
            }
          }
        },
        stopTask: ({ workspace, id }) => {
          const t = state(workspace).tasks.get(id);
          if (!t) notFound(id);
          t.stopped ??= Date.now();
          return { task: fakeTask(state(workspace), id) };
        },
        trustProject: ({ workspace, trusted }) => {
          state(workspace).projectTrust = trusted ? "trusted" : "declined";
          return { reopened: true };
        },
        closeWorkspace: () => ({}),
        loadImage: ({ path }) => ({ image: { id: `img-${path}`, name: path, mime: "image/png", width: 640, height: 480, size: 12345n } }),
        // ?oldservice: a service older than the app (the stale banner).
        getServiceInfo: () => ({ version: new URLSearchParams(location.search).has("oldservice") ? "0.6.1" : "dev", executable: "" }),
        listLogDays: () => ({ days: [...fakeLogDays], dir: "~/.blitz/logs" }),
        deleteLogDay: ({ day }) => {
          fakeLogDays = fakeLogDays.filter((d) => d !== day);
          return {};
        },
        readLog: ({ day, minLevel, text }) => {
          const d = day || "2026-09-28";
          const rank: Record<string, number> = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 };
          const min = rank[(minLevel || "debug").toUpperCase()];
          const all = [
            { time: timestampFromDate(new Date(`${d}T09:14:02.120`)), level: "INFO", message: "start", attrs: [{ key: "version", value: "dev" }, { key: "provider", value: "gemini" }] },
            { time: timestampFromDate(new Date(`${d}T09:14:09.803`)), level: "INFO", message: "workspace opened", attrs: [{ key: "workspace", value: "/home/me/blitz" }] },
            { time: timestampFromDate(new Date(`${d}T09:20:41.017`)), level: "WARN", message: "model unavailable", attrs: [{ key: "workspace", value: "/home/me/blitz" }, { key: "error", value: "quota exceeded: Quota exceeded for aiplatform.googleapis.com/online_prediction_requests_per_base_model" }] },
            { time: timestampFromDate(new Date(`${d}T09:21:03.500`)), level: "ERROR", message: "turn failed", attrs: [{ key: "error", value: "the model isn't available" }, { key: "attempt", value: "2" }] },
          ];
          const words = text.toLowerCase().split(/\s+/).filter(Boolean);
          const entries = all
            .filter((e) => rank[e.level] >= min && words.every((w) => `${e.level} ${e.message} ${e.attrs.map((a) => `${a.key}=${a.value}`).join(" ")}`.toLowerCase().includes(w)))
            .reverse();
          return { day: d, path: `~/.blitz/logs/blitz-${d}.jsonl`, entries, matched: entries.length };
        },
        listWorkspaces: () => ({ workspaces: [...states.keys()] }),
        getSuggestions: async ({ workspace }) => {
          await sleep(150);
          return fakeSuggestions(workspace);
        },
        listCommands: () => ({
          commands: [
            { name: "setup", description: "Set up the agent harness: .agents/AGENTS.md, skills and agents", source: "bundled" },
            { name: "spec", description: "Write or revise the project's specification, as numbered specs in .agents/specs/", argumentHint: "[what to build, or what changed]", source: "bundled" },
            { name: "skills-update", description: "Revisit this workspace's skills against its specification; also /skills update", argumentHint: "[what to focus on]", source: "bundled" },
            { name: "agents-update", description: "Revisit this workspace's agents against its specification; also /agents update", argumentHint: "[what to focus on]", source: "bundled" },
            { name: "rules-update", description: "Revisit this workspace's rules and guardrails against its specification and runs; also /rules update", argumentHint: "[what to focus on]", source: "bundled" },
            { name: "reflect", description: "Learn from this workspace's runs: what went wrong, the patterns, and what to change in its harness", argumentHint: "[since: a date or a number of days]", source: "bundled" },
            { name: "review", description: "Review the uncommitted changes for bugs", argumentHint: "[branch | files]", source: "bundled" },
            { name: "db:migrate", description: "Write a database migration", argumentHint: "<name>", source: "project" },
          ],
        }),
        searchWeb: ({ terms }) => ({ provider: "Google", links: [{ title: `${terms} — docs`, url: "https://example.com/docs" }], prompt: "Read these pages…" }),
        addImage: async ({ name, data }) => {
          await sleep(400);
          return { image: { id: `img-${data.length}`, name, mimeType: "image/png", width: 1280, height: 720, size: BigInt(data.length), resized: false } };
        },
        listCheckpoints: () => ({ checkpoints: [{ id: 2, label: "Fix the coupon rounding", time: now(), files: ["internal/cart/discount.go", "internal/cart/discount_test.go"] }] }),
        undo: () => ({ label: "Fix the coupon rounding", restored: ["internal/cart/discount.go"] }),
        // ?fake&norepo: a folder that isn't a repository yet.
        getGitStatus: () => fakeGitStatus(),
        fetchRemote: async () => {
          await sleep(300);
          return { status: fakeGitStatus() };
        },
        pull: async () => {
          await sleep(700);
          const commits = fakeRemote.behind;
          fakeRemote.behind = 0;
          fakeRemote.operation = GitOperation.MERGE;
          fakeRemote.conflicts = ["internal/cart/discount.go"];
          fakeRemote.merging = fakeRemote.upstream;
          return { outcome: PullOutcome.CONFLICTS, commits, status: fakeGitStatus() };
        },
        push: async () => {
          await sleep(600);
          if (fakeRemote.behind > 0) throw fakeError("the remote has commits this branch doesn't have: pull them first", "PUSH_REJECTED");
          const target = fakeRemote.upstream || "origin/fix/coupon-rounding";
          fakeRemote.upstream = target;
          fakeRemote.ahead = 0;
          return { target, status: fakeGitStatus() };
        },
        resolveConflict: ({ path, choice }) => {
          if (choice === ConflictChoice.RESOLVED) throw fakeError("the file still has conflict markers", "MARKERS_REMAIN");
          fakeRemote.conflicts = fakeRemote.conflicts.filter((c) => c !== path);
          return { status: fakeGitStatus() };
        },
        completeMerge: async () => {
          await sleep(400);
          fakeRemote.operation = GitOperation.UNSPECIFIED;
          fakeRemote.merging = "";
          fakeRemote.ahead += 1;
          return { status: fakeGitStatus() };
        },
        abortMerge: () => {
          fakeRemote.operation = GitOperation.UNSPECIFIED;
          fakeRemote.conflicts = [];
          fakeRemote.merging = "";
          fakeRemote.behind = 2;
          return { status: fakeGitStatus() };
        },
        listBranches: async () => {
          await sleep(200);
          return { branches: fakeBranches.map((b, i) => ({ ...b, remote: !!b.remote, current: b.name === fakeBranch, time: timestampFromDate(new Date(Date.now() - i * 86_400_000)) })) };
        },
        switchBranch: async ({ branch }) => {
          await sleep(300);
          if (fakeRemote.operation !== GitOperation.UNSPECIFIED) throw fakeError("a merge or rebase is in progress: complete or abort it first", "GIT_BUSY");
          if (!fakeBranches.some((b) => b.name === branch)) throw fakeError("there's no such branch", "NO_BRANCH");
          const local = branch.replace(/^origin\//, "");
          if (local !== branch && !fakeBranches.some((b) => b.name === local)) fakeBranches.push({ name: local, subject: fakeBranches.find((b) => b.name === branch)!.subject, upstream: branch });
          fakeBranch = local;
          return { status: fakeGitStatus() };
        },
        createBranch: async ({ branch }) => {
          await sleep(300);
          if (!branch || /\s|\.\.|^-/.test(branch)) throw fakeError("that isn't a valid branch name", "BAD_BRANCH_NAME");
          if (fakeBranches.some((b) => b.name === branch)) throw fakeError("a branch with that name already exists", "BRANCH_EXISTS");
          fakeBranches.unshift({ name: branch, subject: fakeBranches.find((b) => b.name === fakeBranch)?.subject ?? "", upstream: "" });
          fakeBranch = branch;
          return { status: fakeGitStatus() };
        },
        mergeBranch: async ({ branch }) => {
          await sleep(500);
          if (branch === fakeBranch) throw fakeError("that's the current branch", "MERGE_SELF");
          if (branch === "spike/lazy-totals") {
            fakeRemote.operation = GitOperation.MERGE;
            fakeRemote.conflicts = ["internal/cart/totals.go"];
            fakeRemote.merging = branch;
            return { outcome: PullOutcome.CONFLICTS, commits: 3, status: fakeGitStatus() };
          }
          fakeRemote.ahead += 2;
          return { outcome: PullOutcome.FAST_FORWARD, commits: 2, status: fakeGitStatus() };
        },
        reloadAgentFiles: async () => {
          await sleep(300);
          return { agents: 2, skills: 3, files: ["AGENTS.md", ".agents/rules/style.md"], problems: [] };
        },
        // The commit dialog: nothing staged until "Stage all changes".
        draftCommit: async ({ stageAll }) => {
          if (stageAll) fakeStaged = true;
          if (!fakeStaged) return { files: [], message: "", problem: "" };
          await sleep(900);
          return {
            files: [
              { status: "M", path: "internal/cart/discount.go" },
              { status: "A", path: "internal/cart/discount_test.go" },
            ],
            message: "Fix coupon rounding: round the total, not each line\n\nApplyCoupon rounded every line item before summing, so a cart could\nbe off by a cent. Sum first, then apply the discount and round once.\n\n- Add TestApplyCouponRounding for the 333/333/334 cart",
            problem: "",
          };
        },
        commit: async ({ message }) => {
          await sleep(500);
          fakeStaged = false;
          return { hash: "3f2a9c1", subject: message.split("\n")[0] };
        },
        installSecretHook: ({ workspace }) => ({ status: "installed", path: `${workspace}/.githooks/pre-commit`, detail: "" }),
        initGitRepo: () => {
          fakeRepo = true;
          return { status: { git: true, repo: true, branch: "main", changed: 0 } };
        },
        getDiff: ({ git }) => ({
          diff: git
            ? fakeRemote.conflicts.length
              ? "--- a/internal/cart/discount.go\n+++ b/internal/cart/discount.go\n@@ -40,7 +40,13 @@ func ApplyCoupon(items []Item, c Coupon) int {\n \ttotal := 0\n \tfor _, it := range items {\n+<<<<<<< HEAD\n \t\ttotal += it.Cents\n+=======\n+\t\ttotal += it.Cents * it.Qty\n+>>>>>>> origin/fix/coupon-rounding\n \t}\n"
              : ""
            : "--- a/internal/cart/discount.go\n+++ b/internal/cart/discount.go\n@@ -40,9 +40,9 @@ func ApplyCoupon(items []Item, c Coupon) int {\n \ttotal := 0\n \tfor _, it := range items {\n-\t\ttotal += round(it.Cents * (100 - c.Percent) / 100)\n+\t\ttotal += it.Cents\n \t}\n-\treturn total\n+\treturn round(total * (100 - c.Percent) / 100)\n }\n--- /dev/null\n+++ b/internal/cart/discount_test.go\n@@ -0,0 +1,6 @@\n+package cart\n+\n+import \"testing\"\n+\n+func TestApplyCouponRounding(t *testing.T) {\n+}\n",
        }),
      });
      service(ConfigService, {
        // Reviewing a skill's scripts: trusted at the content shown.
        trustSkillScripts: ({ dir, hash }) => (fakeTrusted.set(dir.split("/").pop()!, hash), {}),
        getSkillReview: () => ({}),
        listSkillRegistries: () => ({ registries: fakeRegistries, installed: fakeInstalledSkills }),
        listSecrets: ({ workspace }) => ({ secrets: (fakeSecrets.get(workspace) ?? []).map((key) => ({ key, updated: { seconds: BigInt(1760700000), nanos: 0 } })), store: "macOS Keychain" }),
        setSecret: ({ workspace, key }) => {
          const l = fakeSecrets.get(workspace) ?? [];
          if (!l.includes(key)) fakeSecrets.set(workspace, [...l, key].sort());
          return {};
        },
        deleteSecret: ({ workspace, key }) => {
          fakeSecrets.set(workspace, (fakeSecrets.get(workspace) ?? []).filter((k) => k !== key));
          return {};
        },
        listBuiltinSkills: () => ({
          skills: [
            { name: "code-review", description: "Review a change for bugs, style and tests", version: "1.0.0" },
            { name: "git-workflow", description: "Branches, commits and pull requests the team's way", version: "1.0.0" },
            { name: "testing-tdd", description: "Write the failing test first, then the code", version: "1.0.0" },
          ],
        }),
        listMCPServerConfigs: () => ({ servers: fakeMCP, path: "~/.blitz/.env.toml" }),
        // Settings › Connectors: the global connections.
        listGlobalConnectors: () => ({
          categories: [{ ...fakeConnectorCategory, workspaceDir: "", state: "", toolCount: 0 }],
          kinds: fakeConnectorKinds,
          connections: fakeConnectors.filter((d) => d.scope === "global").map((d) => fakeConnectorMsg("", { ...d, enabled: true })),
          globalDir: "~/.blitz/connectors",
        }),
        saveGlobalConnector: ({ connection, previousName, secretValues }) => fakeConnectorCalls.save("", { ...connection!, scope: "global" }, previousName, secretValues),
        deleteGlobalConnector: ({ category, name }) => {
          const i = fakeConnectors.findIndex((d) => d.name === name && d.scope === "global" && d.category === category);
          if (i < 0) throw new ConnectError(`no connection ${name}`, Code.NotFound);
          fakeConnectors.splice(i, 1);
          return {};
        },
        testGlobalConnector: ({ connection }) => fakeConnectorCalls.test(connection!),
        tryGlobalConnectorTool: ({ connection, tool }) => fakeConnectorCalls.tryTool(connection!, tool),
        readGlobalConnectorSchema: ({ connection }) => fakeConnectorCalls.readSchema(connection!),
        draftGlobalConnectorQuery: ({ request }) => fakeConnectorCalls.draft(request),
        saveMCPServer: ({ name, server }) => {
          const i = fakeMCP.findIndex((m) => m.name === name);
          if (!name && fakeMCP.some((m) => m.name === server?.name)) throw new Error(`an MCP server named "${server?.name}" is already configured`);
          if (name && i < 0) throw new Error(`no MCP server by that name: ${name}`);
          if (i >= 0) fakeMCP[i] = server!;
          else fakeMCP.push(server!);
          return { change: { path: "~/.blitz/.env.toml" } };
        },
        removeMCPServer: ({ name }) => {
          fakeMCP.splice(fakeMCP.findIndex((m) => m.name === name), 1);
          return { change: { path: "~/.blitz/.env.toml" } };
        },
        setMCPServerEnabled: ({ name, enabled }) => {
          const m = fakeMCP.find((x) => x.name === name);
          if (m) m.disabled = !enabled;
          return { change: { path: "~/.blitz/.env.toml" } };
        },
        addSkillRegistry: ({ url, name, token }) => {
          if (!/^https:\/\//.test(url) && !url.startsWith("/")) throw new ConnectError(`${url}: a registry needs https (http only for localhost)`, Code.FailedPrecondition);
          const reg = { name: name || "castor", url, hasToken: !!token };
          fakeRegistries.splice(0, fakeRegistries.length, ...fakeRegistries.filter((r) => r.name !== reg.name), reg);
          return { registry: reg, skills: fakeRegistrySkills.length };
        },
        removeSkillRegistry: ({ name }) => {
          fakeRegistries.splice(0, fakeRegistries.length, ...fakeRegistries.filter((r) => r.name !== name));
          return {};
        },
        searchRegistrySkills: ({ query }) => ({
          skills: fakeRegistries.length ? fakeRegistrySkills.filter((k) => query.toLowerCase().split(/\s+/).every((w) => (k.name + " " + k.description).toLowerCase().includes(w))) : [],
          problems: [],
        }),
        installRegistrySkill: ({ ref, workspace }) => {
          const [registry, name] = ref.includes("/") ? ref.split("/") : ["castor", ref];
          const k = fakeRegistrySkills.find((x) => x.name === name);
          if (!k) throw new ConnectError(`no registry offers that skill: ${ref}`, Code.NotFound);
          const skill = { name, registry, version: k.version, dir: workspace ? `${workspace}/.agents/skills/${name}` : `/home/me/.blitz/skills/${name}`, installed: now() };
          fakeInstalledSkills.splice(0, fakeInstalledSkills.length, ...fakeInstalledSkills.filter((i) => i.dir !== skill.dir), skill);
          return { skill };
        },
        updateRegistrySkills: () => ({ updated: [], problems: [] }),
        uninstallRegistrySkill: ({ name }) => {
          const i = fakeInstalledSkills.findIndex((x) => x.name === name);
          const [skill] = fakeInstalledSkills.splice(i, 1);
          return { skill };
        },
        getInterfaceLanguage: () => ({ locale: "", catalogs: [] }),
        // Sign-ins: gcloud and aws "installed", ant and az not.
        getSignIn: ({ provider }) => ({
          statuses: (provider ? [provider] : ["google", "anthropic", "aws", "azure"]).map((p) => fakeSignIn(p)),
        }),
        signIn: async function* ({ provider }) {
          yield { line: "Your browser has been opened to visit:" };
          await sleep(500);
          yield { line: "https://accounts.example.com/o/oauth2/auth?client_id=blitz", url: "https://accounts.example.com/o/oauth2/auth?client_id=blitz" };
          if (provider === "aws") yield { line: "Then enter the code: WXYZ-1234", code: "WXYZ-1234" };
          await sleep(1500);
          fakeSignedIn.add(provider);
          yield { done: true, status: fakeSignIn(provider) };
        },
        signOut: ({ provider }) => {
          fakeSignedIn.delete(provider);
          return { status: fakeSignIn(provider) };
        },
        listModels: () => ({
          defaultProvider: "gemini",
          providers: [
            { provider: "gemini", ids: ["gemini-3.5-flash-lite", "gemini-3.8-flash", "gemini-3.8-pro", "gemini-4-argon"] },
            { provider: "anthropic", error: "no API key" },
          ],
        }),
        describeConfig: ({ workspace }) => {
          const c = scopeConfig(workspace);
          return {
            path: configPath(workspace),
            provider: c.provider,
            defaultModel: c.model,
            secretStore: "macOS Keychain",
            providers: Object.entries(c.keys).map(([name, keySource]) => {
              const a = c.auth[name];
              return { name, keySource, keyMissing: false, baseUrl: "", model: "", auth: a?.method ?? "", projectId: a?.projectId ?? "", location: a?.location ?? "", profile: a?.profile ?? "" };
            }),
          };
        },
        setApiKey: ({ workspace, provider }) => {
          scopeConfig(workspace).keys[provider] = KeySource.KEYCHAIN;
          return change(workspace);
        },
        secureApiKey: ({ workspace, provider }) => {
          scopeConfig(workspace).keys[provider] = KeySource.KEYCHAIN;
          return change(workspace);
        },
        removeApiKey: ({ workspace, provider }) => {
          scopeConfig(workspace).keys[provider] = workspace ? KeySource.INHERITED : KeySource.NONE;
          return change(workspace);
        },
        setProvider: ({ workspace, provider, defaultModel, key, auth }) => {
          if (key && !["gemini", "anthropic", "openai"].includes(provider)) throw new ConnectError(`${provider || "no provider"} takes no API key`, Code.InvalidArgument);
          const method = auth?.method === "api_key" ? "" : (auth?.method ?? "");
          if (method && !({ gemini: ["adc"], anthropic: ["oauth", "adc"] }[provider] ?? []).includes(method)) throw new ConnectError(`${provider} doesn't sign in with ${method}`, Code.InvalidArgument);
          const c = scopeConfig(workspace);
          c.provider = provider;
          c.model = defaultModel;
          if (key) c.keys[provider] = KeySource.KEYCHAIN;
          if (auth) c.auth[provider] = { method, projectId: auth.projectId, location: auth.location, profile: auth.profile };
          return change(workspace);
        },
        setConfigValue: ({ workspace, key, value }) => {
          const c = scopeConfig(workspace);
          if (key === "llm.provider") c.provider = value;
          if (key === "blitz.default_model") c.model = value;
          if (key === "ui.task_alerts") fakeAlerts = value;
          if (key === "audio.model" && workspace) state(workspace).settings.speechModel = value;
          if (key.startsWith("search.") && workspace) {
            const s = searchSettingsOf(workspace);
            if (key === "search.enabled") s.enabled = value !== "false";
            if (key === "search.sources") s.sources = value ? value.split(",") : ["files", "documents"];
            if (key === "search.embedding_model") s.embeddingModel = value;
            if (key === "search.enrich") s.enrich = value === "true";
            if (key === "search.enrich_model") s.enrichModel = value;
            if (key === "search.enrich_daily_limit") s.enrichDailyLimit = Number(value) || 200;
            if (key === "search.include_ignored") s.includeIgnored = value === "true";
          }
          // Settings › Workers' limits.
          if (key === "workers.policy.allow") fakePolicy.allow = value === "none" ? [] : value ? value.split(",").map((k) => k.trim()) : ["shell", "write", "delete", "web", "mcp"];
          const limit = { max_concurrent: "maxConcurrent", default_max_turns: "defaultMaxTurns", max_turns: "maxTurns", default_max_cost_usd: "defaultMaxCostUsd", max_cost_usd: "maxCostUsd" }[key.replace("workers.policy.", "")] as "maxConcurrent" | undefined;
          if (key.startsWith("workers.policy.") && limit) fakePolicy[limit] = Number(value);
          if (key === "workers.policy.default_timeout") fakePolicy.defaultTimeout = value || "30m";
          if (key === "workers.policy.max_timeout") fakePolicy.maxTimeout = value || "2h";
          return change(workspace);
        },
        getConfigFile: ({ workspace }) => ({ path: configPath(workspace), text: scopeConfig(workspace).text }),
        describePermissions: ({ workspace }) => {
          const p = scopePermissions(workspace);
          const global = scopePermissions("");
          const on = p.defaults === "" ? global.defaults !== "off" : p.defaults === "on";
          return {
            path: configPath(workspace),
            rules: p.rules,
            inherited: workspace ? global.rules : [],
            readOnlyDefaults: p.defaults,
            readOnlyDefaultsOn: on,
            readOnlyCommands: ["ls", "pwd", "cat", "head", "tail", "wc", "stat", "du", "df", "which", "grep", "rg", "diff", "git status", "git log", "git show", "git diff", "git blame", "git rev-parse"],
            readOnlyGuards: ["git … --output", "git … --ext-diff", "rg … --pre"],
          };
        },
        checkPermission: ({ rule, sample }) => {
          // A stand-in for the service's parser: kind(pattern), prefix matching.
          const m = /^(shell|bash|write|delete|read|web|search|mcp|skill|agent)\((.+)\)$/i.exec(rule.trim());
          if (!m) return { error: `invalid permission rule "${rule}": use kind(pattern) or a tool name` };
          const kind = m[1].toLowerCase() === "bash" ? "shell" : m[1].toLowerCase();
          const pattern = m[2].trim();
          const form = kind !== "shell" ? (["read", "write", "delete"].includes(kind) ? "path" : "name") : pattern.startsWith("re:") ? "regex" : /[*?]/.test(pattern) ? "glob" : "prefix";
          const tested = !!sample.trim();
          const matches = tested && (form === "prefix" ? sample.trim() === pattern || sample.trim().startsWith(pattern + " ") : sample.includes(pattern.replace(/\*/g, "")));
          const redirect = / >>? *([^ ]+)/.exec(sample)?.[1] ?? "";
          return { rule: `${kind}(${pattern})`, kind, pattern, form, tested, matches, redirect };
        },
        addPermission: ({ workspace, effect, rule }) => {
          const p = scopePermissions(workspace);
          const canonical = rule.trim().replace(/^bash\(/i, "shell(");
          if (!p.rules.some((r) => r.rule === canonical)) p.rules.push({ effect, rule: canonical });
          return { ...change(workspace), rule: canonical };
        },
        removePermission: ({ workspace, rule }) => {
          const p = scopePermissions(workspace);
          const before = p.rules.length;
          p.rules = p.rules.filter((r) => r.rule !== rule);
          return { ...change(workspace), removed: before - p.rules.length };
        },
        setReadOnlyDefaults: ({ workspace, value }) => {
          scopePermissions(workspace).defaults = value;
          return change(workspace);
        },
        checkConfigFile: ({ text }) => {
          // A stand-in for the service's check: an unclosed table header is
          // an error, a setting it doesn't know a warning.
          const problems: { line: number; column: number; error: boolean; message: string }[] = [];
          text.split("\n").forEach((l, i) => {
            if (/^\s*\[[^\]]*$/.test(l)) problems.push({ line: i + 1, column: 1, error: true, message: "expected '.' or ']' to end table name" });
            else if (/^\s*(temprature|modle)\s*=/.test(l)) problems.push({ line: i + 1, column: 1, error: false, message: `unknown setting ${l.split("=")[0].trim()}` });
          });
          return { problems };
        },
        getSettingsReference: () => ({
          settings: [
            { key: "blitz", type: "table", default: "", doc: "BlitzConfig controls the core behaviour settings." },
            { key: "blitz.temperature", type: "number", default: "0.2", doc: "Temperature is the sampling temperature, unless [model_settings] sets one for the model." },
            { key: "llm", type: "table", default: "", doc: "LLMConfig holds provider configurations for LLM backends." },
            { key: "llm.provider", type: "string", default: '"gemini"', doc: "Provider is the model provider: gemini, openai, anthropic or ollama." },
            { key: "llm.max_retries", type: "integer", default: "3", doc: "MaxRetries is how many times a failed model call is tried again." },
            { key: "llm.stream", type: "boolean", default: "true", doc: "Stream shows the answer as it comes." },
            { key: "llm.gemini", type: "table", default: "", doc: "GeminiConfig configures the Gemini provider." },
            { key: "llm.gemini.model", type: "string", default: '"gemini-4-argon"', doc: "Model is the Gemini model to use." },
            { key: "model_settings.<model>.top_p", type: "number", default: "", doc: "TopP is nucleus sampling: only the most likely tokens up to this probability." },
          ],
        }),
        saveConfigFile: ({ workspace, text }) => {
          if (text.includes("[[")) throw new ConnectError("line 1: expected a table", Code.InvalidArgument);
          scopeConfig(workspace).text = text;
          return { ...change(workspace), warnings: text.includes("sk-") ? ["[llm.anthropic] api_key is written as plain text: set it in Providers & keys to keep it in the keychain"] : [] };
        },
      });
      service(FileService, {
        listDir: ({ path, showHidden }) => {
          const prefix = path ? path + "/" : "";
          const seen = new Map<string, boolean>();
          for (const p of fakeFiles.keys()) {
            if (!p.startsWith(prefix)) continue;
            const rest = p.slice(prefix.length);
            const name = rest.split("/")[0];
            seen.set(name, seen.get(name) || rest.includes("/"));
          }
          const entries = [...seen].map(([name, folder]) => {
            const p = prefix + name;
            const git = folder ? (Object.keys(fakeGit).some((g) => g.startsWith(p + "/")) ? "changed" : "") : (fakeGit[p] ?? "");
            return { name, path: p, kind: folder ? FileKind.FOLDER : FileKind.FILE, size: BigInt(fakeFiles.get(p)?.length ?? 0), git, hidden: fakeHidden(p), ignored: fakeIgnored(p), agentRule: p === ".env" ? "blocked" : "" };
          });
          entries.sort((a, b) => (a.kind !== b.kind ? (a.kind === FileKind.FOLDER ? -1 : 1) : a.name.localeCompare(b.name)));
          return { entries: showHidden ? entries : entries.filter((e) => !e.hidden), repo: true };
        },
        readPreview: ({ path }) => {
          if (!fakeFiles.has(path) && !path.startsWith(".agents/skills/")) notFound(path); // a skill's references preview too
          if (path.endsWith(".pdf")) {
            // A two-page PDF.
            const page = (n: number) => `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents ${n} 0 R /Resources << /Font << /F1 5 0 R >> >> >>`;
            const text = (s: string) => `<< /Length ${s.length} >>\nstream\n${s}\nendstream`;
            const objs = ["<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>", page(4), text("BT /F1 28 Tf 30 90 Td (Blitz spec, page 1) Tj ET"), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", page(7), text("BT /F1 28 Tf 30 90 Td (Page 2) Tj ET")];
            let out = "%PDF-1.4\n";
            const offs: number[] = [];
            objs.forEach((o, i) => {
              offs.push(out.length);
              out += `${i + 1} 0 obj\n${o}\nendobj\n`;
            });
            const x = out.length;
            out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n${offs.map((o) => `${String(o).padStart(10, "0")} 00000 n \n`).join("")}trailer\n<< /Size ${objs.length + 1} /Root 1 0 R >>\nstartxref\n${x}\n%%EOF\n`;
            return { path, mime: "application/pdf", data: new TextEncoder().encode(out) };
          }
          if (path.endsWith(".wav")) {
            // A second of a 440 Hz tone, 8 kHz mono.
            const rate = 8000;
            const pcm = new Int16Array(rate).map((_, i) => Math.round(8000 * Math.sin((2 * Math.PI * 440 * i) / rate)));
            const head = new DataView(new ArrayBuffer(44));
            const str = (o: number, s: string) => [...s].forEach((c, i) => head.setUint8(o + i, c.charCodeAt(0)));
            str(0, "RIFF");
            head.setUint32(4, 36 + pcm.byteLength, true);
            str(8, "WAVEfmt ");
            head.setUint32(16, 16, true); // fmt chunk size
            head.setUint16(20, 1, true); // PCM
            head.setUint16(22, 1, true); // mono
            head.setUint32(24, rate, true);
            head.setUint32(28, rate * 2, true); // bytes a second
            head.setUint16(32, 2, true); // block align
            head.setUint16(34, 16, true); // bits a sample
            str(36, "data");
            head.setUint32(40, pcm.byteLength, true);
            const data = new Uint8Array(44 + pcm.byteLength);
            data.set(new Uint8Array(head.buffer), 0);
            data.set(new Uint8Array(pcm.buffer), 44);
            return { path, mime: "audio/wav", data };
          }
          // A 2×2 PNG, whatever the file.
          const png = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAFklEQVR4nGP8z8DwnwEIGP8zMDAAAB0IAgBfJnXoAAAAAElFTkSuQmCC"), (c) => c.charCodeAt(0));
          return { path, mime: "image/png", data: png };
        },
        readFile: ({ path }) => {
          const text = fakeFiles.get(path);
          if (text === undefined) notFound(path);
          const binary = text.includes("\u0000");
          return { path, text: binary ? "" : text, version: fakeVersion(text), size: BigInt(text.length), binary, agentRule: path === ".env" ? "blocked" : "" };
        },
        writeFile: ({ path, text, version }) => {
          const now = fakeFiles.get(path);
          if ((now === undefined ? "" : fakeVersion(now)) !== version) {
            const err = new ConnectError(`${path} was changed since it was opened`, Code.FailedPrecondition);
            err.details.push({ desc: ErrorInfoSchema, value: { reason: "FILE_CHANGED", metadata: { current_version: now === undefined ? "" : fakeVersion(now) } } });
            throw err;
          }
          fakeFiles.set(path, text);
          return { version: fakeVersion(text) };
        },
        writeBinaryFile: ({ path, data, version }) => {
          const now = fakeFiles.get(path);
          if ((now === undefined ? "" : fakeVersion(now)) !== version) throw new ConnectError(`${path} was changed since it was opened`, Code.FailedPrecondition);
          const text = new TextDecoder().decode(data);
          fakeFiles.set(path, text);
          return { version: fakeVersion(text) };
        },
        uploadFile: ({ path, uploadId, data, done, replace }) => {
          const id = uploadId || `upload-${++fakeUploadSeq}`;
          const text = (fakeUploads.get(id) ?? "") + new TextDecoder().decode(data);
          if (!done) {
            fakeUploads.set(id, text);
            return { uploadId: id, version: "" };
          }
          fakeUploads.delete(id);
          if (fakeFiles.has(path) && !replace) throw new ConnectError(`${path}: file already exists`, Code.AlreadyExists);
          fakeFiles.set(path, text);
          return { uploadId: id, version: fakeVersion(text) };
        },
        createFolder: ({ path }) => {
          fakeFiles.set(path + "/.keep", "");
          return {};
        },
        renameFile: ({ from, to }) => {
          for (const [p, v] of [...fakeFiles]) {
            if (p === from || p.startsWith(from + "/")) {
              fakeFiles.delete(p);
              fakeFiles.set(to + p.slice(from.length), v);
            }
          }
          return {};
        },
        gitFileAction: ({ path, action }) => {
          // Recorded for checks in a browser; a discard drops the path's
          // changes, an ignore appends to .gitignore.
          ((window as unknown as { __gitActions?: unknown[] }).__gitActions ??= []).push({ path, action });
          if (action === GitAction.DISCARD) for (const g of Object.keys(fakeGit)) if (g === path || g.startsWith(path + "/")) delete fakeGit[g];
          if (action === GitAction.IGNORE) fakeFiles.set(".gitignore", (fakeFiles.get(".gitignore") ?? "") + `/${path}${fakeFiles.has(path) ? "" : "/"}\n`);
          if (action === GitAction.UNIGNORE) {
            const lines = (fakeFiles.get(".gitignore") ?? "").split("\n");
            const kept = lines.filter((l) => l !== `/${path}` && l !== `/${path}/`);
            if (kept.length === lines.length && fakeIgnored(path)) {
              const [pattern, line] = path.startsWith("bin") ? ["bin/", 1] : ["*.log", 2];
              throw new ConnectError(`${path} is ignored by \`${pattern}\` (.gitignore:${line}), not by a line Blitz added: edit that file to change it.`, Code.FailedPrecondition);
            }
            fakeFiles.set(".gitignore", kept.join("\n"));
          }
          return {};
        },
        getFileHistory: ({ path, skip }) => ({ revisions: skip ? [] : fakeRevisions(path), more: false }),
        getFileRevisionDiff: ({ path, commit, against, otherCommit }) => {
          const file = fakeFiles.has(path) ? path : "internal/cart/discount.go";
          const now = fakeFiles.get(file) ?? "";
          if (against === RevisionDiff.NOW && commit === fakeRevisions(path)[0].commit) return { diff: "" };
          const then = fakeOldText(now);
          const add = now.split("\n").slice(3).filter((l) => l !== "");
          const note = against === RevisionDiff.OTHER ? ` (${commit.slice(0, 7)}..${otherCommit.slice(0, 7)})` : "";
          return { diff: `diff --git a/${file} b/${file}\n--- a/${file}\n+++ b/${file}${note}\n@@ -1,3 +1,${3 + add.length} @@\n${then.trimEnd().split("\n").map((l) => " " + l).join("\n")}\n${add.map((l) => "+" + l).join("\n")}\n`, truncated: false };
        },
        restoreFileRevision: ({ path, commit, preview }) => {
          if (!fakeFiles.has(path)) {
            const under = [...fakeFiles.keys()].filter((p) => p.startsWith(path ? path + "/" : "")).slice(0, 3);
            return { files: under.map((p, i) => ({ path: p, recreated: i === 2 })), version: "" };
          }
          if (preview) return { files: [], version: "" };
          const text = commit === fakeRevisions(path)[0].commit ? (fakeFiles.get(path) ?? "") : fakeOldText(fakeFiles.get(path) ?? "");
          fakeFiles.set(path, text);
          return { version: fakeVersion(text) };
        },
        deleteFile: ({ path }) => {
          for (const p of [...fakeFiles.keys()]) if (p === path || p.startsWith(path + "/")) fakeFiles.delete(p);
          return {};
        },
        findFiles: ({ query, folders, limit, extensions }) => {
          const q = query.toLowerCase().replace(/\s/g, "");
          const match = (p: string) => {
            let i = 0;
            for (const c of p.toLowerCase()) if (c === q[i]) i++;
            return i === q.length;
          };
          let all = [...fakeFiles.keys()].filter((p) => !fakeHidden(p) && !p.split("/").some((x) => x.startsWith(".")));
          if (extensions.length > 0) {
            all = all.filter((p) => extensions.some((e) => p.toLowerCase().endsWith("." + e.replace(/^\./, "").toLowerCase())));
            folders = false;
          }
          if (folders) for (const p of [...all]) for (let d = p.slice(0, p.lastIndexOf("/")); d; d = d.slice(0, Math.max(0, d.lastIndexOf("/")))) if (!all.includes(d + "/")) all.push(d + "/");
          return { paths: all.filter(match).sort((a, b) => a.length - b.length).slice(0, limit || 50) };
        },
        readTable: ({ path, search, filters, sortColumn, descending, offset, limit }) => {
          const [head, ...lines] = (fakeFiles.get(path) ?? "").trim().split("\n");
          if (!head) notFound(path);
          const names = head.split(",");
          const intents: Record<string, string> = { units: "units sold", price: "unit price, USD" };
          let rows = lines.map((l, i) => ({ number: i + 1, cells: l.split(",") }));
          const q = search.trim().toLowerCase();
          if (q) rows = rows.filter((r) => r.cells.some((c) => c.toLowerCase().includes(q)));
          for (const f of filters) {
            const m = /^(>=|<=|>|<)\s*(-?[\d.]+)$/.exec(f.expr);
            rows = rows.filter((r) => {
              const v = r.cells[f.column] ?? "";
              if (!m) return v.toLowerCase().includes(f.expr.toLowerCase());
              const n = Number(v), x = Number(m[2]);
              return { ">": n > x, ">=": n >= x, "<": n < x, "<=": n <= x }[m[1] as ">" | ">=" | "<" | "<="];
            });
          }
          if (sortColumn > 0) {
            const c = sortColumn - 1;
            rows = [...rows].sort((a, b) => (Number.isNaN(Number(a.cells[c])) ? a.cells[c].localeCompare(b.cells[c]) : Number(a.cells[c]) - Number(b.cells[c])) * (descending ? -1 : 1));
          }
          const numeric = (i: number) => lines.every((l) => !Number.isNaN(Number(l.split(",")[i])));
          return {
            columns: names.map((n, i) => ({ name: n, kind: n === "date" ? "date" : numeric(i) ? "number" : "text", empty: 0, min: 0, max: 0, intent: intents[n] ?? "" })),
            rows: rows.slice(offset, offset + (limit || 100)),
            matched: rows.length,
            total: lines.length,
            sorted: sortColumn > 0,
            latin1: false,
          };
        },
        renderDocx: ({ markdown, diagrams }) => {
          ((window as unknown as { __diagrams?: unknown[] }).__diagrams ??= []).push(...diagrams.map((d) => ({ source: d.source, bytes: d.png.length, width: d.width, height: d.height })));
          return { data: new TextEncoder().encode("PK fake docx\n" + markdown) };
        },
        statFiles: ({ paths }) => ({ versions: Object.fromEntries(paths.map((p) => [p, fakeFiles.has(p) ? fakeVersion(fakeFiles.get(p)!) : ""])) }),
      });
      service(WorkerService, {
        createWorker: (r) => {
          if (/^when/i.test(r.schedule)) return { problems: [`schedule "${r.schedule}": use a cron expression, a descriptor or plain text like "Daily at 6 AM"`] };
          if (fakeWorkers.some((w) => w.name === r.name)) throw new ConnectError(`a worker named ${r.name} exists`, Code.AlreadyExists);
          const { workspace, ...spec } = r;
          const w: FakeWorker = { workspace, name: r.name, description: r.description, state: WorkerState.NEW, schedule: r.schedule, cron: "0 6 * * *", timezone: r.timezone, agent: r.agent, model: r.model, permissions: r.permissions, path: `${workspace}/.agents/workers/${r.name}/WORKER.md`, hash: "sha256:new", problems: [], spec };
          fakeWorkers.unshift(w);
          return { worker: w };
        },
        listWorkers: ({ workspace }) => ({ workers: workspace === fakeHome ? fakeGlobal : fakeWorkers }),
        // The schedule picker's preview: the picker's own cron forms, roughly
        // (hours and minutes, every day or the weekdays named), else a problem.
        previewSchedule: ({ schedule, count }) => {
          const f = schedule.trim().split(/\s+/);
          if (f.length !== 5 || !/^\d+$/.test(f[0])) return { problem: `can't read "${schedule}" (the fake reads only the picker's own cron)`, cron: "", timezone: "", nextRuns: [] };
          const days = f[4] === "*" ? [0, 1, 2, 3, 4, 5, 6] : f[4] === "1-5" ? [1, 2, 3, 4, 5] : f[4].split(",").map(Number);
          const runs: ReturnType<typeof now>[] = [];
          for (let d = new Date(); runs.length < (count || 5); ) {
            d = new Date(d.getTime() + (f[1] === "*" ? 3600e3 : 0));
            if (f[1] !== "*") d.setDate(d.getDate() + 1);
            d.setHours(f[1] === "*" ? d.getHours() : Number(f[1]), Number(f[0]), 0, 0);
            if (/^\d+$/.test(f[2]) && d.getDate() !== Number(f[2])) continue;
            if (days.includes(d.getDay())) runs.push(timestampFromDate(d));
          }
          return { cron: schedule.trim(), timezone: "Europe/Paris", nextRuns: runs, problem: "" };
        },
        listAllWorkers: () => ({
          home: fakeHome,
          policy: fakePolicy,
          workspaces: [
            { workspace: fakeHome, home: true, workers: fakeGlobal },
            { workspace: "/home/me/shop", workers: fakeWorkers },
            ...fakeElsewhere,
          ],
        }),
        forgetWorkspaceWorkers: ({ workspace }) => {
          const i = fakeElsewhere.findIndex((g) => g.workspace === workspace);
          if (i >= 0) fakeElsewhere.splice(i, 1);
          return {};
        },
        deleteWorker: ({ name }) => {
          const i = fakeWorkers.findIndex((x) => x.name === name);
          if (i < 0) throw new ConnectError(`no such worker: ${name}`, Code.NotFound);
          fakeWorkers.splice(i, 1);
          return {};
        },
        // The file as written; an edit changes the hash, and suspends an enabled worker.
        getWorkerSpec: ({ name }) => {
          const w = fakeWorkers.find((x) => x.name === name);
          if (!w) throw new ConnectError(`no worker ${name}`, Code.NotFound);
          return { worker: w.spec, hash: w.hash };
        },
        updateWorker: ({ hash, worker }) => {
          const w = fakeWorkers.find((x) => x.name === worker?.name);
          if (!w || !worker) throw new ConnectError(`no worker ${worker?.name}`, Code.NotFound);
          if (hash !== w.hash) {
            const err = new ConnectError(`${w.path} changed since it was loaded`, Code.FailedPrecondition);
            err.details.push({ desc: ErrorInfoSchema, value: { reason: "HASH_MISMATCH", metadata: {} } });
            throw err;
          }
          if (/^when/i.test(worker.schedule)) return { problems: [`schedule "${worker.schedule}": use a cron expression, a descriptor or plain text like "Daily at 6 AM"`] };
          w.spec = { ...worker };
          Object.assign(w, { description: worker.description, schedule: worker.schedule, timezone: worker.timezone, agent: worker.agent, model: worker.model, permissions: worker.permissions, problems: [] });
          w.hash = `sha256:${fakeVersion(JSON.stringify(worker))}`;
          w.state = w.state === WorkerState.ENABLED ? WorkerState.CHANGED : w.state === WorkerState.INVALID ? WorkerState.NEW : w.state;
          return { worker: w };
        },
        enableWorker: ({ name, hash }) => {
          const w = fakeWorkers.find((x) => x.name === name)!;
          if (hash !== w.hash) throw new ConnectError("the worker changed since it was reviewed", Code.FailedPrecondition);
          w.state = WorkerState.ENABLED;
          return { worker: w };
        },
        disableWorker: ({ name }) => {
          const w = fakeWorkers.find((x) => x.name === name)!;
          w.state = WorkerState.DISABLED;
          return { worker: w };
        },
        // A run that's over as soon as it's watched.
        runWorker: ({ name }) => ({ run: { id: `run-${name}-${Date.now()}`, status: RunStatus.RUNNING, started: now(), manual: true } }),
        async *watchWorkerRun() {},
        // Rewrite with AI: a tidier version of whatever was typed.
        rewriteWorker: async ({ worker }) => {
          await sleep(900);
          if (/fail/i.test(worker?.prompt ?? "")) return { prompt: "", permissions: [], changes: [], problem: "the suggestions model is unavailable (quota exceeded)" };
          return {
            prompt: `Write today's digest. Everything you need is here: don't look up this worker's definition or open a browser.\n\n## Sources\n\nFetch https://news.google.com/rss/search?q=artificial+intelligence+when:1d once with web_fetch.\n\n## The file\n\nWrite \`daily-news/MM-DD-YYYY.md\` (today, in ${worker?.timezone || "the worker's time zone"}), overwriting it if it exists.\n\n## Finish\n\nEnd with the file's path and how many stories went in.`,
            permissions: ["web:news.google.com", "write:daily-news/*"],
            changes: ["Named the feed to fetch, once", "Said where the file goes and in what format", "Narrowed web access to news.google.com"],
            problem: "",
          };
        },
        // Runs going now, in this workspace and the global workers' (Running
        // now); Stop ends one, recorded as stopped.
        listRunningWorkerRuns: () => ({ runs: fakeLive.filter((r) => r.status === RunStatus.RUNNING) }),
        stopWorkerRun: ({ runId }) => {
          const r = fakeLive.find((x) => x.id === runId && x.status === RunStatus.RUNNING);
          if (!r) throw new ConnectError(`run ${runId} isn't running`, Code.NotFound);
          const before = { ...r };
          r.status = RunStatus.STOPPED;
          r.error = { reason: "RUN_STOPPED", message: "stopped by the user" };
          return { run: before };
        },
        listWorkerRuns: ({ workspace, name }) => ({
          runs: [
            ...fakeLive.filter((r) => r.workspace === workspace && r.worker === name),
            { id: "r2", status: RunStatus.SUCCEEDED, started: now(), manual: false, sessionId: "worker-nightly-2", usage: { costUsd: 0.0041, priced: true }, files: ["reports/deps.md"] },
            { id: "r1", status: RunStatus.FAILED, started: now(), manual: true, sessionId: "worker-nightly-1", error: { reason: "RUN_FAILED", message: "the run reached its cost limit" } },
          ],
        }),
      });
    }),
  );
}
