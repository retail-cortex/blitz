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

import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { mdiAlertCircleOutline, mdiCalendarClock, mdiDeleteOutline, mdiFolderAlertOutline, mdiFolderOutline, mdiProgressClock, mdiStopCircleOutline } from "@mdi/js";
import { workers } from "./api";
import { message } from "./errors";
import { showView } from "./events";
import { language, t } from "./i18n";
import { WorkerState, type WorkerPolicySettings, type WorkerRun, type WorkspaceWorkers } from "./gen/blitz/v1/worker_pb";
import { WorkerLimits } from "./WorkerLimits";
import { displayName, openWorkspace } from "./prefs";
import { runDetail } from "./runningWorkers";
import { useApp } from "./state";
import { intent as newIntent } from "./intent";
import { WorkersPane } from "./studio/WorkersPane";
import { lastFailed, workerStateLabel } from "./Workers";
import { workspaceFlag } from "./workersOverview";
import { Button, Chip, Dialog, Icon, IconButton, useSnackbar } from "./ui/controls";

/** How often the overview asks for every workspace's workers again. */
const overviewPollMs = 10000;

/**
 * Settings › Workers (spec_workers_023 WK-60–WK-62): the global workers,
 * saved in ~/.blitz/workers and run in the home workspace, listed and
 * edited as Studio lists a workspace's; the limits on every worker; the
 * worker runs going now, in
 * every workspace; and every workspace the service has workers on record
 * for, open in the app or not, so none is forgotten: one not in the app's
 * workspaces says so, with Open; one whose folder is gone, with Forget.
 * onLeave closes Settings (a workspace opened, a run's conversation).
 */
