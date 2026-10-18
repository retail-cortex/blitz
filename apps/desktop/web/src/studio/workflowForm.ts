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
export const stepTypes = ["agent", "refine", "summarize", "browser", "extract", "accumulate", "script", "read_file", "save_file", "mcp_call", "a2a_call", "api_call", "exclusive", "inclusive", "parallel", "email", "data"] as const;
/** A step's type. */
export type StepType = (typeof stepTypes)[number];
/** The palette's groups, in order. */
export const stepGroups: { group: string; types: StepType[] }[] = [
  { group: "agents", types: ["agent", "refine", "summarize", "browser"] },
  { group: "data", types: ["extract", "accumulate", "script", "read_file", "save_file"] },
  { group: "calls", types: ["api_call", "mcp_call", "a2a_call"] },
  { group: "flow", types: ["exclusive", "inclusive", "parallel"] },
  { group: "soon", types: ["email", "data"] },
];
/** The gateways: they route rather than work. */
export const gatewayTypes: StepType[] = ["exclusive", "inclusive", "parallel"];
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
  fields: FieldForm[];
  retryAttempts: string;
  retryDelay: string;
  timeout: string;
  server: string;
  tool: string;
  argumentsJson: string;
  url: string;
  method: string;
  query: PairForm[];
  headers: PairForm[];
  bodyKind: "none" | "text" | "json";
  body: string;
  auth: AuthForm;
}

/** A header or a query parameter. */
export interface PairForm {
  name: string;
  value: string;
}

/** An API call's auth, as the form holds it (scopes space-separated). */
export interface AuthForm {
  type: "" | "bearer" | "basic" | "api_key" | "oauth2";
  token: string;
  username: string;
  password: string;
  name: string;
  value: string;
  in: "header" | "query";
  tokenUrl: string;
  clientId: string;
  clientSecret: string;
  scopes: string;
  audience: string;
}

/** No auth. */
export const noAuth: AuthForm = { type: "", token: "", username: "", password: "", name: "", value: "", in: "header", tokenUrl: "", clientId: "", clientSecret: "", scopes: "", audience: "" };

/** The HTTP methods an API call can use. */
export const methods = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"] as const;

/** A value an extract step pulls out. */
export interface FieldForm {
  name: string;
  type: "string" | "number" | "boolean" | "list";
  description: string;
}

/** How a link is taken: always, on a condition (CEL or plain English), by default, or when its step fails. */
export type LinkKind = "always" | "when" | "if" | "default" | "error";

/** A link from one step to another. */
export interface LinkForm {
  from: string;
  to: string;
  kind: LinkKind;
  condition: string;
  label: string;
}

/** A link's ID on the canvas. */
export const linkId = (l: { from: string; to: string }) => `${l.from}->${l.to}`;

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
    steps: w.nodes.map((n) => ({
      id: n.id,
      type: asType(n.type),
      title: n.title,
      x: n.x,
      y: n.y,
      agent: n.agent,
      skills: [...n.skills],
      instructions: n.instructions,
      rules: [...n.rules],
      skill: n.skill,
      script: n.script,
      args: n.args.join(" "),
      path: n.path,
      fields: n.fields.map((f) => ({ name: f.name, type: (["number", "boolean", "list"].includes(f.type) ? f.type : "string") as FieldForm["type"], description: f.description })),
      retryAttempts: n.retryAttempts > 1 ? String(n.retryAttempts) : "",
      retryDelay: n.retryDelay,
      timeout: n.timeout,
      server: n.server,
      tool: n.tool,
      argumentsJson: n.argumentsJson,
      url: n.url,
      method: n.method || "GET",
      query: n.query.map((p) => ({ name: p.name, value: p.value })),
      headers: n.headers.map((p) => ({ name: p.name, value: p.value })),
      bodyKind: (n.jsonBody ? "json" : n.body ? "text" : "none") as StepForm["bodyKind"],
      body: n.jsonBody || n.body,
      auth: n.auth?.type
        ? {
            type: n.auth.type as AuthForm["type"],
            token: n.auth.token,
            username: n.auth.username,
            password: n.auth.password,
            name: n.auth.name,
            value: n.auth.value,
            in: n.auth.in === "query" ? "query" : "header",
            tokenUrl: n.auth.tokenUrl,
            clientId: n.auth.clientId,
            clientSecret: n.auth.clientSecret,
            scopes: n.auth.scopes.join(" "),
            audience: n.auth.audience,
          }
        : { ...noAuth },
    })),
    links: w.edges.map((e) => ({ from: e.from, to: e.to, kind: (e.error ? "error" : e.default ? "default" : e.when ? "when" : e.if ? "if" : "always") as LinkKind, condition: e.when || e.if, label: e.label })),
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
        fields: s.type === "extract" ? s.fields.filter((x) => x.name.trim()).map((x) => ({ name: x.name.trim(), type: x.type === "string" ? "" : x.type, description: x.description.trim() })) : [],
        retryAttempts: gatewayTypes.includes(s.type) ? 0 : Math.max(0, Math.floor(Number(s.retryAttempts)) || 0),
        retryDelay: gatewayTypes.includes(s.type) || !s.retryAttempts ? "" : s.retryDelay.trim(),
        timeout: gatewayTypes.includes(s.type) ? "" : s.timeout.trim(),
        server: s.type === "mcp_call" ? s.server : "",
        tool: s.type === "mcp_call" ? s.tool.trim() : "",
        argumentsJson: s.type === "mcp_call" ? s.argumentsJson.trim() : "",
        url: s.type === "api_call" || s.type === "a2a_call" ? s.url.trim() : "",
        method: s.type === "api_call" && s.method !== "GET" ? s.method : "",
        query: s.type === "api_call" ? s.query.filter((p) => p.name.trim()) : [],
        headers: s.type === "api_call" || s.type === "a2a_call" ? s.headers.filter((p) => p.name.trim()) : [],
        body: s.type === "api_call" && s.bodyKind === "text" ? s.body : "",
        jsonBody: s.type === "api_call" && s.bodyKind === "json" ? s.body.trim() : "",
        auth: s.type === "api_call" && s.auth.type ? authSpec(s.auth) : undefined,
      };
    }),
    edges: f.links
      .filter((l) => f.steps.some((s) => s.id === l.from) && f.steps.some((s) => s.id === l.to))
      .map((l) => ({ from: l.from, to: l.to, when: l.kind === "when" ? l.condition.trim() : "", if: l.kind === "if" ? l.condition.trim() : "", default: l.kind === "default", error: l.kind === "error", label: l.label.trim() })),
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
  return {
    id: `${type.replace("_", "-")}-${n}`,
    type,
    title: "",
    x,
    y,
    agent: "",
    skills: [],
    instructions: "",
    rules: [],
    skill: "",
    script: "",
    args: "",
    path: "",
    fields: [],
    retryAttempts: "",
    retryDelay: "",
    timeout: "",
    server: "",
    tool: "",
    argumentsJson: "",
    url: "",
    method: "GET",
    query: [],
    headers: [],
    bodyKind: "none",
    body: "",
    auth: { ...noAuth },
  };
}

