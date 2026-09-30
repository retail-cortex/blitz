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

// Reading a settings file's keys as the settings reference names them.

/** A setting of the reference (GetSettingsReference). */
export interface SettingRef {
  key: string;
  type: string;
  default: string;
  doc: string;
}

/** The dotted key a line names, and where the name is on the line. */
export interface KeySpan {
  /** The key's parts: model_settings."gpt-5.1".top_p has three. */
  path: string[];
  /** Offsets in the line, from 0; end is exclusive. */
  from: number;
  to: number;
}

/** Splits a dotted TOML key, unquoting quoted parts. */
export function splitKey(s: string): string[] {
  const out: string[] = [];
  const re = /\s*(?:"((?:[^"\\]|\\.)*)"|'([^']*)'|([^.\s"']+))\s*(?:\.|$)/y;
  let m: RegExpExecArray | null;
  while (re.lastIndex < s.length && (m = re.exec(s))) out.push(m[1] ?? m[2] ?? m[3]);
  return out;
}

const header = /^(\s*)(\[\[?)\s*([^\]]*?)\s*\]\]?/;
const assignment = /^(\s*)((?:"(?:[^"\\]|\\.)*"|'[^']*'|[^=#"'])+?)\s*=/;

/**
 * The key the given line (from 1) of text names: a table's header names
 * the table; an assignment names its table and key. Null for comments,
 * blank lines and the lines of a multi-line value.
 */
export function keyAt(text: string, line: number): KeySpan | null {
  const lines = text.split("\n");
  let table: string[] = [];
  let open = 0; // brackets of a multi-line array still open
  for (let i = 0; i < Math.min(line, lines.length); i++) {
    const l = lines[i];
    const here = i === line - 1;
    if (open > 0) {
      open += depth(l);
      if (here) return null;
      continue;
    }
    const h = header.exec(l);
    if (h) {
      table = splitKey(h[3]);
      if (here) {
        const from = h[1].length + h[2].length;
        return { path: table, from, to: from + h[0].length - h[1].length - 2 * h[2].length };
      }
      continue;
    }
    const a = assignment.exec(l);
    if (!a) {
      if (here) return null;
      continue;
    }
    open = Math.max(0, depth(l.slice(a[0].length)));
    if (here) {
      const from = a[1].length;
      return { path: [...table, ...splitKey(a[2])], from, to: from + a[2].trimEnd().length };
    }
  }
  return null;
}

// How many more brackets open than close in a value, ignoring strings and
// comments.
function depth(value: string): number {
  let n = 0;
  const bare = value.replace(/"(?:[^"\\]|\\.)*"|'[^']*'/g, "").replace(/#.*/, "");
  for (const c of bare) n += c === "[" ? 1 : c === "]" ? -1 : 0;
  return n;
}

/**
 * The reference's entry for a key: model_settings."gpt-5".top_p is
 * model_settings.<model>.top_p, a key in [[hooks.pre_tool]] is under
 * hooks.pre_tool[].
 */
export function findSetting(ref: SettingRef[], parts: string[]): SettingRef | undefined {
  return ref.find((s) => {
    const want = s.key.replace(/\[\]/g, "").split(".");
    return want.length === parts.length && want.every((w, i) => (w.startsWith("<") && w.endsWith(">")) || w === parts[i]);
  });
}

/** The reference's settings matching a search, by key or description. */
export function searchSettings(ref: SettingRef[], query: string): SettingRef[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return ref;
  return ref.filter((s) => {
    const hay = `${s.key} ${s.doc}`.toLowerCase();
    return words.every((w) => hay.includes(w));
  });
}

/** A completion the settings file offers at the cursor. */
export interface SettingCompletion {
  /** What's shown and, with apply, what's inserted. */
  label: string;
  apply: string;
  kind: "table" | "key" | "value";
  /** The setting it names (for its type, default and description). */
  setting?: SettingRef;
}

/** What to complete, and from where in the line (an offset from 0). */
export interface SettingCompletions {
  from: number;
  options: SettingCompletion[];
}

