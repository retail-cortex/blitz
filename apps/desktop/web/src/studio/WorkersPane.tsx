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
import { createPortal } from "react-dom";
import { mdiAlertCircleOutline, mdiArrowLeft, mdiCalendarClock, mdiCalendarPlus, mdiDeleteOutline, mdiProgressClock, mdiRefresh, mdiStopCircleOutline, mdiUndoVariant } from "@mdi/js";
import { workers } from "../api";
import { message } from "../errors";
import { t } from "../i18n";
import { WorkerState, type Worker, type WorkerRun } from "../gen/blitz/v1/worker_pb";
import { runDetail } from "../runningWorkers";
import { WorkerEditor } from "../WorkerEditor";
import { savePauses } from "../workerForm";
import { lastFailed, WorkerChip, WorkerView, workerStateLabel } from "../Workers";
import { Button, Dialog, Icon, IconButton, useSnackbar } from "../ui/controls";
import type { ItemIntent } from "../intent";

/** How often the pane asks for the workers and their runs again. */
const workersPollMs = 5000;

/** What slides in over the list: a worker's view or form, or a new worker's form. */
type Open = { mode: "read" | "edit"; name: string } | { mode: "new"; name: "" };

/**
 * A workspace's workers (Studio › Workers; the global ones, the home
 * workspace's, in Settings › Workers): its runs going now, each with
 * Stop, then its workers, filling the pane. A worker's view (what it does
 * and may do, its runs; Run, Enable or Disable, Edit, Delete), its form
 * or a new worker's slides in over the list; back, Cancel or Save return,
 * asking first when the form has unsaved changes (onDirty tells the
 * drawer too). intent opens one (the status bar's failed runs: the first
 * that failed; a blitz://workers link: the list).
 */
