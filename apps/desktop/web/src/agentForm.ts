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

// The agent editor's form: its fields, the agent file they make, and what
// the form checks before the service does.
import { create } from "@bufbuild/protobuf";
import { AgentDefinitionSchema, type AgentDefinition } from "./gen/blitz/v1/workspace_pb";
import { checkModelRef, emptyCatalog } from "./models";

/** The form's fields, as typed (numbers as text: "" leaves them unset). */
export interface AgentForm {
  name: string;
  displayName: string;
  description: string;
  tools: string[];
  defaultModel: string;
  /** "" is high. */
  agencyLevel: string;
  /** "" is the workspace's. */
  permissionMode: string;
  maxTurns: string;
  background: boolean;
  isolation: "" | "worktree";
  temperature: string;
  topP: string;
  maxTokens: string;
  /** "" is the model's own. */
  effort: string;
  thinkingBudget: string;
  prompt: string;
}

/** A blank form. */
export const emptyAgentForm: AgentForm = {
  name: "",
  displayName: "",
  description: "",
  tools: [],
  defaultModel: "",
  agencyLevel: "",
  permissionMode: "",
  maxTurns: "",
  background: false,
  isolation: "",
  temperature: "",
  topP: "",
  maxTokens: "",
  effort: "",
  thinkingBudget: "",
  prompt: "",
};

/** A field the form can find wrong. */
export type AgentField = "name" | "description" | "defaultModel" | "maxTurns" | "temperature" | "topP" | "maxTokens" | "thinkingBudget";

/** Whether a name can be an agent's (as the service checks it). */
export const validAgentName = (name: string) => /^[a-z0-9][a-z0-9_-]*$/.test(name);

const text = (n: number | undefined) => (n === undefined ? "" : String(n));

/** The form showing an agent file's definition. */
export function formFromAgent(a: AgentDefinition): AgentForm {
  return {
    name: a.name,
    displayName: a.displayName,
    description: a.description,
    tools: [...a.tools],
    defaultModel: a.defaultModel,
    agencyLevel: a.agencyLevel,
    permissionMode: a.permissionMode,
    maxTurns: a.maxTurns ? String(a.maxTurns) : "",
    background: a.background,
    isolation: a.isolation === "worktree" ? "worktree" : "",
    temperature: text(a.temperature),
    topP: text(a.topP),
    maxTokens: text(a.maxTokens),
    effort: a.effort,
    thinkingBudget: text(a.thinkingBudget),
    prompt: a.prompt,
  };
}

// A number field: undefined when empty, NaN when it isn't a number.
const num = (s: string) => (s.trim() === "" ? undefined : Number(s.trim()));

/** The fields that are wrong, in the form's order (none: it can be saved). */
export function invalidFields(f: AgentForm): AgentField[] {
  const out: AgentField[] = [];
  if (!validAgentName(f.name.trim())) out.push("name");
  if (!f.description.trim()) out.push("description");
  if (checkModelRef(f.defaultModel, emptyCatalog).error) out.push("defaultModel");
  const check = (field: AgentField, ok: (n: number) => boolean) => {
    const n = num(f[field]);
    if (n !== undefined && (!Number.isFinite(n) || !ok(n))) out.push(field);
  };
  check("maxTurns", (n) => Number.isInteger(n) && n >= 0);
  check("temperature", (n) => n >= 0 && n <= 2);
  check("topP", (n) => n > 0 && n <= 1);
  check("maxTokens", (n) => Number.isInteger(n) && n >= 1);
  check("thinkingBudget", (n) => Number.isInteger(n) && n >= 0);
  return out;
}

/** The agent file's definition for the form (check it with invalidFields first). */
export function agentFromForm(f: AgentForm): AgentDefinition {
  const opt = (s: string) => {
    const n = num(s);
    return n !== undefined && Number.isFinite(n) ? n : undefined;
  };
  return create(AgentDefinitionSchema, {
    name: f.name.trim(),
    displayName: f.displayName.trim(),
    description: f.description.trim(),
    tools: [...new Set(f.tools.map((x) => x.trim()).filter(Boolean))],
    defaultModel: f.defaultModel.trim(),
    agencyLevel: f.agencyLevel,
    permissionMode: f.permissionMode,
    maxTurns: Math.max(0, Math.floor(opt(f.maxTurns) ?? 0)),
    background: f.background,
    isolation: f.isolation,
    temperature: opt(f.temperature),
    topP: opt(f.topP),
    maxTokens: opt(f.maxTokens),
    effort: f.effort,
    thinkingBudget: opt(f.thinkingBudget),
    prompt: f.prompt.trim(),
  });
}

/** Whether two forms say the same (so the editor knows when there are unsaved edits). */
export function sameForm(a: AgentForm, b: AgentForm): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

/**
 * How many of the Advanced section's settings the form sets: the model,
 * agency, mode, max turns, isolation, tools and the model settings. The
 * file name, always set, isn't counted.
 */
export function advancedCount(f: AgentForm): number {
  const set = [f.defaultModel, f.agencyLevel, f.permissionMode, f.maxTurns, f.isolation, f.effort, f.thinkingBudget, f.temperature, f.topP, f.maxTokens].filter((v) => v.trim() !== "").length;
  return set + (f.tools.length > 0 ? 1 : 0);
}

/** The tools to show boxes for: those offered and those chosen, sorted. */
export function toolChoices(offered: string[], chosen: string[]): string[] {
  return [...new Set([...offered, ...chosen])].sort();
}

/** chosen with the tools typed (separated by spaces or commas) added once each. */
export function addTools(chosen: string[], typed: string): string[] {
  const names = typed
    .split(/[\s,]+/)
    .map((x) => x.trim())
    .filter(Boolean);
  return [...new Set([...chosen, ...names])];
}

/** Where an agent named name is saved in the folder dir ("" without a folder). */
export function agentPath(dir: string, name: string): string {
  return dir ? `${dir.replace(/\/+$/, "")}/${name.trim() || "<name>"}.md` : "";
}

/** A file's name in a listing: its agent's name, or the file's own when it doesn't load. */
export function fileName(path: string, agentName?: string): string {
  return agentName || path.slice(path.lastIndexOf("/") + 1);
}

/** What DeleteAgentFile is asked to delete: the agent by name, or a file that doesn't load by its path. */
export function deleteTarget(path: string, agentName: string): { name: string } | { path: string } {
  return agentName ? { name: agentName } : { path };
}
