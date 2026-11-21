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

import { create, type JsonObject, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import type { RunTurnResponseSchema } from "../gen/blitz/v1/session_pb";
import { ActionKind, UsageSchema } from "../gen/blitz/v1/turn_pb";
import { SuggestionKind, type SuggestionSchema } from "../gen/blitz/v1/workspace_pb";
import { active, msg, sleep, state, type State } from "./fakeHelpers";
import { fakeGit } from "./fakeWorkspaceFiles";

type Out = MessageInitShape<typeof RunTurnResponseSchema>;

let nextRequest = 0;

/** Streams a scripted agent turn in response to user prompts (including question, approval, plan, and tool scenarios). */
export async function* runTurn(dir: string, text: string, plan: boolean): AsyncGenerator<Out> {
  const s = state(dir);
  const sess = active(s);
  sess.messages.push(msg("user", plan ? `/plan ${text}` : text));
  if (!sess.title) sess.title = text.slice(0, 50);
  const ev = (kind: unknown, author = "blitz") => ({ event: { author, kind } }) as Out;
  const before = create(UsageSchema, s.usage);
  yield ev({ case: "accepted", value: {} }, "");
  await sleep(300);
  if (/framework/i.test(text)) {
    // A technical choice, explained (spec_005_approvals APR-40b).
    const id = `q${++nextRequest}`;
    const answer = new Promise<string>((r) => s.pending.set(id, r));
    yield ev({
      case: "question",
      value: {
        requestId: id,
        question: "Which front end should the shop's admin pages use?",
        options: ["Vite + React + Tailwind", "Vite + React + MUI", "Angular + Material 3"],
        order: "What's easiest to keep up with AI's help first, then the size of each one's community, then the project's ADR-004 (one design system)",
        choices: [
          { label: "Vite + React + Tailwind", summary: "The most popular way to build web pages today, styled with small ready-made classes", details: "**React** builds the pages, **Vite** runs them while you work, and **Tailwind** styles them with short class names. AI tools know this combination best, so changes come out right more often. You'd pick colours and spacing yourself, so pages can drift apart without care.", why: "Easiest to maintain with AI, and the largest community" },
          { label: "Vite + React + MUI", summary: "React with Google's Material design components ready to use", details: "Same React and Vite, with **MUI**: buttons, tables and forms that already look finished and consistent. Less freedom in how things look, a little heavier pages.", why: "Meets ADR-004's one design system, with nearly as much AI help" },
          { label: "Angular + Material 3", summary: "Google's all-in-one framework with its own Material components", details: "**Angular** comes with everything built in (routing, forms, testing), which suits large teams with strict rules. It's more to learn, fewer people know it, and AI tools help less with it.", why: "Solid, but harder to learn and maintain" },
        ],
      },
    });
    const a = await answer;
    const reply = `Going with ${a}.`;
    sess.messages.push(msg("model", reply));
    yield ev({ case: "text", value: { text: reply } });
    yield ev({ case: "finished", value: { before, after: s.usage } });
    return;
  }
  if (/choose/i.test(text)) {
    // A multi-select question (ask_user_question with multi_select).
    const id = `q${++nextRequest}`;
    const answer = new Promise<string>((r) => s.pending.set(id, r));
    yield ev({
      case: "question",
      value: { requestId: id, question: "Which packages should I check for the rounding bug?", options: ["internal/cart", "internal/store", "cmd/shop"], multiSelect: true },
    });
    const a = await answer;
    const reply = `Checking ${a.split("\n").join(", ")}.`;
    sess.messages.push(msg("model", reply));
    yield ev({ case: "text", value: { text: reply } });
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
    const reply = `Going with: ${await answer}`;
    sess.messages.push(msg("model", reply));
    yield ev({ case: "text", value: { text: reply } });
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
    const reply = "Created the agent instruction files.";
    sess.messages.push(msg("model", reply));
    yield ev({ case: "text", value: { text: reply } });
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
  if (/reviewer/i.test(text)) {
    // Auto mode's reviewer denied a write; the person may allow it anyway
    // (spec_005_approvals APR-14d).
    const id = `a${++nextRequest}`;
    const decision = new Promise<string>((r) => s.pending.set(id, r));
    yield call("r1", "create_file", { path: ".agents/scratch/db-info.md" });
    yield ev({
      case: "approvalRequest",
      value: { requestId: id, tool: "create_file", kind: ActionKind.WRITE, detail: "Create .agents/scratch/db-info.md (2130 bytes)", reviewerDenied: "The action attempts to create a file outside the workspace." },
    });
    const d = await decision;
    yield result("r1", "create_file", d === "deny" ? { error: "the user denied it" } : { path: ".agents/scratch/db-info.md", bytes_written: 2130 });
    yield ev({ case: "text", value: { text: d === "deny" ? "OK, I won't write it." : "Wrote `.agents/scratch/db-info.md`." } });
    yield ev({ case: "finished", value: { before, after: s.usage } });
    return;
  }
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
    // ?citations: the answer cites the provider's search (spec_056 CA-84).
    if (new URLSearchParams(location.search).has("citations"))
      yield ev({ case: "citations", value: { items: [{ title: "Blitz docs", url: "https://retail-cortex.github.io/blitz/", text: "" }] } });
  }
  s.usage = create(UsageSchema, { ...s.usage, calls: s.usage.calls + 4, input: s.usage.input + 9120n, output: s.usage.output + 812n, lastPrompt: 14210n, costUsd: s.usage.costUsd + 0.0061 });
  sess.messageCount = sess.messages.length;
  // ?reflectdue: the turn says it's time to /reflect (HE-63).
  if (new URLSearchParams(location.search).has("reflectdue")) yield ev({ case: "reflectDue", value: { patterns: 6, score: 8 } });
  yield ev({ case: "finished", value: { before, after: s.usage } });
}

/** A background task as the service reports it: after 3 s it asks to run the tests and waits; 3 s after the answer it's done (or stopped). */
export function fakeTask(s: State, id: string) {
  const t = s.tasks.get(id)!;
  const end = t.stopped ?? (t.answered && Date.now() - t.answered > 3000 ? t.answered + 3000 : 0);
  const taskState = t.stopped ? "stopped" : end ? "done" : !t.answered && Date.now() - t.started > 3000 ? "waiting" : "running";
  const ms = (end || Date.now()) - t.started;
  return {
    id,
    agent: t.agent,
    prompt: t.prompt,
    sessionId: t.sessionId,
    state: taskState,
    started: timestampFromDate(new Date(t.started)),
    ended: end ? timestampFromDate(new Date(end)) : undefined,
    result: taskState === "done" ? "No other rounding bugs: `SplitTotal` and `Refund` already work in cents." : "",
    error: t.stopped ? "stopped" : "",
    usage: create(UsageSchema, { calls: Math.ceil(ms / 2000), costUsd: 0.0009 * Math.ceil(ms / 2000), priced: true }),
  };
}

/** The shop workspace's .blitz/settings.toml state (spec_031_project_config). */
export function fakeProject(s: State) {
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

const suggestionsAsked = new Map<string, number>();

/** Welcome tiles for GetSuggestions, including ?stale/?reflect/?review/?noharness switches. */
export function fakeSuggestions(dir: string) {
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
