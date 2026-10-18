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

import { mdiCalendarClock, mdiCalendarPlus, mdiCheck, mdiDeleteOutline, mdiPencilOutline } from "@mdi/js";
import { useCallback, useEffect, useState } from "react";
import { workers } from "../api";
import { message } from "../errors";
import { WorkerState, type Worker } from "../gen/blitz/v1/worker_pb";
import { t } from "../i18n";
import { Button, Field, Icon, IconButton, Switch, useSnackbar } from "../ui/controls";

/** A new schedule's worker's name: the workflow's, then -2, -3, … past those taken. */
export function scheduleName(workflow: string, taken: string[]): string {
  const base = `${workflow}-schedule`.replace(/[^a-z0-9_-]/g, "-");
  let name = base;
  for (let n = 2; taken.includes(name); n++) name = `${base}-${n}`;
  return name;
}

/** When a scheduled worker runs next, in the user's words. */
const next = (w: Worker) => (w.nextRun ? new Date(Number(w.nextRun.seconds) * 1000).toLocaleString(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit", month: "short", day: "numeric" }) : "");

type Draft = { name: string; schedule: string; input: string };

/**
 * When a workflow runs by itself (spec_workflows_043 WF-20): its schedules,
 * each a worker that runs it (a deterministic worker: the workflow's steps,
 * its prompt the workflow's input), added, changed, switched on and off
 * and removed here. What a worker may do and its limits are in Studio ›
 * Workers.
 */
export function WorkflowSchedule({ workspace, workflow, disabled }: { workspace: string; workflow: string; disabled: boolean }) {
  const snack = useSnackbar();
  const [all, setAll] = useState<Worker[]>([]);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const mine = all.filter((w) => w.workflow === workflow && workflow);

  const refresh = useCallback(async () => {
    try {
      setAll((await workers.listWorkers({ workspace })).workers);
    } catch {
      setAll([]); // no scheduler: the section says nothing's scheduled
    }
  }, [workspace]);
  useEffect(() => void refresh(), [refresh]);

  const act = async (f: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await f();
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy(false);
    }
  };
  const save = () =>
    act(async () => {
      if (!draft) return;
      setProblems([]);
      if (!draft.name) {
        const r = await workers.createWorker({ workspace, name: scheduleName(workflow, all.map((w) => w.name)), description: t("desktop.flows.schedule.description", { name: workflow }), schedule: draft.schedule.trim(), workflow, prompt: draft.input });
        if (!r.worker) return setProblems(r.problems);
        await workers.enableWorker({ workspace, name: r.worker.name, hash: r.worker.hash });
        snack(t("desktop.flows.schedule.added", { when: r.worker.schedule }));
      } else {
        const spec = await workers.getWorkerSpec({ workspace, name: draft.name });
        const was = all.find((w) => w.name === draft.name)?.state === WorkerState.ENABLED;
        const { $typeName: _, ...def } = spec.worker ?? { $typeName: "" };
        const r = await workers.updateWorker({ workspace, hash: spec.hash, worker: { ...def, schedule: draft.schedule.trim(), prompt: draft.input } });
        if (!r.worker) return setProblems(r.problems);
        if (was) await workers.enableWorker({ workspace, name: r.worker.name, hash: r.worker.hash });
      }
      setDraft(null);
    });
  const edit = (w: Worker) =>
    act(async () => {
      const spec = await workers.getWorkerSpec({ workspace, name: w.name });
      setProblems([]);
      setDraft({ name: w.name, schedule: spec.worker?.schedule ?? w.schedule, input: spec.worker?.prompt ?? "" });
    });
  const flip = (w: Worker, on: boolean) => act(() => (on ? workers.enableWorker({ workspace, name: w.name, hash: w.hash }) : workers.disableWorker({ workspace, name: w.name })));
  const remove = (w: Worker) => act(() => workers.deleteWorker({ workspace, name: w.name }));

  return (
    <div className="stack flow-schedule" style={{ gap: 6 }}>
      <span className="t-label">{t("desktop.flows.schedule.title")}</span>
      <span className="t-body-sm muted">{t(disabled ? "desktop.flows.schedule.save_first" : "desktop.flows.schedule.hint")}</span>
      {mine.map((w) => (
        <div key={w.name} className="row flow-schedule-row" style={{ gap: 6 }}>
          <Icon path={mdiCalendarClock} className={w.state === WorkerState.INVALID ? "error-text" : undefined} />
          <span className="lines spacer">
            <span className="ellipsis">{w.schedule || w.cron}</span>
            <small className={`ellipsis ${w.state === WorkerState.INVALID ? "error-text" : "muted"}`}>
              {w.state === WorkerState.INVALID ? w.problems[0] : w.state === WorkerState.ENABLED && next(w) ? t("desktop.flows.schedule.next", { when: next(w) }) : t("desktop.flows.schedule.off")}
            </small>
          </span>
          <Switch checked={w.state === WorkerState.ENABLED} label={t("desktop.flows.schedule.on", { when: w.schedule })} disabled={busy || w.state === WorkerState.INVALID} onChange={(v) => void flip(w, v)} />
          <IconButton icon={mdiPencilOutline} label={t("desktop.flows.schedule.edit")} small disabled={busy} onClick={() => void edit(w)} />
          <IconButton icon={mdiDeleteOutline} label={t("desktop.flows.schedule.remove", { when: w.schedule })} small disabled={busy} onClick={() => void remove(w)} />
        </div>
      ))}
      {draft ? (
        <div className="card outlined stack" style={{ gap: 8 }}>
          <Field label={t("desktop.flows.schedule.when")} supporting={t("desktop.flows.schedule.when_hint")}>
            {(id) => <input id={id} className="input" autoFocus value={draft.schedule} placeholder={t("desktop.flows.schedule.when_placeholder")} onChange={(e) => setDraft({ ...draft, schedule: e.target.value })} />}
          </Field>
          <Field label={t("desktop.flows.input")}>{(id) => <textarea id={id} className="input" rows={2} value={draft.input} placeholder={t("desktop.flows.input_placeholder")} onChange={(e) => setDraft({ ...draft, input: e.target.value })} />}</Field>
          {problems.map((p) => (
            <span key={p} className="t-body-sm error-text">
              {p}
            </span>
          ))}
          <div className="row" style={{ gap: 6, justifyContent: "flex-end" }}>
            <Button small onClick={() => setDraft(null)}>
              {t("desktop.cancel")}
            </Button>
            <Button small variant="filled" icon={mdiCheck} disabled={busy || !draft.schedule.trim()} onClick={() => void save()}>
              {t(draft.name ? "desktop.flows.schedule.save" : "desktop.flows.schedule.add")}
            </Button>
          </div>
        </div>
      ) : (
        <div className="row">
          <Button small icon={mdiCalendarPlus} disabled={disabled || busy} onClick={() => setDraft({ name: "", schedule: "", input: "" })}>
            {t("desktop.flows.schedule.add")}
          </Button>
        </div>
      )}
    </div>
  );
}
