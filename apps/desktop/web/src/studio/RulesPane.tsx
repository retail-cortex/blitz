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
  mdiArrowDown,
  mdiArrowLeft,
  mdiArrowUp,
  mdiCancel,
  mdiCheckCircleOutline,
  mdiCloseCircleOutline,
  mdiContentSaveOutline,
  mdiDeleteOutline,
  mdiDrag,
  mdiFileDocumentOutline,
  mdiLoading,
  mdiPencilRuler,
  mdiPlus,
  mdiRefresh,
  mdiScaleBalance,
  mdiUndoVariant,
} from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import { t } from "../i18n";
import type { RuleFile } from "../gen/blitz/v1/workspace_pb";
import { SuggestTextarea } from "../SecretSuggest";
import { Button, Chip, Dialog, Field, Icon, IconButton, Segmented, useSnackbar } from "../ui/controls";
import { DraftButton } from "./Draft";
import { RulesOrGuardrails } from "./RulesOrGuardrails";
import { emptyRuleForm, formFromRule, moved, nextOrder, ruleSections, sameRuleForm, specFromForm, validRuleName, type RuleAction, type RuleForm, type RuleWhen } from "./ruleForm";

/** Each action's icon. */
const actionIcons: Record<RuleAction, string> = { block: mdiCancel, warn: mdiAlertCircleOutline, revise: mdiPencilRuler };

/** What a rule is open as: one by name, or a new one (""). */
type Open = { name: string } | null;

/**
 * The workspace's rules (Studio › Rules, spec_gates_041): the gates
 * before the agent and after it, each list in the order its rules run
 * (dragged, or moved with ↑ ↓, which writes their order), then the
 * instruction rules; each with its action (block, warn, revise). A rule's
 * form slides in over the list, as an agent's does: name, what it's for,
 * when (before, after, or an instruction), its action, the agents it
 * gates (or an instruction's paths), the rule with Draft, and for a gate
 * **Try it**, judging a sample prompt or answer as a turn would. Rules
 * outside .agents/rules (another rule folder, the global one) open
 * read-only.
 */
