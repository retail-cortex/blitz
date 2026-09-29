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
import { mdiAlertCircleOutline, mdiCalendarPlus } from "@mdi/js";
import { workers, workspaces } from "./api";
import { message } from "./errors";
import { t } from "./i18n";
import { Button, Dialog, Icon, Segmented, useSnackbar } from "./ui/controls";
import { emptyWorkerForm, missing, validWorkerName, workerName, workerRequest, type WorkerForm } from "./workerForm";

// An example of the permissions field (syntax, not prose).
const permissionsExample = ["shell:go test ./...", "write:reports/*"].join("\n");

/**
 * A form for a new worker: a field for each of WORKER.md's frontmatter
 * settings and the workflow. The service checks it as the scheduler would
 * read it before writing .agents/workers/<name>/WORKER.md, and says what's
 * wrong otherwise. The new worker waits to be reviewed and enabled.
 */
export function NewWorkerDialog({ dir, onClose, onCreated }: { dir: string; onClose: () => void; onCreated: (name: string) => void }) {
  const snack = useSnackbar();
  const [f, setF] = useState<WorkerForm>({ ...emptyWorkerForm, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone ?? "" });
  const [named, setNamed] = useState(false); // the name was typed, not made from the description
  const [agents, setAgents] = useState<string[]>([]);
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
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
  const lack = missing(f);

  const create = async () => {
    if (lack || busy) return;
    setBusy(true);
    setProblems([]);
    try {
      const res = await workers.createWorker({ workspace: dir, ...workerRequest(f) });
      if (res.problems.length) {
        setProblems(res.problems);
        return;
      }
      snack(t("desktop.newworker.created", { name: f.name.trim() }));
      onCreated(f.name.trim());
      onClose();
    } catch (e) {
      setProblems([message(e)]);
    } finally {
      setBusy(false);
    }
  };

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
      title={t("desktop.newworker.title")}
      icon={mdiCalendarPlus}
      onClose={onClose}
      wide
      footer={
        <>
          <span className="t-body-sm muted spacer">{lack ? t(`desktop.newworker.needs.${lack}`) : t("desktop.newworker.where", { path: `.agents/workers/${f.name.trim()}/WORKER.md` })}</span>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" disabled={!!lack || busy} onClick={create}>
            {t("desktop.newworker.create")}
          </Button>
        </>
      }
    >
      <div className="stack new-worker" style={{ gap: 14 }}>
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
              aria-invalid={!!f.name && !validWorkerName(f.name.trim())}
              onChange={(e) => {
                setNamed(true);
                set("name", e.target.value);
              }}
            />
            <span className={`t-body-sm ${f.name && !validWorkerName(f.name.trim()) ? "error-text" : "muted"}`}>{t("desktop.newworker.name_hint")}</span>
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
          {field("model", { placeholder: t("desktop.newworker.model_placeholder"), mono: true })}
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
