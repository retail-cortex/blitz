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
import {
  mdiAlertCircleOutline,
  mdiAlertOutline,
  mdiArrowDown,
  mdiArrowLeft,
  mdiArrowUp,
  mdiCheckCircleOutline,
  mdiChevronDown,
  mdiChevronRight,
  mdiCloseCircleOutline,
  mdiContentSaveOutline,
  mdiDeleteOutline,
  mdiFlaskOutline,
  mdiInformationOutline,
  mdiLoading,
  mdiPlay,
  mdiPlus,
  mdiRefresh,
  mdiStop,
  mdiUndoVariant,
} from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import { t, tn } from "../i18n";
import type { EvalFile, EvalResult, EvalRun } from "../gen/blitz/v1/workspace_pb";
import { Button, Chip, Dialog, Field, Icon, IconButton, Segmented, Switch, Tabs, useSnackbar } from "../ui/controls";
import { duplicateIds, emptyEvalForm, formFromEval, moveCase, newCase, resultsByCase, resultStatuses, runCounts, sameEvalForm, specFromForm, type CaseForm, type EvalForm } from "./evalForm";

type Tab = "cases" | "settings" | "runs";

/** A result's (or a run's) icon and colour, by status. */
const statusIcon: Record<string, string> = {
  pass: mdiCheckCircleOutline,
  passed: mdiCheckCircleOutline,
  fail: mdiCloseCircleOutline,
  failed: mdiCloseCircleOutline,
  warn: mdiAlertOutline,
  info: mdiInformationOutline,
  error: mdiAlertCircleOutline,
  running: mdiLoading,
  stopped: mdiStop,
};
const statusClass = (s: string) => (s === "fail" || s === "failed" || s === "error" ? "error-text" : s === "warn" ? "warn-text" : "muted");

/** A run's counts, as "2 passed · 1 failed". */
const summary = (run: Pick<EvalRun, "counts" | "results">) => {
  const c = runCounts(run);
  return resultStatuses
    .filter((s) => c[s])
    .map((s) => tn(`desktop.evals.count.${s}`, c[s]))
    .join(" · ");
};

/** A result's score: "0.91 / 0.80" for a near case, "exact" or "no match" for an exact one. */
const scoreText = (r: EvalResult) => (r.status === "error" ? "" : r.type === "near" ? `${r.score.toFixed(2)} / ${r.threshold.toFixed(2)}` : t(r.score ? "desktop.evals.matched" : "desktop.evals.no_match"));

/**
 * The workspace's evals (Studio › Evals, spec_evals_042): each listed with
 * its last run, and run from the list. An eval's form slides in over the
 * list with three tabs: its cases (each a row that opens into its editor:
 * the prompt, the output expected, exact or near and the threshold, and
 * what a failure means), its settings (agents, skills, how near cases are
 * scored, what cases may reach), and its runs, live as they're scored.
 */
