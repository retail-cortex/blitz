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

import { sessions } from "../api";
import { message, reason } from "../errors";
import type { SessionInfo } from "../gen/blitz/v1/session_pb";
import { chatTitle } from "../HistoryMenu";
import { t, tn } from "../i18n";

/** Manages session actions: new, load, delete, rename, rewind. */
export function useSessionOps({
  dir,
  show,
  reload,
  refreshList,
  refreshTotal,
  fail,
  snack,
  setDeleting,
  setForceRewind,
  setSession,
  setDraft,
}: {
  dir: string;
  show: (s: SessionInfo) => void;
  reload: () => Promise<void>;
  refreshList: () => Promise<void>;
  refreshTotal: () => Promise<void>;
  fail: (e: unknown) => void;
  snack: (msg: string, opts?: { error?: boolean }) => void;
  setDeleting: (l: SessionInfo[] | null) => void;
  setForceRewind: (fr: { index: number; mode: string; error: string } | null) => void;
  setSession: React.Dispatch<React.SetStateAction<SessionInfo | undefined>>;
  setDraft: (d: string) => void;
}) {
  const newSession = async () => {
    try {
      show((await sessions.newSession({ workspace: dir })).session!);
      await refreshList();
    } catch (e) {
      fail(e);
    }
  };

  const load = async (id: string) => {
    try {
      show((await sessions.loadSession({ workspace: dir, ref: id })).session!);
      await refreshList();
    } catch (e) {
      fail(e);
    }
  };

  const remove = async (list: SessionInfo[]) => {
    setDeleting(null);
    let done = 0;
    const failed: string[] = [];
    for (const s of list) {
      try {
        await sessions.deleteSession({ workspace: dir, sessionId: s.id });
        done++;
      } catch (e) {
        failed.push(`${chatTitle(s)}: ${message(e)}`);
      }
    }
    const deleted = done === 0 ? "" : done === 1 && list.length === 1 ? t("desktop.chat.deleted", { title: chatTitle(list[0]) }) : tn("desktop.chat.deleted_n", done);
    if (failed.length) snack([deleted, tn("desktop.chat.delete_failed", failed.length, { reason: failed.join("; ") })].filter(Boolean).join(". "), { error: true });
    else snack(deleted);
    await refreshList();
  };

  const rename = async (title: string) => {
    try {
      setSession((await sessions.renameSession({ workspace: dir, title })).session);
      await refreshList();
    } catch (e) {
      fail(e);
    }
  };

  const rewind = async (index: number, mode: string, force = false) => {
    try {
      const res = await sessions.rewind({ workspace: dir, index, mode, force });
      if (mode === "both" || mode === "conversation") {
        await reload();
        setDraft(res.prompt);
      }
      const files = res.restored.length ? tn("desktop.rewind.restored", res.restored.length) : "";
      if (mode.startsWith("summarize")) {
        const c = res.compacted;
        snack(t("desktop.compacted", { events: c?.eventsCompacted ?? 0, chars: c?.summaryChars ?? 0 }));
        void refreshTotal();
      } else snack(mode === "code" ? t("desktop.rewind.code_done", { files }) : t("desktop.rewind.done", { files }));
    } catch (e) {
      if (reason(e) === "UNDO_CONFLICT") setForceRewind({ index, mode, error: message(e) });
      else snack(message(e), { error: true });
    }
  };

  return { newSession, load, remove, rename, rewind };
}
