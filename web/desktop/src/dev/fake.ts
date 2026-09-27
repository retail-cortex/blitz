// A fake Blitz service for working on the page without one (?fake in a
// development build). It keeps a little state in memory and scripts turns
// so every state of the conversation can be seen: streamed Markdown,
// thinking, tool calls, a task list, an approval and a plan review.
import { create, type JsonObject, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { setTransport } from "../api";
import { MessageSchema, RunTurnResponseSchema, SessionInfoSchema, SessionService, type Message, type SessionInfo } from "../gen/blitz/v1/session_pb";
import { ActionKind, ErrorInfoSchema, UsageSchema, type Usage } from "../gen/blitz/v1/turn_pb";

type Out = MessageInitShape<typeof RunTurnResponseSchema>;
import { RunStatus, WorkerService, WorkerState } from "../gen/blitz/v1/worker_pb";
import { WorkspaceService } from "../gen/blitz/v1/workspace_pb";

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
  settings: { agent: string; model: string; provider: string; effort: string; mode: string; agency: string; locale: string };
  pending: Map<string, (answer: string) => void>;
  usage: Usage;
}

const states = new Map<string, State>();

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
      settings: { agent: "blitz", model: "gemini-3.8-flash", provider: "gemini", effort: "", mode: "default", agency: "high", locale: "en-US" },
      pending: new Map(),
      usage: create(UsageSchema, { calls: 3, input: 18234n, output: 2210n, lastPrompt: 12876n, costUsd: 0.0123, priced: true }),
    };
    states.set(dir, s);
  }
  return s;
}

const active = (s: State) => s.sessions.find((x) => x.id === s.active)!;

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
        loadSession: ({ workspace, ref }) => {
          const s = state(workspace);
          if (!s.sessions.some((x) => x.id === ref)) notFound(ref);
          s.active = ref;
          return { session: active(s) };
        },
        renameSession: ({ workspace, title }) => {
          const sess = active(state(workspace));
          sess.title = title;
          return { session: sess };
        },
        runTurn: (req) => runTurn(req.workspace, req.turn?.text ?? "", !!req.turn?.plan),
        steer: ({ workspace, text }) => {
          active(state(workspace)).messages.push(msg("user", text, "steer"));
          return {};
        },
        approve: ({ workspace, requestId, decision }) => {
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
      });
      service(WorkspaceService, {
        getSettings: ({ workspace }) => {
          const s = state(workspace).settings;
          return { agent: s.agent, model: s.model, provider: s.provider, effort: s.effort, permissionMode: s.mode, agency: s.agency, locale: s.locale, imagesEnabled: true };
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
        setSetting: ({ workspace, key, value }) => {
          const s = state(workspace).settings;
          if (key === "effort") s.effort = value === "auto" ? "" : value;
          if (key === "agency") s.agency = value;
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
            ? { model: { model: ref.split("/").pop(), provider: "gemini", settings: { temperature: 0.3 }, globalTemperature: 0.2, globalMaxTokens: 8192 } }
            : { all: { "gpt-5": { temperature: 1 }, "claude-opus-5-5": { reasoningEffort: "high" } } },
        updateModelSettings: ({ ref }) => ({ model: { model: ref, settings: {} }, unsupported: [] }),
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
        closeWorkspace: () => ({}),
        listWorkspaces: () => ({ workspaces: [...states.keys()] }),
        listCheckpoints: () => ({ checkpoints: [{ id: 2, label: "Fix the coupon rounding", time: now(), files: ["internal/cart/discount.go", "internal/cart/discount_test.go"] }] }),
        undo: () => ({ label: "Fix the coupon rounding", restored: ["internal/cart/discount.go"] }),
        getDiff: ({ git }) => ({
          diff: git
            ? ""
            : "--- a/internal/cart/discount.go\n+++ b/internal/cart/discount.go\n@@ -40,9 +40,9 @@ func ApplyCoupon(items []Item, c Coupon) int {\n \ttotal := 0\n \tfor _, it := range items {\n-\t\ttotal += round(it.Cents * (100 - c.Percent) / 100)\n+\t\ttotal += it.Cents\n \t}\n-\treturn total\n+\treturn round(total * (100 - c.Percent) / 100)\n }\n--- /dev/null\n+++ b/internal/cart/discount_test.go\n@@ -0,0 +1,6 @@\n+package cart\n+\n+import \"testing\"\n+\n+func TestApplyCouponRounding(t *testing.T) {\n+}\n",
        }),
      });
      service(WorkerService, {
        listWorkers: () => ({
          workers: [
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
            },
            { name: "weekly-report", state: WorkerState.NEW, schedule: "Mondays at 09:00", cron: "0 9 * * 1", timezone: "America/Chicago", hash: "11aa", limits: { maxTurns: 10, maxCostUsd: 0.2 } },
          ],
        }),
        listWorkerRuns: () => ({
          runs: [
            { id: "r2", status: RunStatus.SUCCEEDED, started: now(), manual: false, sessionId: "worker-nightly-2", usage: { costUsd: 0.0041, priced: true } },
            { id: "r1", status: RunStatus.FAILED, started: now(), manual: true, sessionId: "worker-nightly-1", error: { reason: "RUN_FAILED", message: "the run reached its cost limit" } },
          ],
        }),
      });
    }),
  );
}
