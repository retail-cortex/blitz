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

// Studio's eval form (spec_evals_042): an eval as the form holds it, from
// and to the API, and its runs summed up. A case's threshold is typed as
// text; each case has a key of its own so the list can be reordered.

import type { EvalCase, EvalFile, EvalResult, EvalRun } from "../gen/blitz/v1/workspace_pb";

/** A case's output type. */
export type CaseType = "exact" | "near";
/** A case's scope: what its failure means. */
export type CaseScope = "fail" | "warn" | "info";
/** How near cases are scored. */
export type NearMethod = "judge" | "embedding";

/** A case as the form holds it. */
export interface CaseForm {
  key: number;
  id: string;
  name: string;
  prompt: string;
  output: string;
  type: CaseType;
  threshold: string;
  scope: CaseScope;
}

/** What the form holds. */
export interface EvalForm {
  name: string;
  description: string;
  agents: string[];
  skills: string[];
  near: NearMethod;
  allowNetwork: boolean;
  allowMcp: boolean;
  cases: CaseForm[];
}

/** A near case's threshold when it has none. */
export const defaultThreshold = 0.8;

let keys = 0;
const nextKey = () => ++keys;

/** A new eval's form: no cases yet, judged. */
export const emptyEvalForm: EvalForm = { name: "", description: "", agents: [], skills: [], near: "judge", allowNetwork: false, allowMcp: false, cases: [] };

/** The next free case ID: c1, c2… */
export function nextCaseId(cases: CaseForm[]): string {
  const taken = new Set(cases.map((c) => c.id));
  let n = cases.length + 1;
  while (taken.has(`c${n}`)) n++;
  return `c${n}`;
}

/** A new case: near, scoped fail, at the default threshold. */
export function newCase(cases: CaseForm[]): CaseForm {
  return { key: nextKey(), id: nextCaseId(cases), name: "", prompt: "", output: "", type: "near", threshold: String(defaultThreshold), scope: "fail" };
}

/** The form for an eval as read. */
export function formFromEval(e: EvalFile): EvalForm {
  return {
    name: e.name,
    description: e.description,
    agents: [...e.agents],
    skills: [...e.skills],
    near: e.near === "embedding" ? "embedding" : "judge",
    allowNetwork: e.allow.includes("network"),
    allowMcp: e.allow.includes("mcp"),
    cases: casesFromDraft(e.cases),
  };
}

/** Drafted cases, as the form holds them. */
export function casesFromDraft(cases: EvalCase[]): CaseForm[] {
  return cases.map((c) => ({
    key: nextKey(),
    id: c.id,
    name: c.name,
    prompt: c.prompt,
    output: c.output,
    type: c.type === "exact" ? "exact" : "near",
    threshold: String(c.type === "exact" ? defaultThreshold : c.threshold || defaultThreshold),
    scope: c.scope === "warn" || c.scope === "info" ? c.scope : "fail",
  }));
}

/** The form as SaveEval takes it. */
export function specFromForm(f: EvalForm) {
  return {
    name: f.name.trim(),
    description: f.description.trim(),
    agents: f.agents,
    skills: f.skills,
    near: f.near,
    allow: [...(f.allowNetwork ? ["network"] : []), ...(f.allowMcp ? ["mcp"] : [])],
    cases: f.cases.map((c) => ({ id: c.id.trim(), name: c.name.trim(), prompt: c.prompt, output: c.output, type: c.type, threshold: c.type === "near" ? Number(c.threshold) || 0 : 0, scope: c.scope })),
  };
}

/** Whether two forms hold the same eval. */
export function sameEvalForm(a: EvalForm, b: EvalForm): boolean {
  return JSON.stringify(specFromForm(a)) === JSON.stringify(specFromForm(b));
}

/** The case IDs more than one case has. */
export function duplicateIds(cases: CaseForm[]): Set<string> {
  const seen = new Set<string>();
  const dup = new Set<string>();
  for (const c of cases) {
    const id = c.id.trim();
    if (seen.has(id)) dup.add(id);
    seen.add(id);
  }
  return dup;
}

/** Moves the case at index i by d (−1 up, +1 down), if it can. */
export function moveCase(cases: CaseForm[], i: number, d: number): CaseForm[] {
  const j = i + d;
  if (j < 0 || j >= cases.length) return cases;
  const out = [...cases];
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}

/** The statuses a run's results can have, in the order they're summed up. */
export const resultStatuses = ["pass", "fail", "warn", "info", "error"] as const;

/** A run's results counted by status (a listed run carries its counts; a live one, its results). */
export function runCounts(run: Pick<EvalRun, "counts" | "results">): Record<string, number> {
  if (run.results.length) {
    const out: Record<string, number> = {};
    for (const r of run.results) out[r.status] = (out[r.status] ?? 0) + 1;
    return out;
  }
  return { ...run.counts };
}

/** Each case's latest results, by case ID. */
export function resultsByCase(results: EvalResult[]): Map<string, EvalResult[]> {
  const out = new Map<string, EvalResult[]>();
  for (const r of results) out.set(r.case, [...(out.get(r.case) ?? []), r]);
  return out;
}
