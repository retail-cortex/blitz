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
  /** Tells the form's queries apart, so a card keeps its own Try it state; never sent. */
  key: string;
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
  // A kind picked for a new form (none before) starts read-only, as newConnectorForm does.
  const readOnly = kind.canReadOnly && (f.kind === "" || f.readOnly);
  return { ...f, kind: kind.id, settings, secretValues: {}, readOnly, tools: kind.tools.filter((t) => t.on).map((t) => t.id), queries: can(kind, "queries") ? f.queries : [] };
}

let lastQueryKey = 0;

/** A new key for a query in the form (QueryForm.key). */
export function queryKey(): string {
  return `q${++lastQueryKey}`;
}

/** An empty query, as Add a query makes it. */
export function newQueryForm(): QueryForm {
  return { key: queryKey(), name: "", description: "", statement: "", params: [] };
}

/** A query from the API as the form holds it. */
export function queryForm(q: ConnectorQuery): QueryForm {
  return { key: queryKey(), name: q.name, description: q.description, statement: q.statement, params: q.params.map((p) => ({ name: p.name, type: p.type, description: p.description })) };
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

/**
 * A connection's name as the service takes it (lower case, digits and
 * _, starting with a letter, at most 40), from whatever was typed: other
 * characters become _, and runs of them one.
 */
export function slugName(typed: string): string {
  return typed
    .toLowerCase()
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/[^a-z0-9_]+/g, "_")
    .replace(/_+/g, "_")
    .replace(/^[_0-9]+/, "")
    .slice(0, 40)
    .replace(/_+$/, "");
}

/**
 * A name as it's being typed: as slugName, but a _ typed last stays (the
 * next character may follow it).
 */
export function typedName(typed: string): string {
  const slug = slugName(typed);
  return slug && /[^a-z0-9]$/i.test(typed) && slug.length < 40 ? `${slug}_` : slug;
}

/**
 * The name a new connection gets until one is typed: its database's (a
 * file's without its folders and extension), else its project's.
 */
export function suggestName(settings: Record<string, string>): string {
  const from = settings.database || settings.project || "";
  const base = from.split(/[\\/]/).pop() ?? "";
  return slugName(base.replace(/\.(sqlite3?|db3?|duckdb)$/i, ""));
}

/**
 * The form with a setting changed; a name that was still the suggested
 * one (or none) follows the new suggestion.
 */
export function withSetting(f: ConnectorForm, key: string, value: string): ConnectorForm {
  const settings = { ...f.settings, [key]: value };
  const named = f.name !== "" && f.name !== suggestName(f.settings);
  return { ...f, settings, name: named ? f.name : suggestName(settings) };
}

/** A connection URL's parts, as a kind's fields name them. */
export interface URLParts {
  settings: Record<string, string>;
  secrets: Record<string, string>;
}

/**
 * The fields a pasted connection URL (postgres://user:pass@host:5432/db)
 * fills, those the kind has; undefined when it isn't one.
 */
export function parseConnectionURL(text: string, kind: ConnectorKind): URLParts | undefined {
  const m = /^\s*([a-z][a-z0-9+.-]*):\/\/(\S+)\s*$/i.exec(text);
  if (!m) return undefined;
  let u: URL;
  try {
    // Parsed as http: the browser reads hosts and ports only for the schemes it knows.
    u = new URL(`http://${m[2]}`);
  } catch {
    return undefined;
  }
  const has = new Set(kind.fields.map((f) => f.key));
  const secret = new Set(kind.fields.filter((f) => f.type === "secret").map((f) => f.key));
  const found: Record<string, string> = {
    host: u.hostname,
    port: u.port,
    database: decodeURIComponent(u.pathname.replace(/^\//, "")),
    user: decodeURIComponent(u.username),
    password: decodeURIComponent(u.password),
  };
  const out: URLParts = { settings: {}, secrets: {} };
  for (const [k, v] of Object.entries(found)) {
    if (!v || !has.has(k)) continue;
    if (secret.has(k)) out.secrets[k] = v;
    else out.settings[k] = v;
  }
  return Object.keys(out.settings).length ? out : undefined;
}

/** Whether a kind's fields take a host (a server the URL names). */
export const takesURL = (kind: ConnectorKind | undefined) => !!kind?.fields.some((f) => f.key === "host");

/**
 * The kind a connection URL or a file is for, by its scheme or its
 * extension, among the category's kinds ("" when it can't be told).
 */
export function kindForTarget(target: string, kinds: ConnectorKind[]): string {
  const t = target.trim().toLowerCase();
  const ids = new Set(kinds.map((k) => k.id));
  const pick = (id: string) => (ids.has(id) ? id : "");
  if (/^postgres(ql)?:\/\//.test(t)) return pick("postgres");
  if (/^sqlite:/.test(t) || /\.(sqlite3?|db3?)$/.test(t)) return pick("sqlite");
  if (/^bigquery:\/\//.test(t)) return pick("bigquery");
  return "";
}

/**
 * A new connection's form for a URL or a file (/connect): its kind told
 * from it, the URL's parts or the file's path filled in, and its name
 * suggested. Undefined when the kind can't be told.
 */
export function formForTarget(target: string, category: string, kinds: ConnectorKind[], scope: ConnectorScope): ConnectorForm | undefined {
  const kind = kinds.find((k) => k.id === kindForTarget(target, kinds));
  if (!kind) return undefined;
  let f = newConnectorForm(category, kind, scope);
  if (takesURL(kind)) {
    const parts = parseConnectionURL(target, kind);
    if (parts) f = { ...f, settings: parts.settings, secretValues: parts.secrets };
  } else if (kind.fields.some((x) => x.key === "database")) {
    f = { ...f, settings: { database: target.trim().replace(/^sqlite:(\/\/)?/i, "") } };
  }
  return { ...f, name: suggestName(f.settings) };
}

/**
 * Where a path field's file dialog opens: the folder of the path it holds
 * (a relative one from base), else base (a workspace; "" the system's
 * choice).
 */
export function startFolder(base: string, value: string): string {
  const v = value.trim();
  if (!v) return base;
  const abs = v.startsWith("/") || v.startsWith("~") || /^[A-Za-z]:[\\/]/.test(v) ? v : base ? `${base.replace(/\/+$/, "")}/${v.replace(/^\.\//, "")}` : "";
  const i = Math.max(abs.lastIndexOf("/"), abs.lastIndexOf("\\"));
  return i > 0 ? abs.slice(0, i) : base;
}

/**
 * A chosen file as a path field holds it: relative to base ("./data/x.db")
 * when it's inside it, so the workspace's connection still works when the
 * project is cloned or moved; else as chosen.
 */
export function relativeTo(base: string, path: string): string {
  const b = base.replace(/[\\/]+$/, "");
  if (!b || !path.startsWith(b)) return path;
  const rest = path.slice(b.length);
  return /^[\\/]./.test(rest) ? `./${rest.slice(1).replace(/\\/g, "/")}` : path;
}
