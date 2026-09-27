// Slash commands in the composer: the built-in ones the window runs
// through the service's API (as the terminal's REPL does), and the
// workspace's custom commands and skills, which run as turns.

export interface CommandSpec {
  name: string; // without the slash
  args?: string; // shown after the name
  description: string;
  /** "builtin" runs in the window; the others run as a command turn. */
  source: "builtin" | "project" | "user" | "bundled" | "skill";
}

export const builtins: CommandSpec[] = [
  { name: "plan", args: "<goal>", description: "Plan first: investigate, then show a plan to approve before changing anything", source: "builtin" },
  { name: "btw", args: "<question>", description: "Ask a side question; the answer isn't kept in the conversation", source: "builtin" },
  { name: "search", args: "web|session <terms>", description: "Search the web (then read the best pages) or this conversation", source: "builtin" },
  { name: "undo", args: "[--force]", description: "Revert the files the last turn changed", source: "builtin" },
  { name: "checkpoints", description: "List the turns that changed files", source: "builtin" },
  { name: "diff", description: "Show everything the agent changed in this session", source: "builtin" },
  { name: "cost", description: "Tokens and cost of this session", source: "builtin" },
  { name: "context", description: "How full the model's context is", source: "builtin" },
  { name: "compact", args: "[focus]", description: "Summarize older turns to free context", source: "builtin" },
  { name: "agent", args: "[name]", description: "Show the agents, or switch to one", source: "builtin" },
  { name: "model", args: "[provider/model]", description: "Show the model, or switch to another", source: "builtin" },
  { name: "mode", args: "[default|accept-edits|plan|dont-ask|bypass]", description: "Show or change the permission mode", source: "builtin" },
  { name: "effort", args: "[minimal|low|medium|high|max|auto]", description: "Show or set how hard the model thinks", source: "builtin" },
  { name: "session", args: "save <name> [--force]", description: "Save this conversation as a named snapshot", source: "builtin" },
  { name: "rename", args: "<title>", description: "Rename this conversation", source: "builtin" },
  { name: "new", description: "Start a new conversation", source: "builtin" },
  { name: "help", description: "List the commands", source: "builtin" },
];

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
  const names = new Set(builtins.map((b) => b.name));
  return [...builtins, ...custom.filter((c) => !names.has(c.name))];
}

/** /help's text, as Markdown. */
export function helpText(all: CommandSpec[]): string {
  // A "|" would end a table cell, also inside code: escape it everywhere.
  const cell = (s: string) => s.replace(/\|/g, "\\|");
  const row = (c: CommandSpec) => `| \`/${cell(c.name + (c.args ? " " + c.args : ""))}\` | ${cell(c.description)} |`;
  const custom = all.filter((c) => c.source !== "builtin");
  let s = "**Commands**\n\n| Command | What it does |\n|---|---|\n" + all.filter((c) => c.source === "builtin").map(row).join("\n");
  if (custom.length) s += "\n\n**This workspace's commands and skills**\n\n| Command | What it does |\n|---|---|\n" + custom.map(row).join("\n");
  s += "\n\nPress **Cmd/Ctrl+K** for the command palette.";
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
