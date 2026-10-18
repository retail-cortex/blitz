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
import {
  mdiAlertCircleOutline,
  mdiArrowLeft,
  mdiCheckCircleOutline,
  mdiCloseCircleOutline,
  mdiContentSaveOutline,
  mdiDeleteOutline,
  mdiEyeOffOutline,
  mdiGaugeFull,
  mdiLoading,
  mdiPlus,
  mdiRefresh,
  mdiShieldAlertOutline,
  mdiShieldCheckOutline,
  mdiTune,
  mdiUndoVariant,
} from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import { t } from "../i18n";
import type { BuiltinGuardrail, GuardrailFile } from "../gen/blitz/v1/workspace_pb";
import { SuggestTextarea } from "../SecretSuggest";
import { Button, Chip, Dialog, Field, Icon, IconButton, Segmented, Switch, useSnackbar } from "../ui/controls";
import { DraftButton } from "./Draft";
import { builtinDefaults, emptyGuardrailForm, formForBuiltin, formFromGuardrail, guardStages, sameGuardrailForm, specFromForm, stageHasTools, type BuiltinId, type GuardrailForm, type GuardStage } from "./guardrailForm";
import { GatesTitle } from "./RulesOrGuardrails";

/** Each built-in's icon. */
const builtinIcons: Record<BuiltinId, string> = { prompt_injection: mdiShieldAlertOutline, secrets: mdiEyeOffOutline, limits: mdiGaugeFull };

/** What a guardrail is open as: a built-in, a custom one by name, or a new one (""). */
type Open = { builtin: BuiltinId } | { name: string } | null;

/**
 * The workspace's guardrails (Studio › Guardrails, spec_gates_041): the
 * built-in ones, each switched on or off where it's listed, with its
 * settings a click away; then the workspace's own. A guardrail's form
 * slides in over the list as the others do: a built-in's settings (the
 * action, personal data, the judge's confirmation, the tools it checks,
 * the limits), or a custom one's stage, action, tools, agents and check
 * with Redraft; and Try it, where a sample can be checked.
 */
