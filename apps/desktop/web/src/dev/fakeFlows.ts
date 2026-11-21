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

import { clone, create, type MessageInitShape } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { BackgroundRunSchema, SessionInfoSchema, WatchBackgroundResponseSchema, type BackgroundRun } from "../gen/blitz/v1/session_pb";
import { ActionKind, ErrorInfoSchema, UsageSchema } from "../gen/blitz/v1/turn_pb";
import { WorkflowRunEventSchema, WorkflowRunSchema, WorkflowRunStepSchema, type WorkflowRun } from "../gen/blitz/v1/workspace_pb";
import { ago, fakeAlertsState, fakeUsage, msg, now, sleep, state, states } from "./fakeHelpers";

/** Studio workflow fixture shape. */
export type FakeFlow = {
  name: string;
  description: string;
  nodes: { id: string; type: string; title: string; x: number; y: number; agent: string; skills: string[]; instructions: string; rules: string[]; skill: string; script: string; args: string[]; path: string; fields?: { name: string; type: string; description: string }[]; url?: string; method?: string; server?: string; tool?: string; argumentsJson?: string; headers?: { name: string; value: string }[]; auth?: { type: string; token?: string } }[];
  edges: { from: string; to: string; when?: string; if?: string; default?: boolean; error?: boolean; label?: string }[];
  mode?: string;
};

/** Builds a FakeFlow step node with defaults. */
export const flowNode = (id: string, type: string, title: string, x: number, y: number, extra: Partial<FakeFlow["nodes"][number]> = {}) => ({ id, type, title, x, y, agent: "", skills: [], instructions: "", rules: [], skill: "", script: "", args: [], path: "", ...extra });

/** Workflows listed in Studio › Workflows. */
export const fakeFlows: FakeFlow[] = [
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

/** Builds a WorkflowFile payload from a FakeFlow. */
export const fakeFlowFile = (workspace: string, f: FakeFlow) => ({ ...f, path: `${workspace}/.agents/workflows/${f.name}.flow.yaml`, problem: fakeFlowProblem(f) });

const fakeMessages = (surfaceId: string, form: { components: unknown[]; data: unknown }) =>
  JSON.stringify([
    { version: "v0.9", createSurface: { surfaceId, catalogId: "https://a2ui.org/specification/v0_9/catalogs/basic/catalog.json", sendDataModel: true } },
    { version: "v0.9", updateComponents: { surfaceId, components: form.components } },
    { version: "v0.9", updateDataModel: { surfaceId, path: "/", value: form.data } },
  ]);

/** Workflows' human tasks waiting: a refund to approve (?fake&notasks for none). */
export const fakeTasks = new URLSearchParams(location.search).has("notasks")
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

/** Streams human tasks whenever the list or alerts preference changes. */
export async function* watchFakeTasks(workspace: string, signal?: AbortSignal) {
  let last = "";
  while (!signal?.aborted) {
    const tasks = fakeTasks.filter((x) => !workspace || x.workspace === workspace);
    const sig = fakeAlertsState.value + tasks.map((x) => x.id).join("|");
    if (sig !== last) {
      last = sig;
      yield { alerts: fakeAlertsState.value, tasks, listed: true };
    }
    await sleep(500);
  }
}

/** Active workflow background runs keyed by background run ID. */
export const fakeFlowRuns = new Map<string, FakeFlow>();
const flowLogs = new Map<string, { events: MessageInitShape<typeof WatchBackgroundResponseSchema>[]; done: boolean }>();
let receiptsVersion = 0;

/** Workflow run receipts, newest first. */
export const fakeReceipts: WorkflowRun[] = [
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

let fakeRuns: BackgroundRun[] | undefined;

/** Background runs stopped by the user. */
export const runStopped = new Set<string>();

/** Returns (and lazily seeds) background runs once a workspace has opened. */
export function backgroundRuns(): BackgroundRun[] {
  if (fakeRuns) return fakeRuns;
  const dir = [...states.keys()][0];
  if (!dir) return [];
  const s = state(dir);
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

/** Runs a workflow in the fake: its steps one after another, agent steps streaming text, save_file asking for approval. */
export async function driveFlow(r: BackgroundRun, f: FakeFlow, s: ReturnType<typeof state>) {
  const log = { events: [] as MessageInitShape<typeof WatchBackgroundResponseSchema>[], done: false };
  flowLogs.set(r.id, log);
  const ev = (kind: unknown) => log.events.push({ event: { author: "blitz", kind } } as MessageInitShape<typeof WatchBackgroundResponseSchema>);
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

/** Strips events and step output/stream text for workflow run summaries. */
export const summary = (r: WorkflowRun) => {
  const c = clone(WorkflowRunSchema, r);
  c.events = [];
  for (const x of c.steps) x.stream = x.output = "";
  return c;
};

/** Returns receipts scoped to the workspace (filling empty workspace on fixtures). */
export const receiptsFor = (workspace: string) => fakeReceipts.map((r) => (r.workspace ? r : Object.assign(r, { workspace: [...states.keys()][0] ?? workspace })));

/** Streams active and recently finished workflow runs. */
export async function* watchFakeRuns(workspace: string, signal?: AbortSignal) {
  let last = -1;
  while (!signal?.aborted) {
    if (receiptsVersion !== last) {
      last = receiptsVersion;
      const cutoff = Date.now() - 10 * 60_000;
      yield { runs: receiptsFor(workspace).filter((r) => (!workspace || r.workspace === workspace) && (r.status === "running" || r.status === "waiting" || (r.ended && Number(r.ended.seconds) * 1000 > cutoff))).map(summary), listed: true };
    }
    await sleep(300);
  }
}

/** Streams updates for one workflow run receipt until it finishes. */
export async function* watchFakeRun(id: string, signal?: AbortSignal) {
  let last = -1;
  while (!signal?.aborted) {
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

/** Streams events for a background run (either a driven workflow or a scripted background run). */
export async function* watchRun(id: string): AsyncGenerator<MessageInitShape<typeof WatchBackgroundResponseSchema>> {
  const r = backgroundRuns().find((x) => x.id === id);
  if (!r) throw new ConnectError(`no background run ${id}`, Code.NotFound);
  const s = state(r.workspace);
  const ev = (kind: unknown) => ({ event: { author: "blitz", kind } }) as MessageInitShape<typeof WatchBackgroundResponseSchema>;
  const log = flowLogs.get(id);
  if (log) {
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