export function RulesPane({ workspace, onDirty, onGuardrails }: { workspace: string; onDirty?: (dirty: boolean) => void; onGuardrails?: () => void }) {
  const snack = useSnackbar();
  const [rules, setRules] = useState<RuleFile[]>([]);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<Open>(null);
  const [form, setForm] = useState<RuleForm>(emptyRuleForm);
  const [base, setBase] = useState<RuleForm>(emptyRuleForm);
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [agents, setAgents] = useState<string[]>([]);
  const [dragging, setDragging] = useState<{ list: "before" | "after"; index: number } | null>(null);
  const [sample, setSample] = useState("");
  const [trying, setTrying] = useState(false);
  const [verdict, setVerdict] = useState<{ pass: boolean; reason: string; problem: string } | null>(null);

  const refresh = useCallback(async () => {
    try {
      setRules((await workspaces.listRules({ workspace })).rules);
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

  const sections = ruleSections(rules);
  const current = open?.name ? rules.find((r) => r.name === open.name) : undefined;
  const editable = open?.name === "" || !!current?.editable;
  const dirty = open !== null && editable && !sameRuleForm(form, base);
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);

  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());
  const show = (r: RuleFile) =>
    leave(() => {
      const f = formFromRule(r);
      setForm(f);
      setBase(f);
      setOpen({ name: r.name });
      setProblems([]);
      setVerdict(null);
      setSample("");
    });
  const startNew = (when: RuleWhen = "before_agent") =>
    leave(() => {
      const f = { ...emptyRuleForm, when, order: nextOrder(when === "after_agent" ? sections.after : sections.before) };
      setForm(f);
      setBase(f);
      setOpen({ name: "" });
      setProblems([]);
      setVerdict(null);
      setSample("");
    });
  const close = () =>
    leave(() => {
      setOpen(null);
      void refresh();
    });
  const set = <K extends keyof RuleForm>(k: K, v: RuleForm[K]) => setForm((f) => ({ ...f, [k]: v }));

  const save = async () => {
    if (busy) return;
    setBusy(true);
    setProblems([]);
    try {
      const r = await workspaces.saveRule({ workspace, previousName: open?.name ?? "", rule: specFromForm(form) });
      if (r.problems.length || !r.rule) return setProblems(r.problems);
      snack(t("desktop.rules.saved", { name: r.rule.name }));
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
    const name = open?.name ?? "";
    try {
      await workspaces.deleteRule({ workspace, name });
      snack(t("desktop.rules.deleted", { name }));
      setOpen(null);
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  // A list's new order, written as the rules' order (only the workspace's own move).
  const reorder = async (list: RuleFile[], from: number, to: number) => {
    const names = moved(
      list.map((r) => r.name),
      from,
      to,
    );
    if (names.join() === list.map((r) => r.name).join()) return;
    try {
      await workspaces.reorderRules({ workspace, names: names.filter((n) => list.find((r) => r.name === n)?.editable) });
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const tryIt = async () => {
    setTrying(true);
    setVerdict(null);
    try {
      const r = await workspaces.tryRule({ workspace, rule: specFromForm(form), sample });
      setVerdict({ pass: r.pass, reason: r.reason, problem: r.problem });
    } catch (e) {
      setVerdict({ pass: false, reason: "", problem: message(e) });
    } finally {
      setTrying(false);
    }
  };

  const gateList = (key: "before" | "after", list: RuleFile[]) => (
    <section className="stack rule-section" style={{ gap: 4 }}>
      <div className="row" style={{ gap: 8 }}>
        <span className="t-title-sm spacer">{t(`desktop.rules.${key}`)}</span>
        <IconButton icon={mdiPlus} label={t(`desktop.rules.new_${key}`)} small onClick={() => startNew(key === "before" ? "before_agent" : "after_agent")} />
      </div>
      <span className="t-body-sm muted">{t(`desktop.rules.${key}_hint`)}</span>
      {list.length === 0 && <p className="t-body-sm muted">{t("desktop.rules.none_here")}</p>}
      <div className="list">
        {list.map((r, i) => (
          <div
            key={r.name}
            className={`list-item rule-row ${dragging?.list === key && dragging.index === i ? "dragging" : ""}`}
            draggable={r.editable}
            onDragStart={() => setDragging({ list: key, index: i })}
            onDragOver={(e) => dragging?.list === key && e.preventDefault()}
            onDrop={() => dragging?.list === key && void reorder(list, dragging.index, i)}
            onDragEnd={() => setDragging(null)}
          >
            {r.editable && <Icon path={mdiDrag} size="sm" className="muted rule-grip" />}
            <button type="button" className="row-open" onClick={() => show(r)}>
              <Icon path={r.problem ? mdiAlertCircleOutline : actionIcons[(r.action as RuleAction) || "warn"]} className={r.problem ? "error-text" : r.action === "block" ? "error-text" : undefined} />
              <span className="lines spacer">
                <span className="ellipsis">
                  <span className="rule-step">{i + 1}.</span> {r.name}
                </span>
                <small className={`ellipsis ${r.problem ? "error-text" : ""}`}>{r.problem || [t(`desktop.rules.action.${r.action || "warn"}`), r.agents.length ? t("desktop.rules.for", { agents: r.agents.join(", ") }) : "", r.description || r.text].filter(Boolean).join(" · ")}</small>
              </span>
            </button>
            {r.editable && (
              <span className="trailing">
                <IconButton icon={mdiArrowUp} label={t("desktop.rules.up", { name: r.name })} small className="hover-only" disabled={i === 0} onClick={() => void reorder(list, i, i - 1)} />
                <IconButton icon={mdiArrowDown} label={t("desktop.rules.down", { name: r.name })} small className="hover-only" disabled={i === list.length - 1} onClick={() => void reorder(list, i, i + 1)} />
              </span>
            )}
          </div>
        ))}
      </div>
    </section>
  );

  const gate = form.when !== "";
  return (
    <div className={`slide-pane rules-pane ${open ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        <div className="row side-head">
          <span className="t-title-sm spacer">{t("desktop.rules.title")}</span>
          <IconButton icon={mdiPlus} label={t("desktop.rules.new")} small onClick={() => startNew()} />
          <IconButton icon={mdiRefresh} label={t("desktop.refresh")} small onClick={() => void refresh()} />
        </div>
        <p className="t-body-sm muted agent-dir">{t("desktop.workers.in", { path: ".agents/rules" })}</p>
        {/* Rules or guardrails: the work's quality here, safety and limits there (spec_gates_041 GT-11). */}
        <RulesOrGuardrails current="rules" onOther={onGuardrails} />
        {error && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
          </div>
        )}
        {gateList("before", sections.before)}
        {gateList("after", sections.after)}
        <section className="stack rule-section" style={{ gap: 4 }}>
          <div className="row" style={{ gap: 8 }}>
            <span className="t-title-sm spacer">{t("desktop.rules.instructions")}</span>
            <IconButton icon={mdiPlus} label={t("desktop.rules.new_instruction")} small onClick={() => startNew("")} />
          </div>
          <span className="t-body-sm muted">{t("desktop.rules.instructions_hint")}</span>
          <div className="list">
            {sections.instructions.map((r) => (
              <button key={r.path} className="list-item" title={r.path} onClick={() => show(r)}>
                <Icon path={mdiFileDocumentOutline} />
                <span className="lines">
                  <span className="ellipsis">{r.name}</span>
                  <small className="ellipsis">{[r.paths.length ? t("desktop.rules.scoped", { paths: r.paths.join(", ") }) : t("desktop.rules.always"), r.editable ? "" : r.path].filter(Boolean).join(" · ")}</small>
                </span>
              </button>
            ))}
          </div>
        </section>
      </aside>
      <section className="slide-detail" inert={open === null}>
        {open && (
          <>
            <div className="row slide-back">
              <IconButton icon={mdiArrowLeft} label={t("desktop.rules.back")} onClick={close} />
              <h2 className="t-headline spacer ellipsis">{form.name || t("desktop.rules.new")}</h2>
              {dirty && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
            </div>
            <p className="t-body-sm muted rule-form-intro">{t(form.when ? "desktop.rules.form_intro_gate" : "desktop.rules.form_intro_instruction")}</p>
            {!editable && current && (
              <div className="card row small-card" style={{ gap: 8 }}>
                <Icon path={mdiFileDocumentOutline} size="sm" />
                <span className="t-body-sm">{t("desktop.rules.read_only", { path: current.path })}</span>
              </div>
            )}
            <fieldset className="worker agent-form rule-form" disabled={!editable}>
              <div className="pair">
                <Field label={t("desktop.rules.name")} error={form.name && !validRuleName(form.name.trim()) ? t("desktop.rules.invalid_name") : undefined} supporting={t("desktop.rules.name_hint")}>
                  {(id) => <input id={id} className="input mono" value={form.name} onChange={(e) => set("name", e.target.value)} />}
                </Field>
                <Field label={t("desktop.rules.description")}>{(id) => <input id={id} className="input" value={form.description} onChange={(e) => set("description", e.target.value)} />}</Field>
              </div>
              <div className="stack" style={{ gap: 6 }}>
                <span className="t-label">{t("desktop.rules.when")}</span>
                <Segmented
                  value={form.when || "instruction"}
                  label={t("desktop.rules.when")}
                  options={[
                    { value: "before_agent", label: t("desktop.rules.before") },
                    { value: "after_agent", label: t("desktop.rules.after") },
                    { value: "instruction", label: t("desktop.rules.instruction") },
                  ]}
                  onChange={(v) => set("when", v === "instruction" ? "" : (v as RuleWhen))}
                />
                <span className="t-body-sm muted">{t(gate ? `desktop.rules.${form.when === "before_agent" ? "before" : "after"}_hint` : "desktop.rules.instructions_hint")}</span>
              </div>
              {gate && (
                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.rules.action")}</span>
                  <Segmented
                    value={form.action}
                    label={t("desktop.rules.action")}
                    options={(["block", "warn", "revise"] as const).map((a) => ({ value: a, label: t(`desktop.rules.action.${a}`), icon: actionIcons[a] }))}
                    onChange={(v) => set("action", v as RuleAction)}
                  />
                  <span className="t-body-sm muted">{t(`desktop.rules.action_hint.${form.when}.${form.action}`)}</span>
                </div>
              )}
              {gate ? (
                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.rules.agents")}</span>
                  <span className="t-body-sm muted">{form.agents.length ? t("desktop.rules.agents_some") : t("desktop.rules.agents_all")}</span>
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
              ) : (
                <Field label={t("desktop.rules.paths")} supporting={t("desktop.rules.paths_hint")}>
                  {(id) => <input id={id} className="input mono" value={form.paths} placeholder={t("desktop.rules.paths_placeholder")} onChange={(e) => set("paths", e.target.value)} />}
                </Field>
              )}
              <div className="row field-tools">
                <span className="spacer" />
                <DraftButton workspace={workspace} kind="rule" name={form.name} description={form.description} current={form.text} onUse={(v) => set("text", v)} disabled={!editable} />
              </div>
              <Field label={t("desktop.rules.text")} supporting={t(gate ? "desktop.rules.text_hint" : "desktop.rules.text_hint_instruction")}>
                {(id) => <SuggestTextarea id={id} className="input agent-prompt" value={form.text} rows={6} placeholder={t(gate ? "desktop.rules.text_placeholder" : "desktop.rules.text_placeholder_instruction")} onValue={(v) => set("text", v)} workspace={workspace} />}
              </Field>
            </fieldset>
            {gate && (
              <div className="card outlined stack rule-try" style={{ gap: 8 }}>
                <span className="t-title-sm">{t("desktop.rules.try")}</span>
                <span className="t-body-sm muted">{t(form.when === "before_agent" ? "desktop.rules.try_before" : "desktop.rules.try_after")}</span>
                <textarea className="input" rows={3} value={sample} aria-label={t("desktop.rules.try")} onChange={(e) => setSample(e.target.value)} />
                <div className="row" style={{ gap: 8 }}>
                  <span className="spacer" />
                  <Button variant="tonal" icon={trying ? mdiLoading : mdiScaleBalance} iconSpin={trying} disabled={trying || !form.text.trim() || !sample.trim()} onClick={() => void tryIt()}>
                    {trying ? t("desktop.rules.checking") : t("desktop.rules.check")}
                  </Button>
                </div>
                {verdict && (
                  <div className={`row small-card ${verdict.problem ? "error-text" : verdict.pass ? "" : "error-text"}`} style={{ gap: 8 }}>
                    <Icon path={verdict.problem ? mdiAlertCircleOutline : verdict.pass ? mdiCheckCircleOutline : mdiCloseCircleOutline} size="sm" />
                    <span className="t-body-sm">{verdict.problem ? t("desktop.rules.try_problem", { problem: verdict.problem }) : t(verdict.pass ? "desktop.rules.passes" : "desktop.rules.fails", { reason: verdict.reason })}</span>
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
              <span className="t-body-sm muted spacer ellipsis" title={current?.path}>
                {current?.path ? t("desktop.agents.where", { path: current.path }) : form.name ? t("desktop.agents.where", { path: `.agents/rules/${form.name.trim()}.md` }) : ""}
              </span>
              {current?.editable && (
                <Button danger icon={mdiDeleteOutline} onClick={() => setDeleting(true)}>
                  {t("desktop.rules.delete")}
                </Button>
              )}
              {dirty && open.name !== "" && (
                <Button icon={mdiUndoVariant} onClick={() => setForm(base)}>
                  {t("desktop.agents.revert")}
                </Button>
              )}
              <Button onClick={close}>{editable ? t("desktop.cancel") : t("desktop.close")}</Button>
              {editable && (
                <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || !form.name.trim() || !validRuleName(form.name.trim()) || (open.name !== "" && !dirty)} onClick={() => void save()}>
                  {t("desktop.rules.save")}
                </Button>
              )}
            </div>
          </>
        )}
      </section>
      {deleting &&
        createPortal(
          <Dialog
            title={t("desktop.rules.delete_title", { name: open?.name ?? "" })}
            icon={mdiDeleteOutline}
            onClose={() => setDeleting(false)}
            footer={
              <>
                <Button onClick={() => setDeleting(false)}>{t("desktop.cancel")}</Button>
                <Button variant="filled" danger onClick={() => void remove()}>
                  {t("desktop.rules.delete")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.rules.delete_body", { path: current?.path ?? "" })}</p>
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
            <p>{t("desktop.agents.discard_body", { name: form.name || t("desktop.agents.unnamed") })}</p>
          </Dialog>,
          document.body,
        )}
    </div>
  );
}
