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
import { timestampDate } from "@bufbuild/protobuf/wkt";
import {
  mdiAlertCircleOutline,
  mdiCalendarClock,
  mdiCalendarPlus,
  mdiCheckCircleOutline,
  mdiCloseCircleOutline,
  mdiMessageTextOutline,
  mdiPauseCircleOutline,
  mdiPencilOutline,
  mdiPlay,
  mdiProgressClock,
  mdiRefresh,
  mdiShieldCheckOutline,
} from "@mdi/js";
import { workers } from "./api";
import { message } from "./errors";
import { loadSession } from "./events";
import { language, t } from "./i18n";
import { RunStatus, WorkerState, type Worker, type WorkerRun } from "./gen/blitz/v1/worker_pb";
import { applyEvent, failed, summarizeArgs, type Entry } from "./turns";
import { WorkerDialog, type WorkerEdit } from "./WorkerDialog";
import { savePauses } from "./workerForm";
import { Button, Chip, Icon, IconButton } from "./ui/controls";

const stateKeys: Record<WorkerState, string> = {
  [WorkerState.UNSPECIFIED]: "",
  [WorkerState.NEW]: "new",
  [WorkerState.ENABLED]: "enabled",
  [WorkerState.DISABLED]: "disabled",
  [WorkerState.CHANGED]: "changed",
  [WorkerState.INVALID]: "invalid",
};
const stateLabel = (s: WorkerState) => (stateKeys[s] ? t(`desktop.worker.state.${stateKeys[s]}`) : "?");

const runKeys: Record<RunStatus, string> = {
  [RunStatus.UNSPECIFIED]: "",
  [RunStatus.RUNNING]: "running",
  [RunStatus.SUCCEEDED]: "succeeded",
  [RunStatus.FAILED]: "failed",
  [RunStatus.LIMITED]: "limited",
  [RunStatus.SKIPPED]: "skipped",
};
const runLabel = (s: RunStatus) => (runKeys[s] ? t(`desktop.run.${runKeys[s]}`) : "?");

const runIcon = (s: RunStatus) =>
  s === RunStatus.SUCCEEDED ? mdiCheckCircleOutline : s === RunStatus.RUNNING ? mdiProgressClock : s === RunStatus.SKIPPED ? mdiPauseCircleOutline : mdiCloseCircleOutline;

/** How often the open Workers view asks for the workers again. */
const workersPollMs = 5000;

/** A workspace's workers: review and enable them, run them, see their runs. */
export function Workers({ dir }: { dir: string }) {
  const [list, setList] = useState<Worker[]>([]);
  const [selected, setSelected] = useState("");
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<WorkerEdit | null>(null);

  const refresh = useCallback(async () => {
    try {
      const ws = (await workers.listWorkers({ workspace: dir })).workers;
      setList(ws);
      setSelected((cur) => cur || ws[0]?.name || "");
    } catch (e) {
      setError(message(e));
    }
  }, [dir]);
  // WORKER.md files edited elsewhere show up: the list is asked for again
  // while the view is open (BL-DSK-50).
  useEffect(() => {
    refresh();
    const timer = setInterval(refresh, workersPollMs);
    return () => clearInterval(timer);
  }, [refresh]);

  const worker = list.find((w) => w.name === selected);
  return (
    <div className="split">
      <aside className="split-side">
        <div className="row side-head">
          <span className="t-title-sm spacer">{t("desktop.workers.title")}</span>
          <IconButton icon={mdiCalendarPlus} label={t("desktop.newworker.title")} small onClick={() => setCreating(true)} />
          <IconButton icon={mdiRefresh} label={t("desktop.refresh")} small onClick={refresh} />
        </div>
        {list.length === 0 && (
          <div className="stack" style={{ gap: 8 }}>
            <p className="muted t-body-sm">{t("desktop.workers.none", { path: ".agents/workers/<name>/WORKER.md" })}</p>
            <Button small variant="tonal" icon={mdiCalendarPlus} onClick={() => setCreating(true)}>
              {t("desktop.newworker.title")}
            </Button>
          </div>
        )}
        <div className="list">
          {list.map((w) => (
            <button key={w.name} className={`list-item ${w.name === selected ? "active" : ""}`} onClick={() => setSelected(w.name)}>
              <Icon path={mdiCalendarClock} />
              <span className="lines">
                <span className="ellipsis">{w.name}</span>
                <small className="ellipsis">
                  {stateLabel(w.state)} · {w.schedule}
                </small>
              </span>
            </button>
          ))}
        </div>
      </aside>
      <section className="split-main">
        {error && (
          <div className="card error row">
            <Icon path={mdiAlertCircleOutline} /> {error}
          </div>
        )}
        {(creating || editing) && (
          <WorkerDialog
            dir={dir}
            edit={editing ?? undefined}
            onClose={() => {
              setCreating(false);
              setEditing(null);
            }}
            onSaved={(name) => {
              setSelected(name);
              refresh();
            }}
          />
        )}
        {worker ? (
          <WorkerView dir={dir} worker={worker} onChange={refresh} onEdit={() => setEditing({ name: worker.name, path: worker.path, pauses: savePauses(worker.state) })} key={worker.name + worker.hash} />
        ) : (
          list.length > 0 && <p className="muted">{t("desktop.workers.choose")}</p>
        )}
      </section>
    </div>
  );
}

