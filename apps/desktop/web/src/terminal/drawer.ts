/*
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

// Each workspace's terminals and the drop-down drawer that shows them
// (spec_062_terminal TRM-20, TRM-26, TRM-30): one place, shared by the
// drawer, the status bar's terminal button and whatever opens a terminal.
// Closing the drawer only hides it: the terminals go on.

import { useSyncExternalStore } from "react";
import { terminals } from "../api";
import { termName } from "./terminals";

/** A terminal the service runs for a workspace. */
export interface TermEntry {
  id: string;
  /** Its folder, relative to the workspace ("" for it). */
  dir: string;
  /** The shell it runs (/bin/zsh), as the service says. */
  shell?: string;
  exited?: boolean;
  exitCode?: number;
}

/** A workspace's terminals: in the drawer's order, the one shown, and whether the drawer is down. */
export interface TermDrawer {
  list: TermEntry[];
  active: string;
  open: boolean;
}

const empty: TermDrawer = { list: [], active: "", open: false };

/** s with t added: shown, and the drawer down, unless show is false. */
export function added(s: TermDrawer, t: TermEntry, show = true): TermDrawer {
  const list = s.list.some((x) => x.id === t.id) ? s.list : [...s.list, t];
  return { list, active: show ? t.id : s.active || t.id, open: show ? true : s.open };
}

/** s without terminal id: its neighbour shown, and the drawer up once none is left. */
export function removed(s: TermDrawer, id: string): TermDrawer {
  const at = s.list.findIndex((x) => x.id === id);
  if (at < 0) return s;
  const list = s.list.filter((x) => x.id !== id);
  const active = s.active === id ? (list[at] ?? list[at - 1])?.id ?? "" : s.active;
  return { list, active, open: list.length > 0 && s.open };
}

/** s with terminal old replaced by t in its place (a restart). */
export function replaced(s: TermDrawer, old: string, t: TermEntry): TermDrawer {
  if (!s.list.some((x) => x.id === old)) return added(s, t);
  return { list: s.list.map((x) => (x.id === old ? t : x)), active: s.active === old ? t.id : s.active, open: s.open };
}

/** The drawer down on terminal id (else the one shown last, else the first). */
export function shown(s: TermDrawer, id?: string): TermDrawer {
  const active = id && s.list.some((x) => x.id === id) ? id : s.active || s.list[0]?.id || "";
  return { ...s, active, open: s.list.length > 0 };
}

/** The drawer up; the terminals go on. */
export const hidden = (s: TermDrawer): TermDrawer => (s.open ? { ...s, open: false } : s);

/** s with what's new of terminal id (it exited, with its code). */
export function updated(s: TermDrawer, id: string, patch: Partial<TermEntry>): TermDrawer {
  return { ...s, list: s.list.map((x) => (x.id === id ? { ...x, ...patch } : x)) };
}

/** s with the service's terminals a reloaded window finds (TRM-26): in the drawer, which stays up. */
export function restored(s: TermDrawer, found: TermEntry[]): TermDrawer {
  const list = [...s.list, ...found.filter((f) => !s.list.some((x) => x.id === f.id))];
  return { list, active: s.active || list[0]?.id || "", open: s.open };
}

// The store: one TermDrawer a workspace, and who's listening.
const drawers = new Map<string, TermDrawer>();
const listeners = new Set<() => void>();
const subscribe = (f: () => void) => {
  listeners.add(f);
  return () => void listeners.delete(f);
};
const get = (ws: string) => drawers.get(ws) ?? empty;
function set(ws: string, f: (s: TermDrawer) => TermDrawer) {
  const before = get(ws);
  const after = f(before);
  if (after === before) return;
  drawers.set(ws, after);
  for (const l of listeners) l();
}

/** A workspace's terminals and drawer, as they change. */
export const useTermDrawer = (ws: string): TermDrawer => useSyncExternalStore(subscribe, () => get(ws));

/** A workspace's terminals and drawer now. */
export const termDrawer = get;

/** Forgets every workspace's terminals (the tests'). */
export function resetTermDrawers() {
  drawers.clear();
  restoredFor.clear();
  for (const l of listeners) l();
}

