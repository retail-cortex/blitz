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
import { create, type JsonObject, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { setTransport } from "../api";
import { BackgroundRunSchema, MessageSchema, RunTurnResponseSchema, SessionInfoSchema, SessionService, WatchBackgroundResponseSchema, type BackgroundRun, type Message, type SessionInfo } from "../gen/blitz/v1/session_pb";
import { ActionKind, ErrorInfoSchema, UsageSchema, type Usage } from "../gen/blitz/v1/turn_pb";

type Out = MessageInitShape<typeof RunTurnResponseSchema>;
import { RunStatus, WorkerService, WorkerState } from "../gen/blitz/v1/worker_pb";
import { ConfigService, KeySource } from "../gen/blitz/v1/config_pb";
import { FileKind, FileService, GitAction } from "../gen/blitz/v1/file_pb";
import { AgentFileSchema, AgentScope, SuggestionKind, SuggestionSchema, WorkspaceService, type AgentDefinition } from "../gen/blitz/v1/workspace_pb";

// Whether the fake workspace is a git repository (?norepo: not yet).
let fakeRepo = !new URLSearchParams(location.search).has("norepo");

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const now = () => timestampFromDate(new Date());

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
      sessions: [first, other],
      active: first.id,
      settings: { agent: "blitz", model: "gemini-3.8-flash", provider: "gemini", effort: "", mode: "default", agency: "high", locale: "en-US", style: "default" },
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
// first 4 s after a workspace first asks; ?noharness offers the setup.
const suggestionsAsked = new Map<string, number>();
function fakeSuggestions(dir: string) {
  if (!suggestionsAsked.has(dir)) suggestionsAsked.set(dir, Date.now());
  const pending = Date.now() - suggestionsAsked.get(dir)! < 4000;
  const last = state(dir).sessions.find((x) => x.messages.length > 0);
  const out: MessageInitShape<typeof SuggestionSchema>[] = [];
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

const fakeFiles = new Map<string, string>([
  ["go.mod", "module example.com/shop\n\ngo 1.27\n"],
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
  ["bin/shop", "\u0000binary"],
]);
const fakeGit: Record<string, string> = { "internal/cart/discount.go": "modified", "internal/cart/discount_test.go": "untracked", "web/app.ts": "added" };
const fakeHidden = (p: string) => (p === ".env" ? "blocked" : p.startsWith("bin") ? "ignored" : p.split("/").pop()!.startsWith(".") ? "dot" : "");
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

async function* watchRun(id: string): AsyncGenerator<MessageInitShape<typeof WatchBackgroundResponseSchema>> {
  const r = backgroundRuns().find((x) => x.id === id);
  if (!r) throw new ConnectError(`no background run ${id}`, Code.NotFound);
  const s = state(r.workspace);
  const ev = (kind: unknown) => ({ event: { author: "blitz", kind } }) as MessageInitShape<typeof WatchBackgroundResponseSchema>;
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
            const run = create(SessionInfoSchema, { id: ref, title: "⏰ nightly-deps", workspace, created: now(), updated: now(), messages: [msg("user", "Check for outdated dependencies."), msg("model", "Two modules are behind: golang.org/x/net and google.golang.org/grpc. The report is in reports/deps.md.")] });
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
          state(workspace).pending.get(requestId)?.(decision === 4 ? "deny" : "allow");
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
        getSettings: ({ workspace }) => {
          const s = state(workspace).settings;
          return { agent: s.agent, model: s.model, provider: s.provider, effort: s.effort, permissionMode: s.mode, agency: s.agency, locale: s.locale, style: s.style, imagesEnabled: true, speechModel: s.speechModel ?? "", acceptedMedia: fakeAccepted(s.provider) };
        },
        getModel: ({ workspace }) => ({ name: state(workspace).settings.model, provider: state(workspace).settings.provider, unavailable: "" }),
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
        getServiceInfo: () => ({ version: "dev", executable: "" }),
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
            { name: "setup", description: "Set up the agent harness: .agents/AGENT.md, skills and agents", source: "bundled" },
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
        getGitStatus: () => (fakeRepo ? { git: true, repo: true, branch: "fix/coupon-rounding", changed: 2 } : { git: true, repo: false }),
        initGitRepo: () => {
          fakeRepo = true;
          return { status: { git: true, repo: true, branch: "main", changed: 0 } };
        },
        getDiff: ({ git }) => ({
          diff: git
            ? ""
            : "--- a/internal/cart/discount.go\n+++ b/internal/cart/discount.go\n@@ -40,9 +40,9 @@ func ApplyCoupon(items []Item, c Coupon) int {\n \ttotal := 0\n \tfor _, it := range items {\n-\t\ttotal += round(it.Cents * (100 - c.Percent) / 100)\n+\t\ttotal += it.Cents\n \t}\n-\treturn total\n+\treturn round(total * (100 - c.Percent) / 100)\n }\n--- /dev/null\n+++ b/internal/cart/discount_test.go\n@@ -0,0 +1,6 @@\n+package cart\n+\n+import \"testing\"\n+\n+func TestApplyCouponRounding(t *testing.T) {\n+}\n",
        }),
      });
      service(ConfigService, {
        getInterfaceLanguage: () => ({ locale: "", catalogs: [] }),
        listModels: () => ({
          defaultProvider: "gemini",
          providers: [
            { provider: "gemini", ids: ["gemini-3.5-flash-lite", "gemini-3.8-flash", "gemini-3.8-pro"] },
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
          if (key === "audio.model" && workspace) state(workspace).settings.speechModel = value;
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
            { key: "llm.gemini.model", type: "string", default: '"gemini-3.8-flash"', doc: "Model is the Gemini model to use." },
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
            return { name, path: p, kind: folder ? FileKind.FOLDER : FileKind.FILE, size: BigInt(fakeFiles.get(p)?.length ?? 0), git, hidden: fakeHidden(p), agentRule: p === ".env" ? "blocked" : "" };
          });
          entries.sort((a, b) => (a.kind !== b.kind ? (a.kind === FileKind.FOLDER ? -1 : 1) : a.name.localeCompare(b.name)));
          return { entries: showHidden ? entries : entries.filter((e) => !e.hidden), repo: true };
        },
        readPreview: ({ path }) => {
          if (!fakeFiles.has(path)) notFound(path);
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
          return {};
        },
        deleteFile: ({ path }) => {
          for (const p of [...fakeFiles.keys()]) if (p === path || p.startsWith(path + "/")) fakeFiles.delete(p);
          return {};
        },
        findFiles: ({ query, folders, limit }) => {
          const q = query.toLowerCase().replace(/\s/g, "");
          const match = (p: string) => {
            let i = 0;
            for (const c of p.toLowerCase()) if (c === q[i]) i++;
            return i === q.length;
          };
          const all = [...fakeFiles.keys()].filter((p) => !fakeHidden(p) && !p.split("/").some((x) => x.startsWith(".")));
          if (folders) for (const p of [...all]) for (let d = p.slice(0, p.lastIndexOf("/")); d; d = d.slice(0, Math.max(0, d.lastIndexOf("/")))) if (!all.includes(d + "/")) all.push(d + "/");
          return { paths: all.filter(match).sort((a, b) => a.length - b.length).slice(0, limit || 50) };
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
        listWorkers: () => ({ workers: fakeWorkers }),
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
        listWorkerRuns: () => ({
          runs: [
            { id: "r2", status: RunStatus.SUCCEEDED, started: now(), manual: false, sessionId: "worker-nightly-2", usage: { costUsd: 0.0041, priced: true }, files: ["reports/deps.md"] },
            { id: "r1", status: RunStatus.FAILED, started: now(), manual: true, sessionId: "worker-nightly-1", error: { reason: "RUN_FAILED", message: "the run reached its cost limit" } },
          ],
        }),
      });
    }),
  );
}
