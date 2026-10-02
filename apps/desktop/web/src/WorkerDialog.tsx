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

import { useEffect, useRef, useState } from "react";
import { mdiAlertCircleOutline, mdiCalendarEdit, mdiCalendarPlus, mdiRefresh } from "@mdi/js";
import { workers, workspaces } from "./api";
import { message, reason } from "./errors";
import { t } from "./i18n";
import { ModelInput, useModelCatalog } from "./ModelInput";
import { Button, Dialog, Icon, Segmented, useSnackbar } from "./ui/controls";
import { canSave, edited, emptyWorkerForm, footerNote, formFromWorker, validWorkerName, workerName, workerRequest, type WorkerForm } from "./workerForm";

// An example of the permissions field (syntax, not prose).
const permissionsExample = ["shell:go test ./...", "write:reports/*"].join("\n");

/** The worker a dialog edits: its name, its WORKER.md, and whether saving pauses it. */
export interface WorkerEdit {
  name: string;
  path: string;
  pauses: boolean;
}

/**
 * A form for a worker, new or existing (edit): a field for each of
 * WORKER.md's frontmatter settings and the workflow. The service checks it
 * as the scheduler would read it before writing
 * .agents/workers/<name>/WORKER.md, and says what's wrong otherwise. A new
 * worker waits to be reviewed and enabled; an edited one keeps its name,
 * is saved only over the file as it was loaded, and an enabled one waits
 * to be enabled again.
 */
