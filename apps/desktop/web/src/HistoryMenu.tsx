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

// The chat bar's History menu: the workspace's saved chats and workers'
// runs, searched and filtered, opened with a click, and deleted one at a
// time or ticked and deleted together. Rules in history.ts.
import { useLayoutEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { mdiDeleteOutline, mdiHistory, mdiMagnify } from "@mdi/js";
import type { SessionInfo } from "./gen/blitz/v1/session_pb";
import { deletable, filterHistory, historyCounts, toggleAll, type HistoryFilter } from "./history";
import { language, t, tn } from "./i18n";
import { Button, Icon, IconButton, Segmented, nudge, useAnchoredMenu } from "./ui/controls";

/** A session's name in the history: a snapshot's, else its title. */
export function chatTitle(s: SessionInfo): string {
  return s.snapshot ? `📸 ${s.snapshot}` : s.title || t("desktop.untitled");
}

/**
 * The History button and its menu. current is the chat in view, which
 * can't be ticked or deleted; onDelete asks to delete sessions (it
 * confirms).
 */
export function HistoryMenu({
  list,
  current,
  disabled,
  onLoad,
  onDelete,
}: {
  list: SessionInfo[];
  current?: string;
  disabled?: boolean;
  onLoad: (id: string) => void;
  onDelete: (s: SessionInfo[]) => void;
}) {
  const { at, toggle, close, ref, layer } = useAnchoredMenu<HTMLSpanElement>("down end");
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<HistoryFilter>("all");
  const [ticked, setTicked] = useState<ReadonlySet<string>>(new Set());
  const shown = useMemo(() => filterHistory(list, filter, query), [list, filter, query]);
  const counts = useMemo(() => historyCounts(list), [list]);
  const chosen = deletable(list, ticked, current);
  const shownTickable = shown.filter((s) => s.id !== current);
  const allTicked = shownTickable.length > 0 && shownTickable.every((s) => ticked.has(s.id));
  const someTicked = shownTickable.some((s) => ticked.has(s.id));

  // Kept in the window, as a menu is.
  useLayoutEffect(() => {
    const el = layer.current;
    if (!el || !at) return;
    const { x, y } = nudge(el.getBoundingClientRect(), window.innerWidth, window.innerHeight);
    el.style.transform = x || y ? `translate(${x}px, ${y}px)` : "";
  }, [at, layer]);

  const tick = (id: string, on: boolean) =>
    setTicked((cur) => {
      const next = new Set(cur);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  const remove = (s: SessionInfo[]) => {
    close();
    onDelete(s);
  };
  const when = (s: SessionInfo) => (s.updated ? ` · ${timestampDate(s.updated).toLocaleString(language(), { dateStyle: "medium", timeStyle: "short" })}` : "");

  return (
    <span className="menu-anchor" ref={ref}>
      <IconButton icon={mdiHistory} label={t("desktop.chat.history")} disabled={disabled} onClick={toggle} aria-expanded={!!at} aria-haspopup="dialog" />
      {at &&
        createPortal(
          <div className="menu floating history-menu" role="dialog" aria-label={t("desktop.chat.history")} style={at} ref={layer}>
            <div className="history-head">
              <label className="history-search">
                <Icon path={mdiMagnify} size="sm" />
                <input
                  className="input"
                  type="search"
                  autoFocus
                  placeholder={t("desktop.chat.search")}
                  aria-label={t("desktop.chat.search")}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                />
              </label>
              <Segmented<HistoryFilter>
                label={t("desktop.chat.filter")}
                small
                value={filter}
                onChange={setFilter}
                options={(["all", "chats", "workers"] as const).map((f) => ({ value: f, label: `${t(`desktop.chat.filter_${f}`)} ${counts[f]}` }))}
              />
            </div>
            {list.length === 0 ? (
              <div className="menu-label">{t("desktop.chat.none")}</div>
            ) : (
              <>
                <label className="history-all menu-label">
                  <input
                    type="checkbox"
                    checked={allTicked}
                    ref={(el) => {
                      if (el) el.indeterminate = someTicked && !allTicked;
                    }}
                    disabled={shownTickable.length === 0}
                    onChange={() => setTicked((cur) => toggleAll(shown, cur, current))}
                  />
                  {t("desktop.chat.select_all")}
                </label>
                <div className="history-list">
                  {shown.length === 0 && <p className="menu-label">{t("desktop.chat.no_match")}</p>}
                  {shown.map((s) => {
                    const here = s.id === current;
                    const title = chatTitle(s);
                    return (
                      <div key={s.id} className={`history-row ${here ? "on" : ""} ${ticked.has(s.id) && !here ? "ticked" : ""}`}>
                        <input
                          type="checkbox"
                          aria-label={t("desktop.chat.select", { title })}
                          title={here ? t("desktop.chat.in_view") : undefined}
                          checked={ticked.has(s.id) && !here}
                          disabled={here}
                          onChange={(e) => tick(s.id, e.target.checked)}
                        />
                        <button
                          type="button"
                          className={`menu-item ${here ? "on" : ""}`}
                          onClick={() => {
                            close();
                            onLoad(s.id);
                          }}
                          onKeyDown={(e) => {
                            if (!here && (e.key === "Delete" || e.key === "Backspace")) {
                              e.preventDefault();
                              remove([s]);
                            }
                          }}
                        >
                          <span>
                            {title}
                            <small>
                              {tn("desktop.messages", s.messageCount)}
                              {when(s)}
                            </small>
                          </span>
                        </button>
                        {!here && <IconButton className="history-delete" icon={mdiDeleteOutline} label={t("desktop.chat.delete")} small onClick={() => remove([s])} />}
                      </div>
                    );
                  })}
                </div>
              </>
            )}
            {chosen.length > 0 && (
              <div className="history-foot">
                <span className="t-body-sm">{tn("desktop.chat.selected", chosen.length)}</span>
                <span className="spacer" />
                <Button small onClick={() => setTicked(new Set())}>
                  {t("desktop.chat.clear")}
                </Button>
                <Button small variant="filled" danger icon={mdiDeleteOutline} onClick={() => remove(chosen)}>
                  {tn("desktop.chat.delete_n", chosen.length)}
                </Button>
              </div>
            )}
          </div>,
          document.body,
        )}
    </span>
  );
}