export function GuardrailsPane({ workspace, onDirty, onRules }: { workspace: string; onDirty?: (dirty: boolean) => void; onRules?: () => void }) {
  const snack = useSnackbar();
  const [builtins, setBuiltins] = useState<BuiltinGuardrail[]>([]);
  const [custom, setCustom] = useState<GuardrailFile[]>([]);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<Open>(null);
  const [form, setForm] = useState<GuardrailForm>(emptyGuardrailForm);
  const [base, setBase] = useState<GuardrailForm>(emptyGuardrailForm);
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [agents, setAgents] = useState<string[]>([]);
  const [sample, setSample] = useState("");
  const [trying, setTrying] = useState(false);
  const [verdict, setVerdict] = useState<{ pass: boolean; reason: string; problem: string } | null>(null);

  const refresh = useCallback(async () => {
    try {
      const r = await workspaces.listGuardrails({ workspace });
      setBuiltins(r.builtins);
      setCustom(r.guardrails);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => {
    void refresh();
    workspaces.listAgents({ workspace }).then(
      (r) => setAgents(r.agents.map((a) => a.name)),
      () => {},
    );
  }, [workspace, refresh]);

  const builtinOpen = open && "builtin" in open ? builtins.find((b) => b.id === open.builtin) : undefined;
  const customOpen = open && "name" in open && open.name ? custom.find((g) => g.name === open.name) : undefined;
  const file = builtinOpen?.file ?? customOpen;
  const editable = !file || file.editable;
  const isNew = open !== null && "name" in open && open.name === "";
  const dirty = open !== null && editable && (!sameGuardrailForm(form, base) || (!!builtinOpen && !builtinOpen.file));
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);

  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());
  const showForm = (o: Open, f: GuardrailForm) =>
    leave(() => {
      setForm(f);
      setBase(f);
      setOpen(o);
      setProblems([]);
      setVerdict(null);
      setSample("");
    });
  const close = () =>
    leave(() => {
      setOpen(null);
      void refresh();
    });
  const set = <K extends keyof GuardrailForm>(k: K, v: GuardrailForm[K]) => setForm((f) => ({ ...f, [k]: v }));

  // Switching a built-in on writes its file with its defaults (or as last
  // set); off deletes it.
  const toggle = async (b: BuiltinGuardrail, on: boolean) => {
    try {
      if (on) await workspaces.saveGuardrail({ workspace, guardrail: specFromForm(formForBuiltin(b.id as BuiltinId)) });
      else await workspaces.deleteGuardrail({ workspace, name: b.id });
      snack(t(on ? "desktop.guardrails.switched_on" : "desktop.guardrails.switched_off", { name: t(`desktop.guardrails.builtin.${b.id}`) }));
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const save = async () => {
    if (busy) return;
    setBusy(true);
    setProblems([]);
    try {
      const previous = file ? file.name : "";
      const r = await workspaces.saveGuardrail({ workspace, previousName: previous, guardrail: specFromForm(form) });
      if (r.problems.length || !r.guardrail) return setProblems(r.problems);
      snack(t("desktop.guardrails.saved", { name: form.builtin ? t(`desktop.guardrails.builtin.${form.builtin}`) : r.guardrail.name }));
      setOpen(null);
      await refresh();
    } catch (e) {
      setProblems([message(e)]);
    } finally {
      setBusy(false);
    }
  };
  const remove = async () => {
    setDeleting(false);
    const name = file?.name ?? "";
    try {
      await workspaces.deleteGuardrail({ workspace, name });
      snack(t("desktop.guardrails.deleted", { name }));
      setOpen(null);
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const tryIt = async () => {
    setTrying(true);
    setVerdict(null);
    try {
      const r = await workspaces.tryGuardrail({ workspace, guardrail: specFromForm(form), sample });
      setVerdict({ pass: r.pass, reason: r.reason, problem: r.problem });
    } catch (e) {
      setVerdict({ pass: false, reason: "", problem: message(e) });
    } finally {
      setTrying(false);
    }
  };

  const b = form.builtin;
  const stage = b ? builtinDefaults[b].stage : form.stage;
  const actions = b ? builtinDefaults[b].actions : ["block", "warn"];
  const title = b ? t(`desktop.guardrails.builtin.${b}`) : form.name || t("desktop.guardrails.new");
  return (
    <div className={`slide-pane guardrails-pane ${open ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        <GatesTitle
          current="guardrails"
          title={t("desktop.gates.guardrails")}
          onOther={onRules}
          subtitle={<p className="t-body-sm muted agent-dir">{t("desktop.workers.in", { path: ".agents/guardrails" })}</p>}
          actions={
            <>
              <IconButton icon={mdiPlus} label={t("desktop.guardrails.new")} small onClick={() => showForm({ name: "" }, emptyGuardrailForm)} />
              <IconButton icon={mdiRefresh} label={t("desktop.refresh")} small onClick={() => void refresh()} />
            </>
          }
        />
        {error && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
          </div>
        )}
        <section className="stack rule-section" style={{ gap: 4 }}>
          <span className="t-title-sm">{t("desktop.guardrails.builtins")}</span>
          <span className="t-body-sm muted">{t("desktop.guardrails.builtins_hint")}</span>
          <div className="list">
            {builtins.map((g) => {
              const id = g.id as BuiltinId;
              const on = !!g.file;
              return (
                <div key={g.id} className="list-item guardrail-row">
                  <button type="button" className="row-open" onClick={() => showForm({ builtin: id }, g.file ? formFromGuardrail(g.file) : formForBuiltin(id))}>
                    <Icon path={builtinIcons[id]} className={on ? undefined : "muted"} />
                    <span className="lines spacer">
                      <span className="ellipsis">{t(`desktop.guardrails.builtin.${id}`)}</span>
                      <small className={`ellipsis ${g.file?.problem ? "error-text" : ""}`}>{g.file?.problem || [t(`desktop.guardrails.stage.${g.stage}`), on ? t(`desktop.guardrails.action.${g.file!.action}`) : t("desktop.guardrails.off"), t(`desktop.guardrails.builtin_about.${id}`)].join(" · ")}</small>
                    </span>
                  </button>
                  <span className="trailing">
                    <IconButton icon={mdiTune} label={t("desktop.guardrails.settings", { name: t(`desktop.guardrails.builtin.${id}`) })} small onClick={() => showForm({ builtin: id }, g.file ? formFromGuardrail(g.file) : formForBuiltin(id))} />
                    <Switch label={t(`desktop.guardrails.builtin.${id}`)} checked={on} disabled={!!g.file && !g.file.editable} onChange={(v) => void toggle(g, v)} />
                  </span>
                </div>
              );
            })}
          </div>
        </section>
        <section className="stack rule-section" style={{ gap: 4 }}>
          <div className="row" style={{ gap: 8 }}>
            <span className="t-title-sm spacer">{t("desktop.guardrails.yours")}</span>
            <IconButton icon={mdiPlus} label={t("desktop.guardrails.new")} small onClick={() => showForm({ name: "" }, emptyGuardrailForm)} />
          </div>
          <span className="t-body-sm muted">{t("desktop.guardrails.yours_hint")}</span>
          {custom.length === 0 && <p className="t-body-sm muted">{t("desktop.rules.none_here")}</p>}
          <div className="list">
            {custom.map((g) => (
              <div key={g.path} className="list-item">
                <button type="button" className="row-open" onClick={() => showForm({ name: g.name }, formFromGuardrail(g))}>
                  <Icon path={g.problem ? mdiAlertCircleOutline : mdiShieldCheckOutline} className={g.problem ? "error-text" : undefined} />
                  <span className="lines spacer">
                    <span className="ellipsis">{g.name}</span>
                    <small className={`ellipsis ${g.problem ? "error-text" : ""}`}>{g.problem || [t(`desktop.guardrails.stage.${g.stage}`), t(`desktop.guardrails.action.${g.action}`), g.description || g.text].filter(Boolean).join(" · ")}</small>
                  </span>
                </button>
              </div>
            ))}
          </div>
        </section>
      </aside>
      <section className="slide-detail" inert={open === null}>
        {open && (
          <>
            <div className="row slide-back">
              <IconButton icon={mdiArrowLeft} label={t("desktop.guardrails.back")} onClick={close} />
              <h2 className="t-headline spacer ellipsis">{title}</h2>
              {builtinOpen && !builtinOpen.file && <span className="t-body-sm muted">{t("desktop.guardrails.off_until_saved")}</span>}
              {dirty && builtinOpen?.file && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
            </div>
            <p className="t-body-sm muted rule-form-intro">{t(b ? `desktop.guardrails.builtin_help.${b}` : "desktop.guardrails.custom_intro")}</p>
            <fieldset className="worker agent-form rule-form" disabled={!editable}>
              {!b && (
                <div className="pair">
                  <Field label={t("desktop.rules.name")} supporting={t("desktop.guardrails.name_hint")}>
                    {(id) => <input id={id} className="input mono" value={form.name} onChange={(e) => set("name", e.target.value)} />}
                  </Field>
                  <Field label={t("desktop.rules.description")}>{(id) => <input id={id} className="input" value={form.description} onChange={(e) => set("description", e.target.value)} />}</Field>
                </div>
              )}
              {!b && (
                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.guardrails.stage")}</span>
                  <Segmented value={form.stage} label={t("desktop.guardrails.stage")} options={guardStages.map((s) => ({ value: s, label: t(`desktop.guardrails.stage.${s}`) }))} onChange={(v) => set("stage", v as GuardStage)} />
                  <span className="t-body-sm muted">{t(`desktop.guardrails.stage_hint.${form.stage}`)}</span>
                </div>
              )}
              {actions.length > 1 && (
                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.rules.action")}</span>
                  <Segmented value={form.action} label={t("desktop.rules.action")} options={actions.map((a) => ({ value: a, label: t(`desktop.guardrails.action.${a}`) }))} onChange={(v) => set("action", v)} />
                  <span className="t-body-sm muted">{t(`desktop.guardrails.action_hint.${form.action}`)}</span>
                </div>
              )}
              {b === "secrets" && (
                <label className="row agent-switch">
                  <Switch label={t("desktop.guardrails.pii")} checked={form.pii} onChange={(v) => set("pii", v)} />
                  <span className="stack" style={{ gap: 0 }}>
                    <span className="t-label">{t("desktop.guardrails.pii")}</span>
                    <span className="t-body-sm muted">{t("desktop.guardrails.pii_hint")}</span>
                  </span>
                </label>
              )}
              {b === "prompt_injection" && (
                <label className="row agent-switch">
                  <Switch label={t("desktop.guardrails.judge")} checked={form.judge} onChange={(v) => set("judge", v)} />
                  <span className="stack" style={{ gap: 0 }}>
                    <span className="t-label">{t("desktop.guardrails.judge")}</span>
                    <span className="t-body-sm muted">{t("desktop.guardrails.judge_hint")}</span>
                  </span>
                </label>
              )}
              {b === "limits" && (
                <div className="stack" style={{ gap: 10 }}>
                  <div className="pair">
                    <Field label={t("desktop.guardrails.max_cost")} supporting={t("desktop.guardrails.per_turn")}>
                      {(id) => <input id={id} className="input" type="number" min={0} step={0.1} value={form.maxCostUsd} onChange={(e) => set("maxCostUsd", e.target.value)} />}
                    </Field>
                    <Field label={t("desktop.guardrails.max_turns")} supporting={t("desktop.guardrails.per_turn")}>
                      {(id) => <input id={id} className="input" type="number" min={0} step={1} value={form.maxTurns} onChange={(e) => set("maxTurns", e.target.value)} />}
                    </Field>
                  </div>
                  <div className="pair">
                    <Field label={t("desktop.guardrails.max_files")} supporting={t("desktop.guardrails.per_turn")}>
                      {(id) => <input id={id} className="input" type="number" min={0} step={1} value={form.maxFilesChanged} onChange={(e) => set("maxFilesChanged", e.target.value)} />}
                    </Field>
                    <Field label={t("desktop.guardrails.domains")} supporting={t("desktop.guardrails.domains_hint")}>
                      {(id) => <input id={id} className="input mono" value={form.domains} placeholder={t("desktop.guardrails.domains_placeholder")} onChange={(e) => set("domains", e.target.value)} />}
                    </Field>
                  </div>
                  <Field label={t("desktop.guardrails.commands")} supporting={t("desktop.guardrails.commands_hint")}>
                    {(id) => <textarea id={id} className="input mono" rows={3} value={form.commands} placeholder={t("desktop.guardrails.commands_placeholder")} onChange={(e) => set("commands", e.target.value)} />}
                  </Field>
                </div>
              )}
              {(b === "prompt_injection" || (!b && stageHasTools(form.stage))) && (
                <Field label={t("desktop.guardrails.tools")} supporting={t(stage === "tool_result" ? "desktop.guardrails.tools_hint_results" : "desktop.guardrails.tools_hint_actions")}>
                  {(id) => <input id={id} className="input mono" value={form.tools} placeholder={t("desktop.guardrails.tools_placeholder")} onChange={(e) => set("tools", e.target.value)} />}
                </Field>
              )}
              <div className="stack" style={{ gap: 6 }}>
                <span className="t-label">{t("desktop.guardrails.agents")}</span>
                <span className="t-body-sm muted">{form.agents.length ? t("desktop.rules.agents_some") : t("desktop.guardrails.agents_all")}</span>
                <div className="row wrap" style={{ gap: 6 }}>
                  {[...new Set([...agents, ...form.agents])].map((a) => {
                    const on = form.agents.includes(a);
                    return (
                      <Chip key={a} selected={on} aria-pressed={on} onClick={() => set("agents", on ? form.agents.filter((x) => x !== a) : [...form.agents, a])}>
                        {a}
                      </Chip>
                    );
                  })}
                </div>
              </div>
              {!b && (
                <>
                  <div className="row field-tools">
                    <span className="spacer" />
                    <DraftButton workspace={workspace} kind="guardrail" name={form.name} description={form.description} current={form.text} onUse={(v) => set("text", v)} disabled={!editable} />
                  </div>
                  <Field label={t("desktop.guardrails.text")} supporting={t("desktop.guardrails.text_hint")}>
                    {(id) => <SuggestTextarea id={id} className="input agent-prompt" value={form.text} rows={5} placeholder={t("desktop.guardrails.text_placeholder")} onValue={(v) => set("text", v)} workspace={workspace} />}
                  </Field>
                </>
              )}
            </fieldset>
            {b !== "limits" && (
              <div className="card outlined stack rule-try" style={{ gap: 8 }}>
                <span className="t-title-sm">{t("desktop.rules.try")}</span>
                <span className="t-body-sm muted">{t(`desktop.guardrails.try_hint.${stage}`)}</span>
                <textarea className="input" rows={3} value={sample} aria-label={t("desktop.rules.try")} onChange={(e) => setSample(e.target.value)} />
                <div className="row" style={{ gap: 8 }}>
                  <span className="spacer" />
                  <Button variant="tonal" icon={trying ? mdiLoading : mdiShieldCheckOutline} iconSpin={trying} disabled={trying || !sample.trim() || (!b && !form.text.trim())} onClick={() => void tryIt()}>
                    {trying ? t("desktop.rules.checking") : t("desktop.rules.check")}
                  </Button>
                </div>
                {verdict && (
                  <div className={`row small-card ${verdict.problem || !verdict.pass ? "error-text" : ""}`} style={{ gap: 8 }}>
                    <Icon path={verdict.problem ? mdiAlertCircleOutline : verdict.pass ? mdiCheckCircleOutline : mdiCloseCircleOutline} size="sm" />
                    <span className="t-body-sm">{verdict.problem ? t("desktop.rules.try_problem", { problem: verdict.problem }) : t(verdict.pass ? "desktop.guardrails.passes" : "desktop.guardrails.caught", { reason: verdict.reason })}</span>
                  </div>
                )}
              </div>
            )}
            {problems.length > 0 && (
              <div className="card error stack" role="alert" style={{ gap: 4 }}>
                {problems.map((p) => (
                  <span key={p} className="row t-body-sm" style={{ gap: 6 }}>
                    <Icon path={mdiAlertCircleOutline} size="sm" /> {p}
                  </span>
                ))}
              </div>
            )}
            <div className="row wrap agent-actions slide-actions">
              <span className="t-body-sm muted spacer ellipsis" title={file?.path}>
                {file?.path ? t("desktop.agents.where", { path: file.path }) : t("desktop.agents.where", { path: `.agents/guardrails/${b || form.name.trim() || "…"}.md` })}
              </span>
              {file?.editable && (
                <Button danger icon={mdiDeleteOutline} onClick={() => setDeleting(true)}>
                  {b ? t("desktop.guardrails.switch_off") : t("desktop.rules.delete")}
                </Button>
              )}
              {dirty && file && (
                <Button icon={mdiUndoVariant} onClick={() => setForm(base)}>
                  {t("desktop.agents.revert")}
                </Button>
              )}
              <Button onClick={close}>{editable ? t("desktop.cancel") : t("desktop.close")}</Button>
              {editable && (
                <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || (!b && !form.name.trim()) || (!!file && !dirty && !isNew)} onClick={() => void save()}>
                  {builtinOpen && !builtinOpen.file ? t("desktop.guardrails.save_on") : t("desktop.rules.save")}
                </Button>
              )}
            </div>
          </>
        )}
      </section>
      {deleting &&
        createPortal(
          <Dialog
            title={t(b ? "desktop.guardrails.off_title" : "desktop.guardrails.delete_title", { name: title })}
            icon={mdiDeleteOutline}
            onClose={() => setDeleting(false)}
            footer={
              <>
                <Button onClick={() => setDeleting(false)}>{t("desktop.cancel")}</Button>
                <Button variant="filled" danger onClick={() => void remove()}>
                  {b ? t("desktop.guardrails.switch_off") : t("desktop.rules.delete")}
                </Button>
              </>
            }
          >
            <p>{t(b ? "desktop.guardrails.off_body" : "desktop.rules.delete_body", { path: file?.path ?? "" })}</p>
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
            <p>{t("desktop.agents.discard_body", { name: title })}</p>
          </Dialog>,
          document.body,
        )}
    </div>
  );
}
