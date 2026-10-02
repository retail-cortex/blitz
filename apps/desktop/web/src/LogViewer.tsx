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
import { createPortal } from "react-dom";
import { mdiDeleteOutline, mdiFolderOpenOutline, mdiRefresh } from "@mdi/js";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { workspaces } from "./api";
import { inApp, openFolder } from "./desktop";
import { message } from "./errors";
import type { LogEntry, ReadLogResponse } from "./gen/blitz/v1/workspace_pb";
import { t } from "./i18n";
import { canDelete, logTime } from "./logs";
import { Button, Dialog, IconButton, Segmented, useSnackbar } from "./ui/controls";

type Level = "" | "info" | "warn" | "error";

/**
 * The service's log (Settings › Logs): a day's records, newest first, by
 * level and text, and the folder they're in; a past day's can be deleted. What an error the user saw
 * said, and around it, in a few seconds.
 */
export function LogViewer() {
  const snack = useSnackbar();
  const [days, setDays] = useState<string[]>([]);
  const [dir, setDir] = useState("");
  const [day, setDay] = useState("");
  const [level, setLevel] = useState<Level>("info");
  const [text, setText] = useState("");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState<ReadLogResponse>();
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [tick, setTick] = useState(0);
  const [deleting, setDeleting] = useState("");

  const remove = async (d: string) => {
    setDeleting("");
    try {
      await workspaces.deleteLogDay({ day: d });
      snack(t("desktop.logs.deleted", { day: d }));
      setDay("");
      setTick((n) => n + 1);
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  useEffect(() => {
    workspaces.listLogDays({}).then(
      (r) => {
        setDays(r.days);
        setDir(r.dir);
        setLoaded(true);
      },
      (e) => setError(message(e)),
    );
  }, [tick]);

  // The search runs once typing pauses.
  useEffect(() => {
    const timer = setTimeout(() => setQuery(text), 250);
    return () => clearTimeout(timer);
  }, [text]);

  useEffect(() => {
    if (!loaded || !dir) return;
    let live = true;
    workspaces.readLog({ day, minLevel: level, text: query }).then(
      (r) => {
        if (!live) return;
        setPage(r);
        setError("");
      },
      (e) => live && setError(message(e)),
    );
    return () => {
      live = false;
    };
  }, [loaded, dir, day, level, query, tick]);

  if (loaded && !dir) return <p className="muted">{t("desktop.logs.off")}</p>;
  const levels: { value: Level; label: string }[] = [
    { value: "", label: t("desktop.logs.all") },
    { value: "info", label: t("desktop.logs.info") },
    { value: "warn", label: t("desktop.logs.warn") },
    { value: "error", label: t("desktop.logs.error") },
  ];
  return (
    <div className="stack log-viewer" style={{ gap: 12 }}>
      <div className="row log-filters" style={{ gap: 8, flexWrap: "wrap" }}>
        <select className="select" value={day} onChange={(e) => setDay(e.target.value)} aria-label={t("desktop.logs.day")}>
          <option value="">{t("desktop.logs.newest")}</option>
          {days.map((d) => (
            <option key={d} value={d}>
              {d}
            </option>
          ))}
        </select>
        <Segmented small value={level} options={levels} onChange={setLevel} label={t("desktop.logs.level")} />
        <input className="input spacer" type="search" value={text} onChange={(e) => setText(e.target.value)} placeholder={t("desktop.logs.search")} aria-label={t("desktop.logs.search")} />
        <IconButton icon={mdiRefresh} label={t("desktop.logs.refresh")} onClick={() => setTick((n) => n + 1)} />
        {canDelete(day, new Date()) && <IconButton icon={mdiDeleteOutline} label={t("desktop.logs.delete", { day })} onClick={() => setDeleting(day)} />}
        {inApp() && dir && (
          <Button small icon={mdiFolderOpenOutline} onClick={() => openFolder(dir).catch((e) => snack(message(e), { error: true }))}>
            {t("desktop.logs.open_folder")}
          </Button>
        )}
      </div>
      {error && <p className="error-text">{error}</p>}
      {deleting &&
        createPortal(
        <Dialog
          title={t("desktop.logs.delete_title", { day: deleting })}
          icon={mdiDeleteOutline}
          onClose={() => setDeleting("")}
          footer={
            <>
              <Button onClick={() => setDeleting("")}>{t("desktop.cancel")}</Button>
              <Button variant="filled" danger onClick={() => void remove(deleting)}>
                {t("desktop.logs.delete_confirm")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.logs.delete_body")}</p>
        </Dialog>,
          document.body,
        )}
      {page && (
        <>
          <code className="t-body-sm muted ellipsis" title={page.path}>
            {page.path}
          </code>
          {page.entries.length === 0 ? (
            <p className="muted">{t(page.path ? "desktop.logs.none" : "desktop.logs.no_log")}</p>
          ) : (
            <div className="log-list" role="log">
              {page.entries.map((e, i) => (
                <LogRow key={i} e={e} />
              ))}
            </div>
          )}
          {page.matched > page.entries.length && <p className="t-body-sm muted">{t("desktop.logs.more", { shown: page.entries.length, matched: page.matched })}</p>}
        </>
      )}
    </div>
  );
}

function LogRow({ e }: { e: LogEntry }) {
  const level = e.level.toLowerCase();
  return (
    <div className={`log-row level-${level}`}>
      <span className="log-time mono">{e.time ? logTime(timestampDate(e.time)) : ""}</span>
      <span className={`log-level level-${level}`}>{e.level}</span>
      <span className="log-msg">
        {e.message}
        {e.attrs.map((a) => (
          <span key={a.key} className="log-attr mono">
            {" "}
            <span className="muted">{a.key}=</span>
            {a.value}
          </span>
        ))}
      </span>
    </div>
  );
}
