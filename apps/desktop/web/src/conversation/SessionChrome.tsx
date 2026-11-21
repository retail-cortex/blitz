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

import { useEffect, useState } from "react";
import { mdiAlertCircleOutline, mdiArrowCollapse, mdiArrowExpand, mdiDeleteOutline, mdiPencilOutline, mdiPlus } from "@mdi/js";
import { Button, Dialog, Icon, IconButton } from "@blitz/webui";
import { workspaces } from "../api";
import { setupCommand } from "../events";
import type { SessionInfo } from "../gen/blitz/v1/session_pb";
import type { GetSuggestionsResponse } from "../gen/blitz/v1/workspace_pb";
import { chatTitle, HistoryMenu } from "../HistoryMenu";
import { t, tn } from "../i18n";
import { pollMs, shouldPoll, welcomeTiles, type TileAction } from "../suggestions";

/** The chat's bar: its title (renamed in place), History, New chat and, when set, expand. */
export function SessionBar({
  session,
  list,
  running,
  onNew,
  onLoad,
  onRename,
  onDelete,
  expand,
}: {
  session?: SessionInfo;
  list: SessionInfo[];
  running: boolean;
  onNew: () => void;
  onLoad: (id: string) => void;
  onRename: (t: string) => void;
  onDelete: (s: SessionInfo[]) => void;
  expand?: { expanded: boolean; toggle: () => void };
}) {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState("");
  const untitled = session?.title ? "" : t("desktop.chat.new");
  const commit = () => {
    setEditing(false);
    const next = title.trim();
    if (next && next !== session?.title) onRename(next);
  };
  return (
    <div className="session-bar chat-column">
      {editing ? (
        <input
          className="input session-title-input"
          autoFocus
          value={title}
          maxLength={80}
          onChange={(e) => setTitle(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") commit();
            if (e.key === "Escape") setEditing(false);
          }}
        />
      ) : (
        <button
          className="session-title"
          title={t("desktop.chat.rename")}
          onClick={() => {
            setTitle(session?.title ?? "");
            setEditing(true);
          }}
        >
          <span className="t-title ellipsis">{session?.title || untitled}</span>
          <Icon path={mdiPencilOutline} size="sm" className="title-edit" />
        </button>
      )}
      <span className="spacer" />
      {expand && (
        <IconButton
          icon={expand.expanded ? mdiArrowCollapse : mdiArrowExpand}
          label={t(expand.expanded ? "desktop.chat.shrink" : "desktop.chat.expand")}
          selected={expand.expanded}
          onClick={expand.toggle}
        />
      )}
      <HistoryMenu list={list} current={session?.id} disabled={running} onLoad={onLoad} onDelete={onDelete} />
      <IconButton icon={mdiPlus} label={t("desktop.chat.new")} variant="filled" onClick={onNew} disabled={running} />
    </div>
  );
}

/**
 * The welcome screen: the workspace's suggestions (GetSuggestions), asked
 * for again while a model writes ideas from the recent conversations, or
 * the canned tiles when there are none.
 */
export function EmptyState({ dir, name, onDraft, onSend, onOpen }: { dir: string; name: string; onDraft: (t: string) => void; onSend: (t: string) => void; onOpen: (id: string) => void }) {
  const [res, setRes] = useState<{ r?: GetSuggestionsResponse; learning: boolean } | null>(null);
  useEffect(() => {
    let attempts = 0;
    let timer = 0;
    let gone = false;
    const ask = async () => {
      let r: GetSuggestionsResponse | undefined;
      try {
        r = await workspaces.getSuggestions({ workspace: dir });
      } catch {
        // the canned tiles, or what came before, will do
        if (!gone) setRes((was) => ({ r: was?.r, learning: false }));
        return;
      }
      if (gone) return;
      const again = shouldPoll(r, attempts);
      setRes({ r, learning: again });
      if (again) {
        attempts++;
        timer = window.setTimeout(ask, pollMs);
      }
    };
    setRes(null);
    void ask();
    return () => {
      gone = true;
      window.clearTimeout(timer);
    };
  }, [dir]);
  const choose = (a: TileAction) => {
    if (a.type === "setup") onSend(setupCommand);
    else if (a.type === "session") onOpen(a.id);
    else if (a.type === "send") onSend(a.prompt);
    else onDraft(a.text);
  };
  return (
    <div className="empty">
      <h2 className="t-display gradient-text">{t("desktop.empty.title", { name })}</h2>
      {res && (
        <div className="suggestions">
          {welcomeTiles(res.r).map((s) => (
            <button key={s.key} className={`suggestion ${s.highlight ? "highlight" : ""}`} title={s.detail} onClick={() => choose(s.action)}>
              <Icon path={s.icon} />
              <span>{s.text}</span>
              {s.highlight && s.detail && <span className="t-body-sm suggestion-detail">{s.detail}</span>}
            </button>
          ))}
        </div>
      )}
      {res?.learning && <p className="t-body-sm muted suggestions-learning">{t("desktop.suggest.learning")}</p>}
    </div>
  );
}

/** Confirmation dialog for deleting one or more saved chat sessions. */
export function DeleteSessionsDialog({
  deleting,
  onCancel,
  onConfirm,
}: {
  deleting: SessionInfo[];
  onCancel: () => void;
  onConfirm: (list: SessionInfo[]) => void;
}) {
  return (
    <Dialog
      title={deleting.length === 1 ? t("desktop.chat.delete_title", { title: chatTitle(deleting[0]) }) : tn("desktop.chat.delete_n_title", deleting.length)}
      icon={mdiDeleteOutline}
      onClose={onCancel}
      footer={
        <>
          <Button onClick={onCancel}>{t("desktop.cancel")}</Button>
          <Button variant="filled" danger onClick={() => onConfirm(deleting)}>
            {deleting.length === 1 ? t("desktop.chat.delete") : tn("desktop.chat.delete_n", deleting.length)}
          </Button>
        </>
      }
    >
      <p>{deleting.length === 1 ? t("desktop.chat.delete_body") : tn("desktop.chat.delete_n_body", deleting.length)}</p>
      {deleting.length > 1 && (
        <ul className="delete-list t-body-sm">
          {deleting.map((s) => (
            <li key={s.id}>{chatTitle(s)}</li>
          ))}
        </ul>
      )}
    </Dialog>
  );
}

/** Confirmation dialog when rewinding files hits an undo conflict on disk. */
export function ForceRewindDialog({
  conflict,
  onCancel,
  onForce,
}: {
  conflict: { index: number; mode: string; error: string };
  onCancel: () => void;
  onForce: (index: number, mode: string) => void;
}) {
  return (
    <Dialog
      title={t("desktop.conflict.title")}
      icon={mdiAlertCircleOutline}
      onClose={onCancel}
      footer={
        <>
          <Button onClick={onCancel}>{t("desktop.conflict.keep")}</Button>
          <Button variant="filled" danger onClick={() => onForce(conflict.index, conflict.mode)}>
            {t("desktop.overwrite")}
          </Button>
        </>
      }
    >
      <p className="muted">{conflict.error}</p>
      <p className="muted">{t("desktop.rewind.overwrite_body")}</p>
    </Dialog>
  );
}
