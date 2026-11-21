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

import { clone, create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { HealthCheckProgressSchema, HealthVerdictSchema, WorkspaceHealthSchema, type HealthItem, type WorkspaceHealth } from "../gen/blitz/v1/workspace_pb";
import { ago } from "./fakeHelpers";

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
  reflectThreshold: 5,
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
let fakeStopCheck = false;

async function* watchFakeHealth(signal?: AbortSignal): AsyncGenerator<{ health: WorkspaceHealth }> {
  let last = "";
  while (!signal?.aborted) {
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

/** Workspace health RPC handlers for WorkspaceService. */
export function fakeHealthHandlers() {
  return {
    getWorkspaceHealth: () => ({ health: clone(WorkspaceHealthSchema, { ...fakeHealth, checking: fakeChecking, progress: fakeChecking && fakeProgress ? create(HealthCheckProgressSchema, fakeProgress) : undefined }) }),
    startHealthCheck: ({ scope }: { scope?: { kinds: string[]; items: string[]; force: boolean } }) => {
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
    watchWorkspaceHealth: (_req: unknown, ctx: { signal: AbortSignal }) => watchFakeHealth(ctx.signal),
    estimateHealthCheck: ({ scope }: { scope?: { kinds: string[]; items: string[]; force: boolean } }) => {
      const n = fakeDue(scope).length;
      return { items: n, costUsd: n * 0.0019, priced: true };
    },
    checkWorkspaceHealth: ({ scope }: { scope?: { kinds: string[]; items: string[]; force: boolean } }) => fakeCheck(scope),
    stopHealthCheck: () => {
      fakeStopCheck = true;
      return {};
    },
  };
}
