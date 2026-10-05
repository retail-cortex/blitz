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

// The chat history menu's rules: which saved sessions show for a search
// and a filter (chats or workers' runs), and which can be ticked to delete.
import type { SessionInfo } from "./gen/blitz/v1/session_pb";

/** What the history shows: every session, chats, or workers' runs. */
export type HistoryFilter = "all" | "chats" | "workers";

/** Whether a session is a worker's run (its origin, from the service). */
export const isWorkerRun = (s: Pick<SessionInfo, "origin">): boolean => s.origin === "worker";

/**
 * The sessions a filter and a search show, in the list's order. Every word
 * of the search must appear, in any case, in the title, the snapshot's
 * name or the agent.
 */
export function filterHistory<T extends Pick<SessionInfo, "title" | "snapshot" | "agent" | "origin">>(list: T[], filter: HistoryFilter, query: string): T[] {
  const words = query.toLocaleLowerCase().split(/\s+/).filter(Boolean);
  return list.filter((s) => {
    if (filter === "chats" && isWorkerRun(s)) return false;
    if (filter === "workers" && !isWorkerRun(s)) return false;
    const text = `${s.title} ${s.snapshot} ${s.agent}`.toLocaleLowerCase();
    return words.every((w) => text.includes(w));
  });
}

/** How many sessions each filter has, for its label. */
export function historyCounts(list: Pick<SessionInfo, "origin">[]): Record<HistoryFilter, number> {
  const workers = list.filter(isWorkerRun).length;
  return { all: list.length, chats: list.length - workers, workers };
}

/**
 * The ticked sessions still listed that can be deleted: never the chat in
 * view (start or load another first).
 */
export function deletable<T extends Pick<SessionInfo, "id">>(list: T[], ticked: ReadonlySet<string>, current?: string): T[] {
  return list.filter((s) => ticked.has(s.id) && s.id !== current);
}

/**
 * Ticking "all shown": every shown session but the current one is ticked,
 * or, when all of them already are, none of them.
 */
export function toggleAll(shown: Pick<SessionInfo, "id">[], ticked: ReadonlySet<string>, current?: string): Set<string> {
  const ids = shown.map((s) => s.id).filter((id) => id !== current);
  const next = new Set(ticked);
  const all = ids.length > 0 && ids.every((id) => ticked.has(id));
  for (const id of ids) {
    if (all) next.delete(id);
    else next.add(id);
  }
  return next;
}
