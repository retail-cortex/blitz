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

// Which evals check an agent or a skill (spec_evals_042 EV-12), what to
// ask when refining an agent from its failures, and running evals in the
// background after a save.

import { workspaces } from "../api";
import { message } from "../errors";
import type { EvalFile, EvalResult } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";

/** The evals that name agent among their agents. */
export const evalsForAgent = (evals: EvalFile[], agent: string) => evals.filter((e) => e.agents.includes(agent));

/** The evals that give their cases skill. */
export const evalsForSkill = (evals: EvalFile[], skill: string) => evals.filter((e) => e.skills.includes(skill));

/** A name for an agent's first eval: its own, or with "-evals" when that's taken. */
export function evalNameFor(agent: string, taken: string[]): string {
  if (!taken.includes(agent)) return agent;
  let name = `${agent}-evals`;
  for (let n = 2; taken.includes(name); n++) name = `${agent}-evals-${n}`;
  return name;
}

/** What Refine asks for: the agent's cases that didn't pass, each with what was expected, what it said and why. */
export function refineRequest(results: EvalResult[], agent: string): string {
  const missed = results.filter((r) => r.agent === agent && r.status !== "pass" && r.status !== "error");
  if (!missed.length) return "";
  const lines = missed.map((r) => {
    const why = r.reason ? `\n  Why: ${r.reason}` : "";
    return `- Prompt: ${r.prompt}\n  Expected: ${r.expected}\n  It answered: ${r.output || "(nothing)"}${why}`;
  });
  return `Change the instructions so the agent passes these eval cases it missed, without breaking what it does well:\n${lines.join("\n")}`;
}

/** The agents with results that didn't pass (an error isn't the agent's doing). */
export const agentsToRefine = (results: EvalResult[]) => [...new Set(results.filter((r) => r.status !== "pass" && r.status !== "error").map((r) => r.agent))];

/** Runs evals one after another in the background, then says how they went. */
export async function runInBackground(workspace: string, names: string[], snack: (text: string, opts?: { error?: boolean }) => void) {
  let failed = 0;
  for (const name of names) {
    try {
      for await (const res of workspaces.runEval({ workspace, name })) {
        if (res.event.case === "finished" && res.event.value.status !== "passed") failed++;
      }
    } catch (e) {
      snack(t("desktop.evals.background_error", { name, error: message(e) }), { error: true });
      return;
    }
  }
  snack(failed ? tn("desktop.evals.background_failed", failed, { total: names.length }) : tn("desktop.evals.background_passed", names.length), { error: failed > 0 });
}
