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

// What a conversation shows, built from saved messages and a turn's
// events. Pure functions, so the rules (streamed text shown once, tool
// results matched to their calls) are tested without a browser.
import type { JsonObject } from "@bufbuild/protobuf";
import type { Message } from "./gen/blitz/v1/session_pb";
import type { Task, TurnEvent } from "./gen/blitz/v1/turn_pb";

/**
 * A user message: a prompt (index is its place in the transcript, once
 * known: what /rewind takes), or a message of another kind — sent while
 * the agent worked (steer), a stop hook's request (hook), or the go-ahead
 * for an approved plan (plan).
 */
export type UserEntry = { kind: "user"; text: string; sub?: "steer" | "hook" | "plan" | "aside"; index?: number; images?: { url: string; name: string }[] };

export type Entry =
  | UserEntry
  | { kind: "model"; text: string; author: string; open: boolean }
  | { kind: "thought"; text: string; open: boolean }
  | { kind: "tool"; id: string; name: string; args?: JsonObject; result?: JsonObject }
  | { kind: "notice"; text: string; tone: "info" | "error"; markdown?: boolean };

const subKinds: Record<string, UserEntry["sub"]> = { steer: "steer", hook: "hook", plan: "plan" };

/** The entries for a saved session's messages. */
export function fromMessages(messages: Message[]): Entry[] {
  return messages.map((m, i): Entry => {
    if (m.role !== "user") return { kind: "model", text: m.text, author: "", open: false };
    const sub = subKinds[m.kind];
    return sub ? { kind: "user", text: m.text, sub } : { kind: "user", text: m.text, index: i };
  });
}

/**
 * Gives each prompt on screen its index in the transcript: the n-th
 * prompt shown is the n-th prompt saved. Prompts shown before a reload
 * (sent in this window) learn theirs this way.
 */
export function assignPromptIndices(entries: Entry[], messages: Message[]): Entry[] {
  const indices: number[] = [];
  messages.forEach((m, i) => m.role === "user" && !m.kind && indices.push(i));
  let n = 0;
  let changed = false;
  const out = entries.map((e) => {
    if (e.kind !== "user" || e.sub) return e;
    const index = indices[n++];
    if (index === e.index) return e;
    changed = true;
    return { ...e, index };
  });
  return changed ? out : entries;
}

/**
 * Adds a turn event to the entries (returning new ones). Streamed text
 * grows the open model entry; the final event that repeats it closes it;
 * final text that wasn't streamed is an entry of its own. Thoughts become
 * thought entries the same way. Streamed copies of tool calls are left
 * out. A tool result fills in the call it answers.
 */
export function applyEvent(entries: Entry[], ev: TurnEvent): Entry[] {
  const k = ev.kind;
  switch (k.case) {
    case "text": {
      const t = k.value;
      const kind = t.thought ? "thought" : "model";
      const last = entries[entries.length - 1];
      const open = last?.kind === kind && last.open ? last : undefined;
      const entry = (text: string, isOpen: boolean): Entry =>
        kind === "thought" ? { kind, text, open: isOpen } : { kind, text, author: ev.author, open: isOpen };
      if (t.partial) {
        if (open) return [...entries.slice(0, -1), { ...open, text: open.text + t.text }];
        return [...close(entries), entry(t.text, true)];
      }
      if (t.repeat) {
        return open ? [...entries.slice(0, -1), { ...open, open: false }] : entries;
      }
      if (open) {
        // A final thought that repeats what streamed, without the repeat mark.
        if (kind === "thought" && t.text === open.text) return [...entries.slice(0, -1), { ...open, open: false }];
        return [...entries.slice(0, -1), { ...open, open: false }, entry(t.text, false)];
      }
      if (!t.text.trim() && kind === "model") {
        // A separator between runs of one turn: add it to the last answer.
        const lastModel = last?.kind === "model" ? last : undefined;
        return lastModel ? [...entries.slice(0, -1), { ...lastModel, text: lastModel.text + t.text }] : entries;
      }
      return [...close(entries), entry(t.text, false)];
    }
    case "toolCall": {
      if (k.value.partial) return entries;
      return [...close(entries), { kind: "tool", id: k.value.id, name: k.value.name, args: k.value.args }];
    }
    case "toolResult": {
      const r = k.value;
      for (let i = entries.length - 1; i >= 0; i--) {
        const e = entries[i];
        if (e.kind === "tool" && !e.result && (e.id === r.id || (!r.id && e.name === r.name))) {
          const next = entries.slice();
          next[i] = { ...e, result: r.result ?? {} };
          return next;
        }
      }
      return [...entries, { kind: "tool", id: r.id, name: r.name, result: r.result ?? {} }];
    }
    case "finished": {
      const err = k.value.error;
      const done = close(entries);
      return err ? [...done, { kind: "notice", text: err.message || err.reason, tone: "error" }] : done;
    }
    default:
      return entries;
  }
}

function close(entries: Entry[]): Entry[] {
  const last = entries[entries.length - 1];
  return (last?.kind === "model" || last?.kind === "thought") && last.open ? [...entries.slice(0, -1), { ...last, open: false }] : entries;
}

/** The task list a turn event carries, if it carries one. */
export function tasksOf(ev: TurnEvent): Task[] | undefined {
  return ev.kind.case === "tasks" ? ev.kind.value.items : undefined;
}

/** Whether a tool result reports failure (the tools return an "error" field). */
export function failed(result: JsonObject | undefined): boolean {
  return !!result && typeof result.error === "string" && result.error !== "";
}

/** A one-line summary of a tool call's arguments. */
export function summarizeArgs(args: JsonObject | undefined): string {
  if (!args) return "";
  for (const key of ["path", "command", "url", "query", "question", "pattern", "agent", "reason"]) {
    const v = args[key];
    if (typeof v === "string" && v) return v.length > 80 ? v.slice(0, 79) + "…" : v;
  }
  return "";
}

/** One file's part of a unified diff. */
export interface FileDiff {
  path: string;
  added: number;
  removed: number;
  lines: { kind: "add" | "del" | "hunk" | "ctx" | "meta"; text: string }[];
}

/** Splits a unified diff into files, counting what each adds and removes. */
export function parseDiff(diff: string): FileDiff[] {
  const files: FileDiff[] = [];
  let cur: FileDiff | undefined;
  const lines = diff.replace(/\n$/, "").split("\n");
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (line.startsWith("--- ") && lines[i + 1]?.startsWith("+++ ")) {
      const to = lines[i + 1].slice(4).replace(/^b\//, "").split("\t")[0];
      const from = line.slice(4).replace(/^a\//, "").split("\t")[0];
      cur = { path: to === "/dev/null" ? from : to, added: 0, removed: 0, lines: [] };
      files.push(cur);
      i++;
      continue;
    }
    if (line.startsWith("diff --git") || line.startsWith("index ") || line.startsWith("# ")) {
      if (line.startsWith("# ")) files.push({ path: line.slice(2).split(":")[0], added: 0, removed: 0, lines: [{ kind: "meta", text: line.slice(2) }] });
      continue;
    }
    if (!cur) continue;
    if (line.startsWith("@@")) cur.lines.push({ kind: "hunk", text: line });
    else if (line.startsWith("+")) {
      cur.added++;
      cur.lines.push({ kind: "add", text: line });
    } else if (line.startsWith("-")) {
      cur.removed++;
      cur.lines.push({ kind: "del", text: line });
    } else cur.lines.push({ kind: "ctx", text: line });
  }
  return files;
}
