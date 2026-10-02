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

import { useCallback, useEffect, useRef, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import {
  mdiAlertCircleOutline,
  mdiCalendarClock,
  mdiCalendarEdit,
  mdiCalendarPlus,
  mdiCheckCircleOutline,
  mdiCloseCircleOutline,
  mdiDeleteOutline,
  mdiMessageTextOutline,
  mdiPauseCircleOutline,
  mdiPencilOutline,
  mdiPlay,
  mdiProgressClock,
  mdiShieldCheckOutline,
} from "@mdi/js";
import { workers } from "./api";
import { message } from "./errors";
import { loadSession } from "./events";
import { language, t } from "./i18n";
import { RunStatus, WorkerState, type Worker, type WorkerRun } from "./gen/blitz/v1/worker_pb";
import { applyEvent, failed, summarizeArgs, type Entry } from "./turns";
import { WorkerEditor } from "./WorkerEditor";
import { savePauses } from "./workerForm";
import { Button, Chip, Dialog, Icon, useSnackbar } from "./ui/controls";
import type { ItemIntent } from "./intent";

const stateKeys: Record<WorkerState, string> = {
  [WorkerState.UNSPECIFIED]: "",
  [WorkerState.NEW]: "new",
  [WorkerState.ENABLED]: "enabled",
  [WorkerState.DISABLED]: "disabled",
  [WorkerState.CHANGED]: "changed",
  [WorkerState.INVALID]: "invalid",
};
/** A worker's state, in words (New, Enabled, Changed…). */
export const workerStateLabel = (s: WorkerState) => (stateKeys[s] ? t(`desktop.worker.state.${stateKeys[s]}`) : "?");

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

/** How often an open worker dialog asks for the workers again. */
const workersPollMs = 5000;

/** Whether a worker's latest run failed or hit a limit. */
const lastFailed = (w: Worker) => w.lastRun?.status === RunStatus.FAILED || w.lastRun?.status === RunStatus.LIMITED;

/**
 * A worker, over the editor, from the top bar's Workers menu (intent): a
 * read view (what it does and may do, its runs; Run, Enable or Disable,
 * Edit, Delete) and an edit view (its form, its buttons in the footer),
 * switching in place in one dialog; a new worker starts on the form, and
 * saving shows it. Cancel goes back to the read view; the close button
 * closes the dialog. Deleting asks first.
 * Without a name (the status bar's failed runs) it shows the first worker
 * whose last run failed, else the first.
 */
export function WorkerModal({ dir, intent, onClose }: { dir: string; intent: ItemIntent; onClose: () => void }) {
  const snack = useSnackbar();
  const [list, setList] = useState<Worker[] | null>(null);
  const [name, setName] = useState(intent.id);
  const [mode, setMode] = useState<"read" | "edit" | "new">(intent.kind === "new" ? "new" : intent.kind === "edit" ? "edit" : "read");
  const [deleting, setDeleting] = useState(intent.kind === "delete");
  // A run or disable the menu asked for, for the read view to do once.
  const [request, setRequest] = useState<"run" | "disable" | undefined>(intent.kind === "run" || intent.kind === "disable" ? intent.kind : undefined);
  const [error, setError] = useState("");
  // The dialog's footer, where the edit view puts its buttons.
  const [footer, setFooter] = useState<HTMLDivElement | null>(null);

  const refresh = useCallback(async () => {
    try {
      setList((await workers.listWorkers({ workspace: dir })).workers);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [dir]);
  // WORKER.md files edited elsewhere show up (BL-DSK-50).
  useEffect(() => {
    void refresh();
    const timer = setInterval(refresh, workersPollMs);
    return () => clearInterval(timer);
  }, [refresh]);
  useEffect(() => {
    if (!name && list?.length) setName((list.find(lastFailed) ?? list[0]).name);
  }, [list, name]);

  const worker = list?.find((w) => w.name === name);
  const remove = async () => {
    setDeleting(false);
    try {
      await workers.deleteWorker({ workspace: dir, name });
      snack(t("desktop.workers.deleted", { name }));
      onClose();
    } catch (e) {
      snack(message(e), { error: true });
      if (intent.kind === "delete") onClose();
    }
  };
  const confirm = deleting && (
    <Dialog
      title={t("desktop.workers.delete_title", { name })}
      icon={mdiDeleteOutline}
      onClose={() => (intent.kind === "delete" ? onClose() : setDeleting(false))}
      footer={
        <>
          <Button onClick={() => (intent.kind === "delete" ? onClose() : setDeleting(false))}>{t("desktop.cancel")}</Button>
          <Button variant="filled" danger onClick={() => void remove()}>
            {t("desktop.workers.delete")}
          </Button>
        </>
      }
    >
      <p>{t("desktop.workers.delete_body", { name })}</p>
    </Dialog>
  );
  // Deleting from the menu asks, and nothing else.
  if (intent.kind === "delete") return confirm || null;

  const toRead = () => {
    setMode("read");
    void refresh();
  };
  // One dialog, its view switching in place: the read view, or the form
  // (editing this worker, or a new one).
  const title = mode === "new" ? t("desktop.newworker.title") : mode === "edit" ? t("desktop.editworker.title", { name }) : worker?.name || t("desktop.view.workers");
  return (
    <>
      <Dialog
        title={title}
        icon={mode === "new" ? mdiCalendarPlus : mode === "edit" ? mdiCalendarEdit : mdiCalendarClock}
        wide
        className="worker-dialog"
        closeButton
        headerActions={mode === "read" && worker && <WorkerChip state={worker.state} />}
        footer={mode === "read" ? undefined : <div className="dialog-actions" ref={setFooter} />}
        onClose={onClose}
      >
        {error && (
          <div className="card error row">
            <Icon path={mdiAlertCircleOutline} /> {error}
          </div>
        )}
        {mode === "new" && (
          <WorkerEditor
            key="new"
            dir={dir}
            footer={footer}
            onCancel={onClose}
            onSaved={(n) => {
              setName(n);
              toRead();
            }}
          />
        )}
        {mode === "edit" && worker && <WorkerEditor key={`edit-${worker.name}`} dir={dir} edit={{ name: worker.name, path: worker.path, pauses: savePauses(worker.state) }} footer={footer} onCancel={toRead} onSaved={toRead} />}
        {mode === "read" && list?.length === 0 && (
          <div className="stack" style={{ gap: 12 }}>
            <p className="muted">{t("desktop.workers.none", { path: ".agents/workers/<name>/WORKER.md" })}</p>
            <Button variant="tonal" icon={mdiCalendarPlus} onClick={() => setMode("new")}>
              {t("desktop.newworker.title")}
            </Button>
          </div>
        )}
        {mode === "read" && worker && (
          <WorkerView
            dir={dir}
            worker={worker}
            onChange={refresh}
            onEdit={() => setMode("edit")}
            onDelete={() => setDeleting(true)}
            request={request}
            onRequestDone={() => setRequest(undefined)}
            onLeave={onClose}
            key={worker.name + worker.hash}
          />
        )}
        {mode === "read" && list && list.length > 0 && name && !worker && <p className="muted">{t("desktop.workers.gone", { name })}</p>}
      </Dialog>
      {confirm}
    </>
  );
}

/** A worker's state as a chip (Enabled tinted, Changed a warning, Invalid an error). */
function WorkerChip({ state }: { state: WorkerState }) {
  return (
    <Chip className="static" selected={state === WorkerState.ENABLED} tone={state === WorkerState.INVALID ? "danger" : state === WorkerState.CHANGED ? "warn" : undefined}>
      {workerStateLabel(state)}
    </Chip>
  );
}

function WorkerView({
  dir,
  worker,
  onChange,
  onEdit,
  onDelete,
  request,
  onRequestDone,
  onLeave,
}: {
  dir: string;
  worker: Worker;
  onChange: () => void;
  onEdit: () => void;
  onDelete: () => void;
  /** A run or disable the Workers menu asked for, done once (onRequestDone). */
  request?: "run" | "disable";
  onRequestDone: () => void;
  /** Called when it sends the user elsewhere (a run's conversation). */
  onLeave: () => void;
}) {
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

  const requested = useRef({ runNow, disable, onRequestDone });
  requested.current = { runNow, disable, onRequestDone };
  useEffect(() => {
    if (!request) return;
    requested.current.onRequestDone();
    void (request === "run" ? requested.current.runNow() : requested.current.disable());
  }, [request]);

  const limits = worker.limits;
  const enabled = worker.state === WorkerState.ENABLED;
  return (
    <div className="worker">
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
        <Button danger icon={mdiDeleteOutline} onClick={onDelete}>
          {t("desktop.workers.delete")}
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
                  <Button small icon={mdiMessageTextOutline} onClick={() => (loadSession({ dir, id: r.sessionId }), onLeave())}>
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
