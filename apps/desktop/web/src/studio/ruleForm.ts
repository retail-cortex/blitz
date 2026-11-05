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

// Studio's rule form (spec_041_gates): a rule as the form holds it, the
// lists it shows (before the agent, after, instructions) and moving a
// rule within its list.

import type { RuleFile } from "../gen/blitz/v1/workspace_pb";

/** When a rule applies: a gate before or after the agent, or an instruction. */
export type RuleWhen = "before_agent" | "after_agent" | "";
/** What a failing gate does. */
export type RuleAction = "block" | "warn" | "revise";

/** What the form holds. Paths are typed comma-separated. */
export interface RuleForm {
  name: string;
  description: string;
  /** A RuleWhen, or the file's own when as written when it isn't one (a problem the form shows, kept so saving doesn't turn a gate into an instruction). */
  when: RuleWhen | (string & {});
  /** A RuleAction, or the file's own action when it isn't one. */
  action: RuleAction | (string & {});
  order: number;
  agents: string[];
  paths: string;
  text: string;
}

/** A new rule's form: a before-agent gate that warns. */
export const emptyRuleForm: RuleForm = { name: "", description: "", when: "before_agent", action: "warn", order: 0, agents: [], paths: "", text: "" };

/** Whether when is one the form offers. */
export const knownWhen = (when: string): when is RuleWhen => when === "" || when === "before_agent" || when === "after_agent";
/** Whether action is one the form offers. */
export const knownAction = (action: string): action is RuleAction => action === "block" || action === "warn" || action === "revise";

/**
 * The form for a rule as read. A when or action the form doesn't know is
 * kept as written (the rule's problem says why), so saving shows Go's
 * problem again instead of rewriting a gate as an instruction rule.
 */
export function formFromRule(r: RuleFile): RuleForm {
  return { name: r.name, description: r.description, when: r.when, action: r.action || "warn", order: r.order, agents: [...r.agents], paths: r.paths.join(", "), text: r.text };
}

/** The form as SaveRule takes it. */
export function specFromForm(f: RuleForm) {
  return {
    name: f.name.trim(),
    description: f.description.trim(),
    when: f.when,
    action: f.when ? f.action : "",
    order: f.when ? f.order : 0,
    agents: f.when ? f.agents : [],
    paths: f.when
      ? []
      : f.paths
          .split(",")
          .map((p) => p.trim())
          .filter(Boolean),
    text: f.text,
  };
}

/** Whether two forms hold the same rule. */
export function sameRuleForm(a: RuleForm, b: RuleForm): boolean {
  return JSON.stringify(specFromForm(a)) === JSON.stringify(specFromForm(b));
}

/** Whether name may name a rule file: lowercase letters, digits, single hyphens or underscores, up to 64 (a copy of gates.ValidName, for the form to say at once; SaveRule checks it too). */
export function validRuleName(name: string): boolean {
  return name.length <= 64 && /^[a-z0-9]+([-_][a-z0-9]+)*$/.test(name);
}

/**
 * The lists Studio shows: gates before the agent and after, in the order
 * they run, instruction rules, and the rest, with a problem (a misspelt
 * when, say), which don't run until their file is fixed.
 */
export function ruleSections(rules: readonly RuleFile[]): { before: RuleFile[]; after: RuleFile[]; instructions: RuleFile[]; broken: RuleFile[] } {
  const gate = (r: RuleFile) => r.when === "before_agent" || r.when === "after_agent";
  return {
    before: rules.filter((r) => r.when === "before_agent"),
    after: rules.filter((r) => r.when === "after_agent"),
    instructions: rules.filter((r) => !gate(r) && !r.problem),
    broken: rules.filter((r) => !gate(r) && r.problem),
  };
}

/** names with the one at from moved to to (a drag, or ↑ ↓). */
export function moved(names: readonly string[], from: number, to: number): string[] {
  const out = [...names];
  if (from < 0 || from >= out.length || to < 0 || to >= out.length || from === to) return out;
  const [n] = out.splice(from, 1);
  out.splice(to, 0, n);
  return out;
}

/** The order a new gate takes in its list: after the last one. */
export function nextOrder(list: readonly RuleFile[]): number {
  return list.reduce((m, r) => Math.max(m, r.order), 0) + 10;
}
