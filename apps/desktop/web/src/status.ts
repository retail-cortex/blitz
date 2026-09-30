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


// What the status bar shows about each workspace, published by the parts
// of the page that know it: the workspace its settings and view, the
// conversation its usage, the editor its cursor.
import { useSyncExternalStore } from "react";
import type { Usage } from "./gen/blitz/v1/turn_pb";
import type { GetSettingsResponse } from "./gen/blitz/v1/workspace_pb";

/** Where the editor's cursor is, in the file shown. */
export interface Cursor {
  line: number;
  column: number;
  /** Characters selected (0: none). */
  selected: number;
  /** The file's language, as CodeMirror names it ("" if none). */
  language: string;
}

/** A workspace's state for the status bar. */
export interface WorkspaceStatus {
  settings?: GetSettingsResponse;
  /** The session's usage so far. */
  total?: Usage;
  /** The last turn's usage, a line ("" before one ends). */
  turn?: string;
  /** The context is compacted past threshold tokens (autoCompact). */
  threshold?: number;
  autoCompact?: boolean;
  cursor?: Cursor;
}

const empty: WorkspaceStatus = {};
let all: Record<string, WorkspaceStatus> = {};
const listeners = new Set<() => void>();

/** Updates what the status bar shows about dir (undefined fields are cleared). */
export function publishStatus(dir: string, patch: Partial<WorkspaceStatus>) {
  const next = { ...(all[dir] ?? empty), ...patch };
  all = { ...all, [dir]: next };
  for (const f of listeners) f();
}

/** Forgets a workspace (closed). */
export function forgetStatus(dir: string) {
  if (!(dir in all)) return;
  const { [dir]: _, ...rest } = all;
  all = rest;
  for (const f of listeners) f();
}

function subscribe(f: () => void) {
  listeners.add(f);
  return () => listeners.delete(f);
}

/** What the status bar shows about dir, now. */
export function statusOf(dir: string): WorkspaceStatus {
  return all[dir] ?? empty;
}

/** What the status bar shows about dir. */
export function useWorkspaceStatus(dir: string): WorkspaceStatus {
  return useSyncExternalStore(subscribe, () => statusOf(dir));
}

/** A token count, short: 950, 12.3k, 1.2M. */
export function tokens(n: bigint | number): string {
  const v = Number(n);
  return v >= 1e6 ? `${(v / 1e6).toFixed(1)}M` : v >= 1000 ? `${(v / 1000).toFixed(1)}k` : String(v);
}

/**
 * How full the context is against the compaction threshold, 0 to 1
 * (undefined without a threshold or a prompt yet).
 */
export function contextShare(lastPrompt: bigint | number, threshold?: number): number | undefined {
  if (!threshold || threshold <= 0 || !lastPrompt) return undefined;
  return Math.min(1, Number(lastPrompt) / threshold);
}
