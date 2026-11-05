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
  mdiAccountClockOutline,
  mdiCheck,
  mdiCheckCircleOutline,
  mdiClose,
  mdiCloseCircleOutline,
  mdiDeleteOutline,
  mdiDownload,
  mdiFileOutline,
  mdiPauseCircleOutline,
  mdiPencilOutline,
  mdiPlay,
  mdiProgressClock,
  mdiRefresh,
  mdiSourceBranch,
  mdiSourceMerge,
  mdiStopCircleOutline,
  mdiUploadOutline,
} from "@mdi/js";
import { config } from "./api";
import { message, reason } from "./errors";
import { useServiceRuns } from "./serviceRuns";
import { formatBytes } from "./bytes";
import { language, t } from "./i18n";
import type { GetServiceJobResponse, ServiceJob, ServiceRun, ServiceRunFile } from "./gen/blitz/v1/config_pb";
import type { Worker } from "./gen/blitz/v1/worker_pb";
import { Button, Icon, IconButton, useSnackbar } from "@blitz/webui";


const statusIcon: Record<string, string> = {
  paused: mdiAccountClockOutline,
  queued: mdiProgressClock,
  running: mdiProgressClock,
  succeeded: mdiCheckCircleOutline,
  skipped: mdiPauseCircleOutline,
  stopped: mdiStopCircleOutline,
};

/** Sync states waiting won't clear: the person acts (signs in, resolves, fixes the file), then Check again. */
const stuckSync = ["offline", "signed_out", "conflicts", "refused", "secrets"];

/** How often, and how many times, Studio looks again while sync puts a job on main (5 minutes). */
const syncPollMs = 5000;
const syncPolls = 60;

/** Whether to look again by itself: sync is putting the job on main, in a state that moves, and it hasn't looked syncPolls times. */
export const waitsForSync = (r: Pick<GetServiceJobResponse, "reason" | "syncState"> | null, polls: number) => r?.reason === "saving" && !stuckSync.includes(r.syncState) && polls < syncPolls;

/** whyNot is what Studio says of a job the service hasn't got, by its reason (GetServiceJob's). */
export function whyNot(r: Pick<GetServiceJobResponse, "reason" | "detail" | "syncState">): string {
  switch (r.reason) {
    case "outside":
      return t("desktop.service_job.outside", { path: r.detail });
    case "not_pushed":
      return t("desktop.service_job.not_pushed");
    case "saving":
      if (r.syncState === "refused" || r.syncState === "secrets") return t(`desktop.service_job.saving.${r.syncState}`, { why: r.detail });
      return ["offline", "signed_out", "conflicts"].includes(r.syncState) ? t(`desktop.service_job.saving.${r.syncState}`) : t("desktop.service_job.saving");
    case "not_service":
      return t("desktop.service_job.not_service", { why: r.detail });
    default:
      return r.detail ? `${t("desktop.service_job.not_read")} ${t("desktop.service_job.not_read_why", { detail: r.detail })}` : t("desktop.service_job.not_read");
  }
}

/**
 * A job that runs on the organization's service (runs_on: service; spec
 * 053 EN-42), in place of the local Run, Enable and runs: the service's
 * runs of it, each with its branch and the files it left, and Run now and
 * on or off there. Edit and Delete change WORKER.md here, as for any job.
 */
