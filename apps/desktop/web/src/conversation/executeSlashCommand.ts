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

import type { MutableRefObject } from "react";
import { serviceLost } from "../api";
import type { Attachment, WebBlock } from "../attachments";
import { helpText, knownCommand, parseCommand, type CommandSpec } from "../commands";
import { message, isUnavailable } from "../errors";
import { openSettings, searchWorkspace } from "../events";
import type { Usage } from "../gen/blitz/v1/turn_pb";
import type { GetSettingsResponse } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";
import type { Prefs } from "../prefs";
import { builtin, type CommandContext } from "../slashCommands";
import type { Entry } from "../turns";

/** How a turn is sent (see Turn in the API). */
export interface TurnOptions {
  accepted?: boolean;
  plan?: boolean;
  aside?: boolean;
  command?: boolean;
  readOnly?: string;
  prompt?: string;
  fetchGrants?: string[];
  images?: Attachment[];
  /** Content added from web pages (EL-104), sent as the turn's context. */
  context?: WebBlock[];
  /** The prompt as shown, when it differs from the text sent. */
  shown?: string;
  /** Follow this background run instead of sending text (blitz --bg). */
  follow?: string;
}

/** Dependencies required to execute a slash command in the conversation. */
export interface SlashCommandEnv {
  dir: string;
  commands: CommandSpec[];
  running: boolean;
  latestRunning: MutableRefObject<boolean>;
  settings?: GetSettingsResponse;
  sessionId?: string;
  sessionTitle?: string;
  projectWaiting: number;
  entries: Entry[];
  prefs: Prefs;
  updatePrefs: (fn: (p: Prefs) => Prefs) => void;
  say: (text: string, tone?: "info" | "error", markdown?: boolean) => void;
  run: (text: string, opts?: TurnOptions) => Promise<void>;
  sendRef: MutableRefObject<(text: string) => void>;
  snack: (text: string, opts?: { error?: boolean }) => void;
  onSettingsChanged: () => void;
  onOpenView?: CommandContext["openView"];
  onReviewProject?: () => void;
  reload: () => Promise<void>;
  refreshTotal: () => Promise<void>;
  refreshList: () => Promise<void>;
  newSession: () => Promise<void>;
  rename: (title: string) => Promise<void>;
  rewind: (index: number, mode: string, force?: boolean) => Promise<void>;
  addImagePath: (path: string) => void;
  clearAttachments: () => number;
}

/**
 * Runs a slash command; false when the line isn't one (a path, say) and
 * should go to the agent. Unknown commands are refused, never sent.
 */
export async function executeSlashCommand(line: string, env: SlashCommandEnv): Promise<boolean> {
  const c = parseCommand(line);
  if (!c) return false;
  const spec = env.commands.find((x) => x.name === c.name);
  if (!spec) {
    // One of the engine's own, before the service's list has come.
    if (knownCommand(c.name, env.commands)) {
      if (env.running) env.say(t("desktop.cmd.wait", { name: c.name }), "error");
      else void env.run(line.trim(), { command: true });
    } else env.say(t("desktop.cmd.unknown", { name: c.name }), "error");
    return true;
  }
  const own = spec.source === "builtin" ? builtin(c.name) : undefined;
  const startsTurn = spec.source !== "builtin" || !!own?.turn;
  if (env.running && startsTurn) {
    env.say(t("desktop.cmd.wait", { name: c.name }), "error");
    return true;
  }
  if (!own) {
    void env.run(line.trim(), { command: true });
    return true;
  }
  const ctx: CommandContext = {
    dir: env.dir,
    args: c.args,
    line,
    settings: env.settings,
    sessionId: env.sessionId,
    sessionTitle: env.sessionTitle,
    projectWaiting: env.projectWaiting,
    say: env.say,
    run: (text, opts) => void env.run(text, opts),
    send: (text) => env.sendRef.current(text),
    snack: env.snack,
    usage: () => env.say(t("desktop.cmd.usage", { usage: `/${spec.name}${spec.args ? " " + spec.args : ""}` }), "error"),
    busy: () => env.latestRunning.current,
    settingsChanged: env.onSettingsChanged,
    openView: env.onOpenView ?? (() => env.say(t("desktop.cmd.no_view"), "error")),
    openSearch: searchWorkspace,
    openSettings: (section, scope, connect) => openSettings({ section, scope, connect }),
    reviewProject: env.onReviewProject,
    reload: env.reload,
    refreshTotal: env.refreshTotal,
    refreshList: env.refreshList,
    newSession: env.newSession,
    rename: env.rename,
    rewind: env.rewind,
    addImagePath: env.addImagePath,
    clearAttachments: env.clearAttachments,
    answers: () => env.entries.filter((e): e is Extract<Entry, { kind: "model" }> => e.kind === "model" && !!e.text.trim()).map((e) => e.text),
    theme: () => env.prefs.theme,
    setTheme: (theme) => env.updatePrefs((p) => ({ ...p, theme })),
    help: () => helpText(env.commands),
  };
  try {
    await own.run(ctx);
  } catch (e) {
    if (isUnavailable(e)) serviceLost();
    env.say(message(e), "error");
  }
  return true;
}

const k = (n: bigint) => (n >= 1_000_000n ? `${(Number(n) / 1e6).toFixed(1)}M` : n >= 1000n ? `${(Number(n) / 1000).toFixed(1)}k` : String(n));

/** Formats token and cost usage for the last completed turn. */
export function usageLine(before?: Usage, after?: Usage): string {
  if (!after || !before || after.calls === before.calls) return "";
  let s = t("desktop.usage.turn", { in: k(after.input - before.input), out: k(after.output - before.output), context: k(after.lastPrompt) });
  if (after.priced) s += ` · $${(after.costUsd - before.costUsd).toFixed(4)}`;
  return s;
}