export function WorkersSettings({ onLeave, worker = "" }: { onLeave: () => void; worker?: string }) {
  // A global job to open on (the tray's Open: blitz://workers on the home workspace, with its name).
  const [intent] = useState(() => (worker ? newIntent("show", worker) : undefined));
  const { prefs, update } = useApp();
  const snack = useSnackbar();
  const [home, setHome] = useState("");
  const [groups, setGroups] = useState<WorkspaceWorkers[]>([]);
  const [policy, setPolicy] = useState<WorkerPolicySettings>();
  const [running, setRunning] = useState<WorkerRun[]>([]);
  const [error, setError] = useState("");
  const [forgetting, setForgetting] = useState("");

  const refresh = useCallback(async () => {
    try {
      const [all, live] = await Promise.all([workers.listAllWorkers({}), workers.listRunningWorkerRuns({})]);
      setHome(all.home);
      setGroups(all.workspaces.filter((g) => !g.home));
      setPolicy(all.policy);
      setRunning(live.runs);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, []);
  useEffect(() => {
    void refresh();
    const timer = setInterval(refresh, overviewPollMs);
    return () => clearInterval(timer);
  }, [refresh]);

  const name = (dir: string) => (dir === home ? t("desktop.workers.global_title") : displayName(prefs.workspaces.find((w) => w.dir === dir) ?? { dir }));
  // The workspace, opened (added to the app's when it isn't there) on its workers.
  const open = (dir: string) => {
    update((p) => openWorkspace(p, dir));
    showView({ dir, view: "workers" });
    onLeave();
  };
  const stop = async (r: WorkerRun) => {
    try {
      await workers.stopWorkerRun({ runId: r.id });
      snack(t("desktop.menu.workers.stopping", { name: r.worker }));
      setRunning((runs) => runs.filter((x) => x.id !== r.id));
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const forget = async () => {
    const dir = forgetting;
    setForgetting("");
    try {
      await workers.forgetWorkspaceWorkers({ workspace: dir });
      snack(t("desktop.workers.forgotten", { name: name(dir) }));
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  return (
    <div className="stack workers-settings" style={{ gap: 20 }}>
      <p className="t-body-sm muted">{t("desktop.workers.settings_intro")}</p>
      {error && <p className="error-text">{error}</p>}
      {policy && <WorkerLimits policy={policy} onChanged={() => void refresh()} />}
      {home && (
        <div className="slide-box">
          <WorkersPane dir={home} global onLeave={onLeave} intent={intent} />
        </div>
      )}

      {running.length > 0 && (
        <section className="stack" style={{ gap: 8 }}>
          <span className="t-title-sm">{t("desktop.menu.workers.running")}</span>
          <div className="list">
            {running.map((r) => (
              <div key={r.id} className="list-item">
                <Icon path={mdiProgressClock} />
                <span className="lines spacer">
                  <span className="ellipsis">{r.worker}</span>
                  <small className="ellipsis">{runDetail(r, "", name)}</small>
                </span>
                <span className="trailing">
                  <IconButton icon={mdiStopCircleOutline} label={t("desktop.menu.workers.stop", { name: r.worker })} small onClick={() => void stop(r)} />
                </span>
              </div>
            ))}
          </div>
        </section>
      )}

      <section className="stack" style={{ gap: 8 }}>
        <span className="t-title-sm">{t("desktop.workers.everywhere")}</span>
        <p className="t-body-sm muted">{t("desktop.workers.everywhere_hint")}</p>
        {groups.length === 0 && !error && <p className="t-body-sm muted">{t("desktop.workers.everywhere_none")}</p>}
        {groups.map((g) => {
          const flag = workspaceFlag(g, prefs.workspaces);
          return (
            <div key={g.workspace} className="card outlined stack workers-group" style={{ gap: 8 }}>
              <div className="row wrap" style={{ gap: 8 }}>
                <Icon path={flag === "missing" ? mdiFolderAlertOutline : mdiFolderOutline} className={flag === "missing" ? "error-text" : "muted"} />
                <span className="stack spacer" style={{ gap: 0, minWidth: 0 }}>
                  <strong className="ellipsis">{name(g.workspace)}</strong>
                  <code className="t-body-sm muted ellipsis" title={g.workspace}>
                    {g.workspace}
                  </code>
                </span>
                {flag === "missing" && (
                  <Chip className="static" tone="danger">
                    {t("desktop.workers.missing")}
                  </Chip>
                )}
                {flag === "unlisted" && (
                  <Chip className="static" tone="warn">
                    {t("desktop.workers.not_listed")}
                  </Chip>
                )}
                {flag === "missing" ? (
                  <Button small danger icon={mdiDeleteOutline} onClick={() => setForgetting(g.workspace)}>
                    {t("desktop.workers.forget")}
                  </Button>
                ) : (
                  <Button small onClick={() => open(g.workspace)}>
                    {t(flag === "unlisted" ? "desktop.workers.add_open" : "desktop.workers.open")}
                  </Button>
                )}
              </div>
              {flag === "missing" && <p className="t-body-sm muted">{t("desktop.workers.missing_hint")}</p>}
              {g.problem && (
                <p className="t-body-sm error-text row" style={{ gap: 6 }}>
                  <Icon path={mdiAlertCircleOutline} size="sm" /> {g.problem}
                </p>
              )}
              <div className="stack" style={{ gap: 4 }}>
                {g.workers.map((w) => (
                  <span key={w.name} className="row t-body-sm" style={{ gap: 8 }}>
                    <Icon path={lastFailed(w) || w.state === WorkerState.INVALID ? mdiAlertCircleOutline : mdiCalendarClock} size="sm" className={lastFailed(w) || w.state === WorkerState.INVALID ? "error-text" : "muted"} />
                    <span className="ellipsis">
                      <strong>{w.name}</strong> · {workerStateLabel(w.state)}
                      {w.schedule && ` · ${w.schedule}`}
                      {w.nextRun && w.state === WorkerState.ENABLED && ` · ${t("desktop.worker.next", { time: timestampDate(w.nextRun).toLocaleString(language()) })}`}
                    </span>
                  </span>
                ))}
              </div>
            </div>
          );
        })}
      </section>

      {forgetting &&
        createPortal(
          <Dialog
            title={t("desktop.workers.forget_title", { name: name(forgetting) })}
            icon={mdiDeleteOutline}
            onClose={() => setForgetting("")}
            footer={
              <>
                <Button onClick={() => setForgetting("")}>{t("desktop.cancel")}</Button>
                <Button variant="filled" danger onClick={() => void forget()}>
                  {t("desktop.workers.forget")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.workers.forget_body", { path: forgetting })}</p>
          </Dialog>,
          document.body,
        )}
    </div>
  );
}
