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
// workflow's human task uses (spec_043_workflows WF-18): a form's
// components (a flat list, one with id "root"), its data model (JSON
// Pointer paths), dynamic values (a literal, {path}, or {call} of a
// catalog function), the basic catalog's checks, and the context a
// button's action sends. No code is ever run from a form: functions are
// named from this file's catalog.
//
// The service (pkg/a2ui) judges an answer; this copy shows its problems
// before a button is pressed, so it must agree with it. Both test suites
// read pkg/a2ui/testdata/checks.json, which holds the cases they share.

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
    if (Array.isArray(cur)) {
      // An index, as the service reads one (not "length", "-1" or "01x").
      const i = /^[+-]?\d+$/.test(key) ? Number(key) : -1;
      if (i < 0 || i >= cur.length) return undefined;
      cur = cur[i];
      continue;
    }
    cur = Object.hasOwn(cur, key) ? (cur as Record<string, unknown>)[key] : undefined;
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
  regex: ({ value, pattern }) => typeof value === "string" && typeof pattern === "string" && safeRegex(pattern)?.test(value) === true,
  length: (args) => {
    const { value } = args;
    // Code points, as the service counts them (an emoji is one).
    const n = typeof value === "string" ? [...value].length : Array.isArray(value) ? value.length : 0;
    return within(n, args);
  },
  numeric: (args) => {
    const n = numberOf(args.value);
    return n !== null && within(n, args);
  },
  email: ({ value }) => typeof value === "string" && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value),
  and: ({ values }) => Array.isArray(values) && values.every(truthy),
  or: ({ values }) => Array.isArray(values) && values.some(truthy),
  not: ({ value }) => !truthy(value),
  formatString: ({ value, template }) => {
    const vals = value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
    return (typeof template === "string" ? template : String(value ?? "")).replace(/\$\{([^}]*)\}/g, (_, k: string) => (vals[k] == null ? "" : String(vals[k])));
  },
  formatNumber: ({ value, decimals }) => {
    const n = numberOf(value);
    return n === null ? "" : n.toFixed(Math.min(100, Math.max(0, Math.trunc(numberOf(decimals) ?? 0))));
  },
  pluralize: ({ count, singular, plural }) => (numberOf(count) === 1 ? (typeof singular === "string" ? singular : "") : typeof plural === "string" ? plural : ""),
};

/** Whether n is within the arguments' min and max (one that isn't a number is no bound), as the service checks. */
function within(n: number, { min, max }: Record<string, unknown>): boolean {
  const lo = numberOf(min);
  const hi = numberOf(max);
  return (lo === null || n >= lo) && (hi === null || n <= hi);
}

/** A value as the service's logic reads it: nothing, false, "" and 0 are false; anything else true. */
function truthy(v: unknown): boolean {
  if (v === undefined || v === null) return false;
  if (typeof v === "boolean") return v;
  if (typeof v === "string") return v !== "";
  if (typeof v === "number") return v !== 0;
  return true;
}

/** A number, or a decimal number written as a string (spaces around it allowed), as the service reads one; null otherwise. */
function numberOf(value: unknown): number | null {
  if (typeof value === "number") return Number.isFinite(value) ? value : null;
  if (typeof value !== "string" || !/^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/.test(value.trim())) return null;
  const n = Number(value.trim());
  return Number.isFinite(n) ? n : null;
}

/** Whether a dynamic value calls, anywhere in it, a function outside the catalog. */
function callsUnknown(value: unknown): boolean {
  if (Array.isArray(value)) return value.some(callsUnknown);
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  if (typeof v.call === "string" && !Object.hasOwn(functions, v.call)) return true;
  return Object.values(v).some(callsUnknown);
}

/**
 * A form's RE2 pattern (the service's syntax) as JavaScript's, or null
 * when RE2 would refuse it: lookahead and lookbehind, and backreferences
 * ("\1", "\k<name>"), which only JavaScript has. RE2's \A and \z become
 * ^ and $, its \pL \p{L}, and its leading flags ("(?i)", "(?s)", "(?m)")
 * the RegExp's.
 */
