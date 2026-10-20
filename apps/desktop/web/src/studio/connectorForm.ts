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

// Studio's connector form (spec_connectors_047): a connection as the
// form holds it, from and to the API, drawn from its kind's descriptor. A
// password is typed into secretValues and saved as a Blitz secret; the
// form never holds a kept one.

import type { Connector, ConnectorKind, ConnectorQuery } from "../gen/blitz/v1/workspace_pb";

/** Where a connection is: this workspace's .agents/connectors, or every workspace's. */
export type ConnectorScope = "workspace" | "global";

/** A capability a kind of connector has (spec_connectors_047). */
export type Capability = "test" | "try" | "queries" | "schema" | "query-builder";

/** A query's parameter as the form holds it. */
export interface ParamForm {
  name: string;
  type: string;
  description: string;
}

/** A query as the form holds it. */
export interface QueryForm {
  name: string;
  description: string;
  statement: string;
  params: ParamForm[];
}

/** What the form holds. */
export interface ConnectorForm {
  name: string;
  category: string;
  scope: ConnectorScope;
  kind: string;
  description: string;
  /** The kind's settings, but the secret ones. */
  settings: Record<string, string>;
  /** Secret settings that have a value kept. */
  secretsSet: string[];
  /** Secret settings typed in, to keep. */
  secretValues: Record<string, string>;
  readOnly: boolean;
  tools: string[];
  queries: QueryForm[];
  /** More than the form describes: its text is edited. */
  custom: boolean;
  text: string;
}

/** The parameter types a query takes. */
export const paramTypes = ["string", "integer", "float", "boolean"];

/** A descriptor's text in lang (es, fr-CA, then fr), else English. */
export function textIn(text: Record<string, string> | undefined, lang: string): string {
  if (!text) return "";
  return text[lang] ?? text[lang.split("-")[0]] ?? text.en ?? "";
}

/** Whether kind has capability c. */
export function can(kind: ConnectorKind | undefined, c: Capability): boolean {
  return !!kind && kind.capabilities.includes(c);
}

/** A new connection's form for kind in category: read-only when it can be, its usual tools on. */
export function newConnectorForm(category: string, kind: ConnectorKind | undefined, scope: ConnectorScope = "workspace"): ConnectorForm {
  return {
    name: "",
    category,
    scope,
    kind: kind?.id ?? "",
    description: "",
    settings: {},
    secretsSet: [],
    secretValues: {},
    readOnly: kind?.canReadOnly ?? false,
    tools: (kind?.tools ?? []).filter((t) => t.on).map((t) => t.id),
    queries: [],
    custom: false,
    text: "",
  };
}

/** The form switched to another kind: the settings it shares kept, its usual tools on. */
export function withKind(f: ConnectorForm, kind: ConnectorKind): ConnectorForm {
  const keys = new Set(kind.fields.filter((x) => x.type !== "secret").map((x) => x.key));
  const settings = Object.fromEntries(Object.entries(f.settings).filter(([k]) => keys.has(k)));
  return { ...f, kind: kind.id, settings, secretValues: {}, readOnly: kind.canReadOnly && f.readOnly, tools: kind.tools.filter((t) => t.on).map((t) => t.id), queries: can(kind, "queries") ? f.queries : [] };
}

/** A query from the API as the form holds it. */
export function queryForm(q: ConnectorQuery): QueryForm {
  return { name: q.name, description: q.description, statement: q.statement, params: q.params.map((p) => ({ name: p.name, type: p.type, description: p.description })) };
}

/** A connection from the API as the form holds it. */
export function formFromConnection(c: Connector, kind: ConnectorKind | undefined): ConnectorForm {
  const secret = new Set(kind?.fields.filter((x) => x.type === "secret").map((x) => x.key));
  const settings = Object.fromEntries(Object.entries(c.settings).filter(([k]) => !secret.has(k)));
  return {
    name: c.name,
    category: c.category,
    scope: c.scope === "global" ? "global" : "workspace",
    kind: c.kind,
    description: c.description,
    settings,
    secretsSet: [...c.secretsSet],
    secretValues: {},
    readOnly: c.readOnly,
    tools: [...c.tools],
    queries: c.queries.map(queryForm),
    custom: c.custom,
    text: c.text,
  };
}

/** The form as the API takes it: the connection, and the secret values typed. */
export function connectionFromForm(f: ConnectorForm) {
  const settings = Object.fromEntries(
    Object.entries(f.settings)
      .map(([k, v]) => [k, v.trim()])
      .filter(([, v]) => v !== ""),
  );
  const secretValues = Object.fromEntries(Object.entries(f.secretValues).filter(([, v]) => v !== ""));
  return {
    connection: {
      name: f.name.trim(),
      category: f.category,
      scope: f.scope,
      kind: f.kind,
      description: f.description.trim(),
      settings,
      readOnly: f.readOnly,
      tools: f.tools,
      queries: f.queries.map((q) => ({ name: q.name.trim(), description: q.description.trim(), statement: q.statement.trim(), params: q.params.map((p) => ({ name: p.name.trim(), type: p.type, description: p.description.trim() })) })),
      custom: f.custom,
      text: f.custom ? f.text : "",
    },
    secretValues,
  };
}

/** Whether two forms hold the same connection. */
export function sameConnectorForm(a: ConnectorForm, b: ConnectorForm): boolean {
  return JSON.stringify(connectionFromForm(a)) === JSON.stringify(connectionFromForm(b));
}

/** The words that start a statement that reads. */
const readVerbs = new Set(["select", "with", "explain", "show", "pragma", "values", "describe", "table"]);
/** Words that may make a statement write (WITH … DELETE). */
const writeWords = /\b(insert|update|delete|merge|upsert|create|drop|alter|truncate|grant|revoke|copy|call|vacuum|attach|detach|reindex)\b/i;

/** Whether a SQL statement only reads, as the service judges it (a hint: a read-only connection is the guard). */
export function readsOnly(statement: string): boolean {
  const s = statement.replace(/--[^\n]*|\/\*[\s\S]*?\*\//g, " ");
  const first = s.trim().replace(/^\(+/, "").toLowerCase().split(/\s+/)[0]?.replace(/;+$/, "") ?? "";
  return readVerbs.has(first) && !writeWords.test(s);
}

/** The parameters a statement refers to ($1, $2… or @name), for a new query's first draft. */
export function statementParams(statement: string): string[] {
  const out: string[] = [];
  for (const m of statement.matchAll(/\$(\d+)|@([A-Za-z_]\w*)/g)) {
    const name = m[2] ?? `p${m[1]}`;
    if (!out.includes(name)) out.push(name);
  }
  return out;
}