export function ServiceJobView({ dir, worker, onEdit, onDelete }: { dir: string; worker: Worker; onEdit: () => void; onDelete: () => void }) {
  const snack = useSnackbar();
  const [job, setJob] = useState<ServiceJob | null | undefined>(undefined);
  const [runs, setRuns] = useState<ServiceRun[]>([]);
  // Why the service hasn't got it, while it hasn't.
  const [missing, setMissing] = useState<Pick<GetServiceJobResponse, "reason" | "detail" | "syncState"> | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const show = (r: GetServiceJobResponse) => {
    setJob(r.found ? r.job! : null);
    setRuns(r.runs);
    setMissing(r.found ? null : r);
  };
  const refresh = useCallback(async () => {
    try {
      show(await config.getServiceJob({ workspace: dir, name: worker.name, path: worker.path }));
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [dir, worker.name, worker.path]);
  // While sync puts it on main, look again until the service has it, for
  // a while; a state waiting won't clear stops it (Check again is there).
  const polls = useRef(0);
  useEffect(() => {
    if (!waitsForSync(missing, polls.current)) return;
    const id = window.setTimeout(() => ((polls.current += 1), void refresh()), syncPollMs);
    return () => window.clearTimeout(id);
  }, [missing, refresh]);
  const push = async () => {
    setBusy(true);
    setError("");
    try {
      const r = await config.pushServiceJob({ workspace: dir, name: worker.name });
      if (r.job) show(r.job);
      if (r.job?.found) snack(t("desktop.service_job.pushed"));
    } catch (e) {
      setError(message(e));
    }
    setBusy(false);
  };
  useEffect(() => void refresh(), [refresh]);
  // Asked again as a run of this job changes, as the service's stream says.
  const { change, changes } = useServiceRuns();
  const jobId = job?.id;
  useEffect(() => {
    if (changes > 0 && jobId && change?.jobId === jobId) void refresh();
  }, [changes, change, jobId, refresh]);

  const act = async (f: () => Promise<unknown>) => {
    setError("");
    try {
      await f();
      await refresh();
    } catch (e) {
      setError(message(e));
    }
  };
  return (
    <>
      <p className="muted t-body-sm">{t("desktop.service_job.where")}</p>
      {job === null && missing && (
        <div className="stack" style={{ gap: 8 }}>
          <p className={missing.reason === "not_service" || missing.reason === "outside" ? "warning-text" : "muted"}>{whyNot(missing)}</p>
          {missing.reason === "not_pushed" ? (
            <span>
              <Button variant="filled" icon={mdiUploadOutline} disabled={busy} onClick={() => void push()}>
                {t("desktop.service_job.push")}
              </Button>
            </span>
          ) : (
            missing.reason !== "outside" && (
              <span>
                <Button icon={mdiRefresh} disabled={busy} onClick={() => void refresh()}>
                  {t("desktop.service_job.check")}
                </Button>
              </span>
            )
          )}
        </div>
      )}
      {job && !job.enabled && <p className="muted">{t("desktop.service_job.off")}</p>}
      {job?.nextRun && <p className="t-body-sm muted">{t("desktop.worker.next", { time: timestampDate(job.nextRun).toLocaleString(language()) })}</p>}
      <div className="row wrap">
        {job?.enabled && (
          <Button variant="filled" icon={mdiPlay} onClick={() => act(() => config.runServiceJob({ jobId: job.id }))}>
            {t("desktop.worker.run_now")}
          </Button>
        )}
        {job && (
          <Button variant={job.enabled ? "outlined" : "filled"} onClick={() => act(() => config.setServiceJobEnabled({ jobId: job.id, enabled: !job.enabled }))}>
            {t(job.enabled ? "desktop.service_job.turn_off" : "desktop.service_job.turn_on")}
          </Button>
        )}
        <Button variant="outlined" icon={mdiPencilOutline} onClick={onEdit} title={worker.path}>
          {t("desktop.worker.edit")}
        </Button>
        <Button danger icon={mdiDeleteOutline} onClick={onDelete}>
          {t("desktop.workers.delete")}
        </Button>
        <IconButton icon={mdiRefresh} label={t("desktop.refresh")} small onClick={() => void refresh()} />
      </div>
      {error && <p className="error-text">{error}</p>}
      <h3 className="t-title">{t("desktop.worker.runs")}</h3>
      {job !== undefined && runs.length === 0 && <p className="muted t-body-sm">{t("desktop.worker.no_runs")}</p>}
      <div className="runs">
        {runs.map((r) => (
          <ServiceRunRow
            key={r.id}
            run={r}
            dir={dir}
            jobId={job?.id ?? ""}
            jobName={worker.name}
            onSaved={(path) => snack(t("desktop.service_job.saved", { path }))}
            onError={(e) => snack(e, { error: true })}
            onAnswered={(msg) => (snack(msg), void refresh())}
          />
        ))}
      </div>
    </>
  );
}

/** One of the service's runs: how it went, its branch, its files on request, and a paused one's Approve and Deny (EN-43). */
function ServiceRunRow({
  run,
  dir,
  jobId,
  jobName,
  onSaved,
  onError,
  onAnswered,
}: {
  run: ServiceRun;
  dir: string;
  jobId: string;
  jobName: string;
  onSaved: (path: string) => void;
  onError: (e: string) => void;
  onAnswered: (message: string) => void;
}) {
  const [files, setFiles] = useState<ServiceRunFile[] | null>(null);
  const [note, setNote] = useState("");
  const [answering, setAnswering] = useState(false);
  const answer = async (approve: boolean) => {
    setAnswering(true);
    try {
      await config.answerServiceRun({ runId: run.id, approve, note: note.trim() });
      onAnswered(t(approve ? "desktop.approvals.approved" : "desktop.approvals.denied", { job: jobName }));
    } catch (e) {
      onError(message(e));
    }
    setAnswering(false);
  };
  const failed = run.status === "failed";
  // Add to my workspace (JB-10): its changes, kept on its branch, onto main.
  const [taking, setTaking] = useState(false);
  const take = async () => {
    setTaking(true);
    try {
      await config.takeServiceRun({ workspace: dir, jobId, runId: run.id });
      onAnswered(t("desktop.service_job.taken", { job: jobName }));
    } catch (e) {
      onError(reason(e) === "CONFLICTS" ? t("desktop.service_job.take_conflicts", { branch: run.branch }) : message(e));
    }
    setTaking(false);
  };
  const showFiles = async () => {
    if (files) return setFiles(null);
    try {
      setFiles((await config.listServiceRunFiles({ artifacts: run.artifacts })).files);
    } catch (e) {
      onError(message(e));
    }
  };
  const save = async (f: ServiceRunFile) => {
    try {
      onSaved((await config.saveServiceRunFile({ urn: f.urn })).path);
    } catch (e) {
      onError(message(e));
    }
  };
  return (
    <div className="run">
      <Icon path={statusIcon[run.status] ?? mdiCloseCircleOutline} className={failed ? "error-text" : "muted"} />
      <div className="stack" style={{ gap: 2 }}>
        <span>
          {t(`desktop.service_job.status.${run.status}`)} · {run.trigger === "now" ? t("desktop.worker.manual") : t("desktop.worker.scheduled")}
          {run.costUsd > 0 ? ` · $${run.costUsd.toFixed(4)}` : ""}
        </span>
        {run.created && <span className="t-body-sm muted">{timestampDate(run.created).toLocaleString(language())}</span>}
        {run.output && <span className="t-body-sm live-text">{run.output}</span>}
        {run.status === "paused" && run.pending ? (
          <span className="stack" style={{ gap: 6 }}>
            <span className="t-body-sm warning-text">{t("desktop.approvals.waits", { tool: run.pending.tool, detail: run.pending.detail })}</span>
            <input className="input" value={note} disabled={answering} placeholder={t("desktop.approvals.note")} aria-label={t("desktop.approvals.note")} onChange={(e) => setNote(e.target.value)} />
            <span className="row" style={{ gap: 8 }}>
              <Button small variant="filled" icon={mdiCheck} disabled={answering} onClick={() => void answer(true)}>
                {t("desktop.approvals.approve")}
              </Button>
              <Button small danger icon={mdiClose} disabled={answering} onClick={() => void answer(false)}>
                {t("desktop.approvals.deny")}
              </Button>
            </span>
          </span>
        ) : (
          run.note && <span className={`t-body-sm ${failed ? "error-text" : "muted"}`}>{run.note}</span>
        )}
        {run.branch && run.merged && (
          <span className="t-body-sm muted row" style={{ gap: 4 }}>
            <Icon path={mdiSourceMerge} size="sm" /> {t("desktop.service_job.on_main")}
          </span>
        )}
        {run.branch && !run.merged && (
          <span className="stack" style={{ gap: 4 }}>
            <span className="t-body-sm muted row" style={{ gap: 4 }}>
              <Icon path={mdiSourceBranch} size="sm" /> {t("desktop.service_job.branch", { branch: run.branch })}
            </span>
            {run.mergeNote && <span className="t-body-sm muted">{run.mergeNote}</span>}
            <span>
              <Button small variant="filled" icon={mdiSourceMerge} disabled={taking || !jobId} onClick={() => void take()}>
                {t("desktop.service_job.take")}
              </Button>
            </span>
          </span>
        )}
        {run.artifacts && run.status !== "queued" && (
          <span>
            <Button small icon={mdiFileOutline} onClick={() => void showFiles()}>
              {t("desktop.service_job.files")}
            </Button>
          </span>
        )}
        {files?.length === 0 && <span className="t-body-sm muted">{t("desktop.service_job.no_files")}</span>}
        {files?.map((f) => (
          <span key={f.urn} className="row t-body-sm" style={{ gap: 4 }}>
            <code className="ellipsis spacer">{f.path}</code>
            <span className="muted">{formatBytes(f.size)}</span>
            <IconButton icon={mdiDownload} label={t("desktop.service_job.save", { name: f.path })} small onClick={() => void save(f)} />
          </span>
        ))}
      </div>
    </div>
  );
}
