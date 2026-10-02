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
import { mdiAlertCircleOutline, mdiChevronDown, mdiChevronRight, mdiClose, mdiContentSaveOutline, mdiDeleteOutline, mdiPlus, mdiRefresh, mdiRobotOutline, mdiUndoVariant } from "@mdi/js";
import { workspaces } from "./api";
import { message } from "./errors";
import { t } from "./i18n";
import { ModelInput, useModelCatalog } from "./ModelInput";
import { AgentScope, type AgentFile } from "./gen/blitz/v1/workspace_pb";
import { addTools, advancedCount, agentFromForm, agentPath, deleteTarget, emptyAgentForm, fileName, formFromAgent, invalidFields, sameForm, toolChoices, validAgentName, type AgentField, type AgentForm } from "./agentForm";
import { agencies, efforts, modes } from "./options";
import { Button, Dialog, Field, Icon, IconButton, Segmented, Switch, useSnackbar } from "./ui/controls";
import { workerName } from "./workerForm";

/** How often the open editor asks for the agent files again. */
const agentsPollMs = 5000;

/** What the editor shows: an agent file (by path), a new agent, or nothing. */
type Open = { path: string } | { path: "" } | null;

/**
 * The agents of one scope, the workspace's (.agents/agents) or the user's
 * (~/.blitz/agents): a list of their files, and a form for each of an
 * agent file's settings, its prompt and the model settings it runs with.
 * context is the workspace asked for the tools and models to offer (the
 * user's agents have none of their own; "" offers none, and tools are
 * typed).
 */
