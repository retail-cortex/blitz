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

// Studio's workflow canvas (spec_workflows_043): a workflow as the canvas
// holds it, from and to the API, each step type's defaults, and the
// templates a new workflow can start from.

import type { WorkflowFile } from "../gen/blitz/v1/workspace_pb";

/** A step's type, in the order the palette shows them. */
export const stepTypes = ["agent", "refine", "summarize", "accumulate", "browser", "script", "read_file", "save_file", "email", "data"] as const;
/** A step's type. */
export type StepType = (typeof stepTypes)[number];
/** The types shown, not run yet. */
export const comingSoon: StepType[] = ["email", "data"];
/** The types an agent runs. */
export const agentTypes: StepType[] = ["agent", "refine", "summarize", "browser"];

/** A step as the canvas holds it. */
export interface StepForm {
  id: string;
  type: StepType;
  title: string;
  x: number;
  y: number;
  agent: string;
  skills: string[];
  instructions: string;
  rules: string[];
  skill: string;
  script: string;
  args: string;
  path: string;
}

/** A link from one step to another. */
export interface LinkForm {
  from: string;
  to: string;
}

/** What the canvas holds. */
export interface WorkflowForm {
  name: string;
  description: string;
  steps: StepForm[];
  links: LinkForm[];
}

/** A new, empty workflow. */
export const emptyWorkflowForm: WorkflowForm = { name: "", description: "", steps: [], links: [] };

const asType = (t: string): StepType => ((stepTypes as readonly string[]).includes(t) ? (t as StepType) : "agent");

/** The canvas for a workflow as read. */
export function formFromWorkflow(w: WorkflowFile): WorkflowForm {
  return {
    name: w.name,
    description: w.description,
    steps: w.nodes.map((n) => ({ id: n.id, type: asType(n.type), title: n.title, x: n.x, y: n.y, agent: n.agent, skills: [...n.skills], instructions: n.instructions, rules: [...n.rules], skill: n.skill, script: n.script, args: n.args.join(" "), path: n.path })),
    links: w.edges.map((e) => ({ from: e.from, to: e.to })),
  };
}

/** The canvas as SaveWorkflow takes it: each step with its type's settings only. */
export function specFromForm(f: WorkflowForm) {
  return {
    name: f.name.trim(),
    description: f.description.trim(),
    nodes: f.steps.map((s) => {
      const agent = agentTypes.includes(s.type);
      return {
        id: s.id,
        type: s.type,
        title: s.title.trim(),
        x: Math.round(s.x),
        y: Math.round(s.y),
        agent: agent ? s.agent : "",
        skills: agent ? s.skills : [],
        instructions: agent ? s.instructions : "",
        rules: agent ? s.rules : [],
        skill: s.type === "script" ? s.skill : "",
        script: s.type === "script" ? s.script : "",
        args: s.type === "script" ? s.args.split(/\s+/).filter(Boolean) : [],
        path: s.type === "read_file" || s.type === "save_file" ? s.path.trim() : "",
      };
    }),
    edges: f.links.filter((l) => f.steps.some((s) => s.id === l.from) && f.steps.some((s) => s.id === l.to)),
  };
}

/** Whether two canvases hold the same workflow. */
export function sameWorkflowForm(a: WorkflowForm, b: WorkflowForm): boolean {
  return JSON.stringify(specFromForm(a)) === JSON.stringify(specFromForm(b));
}

/** A new step of type at (x, y), with an ID no step has. */
export function newStep(type: StepType, steps: StepForm[], x: number, y: number): StepForm {
  const taken = new Set(steps.map((s) => s.id));
  let n = 1;
  while (taken.has(`${type.replace("_", "-")}-${n}`)) n++;
  return { id: `${type.replace("_", "-")}-${n}`, type, title: "", x, y, agent: "", skills: [], instructions: "", rules: [], skill: "", script: "", args: "", path: "" };
}

