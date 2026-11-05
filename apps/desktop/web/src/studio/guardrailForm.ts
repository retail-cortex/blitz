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

// Studio's guardrail form (spec_041_gates): a guardrail as the form
// holds it, from and to the API. Lists are typed as text: tools and
// domains comma-separated, commands one a line.

import type { BuiltinGuardrail, GuardrailFile } from "../gen/blitz/v1/workspace_pb";

/** Where a guardrail checks. */
export type GuardStage = "input" | "tool_result" | "output" | "action";
/** The stages, in the order a turn meets them. */
export const guardStages: GuardStage[] = ["input", "tool_result", "output", "action"];
/** The built-in guardrails, in the order Studio lists them. */
const builtinIds = ["prompt_injection", "secrets", "limits"] as const;
/** A built-in guardrail's id. */
export type BuiltinId = (typeof builtinIds)[number];

/** What the form holds. */
export interface GuardrailForm {
  name: string;
  builtin: BuiltinId | "";
  stage: GuardStage;
  action: string;
  agents: string[];
  tools: string;
  description: string;
  pii: boolean;
  judge: boolean;
  maxCostUsd: string;
  maxTurns: string;
  maxFilesChanged: string;
  domains: string;
  commands: string;
  text: string;
  /** Switched off (enabled: false), its settings kept. */
  off: boolean;
}

/** A new custom guardrail's form: an action check that blocks. */
export const emptyGuardrailForm: GuardrailForm = { name: "", builtin: "", stage: "action", action: "block", agents: [], tools: "", description: "", pii: false, judge: false, maxCostUsd: "", maxTurns: "", maxFilesChanged: "", domains: "", commands: "", text: "", off: false };

const num = (n: number) => (n ? String(n) : "");

/** The form for a guardrail as read. */
export function formFromGuardrail(g: GuardrailFile): GuardrailForm {
  const l = g.limits;
  const builtin = (builtinIds as readonly string[]).includes(g.builtin) ? (g.builtin as BuiltinId) : "";
  return {
    name: g.name,
    builtin,
    stage: (guardStages as string[]).includes(g.stage) ? (g.stage as GuardStage) : "action",
    action: g.action,
    agents: [...g.agents],
    tools: g.tools.join(", "),
    description: g.description,
    pii: g.pii,
    judge: g.judge,
    maxCostUsd: String(l?.maxCostUsd ?? defaultLimits.maxCostUsd),
    maxTurns: String(l?.maxTurns ?? defaultLimits.maxTurns),
    maxFilesChanged: num(l?.maxFilesChanged ?? 0),
    domains: (l?.allowedDomains ?? []).join(", "),
    commands: (l?.commands ?? []).join("\n"),
    text: g.text,
    off: g.off,
  };
}

/**
 * The limits guardrail's commands line for Blitz's own list
 * (gates.DefaultsEntry): kept as the word, so a file follows the
 * defaults as they change instead of keeping a copy.
 */
export const defaultsEntry = "defaults";

/** The limits guardrail's ceilings without a file (gates.DefaultMaxTurns, DefaultMaxCostUSD). */
export const defaultLimits = { maxTurns: "200", maxCostUsd: "10" };

/**
 * A built-in's form with its defaults (on), its stage and action as the
 * service lists them (ListGuardrails' BuiltinGuardrail): the injection
 * judge on; limits' defaults and ceilings.
 */
export function formForBuiltin(b: Pick<BuiltinGuardrail, "id" | "stage" | "defaultAction">): GuardrailForm {
  const id = b.id as BuiltinId;
  const stage = (guardStages as string[]).includes(b.stage) ? (b.stage as GuardStage) : "action";
  const base = { ...emptyGuardrailForm, name: id, builtin: id, stage, action: b.defaultAction };
  if (id === "prompt_injection") return { ...base, judge: true };
  if (id === "limits") return { ...base, commands: defaultsEntry, ...defaultLimits };
  return base;
}

const split = (s: string, by: RegExp) =>
  s
    .split(by)
    .map((x) => x.trim())
    .filter(Boolean);

/**
 * A limits ceiling as typed: empty is the default (sent as the default,
 * which the service leaves out of the file, so it follows the default),
 * 0 is no limit, and anything else not a number at or above 0 (a whole
 * one if whole) is NaN.
 */
function limitValue(s: string, fallback: number, whole: boolean): number {
  const v = s.trim();
  if (!v) return fallback;
  if (!/^\d+(\.\d+)?$|^\.\d+$/.test(v)) return NaN;
  const n = Number(v);
  return whole && !Number.isInteger(n) ? NaN : n;
}

/** Whether a ceiling as typed is 0: no limit. */
export const noLimit = (s: string) => s.trim() !== "" && limitValue(s, 1, false) === 0;

/** The limits fields that aren't a number the guardrail takes (the form shows these and doesn't save). */
export function limitsProblems(f: GuardrailForm): Partial<Record<"maxCostUsd" | "maxTurns" | "maxFilesChanged", true>> {
  if (f.builtin !== "limits") return {};
  const out: Partial<Record<"maxCostUsd" | "maxTurns" | "maxFilesChanged", true>> = {};
  if (Number.isNaN(limitValue(f.maxCostUsd, 0, false))) out.maxCostUsd = true;
  if (Number.isNaN(limitValue(f.maxTurns, 0, true))) out.maxTurns = true;
  if (Number.isNaN(limitValue(f.maxFilesChanged, 0, true))) out.maxFilesChanged = true;
  return out;
}

/** The form as SaveGuardrail takes it: a built-in's own settings only. */
export function specFromForm(f: GuardrailForm) {
  const b = f.builtin;
  return {
    name: b || f.name.trim(),
    builtin: b,
    stage: b ? "" : f.stage,
    action: f.action,
    agents: f.agents,
    tools: b === "secrets" || b === "limits" || (!b && (f.stage === "input" || f.stage === "output")) ? [] : split(f.tools, /,/),
    description: f.description.trim(),
    pii: b === "secrets" && f.pii,
    judge: b === "prompt_injection" && f.judge,
    limits:
      b === "limits"
        ? {
            maxCostUsd: limitValue(f.maxCostUsd, Number(defaultLimits.maxCostUsd), false),
            maxTurns: limitValue(f.maxTurns, Number(defaultLimits.maxTurns), true),
            maxFilesChanged: limitValue(f.maxFilesChanged, 0, true),
            allowedDomains: split(f.domains, /,/),
            commands: split(f.commands, /\n/),
          }
        : undefined,
    text: b ? "" : f.text,
    off: f.off,
  };
}

/** Whether two forms hold the same guardrail. */
export function sameGuardrailForm(a: GuardrailForm, b: GuardrailForm): boolean {
  return JSON.stringify(specFromForm(a)) === JSON.stringify(specFromForm(b));
}

/** Whether the stage checks tools (and so takes a list of them). */
export const stageHasTools = (s: GuardStage) => s === "tool_result" || s === "action";
