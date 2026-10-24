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
import { createPortal } from "react-dom";
import { mdiAlertCircleOutline, mdiCreationOutline, mdiRefresh } from "@mdi/js";
import { workers, workspaces } from "./api";
import { message, reason } from "./errors";
import { t } from "./i18n";
import { ModelInput, useModelCatalog } from "./ModelInput";
import { PermissionRules } from "./PermissionRules";
import { SchedulePicker } from "./SchedulePicker";
import { Button, Dialog, Icon, Segmented, useSnackbar } from "./ui/controls";
import { applyRewrite, canSave, edited, emptyWorkerForm, footerNote, formFromWorker, limitHints, permissionChanges, validWorkerName, workerName, workerRequest, type WorkerForm } from "./workerForm";
import type { RewriteWorkerResponse, WorkerPolicySettings } from "./gen/blitz/v1/worker_pb";

// An example of the permissions field (syntax, not prose).
/** The worker a dialog edits: its name, its WORKER.md, and whether saving pauses it. */
export interface WorkerEdit {
  name: string;
  path: string;
  pauses: boolean;
}

/**
 * A worker's form, new or existing (edit), in the worker dialog's edit
 * view: a field for each of
 * WORKER.md's frontmatter settings and the workflow. The service checks it
 * as the scheduler would read it before writing
 * .agents/workers/<name>/WORKER.md, and says what's wrong otherwise. A new
 * worker waits to be reviewed and enabled; an edited one keeps its name,
 * is saved only over the file as it was loaded, and an enabled one waits
 * to be enabled again. Its buttons (Cancel, Save or Create) go in footer,
 * the drawer's footer, when given. onDirty hears whether the form holds
 * changes that aren't saved, so closing can ask first.
 */
