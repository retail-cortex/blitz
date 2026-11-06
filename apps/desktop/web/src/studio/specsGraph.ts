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

// Pure layout and prompt helpers for Studio › Specs (spec_040_studio STU-11,
// spec_012_memory MEM-45): places numbered specification stages in columns
// by their dependencies, checks for dependency loops, and formats the
// prompts that build, review or resolve a stage from the canvas.

/** A numbered requirement, objective or content item in a stage spec. */
export interface SpecItemInput { id: string; section: string; text: string; done?: boolean }

/** A stage spec as read from .agents/specs/NNN-<name>.md. */
export interface SpecStageInput {
  name: string; path: string; number: string; title: string; status: string; kind: string;
  dependsOn: string[]; context: string; goals: string[]; nonGoals: string[];
  items: SpecItemInput[]; doneWhen: string[]; doneWhenDone?: boolean[]; openQuestions: string[];
  skills: string[]; agents: string[]; content?: string; hash?: string;
}

/** Progress across a stage's requirements and done-when criteria. */
export interface StageProgress {
  itemsDone: number; itemsTotal: number; doneWhenDone: number; doneWhenTotal: number;
  totalChecks: number; doneChecks: number; percent: number; remainingItemIds: string[];
}

/** A stage spec positioned on the Studio › Specs canvas. */
export interface PositionedStage { stage: SpecStageInput; column: number; row: number; x: number; y: number }

/** A dependency link from an earlier stage to a stage that depends on it. */
export interface SpecEdge { id: string; source: string; target: string }

/** Summary counts across a workspace's specification stages. */
export interface SpecCounts { stages: number; done: number; inProgress: number; items: number; openQuestions: number }

/** Horizontal distance between dependency columns on the Specs canvas. */
export const specColumnGap = 250;

/** Vertical distance between stages in the same column on the Specs canvas. */
export const specRowGap = 112;

/** The valid stage statuses, in progression order. */
export const specStatuses = ["draft", "in-progress", "done"] as const;

/** A stage's status value. */
export type SpecStatus = (typeof specStatuses)[number];

/**
 * Computes the topological column and row for each stage in .agents/specs/
 * and returns the positioned nodes and dependency edges for the canvas.
 */
export function layoutSpecStages(stages: readonly SpecStageInput[]): { nodes: PositionedStage[]; edges: SpecEdge[] } {
  const byName = new Map<string, SpecStageInput>();
  for (const s of stages) byName.set(s.name, s);

  const memo = new Map<string, number>();
  const visiting = new Set<string>();
  const depthOf = (name: string): number => {
    const cached = memo.get(name);
    if (cached !== undefined) return cached;
    if (visiting.has(name)) return 0;
    visiting.add(name);
    const s = byName.get(name);
    let col = 0;
    if (s) {
      for (const dep of s.dependsOn) {
        if (byName.has(dep) && dep !== name) {
          col = Math.max(col, depthOf(dep) + 1);
        }
      }
    }
    visiting.delete(name);
    memo.set(name, col);
    return col;
  };

  const columns = new Map<number, SpecStageInput[]>();
  const edges: SpecEdge[] = [];
  for (const s of stages) {
    const col = depthOf(s.name);
    const list = columns.get(col) ?? [];
    list.push(s);
    columns.set(col, list);
    for (const dep of s.dependsOn) {
      if (byName.has(dep) && dep !== s.name) {
        edges.push({ id: `${dep}->${s.name}`, source: dep, target: s.name });
      }
    }
  }

  const nodes: PositionedStage[] = [];
  for (const [col, list] of [...columns.entries()].sort((a, b) => a[0] - b[0])) {
    list.forEach((stage, row) => {
      nodes.push({
        stage,
        column: col,
        row,
        x: 40 + col * specColumnGap,
        y: 40 + row * specRowGap,
      });
    });
  }
  nodes.sort((a, b) => a.stage.name.localeCompare(b.stage.name));
  return { nodes, edges };
}

/**
 * Reports whether stage `fromName` can depend on `candidateDep` without
 * depending on itself or forming a loop.
 */
export function canDependOn(stages: readonly SpecStageInput[], fromName: string, candidateDep: string): boolean {
  if (!fromName || !candidateDep || fromName === candidateDep) return false;
  const byName = new Map<string, string[]>();
  for (const s of stages) byName.set(s.name, s.dependsOn);
  if (!byName.has(fromName) || !byName.has(candidateDep)) return false;

  const nextDeps = new Set(byName.get(fromName) ?? []);
  nextDeps.add(candidateDep);

  const visiting = new Set<string>();
  const visited = new Set<string>();
  const hasLoop = (cur: string): boolean => {
    if (visiting.has(cur)) return true;
    if (visited.has(cur)) return false;
    visiting.add(cur);
    const deps = cur === fromName ? [...nextDeps] : (byName.get(cur) ?? []);
    for (const d of deps) {
      if (hasLoop(d)) return true;
    }
    visiting.delete(cur);
    visited.add(cur);
    return false;
  };
  return !hasLoop(fromName);
}

