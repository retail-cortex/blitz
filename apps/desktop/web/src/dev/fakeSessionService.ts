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
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { BackgroundRunSchema, SessionInfoSchema, SessionService, UsageGroupSchema } from "../gen/blitz/v1/session_pb";
import { Decision } from "../gen/blitz/v1/turn_pb";
import { backgroundRuns, driveFlow, fakeFlowRuns, fakeFlows, runStopped, watchRun } from "./fakeFlows";
import { active, msg, notFound, now, state } from "./fakeHelpers";
import { runTurn } from "./fakeTurn";
import { fakeReasoning } from "./fakeReasoning";

let fakeGoal: { condition: string; continues: number; max: number; last: string } | undefined;

/** Returns the fake ConnectRPC handler implementation for SessionService. */
export function fakeSessionService(): ServiceImpl<typeof SessionService> {
  const impl: Partial<ServiceImpl<typeof SessionService>> = {
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
    setSessionModel: ({ workspace, model }) => {
      const sess = active(state(workspace));
      (sess as { model?: string }).model = model;
      return { preview: { model, tokens: 12_000n, costUsd: 0.04, priced: true, compacts: false, unseen: [], noTools: false, activePin: "" } };
    },
    previewSessionModel: ({ model }) => ({ preview: { model, tokens: 12_000n, costUsd: 0.04, priced: true, compacts: false, unseen: [], noTools: false, activePin: "" } }),
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
    getUsage: ({ workspace }) => ({ usage: state(workspace).usage, byModel: [create(UsageGroupSchema, { name: "gemini-4-argon", usage: state(workspace).usage })] }),
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
    // The Reasoning view's graph (spec_066_reasoning): two turns, once the chat has a prompt.
    getReasoning: ({ workspace, turn }) => fakeReasoning(active(state(workspace)).messages.filter((m) => m.role === "user" && !m.kind).length, turn),
    searchSession: ({ terms }) => ({ found: terms === "zzz" ? 0 : 2, prompt: `Look at these passages about ${terms}…` }),
    saveSnapshot: ({ name }) => ({ snapshot: { id: "snap-1", snapshot: name } }),
  };
  return impl as ServiceImpl<typeof SessionService>;
}