export function re2ToJs(pattern: string): { source: string; flags: string } | null {
  const lead = /^\(\?([ims]+)\)/.exec(pattern);
  const flags = lead ? [...new Set(lead[1])].join("") : "";
  let out = "";
  let inClass = false;
  for (let i = lead ? lead[0].length : 0; i < pattern.length; i++) {
    const c = pattern[i];
    if (c === "\\") {
      const n = pattern[i + 1] ?? "";
      if (/[1-9k]/.test(n)) return null;
      i++;
      if (!inClass && n === "A") out += "^";
      else if (!inClass && n === "z") out += "$";
      else if (n === "p" || n === "P") {
        if (pattern[i + 1] === "{") out += `\\${n}`;
        else {
          out += `\\${n}{${pattern[i + 1] ?? ""}}`;
          i++;
        }
      } else out += c + n;
      continue;
    }
    if (inClass) inClass = c !== "]";
    else if (c === "[") {
      inClass = true;
      // A ] first in a class is a character, in both.
      if (pattern[i + 1] === "^" && pattern[i + 2] === "]") {
        out += "[^\\]";
        i += 2;
        continue;
      }
      if (pattern[i + 1] === "]") {
        out += "[\\]";
        i++;
        continue;
      }
    } else if (c === "(" && /^\(\?<?[=!]/.test(pattern.slice(i, i + 4))) return null;
    out += c;
  }
  return { source: out, flags };
}

/**
 * A pattern from a form, or null when it isn't one (or is too long to
 * trust), read as close to RE2 as JavaScript allows (re2ToJs), matching
 * code points (a "." is one emoji). What can't be matched exactly: \s,
 * which in JavaScript also takes Unicode spaces (RE2's is [\t\n\f\r ]);
 * "." without (?s), which in JavaScript also stops at \r,   and
 *  ; POSIX classes ("[[:alpha:]]"), which JavaScript doesn't know;
 * and case folding, which differs at the edges. Forms keep to ASCII
 * classes to be judged alike.
 */
function safeRegex(pattern: string): RegExp | null {
  const re = new TextEncoder().encode(pattern).length > 200 ? null : re2ToJs(pattern);
  if (!re) return null;
  for (const f of [re.flags + "u", re.flags]) {
    try {
      return new RegExp(re.source, f);
    } catch {
      // Without u: an escape JavaScript's Unicode mode refuses ("\-", "\_"), which RE2 takes.
    }
  }
  return null;
}

/** A dynamic value: a literal, {path} in the data (scoped to a template item), or {call} of a catalog function. */
export function resolve(value: unknown, data: Record<string, unknown>, scope = ""): unknown {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const v = value as Record<string, unknown>;
    if (typeof v.path === "string") return getPath(data, scoped(v.path, scope));
    if (typeof v.call === "string") {
      const fn = Object.hasOwn(functions, v.call) ? functions[v.call] : undefined;
      if (!fn) return undefined;
      const args: Record<string, unknown> = {};
      for (const [k, a] of Object.entries((v.args as Record<string, unknown>) ?? {})) args[k] = resolve(a, data, scope);
      return fn(args);
    }
  }
  return value;
}

/**
 * A component's failing checks' messages (none: it passes). As in the service, a check calling a function outside
 * the catalog fails, and so does a validationRegexp that isn't a usable pattern.
 */
export function failing(c: Component, data: Record<string, unknown>, scope = ""): string[] {
  const checks = Array.isArray(c.checks) ? (c.checks as Record<string, unknown>[]) : [];
  const out: string[] = [];
  for (const check of checks) {
    if (callsUnknown(check) || resolve(check, data, scope) === false) out.push(typeof check?.message === "string" && check.message !== "" ? check.message : t("desktop.a2ui.check"));
  }
  if (typeof c.validationRegexp === "string") {
    const re = safeRegex(c.validationRegexp);
    const v = resolve(c.value, data, scope);
    if (!re || (typeof v === "string" && v !== "" && !re.test(v))) out.push(t("desktop.a2ui.check"));
  }
  return out;
}