export function EvalsPane({ workspace, onDirty }: { workspace: string; onDirty?: (dirty: boolean) => void }) {
  const snack = useSnackbar();
  const [list, setList] = useState<EvalFile[]>([]);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("cases");
  const [form, setForm] = useState<EvalForm>(emptyEvalForm);
  const [base, setBase] = useState<EvalForm>(emptyEvalForm);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [agents, setAgents] = useState<string[]>([]);
  const [skills, setSkills] = useState<string[]>([]);
  const [runs, setRuns] = useState<EvalRun[]>([]);
  const [live, setLive] = useState<{ run: EvalRun | null; results: EvalResult[]; total: number } | null>(null);
  const [shown, setShown] = useState<string>("");
  const stop = useRef<AbortController | null>(null);

  const refresh = useCallback(async () => {
    try {
      const r = await workspaces.listEvals({ workspace });
      setList(r.evals);
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
    workspaces.listSkills({ workspace }).then(
      (r) => setSkills(r.skills.map((s) => s.name)),
      () => {},
    );
  }, [workspace, refresh]);
  useEffect(() => () => stop.current?.abort(), []);

  const file = open ? list.find((e) => e.name === open) : undefined;
  const isNew = open === "";
  const dirty = open !== null && !sameEvalForm(form, base);
  const running = !!live && !live.run?.status.match(/passed|failed|stopped/);
  useEffect(() => onDirty?.(dirty || running), [dirty, running, onDirty]);

  const loadRuns = useCallback(
    async (name: string) => {
      try {
        setRuns((await workspaces.listEvalRuns({ workspace, name })).runs);
      } catch {
        setRuns([]);
      }
    },
    [workspace],
  );
  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());
  const showForm = (name: string, f: EvalForm, to: Tab = "cases") =>
    leave(() => {
      setForm(f);
      setBase(f);
      setOpen(name);
      setTab(to);
      setProblems([]);
      setExpanded(new Set(name === "" ? f.cases.map((c) => c.key) : []));
      setLive(null);
      setShown("");
      setRuns([]);
      if (name) void loadRuns(name);
    });
  const close = () => {
    if (running) return;
    leave(() => {
      setOpen(null);
      void refresh();
    });
  };
  const set = <K extends keyof EvalForm>(k: K, v: EvalForm[K]) => setForm((f) => ({ ...f, [k]: v }));
  const setCase = (key: number, patch: Partial<CaseForm>) => setForm((f) => ({ ...f, cases: f.cases.map((c) => (c.key === key ? { ...c, ...patch } : c)) }));
  const toggle = (key: number) =>
    setExpanded((s) => {
      const n = new Set(s);
      if (n.has(key)) n.delete(key);
      else n.add(key);
      return n;
    });
  const addCase = () => {
    const c = newCase(form.cases);
    set("cases", [...form.cases, c]);
    setExpanded((s) => new Set([...s, c.key]));
  };

  const save = async () => {
    if (busy) return;
    setBusy(true);
    setProblems([]);
    try {
      const r = await workspaces.saveEval({ workspace, previousName: file ? file.name : "", eval: specFromForm(form) });
      if (r.problems.length || !r.eval) return setProblems(r.problems);
      snack(t("desktop.evals.saved", { name: r.eval.name }));
      const f = formFromEval(r.eval);
      setForm(f);
      setBase(f);
      setExpanded(new Set());
      setOpen(r.eval.name);
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
      await workspaces.deleteEval({ workspace, name });
      snack(t("desktop.evals.deleted", { name }));
      setOpen(null);
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  // Runs the eval as saved: the run, then each result as it's scored.
  const run = async (name: string, cases: number, agentCount: number) => {
    const ctl = new AbortController();
    stop.current = ctl;
    setTab("runs");
    setShown("");
    setLive({ run: null, results: [], total: cases * Math.max(1, agentCount) });
    try {
      for await (const res of workspaces.runEval({ workspace, name }, { signal: ctl.signal })) {
        const ev = res.event;
        if (ev.case === "started") setLive((l) => l && { ...l, run: ev.value });
        else if (ev.case === "result") setLive((l) => l && { ...l, results: [...l.results, ev.value] });
        else if (ev.case === "finished") {
          setLive((l) => l && { ...l, run: ev.value, results: ev.value.results });
          snack(t(`desktop.evals.run_${ev.value.status === "passed" ? "passed" : "failed"}`, { name, summary: summary(ev.value) }), { error: ev.value.status !== "passed" });
        }
      }
    } catch (e) {
      if (ctl.signal.aborted) setLive((l) => l && { ...l, run: l.run && { ...l.run, status: "stopped" } });
      else {
        setLive(null);
        snack(message(e), { error: true });
      }
    } finally {
      stop.current = null;
      void loadRuns(name);
      void refresh();
    }
  };

  const latest = live?.run?.results.length ? live.run.results : (live?.results ?? runs[0]?.results ?? []);
  const byCase = resultsByCase(latest);
  const dup = duplicateIds(form.cases);
  const title = isNew && !form.name ? t("desktop.evals.new") : form.name || open || "";
  const canRun = !!file && !dirty && !file.problem && form.cases.length > 0;
  const shownRun = runs.find((r) => r.id === shown);

  return (
    <div className={`slide-pane evals-pane ${open !== null ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        <div className="row side-head">
          <span className="t-title-sm spacer">{t("desktop.evals.title")}</span>
          <IconButton icon={mdiPlus} label={t("desktop.evals.new")} small onClick={() => showForm("", { ...emptyEvalForm, cases: [newCase([])] })} />
          <IconButton icon={mdiRefresh} label={t("desktop.refresh")} small onClick={() => void refresh()} />
        </div>
        <p className="t-body-sm muted agent-dir">{t("desktop.workers.in", { path: ".agents/evals" })}</p>
        <p className="t-body-sm muted">{t("desktop.evals.intro")}</p>
        {error && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
          </div>
        )}
        {list.length === 0 && !error && <p className="t-body-sm muted">{t("desktop.evals.none")}</p>}
        <div className="list">
          {list.map((e) => {
            const last = e.lastRun;
            const icon = e.problem ? mdiAlertCircleOutline : last ? statusIcon[last.status] : mdiFlaskOutline;
            const cls = e.problem ? "error-text" : last ? statusClass(last.status) : undefined;
            const what = [tn("desktop.evals.cases", e.cases.length), last ? t("desktop.evals.last_run", { status: t(`desktop.evals.status.${last.status}`), summary: summary(last) }) : t("desktop.evals.never_run"), e.description].filter(Boolean).join(" · ");
            return (
              <div key={e.path} className="list-item">
                <button type="button" className="row-open" onClick={() => showForm(e.name, formFromEval(e))}>
                  <Icon path={icon} className={cls} />
                  <span className="lines spacer">
                    <span className="ellipsis">{e.name}</span>
                    <small className={`ellipsis ${e.problem ? "error-text" : ""}`}>{e.problem || what}</small>
                  </span>
                </button>
                <span className="trailing">
                  <IconButton
                    icon={mdiPlay}
                    label={t("desktop.evals.run_name", { name: e.name })}
                    small
                    disabled={!!e.problem || e.cases.length === 0}
                    onClick={() => {
                      showForm(e.name, formFromEval(e), "runs");
                      void run(e.name, e.cases.length, e.agents.length);
                    }}
                  />
                </span>
              </div>
            );
          })}
        </div>
      </aside>
      <section className="slide-detail" inert={open === null}>
        {open !== null && (
          <>
            <div className="row slide-back">
              <IconButton icon={mdiArrowLeft} label={t("desktop.evals.back")} onClick={close} disabled={running} />
              <h2 className="t-headline spacer ellipsis">{title}</h2>
              {dirty && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
              {running ? (
                <Button icon={mdiStop} onClick={() => stop.current?.abort()}>
                  {t("desktop.evals.stop")}
                </Button>
              ) : (
                <Button variant="tonal" icon={mdiPlay} disabled={!canRun} title={canRun ? undefined : t(dirty || isNew ? "desktop.evals.save_first" : "desktop.evals.cannot_run")} onClick={() => void run(file!.name, form.cases.length, form.agents.length)}>
                  {t("desktop.evals.run")}
                </Button>
              )}
            </div>
            <Tabs
              value={tab}
              label={t("desktop.evals.tabs")}
              onChange={setTab}
              tabs={[
                { value: "cases", label: t("desktop.evals.tab.cases"), count: form.cases.length },
                { value: "settings", label: t("desktop.evals.tab.settings") },
                { value: "runs", label: t("desktop.evals.tab.runs"), count: runs.length },
              ]}
            />
            {tab === "cases" && (
              <div className="stack eval-cases" style={{ gap: 6 }}>
                <p className="t-body-sm muted">{t("desktop.evals.cases_hint")}</p>
                <div className="list">
                  {form.cases.map((c, i) => {
                    const results = byCase.get(c.id.trim()) ?? [];
                    const worst = results.find((r) => r.status !== "pass")?.status ?? (results.length ? "pass" : "");
                    const isOpen = expanded.has(c.key);
                    return (
                      <div key={c.key} className="stack" style={{ gap: 0 }}>
                        <div className="list-item eval-case">
                          <button type="button" className="row-open" aria-expanded={isOpen} onClick={() => toggle(c.key)}>
                            <Icon path={isOpen ? mdiChevronDown : mdiChevronRight} size="sm" />
                            <Icon path={worst ? statusIcon[worst] : mdiFlaskOutline} className={worst ? statusClass(worst) : "muted"} />
                            <span className="lines spacer">
                              <span className={`ellipsis ${dup.has(c.id.trim()) ? "error-text" : ""}`}>
                                <span className="mono">{c.id || "?"}</span>
                                {c.name && ` · ${c.name}`}
                              </span>
                              <small className="ellipsis">
                                {[c.type === "exact" ? t("desktop.evals.type.exact") : t("desktop.evals.near_at", { threshold: c.threshold }), t(`desktop.evals.scope.${c.scope}`), c.prompt.split("\n")[0]].filter(Boolean).join(" · ")}
                              </small>
                            </span>
                          </button>
                          <span className="trailing">
                            <IconButton icon={mdiArrowUp} label={t("desktop.evals.move_up", { id: c.id })} small disabled={i === 0} onClick={() => set("cases", moveCase(form.cases, i, -1))} />
                            <IconButton icon={mdiArrowDown} label={t("desktop.evals.move_down", { id: c.id })} small disabled={i === form.cases.length - 1} onClick={() => set("cases", moveCase(form.cases, i, 1))} />
                            <IconButton icon={mdiDeleteOutline} label={t("desktop.evals.remove_case", { id: c.id })} small onClick={() => set("cases", form.cases.filter((x) => x.key !== c.key))} />
                          </span>
                        </div>
                        {isOpen && (
                          <div className="card outlined stack eval-case-form" style={{ gap: 10 }}>
                            <div className="pair">
                              <Field label={t("desktop.evals.case_id")} error={dup.has(c.id.trim()) ? t("desktop.evals.duplicate_id") : undefined}>
                                {(id) => <input id={id} className="input mono" value={c.id} onChange={(e) => setCase(c.key, { id: e.target.value })} />}
                              </Field>
                              <Field label={t("desktop.evals.case_name")}>{(id) => <input id={id} className="input" value={c.name} onChange={(e) => setCase(c.key, { name: e.target.value })} />}</Field>
                            </div>
                            <div className="pair">
                              <Field label={t("desktop.evals.prompt")}>{(id) => <textarea id={id} className="input" rows={4} value={c.prompt} placeholder={t("desktop.evals.prompt_placeholder")} onChange={(e) => setCase(c.key, { prompt: e.target.value })} />}</Field>
                              <Field label={t("desktop.evals.output")}>{(id) => <textarea id={id} className="input" rows={4} value={c.output} placeholder={t("desktop.evals.output_placeholder")} onChange={(e) => setCase(c.key, { output: e.target.value })} />}</Field>
                            </div>
                            <div className="row wrap" style={{ gap: 16, alignItems: "flex-start" }}>
                              <div className="stack" style={{ gap: 4 }}>
                                <span className="t-label">{t("desktop.evals.type")}</span>
                                <Segmented
                                  label={t("desktop.evals.type")}
                                  value={c.type}
                                  options={[
                                    { value: "exact", label: t("desktop.evals.type.exact") },
                                    { value: "near", label: t("desktop.evals.type.near") },
                                  ]}
                                  onChange={(v) => setCase(c.key, { type: v as CaseForm["type"] })}
                                />
                                <span className="t-body-sm muted">{t(`desktop.evals.type_hint.${c.type}`)}</span>
                              </div>
                              {c.type === "near" && (
                                <Field label={t("desktop.evals.threshold")}>
                                  {(id) => <input id={id} className="input eval-threshold" type="number" min={0.05} max={1} step={0.05} value={c.threshold} onChange={(e) => setCase(c.key, { threshold: e.target.value })} />}
                                </Field>
                              )}
                              <div className="stack" style={{ gap: 4 }}>
                                <span className="t-label">{t("desktop.evals.scope")}</span>
                                <Segmented
                                  label={t("desktop.evals.scope")}
                                  value={c.scope}
                                  options={(["fail", "warn", "info"] as const).map((s) => ({ value: s, label: t(`desktop.evals.scope.${s}`) }))}
                                  onChange={(v) => setCase(c.key, { scope: v as CaseForm["scope"] })}
                                />
                                <span className="t-body-sm muted">{t(`desktop.evals.scope_hint.${c.scope}`)}</span>
                              </div>
                            </div>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
                <div className="row">
                  <Button icon={mdiPlus} onClick={addCase}>
                    {t("desktop.evals.add_case")}
                  </Button>
                </div>
              </div>
            )}
            {tab === "settings" && (
              <div className="worker agent-form">
                <div className="pair">
                  <Field label={t("desktop.evals.name")} supporting={t("desktop.evals.name_hint")}>
                    {(id) => <input id={id} className="input mono" value={form.name} onChange={(e) => set("name", e.target.value)} />}
                  </Field>
                  <Field label={t("desktop.rules.description")}>{(id) => <input id={id} className="input" value={form.description} onChange={(e) => set("description", e.target.value)} />}</Field>
                </div>
                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.evals.agents")}</span>
                  <span className="t-body-sm muted">{form.agents.length ? t("desktop.evals.agents_some") : t("desktop.evals.agents_none")}</span>
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
                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.evals.skills")}</span>
                  <span className="t-body-sm muted">{t("desktop.evals.skills_hint")}</span>
                  <div className="row wrap" style={{ gap: 6 }}>
                    {[...new Set([...skills, ...form.skills])].map((s) => {
                      const on = form.skills.includes(s);
                      return (
                        <Chip key={s} selected={on} aria-pressed={on} onClick={() => set("skills", on ? form.skills.filter((x) => x !== s) : [...form.skills, s])}>
                          {s}
                        </Chip>
                      );
                    })}
                    {skills.length === 0 && form.skills.length === 0 && <span className="t-body-sm muted">{t("desktop.evals.no_skills")}</span>}
                  </div>
                </div>
                <div className="stack" style={{ gap: 4 }}>
                  <span className="t-label">{t("desktop.evals.near")}</span>
                  <Segmented
                    label={t("desktop.evals.near")}
                    value={form.near}
                    options={[
                      { value: "judge", label: t("desktop.evals.near.judge") },
                      { value: "embedding", label: t("desktop.evals.near.embedding") },
                    ]}
                    onChange={(v) => set("near", v as EvalForm["near"])}
                  />
                  <span className="t-body-sm muted">{t(`desktop.evals.near_hint.${form.near}`)}</span>
                </div>
                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.evals.allow")}</span>
                  <span className="t-body-sm muted">{t("desktop.evals.allow_hint")}</span>
                  <label className="row agent-switch">
                    <Switch label={t("desktop.evals.allow_network")} checked={form.allowNetwork} onChange={(v) => set("allowNetwork", v)} />
                    <span className="t-label">{t("desktop.evals.allow_network")}</span>
                  </label>
                  <label className="row agent-switch">
                    <Switch label={t("desktop.evals.allow_mcp")} checked={form.allowMcp} onChange={(v) => set("allowMcp", v)} />
                    <span className="t-label">{t("desktop.evals.allow_mcp")}</span>
                  </label>
                </div>
              </div>
            )}
            {tab === "runs" && (
              <div className="stack eval-runs" style={{ gap: 12 }}>
                {live && (
                  <section className="stack" style={{ gap: 6 }}>
                    <div className="row" style={{ gap: 8 }}>
                      <Icon path={statusIcon[live.run?.status ?? "running"]} spin={running} className={statusClass(live.run?.status ?? "running")} />
                      <span className="t-title-sm spacer">
                        {running ? t("desktop.evals.running", { done: live.results.length, total: live.total }) : t(`desktop.evals.run_status.${live.run?.status ?? "stopped"}`)}
                      </span>
                      <span className="t-body-sm muted">{summary({ counts: {}, results: live.results })}</span>
                    </div>
                    {running && <progress className="eval-progress" max={live.total} value={live.results.length} aria-label={t("desktop.evals.progress")} />}
                    <Results results={live.results} />
                  </section>
                )}
                {!live && runs.length === 0 && <p className="t-body-sm muted">{t("desktop.evals.no_runs")}</p>}
                {runs.length > 0 && (
                  <section className="stack" style={{ gap: 4 }}>
                    <span className="t-title-sm">{t("desktop.evals.past_runs")}</span>
                    <div className="list">
                      {runs.map((r) => (
                        <div key={r.id} className="stack" style={{ gap: 0 }}>
                          <div className="list-item">
                            <button type="button" className="row-open" aria-expanded={shown === r.id} onClick={() => setShown(shown === r.id ? "" : r.id)}>
                              <Icon path={statusIcon[r.status] ?? mdiFlaskOutline} className={statusClass(r.status)} />
                              <span className="lines spacer">
                                <span className="ellipsis">{r.started ? new Date(Number(r.started.seconds) * 1000).toLocaleString() : r.id}</span>
                                <small className="ellipsis">{[t(`desktop.evals.status.${r.status}`), summary(r), r.costUsd ? `$${r.costUsd.toFixed(4)}` : "", r.error].filter(Boolean).join(" · ")}</small>
                              </span>
                            </button>
                          </div>
                          {shownRun?.id === r.id && <Results results={r.results} />}
                        </div>
                      ))}
                    </div>
                  </section>
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
            {file?.problem && !dirty && (
              <div className="card error row small-card">
                <Icon path={mdiAlertCircleOutline} size="sm" /> {file.problem}
              </div>
            )}
            <div className="row wrap agent-actions slide-actions">
              <span className="t-body-sm muted spacer ellipsis" title={file?.path}>
                {t("desktop.agents.where", { path: file?.path || `.agents/evals/${form.name.trim() || "…"}.md` })}
              </span>
              {file && (
                <Button danger icon={mdiDeleteOutline} disabled={running} onClick={() => setDeleting(true)}>
                  {t("desktop.rules.delete")}
                </Button>
              )}
              {dirty && file && (
                <Button icon={mdiUndoVariant} onClick={() => setForm(base)}>
                  {t("desktop.agents.revert")}
                </Button>
              )}
              <Button onClick={close} disabled={running}>
                {t("desktop.cancel")}
              </Button>
              <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || running || !form.name.trim() || (!dirty && !isNew)} onClick={() => void save()}>
                {t("desktop.rules.save")}
              </Button>
            </div>
          </>
        )}
      </section>
      {deleting &&
        createPortal(
          <Dialog
            title={t("desktop.evals.delete_title", { name: title })}
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
            <p>{t("desktop.rules.delete_body", { path: file?.path ?? "" })}</p>
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
                    setForm(base);
                    go();
                  }}
                >
                  {t("desktop.agents.discard")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.agents.discard_body")}</p>
          </Dialog>,
          document.body,
        )}
    </div>
  );
}

/** A run's results, each a row that opens into the prompt, the output expected and the output, and why. */
function Results({ results }: { results: EvalResult[] }) {
  const [open, setOpen] = useState<number | null>(null);
  if (!results.length) return null;
  return (
    <div className="list eval-results">
      {results.map((r, i) => (
        <div key={`${r.case}-${r.agent}-${i}`} className="stack" style={{ gap: 0 }}>
          <div className="list-item">
            <button type="button" className="row-open" aria-expanded={open === i} onClick={() => setOpen(open === i ? null : i)}>
              <Icon path={statusIcon[r.status]} className={statusClass(r.status)} />
              <span className="lines spacer">
                <span className="ellipsis">
                  <span className="mono">{r.case}</span>
                  {r.name && ` · ${r.name}`}
                  {` · ${r.agent}`}
                </span>
                <small className={`ellipsis ${r.status === "error" ? "error-text" : ""}`}>{[t(`desktop.evals.result.${r.status}`), scoreText(r), r.reason].filter(Boolean).join(" · ")}</small>
              </span>
            </button>
          </div>
          {open === i && (
            <div className="card outlined stack eval-result-detail" style={{ gap: 8 }}>
              <div className="pair">
                <div className="stack" style={{ gap: 2 }}>
                  <span className="t-label">{t("desktop.evals.output")}</span>
                  <pre className="eval-text">{r.expected}</pre>
                </div>
                <div className="stack" style={{ gap: 2 }}>
                  <span className="t-label">{t("desktop.evals.got")}</span>
                  <pre className="eval-text">{r.output || "—"}</pre>
                </div>
              </div>
              {r.reason && <span className="t-body-sm">{r.reason}</span>}
              {r.refusals.length > 0 && (
                <div className="stack" style={{ gap: 2 }}>
                  <span className="t-label">{t("desktop.evals.refused")}</span>
                  {r.refusals.map((x) => (
                    <span key={x} className="t-body-sm muted mono">
                      {x}
                    </span>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
