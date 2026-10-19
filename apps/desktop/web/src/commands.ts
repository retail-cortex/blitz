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
import { builtinsInOrder, commandGroups, terminalOnly, type CommandGroup } from "./slashCommands";

/** A slash command as the composer offers it. */
export interface CommandSpec {
  name: string; // without the slash
  args?: string; // shown after the name
  description: string;
  /** "builtin" runs in the window; the others run as a command turn. */
  source: "builtin" | "project" | "user" | "bundled" | "skill";
  /** A built-in's group in /help (slashCommands.ts). */
  group?: CommandGroup;
}

/** The built-in commands, by group then name, described in the window's language. */
export const builtins = (): CommandSpec[] =>
  builtinsInOrder().map((b) => ({ name: b.name, args: b.args, description: t(b.describe ?? `desktop.cmd.${b.name}`), source: "builtin", group: b.group }));

/** A composer line as a command, or undefined when it isn't one. */
export function parseCommand(line: string): { name: string; args: string } | undefined {
  const t = line.trim();
  if (!t.startsWith("/") || t.startsWith("//")) return undefined;
  // Names are letters, digits, "-", "_", ":" and "."; "/usr/bin …" is a path.
  const m = /^\/([A-Za-z][\w:.-]*)(?:\s+([\s\S]*))?$/.exec(t);
  if (!m) return undefined;
  const name = m[1].toLowerCase();
  const args = (m[2] ?? "").trim();
  // /skills update and /agents update are the bundled /skills-update and
  // /agents-update (the engine reads them the same way).
  const update = /^update(?:\s+([\s\S]*))?$/i.exec(args);
  if ((name === "skills" || name === "agents") && update) return { name: `${name}-update`, args: (update[1] ?? "").trim() };
  if (name === "set") return { name: "config", args }; // the terminal's other name for it
  return { name, args };
}

/**
 * The commands offered while the user types "/prefix" (no space yet;
 * leading whitespace allowed, as parseCommand trims it): the one named
 * exactly first, then prefix matches, then the rest containing it.
 */
export function matchCommands(draft: string, all: CommandSpec[]): CommandSpec[] {
  const m = /^\s*\/(\S*)$/.exec(draft);
  if (!m) return [];
  const q = m[1].toLowerCase();
  // The command typed in full first: Enter runs /mode, not /model.
  const starts = all.filter((c) => c.name.startsWith(q)).sort((a, b) => Number(b.name === q) - Number(a.name === q));
  const contains = all.filter((c) => !c.name.startsWith(q) && (c.name.includes(q) || (q.length > 2 && c.description.toLowerCase().includes(q))));
  return [...starts, ...contains].slice(0, 12);
}

/** Every command: the built-in ones first, then the workspace's (a built-in name wins). */
export function allCommands(custom: CommandSpec[]): CommandSpec[] {
  const own = builtins();
  const names = new Set(own.map((b) => b.name));
  return [...own, ...custom.filter((c) => !names.has(c.name)).sort((a, b) => a.name.localeCompare(b.name))];
}

/** /help's text, as Markdown: the built-ins in their groups, then the custom commands by source, each sorted. */
export function helpText(all: CommandSpec[]): string {
  // A "|" would end a table cell, also inside code: escape it everywhere.
  const cell = (s: string) => s.replace(/\|/g, "\\|");
  const row = (c: CommandSpec) => `| \`/${cell(c.name + (c.args ? " " + c.args : ""))}\` | ${cell(c.description)} |`;
  const head = `| ${t("desktop.help.command")} | ${t("desktop.help.does")} |\n|---|---|\n`;
  const byName = (a: CommandSpec, b: CommandSpec) => a.name.localeCompare(b.name);
  let s = `**${t("desktop.help.title")}**`;
  for (const g of commandGroups) {
    const list = all.filter((c) => c.source === "builtin" && c.group === g).sort(byName);
    if (list.length) s += `\n\n**${t(`help.group.${g}`)}**\n\n${head}` + list.map(row).join("\n");
  }
  const custom = all.filter((c) => c.source !== "builtin");
  if (custom.length) {
    s += `\n\n**${t("desktop.help.custom")}**`;
    const sources = [...new Set(custom.map((c) => c.source))].sort((a, b) => sourceRank(a) - sourceRank(b) || a.localeCompare(b));
    for (const src of sources) {
      const key = `help.source.${src}`;
      const title = t(key);
      s += `\n\n**${title === key ? src : title}**\n\n${head}` + custom.filter((c) => c.source === src).sort(byName).map(row).join("\n");
    }
  }
  s += `\n\n${t("desktop.help.terminal_only", { commands: terminalOnly.map((c) => (c === "!" ? "`!<command>`" : `\`/${c}\``)).join(", ") })}`;
  s += `\n\n${t("desktop.help.palette")}`;
  return s;
}

const customOrder = ["bundled", "project", "user", "plugin", "skill", "mcp"];
const sourceRank = (src: string) => {
  const i = customOrder.indexOf(src.split(/[: ]/)[0]);
  return i < 0 ? customOrder.length : i;
};

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

/** Whether the palette looks for files as well: a query that isn't a command. */
export function searchesFiles(query: string): boolean {
  const q = query.trim();
  return q !== "" && !q.startsWith("/");
}

/**
 * What the palette shows for a query: the files whose names match first
 * (as FindFiles ranked them, not filtered again: it matches loosely), then
 * the passages workspace search found (as it ranked them), then the other
 * items that match. A command query ("/…") shows neither.
 */
export function paletteResults(files: PaletteItem[], items: PaletteItem[], query: string, text: PaletteItem[] = []): PaletteItem[] {
  const rest = filterPalette(items, query);
  return searchesFiles(query) ? [...files, ...text, ...rest] : rest;
}
