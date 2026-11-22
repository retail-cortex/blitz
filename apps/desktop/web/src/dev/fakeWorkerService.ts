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

import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { ErrorInfoSchema } from "../gen/blitz/v1/turn_pb";
import { RunStatus, WorkerService, WorkerState } from "../gen/blitz/v1/worker_pb";
import { parseDuration } from "../workerForm";
import { jobProblems } from "./fakeChecks";
import { fakePolicy } from "./fakeConfigData";
import { fakeHome, fakeVersion, now, sleep } from "./fakeHelpers";

type FakeWorker = Record<string, unknown> & { name: string; hash: string; state: WorkerState; path: string; spec: Record<string, unknown> & { schedule: string } };
const fakeSpec = (name: string, o: Record<string, unknown> = {}) => ({ name, description: "", schedule: "", timezone: "", agent: "", model: "", permissions: [] as string[], maxTurns: 0, maxCostUsd: 0, timeout: "", catchUp: "", prompt: "", ...o });

const fakeLimits = (w: { maxTurns: number; maxCostUsd: number; timeout: string }) => {
  const ms = parseDuration(w.timeout);
  return { maxTurns: w.maxTurns, maxCostUsd: w.maxCostUsd, timeout: ms ? { seconds: BigInt(Math.round(ms / 1000)) } : undefined };
};

const fakeGlobal: FakeWorker[] = [
  { name: "daily-news", description: "Summarises the morning's news in ~/notes/news.md.", state: WorkerState.ENABLED, schedule: "Daily at 7 AM", cron: "0 7 * * *", timezone: "America/Chicago", hash: "77ab", path: "/home/me/.blitz/workers/daily-news/WORKER.md", limits: { maxTurns: 10, maxCostUsd: 0.2 }, spec: fakeSpec("daily-news", { schedule: "Daily at 7 AM", timezone: "America/Chicago", prompt: "Summarise the morning's news." }) },
];

const fakeElsewhere = [
  { workspace: "/home/me/notes", home: false, missing: false, problem: "", workers: [{ workspace: "/home/me/notes", name: "weekly-notes", state: WorkerState.ENABLED, schedule: "Fridays at 17:00", problems: [], permissions: [] }] },
  { workspace: "/home/me/old-site", home: false, missing: true, problem: "", workers: [{ workspace: "/home/me/old-site", name: "backup", state: WorkerState.ENABLED, problems: [], permissions: [] }] },
];

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
  {
    name: "sales-digest",
    description: "Writes last week's sales by region, with a chart, every Monday.",
    state: WorkerState.NEW,
    schedule: "Mondays at 06:00",
    cron: "0 6 * * 1",
    timezone: "America/Chicago",
    hash: "5e1d",
    path: "workers/sales-digest/WORKER.md",
    runsOn: "service",
    limits: { maxTurns: 30, maxCostUsd: 1 },
    spec: fakeSpec("sales-digest", { schedule: "Mondays at 06:00", timezone: "America/Chicago", prompt: "Write last week's sales by region to out/sales.csv, with a chart." }),
  },
];

const workersOf = (workspace: string) => (workspace === fakeHome ? fakeGlobal : fakeWorkers);

/** Returns the fake ConnectRPC handler implementation for WorkerService. */
export function fakeWorkerService(): ServiceImpl<typeof WorkerService> {
  const impl: Partial<ServiceImpl<typeof WorkerService>> = {
    createWorker: (r) => {
      const problems = jobProblems(r);
      if (problems.length) return { problems };
      const list = workersOf(r.workspace);
      if (list.some((w) => w.name === r.name)) throw new ConnectError(`a worker named ${r.name} exists`, Code.AlreadyExists);
      const { workspace, ...spec } = r;
      const w: FakeWorker = { workspace, name: r.name, description: r.description, state: WorkerState.NEW, schedule: r.schedule, cron: "0 6 * * *", timezone: r.timezone, agent: r.agent, model: r.model, permissions: r.permissions, limits: fakeLimits(r), path: `${workspace}/.agents/workers/${r.name}/WORKER.md`, hash: "sha256:new", problems: [], spec };
      list.unshift(w);
      return { worker: w };
    },
    moveWorkerToService: ({ name, force }) => {
      const problems = name === "nightly-deps" ? ["the MCP server github is a program on this computer (npx): the service can't start it", "it reaches localhost:5432, which on the service is the runner itself"] : [];
      const w = fakeWorkers.find((x) => x.name === name);
      if (!w) throw new ConnectError(`no worker ${name}`, Code.NotFound);
      if (problems.length && !force) return { problems, moved: false };
      w.runsOn = "service";
      return { problems, moved: true, worker: w };
    },
    listWorkers: ({ workspace }) => ({ workers: workspace === fakeHome ? fakeGlobal : fakeWorkers }),
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
    getWorkerSpec: ({ workspace, name }) => {
      const w = workersOf(workspace).find((x) => x.name === name);
      if (!w) throw new ConnectError(`no worker ${name}`, Code.NotFound);
      return { worker: w.spec, hash: w.hash };
    },
    updateWorker: ({ workspace, hash, worker }) => {
      const w = workersOf(workspace).find((x) => x.name === worker?.name);
      if (!w || !worker) throw new ConnectError(`no worker ${worker?.name}`, Code.NotFound);
      if (hash !== w.hash) {
        const err = new ConnectError(`${w.path} changed since it was loaded`, Code.FailedPrecondition);
        err.details.push({ desc: ErrorInfoSchema, value: { reason: "HASH_MISMATCH", metadata: {} } });
        throw err;
      }
      const problems = jobProblems(worker);
      if (problems.length) return { problems };
      w.spec = { ...worker };
      Object.assign(w, { description: worker.description, schedule: worker.schedule, timezone: worker.timezone, agent: worker.agent, model: worker.model, permissions: worker.permissions, limits: fakeLimits(worker), problems: [] });
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
    runWorker: ({ name }) => ({ run: { id: `run-${name}-${Date.now()}`, status: RunStatus.RUNNING, started: now(), manual: true } }),
    async *watchWorkerRun() {},
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
  };
  return impl as ServiceImpl<typeof WorkerService>;
}