export function AgentEditor({ workspace, scope, context = workspace }: { workspace: string; scope: AgentScope; context?: string }) {
  const snack = useSnackbar();
  const [files, setFiles] = useState<AgentFile[]>([]);
  const [dir, setDir] = useState("");
  const [error, setError] = useState("");
  const [open, setOpen] = useState<Open>(null);
  // The form, and what it was when loaded or saved (to tell unsaved edits).
  const [form, setForm] = useState<AgentForm>(emptyAgentForm);
  const [base, setBase] = useState<AgentForm>(emptyAgentForm);
  const [named, setNamed] = useState(false); // the name was typed, not made from the display name
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [deleting, setDeleting] = useState(false);
  // A switch waiting for the user to drop unsaved edits.
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [tools, setTools] = useState<string[]>([]);
  const [models, setModels] = useState<string[]>([]);
  const catalog = useModelCatalog(context);

  const dirty = open !== null && !sameForm(form, base);
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;
  const openRef = useRef(open);
  openRef.current = open;

  const refresh = useCallback(async () => {
    try {
      const res = await workspaces.listAgentFiles({ workspace, scope });
      setFiles(res.files);
      setDir(res.dir);
      setError("");
      // The open file changed on disk: shown again, unless it has unsaved edits.
      const cur = openRef.current;
      if (cur?.path && !dirtyRef.current) {
        const f = res.files.find((x) => x.path === cur.path);
        if (!f) setOpen(null);
        else if (f.agent) {
          const loaded = formFromAgent(f.agent);
          setBase((b) => (sameForm(b, loaded) ? b : loaded));
          setForm((x) => (sameForm(x, loaded) ? x : loaded));
        }
      }
    } catch (e) {
      setError(message(e));
    }
  }, [workspace, scope]);
  // Files added or edited on disk show up: the list is asked for again
  // while the editor is open.
  useEffect(() => {
    void refresh();
    const timer = setInterval(refresh, agentsPollMs);
    return () => clearInterval(timer);
  }, [refresh]);

  // Every tool an agent can be given, and the models to offer, from the
  // context workspace.
  useEffect(() => {
    if (!context) return;
    workspaces.listTools({ workspace: context, all: true }).then(
      (r) => setTools(r.tools.map((x) => x.name)),
      () => {},
    );
    Promise.all([workspaces.listAgents({ workspace: context }), workspaces.getModelSettings({ workspace: context }), workspaces.getModel({ workspace: context })]).then(
      ([a, all, m]) => setModels([...new Set([m.provider ? `${m.provider}/${m.name}` : m.name, ...a.agents.map((x) => x.pinnedModel), ...Object.keys(all.all)].filter(Boolean))]),
      () => {},
    );
  }, [context]);

  // Runs go, asking first when there are unsaved edits.
  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());

  const show = (f: AgentFile) =>
    leave(() => {
      const loaded = f.agent ? formFromAgent(f.agent) : emptyAgentForm;
      setOpen({ path: f.path });
      setForm(loaded);
      setBase(loaded);
      setNamed(true);
      setProblems([]);
    });

  const startNew = () =>
    leave(() => {
      setOpen({ path: "" });
      setForm(emptyAgentForm);
      setBase(emptyAgentForm);
      setNamed(false);
      setProblems([]);
    });

  const file = open?.path ? files.find((f) => f.path === open.path) : undefined;
  // The agent the open file defines (a rename replaces it).
  const previous = file?.agent?.name ?? "";
  const invalid = invalidFields(form);
  const set = <K extends keyof AgentForm>(k: K, v: AgentForm[K]) => setForm((x) => ({ ...x, [k]: v }));

  const save = async () => {
    if (busy || invalid.length) return;
    setBusy(true);
    setProblems([]);
    try {
      const res = await workspaces.saveAgentFile({ workspace, scope, previousName: previous, agent: agentFromForm(form) });
      if (res.problems.length || !res.file) {
        setProblems(res.problems);
        return;
      }
      const saved = res.file.agent ? formFromAgent(res.file.agent) : form;
      setOpen({ path: res.file.path });
      setForm(saved);
      setBase(saved);
      snack(t("desktop.agents.saved", { name: saved.name }));
      await refresh();
    } catch (e) {
      setProblems([message(e)]);
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    setDeleting(false);
    try {
      await workspaces.deleteAgentFile({ workspace, scope, ...deleteTarget(file?.path ?? "", previous) });
      snack(t("desktop.agents.deleted", { name: previous || fileName(file?.path ?? "") }));
      setOpen(null);
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const where = agentPath(dir, form.name);
  const fieldError = (k: AgentField) => (invalid.includes(k) && (k !== "name" || form.name) && (k !== "description" || form.description || form.name) ? t(`desktop.agents.invalid.${k}`) : undefined);

  return (
    <div className={`split agent-editor ${scope === AgentScope.USER ? "agent-editor-user" : ""}`}>
      <aside className="split-side">
        <div className="row side-head">
          <span className="t-title-sm spacer">{t(scope === AgentScope.USER ? "desktop.agents.user_title" : "desktop.agents.workspace_title")}</span>
          <IconButton icon={mdiPlus} label={t("desktop.agents.new")} small onClick={startNew} />
          <IconButton icon={mdiRefresh} label={t("desktop.refresh")} small onClick={() => void refresh()} />
        </div>
        {dir && (
          <p className="t-body-sm muted agent-dir" title={dir}>
            {t("desktop.agents.in", { path: dir })}
          </p>
        )}
        {error && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
          </div>
        )}
        {files.length === 0 && !error && (
          <div className="stack" style={{ gap: 8 }}>
            <p className="muted t-body-sm">{t("desktop.agents.none")}</p>
            <Button small variant="tonal" icon={mdiPlus} onClick={startNew}>
              {t("desktop.agents.new")}
            </Button>
          </div>
        )}
        <div className="list">
          {open?.path === "" && (
            <button className="list-item active">
              <Icon path={mdiRobotOutline} />
              <span className="lines">
                <span className="ellipsis">{form.name.trim() || t("desktop.agents.unnamed")}</span>
                <small className="ellipsis">{t("desktop.agents.unsaved")}</small>
              </span>
            </button>
          )}
          {files.map((f) => {
            const name = fileName(f.path, f.agent?.name);
            return (
              <button key={f.path} className={`list-item ${open?.path === f.path ? "active" : ""} ${f.problem ? "has-problem" : ""}`} title={f.problem || f.path} onClick={() => show(f)}>
                <Icon path={f.problem ? mdiAlertCircleOutline : mdiRobotOutline} className={f.problem ? "error-text" : undefined} />
                <span className="lines">
                  <span className="ellipsis">{f.agent?.displayName || name}</span>
                  <small className={`ellipsis ${f.problem ? "error-text" : ""}`}>{f.problem ? t("desktop.agents.broken") : f.agent?.description}</small>
                </span>
              </button>
            );
          })}
        </div>
      </aside>
      <section className="split-main">
        {open === null && files.length > 0 && <p className="muted">{t("desktop.agents.choose")}</p>}
        {open !== null && file?.problem && !file.agent && (
          <div className="worker">
            <h2 className="t-headline">{fileName(file.path)}</h2>
            <div className="card error stack" style={{ gap: 4 }}>
              <span className="row" style={{ gap: 6 }}>
                <Icon path={mdiAlertCircleOutline} size="sm" /> {t("desktop.agents.broken_detail")}
              </span>
              <code className="t-body-sm">{file.problem}</code>
            </div>
            <p className="t-body-sm muted">{t("desktop.agents.broken_fix", { path: file.path })}</p>
            <div className="row">
              <Button danger icon={mdiDeleteOutline} onClick={() => setDeleting(true)}>
                {t("desktop.agents.delete")}
              </Button>
            </div>
          </div>
        )}
        {open !== null && !(file?.problem && !file.agent) && (
          <div className="worker agent-form">
            <div className="row">
              <h2 className="t-headline spacer ellipsis">{form.displayName.trim() || form.name.trim() || t("desktop.agents.new")}</h2>
              {dirty && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
            </div>
            {file?.problem && (
              <div className="card error row small-card">
                <Icon path={mdiAlertCircleOutline} size="sm" /> {file.problem}
              </div>
            )}
            <div className="pair">
              <Field label={t("desktop.agents.display_name")}>
                {(id) => (
                  <input
                    id={id}
                    className="input"
                    value={form.displayName}
                    placeholder={t("desktop.agents.display_name_placeholder")}
                    onChange={(e) => setForm((x) => ({ ...x, displayName: e.target.value, name: named ? x.name : workerName(e.target.value) }))}
                  />
                )}
              </Field>
              <Field label={t("desktop.agents.name")} supporting={t("desktop.agents.name_hint")} error={fieldError("name")}>
                {(id) => (
                  <input
                    id={id}
                    className="input mono"
                    value={form.name}
                    spellCheck={false}
                    aria-invalid={!!form.name && !validAgentName(form.name.trim())}
                    onChange={(e) => {
                      setNamed(true);
                      set("name", e.target.value);
                    }}
                  />
                )}
              </Field>
            </div>
            <Field label={t("desktop.agents.description")} supporting={t("desktop.agents.description_hint")} error={fieldError("description")}>
              {(id) => <input id={id} className="input" value={form.description} onChange={(e) => set("description", e.target.value)} />}
            </Field>
            <div className="pair">
              <Field label={t("desktop.rs.model")}>
                {(id) => (
                  <ModelInput
                    id={id}
                    value={form.defaultModel}
                    onChange={(v) => set("defaultModel", v)}
                    catalog={catalog}
                    extras={models}
                    placeholder={t("desktop.agents.model_placeholder")}
                    hint={t("desktop.agents.model_hint")}
                  />
                )}
              </Field>
              <Field label={t("desktop.rs.agency")} supporting={agencies().find((a) => a.value === (form.agencyLevel || "high"))?.detail}>
                {(id) => (
                  <select id={id} className="select" value={form.agencyLevel} onChange={(e) => set("agencyLevel", e.target.value)}>
                    <option value="">{t("desktop.agents.agency_default")}</option>
                    {agencies().map((a) => (
                      <option key={a.value} value={a.value}>
                        {a.label}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
            </div>
            <div className="pair">
              <Field label={t("desktop.rs.mode")} supporting={form.permissionMode ? modes().find((m) => m.value === form.permissionMode)?.detail : t("desktop.agents.mode_hint")}>
                {(id) => (
                  <select id={id} className="select" value={form.permissionMode} onChange={(e) => set("permissionMode", e.target.value)}>
                    <option value="">{t("desktop.agents.mode_default")}</option>
                    {modes().map((m) => (
                      <option key={m.value} value={m.value}>
                        {m.label}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
              <Field label={t("desktop.agents.max_turns")} supporting={t("desktop.agents.max_turns_hint")} error={fieldError("maxTurns")}>
                {(id) => <input id={id} className="input" type="number" min={0} step={1} value={form.maxTurns} placeholder={t("desktop.agents.max_turns_placeholder")} onChange={(e) => set("maxTurns", e.target.value)} />}
              </Field>
            </div>
            <div className="row wrap agent-switches">
              <Switch checked={form.background} label={t("desktop.agents.background")} onChange={(v) => set("background", v)} />
              <span className="t-label">{t("desktop.agents.background")}</span>
              <span className="spacer" />
              <span className="t-label">{t("desktop.agents.isolation")}</span>
              <Segmented
                small
                label={t("desktop.agents.isolation")}
                value={form.isolation || "none"}
                onChange={(v) => set("isolation", v === "worktree" ? "worktree" : "")}
                options={[
                  { value: "none", label: t("desktop.agents.isolation_none") },
                  { value: "worktree", label: t("desktop.agents.isolation_worktree") },
                ]}
              />
            </div>
            <ToolPicker offered={tools} chosen={form.tools} onChange={(v) => set("tools", v)} />
            <Field label={t("desktop.agents.prompt")} supporting={t("desktop.agents.prompt_hint", { placeholder: "{agency_instructions}" })}>
              {(id) => <textarea id={id} className="input mono agent-prompt" value={form.prompt} rows={14} spellCheck={false} placeholder={t("desktop.agents.prompt_placeholder")} onChange={(e) => set("prompt", e.target.value)} />}
            </Field>
            <Advanced form={form} set={set} fieldError={fieldError} />
            {problems.length > 0 && (
              <div className="card error stack" role="alert" style={{ gap: 4 }}>
                {problems.map((p) => (
                  <span key={p} className="row t-body-sm" style={{ gap: 6 }}>
                    <Icon path={mdiAlertCircleOutline} size="sm" /> {p}
                  </span>
                ))}
              </div>
            )}
            <div className="row wrap agent-actions">
              <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || invalid.length > 0 || (!dirty && open.path !== "")} onClick={() => void save()}>
                {t("desktop.agents.save")}
              </Button>
              {dirty && open.path !== "" && (
                <Button icon={mdiUndoVariant} onClick={() => setForm(base)}>
                  {t("desktop.agents.revert")}
                </Button>
              )}
              {open.path === "" && (
                <Button icon={mdiClose} onClick={() => setOpen(null)}>
                  {t("desktop.cancel")}
                </Button>
              )}
              {previous && (
                <Button danger icon={mdiDeleteOutline} onClick={() => setDeleting(true)}>
                  {t("desktop.agents.delete")}
                </Button>
              )}
              <span className="t-body-sm muted spacer ellipsis agent-where" title={where}>
                {where && t("desktop.agents.where", { path: where })}
              </span>
            </div>
          </div>
        )}
      </section>
      {deleting && (
        <Dialog
          title={t("desktop.agents.delete_title", { name: previous || fileName(file?.path ?? "") })}
          icon={mdiDeleteOutline}
          onClose={() => setDeleting(false)}
          footer={
            <>
              <Button onClick={() => setDeleting(false)}>{t("desktop.cancel")}</Button>
              <Button variant="filled" danger onClick={() => void remove()}>
                {t("desktop.agents.delete")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.agents.delete_body", { path: file?.path ?? "" })}</p>
        </Dialog>
      )}
      {pending && (
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
          <p>{t("desktop.agents.discard_body", { name: form.name.trim() || t("desktop.agents.unnamed") })}</p>
        </Dialog>
      )}
    </div>
  );
}

/**
 * The tools an agent may use: a box for each tool offered and each the
 * agent already names, and a field to add one by name.
 */
function ToolPicker({ offered, chosen, onChange }: { offered: string[]; chosen: string[]; onChange: (tools: string[]) => void }) {
  const [adding, setAdding] = useState("");
  const all = toolChoices(offered, chosen);
  const toggle = (name: string, on: boolean) => onChange(on ? [...chosen, name] : chosen.filter((x) => x !== name));
  const add = () => {
    onChange(addTools(chosen, adding));
    setAdding("");
  };
  return (
    <fieldset className="field agent-tools">
      <legend className="t-label">{t("desktop.agents.tools", { count: chosen.length })}</legend>
      {all.length > 0 && (
        <div className="agent-tool-grid">
          {all.map((name) => (
            <label key={name} className="agent-tool">
              <input type="checkbox" checked={chosen.includes(name)} onChange={(e) => toggle(name, e.target.checked)} />
              <code className="ellipsis">{name}</code>
            </label>
          ))}
        </div>
      )}
      <div className="row" style={{ gap: 8 }}>
        <input
          className="input mono"
          value={adding}
          placeholder={t("desktop.agents.tool_add_placeholder")}
          aria-label={t("desktop.agents.tool_add")}
          spellCheck={false}
          onChange={(e) => setAdding(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              add();
            }
          }}
        />
        <Button small disabled={!adding.trim()} onClick={add}>
          {t("desktop.agents.tool_add")}
        </Button>
      </div>
      <span className="t-body-sm muted">{t(offered.length ? "desktop.agents.tools_hint" : "desktop.agents.tools_hint_typed")}</span>
    </fieldset>
  );
}

/** The model settings the agent runs with, over its model's own; folded until opened. */
function Advanced({
  form,
  set,
  fieldError,
}: {
  form: AgentForm;
  set: <K extends keyof AgentForm>(k: K, v: AgentForm[K]) => void;
  fieldError: (k: AgentField) => string | undefined;
}) {
  const count = advancedCount(form);
  const [open, setOpen] = useState(count > 0);
  const number = (k: "thinkingBudget" | "maxTokens", label: string, help: string, min: number, step: number) => (
    <Field label={label} supporting={help} error={fieldError(k)}>
      {(id) => <input id={id} className="input" type="number" min={min} step={step} value={form[k]} placeholder={t("desktop.agents.from_model")} onChange={(e) => set(k, e.target.value)} />}
    </Field>
  );
  return (
    <div className="agent-advanced">
      <button type="button" className="panel-divider agent-advanced-toggle" aria-expanded={open} onClick={() => setOpen(!open)}>
        <Icon path={open ? mdiChevronDown : mdiChevronRight} size="sm" />
        <span className="t-label">{count ? t("desktop.agents.advanced_count", { count }) : t("desktop.agents.advanced")}</span>
      </button>
      {open && (
        <div className="stack" style={{ gap: 14 }}>
          <p className="t-body-sm muted">{t("desktop.agents.advanced_hint")}</p>
          <div className="pair">
            <Field label={t("desktop.rs.effort")} supporting={form.effort ? efforts().find((e) => e.value === form.effort)?.detail : t("desktop.agents.effort_hint")}>
              {(id) => (
                <select id={id} className="select" value={form.effort} onChange={(e) => set("effort", e.target.value)}>
                  <option value="">{t("desktop.agents.from_model")}</option>
                  {efforts()
                    .filter((e) => e.value)
                    .map((e) => (
                      <option key={e.value} value={e.value}>
                        {e.label}
                      </option>
                    ))}
                </select>
              )}
            </Field>
            {number("thinkingBudget", t("desktop.rs.budget"), t("desktop.rs.budget_help"), 0, 1024)}
          </div>
          <div className="pair">
            <RangeField label={t("desktop.rs.temperature")} value={form.temperature} min={0} max={2} step={0.05} error={fieldError("temperature")} onChange={(v) => set("temperature", v)} />
            <RangeField label={t("desktop.rs.top_p")} value={form.topP} min={0.01} max={1} step={0.01} error={fieldError("topP")} onChange={(v) => set("topP", v)} />
          </div>
          <div className="pair">{number("maxTokens", t("desktop.rs.max_tokens"), t("desktop.agents.max_tokens_hint"), 1, 256)}</div>
        </div>
      )}
    </div>
  );
}

/** A number set with a slider or typed; empty is the model's own, and the clear button empties it. */
function RangeField({ label, value, min, max, step, error, onChange }: { label: string; value: string; min: number; max: number; step: number; error?: string; onChange: (v: string) => void }) {
  const n = value.trim() === "" ? undefined : Number(value);
  return (
    <div className="field">
      <label className="row">
        <span className="spacer t-label">{label}</span>
        {n === undefined && <span className="muted t-body-sm">{t("desktop.agents.from_model")}</span>}
        <input className="input slider-value" type="number" min={min} max={max} step={step} value={value} onChange={(e) => onChange(e.target.value)} />
        {n !== undefined && <IconButton icon={mdiClose} label={t("desktop.agents.clear", { setting: label })} small onClick={() => onChange("")} />}
      </label>
      <input type="range" aria-label={label} min={min} max={max} step={step} value={n !== undefined && Number.isFinite(n) ? n : min} className={n === undefined ? "unset" : ""} onChange={(e) => onChange(e.target.value)} />
      {error && <span className="t-body-sm error-text">{error}</span>}
    </div>
  );
}