/**
 * Every failing check of the components shown, by component id, as the
 * service's Form.Problems finds them: walked from root, through every
 * child (templates over the data's lists included, each item's checks on
 * its own paths), in the order they're drawn. A component the form never
 * shows isn't checked.
 */
export function formProblems(form: Form, data: Record<string, unknown>): Map<string, string[]> {
  const all = byId(form);
  const out = new Map<string, string[]>();
  const walk = (id: string, scope: string, depth: number) => {
    const c = all.get(id);
    if (!c || depth > 24) return;
    const f = failing(c, data, scope);
    if (f.length) out.set(id, [...(out.get(id) ?? []), ...f]);
    for (const k of shownChildren(c, data, scope)) walk(k.id, k.scope, depth + 1);
  };
  walk("root", "", 0);
  return out;
}

/** Every component a component shows, as the service walks them: its child, content and tabs too, besides its children. */
function shownChildren(c: Component, data: Record<string, unknown>, scope: string): { id: string; scope: string }[] {
  const out: { id: string; scope: string }[] = [];
  for (const key of ["child", "content", "entryPointChild", "contentChild"]) if (typeof c[key] === "string") out.push({ id: c[key] as string, scope });
  if (Array.isArray(c.tabs)) for (const tab of c.tabs as { child?: unknown }[]) if (typeof tab?.child === "string") out.push({ id: tab.child, scope });
  return [...out, ...childrenOf(c, data, scope)];
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

/** A button pressed: its action's name and resolved context, the button, and the surface's data model then. */
export interface FormAction {
  name: string;
  context: Record<string, unknown>;
  sourceComponentId: string;
  data: Record<string, unknown>;
}

/** A2UI's client-to-server action message for a surface: what the service checks and a run gets. */
export const actionMessage = (surfaceId: string, a: FormAction, now = new Date()) => ({
  version: "v0.9",
  action: { name: a.name, surfaceId, sourceComponentId: a.sourceComponentId, timestamp: now.toISOString(), context: a.context },
});

/**
 * A surface's form from A2UI's server-to-client messages (a JSON array:
 * createSurface, updateComponents, updateDataModel; a path's update is
 * applied), with its surface's ID, or why it isn't one.
 */
export function parseMessages(text: string): { form?: Form; surface?: string; problem?: FormProblem } {
  let list: unknown;
  try {
    list = JSON.parse(text);
  } catch {
    return { problem: "json" };
  }
  if (!Array.isArray(list)) return { problem: "messages" };
  let surface = "";
  const form: Form = { components: [], data: {} };
  for (const m of list as Record<string, Record<string, unknown> | undefined>[]) {
    if (m?.createSurface) surface = String(m.createSurface.surfaceId ?? "");
    if (m?.updateComponents && Array.isArray(m.updateComponents.components)) form.components = m.updateComponents.components as Component[];
    if (m?.updateDataModel) {
      const path = String(m.updateDataModel.path ?? "/");
      const value = m.updateDataModel.value;
      form.data = path === "/" ? ((value as Record<string, unknown>) ?? {}) : setPath(form.data ?? {}, path, value);
    }
  }
  if (!surface) return { problem: "createSurface" };
  if (!form.components.some((c) => c?.id === "root")) return { problem: "root" };
  return { form, surface };
}

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

/** Why a text isn't a form: not JSON, no messages list, no surface, no root, empty, or no components. */
export type FormProblem = "json" | "messages" | "createSurface" | "root" | "empty" | "components";

/** A form from a step's JSON, or why it isn't one. */
export function parseForm(text: string): { form?: Form; problem?: FormProblem } {
  if (!text.trim()) return { problem: "empty" };
  try {
    const v = JSON.parse(text) as Form;
    if (!v || !Array.isArray(v.components)) return { problem: "components" };
    if (!v.components.some((c) => c?.id === "root")) return { problem: "root" };
    return { form: v };
  } catch {
    return { problem: "json" };
  }
}

/** Why a form isn't one, from parseForm's or parseMessages' problem, in words. */
export function problemText(problem: string): string {
  return t(`desktop.a2ui.problem.${problem}`);
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