export function WorkerEditor({
  dir,
  edit,
  footer,
  onCancel,
  onSaved,
  onDirty,
}: {
  dir: string;
  edit?: WorkerEdit;
  footer: HTMLElement | null;
  onCancel: () => void;
  onSaved: (name: string) => void;
  onDirty?: (dirty: boolean) => void;
}) {
  const snack = useSnackbar();
  const [f, setF] = useState<WorkerForm>({ ...emptyWorkerForm, name: edit?.name ?? "", timezone: edit ? "" : (Intl.DateTimeFormat().resolvedOptions().timeZone ?? "") });
  const [named, setNamed] = useState(!!edit); // the name was typed (or is fixed), not made from the description
  const [agents, setAgents] = useState<string[]>([]);
  const [flows, setFlows] = useState<string[]>([]);
  const catalog = useModelCatalog(dir);
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  // The host's limits for every job, for the limit fields' defaults and caps.
  const [policy, setPolicy] = useState<WorkerPolicySettings>();
  useEffect(() => {
    workers.listAllWorkers({}).then(
      (r) => setPolicy(r.policy),
      () => undefined, // the fields then say "Default"
    );
  }, []);
  // Editing: the file as loaded (its hash, and the form it made), and
  // whether it changed on disk since.
  const [hash, setHash] = useState("");
  const [loaded, setLoaded] = useState<WorkerForm | null>(null);
  const [stale, setStale] = useState(false);
  const [confirmReload, setConfirmReload] = useState(false);
  // Rewrite with AI: the model's suggestion, shown before it's taken.
  const [rewriting, setRewriting] = useState(false);
  const [suggestion, setSuggestion] = useState<RewriteWorkerResponse | null>(null);
  const [takePermissions, setTakePermissions] = useState(true);
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
    workspaces.listWorkflows({ workspace: dir }).then(
      (r) => setFlows(r.workflows.map((w) => w.name)),
      () => {},
    );
  }, [dir]);
  // Unsaved: an edit that differs from the file as loaded, or a new
  // worker's form that differs from the blank one it started as.
  const [blank] = useState(f);
  const dirty = edited(f, edit ? loaded : blank);
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);
  useEffect(() => () => onDirty?.(false), [onDirty]);
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
    } catch (e) {
      if (edit && reason(e) === "HASH_MISMATCH") setStale(true);
      else setProblems([message(e)]);
    } finally {
      setBusy(false);
    }
  };
  const rewrite = async () => {
    setRewriting(true);
    try {
      const res = await workers.rewriteWorker({ workspace: dir, worker: workerRequest(f) });
      if (res.problem) snack(t("desktop.worker.rewrite_failed", { problem: res.problem }), { error: true });
      else {
        setTakePermissions(true);
        setSuggestion(res);
      }
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setRewriting(false);
    }
  };
  const perms = suggestion ? permissionChanges(f, suggestion.permissions) : { added: [], removed: [] };
  const permsDiffer = perms.added.length + perms.removed.length > 0;

  // Reloading the file from disk drops the form's edits: asked first when there are some.
  const reload = () => (edited(f, loaded) ? setConfirmReload(true) : void load());

  const hints = limitHints(policy);
  const limitProps = (h: { def: string; cap: string }) => ({
    placeholder: h.def ? t("desktop.newworker.default_value", { value: h.def }) : t("desktop.newworker.policy_default"),
    hint: h.cap ? t("desktop.newworker.capped_at", { value: h.cap }) : undefined,
  });
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
  const actions = (
    <>
      <span className="t-body-sm muted spacer">{t(note.key, { path: note.path })}</span>
      <Button onClick={onCancel}>{t("desktop.cancel")}</Button>
      <Button variant="filled" disabled={!canSave(f, { busy, editing: !!edit, loaded: !!loaded, stale })} onClick={save}>
        {edit ? t("desktop.editworker.save") : t("desktop.newworker.create")}
      </Button>
    </>
  );
  return (
    <>
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
        <SchedulePicker schedule={f.schedule} timezone={f.timezone} onSchedule={(v) => set("schedule", v)} onTimezone={(v) => set("timezone", v)} />
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
          <span className="t-label">{t("desktop.newworker.workflow")}</span>
          <select className="select" value={f.workflow} onChange={(e) => set("workflow", e.target.value)}>
            <option value="">{t("desktop.newworker.workflow_none")}</option>
            {[...new Set([...flows, f.workflow].filter(Boolean))].map((n) => (
              <option key={n} value={n}>
                /{n}
              </option>
            ))}
          </select>
          <span className="supporting">{t(f.workflow ? "desktop.newworker.workflow_hint" : "desktop.newworker.workflow_none_hint")}</span>
        </label>
        <div className="field">
          <label className="t-label" htmlFor="worker-prompt">
            {t(f.workflow ? "desktop.newworker.input" : "desktop.newworker.prompt")}
          </label>
          {rewriting && <div className="progress" role="progressbar" aria-label={t("desktop.worker.rewriting")} />}
          <textarea
            id="worker-prompt"
            className="input new-worker-prompt"
            value={f.prompt}
            placeholder={t(f.workflow ? "desktop.newworker.input_placeholder" : "desktop.newworker.prompt_placeholder")}
            rows={f.workflow ? 3 : 8}
            onChange={(e) => set("prompt", e.target.value)}
          />
          <span>
            <Button small icon={mdiCreationOutline} disabled={rewriting || !f.prompt.trim()} title={t("desktop.worker.rewrite_hint")} onClick={() => void rewrite()}>
              {rewriting ? t("desktop.worker.rewriting") : t("desktop.worker.rewrite")}
            </Button>
          </span>
        </div>
        <div className="field">
          <span className="t-label">{t("desktop.newworker.permissions")}</span>
          <PermissionRules value={f.permissions} onChange={(v) => set("permissions", v)} allow={policy?.allow} />
          <span className="t-body-sm muted">{t("desktop.newworker.permissions_hint")}</span>
        </div>
        {/* The host's defaults fill in what's left empty; a job may ask for more, up to a cap the host set (WK-73). */}
        <div className="triple">
          {field("maxTurns", { ...limitProps(hints.maxTurns), type: "number" })}
          {field("maxCostUSD", { ...limitProps(hints.maxCostUSD), type: "number" })}
          {field("timeout", { ...limitProps(hints.timeout), mono: true })}
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
        {suggestion && (
          <Dialog
            wide
            title={t("desktop.worker.rewrite_title")}
            icon={mdiCreationOutline}
            onClose={() => setSuggestion(null)}
            footer={
              <>
                <Button onClick={() => setSuggestion(null)}>{t("desktop.worker.rewrite_keep")}</Button>
                <Button
                  variant="filled"
                  onClick={() => {
                    setF((x) => applyRewrite(x, suggestion, permsDiffer && takePermissions));
                    setSuggestion(null);
                  }}
                >
                  {t("desktop.worker.rewrite_use")}
                </Button>
              </>
            }
          >
            <div className="stack rewrite-review">
              {suggestion.changes.length > 0 && (
                <ul className="rewrite-changes t-body-sm">
                  {suggestion.changes.map((c) => (
                    <li key={c}>{c}</li>
                  ))}
                </ul>
              )}
              <textarea className="input mono rewrite-prompt" readOnly rows={11} value={suggestion.prompt} aria-label={t("desktop.worker.rewrite_title")} />
              {permsDiffer && (
                <div className="stack" style={{ gap: 4 }}>
                  <label className="row check">
                    <input type="checkbox" checked={takePermissions} onChange={(e) => setTakePermissions(e.target.checked)} />
                    <span>{t("desktop.worker.rewrite_permissions")}</span>
                  </label>
                  {perms.added.map((p) => (
                    <code key={"+" + p} className="t-body-sm add-count">
                      + {p}
                    </code>
                  ))}
                  {perms.removed.map((p) => (
                    <code key={"-" + p} className="t-body-sm del-count">
                      − {p}
                    </code>
                  ))}
                </div>
              )}
              <p className="t-body-sm muted">{t("desktop.worker.rewrite_note")}</p>
            </div>
          </Dialog>
        )}
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
      {footer ? createPortal(actions, footer) : <div className="row worker-editor-actions">{actions}</div>}
    </>
  );
}