/** Removes a step and its links. */
export function removeStep(f: WorkflowForm, id: string): WorkflowForm {
  return { ...f, steps: f.steps.filter((s) => s.id !== id), links: f.links.filter((l) => l.from !== id && l.to !== id) };
}

/** Adds a link, unless it's there, joins a step to itself, or makes a loop. */
export function addLink(f: WorkflowForm, from: string, to: string): WorkflowForm {
  if (from === to || f.links.some((l) => l.from === from && l.to === to) || reaches(f.links, to, from)) return f;
  return { ...f, links: [...f.links, { from, to, kind: "always", condition: "", label: "" }] };
}

/** Changes the link from → to. */
export function setLink(f: WorkflowForm, id: string, patch: Partial<LinkForm>): WorkflowForm {
  return { ...f, links: f.links.map((l) => (linkId(l) === id ? { ...l, ...patch } : l)) };
}

/** What a link's line says: its label, else its condition, default or on error. */
export function linkText(l: LinkForm, text: (key: string) => string): string {
  if (l.label.trim()) return l.label.trim();
  if (l.kind === "default") return text("default");
  if (l.kind === "error") return text("error");
  if (l.kind === "when" || l.kind === "if") return l.condition.length > 28 ? `${l.condition.slice(0, 27)}…` : l.condition;
  return "";
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
  if (s.type === "extract" && !s.fields.some((x) => x.name.trim())) return "fields";
  if (s.type === "mcp_call" && (!s.server || !s.tool.trim())) return "mcp";
  if ((s.type === "api_call" || s.type === "a2a_call") && !s.url.trim()) return "url";
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
          { from: "notes", to: "gather", kind: "always", condition: "", label: "" },
          { from: "look-up", to: "gather", kind: "always", condition: "", label: "" },
          { from: "gather", to: "draft", kind: "always", condition: "", label: "" },
          { from: "draft", to: "save", kind: "always", condition: "", label: "" },
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
          { from: "draft", to: "tighten", kind: "always", condition: "", label: "" },
          { from: "tighten", to: "save", kind: "always", condition: "", label: "" },
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
          { from: "run", to: "sum-up", kind: "always", condition: "", label: "" },
          { from: "sum-up", to: "save", kind: "always", condition: "", label: "" },
        ],
      };
    default:
      return { ...emptyWorkflowForm, steps: [step("agent-1", "agent", 0, 80)] };
  }
}

/** An auth as SaveWorkflow takes it: its type's values only. */
function authSpec(a: AuthForm) {
  const blank = { token: "", username: "", password: "", name: "", value: "", in: "", tokenUrl: "", clientId: "", clientSecret: "", scopes: [] as string[], audience: "" };
  switch (a.type) {
    case "bearer":
      return { ...blank, type: a.type, token: a.token };
    case "basic":
      return { ...blank, type: a.type, username: a.username, password: a.password };
    case "api_key":
      return { ...blank, type: a.type, name: a.name.trim(), value: a.value, in: a.in === "query" ? "query" : "" };
    default:
      return { ...blank, type: a.type, tokenUrl: a.tokenUrl.trim(), clientId: a.clientId, clientSecret: a.clientSecret, scopes: a.scopes.split(/\s+/).filter(Boolean), audience: a.audience.trim() };
  }
}
