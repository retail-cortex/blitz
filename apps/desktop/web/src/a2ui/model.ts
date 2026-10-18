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

// A2UI v0.9 (https://a2ui.org/specification/v0.9-a2ui/), the part a
// workflow's human task uses (spec_workflows_043 WF-18): a form's
// components (a flat list, one with id "root"), its data model (JSON
// Pointer paths), dynamic values (a literal, {path}, or {call} of a
// catalog function), the basic catalog's checks, and the context a
// button's action sends. No code is ever run from a form: functions are
// named from this file's catalog.

import { t } from "../i18n";

/** A component: its id, its type (component), and its properties. */
export type Component = { id: string; component: string } & Record<string, unknown>;

/** A form: its components, and its starting data. */
export interface Form {
  components: Component[];
  data?: Record<string, unknown>;
}

/** The components by id. */
export function byId(form: Form): Map<string, Component> {
  return new Map(form.components.filter((c) => c && typeof c.id === "string").map((c) => [c.id, c]));
}

/** The value at a JSON Pointer path ("/a/b/0"), or undefined. */
export function getPath(data: unknown, path: string): unknown {
  if (!path || path === "/") return data;
  let cur: unknown = data;
  for (const raw of path.replace(/^\//, "").split("/")) {
    const key = raw.replace(/~1/g, "/").replace(/~0/g, "~");
    if (cur == null || typeof cur !== "object") return undefined;
    cur = (cur as Record<string, unknown>)[key];
  }
  return cur;
}

/** data with value at path (new objects along the way; nothing changed in place). */
export function setPath(data: Record<string, unknown>, path: string, value: unknown): Record<string, unknown> {
  const keys = path
    .replace(/^\//, "")
    .split("/")
    .map((k) => k.replace(/~1/g, "/").replace(/~0/g, "~"));
  const put = (obj: unknown, i: number): unknown => {
    const base: Record<string, unknown> = obj && typeof obj === "object" && !Array.isArray(obj) ? { ...(obj as Record<string, unknown>) } : {};
    base[keys[i]] = i === keys.length - 1 ? value : put(base[keys[i]], i + 1);
    return base;
  };
  return put(data, 0) as Record<string, unknown>;
}

/** A path relative to a template's item ("name" in the item at /items/2 is /items/2/name). */
export function scoped(path: string, scope: string): string {
  return path.startsWith("/") || !scope ? path : `${scope}/${path}`;
}

/** The catalog's functions: validation, formatting and logic (by name; nothing else is called). */
const functions: Record<string, (args: Record<string, unknown>) => unknown> = {
  required: ({ value }) => !(value === undefined || value === null || value === "" || (Array.isArray(value) && value.length === 0)),
  regex: ({ value, pattern }) => typeof value === "string" && safeRegex(String(pattern))?.test(value) === true,
  length: ({ value, min, max }) => {
    const n = typeof value === "string" || Array.isArray(value) ? value.length : 0;
    return (min === undefined || n >= Number(min)) && (max === undefined || n <= Number(max));
  },
  numeric: ({ value, min, max }) => {
    const n = Number(value);
    return value !== "" && Number.isFinite(n) && (min === undefined || n >= Number(min)) && (max === undefined || n <= Number(max));
  },
  email: ({ value }) => typeof value === "string" && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value),
  and: ({ values }) => Array.isArray(values) && values.every(Boolean),
  or: ({ values }) => Array.isArray(values) && values.some(Boolean),
  not: ({ value }) => !value,
  formatString: ({ value, template }) => String(template ?? value ?? "").replace(/\$\{([^}]*)\}/g, (_, k: string) => String((value as Record<string, unknown>)?.[k] ?? "")),
  formatNumber: ({ value, decimals }) => (Number.isFinite(Number(value)) ? Number(value).toFixed(decimals === undefined ? 0 : Number(decimals)) : ""),
  pluralize: ({ count, singular, plural }) => (Number(count) === 1 ? String(singular ?? "") : String(plural ?? "")),
};

/** A pattern from a form, or null when it isn't one (or is too long to trust). */
function safeRegex(pattern: string): RegExp | null {
  if (pattern.length > 200) return null;
  try {
    return new RegExp(pattern);
  } catch {
    return null;
  }
}

