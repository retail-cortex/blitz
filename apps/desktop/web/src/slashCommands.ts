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

// The window's own slash commands (spec_desktop_024 DSK-102): the
// terminal's, as far as a window has them, in the same groups as its
// /help. Each runs against the service's API and answers with a notice in
// the conversation, a turn, or the window's own UI. The terminal-only ones
// (terminalOnly) are named in /help with the window's way instead.
import { config, files, sessions, workspaces } from "./api";
import { copyText } from "./clipboard";
import { openDocs, showLicense } from "./events";
import { loadAppDocs } from "./docs/load";
import { searchDocs } from "./docs/index";
import { language, t, tn } from "./i18n";
import { formatInterval, listLoops, parseInterval, startLoop, stopLoop } from "./loops";
import { efforts, modeOf } from "./options";
import { reloadSummary } from "./reload";
import type { GetSettingsResponse } from "./gen/blitz/v1/workspace_pb";

/** The groups, in /help's order (titles: help.group.<id>, shared with the terminal). */
export const commandGroups = ["session", "agents", "work", "workspace", "safety", "extensions", "settings"] as const;
/** A group of commands in /help. */
export type CommandGroup = (typeof commandGroups)[number];

/** The terminal's commands a window has no use for, and the window's way instead (desktop.help.instead.<name>). */
export const terminalOnly = ["clear", "exit", "quit", "paste", "cd", "!"] as const;

/** How a command's turn is sent (a subset of Conversation's options). */
export interface CommandTurn {
  plan?: boolean;
  aside?: boolean;
  command?: boolean;
  readOnly?: string;
  prompt?: string;
  fetchGrants?: string[];
  shown?: string;
}

/** What a command can see and do: the conversation it was typed in. */
export interface CommandContext {
  dir: string;
  /** The words after the command's name. */
  args: string;
  /** The line as typed. */
  line: string;
  settings?: GetSettingsResponse;
  sessionId?: string;
  sessionTitle?: string;
  projectWaiting: number;
  say(text: string, tone?: "info" | "error", markdown?: boolean): void;
  run(text: string, opts?: CommandTurn): void;
  /** Sends a prompt, or runs a command, as if typed (a loop's firing). */
  send(text: string): void;
  snack(text: string, opts?: { error?: boolean }): void;
  /** Says the command's usage, as an error. */
  usage(): void;
  busy(): boolean;
  settingsChanged(): void;
  openView(v: "changes" | "workers" | "processes"): void;
  /** Opens Settings on a section; connect, a URL or file for a new connection there (Connectors). */
  openSettings(section: string, scope?: string, connect?: string): void;
  reviewProject?: () => void;
  reload(): Promise<void>;
  refreshTotal(): void;
  refreshList(): void;
  newSession(): Promise<void>;
  rename(title: string): Promise<void>;
  rewind(index: number, mode: string, force: boolean): Promise<void>;
  addImagePath(path: string): void;
  /** Empties the attachments; how many there were. */
  clearAttachments(): number;
  /** The conversation's answers, oldest first. */
  answers(): string[];
  theme(): string;
  setTheme(theme: "system" | "light" | "dark"): void;
  /** /help's text. */
  help(): string;
}

/** A built-in command. */
export interface Builtin {
  name: string;
  args?: string;
  group: CommandGroup;
  /** Its description's key (default desktop.cmd.<name>). */
  describe?: string;
  /** It starts a turn or changes the session, so not while one runs. */
  turn?: boolean;
  run(c: CommandContext): Promise<void> | void;
}