/** Opens a terminal in a folder of the workspace ("" for it) and drops the drawer down on it (TRM-30, TRM-31). */
export async function newTerminal(ws: string, folder: string): Promise<TermEntry> {
  const { terminal } = await terminals.openTerminal({ workspace: ws, dir: folder });
  const t: TermEntry = { id: terminal!.id, dir: terminal!.dir, shell: terminal!.shell };
  set(ws, (s) => added(s, t));
  return t;
}

/** Drops the drawer down on terminal id, or the one shown last. */
export const showTerminals = (ws: string, id?: string) => set(ws, (s) => shown(s, id));

/** Puts the drawer up; the terminals go on. */
export const hideTerminals = (ws: string) => set(ws, hidden);

/** ⌃` (TRM-30b): the drawer up, or down, with a new terminal when there's none. */
export async function toggleTerminals(ws: string): Promise<void> {
  const s = get(ws);
  if (s.open) hideTerminals(ws);
  else if (s.list.length) showTerminals(ws);
  else await newTerminal(ws, "");
}

/** Ends terminal id: its tab goes, then its shell. */
export async function closeTerminal(ws: string, id: string): Promise<void> {
  set(ws, (s) => removed(s, id));
  await terminals.closeTerminal({ workspace: ws, id }).catch(() => {}); // gone already: nothing to do
}

/** A new shell in the folder of terminal id, in its place (Restart, after it exited). */
export async function restartTerminal(ws: string, id: string): Promise<void> {
  const old = get(ws).list.find((x) => x.id === id);
  if (!old) return;
  const { terminal } = await terminals.openTerminal({ workspace: ws, dir: old.dir });
  set(ws, (s) => replaced(s, id, { id: terminal!.id, dir: terminal!.dir, shell: terminal!.shell }));
  await terminals.closeTerminal({ workspace: ws, id }).catch(() => {});
}

/** Terminal id's shell exited with code. */
export const terminalExited = (ws: string, id: string, code: number) => set(ws, (s) => updated(s, id, { exited: true, exitCode: code }));

const restoredFor = new Set<string>();

/** The workspace's terminals the service still runs, once a window (TRM-26): in the drawer, which stays up. */
export async function restoreTerminals(ws: string): Promise<void> {
  if (restoredFor.has(ws)) return;
  restoredFor.add(ws);
  try {
    const { terminals: list } = await terminals.listTerminals({ workspace: ws });
    set(ws, (s) => restored(s, list.map((x) => ({ id: x.id, dir: x.dir, shell: x.shell, exited: x.exited || undefined, exitCode: x.exited ? x.exitCode : undefined }))));
  } catch {
    // no terminals (an older service): none to show
  }
}

/** A terminal's name in a list: its folder's, and its shell's (zsh), or how it ended. */
export function termDetail(t: TermEntry, ended: (code: number) => string): string {
  const shell = (t.shell ?? "").split("/").filter(Boolean).pop() ?? "";
  return [t.dir || ".", t.exited ? ended(t.exitCode ?? 0) : shell].filter(Boolean).join(" · ");
}

/** The drawer's height as the user left it (0: the default), within what the window has room for. */
export function drawerHeight(stored: number, room: number): number {
  if (!stored || stored < 160) return 0;
  return Math.min(stored, Math.max(160, room));
}

/** A workspace's terminals as the status bar's menu lists them (TRM-30a): name, folder and shell, the one shown marked. */
export function termChoices(s: TermDrawer, ws: string, ended: (code: number) => string): { id: string; label: string; detail: string; current: boolean }[] {
  return s.list.map((x) => ({ id: x.id, label: termName(x.dir, ws), detail: termDetail(x, ended), current: s.open && x.id === s.active }));
}

/** ⌃` (Control-backtick, on every system): the key that puts the drawer down or up (TRM-30b). */
export const isToggleTerminalKey = (e: Pick<KeyboardEvent, "key" | "code" | "ctrlKey" | "metaKey" | "altKey">) =>
  e.ctrlKey && !e.metaKey && !e.altKey && (e.key === "`" || e.code === "Backquote");