// The parts of a reference key, without the marks of arrays of tables.
const refParts = (key: string) => key.replace(/\[\]/g, "").split(".");
// A reference part matches a key's part: equal, or a <name> placeholder.
const partMatches = (want: string, part: string) => (want.startsWith("<") && want.endsWith(">")) || want === part;
const isTable = (s: SettingRef) => s.type === "table" || s.type === "array of tables";
// A key part as TOML writes it: bare when it can be, else quoted.
const tomlPart = (p: string) => (/^[A-Za-z0-9_-]+$/.test(p) ? p : JSON.stringify(p));

/** The table the given line (from 1) is in: the last header above it. */
export function tableAt(text: string, line: number): string[] {
  const lines = text.split("\n");
  for (let i = Math.min(line, lines.length) - 1; i >= 0; i--) {
    const h = header.exec(lines[i]);
    if (h && i < line - 1) return splitKey(h[3]);
  }
  return [];
}

/**
 * What the settings file can complete where the cursor is, given the text
 * before it on its line (before) and its line number (from 1):
 *
 * - in a table's header, the tables, and the named ones' names typed so far;
 * - at the start of a line, the current table's settings, not yet set;
 * - after `key =`, the setting's values: true and false, or its default.
 *
 * Null when there's nothing to offer (a comment, a string being typed).
 */
export function settingsCompletions(text: string, line: number, before: string, ref: SettingRef[]): SettingCompletions | null {
  if (before.includes("#")) return null;
  const inHeader = /^(\s*\[\[?\s*)([^\]]*)$/.exec(before);
  if (inHeader) {
    const typed = inHeader[2];
    const tables = ref.filter((s) => isTable(s) && !s.key.includes("<"));
    const options = tables.map((s) => {
      const name = s.key.replace(/\[\]$/, "");
      const array = s.type === "array of tables";
      return { label: name, apply: name, kind: "table" as const, setting: s, array };
    });
    // Only the kind of header being typed: [x] for tables, [[x]] for arrays.
    const double = before.trimStart().startsWith("[[");
    return {
      from: inHeader[1].length,
      options: options.filter((o) => o.array === double && o.label.startsWith(typed.trim())).map(({ array: _, ...o }) => o),
    };
  }
  const value = /^(\s*)((?:"(?:[^"\\]|\\.)*"|'[^']*'|[^=#"'])+?)\s*=\s*("?)([\w.-]*)$/.exec(before);
  if (value) {
    const table = tableAt(text, line);
    const s = findSetting(ref, [...table, ...splitKey(value[2])]);
    if (!s || isTable(s)) return null;
    const quote = value[3];
    const from = before.length - value[4].length - quote.length;
    let values: string[] = [];
    if (s.type === "boolean") values = ["true", "false"];
    else if (s.default) values = [s.default];
    const options = values.filter((v) => v.replace(/^"/, "").startsWith(value[4]) && v !== quote + value[4]).map((v) => ({ label: v, apply: v, kind: "value" as const, setting: s }));
    return options.length ? { from, options } : null;
  }
  const key = /^(\s*)([A-Za-z0-9_-]*)$/.exec(before);
  if (!key) return null;
  const table = tableAt(text, line);
  const set = new Set(setKeys(text, table));
  const options = ref
    .filter((s) => !isTable(s))
    .filter((s) => {
      const parts = refParts(s.key);
      return parts.length === table.length + 1 && table.every((p, i) => partMatches(parts[i], p));
    })
    .map((s) => refParts(s.key).at(-1)!)
    .filter((name, i, all) => !name.startsWith("<") && all.indexOf(name) === i && !set.has(name) && name.startsWith(key[2]))
    .map((name) => ({ label: name, apply: `${tomlPart(name)} = `, kind: "key" as const, setting: findSetting(ref, [...table, name]) }));
  return { from: key[1].length, options };
}

// The keys already set in a table (its own lines, not its subtables').
function setKeys(text: string, table: string[]): string[] {
  const out: string[] = [];
  const lines = text.split("\n");
  let current: string[] = [];
  for (const l of lines) {
    const h = header.exec(l);
    if (h) {
      current = splitKey(h[3]);
      continue;
    }
    const a = assignment.exec(l);
    if (a && current.length === table.length && current.every((p, i) => p === table[i])) out.push(...splitKey(a[2]).slice(0, 1));
  }
  return out;
}
