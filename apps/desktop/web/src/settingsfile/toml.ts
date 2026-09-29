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