function WorkerView({ dir, worker, onChange, onEdit }: { dir: string; worker: Worker; onChange: () => void; onEdit: () => void }) {
  const [runs, setRuns] = useState<WorkerRun[]>([]);
  const [live, setLive] = useState<Entry[] | null>(null);
  const [error, setError] = useState("");

  const refreshRuns = useCallback(async () => {
    setRuns((await workers.listWorkerRuns({ workspace: dir, name: worker.name, limit: 20 })).runs);
  }, [dir, worker.name]);
  useEffect(() => {
    refreshRuns().catch((e) => setError(message(e)));
  }, [refreshRuns]);

  const act = async (f: () => Promise<unknown>) => {
    setError("");
    try {
      await f();
      onChange();
    } catch (e) {
      setError(message(e));
    }
  };
  // The hash shown is the one enabled: an edit since then fails.
  const enable = () => act(() => workers.enableWorker({ workspace: dir, name: worker.name, hash: worker.hash }));
  const disable = () => act(() => workers.disableWorker({ workspace: dir, name: worker.name }));
  const runNow = () =>
    act(async () => {
      const started = (await workers.runWorker({ workspace: dir, name: worker.name })).run!;
      setLive([]);
      try {
        for await (const res of workers.watchWorkerRun({ runId: started.id })) {
          const ev = res.event; // none in the proxy's keepalive messages
          if (ev) setLive((e) => applyEvent(e ?? [], ev));
        }
      } catch {
        // A run too quick to watch is simply recorded.
      }
      await refreshRuns();
    });

  const limits = worker.limits;
  const enabled = worker.state === WorkerState.ENABLED;
  return (
    <div className="worker">
      <div className="row">
        <h2 className="t-headline spacer">{worker.name}</h2>
        <Chip className="static" selected={enabled} tone={worker.state === WorkerState.INVALID ? "danger" : worker.state === WorkerState.CHANGED ? "warn" : undefined}>
          {stateLabel(worker.state)}
        </Chip>
      </div>
      {worker.description && <p className="muted">{worker.description}</p>}
      <div className="card outlined">
        <dl className="facts">
          <dt>{t("desktop.worker.schedule")}</dt>
          <dd>
            {worker.schedule} · <code>{worker.cron}</code> ({worker.timezone})
            {worker.nextRun && enabled && <div className="t-body-sm muted">{t("desktop.worker.next", { time: timestampDate(worker.nextRun).toLocaleString(language()) })}</div>}
          </dd>
          {worker.agent && (
            <>
              <dt>{t("desktop.worker.agent")}</dt>
              <dd>{worker.agent}</dd>
            </>
          )}
          {worker.model && (
            <>
              <dt>{t("desktop.worker.model")}</dt>
              <dd>{worker.model}</dd>
            </>
          )}
          <dt>{t("desktop.worker.may")}</dt>
          <dd className="row wrap">{worker.permissions.length ? worker.permissions.map((p) => <code key={p} className="chip static">{p}</code>) : t("desktop.worker.read_only")}</dd>
          <dt>{t("desktop.worker.limits")}</dt>
          <dd>
            {t("desktop.worker.limits.value", {
              calls: limits?.maxTurns ?? "?",
              cost: limits ? limits.maxCostUsd.toFixed(2) : "?",
              minutes: limits?.timeout ? Number(limits.timeout.seconds) / 60 : "?",
            })}
          </dd>
          <dt>{t("desktop.worker.content")}</dt>
          <dd>
            <code className="t-body-sm muted">{worker.hash}</code>
          </dd>
        </dl>
      </div>
      {worker.problems.map((p) => (
        <div key={p} className="card error row">
          <Icon path={mdiAlertCircleOutline} size="sm" /> {p}
        </div>
      ))}
      <div className="row wrap">
        {!enabled && worker.state !== WorkerState.INVALID && (
          <Button variant="filled" icon={mdiShieldCheckOutline} onClick={enable} title={t("desktop.worker.enable_hint", { path: worker.path })}>
            {t("desktop.worker.enable")}
          </Button>
        )}
        {enabled && (
          <Button variant="filled" icon={mdiPlay} onClick={runNow}>
            {t("desktop.worker.run_now")}
          </Button>
        )}
        {enabled && (
          <Button variant="outlined" onClick={disable}>
            {t("desktop.worker.disable")}
          </Button>
        )}
        <Button variant={worker.state === WorkerState.INVALID ? "filled" : "outlined"} icon={mdiPencilOutline} onClick={onEdit} title={worker.path}>
          {t("desktop.worker.edit")}
        </Button>
      </div>
      {error && <p className="error-text">{error}</p>}
      {live && (
        <div className="card outlined live-run">
          <div className="t-title-sm">{t("desktop.worker.this_run")}</div>
          {live.length === 0 && <p className="muted">{t("desktop.worker.running")}</p>}
          {live.map((e, i) =>
            e.kind === "tool" ? (
              <div key={i} className={`t-body-sm ${failed(e.result) ? "error-text" : "muted"}`}>
                <code>{e.name}</code> {summarizeArgs(e.args)} {e.result === undefined ? "…" : failed(e.result) ? "✗" : "✓"}
              </div>
            ) : "text" in e && e.kind !== "thought" ? (
              <div key={i} className="live-text">
                {e.text}
              </div>
            ) : null,
          )}
        </div>
      )}
      <h3 className="t-title">{t("desktop.worker.runs")}</h3>
      {runs.length === 0 && <p className="muted t-body-sm">{t("desktop.worker.no_runs")}</p>}
      <div className="runs">
        {runs.map((r) => (
          <div key={r.id} className="run">
            <Icon path={runIcon(r.status)} className={r.status === RunStatus.FAILED || r.status === RunStatus.LIMITED ? "error-text" : "muted"} />
            <div className="stack" style={{ gap: 2 }}>
              <span>
                {runLabel(r.status)} · {r.manual ? t("desktop.worker.manual") : t("desktop.worker.scheduled")}
                {r.usage?.priced ? ` · $${r.usage.costUsd.toFixed(4)}` : ""}
              </span>
              <span className="t-body-sm muted">
                {t("desktop.worker.run_detail", { time: r.started ? timestampDate(r.started).toLocaleString(language()) : "", session: r.sessionId })}
              </span>
              {r.refusals.map((f, i) => (
                <span key={i} className="t-body-sm muted">
                  {t("desktop.worker.refused", { detail: f.detail })}
                </span>
              ))}
              {r.error && <span className="t-body-sm error-text">{r.error.message}</span>}
              {r.files.length > 0 && <span className="t-body-sm muted">{t("desktop.worker.changed", { files: r.files.join(", ") })}</span>}
              {r.sessionId && (
                <span>
                  <Button small icon={mdiMessageTextOutline} onClick={() => loadSession({ dir, id: r.sessionId })}>
                    {t("desktop.worker.open_session")}
                  </Button>
                </span>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