/** A dynamic value: a literal, {path} in the data (scoped to a template item), or {call} of a catalog function. */
export function resolve(value: unknown, data: Record<string, unknown>, scope = ""): unknown {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const v = value as Record<string, unknown>;
    if (typeof v.path === "string") return getPath(data, scoped(v.path, scope));
    if (typeof v.call === "string") {
      const fn = functions[v.call];
      if (!fn) return undefined;
      const args: Record<string, unknown> = {};
      for (const [k, a] of Object.entries((v.args as Record<string, unknown>) ?? {})) args[k] = resolve(a, data, scope);
      return fn(args);
    }
  }
  return value;
}

/** A component's failing checks' messages (none: it passes). */
export function failing(c: Component, data: Record<string, unknown>, scope = ""): string[] {
  const checks = Array.isArray(c.checks) ? (c.checks as Record<string, unknown>[]) : [];
  const out: string[] = [];
  for (const check of checks) {
    if (resolve(check, data, scope) === false) out.push(String(check.message ?? t("desktop.a2ui.check")));
  }
  if (typeof c.validationRegexp === "string" && c.value && typeof c.value === "object") {
    const v = resolve(c.value, data, scope);
    if (typeof v === "string" && v !== "" && safeRegex(c.validationRegexp)?.test(v) === false) out.push(t("desktop.a2ui.check"));
  }
  return out;
}

/** Every failing check in the form, by component id (for a button's own checks, and the whole form's). */
export function formProblems(form: Form, data: Record<string, unknown>): Map<string, string[]> {
  const out = new Map<string, string[]>();
  for (const c of form.components) {
    const f = failing(c, data);
    if (f.length) out.set(c.id, f);
  }
  return out;
}

/** What a button's action sends: its event's name, and its context with every binding resolved. */
export function actionOf(c: Component, data: Record<string, unknown>, scope = ""): { name: string; context: Record<string, unknown> } | null {
  const action = c.action as Record<string, unknown> | undefined;
  const event = action?.event as Record<string, unknown> | undefined;
  if (!event || typeof event.name !== "string") return null;
  const context: Record<string, unknown> = {};
  for (const [k, v] of Object.entries((event.context as Record<string, unknown>) ?? {})) context[k] = resolve(v, data, scope);
  return { name: event.name, context };
}

/** What a human task's answer is: the action's name and its context's values. */
export const answerOf = (a: { name: string; context: Record<string, unknown> }) => ({ action: a.name, ...a.context });

/** The child ids of a container: a list, or a template over a list in the data ({path, componentId}), each with its item's scope. */
export function childrenOf(c: Component, data: Record<string, unknown>, scope = ""): { id: string; scope: string }[] {
  const ch = c.children;
  if (Array.isArray(ch)) return ch.filter((x): x is string => typeof x === "string").map((id) => ({ id, scope }));
  if (ch && typeof ch === "object") {
    const t = ch as { path?: string; componentId?: string };
    const base = scoped(String(t.path ?? ""), scope);
    const items = getPath(data, base);
    if (Array.isArray(items) && t.componentId) return items.map((_, i) => ({ id: t.componentId!, scope: `${base}/${i}` }));
  }
  return [];
}

/** Whether a URL may be shown (https, or an inline image). */
export const safeUrl = (u: unknown): u is string => typeof u === "string" && (/^https:\/\//i.test(u) || /^data:image\/(png|jpeg|gif|webp);base64,/i.test(u));

/** A form from a step's JSON, or why it isn't one. */
export function parseForm(text: string): { form?: Form; problem?: string } {
  if (!text.trim()) return { problem: "empty" };
  try {
    const v = JSON.parse(text) as Form;
    if (!v || !Array.isArray(v.components)) return { problem: "components" };
    if (!v.components.some((c) => c?.id === "root")) return { problem: "root" };
    return { form: v };
  } catch (e) {
    return { problem: String((e as Error).message) };
  }
}

/** The form a new human task starts with: a note, and approve or reject. */
export const starterForm = (): Form => ({
  components: [
    { id: "root", component: "Column", children: ["note", "buttons"] },
    { id: "note", component: "TextField", label: t("desktop.a2ui.starter_note"), value: { path: "/note" }, variant: "longText" },
    { id: "buttons", component: "Row", justify: "end", children: ["reject", "approve"] },
    { id: "reject_label", component: "Text", text: t("desktop.a2ui.starter_reject") },
    { id: "reject", component: "Button", child: "reject_label", action: { event: { name: "reject", context: { note: { path: "/note" } } } } },
    { id: "approve_label", component: "Text", text: t("desktop.a2ui.starter_approve") },
    { id: "approve", component: "Button", variant: "primary", child: "approve_label", action: { event: { name: "approve", context: { note: { path: "/note" } } } } },
  ],
  data: { note: "" },
});