const words = (s: string) => s.split(/\s+/).filter(Boolean);
const k = (n: number | bigint) => Number(n).toLocaleString(language());
// A titled list; the title is text (a "<name>" in it is shown, not taken for HTML).
const bullets = (title: string, lines: string[]) => `**${md(title)}**\n\n${lines.map((l) => `- ${l}`).join("\n")}`;
const code = (s: string) => "`" + s.replace(/`/g, "'") + "`";
const md = (s: string) => s.replace(/([\\*_[\]|<>])/g, "\\$1");

/** The file /export writes when given none, as the terminal names it. */
export function exportName(title: string, id: string): string {
  let slug = title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  if (!slug) slug = `session-${id}`;
  if (slug.length > 50) slug = slug.slice(0, 50).replace(/-+$/, "");
  return `${slug}.md`;
}

/** The goal's prompt, as the terminal sends it. */
export const goalPrompt = (condition: string) => `Work toward this goal, and keep going until it holds: ${condition}`;

async function listAgents(c: CommandContext) {
  const r = await workspaces.listAgents({ workspace: c.dir });
  c.say(
    t("desktop.agents.title") +
      "\n\n" +
      r.agents.map((a) => `- ${a.active ? "**" : ""}${a.displayName}${a.active ? `** ${t("desktop.agents.active")}` : ""} — \`${a.name}\`: ${a.description}${a.pinnedModel ? ` (${a.pinnedModel})` : ""}`).join("\n"),
    "info",
    true,
  );
}

async function usageTable(c: CommandContext, which: "cost" | "context") {
  const r = await sessions.getUsage({ workspace: c.dir });
  const u = r.usage;
  if (!u || u.calls === 0) return c.say(t("desktop.usage.none"));
  c.say(
    which === "cost"
      ? `| | |\n|---|---|\n| ${t("desktop.usage.calls")} | ${u.calls} |\n| ${t("desktop.usage.sent")} | ${k(u.input)} (${t("desktop.usage.cached", { count: k(u.cached) })}) |\n| ${t("desktop.usage.received")} | ${k(u.output)} |\n| ${t("desktop.usage.cost")} | ${u.priced ? "$" + u.costUsd.toFixed(4) : t("desktop.usage.unpriced")} |`
      : t("desktop.usage.context", { count: k(u.lastPrompt) }) + (r.autoCompact ? t("desktop.usage.auto", { threshold: r.threshold.toLocaleString(language()) }) : t("desktop.usage.manual")),
    "info",
    true,
  );
}

/** Every built-in, in no particular order (builtinsInOrder sorts them). */
export const builtinCommands: Builtin[] = [
  // Session
  { name: "btw", args: "<question>", group: "session", turn: true, run: (c) => (c.args ? c.run(c.args, { aside: true, shown: `/btw ${c.args}` }) : c.usage()) },
  {
    name: "checkpoints",
    group: "session",
    async run(c) {
      const r = await workspaces.listCheckpoints({ workspace: c.dir });
      c.say(
        r.checkpoints.length ? t("desktop.checkpoints.title") + "\n\n" + r.checkpoints.map((cp) => `- ${cp.label} — ${cp.files.map((f) => `\`${f}\``).join(", ")}`).join("\n") : t("desktop.checkpoints.none"),
        "info",
        true,
      );
    },
  },
  {
    name: "compact",
    args: "[focus]",
    group: "session",
    turn: true,
    async run(c) {
      c.say(t("desktop.compacting"));
      const r = await sessions.compact({ workspace: c.dir, focus: c.args });
      c.say(t("desktop.compacted", { events: r.eventsCompacted, chars: r.summaryChars }));
      c.refreshTotal();
    },
  },
  { name: "context", group: "session", run: (c) => usageTable(c, "context") },
  {
    name: "copy",
    args: "[n]",
    group: "session",
    describe: "help.copy",
    async run(c) {
      const n = c.args ? Number(c.args) : 1;
      if (!Number.isInteger(n) || n < 1) return c.usage();
      const all = c.answers();
      if (all.length === 0) return c.say(t("copy.none"));
      if (n > all.length) return c.usage();
      await copyText(all[all.length - n]);
      c.snack(t("desktop.copied"));
    },
  },
  { name: "cost", group: "session", run: (c) => usageTable(c, "cost") },
  {
    name: "export",
    args: "[file]",
    group: "session",
    describe: "help.export",
    async run(c) {
      const r = await sessions.exportSession({ workspace: c.dir });
      let path = c.args || exportName(c.sessionTitle ?? "", c.sessionId ?? "");
      if (path.startsWith(c.dir + "/")) path = path.slice(c.dir.length + 1);
      if (path.startsWith("/")) return c.say(t("desktop.cmd.in_workspace", { path }), "error");
      // A .docx name writes a Word document (FS-75); anything else, Markdown.
      if (/\.docx$/i.test(path)) {
        const { data } = await files.renderDocx({ workspace: c.dir, path: "", markdown: r.markdown });
        await files.writeBinaryFile({ workspace: c.dir, path, data });
      } else {
        await files.writeFile({ workspace: c.dir, path, text: r.markdown });
      }
      c.say(t("export.done", { path }));
    },
  },
  {
    name: "fork",
    args: "[n]",
    group: "session",
    describe: "help.fork",
    turn: true,
    async run(c) {
      const n = c.args ? Number(c.args) : 0;
      if (!Number.isInteger(n) || n < 0) return c.say(t("fork.usage"), "error");
      const r = await sessions.forkSession({ workspace: c.dir, turn: n });
      await c.reload();
      c.refreshList();
      c.say(t("fork.done", { id: r.session?.id ?? "", messages: r.session?.messageCount ?? 0 }));
    },
  },
  { name: "new", group: "session", turn: true, run: (c) => c.newSession() },
  { name: "rename", args: "<title>", group: "session", turn: true, run: async (c) => (c.args ? (await c.rename(c.args), c.say(t("desktop.renamed", { title: c.args }))) : c.usage()) },
  {
    name: "rewind",
    args: "[n [both|conversation|code]] [--force]",
    group: "session",
    describe: "help.rewind",
    turn: true,
    async run(c) {
      const points = [...(await sessions.listRewindPoints({ workspace: c.dir })).points].sort((a, b) => b.index - a.index);
      const w = words(c.args);
      const force = w.includes("--force");
      const [n, mode = "both"] = w.filter((x) => x !== "--force");
      if (!n) {
        if (points.length === 0) return c.say(t("rewind.none"));
        return c.say(bullets(t("rewind.title"), points.map((p, i) => `${i + 1}. ${md(p.text.split("\n")[0].slice(0, 100))}`)).replace(/^- /gm, ""), "info", true);
      }
      const p = points[Number(n) - 1];
      if (!p || !["both", "conversation", "code", "summarize-from", "summarize-up-to"].includes(mode)) return c.say(t("rewind.usage"), "error");
      await c.rewind(p.index, mode, force);
    },
  },
  {
    name: "session",
    args: "save <name> [--force]",
    group: "session",
    turn: true,
    async run(c) {
      const m = /^save\s+(\S+)(\s+--force)?$/.exec(c.args);
      if (!m) return c.usage();
      const r = await sessions.saveSnapshot({ workspace: c.dir, name: m[1], force: !!m[2] });
      c.say(t("desktop.snapshot.saved", { name: r.snapshot?.snapshot ?? m[1] }));
      c.refreshList();
    },
  },
  {
    name: "status",
    group: "session",
    describe: "help.status",
    async run(c) {
      const ws = c.dir;
      const [model, usage, git, sandbox, mcp, info] = await Promise.all([
        workspaces.getModel({ workspace: ws }),
        sessions.getUsage({ workspace: ws }).catch(() => undefined),
        workspaces.getGitStatus({ workspace: ws }).catch(() => undefined),
        workspaces.getSandbox({ workspace: ws }).catch(() => undefined),
        workspaces.listMCPServers({ workspace: ws }).catch(() => undefined),
        workspaces.getServiceInfo({}).catch(() => undefined),
      ]);
      const s = c.settings;
      const u = usage?.usage;
      const rows: [string, string][] = [
        [t("status.workspace"), code(ws)],
        [t("status.session"), md(c.sessionTitle || c.sessionId || "")],
        [t("status.agent"), md(s?.agent ?? "")],
        [t("status.model"), md(`${model.provider ? model.provider + "/" : ""}${model.name}`) + (model.unavailable ? ` — ${md(model.unavailable)}` : "")],
        [t("status.mode"), modeOf(s?.permissionMode ?? "auto").label],
        [t("desktop.cmd.effort_row"), efforts().find((e) => e.value === (s?.effort ?? ""))?.label ?? t("desktop.effort.auto")],
      ];
      if (u && u.calls > 0) rows.push([t("desktop.usage.cost"), u.priced ? "$" + u.costUsd.toFixed(4) : t("desktop.usage.unpriced")]);
      if (git?.repo) rows.push([t("desktop.cmd.git_row"), `${md(git.branch)} · ${tn("desktop.cmd.changed", git.changed)}`]);
      if (sandbox?.summary.length) rows.push([t("status.sandbox"), md(sandbox.summary[0])]);
      rows.push([t("status.mcp"), mcp?.servers.length ? mcp.servers.map((m) => md(m.name)).join(", ") : "—"]);
      if (info?.version) rows.push([t("status.version"), md(info.version)]);
      c.say(`**${t("status.title")}**\n\n| | |\n|---|---|\n` + rows.map(([a, b]) => `| ${a} | ${b} |`).join("\n"), "info", true);
    },
  },
  {
    name: "undo",
    args: "[--force]",
    group: "session",
    async run(c) {
      const res = await workspaces.undo({ workspace: c.dir, force: c.args === "--force" });
      c.say(res.restored.length ? t("desktop.changes.undid", { label: res.label, files: res.restored.join(", ") }) : t("desktop.undo.nothing"));
      if (res.error) c.say(res.error.message, "error");
    },
  },

  // Agents and models
  {
    name: "agent",
    args: "[name]",
    group: "agents",
    turn: true,
    async run(c) {
      if (!c.args) return listAgents(c);
      const r = await workspaces.setAgent({ workspace: c.dir, name: c.args });
      c.say(t("desktop.agents.switched", { name: r.agent?.displayName ?? c.args }));
      c.settingsChanged();
    },
  },
  {
    name: "agents",
    args: "[update [focus]]",
    group: "agents",
    describe: "help.agents",
    run: (c) => (/^update\b/i.test(c.args) ? c.run(c.line.trim(), { command: true }) : listAgents(c)),
  },
  {
    name: "effort",
    args: "[minimal|low|medium|high|max|auto]",
    group: "agents",
    async run(c) {
      if (!c.args) return c.say(t("desktop.effort.is", { effort: efforts().find((e) => e.value === (c.settings?.effort ?? ""))?.label ?? t("desktop.effort.auto") }), "info", true);
      await workspaces.setSetting({ workspace: c.dir, key: "effort", value: c.args });
      c.say(t("desktop.effort.now", { effort: c.args }));
      c.settingsChanged();
    },
  },
  {
    name: "model",
    args: "[provider/model]",
    group: "agents",
    turn: true,
    async run(c) {
      if (!c.args) {
        const m = await workspaces.getModel({ workspace: c.dir });
        const ref = `${m.provider ? m.provider + "/" : ""}${m.name}`;
        return c.say(m.unavailable ? t("desktop.model.unavailable", { model: ref, reason: m.unavailable }) : t("desktop.model.is", { model: ref }), "info", true);
      }
      await workspaces.setModel({ workspace: c.dir, ref: c.args });
      c.say(t("desktop.model.now", { model: c.args }));
      c.settingsChanged();
    },
  },
  {
    name: "model_settings",
    args: "[<model> [key=value…|reset]]",
    group: "agents",
    describe: "help.model_settings",
    async run(c) {
      const [ref, ...rest] = words(c.args);
      if (!ref) {
        const all = (await workspaces.getModelSettings({ workspace: c.dir })).all;
        const names = Object.keys(all).sort();
        if (names.length === 0) return c.say(t("msettings.none"));
        return c.say(bullets(t("msettings.title"), names.map((n) => `${code(n)} ${settingsText(all[n])}`)), "info", true);
      }
      if (rest.length === 0) {
        const m = (await workspaces.getModelSettings({ workspace: c.dir, ref })).model;
        return c.say(`${code(m?.model ?? ref)} ${settingsText(m?.settings)}`, "info", true);
      }
      const reset = rest.length === 1 && rest[0] === "reset";
      const changes: { key: string; value: string }[] = [];
      if (!reset)
        for (const pair of rest) {
          const [key, ...v] = pair.split("=");
          if (!key || v.length === 0) return c.say(t("msettings.bad_pair", { arg: pair }), "error");
          changes.push({ key, value: v.join("=") });
        }
      const r = await workspaces.updateModelSettings({ workspace: c.dir, ref, reset, changes });
      const model = r.model?.model ?? ref;
      c.say(reset ? t("msettings.cleared", { model }) : t("msettings.updated", { model, settings: changes.map((x) => `${x.key}=${x.value}`).join(" ") }));
      if (r.unsupported.length) c.say(t("msettings.unsupported", { provider: r.model?.provider ?? "", keys: r.unsupported.join(", "), model }));
      if (r.saved?.error) c.say(r.saved.error, "error");
      c.settingsChanged();
    },
  },
  {
    name: "pin_model",
    args: "[<agent> <model>]",
    group: "agents",
    describe: "help.pin",
    async run(c) {
      const w = words(c.args);
      if (w.length === 0) {
        const pinned = (await workspaces.listAgents({ workspace: c.dir })).agents.filter((a) => a.pinnedModel);
        return c.say(pinned.length ? bullets(t("pin.title"), pinned.map((a) => `${code(a.name)} → ${code(a.pinnedModel)}`)) : t("pin.none"), "info", true);
      }
      if (w.length !== 2) return c.say(t("pin.usage"), "error");
      const r = await workspaces.pinModel({ workspace: c.dir, agent: w[0], ref: w[1] });
      c.say(t("pin.done", { agent: r.agent, model: r.model }) + (r.saved?.path ? " " + t("pin.saved", { path: r.saved.path }) : ""));
      c.settingsChanged();
    },
  },
  {
    name: "style",
    args: "[name]",
    group: "agents",
    describe: "help.style",
    async run(c) {
      if (!c.args) {
        const r = await workspaces.listStyles({ workspace: c.dir });
        return c.say(bullets(t("style.list"), r.styles.map((s) => `${s.active ? "**" : ""}${code(s.name)}${s.active ? "**" : ""}: ${md(s.description)}`)), "info", true);
      }
      await workspaces.setSetting({ workspace: c.dir, key: "style", value: c.args });
      c.say(t("style.set", { name: c.args }));
      c.settingsChanged();
    },
  },
  {
    name: "unpin",
    args: "<agent>",
    group: "agents",
    describe: "help.unpin",
    async run(c) {
      if (words(c.args).length !== 1) return c.say(t("pin.unpin_usage"), "error");
      const r = await workspaces.unpinModel({ workspace: c.dir, agent: c.args });
      c.say(t("pin.unpinned", { agent: r.agent, model: r.model }));
      c.settingsChanged();
    },
  },

  // Planning and long work
  {
    name: "goal",
    args: "[<condition>|clear]",
    group: "work",
    describe: "help.goal",
    turn: true,
    async run(c) {
      if (!c.args) {
        const g = (await sessions.getGoal({ workspace: c.dir }).catch(() => undefined))?.goal;
        return c.say(g ? t("goal.show", { condition: g.condition, n: g.continues, max: g.max }) : t("goal.none"));
      }
      if (c.args === "clear") {
        await sessions.clearGoal({ workspace: c.dir }).then(
          () => c.say(t("goal.cleared")),
          () => c.say(t("goal.none")),
        );
        return;
      }
      const g = (await sessions.setGoal({ workspace: c.dir, condition: c.args })).goal;
      c.say(t("goal.set", { max: g?.max ?? 0 }));
      c.run(c.line.trim(), { prompt: goalPrompt(c.args), shown: c.line.trim() });
    },
  },
  {
    name: "loop",
    args: "[<interval> <prompt>|stop <n>]",
    group: "work",
    describe: "help.loop",
    run(c) {
      const [first, ...rest] = words(c.args);
      if (!first) {
        const list = listLoops(c.dir);
        if (list.length === 0) return c.say(t("loop.none"));
        const next = (ms: number) => new Date(ms).toLocaleTimeString(language(), { hour: "2-digit", minute: "2-digit" });
        return c.say(bullets(t("loop.list"), list.map((l) => md(t("loop.item", { n: l.n, interval: formatInterval(l.everyMs), prompt: l.prompt, next: next(l.next) })).replace(/^(\d+)\./, "$1\\."))) + `\n\n${t("desktop.cmd.loop_workers")}`, "info", true);
      }
      if (first === "stop") {
        const n = Number(rest[0]);
        return stopLoop(c.dir, n) ? c.say(t("loop.stopped", { n })) : c.say(t("loop.usage"), "error");
      }
      const every = parseInterval(first);
      const prompt = rest.join(" ");
      if (!every || !prompt) return c.say(t("loop.usage"), "error");
      const n = startLoop(c.dir, prompt, every, c.send, c.busy);
      c.say(t("loop.started", { n, prompt, interval: formatInterval(every) }));
    },
  },
  { name: "plan", args: "<goal>", group: "work", turn: true, run: (c) => (c.args ? c.run(c.args, { plan: true }) : c.usage()) },
  {
    name: "search",
    args: "web|session <terms>",
    group: "work",
    turn: true,
    async run(c) {
      const [sub, ...rest] = words(c.args);
      const terms = rest.join(" ").trim();
      if (!terms || (sub !== "web" && sub !== "session")) return c.usage();
      const recorded = `/search ${sub} ${terms}`;
      if (sub === "session") {
        const r = await sessions.searchSession({ workspace: c.dir, terms });
        if (r.found === 0) c.say(t("desktop.search.session_none", { terms }));
        else c.run(recorded, { prompt: r.prompt, readOnly: "search", shown: recorded });
        return;
      }
      c.say(t("desktop.search.searching", { terms }));
      const r = await workspaces.searchWeb({ workspace: c.dir, terms });
      if (r.links.length === 0) return c.say(t("desktop.search.none", { terms }));
      c.say(`${t("desktop.search.found", { provider: r.provider })}\n\n${r.links.map((l, i) => `${i + 1}. [${l.title.replace(/[[\]]/g, "")}](${l.url})`).join("\n")}\n\n${t("desktop.search.reading")}`, "info", true);
      c.run(recorded, { prompt: r.prompt, readOnly: "search", fetchGrants: r.links.map((l) => l.url), shown: recorded });
    },
  },
  {
    name: "tasks",
    args: "[show|stop <id>]",
    group: "work",
    describe: "help.tasks",
    async run(c) {
      const [sub, id] = words(c.args);
      const req = { workspace: c.dir, sessionIds: c.sessionId ? [c.sessionId] : [] };
      const state = (s: string) => t(`tasks.state.${s}`) || s;
      if (!sub) {
        const list = (await workspaces.listTasks(req)).tasks;
        if (list.length === 0) return c.say(t("tasks.none"));
        return c.say(bullets(t("tasks.title"), list.map((x) => `${code(x.id)} ${md(x.agent)} — ${state(x.state)}: ${md(x.prompt.split("\n")[0].slice(0, 100))}`)), "info", true);
      }
      if ((sub !== "show" && sub !== "stop") || !id) return c.say(t("tasks.usage"), "error");
      if (sub === "stop") {
        const r = await workspaces.stopTask({ ...req, id });
        return c.say(`${code(id)} ${state(r.task?.state ?? "stopped")}`, "info", true);
      }
      const r = await workspaces.getTask({ ...req, id });
      const x = r.task;
      c.say(`${code(id)} ${md(x?.agent ?? "")} — ${state(x?.state ?? "")}\n\n${md(x?.result || x?.error || r.events.slice(-5).join("\n"))}`, "info", true);
    },
  },

  // Workspace and files
  {
    name: "attach",
    args: "<path>|clear",
    group: "workspace",
    describe: "help.attach",
    run(c) {
      if (!c.args) return c.usage();
      if (c.args === "clear") return c.say(tn("attach.cleared", c.clearAttachments()));
      c.addImagePath(c.args);
      c.say(t("attach.queued", { summary: c.args }));
    },
  },
  { name: "diff", group: "workspace", run: (c) => c.openView("changes") },
  // The window's way to the terminal's list, output and stop: the Processes view.
  { name: "processes", group: "work", run: (c) => c.openView("processes") },
  { name: "init", group: "workspace", describe: "help.init", turn: true, run: (c) => c.run("/setup", { command: true }) },
  {
    name: "reload",
    group: "workspace",
    describe: "help.reload",
    async run(c) {
      const r = await workspaces.reloadAgentFiles({ workspace: c.dir });
      const files = r.files.length ? bullets(reloadSummary(r), r.files.map(code)) : reloadSummary(r);
      c.say(r.problems.length ? `${files}\n\n${bullets(t("reload.problems"), r.problems.map(md))}` : files, r.problems.length ? "error" : "info", true);
    },
  },
  {
    name: "memory",
    args: "[reload|add <note>|notes|forget <name>]",
    group: "workspace",
    describe: "help.memory",
    async run(c) {
      const [sub = "reload", ...rest] = words(c.args);
      const arg = rest.join(" ");
      switch (sub) {
        case "reload":
        case "show": {
          const r = await workspaces.reloadMemory({ workspace: c.dir });
          return c.say(r.paths.length ? bullets(t("memory.loaded"), r.paths.map(code)) : t("memory.none", { files: r.files.join(", ") }), "info", true);
        }
        case "add":
          if (!arg) break;
          return c.say(t("memory.added", { path: (await workspaces.addMemory({ workspace: c.dir, text: arg })).path }));
        case "notes": {
          const notes = (await workspaces.listNotes({ workspace: c.dir })).notes;
          return c.say(notes.length ? bullets(t("memory.notes"), notes.map((n) => `${code(n.name)} (${md(n.kind)}): ${md(n.text.split("\n")[0].slice(0, 120))}`)) : t("memory.no_notes"), "info", true);
        }
        case "forget":
          if (!arg) break;
          await workspaces.forgetNote({ workspace: c.dir, name: arg });
          return c.say(t("memory.forgot", { name: arg }));
      }
      c.say(t("memory.usage"), "error");
    },
  },
  {
    name: "trust",
    group: "workspace",
    describe: "help.trust",
    async run(c) {
      if (c.projectWaiting > 0 && c.reviewProject) return c.reviewProject();
      const p = (await workspaces.getProjectSettings({ workspace: c.dir })).settings;
      if (!p || p.files.length === 0) return c.say(t("desktop.cmd.trust_none"));
      c.say(t("desktop.cmd.trust_state", { files: p.files.join(", "), state: p.state }));
    },
  },

  // Permissions and safety
  {
    name: "approvals",
    args: "[revoke <n>|clear]",
    group: "safety",
    describe: "help.approvals",
    async run(c) {
      const [sub, n] = words(c.args);
      const list = (await workspaces.listApprovals({ workspace: c.dir })).approvals;
      const done = (r: { revoked: number; error?: { message: string } }) => {
        c.say(tn("approvals.revoked", r.revoked));
        if (r.error) c.say(t("approvals.still_saved", { error: r.error.message }), "error");
      };
      if (!sub) {
        if (list.length === 0) return c.say(t("approvals.none"));
        return c.say(
          `**${t("approvals.title")}**\n\n${list.map((a, i) => `${i + 1}. ${md(a.kind)} ${code(a.subject)} — ${a.always ? t("approvals.scope_always", { date: a.added ? new Date(Number(a.added.seconds) * 1000).toLocaleDateString(language()) : "" }) : t("approvals.scope_session")}`).join("\n")}\n\n${t("approvals.revoke_hint")}`,
          "info",
          true,
        );
      }
      if (sub === "clear") return done(await workspaces.revokeApprovals({ workspace: c.dir, all: true }));
      const a = list[Number(n) - 1];
      if (sub !== "revoke" || !a) return c.say(t("approvals.revoke_usage"), "error");
      done(await workspaces.revokeApprovals({ workspace: c.dir, keys: [a.key] }));
    },
  },
  {
    name: "hooks",
    group: "safety",
    describe: "help.hooks",
    async run(c) {
      const hooks = (await workspaces.listHooks({ workspace: c.dir })).hooks;
      if (hooks.length === 0) return c.say(t("hooks.none"));
      c.say(
        `| ${t("desktop.cmd.hook_event")} | ${t("desktop.cmd.hook_runs")} | ${t("desktop.cmd.hook_from")} |\n|---|---|---|\n` +
          hooks.map((h) => `| ${md(h.event)}${h.match ? ` ${code(h.match)}` : ""} | ${md(h.type)}: ${code(h.runs)} | ${md(h.source)}${h.failures.length ? ` ⚠ ${h.failures.length}` : ""} |`).join("\n"),
        "info",
        true,
      );
    },
  },
  {
    name: "mode",
    args: "[default|accept-edits|plan|dont-ask|bypass]",
    group: "safety",
    async run(c) {
      if (!c.args) return c.say(t("desktop.mode.is", { mode: modeOf(c.settings?.permissionMode ?? "auto").label }), "info", true);
      const r = await workspaces.setPermissionMode({ workspace: c.dir, mode: c.args });
      c.say(t("desktop.mode.now", { mode: modeOf(r.mode).label }));
      c.settingsChanged();
    },
  },
  {
    name: "permissions",
    args: "[allow|ask|deny|remove <rule> [--save]]",
    group: "safety",
    describe: "help.permissions",
    async run(c) {
      const w = words(c.args);
      const save = w.includes("--save");
      const [effect, ...ruleWords] = w.filter((x) => x !== "--save");
      const rule = ruleWords.join(" ");
      if (!effect) {
        const r = await workspaces.listPermissionRules({ workspace: c.dir });
        if (r.problem) c.say(r.problem, "error");
        if (r.rules.length === 0) return c.say(t("desktop.cmd.rules_none"));
        return c.say(bullets(t("desktop.cmd.rules_title"), r.rules.map((x) => `**${md(x.effect)}** ${code(x.rule)}${x.source ? ` (${md(x.source)})` : ""}`)), "info", true);
      }
      if (!rule || !["allow", "ask", "deny", "remove"].includes(effect)) return c.usage();
      if (effect === "remove") {
        const r = await workspaces.removePermissionRule({ workspace: c.dir, rule, save });
        c.say(t("desktop.cmd.rule_removed", { rule: r.rule, count: r.removed }));
        if (r.saved?.error) c.say(r.saved.error, "error");
      } else {
        const r = await workspaces.addPermissionRule({ workspace: c.dir, effect, rule, save });
        c.say(t("desktop.cmd.rule_added", { effect, rule: r.rule }) + (r.saved?.path ? " " + t("pin.saved", { path: r.saved.path }) : ""));
        if (r.saved?.error) c.say(r.saved.error, "error");
      }
      c.settingsChanged();
    },
  },
  {
    name: "sandbox",
    group: "safety",
    describe: "help.sandbox",
    async run(c) {
      const r = await workspaces.getSandbox({ workspace: c.dir });
      c.say(bullets(t("sandbox.title"), r.summary.map(md)), "info", true);
    },
  },

  // Extensions
  {
    name: "envs",
    args: "[prune|remove <key>]",
    group: "extensions",
    describe: "help.envs",
    async run(c) {
      const [sub, key] = words(c.args);
      if (!sub) {
        const envs = (await workspaces.listEnvs({ workspace: c.dir })).envs;
        if (envs.length === 0) return c.say(t("envs.none"));
        return c.say(
          bullets(
            t("envs.title", { count: envs.length }),
            envs.map((e) => `${code(e.key)} ${md(e.deps.join(", "))} — ${bytes(e.size)}${e.skills.length ? ", " + t("envs.used_by", { skills: e.skills.join(", ") }) : ""}${e.ready ? "" : " " + t("envs.incomplete")}`),
          ),
          "info",
          true,
        );
      }
      if (sub === "prune") {
        const r = await workspaces.pruneEnvs({ workspace: c.dir });
        c.say(t("envs.pruned", { count: r.removed, size: bytes(r.freed) }));
        r.failed.forEach((f) => c.say(t("envs.remove_failed", { key: f.key, error: f.error }), "error"));
        return;
      }
      if (sub === "remove" && key) {
        await workspaces.removeEnv({ workspace: c.dir, key });
        return c.say(t("envs.removed", { key }));
      }
      c.say(t("envs.usage"), "error");
    },
  },
  {
    name: "connect",
    args: "[postgres://…|file.sqlite]",
    group: "extensions",
    describe: "help.connect",
    run(c) {
      c.openSettings("connectors", undefined, c.args.trim() || undefined);
    },
  },
  {
    name: "mcp",
    group: "extensions",
    describe: "help.mcp",
    async run(c) {
      const servers = (await workspaces.listMCPServers({ workspace: c.dir })).servers;
      if (servers.length === 0) return c.say(t("mcp.none"));
      c.say(bullets(t("mcp.title"), servers.map((s) => `**${md(s.name)}** ${code(s.target)} — ${s.disabled ? t("mcp.disabled") : s.autoApprove ? t("mcp.auto_approved") : t("mcp.approval_required")}`)), "info", true);
    },
  },
  {
    name: "skills",
    args: "[show <name>|search <q>|install <registry>/<name> [--project]|registry|update [focus]]",
    group: "extensions",
    describe: "help.skills",
    async run(c) {
      const [sub = "list", ...rest] = words(c.args);
      const arg = rest.join(" ");
      if (sub === "update") return c.run(c.line.trim(), { command: true });
      // Registries (SK-96): managed in Settings › Library › Skills, installed from here too.
      if (sub === "registry" || sub === "registries") return c.openSettings("skills");
      if (sub === "install") {
        const ref = rest.find((w) => w !== "--project");
        if (!ref) return c.say(t("skills.install_usage"), "error");
        const r = await config.installRegistrySkill({ ref, workspace: rest.includes("--project") ? c.dir : "" });
        return c.say(t("skills.installed_from", { name: r.skill?.name ?? ref, version: r.skill?.version ?? "", registry: r.skill?.registry ?? "" }));
      }
      if (sub === "show") {
        if (!arg) return c.say(t("skills.show_usage"), "error");
        const s = (await workspaces.getSkill({ workspace: c.dir, name: arg })).skill;
        if (!s) return c.say(t("skills.not_found", { name: arg }), "error");
        const tools = s.tools.map((x) => code(x.name)).join(", ");
        return c.say(`**${md(s.name)}**: ${md(s.description)}${tools ? `\n\n${tools}` : ""}`, "info", true);
      }
      if (sub !== "list" && sub !== "search") return c.usage();
      const all = (await workspaces.listSkills({ workspace: c.dir, query: sub === "search" ? arg : "" })).skills;
      const title = sub === "search" ? t("skills.search_results", { query: arg, count: all.length }) : t("skills.discovered", { count: all.length });
      let text = bullets(title, all.map((s) => `**${md(s.name)}**: ${md(s.description)}`));
      // A search also looks in the user's registries, when there are any.
      if (sub === "search" && (await config.listSkillRegistries({})).registries.length) {
        const r = await config.searchRegistrySkills({ query: arg });
        text += "\n\n" + bullets(t("skills.registry_hits", { count: r.skills.length }), r.skills.map((s) => `**${md(s.registry)}/${md(s.name)}** ${md(s.version)}: ${md(s.description)}`));
        if (r.skills.length) text += `\n\n${t("skills.install_hint")}`;
        if (r.problems.length) text += `\n\n${r.problems.map(md).join("\n")}`;
      }
      c.say(text, "info", true);
    },
  },
  {
    name: "tools",
    group: "extensions",
    describe: "help.tools",
    async run(c) {
      const r = await workspaces.listTools({ workspace: c.dir });
      c.say(bullets(t("tools.title", { agent: r.agent }), r.tools.map((x) => `${x.planAllowed ? "● " : ""}${code(x.name)}: ${md(x.description.split("\n")[0])}`)) + `\n\n${t("tools.legend")}`, "info", true);
    },
  },

  // Settings
  {
    name: "config",
    args: "[key=value [--save]]",
    group: "settings",
    describe: "help.set",
    async run(c) {
      if (!c.args) return c.openSettings("file", c.dir);
      const w = words(c.args);
      const save = w.includes("--save");
      const pair = w.filter((x) => x !== "--save").join(" ");
      const i = pair.indexOf("=");
      if (i < 1) return c.usage();
      const key = pair.slice(0, i).trim(),
        value = pair.slice(i + 1).trim();
      if (save) {
        const r = await config.setConfigValue({ workspace: "", key, value });
        c.say(t("desktop.cmd.config_saved", { key, value, path: r.change?.path ?? "" }));
      } else {
        await workspaces.setSetting({ workspace: c.dir, key, value });
        c.say(t("desktop.cmd.config_set", { key, value }));
      }
      c.settingsChanged();
    },
  },
  {
    name: "docs",
    args: "[topic]",
    group: "settings",
    // The docs of this release, in the app (DSK-103): where they were left, or the best match for the topic.
    async run(c) {
      if (!c.args) return openDocs({}); // where it was left
      const { pages } = await loadAppDocs();
      const hit = searchDocs([...pages.values()], c.args, 1)[0];
      if (!hit) return c.say(t("desktop.docs.none", { query: c.args }));
      openDocs({ page: hit.page.path });
    },
  },
  { name: "help", group: "settings", run: (c) => c.say(c.help(), "info", true) },
  {
    name: "license",
    args: "[full|third-party]",
    group: "settings",
    run(c) {
      const which = c.args === "full" || c.args === "third-party" ? c.args : c.args ? null : "notice";
      if (!which) return c.usage();
      showLicense({ which });
    },
  },
  {
    name: "locale",
    args: "[code]",
    group: "settings",
    describe: "desktop.cmd.locale",
    async run(c) {
      if (!c.args) return c.say(t("desktop.cmd.locale_is", { tag: c.settings?.locale || "—" }));
      const r = await workspaces.setLocale({ workspace: c.dir, input: c.args });
      c.say(t("desktop.cmd.locale_now", { name: r.nativeName || r.languageName, tag: r.tag }));
      c.settingsChanged();
    },
  },
  {
    name: "theme",
    args: "[system|light|dark]",
    group: "settings",
    describe: "desktop.cmd.theme",
    run(c) {
      if (!c.args) return c.say(t("desktop.cmd.theme_is", { theme: c.theme() }));
      if (c.args !== "system" && c.args !== "light" && c.args !== "dark") return c.usage();
      c.setTheme(c.args);
      c.say(t("desktop.cmd.theme_now", { theme: c.args }));
    },
  },
];

/** The settings a model has of its own, as key=value words (or that it has none). */
function settingsText(s?: { temperature?: number; maxTokens?: number; topP?: number; seed?: number; reasoningEffort?: string; thinkingBudget?: number }): string {
  if (!s) return t("msettings.provider_default");
  const parts = [
    ["temperature", s.temperature],
    ["max_tokens", s.maxTokens],
    ["top_p", s.topP],
    ["seed", s.seed],
    ["reasoning_effort", s.reasoningEffort],
    ["thinking_budget", s.thinkingBudget],
  ]
    .filter(([, v]) => v !== undefined && v !== "")
    .map(([key, v]) => `${key}=${v}`);
  return parts.length ? code(parts.join(" ")) : t("msettings.provider_default");
}

/** A size in bytes, as the terminal writes it. */
function bytes(n: number | bigint): string {
  let v = Number(n);
  for (const unit of ["B", "KB", "MB", "GB"]) {
    if (v < 1024 || unit === "GB") return `${unit === "B" ? v : v.toFixed(1)} ${unit}`;
    v /= 1024;
  }
  return `${v} B`;
}

/** The built-ins by group, in /help's order, each sorted by name. */
export function builtinsInOrder(): Builtin[] {
  return [...builtinCommands].sort((a, b) => commandGroups.indexOf(a.group) - commandGroups.indexOf(b.group) || a.name.localeCompare(b.name));
}

/** The built-in named name, if there is one. */
export function builtin(name: string): Builtin | undefined {
  return builtinCommands.find((b) => b.name === name);
}