/**
 * Toggles `dep` in `dependsOn` and returns the sorted list of stage names.
 */
export function toggleSpecDependency(dependsOn: readonly string[], dep: string): string[] {
  const next = dependsOn.includes(dep) ? dependsOn.filter((d) => d !== dep) : [...dependsOn, dep];
  return [...new Set(next)].sort();
}

/**
 * Counts stages by status, requirements/objectives, and open questions.
 */
export function countSpecs(stages: readonly SpecStageInput[]): SpecCounts {
  let done = 0;
  let inProgress = 0;
  let items = 0;
  let openQuestions = 0;
  for (const s of stages) {
    if (s.status === "done") done++;
    else if (s.status === "in-progress") inProgress++;
    items += s.items.length;
    openQuestions += s.openQuestions.length;
  }
  return { stages: stages.length, done, inProgress, items, openQuestions };
}

/**
 * Computes checklist completion progress for a stage's items and done-when criteria.
 */
export function stageProgress(stage: SpecStageInput): StageProgress {
  let itemsDone = 0;
  const remainingItemIds: string[] = [];
  for (const it of stage.items) {
    if (it.done) itemsDone++;
    else remainingItemIds.push(it.id);
  }
  const dwFlags = stage.doneWhenDone ?? [];
  let doneWhenDone = 0;
  for (let i = 0; i < stage.doneWhen.length; i++) {
    if (dwFlags[i]) doneWhenDone++;
  }
  const totalChecks = stage.items.length + stage.doneWhen.length;
  const doneChecks = itemsDone + doneWhenDone;
  const percent = totalChecks > 0 ? Math.round((doneChecks * 100) / totalChecks) : stage.status === "done" ? 100 : 0;
  return {
    itemsDone,
    itemsTotal: stage.items.length,
    doneWhenDone,
    doneWhenTotal: stage.doneWhen.length,
    totalChecks,
    doneChecks,
    percent,
    remainingItemIds,
  };
}

/**
 * Formats the composer prompt to build a stage (or its selected item IDs).
 */
export function buildStagePrompt(stage: SpecStageInput, selectedIds: readonly string[]): string {
  const picked = stage.items.map((it) => it.id).filter((id) => selectedIds.includes(id));
  const label = `${stage.number ? `${stage.number} · ` : ""}${stage.title}`;
  if (picked.length > 0) {
    return `Implement ${picked.join(", ")} from @${stage.path} (${label}) and verify its done-when criteria.`;
  }
  return `Work on stage ${label} (@${stage.path}) and satisfy its requirements and done-when criteria.`;
}

/**
 * Formats the in-place AI draft request to fold the user's open-question
 * answers into the stage's requirements and done-when criteria. Returns ""
 * when no answers have been typed.
 */
export function resolveQuestionsDraftRequest(stage: SpecStageInput, answers: Readonly<Record<number, string>>): string {
  const pairs: string[] = [];
  stage.openQuestions.forEach((q, idx) => {
    const a = (answers[idx] ?? "").trim();
    if (a) pairs.push(`- Question: ${q}\n  Decision: ${a}`);
  });
  if (pairs.length === 0) return "";
  return `Resolve these open questions in ${stage.name} using the user's decisions below, fold the decisions into Context, Goals, Requirements and Done when, and remove the answered questions from Open questions:\n${pairs.join("\n")}`;
}

/**
 * Formats the `/spec` command to resolve a stage's open questions (including any typed answers).
 */
export function resolveQuestionsPrompt(stage: SpecStageInput, answers?: Readonly<Record<number, string>>): string {
  const pairs: string[] = [];
  if (answers) {
    stage.openQuestions.forEach((q, idx) => {
      const a = (answers[idx] ?? "").trim();
      if (a) pairs.push(`${q} -> ${a}`);
    });
  }
  if (pairs.length > 0) {
    return `/spec Resolve the open questions in ${stage.path} with these decisions: ${pairs.join("; ")}`;
  }
  return `/spec Resolve the open questions in ${stage.path}`;
}

/**
 * Formats the `/review` command to check the workspace against a stage spec.
 */
export function reviewStagePrompt(stage: SpecStageInput): string {
  const label = `${stage.number ? `${stage.number} · ` : ""}${stage.title}`;
  return `/review Check the workspace against @${stage.path} (${label}).`;
}

const itemLineRe = /^\s*(?:[-*]|\d+\.)\s+(?:\[([xX ])\]\s+)?(?:\*\*([A-Z]{2,6}-\d+)\*\*|([A-Z]{2,6}-\d+))\s*[:—–-]?\s*(.+)$/;
const checkLineRe = /^\s*(?:[-*]|\d+\.)\s+(?:\[([xX ])\]\s+)?(.+)$/;

/**
 * Parses an unsaved stage Markdown draft over `base` so the structured
 * workbench can preview AI drafts before saving to disk (STU-05).
 */