export function WorkerDialog({ dir, edit, onClose, onSaved }: { dir: string; edit?: WorkerEdit; onClose: () => void; onSaved: (name: string) => void }) {
  const snack = useSnackbar();
  const [f, setF] = useState<WorkerForm>({ ...emptyWorkerForm, name: edit?.name ?? "", timezone: edit ? "" : (Intl.DateTimeFormat().resolvedOptions().timeZone ?? "") });
  const [named, setNamed] = useState(!!edit); // the name was typed (or is fixed), not made from the description
  const [agents, setAgents] = useState<string[]>([]);
  const catalog = useModelCatalog(dir);
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  // Editing: the file as loaded (its hash, and the form it made), and
  // whether it changed on disk since.
  const [hash, setHash] = useState("");
  const [loaded, setLoaded] = useState<WorkerForm | null>(null);
  const [stale, setStale] = useState(false);
  const [confirmReload, setConfirmReload] = useState(false);
  const load = async () => {
    if (!edit) return;
    try {
      const r = await workers.getWorkerSpec({ workspace: dir, name: edit.name });
      const form = formFromWorker(r.worker!);
      setF(form);
      setLoaded(form);
      setHash(r.hash);
      setStale(false);
      setProblems([]);
    } catch (e) {
      setProblems([message(e)]);
    }
  };
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per worker
  }, [dir, edit?.name]);
  // The problems are at the end of a long form: show them.
  const alert = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (problems.length) alert.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [problems]);
  useEffect(() => {
    workspaces.listAgents({ workspace: dir }).then(
      (r) => setAgents(r.agents.map((a) => a.name)),
      () => {},
    );
  }, [dir]);
  const set = <K extends keyof WorkerForm>(k: K, v: WorkerForm[K]) => setF((x) => ({ ...x, [k]: v }));
  const note = footerNote(f, edit);

  const save = async () => {
    if (!canSave(f, { busy, editing: !!edit, loaded: !!loaded, stale })) return;
    setBusy(true);
    setProblems([]);
    try {
      const res = edit
        ? await workers.updateWorker({ workspace: dir, hash, worker: workerRequest(f) })
        : await workers.createWorker({ workspace: dir, ...workerRequest(f) });
      if (res.problems.length) {
        setProblems(res.problems);
        return;
      }
      snack(edit ? t(edit.pauses ? "desktop.editworker.saved_paused" : "desktop.editworker.saved", { name: f.name.trim() }) : t("desktop.newworker.created", { name: f.name.trim() }));
      onSaved(f.name.trim());
      onClose();
    } catch (e) {
      if (edit && reason(e) === "HASH_MISMATCH") setStale(true);
      else setProblems([message(e)]);
    } finally {
      setBusy(false);
    }
  };
  // Reloading the file from disk drops the form's edits: asked first when there are some.
  const reload = () => (edited(f, loaded) ? setConfirmReload(true) : void load());

  const field = (key: keyof WorkerForm, props: { hint?: string; placeholder?: string; mono?: boolean; type?: string }) => (
    <label className="field">
      <span className="t-label">{t(`desktop.newworker.${key}`)}</span>
      <input
        className={`input ${props.mono ? "mono" : ""}`}
        type={props.type ?? "text"}
        value={f[key] as string}
        placeholder={props.placeholder}
        spellCheck={false}
        onChange={(e) => set(key, e.target.value)}
      />
      {props.hint && <span className="t-body-sm muted">{props.hint}</span>}
    </label>
  );
  return (
    <Dialog
      title={edit ? t("desktop.editworker.title", { name: edit.name }) : t("desktop.newworker.title")}
      icon={edit ? mdiCalendarEdit : mdiCalendarPlus}
      onClose={onClose}
      wide
      footer={
        <>
          <span className="t-body-sm muted spacer">{t(note.key, { path: note.path })}</span>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" disabled={!canSave(f, { busy, editing: !!edit, loaded: !!loaded, stale })} onClick={save}>
            {edit ? t("desktop.editworker.save") : t("desktop.newworker.create")}
          </Button>
        </>
      }
    >
      <div className="stack new-worker" style={{ gap: 14 }}>
        {stale && (
          <div className="card error row" role="alert" style={{ gap: 8 }}>
            <Icon path={mdiAlertCircleOutline} size="sm" />
            <span className="spacer t-body-sm">{t("desktop.editworker.stale", { path: edit?.path ?? "" })}</span>
            <Button small icon={mdiRefresh} onClick={reload}>
              {t("desktop.editworker.reload")}
            </Button>
          </div>
        )}
        <div className="pair">
          <label className="field">
            <span className="t-label">{t("desktop.newworker.description")}</span>
            <input
              className="input"
              value={f.description}
              placeholder={t("desktop.newworker.description_placeholder")}
              onChange={(e) => setF((x) => ({ ...x, description: e.target.value, name: named ? x.name : workerName(e.target.value) }))}
            />
          </label>
          <label className="field">
            <span className="t-label">{t("desktop.newworker.name")}</span>
            <input
              className="input mono"
              value={f.name}
              spellCheck={false}
              readOnly={!!edit}
              aria-invalid={!!f.name && !validWorkerName(f.name.trim())}
              onChange={(e) => {
                setNamed(true);
                set("name", e.target.value);
              }}
            />
            <span className={`t-body-sm ${f.name && !validWorkerName(f.name.trim()) ? "error-text" : "muted"}`}>{t(edit ? "desktop.editworker.name_fixed" : "desktop.newworker.name_hint")}</span>
          </label>
        </div>
        <div className="pair">
          {field("schedule", { placeholder: "Weekdays at 9:30", hint: t("desktop.newworker.schedule_hint") })}
          {field("timezone", { placeholder: "Europe/Paris", hint: t("desktop.newworker.timezone_hint") })}
        </div>
        <div className="pair">
          <label className="field">
            <span className="t-label">{t("desktop.newworker.agent")}</span>
            <select className="select" value={f.agent} onChange={(e) => set("agent", e.target.value)}>
              <option value="">{t("desktop.newworker.agent_default")}</option>
              {agents.map((a) => (
                <option key={a} value={a}>
                  {a}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span className="t-label">{t("desktop.newworker.model")}</span>
            <ModelInput value={f.model} onChange={(v) => set("model", v)} catalog={catalog} placeholder={t("desktop.newworker.model_placeholder")} />
          </label>
        </div>
        <label className="field">
          <span className="t-label">{t("desktop.newworker.prompt")}</span>
          <textarea className="input new-worker-prompt" value={f.prompt} placeholder={t("desktop.newworker.prompt_placeholder")} rows={8} onChange={(e) => set("prompt", e.target.value)} />
        </label>
        <label className="field">
          <span className="t-label">{t("desktop.newworker.permissions")}</span>
          <textarea className="input mono" value={f.permissions} placeholder={permissionsExample} rows={3} spellCheck={false} onChange={(e) => set("permissions", e.target.value)} />
          <span className="t-body-sm muted">{t("desktop.newworker.permissions_hint")}</span>
        </label>
        <div className="triple">
          {field("maxTurns", { placeholder: t("desktop.newworker.policy_default"), type: "number" })}
          {field("maxCostUSD", { placeholder: t("desktop.newworker.policy_default"), type: "number" })}
          {field("timeout", { placeholder: "20m", mono: true })}
        </div>
        <div className="row" style={{ gap: 12 }}>
          <span className="t-label">{t("desktop.newworker.catchUp")}</span>
          <Segmented
            small
            label={t("desktop.newworker.catchUp")}
            value={f.catchUp}
            onChange={(v) => set("catchUp", v)}
            options={[
              { value: "none", label: t("desktop.newworker.catchup_none") },
              { value: "once", label: t("desktop.newworker.catchup_once") },
            ]}
          />
          <span className="t-body-sm muted">{t("desktop.newworker.catchup_hint")}</span>
        </div>
        {confirmReload && (
          <Dialog
            title={t("desktop.editworker.reload_title")}
            onClose={() => setConfirmReload(false)}
            footer={
              <>
                <Button onClick={() => setConfirmReload(false)}>{t("desktop.cancel")}</Button>
                <Button
                  variant="filled"
                  danger
                  onClick={() => {
                    setConfirmReload(false);
                    void load();
                  }}
                >
                  {t("desktop.editworker.reload_discard")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.editworker.reload_body")}</p>
          </Dialog>
        )}
        {problems.length > 0 && (
          <div ref={alert} className="card error stack" role="alert" style={{ gap: 4 }}>
            {problems.map((p) => (
              <span key={p} className="row t-body-sm" style={{ gap: 6 }}>
                <Icon path={mdiAlertCircleOutline} size="sm" /> {p}
              </span>
            ))}
          </div>
        )}
      </div>
    </Dialog>
  );
}