/** Removes a step and its links. */
export function removeStep(f: WorkflowForm, id: string): WorkflowForm {
  return { ...f, steps: f.steps.filter((s) => s.id !== id), links: f.links.filter((l) => l.from !== id && l.to !== id) };
}

/** Adds a link, unless it's there, joins a step to itself, or makes a loop. */
export function addLink(f: WorkflowForm, from: string, to: string): WorkflowForm {
  if (from === to || f.links.some((l) => l.from === from && l.to === to) || reaches(f.links, to, from)) return f;
  return { ...f, links: [...f.links, { from, to }] };
}

/** Whether a path of links leads from a to b. */
function reaches(links: LinkForm[], a: string, b: string): boolean {
  const seen = new Set<string>();
  const next = [a];
  while (next.length) {
    const id = next.pop()!;
    if (id === b) return true;
    if (seen.has(id)) continue;
    seen.add(id);
    next.push(...links.filter((l) => l.from === id).map((l) => l.to));
  }
  return false;
}

/** What a step still needs before it can run ("" when it's ready). */
export function stepNeeds(s: StepForm): string {
  if (comingSoon.includes(s.type)) return "coming_soon";
  if ((s.type === "agent" || s.type === "refine" || s.type === "browser") && !s.instructions.trim()) return "instructions";
  if (s.type === "script" && (!s.skill || !s.script)) return "script";
  if ((s.type === "read_file" || s.type === "save_file") && !s.path.trim()) return "path";
  return "";
}

const step = (id: string, type: StepType, x: number, y: number, extra: Partial<StepForm> = {}): StepForm => ({ ...newStep(type, [], x, y), id, ...extra });

/** The templates a new workflow can start from (titles and texts are the page's, translated). */
export const templates = ["blank", "brief", "refine", "report"] as const;
/** A template's name. */
export type Template = (typeof templates)[number];

/** A template's steps and links; text fills in its titles and instructions. */
export function fromTemplate(tpl: Template, text: (key: string) => string): WorkflowForm {
  switch (tpl) {
    case "brief":
      return {
        name: "research-brief",
        description: text("brief.description"),
        steps: [
          step("notes", "read_file", 0, 0, { title: text("brief.notes"), path: "notes.md" }),
          step("look-up", "browser", 0, 160, { title: text("brief.look_up"), instructions: text("brief.look_up_goal") }),
          step("gather", "accumulate", 280, 80, { title: text("brief.gather") }),
          step("draft", "agent", 560, 80, { title: text("brief.draft"), instructions: text("brief.draft_instructions") }),
          step("save", "save_file", 840, 80, { title: text("brief.save"), path: "briefs/brief.md" }),
        ],
        links: [
          { from: "notes", to: "gather" },
          { from: "look-up", to: "gather" },
          { from: "gather", to: "draft" },
          { from: "draft", to: "save" },
        ],
      };
    case "refine":
      return {
        name: "draft-and-refine",
        description: text("refine.description"),
        steps: [
          step("draft", "agent", 0, 80, { title: text("refine.draft"), instructions: text("refine.draft_instructions") }),
          step("tighten", "refine", 280, 80, { title: text("refine.tighten"), instructions: text("refine.tighten_instructions") }),
          step("save", "save_file", 560, 80, { title: text("refine.save"), path: "drafts/draft.md" }),
        ],
        links: [
          { from: "draft", to: "tighten" },
          { from: "tighten", to: "save" },
        ],
      };
    case "report":
      return {
        name: "script-report",
        description: text("report.description"),
        steps: [
          step("run", "script", 0, 80, { title: text("report.run") }),
          step("sum-up", "summarize", 280, 80, { title: text("report.sum_up"), instructions: text("report.sum_up_focus") }),
          step("save", "save_file", 560, 80, { title: text("report.save"), path: "reports/report.md" }),
        ],
        links: [
          { from: "run", to: "sum-up" },
          { from: "sum-up", to: "save" },
        ],
      };
    default:
      return { ...emptyWorkflowForm, steps: [step("agent-1", "agent", 0, 80)] };
  }
}