export function parseSpecStageDraft(base: SpecStageInput, markdown: string): SpecStageInput {
  if (!markdown.trim()) return base;
  let title = base.title;
  let status = base.status;
  const ctxLines: string[] = [];
  const items: SpecItemInput[] = [];
  const doneWhen: string[] = [];
  const doneWhenDone: boolean[] = [];
  const openQuestions: string[] = [];
  let section = "";
  for (const raw of markdown.split(/\r?\n/)) {
    const line = raw.trim();
    if (line.startsWith("# ") && !line.startsWith("## ")) {
      const h = line.replace(/^#\s+(?:\d{1,3}[.:-]?\s+)?/, "").trim();
      if (h) title = h;
      continue;
    }
    if (line.startsWith("## ")) {
      section = line.slice(3).trim().toLowerCase();
      continue;
    }
    if (!section) {
      const st = /^status\s*:\s*(.+)$/i.exec(line);
      if (st) status = st[1].trim().toLowerCase();
      continue;
    }
    if (section === "context" && line) {
      ctxLines.push(line);
    } else if (section === "requirements" || section === "objectives" || section === "content") {
      const m = itemLineRe.exec(line);
      if (m) items.push({ id: m[2] || m[3], section, text: m[4].trim(), done: (m[1] ?? "").toLowerCase() === "x" });
    } else if (section === "done when" || section === "acceptance criteria") {
      const m = checkLineRe.exec(line);
      if (m && !/^none\.?$/i.test(m[2].trim())) {
        doneWhen.push(m[2].trim());
        doneWhenDone.push((m[1] ?? "").toLowerCase() === "x");
      }
    } else if (section === "open questions" || section === "unresolved questions") {
      const m = checkLineRe.exec(line);
      if (m && !/^none\.?$/i.test(m[2].trim())) openQuestions.push(m[2].trim());
    }
  }
  return {
    ...base,
    title,
    status,
    context: ctxLines.length > 0 ? ctxLines.join(" ") : base.context,
    items: items.length > 0 ? items : base.items,
    doneWhen: doneWhen.length > 0 ? doneWhen : base.doneWhen,
    doneWhenDone: doneWhen.length > 0 ? doneWhenDone : base.doneWhenDone,
    openQuestions,
    content: markdown,
  };
}

/** Per-stage unsaved workbench state kept across stage and Studio tab switches. */
export interface SpecStageDraftState {
  answers: Record<number, string>;
  newReq: string;
  newDoneWhen: string;
  newQuestion: string;
  stagedContent: string;
  changes: string[];
  baseHash: string;
  rawMode: boolean;
  title?: string;
  context?: string;
  deps?: string[];
}

const emptyStageDraft = (): SpecStageDraftState => ({
  answers: {}, newReq: "", newDoneWhen: "", newQuestion: "", stagedContent: "", changes: [], baseHash: "", rawMode: false,
});

const draftStoreKey = "blitz.specs.drafts.v1";
const stageDrafts = new Map<string, SpecStageDraftState>();

function loadDraftMap(): void {
  if (stageDrafts.size > 0) return;
  try {
    const raw = typeof sessionStorage !== "undefined" ? sessionStorage.getItem(draftStoreKey) : null;
    if (!raw) return;
    for (const [k, v] of Object.entries(JSON.parse(raw) as Record<string, Partial<SpecStageDraftState>>)) {
      stageDrafts.set(k, { ...emptyStageDraft(), ...v });
    }
  } catch { /* ignore storage restrictions */ }
}

function persistDraftMap(): void {
  try {
    if (typeof sessionStorage !== "undefined") sessionStorage.setItem(draftStoreKey, JSON.stringify(Object.fromEntries(stageDrafts)));
  } catch { /* ignore storage quota errors */ }
}

/** Reads the unsaved workbench state for `(workspace, stageName)`. */
export function getStageDraft(workspace: string, stageName: string): SpecStageDraftState {
  loadDraftMap();
  return stageDrafts.get(`${workspace}::${stageName}`) ?? emptyStageDraft();
}

/** Updates the unsaved workbench state for `(workspace, stageName)`. */
export function setStageDraft(workspace: string, stageName: string, patch: Partial<SpecStageDraftState>): SpecStageDraftState {
  loadDraftMap();
  const next = { ...getStageDraft(workspace, stageName), ...patch };
  stageDrafts.set(`${workspace}::${stageName}`, next);
  persistDraftMap();
  return next;
}

/** Reports whether `(workspace, stageName)` has any unsaved draft or typed input. */
export function hasUnsavedStageDraft(workspace: string, stageName: string): boolean {
  const d = getStageDraft(workspace, stageName);
  return Boolean(
    d.stagedContent.trim() || d.newReq.trim() || d.newDoneWhen.trim() || d.newQuestion.trim() ||
      Object.values(d.answers).some((v) => v.trim().length > 0),
  );
}

/** Clears all unsaved state in the draft store (used in tests). */
export function resetSpecDraftStore(): void {
  stageDrafts.clear();
  try { if (typeof sessionStorage !== "undefined") sessionStorage.removeItem(draftStoreKey); } catch { /* ignore */ }
}
