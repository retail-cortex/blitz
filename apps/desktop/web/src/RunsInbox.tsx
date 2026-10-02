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

import { timestampDate } from "@bufbuild/protobuf/wkt";
import { mdiInboxOutline } from "@mdi/js";
import { sessions } from "./api";
import { message } from "./errors";
import { loadSession } from "./events";
import type { BackgroundRun } from "./gen/blitz/v1/session_pb";
import { t } from "./i18n";
import { isActive, refreshRuns, runAge } from "./backgroundRuns";
import { displayName, openWorkspace } from "./prefs";
import { useApp } from "./state";
import { Button, Dialog, useSnackbar } from "./ui/controls";

/** The background runs, to open or stop (the Workers menu's and the status bar's). */
export function InboxDialog({ list, onClose }: { list: BackgroundRun[]; onClose: () => void }) {
  const { prefs, update } = useApp();
  const snack = useSnackbar();
  const now = new Date();
  const openRun = (r: BackgroundRun) => {
    update((p) => openWorkspace(p, r.workspace));
    loadSession({ dir: r.workspace, id: r.sessionId });
    onClose();
  };
  const stop = async (r: BackgroundRun) => {
    try {
      await sessions.stopBackground({ id: r.id });
      await refreshRuns();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  return (
    <Dialog title={t("desktop.inbox.title")} icon={mdiInboxOutline} onClose={onClose} wide footer={<Button onClick={onClose}>{t("desktop.done")}</Button>}>
      {list.length === 0 ? (
        <p className="muted">{t("desktop.inbox.empty")}</p>
      ) : (
        <ul className="inbox-list">
          {list.map((r) => (
            <li key={r.id} className="inbox-row">
              <span className={`inbox-state ${r.state}`}>{r.state === "waiting" ? t("desktop.inbox.state.waiting", { count: r.waiting }) : t(`desktop.inbox.state.${r.state}`)}</span>
              <div className="inbox-main">
                <span className="t-title-sm ellipsis">{r.prompt}</span>
                <span className="t-body-sm muted ellipsis">
                  {r.id} · {displayName(prefs.workspaces.find((w) => w.dir === r.workspace) ?? { dir: r.workspace })} · {runAge(r.started ? timestampDate(r.started) : now, r.ended ? timestampDate(r.ended) : undefined, now)} · ${r.costUsd.toFixed(2)}
                  {r.error ? ` · ${r.error}` : ""}
                </span>
              </div>
              <Button small onClick={() => openRun(r)}>
                {t("desktop.inbox.open")}
              </Button>
              {isActive(r) && (
                <Button small danger onClick={() => stop(r)}>
                  {t("desktop.inbox.stop")}
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  );
}
