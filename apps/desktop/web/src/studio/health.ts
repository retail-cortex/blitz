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

// The workspace dashboard's state and arithmetic (spec_health_044): the
// service's WorkspaceHealth, followed live, and what the Overview and
// Studio's grade badges make of it.

import { useCallback, useEffect, useState } from "react";
import { workspaces } from "../api";
import type { HealthCheckScope, HealthDay, HealthItem, WorkspaceHealth } from "../gen/blitz/v1/workspace_pb";
import type { MessageInitShape } from "@bufbuild/protobuf";
import type { HealthCheckScopeSchema } from "../gen/blitz/v1/workspace_pb";
import type { Column } from "../ui/charts";

/** The kinds of item, in the dashboard's order. */
export const healthKinds = ["agent", "skill", "rule", "guardrail", "eval", "workflow", "worker"] as const;
/** A kind of item the dashboard judges. */
export type HealthKind = (typeof healthKinds)[number];

/** Each kind's Studio list. */
export const studioKindOf = { agent: "agents", skill: "skills", rule: "rules", guardrail: "guardrails", eval: "evals", workflow: "workflows", worker: "workers" } as const;

/** A score's letter, as the service grades: A from 4.5, B from 3.5, C from 2.5, else D; "" for none. */
export function grade(score: number): string {
  if (!score || score <= 0) return "";
  if (score >= 4.5) return "A";
  if (score >= 3.5) return "B";
  if (score >= 2.5) return "C";
  return "D";
}

/** An item's key: kind and name, as the service's. */
export const itemKey = (it: Pick<HealthItem, "kind" | "name">) => `${it.kind}/${it.name}`;

/** How often the dashboard is asked for while shown. */
export const pollMs = 5_000;
/** How often it's asked for while a check runs. */
export const checkingPollMs = 1_500;

/**
 * The dashboard of a workspace, asked for while shown (every pollMs, more
 * often while a check runs); refresh asks again now. It's asked for rather
 * than streamed: the window has a few connections to the service, and a
 * stream would hold one for as long as Studio is open.
 */
export function useWorkspaceHealth(workspace: string): { health?: WorkspaceHealth; error: string; refresh: () => void } {
  const [health, setHealth] = useState<WorkspaceHealth>();
  const [error, setError] = useState("");
  const [tick, setTick] = useState(0);
  const refresh = useCallback(() => setTick((n) => n + 1), []);
  const checking = !!health?.checking;
  useEffect(() => {
    let live = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const ask = async () => {
      try {
        const r = await workspaces.getWorkspaceHealth({ workspace });
        if (!live) return;
        setHealth(r.health);
        setError("");
      } catch (e) {
        if (live) setError(String(e));
      }
      if (live) timer = setTimeout(ask, checking ? checkingPollMs : pollMs);
    };
    void ask();
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [workspace, checking, tick]);
  return { health, error, refresh };
}

/** How urgent something needing attention is. */
export type Level = "error" | "warn" | "info";

/** One thing needing attention: about an item (or a signal), why, and how urgent. */
export interface Attention {
  key: string;
  kind: string;
  name: string;
  level: Level;
  /** An i18n key under desktop.health.why, and its values. */
  why: string;
  values?: Record<string, string | number>;
}

const order: Record<Level, number> = { error: 0, warn: 1, info: 2 };

/** What needs attention, most urgent first: problems, failing runs and low grades, then stale or missing evals, then signals and unused items. */
export function attention(h: Pick<WorkspaceHealth, "items" | "signals">): Attention[] {
  const out: Attention[] = [];
  for (const it of h.items) {
    const base = { key: itemKey(it), kind: it.kind, name: it.name };
    if (it.problem) out.push({ ...base, level: "error", why: "problem", values: { problem: it.problem } });
    const g = it.verdict?.grade ?? "";
    if (g === "D") out.push({ ...base, level: "error", why: "grade", values: { grade: g } });
    if (g === "C") out.push({ ...base, level: "warn", why: "grade", values: { grade: g } });
    for (const f of it.flags) {
      if (f === "failing") out.push({ ...base, level: "error", why: "failing", values: { failed: it.runs?.failed ?? 0, runs: it.runs?.runs ?? 0 } });
      if (f === "stale" || f === "uncovered") out.push({ ...base, level: "warn", why: f });
      if (f === "unused" || f === "never_run") out.push({ ...base, level: "info", why: f });
    }
  }
  for (const s of h.signals) {
    out.push({ key: `${s.kind}/${s.about}`, kind: s.kind, name: s.about, level: s.kind === "denial" ? "warn" : "info", why: `signal_${s.kind}`, values: { count: s.count, detail: s.detail } });
  }
  return out.sort((a, b) => order[a.level] - order[b.level] || a.key.localeCompare(b.key));
}

/** A kind's summary: how many items, with a problem, flagged, and by grade. */
export function kindSummary(items: HealthItem[], kind: string) {
  const mine = items.filter((it) => it.kind === kind);
  const grades: Record<string, number> = { A: 0, B: 0, C: 0, D: 0 };
  for (const it of mine) if (it.verdict?.grade) grades[it.verdict.grade]++;
  return { count: mine.length, problems: mine.filter((it) => it.problem).length, flagged: mine.filter((it) => it.flags.length > 0).length, judged: mine.filter((it) => it.verdict).length, grades };
}

/** The days as Columns: done, failed and stopped stacked, cost as the line. */
export function dayColumns(days: HealthDay[], title: (d: HealthDay) => string): Column[] {
  return days.map((d) => ({
    key: d.date,
    parts: [
      { value: d.done, tone: "done" },
      { value: d.failed, tone: "failed" },
      { value: d.stopped, tone: "stopped" },
    ],
    line: d.costUsd,
    title: title(d),
  }));
}

/** The runs' totals over the days: runs, done and cost. */
export function dayTotals(days: HealthDay[]) {
  let runs = 0,
    done = 0,
    cost = 0;
  for (const d of days) {
    runs += d.done + d.failed + d.stopped;
    done += d.done;
    cost += d.costUsd;
  }
  return { runs, done, cost, rate: runs ? done / runs : 0 };
}

/** The items with runs, most by key first (cost or failures), at most n. */
export function topRuns(items: HealthItem[], by: "cost" | "failed", n = 5): HealthItem[] {
  const v = (it: HealthItem) => (by === "cost" ? (it.runs?.costUsd ?? 0) : (it.runs?.failed ?? 0));
  return items.filter((it) => it.runs && v(it) > 0).sort((a, b) => v(b) - v(a) || a.name.localeCompare(b.name)).slice(0, n);
}

/** The change of a score since before, rounded to a tenth ("" when either is missing or it didn't move). */
export function delta(now: number, before: number): string {
  if (!now || !before) return "";
  const d = Math.round((now - before) * 10) / 10;
  if (d === 0) return "";
  return d > 0 ? `+${d.toFixed(1)}` : d.toFixed(1);
}

/** Starts a check of scope in the service, which runs it whoever watches; its progress shows in the dashboard. */
export async function startCheck(workspace: string, scope: MessageInitShape<typeof HealthCheckScopeSchema>) {
  await workspaces.startHealthCheck({ workspace, scope });
}

/** Stops the workspace's check after the item being judged. */
export async function stopCheck(workspace: string) {
  await workspaces.stopHealthCheck({ workspace }).catch(() => {});
}

/** Whether a check just ended: it was checking before and isn't now. */
export const checkEnded = (before: Pick<WorkspaceHealth, "checking"> | undefined, now: Pick<WorkspaceHealth, "checking"> | undefined) => !!before?.checking && !!now && !now.checking;

export type { HealthCheckScope };