export function WorkersPane({
  dir,
  intent,
  global,
  onLeave,
  onDirty,
}: {
  dir: string;
  intent?: ItemIntent;
  /** The global workers: saved in ~/.blitz/workers. */
  global?: boolean;
  /** Called when a worker sends the user elsewhere (a run's conversation). */
  onLeave?: () => void;
  onDirty?: (dirty: boolean) => void;
}) {
  const snack = useSnackbar();
  const [list, setList] = useState<Worker[] | null>(null);
  const [running, setRunning] = useState<WorkerRun[]>([]);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<Open | null>(null);
  // A run or disable asked for with the intent, for the view to do once.
  const [request, setRequest] = useState<"run" | "disable" | undefined>();
  const [deleting, setDeleting] = useState("");
  // Where the form puts its buttons, whether it holds unsaved changes, and
  // a move waiting for the user to drop them.
  const [footer, setFooter] = useState<HTMLDivElement | null>(null);
  const [dirty, setDirty] = useState(false);
  const [pending, setPending] = useState<(() => void) | null>(null);

  const refresh = useCallback(async () => {
    try {
      const [l, r] = await Promise.all([workers.listWorkers({ workspace: dir }), workers.listRunningWorkerRuns({})]);
      setList(l.workers);
      setRunning(r.runs.filter((x) => x.workspace === dir));
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

  const editing = open !== null && open.mode !== "read" && dirty;
  useEffect(() => onDirty?.(editing), [editing, onDirty]);

  // The intent, once the list has the worker it names.
  const handled = useRef(0);
  useEffect(() => {
    if (!intent || intent.n === handled.current || !list) return;
    handled.current = intent.n;
    if (intent.kind === "new") return setOpen({ mode: "new", name: "" });
    const name = intent.id || (intent.kind === "show" ? list.find(lastFailed)?.name : "") || "";
    if (!name || !list.some((w) => w.name === name)) return;
    if (intent.kind === "delete") return setDeleting(name);
    setOpen({ mode: intent.kind === "edit" ? "edit" : "read", name });
    if (intent.kind === "run" || intent.kind === "disable") setRequest(intent.kind);
  }, [intent, list]);

  // Runs go, asking first when the form has unsaved changes.
  const leave = (go: () => void) => (editing ? setPending(() => go) : go());
  const back = () =>
    leave(() => {
      setDirty(false);
      setOpen(open?.mode === "edit" ? { mode: "read", name: open.name } : null);
      void refresh();
    });
  const toRead = (name: string) => {
    setDirty(false);
    setOpen({ mode: "read", name });
    void refresh();
  };

  const remove = async () => {
    const name = deleting;
    setDeleting("");
    try {
      await workers.deleteWorker({ workspace: dir, name });
      snack(t("desktop.workers.deleted", { name }));
      setOpen(null);
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
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

  const folder = global ? "~/.blitz/workers" : ".agents/workers";
  const worker = open?.name ? list?.find((w) => w.name === open.name) : undefined;
  const title = open?.mode === "new" ? t("desktop.newworker.title") : open?.mode === "edit" ? t("desktop.editworker.title", { name: open.name }) : open?.name ?? "";
  const doomed = list?.find((w) => w.name === deleting);
  return (
    <div className={`slide-pane workers-pane ${open ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        <div className="row side-head">
          <span className="t-title-sm spacer">{t(global ? "desktop.workers.global_title" : "desktop.workers.project_title")}</span>
          <IconButton icon={mdiCalendarPlus} label={t("desktop.newworker.title")} small onClick={() => setOpen({ mode: "new", name: "" })} />
          <IconButton icon={mdiRefresh} label={t("desktop.refresh")} small onClick={() => void refresh()} />
        </div>
        <p className="t-body-sm muted agent-dir">{t("desktop.workers.in", { path: folder })}</p>
        {error && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
          </div>
        )}
        {running.length > 0 && (
          <div className="list">
            <span className="menu-label">{t("desktop.menu.workers.running")}</span>
            {running.map((r) => (
              <div key={r.id} className="list-item">
                <Icon path={mdiProgressClock} />
                <span className="lines spacer">
                  <span className="ellipsis">{r.worker}</span>
                  <small className="ellipsis">{runDetail(r, dir, () => "")}</small>
                </span>
                <span className="trailing">
                  <IconButton icon={mdiStopCircleOutline} label={t("desktop.menu.workers.stop", { name: r.worker })} small onClick={() => void stop(r)} />
                </span>
              </div>
            ))}
          </div>
        )}
        {list?.length === 0 && (
          <div className="stack" style={{ gap: 12 }}>
            <p className="muted t-body-sm">{t("desktop.workers.none", { path: `${folder}/<name>/WORKER.md` })}</p>
            <span>
              <Button variant="tonal" icon={mdiCalendarPlus} onClick={() => setOpen({ mode: "new", name: "" })}>
                {t("desktop.newworker.title")}
              </Button>
            </span>
          </div>
        )}
        <div className="list">
          {list?.map((w) => (
            <button key={w.name} className={`list-item ${w.state === WorkerState.INVALID ? "has-problem" : ""}`} title={w.problems[0] || w.path} onClick={() => setOpen({ mode: "read", name: w.name })}>
              <Icon path={w.state === WorkerState.INVALID || lastFailed(w) ? mdiAlertCircleOutline : mdiCalendarClock} className={w.state === WorkerState.INVALID || lastFailed(w) ? "error-text" : undefined} />
              <span className="lines">
                <span className="ellipsis">{w.name}</span>
                <small className="ellipsis">{[workerStateLabel(w.state), w.schedule, w.description].filter(Boolean).join(" · ")}</small>
              </span>
            </button>
          ))}
        </div>
      </aside>
      <section className="slide-detail" inert={open === null}>
        {open && (
          <>
            <div className="row slide-back">
              <IconButton icon={mdiArrowLeft} label={t("desktop.workers.back")} onClick={back} />
              <h2 className="t-headline spacer ellipsis">{title}</h2>
              {open.mode === "read" && worker && <WorkerChip state={worker.state} />}
            </div>
            {open.mode === "new" && <WorkerEditor key="new" dir={dir} footer={footer} onCancel={back} onDirty={setDirty} onSaved={toRead} />}
            {open.mode === "edit" && worker && (
              <WorkerEditor key={`edit-${worker.name}`} dir={dir} edit={{ name: worker.name, path: worker.path, pauses: savePauses(worker.state) }} footer={footer} onCancel={back} onSaved={toRead} onDirty={setDirty} />
            )}
            {open.mode === "read" && worker && (
              <WorkerView
                key={worker.name + worker.hash}
                dir={dir}
                worker={worker}
                onChange={refresh}
                onEdit={() => setOpen({ mode: "edit", name: worker.name })}
                onDelete={() => setDeleting(worker.name)}
                request={request}
                onRequestDone={() => setRequest(undefined)}
                onLeave={() => onLeave?.()}
              />
            )}
            {open.mode !== "new" && list && !worker && <p className="muted">{t("desktop.workers.gone", { name: open.name })}</p>}
            <div className="row slide-actions dialog-actions" ref={setFooter} />
          </>
        )}
      </section>
      {deleting &&
        createPortal(
          <Dialog
            title={t("desktop.workers.delete_title", { name: deleting })}
            icon={mdiDeleteOutline}
            onClose={() => setDeleting("")}
            footer={
              <>
                <Button onClick={() => setDeleting("")}>{t("desktop.cancel")}</Button>
                <Button variant="filled" danger onClick={() => void remove()}>
                  {t("desktop.workers.delete")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.workers.delete_body", { name: deleting, folder: doomed?.path.replace(/\/WORKER\.md$/, "") || `${folder}/${deleting}` })}</p>
          </Dialog>,
          document.body,
        )}
      {pending &&
        createPortal(
          <Dialog
            title={t("desktop.agents.discard_title")}
            icon={mdiUndoVariant}
            onClose={() => setPending(null)}
            footer={
              <>
                <Button onClick={() => setPending(null)}>{t("desktop.cancel")}</Button>
                <Button
                  variant="filled"
                  danger
                  onClick={() => {
                    const go = pending;
                    setPending(null);
                    go();
                  }}
                >
                  {t("desktop.agents.discard")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.agents.discard_body", { name: open?.name || t("desktop.agents.unnamed") })}</p>
          </Dialog>,
          document.body,
        )}
    </div>
  );
}
