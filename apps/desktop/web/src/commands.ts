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

// Slash commands in the composer: the built-in ones the window runs
// through the service's API (as the terminal's REPL does), and the
// workspace's custom commands and skills, which run as turns.
import { t } from "./i18n";

export interface CommandSpec {
  name: string; // without the slash
  args?: string; // shown after the name
  description: string;
  /** "builtin" runs in the window; the others run as a command turn. */
  source: "builtin" | "project" | "user" | "bundled" | "skill";
}

const builtinArgs: Record<string, string | undefined> = {
  plan: "<goal>",
  btw: "<question>",
  search: "web|session <terms>",
  undo: "[--force]",
  checkpoints: undefined,
  diff: undefined,
  cost: undefined,
  context: undefined,
  compact: "[focus]",
  agent: "[name]",
  model: "[provider/model]",
  mode: "[default|accept-edits|plan|dont-ask|bypass]",
  effort: "[minimal|low|medium|high|max|auto]",
  session: "save <name> [--force]",
  rename: "<title>",
  new: undefined,
  help: undefined,
};

/** The built-in commands, described in the window's language. */
export const builtins = (): CommandSpec[] =>
  Object.entries(builtinArgs).map(([name, args]) => ({ name, args, description: t(`desktop.cmd.${name}`), source: "builtin" }));

/** A composer line as a command, or undefined when it isn't one. */
export function parseCommand(line: string): { name: string; args: string } | undefined {
  const t = line.trim();
  if (!t.startsWith("/") || t.startsWith("//")) return undefined;
  // Names are letters, digits, "-", "_", ":" and "."; "/usr/bin …" is a path.
  const m = /^\/([A-Za-z][\w:.-]*)(?:\s+([\s\S]*))?$/.exec(t);
  if (!m) return undefined;
  return { name: m[1].toLowerCase(), args: (m[2] ?? "").trim() };
}

/** The commands offered while the user types "/prefix" (no space yet): prefix matches first, then the rest containing it. */
export function matchCommands(draft: string, all: CommandSpec[]): CommandSpec[] {
  const m = /^\/(\S*)$/.exec(draft);
  if (!m) return [];
  const q = m[1].toLowerCase();
  const starts = all.filter((c) => c.name.startsWith(q));
  const contains = all.filter((c) => !c.name.startsWith(q) && (c.name.includes(q) || (q.length > 2 && c.description.toLowerCase().includes(q))));
  return [...starts, ...contains].slice(0, 12);
}

/** Every command: the built-in ones first, then the workspace's (a built-in name wins). */
export function allCommands(custom: CommandSpec[]): CommandSpec[] {
  const own = builtins();
  const names = new Set(own.map((b) => b.name));
  return [...own, ...custom.filter((c) => !names.has(c.name))];
}

/** /help's text, as Markdown. */
export function helpText(all: CommandSpec[]): string {
  // A "|" would end a table cell, also inside code: escape it everywhere.
  const cell = (s: string) => s.replace(/\|/g, "\\|");
  const row = (c: CommandSpec) => `| \`/${cell(c.name + (c.args ? " " + c.args : ""))}\` | ${cell(c.description)} |`;
  const custom = all.filter((c) => c.source !== "builtin");
  const head = `| ${t("desktop.help.command")} | ${t("desktop.help.does")} |\n|---|---|\n`;
  let s = `**${t("desktop.help.title")}**\n\n${head}` + all.filter((c) => c.source === "builtin").map(row).join("\n");
  if (custom.length) s += `\n\n**${t("desktop.help.custom")}**\n\n${head}` + custom.map(row).join("\n");
  s += `\n\n${t("desktop.help.palette")}`;
  return s;
}

/** An entry of the command palette. */
export interface PaletteItem {
  group: string;
  label: string;
  detail?: string;
  icon?: string;
  run: () => void;
}

/**
 * The palette's items for a query: every word of it must appear in the
 * label, detail or group (ignoring case). Items whose label starts with
 * the query come first, then those whose label has every word, then the
 * rest; group order is kept within each.
 */
export function filterPalette(items: PaletteItem[], query: string): PaletteItem[] {
  const q = query.trim().toLowerCase();
  if (!q) return items;
  const words = q.split(/\s+/);
  const hits = items.filter((it) => {
    const text = `${it.label} ${it.detail ?? ""} ${it.group}`.toLowerCase();
    return words.every((w) => text.includes(w));
  });
  const tier = (it: PaletteItem) => {
    const label = it.label.toLowerCase();
    if (label.startsWith(q) || label.startsWith("/" + q)) return 0;
    return words.every((w) => label.includes(w)) ? 1 : 2;
  };
  return hits.map((it, i) => ({ it, i, t: tier(it) })).sort((a, b) => a.t - b.t || a.i - b.i).map((x) => x.it);
}
