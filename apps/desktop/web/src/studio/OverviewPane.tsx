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

import { useMemo, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { mdiAlertCircleOutline, mdiChartDonut, mdiChevronDown, mdiChevronRight, mdiCogOutline, mdiInformationOutline, mdiOpenInNew, mdiRefresh, mdiStop, mdiWrenchOutline } from "@mdi/js";
import { config, workspaces } from "../api";
import { message } from "../errors";
import { compose } from "../events";
import type { HealthItem, HealthVerdict, WorkspaceHealth } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";
import { Bars, Columns, ScoreRing, Sparkline, gradeTone } from "../ui/charts";
import { Button, Chip, Dialog, Field, Icon, IconButton, useSnackbar } from "../ui/controls";
import { cost, ms, took } from "../workflowRuns";
import { attention, dayColumns, dayTotals, delta, grade, healthKinds, itemKey, kindSummary, startCheck, stopCheck, topRuns, type HealthKind } from "./health";

// Studio's Overview (spec_health_044): the workspace's dashboard, every
// card opening in place into its detail.

const openKey = (dir: string) => `blitz.health.open.${dir}`;

function loadOpen(dir: string): string[] {
  try {
    return JSON.parse(localStorage.getItem(openKey(dir)) ?? "[]");
  } catch {
    return [];
  }
}

/** When something was, as a date and time ("" for never). */
const when = (t?: { seconds: bigint; nanos: number }) => (t ? new Date(ms(t)).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" }) : "");

/**
 * The Overview: the workspace's score and each kind's, its runs, its eval
 * coverage, what needs attention, and a kind's items once its ring is
 * picked (its card, closed again from its head); Re-check judges
 * what changed (Re-check all, everything), after saying what it costs.
 */
export function OverviewPane({ workspace, health, onOpen, onClose, onStarted }: { workspace: string; health?: WorkspaceHealth; onOpen: (kind: HealthKind, name: string, path: string) => void; onClose: () => void; onStarted: () => void }) {
  const snack = useSnackbar();
  const [open, setOpen] = useState<string[]>(() => loadOpen(workspace));
  const [asking, setAsking] = useState<{ force: boolean; items: number; cost: number; priced: boolean; scope: { kinds?: string[]; items?: string[]; force: boolean } } | null>(null);
  const [settings, setSettings] = useState(false);
  const [shown, setShown] = useState(""); // the item whose verdict shows
  const toggle = (id: string) =>
    setOpen((cur) => {
      const next = cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id];
      try {
        localStorage.setItem(openKey(workspace), JSON.stringify(next));
      } catch {
        // kept for this view only
      }
      return next;
    });
  const expand = (id: string) => {
    if (!open.includes(id)) toggle(id);
    requestAnimationFrame(() => document.getElementById(`health-${id}`)?.scrollIntoView({ behavior: "smooth", block: "start" }));
  };

  const ask = async (scope: { kinds?: string[]; items?: string[]; force: boolean }) => {
    try {
      const e = await workspaces.estimateHealthCheck({ workspace, scope });
      if (e.items === 0) return snack(t("desktop.health.nothing_due"));
      setAsking({ force: scope.force, items: e.items, cost: e.costUsd, priced: e.priced, scope });
    } catch (err) {
      snack(message(err), { error: true });
    }
  };

  if (!health) return <p className="muted overview-loading">{t("desktop.checking")}</p>;
  const items = health.items;
  const needs = attention(health);
  const totals = dayTotals(health.days);
  const agents = items.filter((it) => it.kind === "agent");
  const covered = agents.filter((a) => !a.flags.includes("uncovered"));
  const going = health.checking;
  const progress = health.progress;

  return (
    <div className="overview">
      <div className="row side-head overview-head">
        <span className="t-title-sm">{t("desktop.studio.overview")}</span>
        <span className="t-body-sm muted spacer ellipsis">
          {[
            health.lastCheck ? t("desktop.health.last_check", { when: when(health.lastCheck), cost: cost(health.lastCostUsd) }) : t("desktop.health.never_checked"),
            health.next ? t("desktop.health.next", { when: when(health.next) }) : "",
          ]
            .filter(Boolean)
            .join(" · ")}
        </span>
        <Button small icon={mdiRefresh} disabled={going} onClick={() => void ask({ force: false })}>
          {t("desktop.health.recheck")}
        </Button>
        <Button small variant="text" disabled={going} onClick={() => void ask({ force: true })}>
          {t("desktop.health.recheck_all")}
        </Button>
        <IconButton icon={mdiCogOutline} label={t("desktop.health.settings")} small onClick={() => setSettings(true)} />
      </div>
      {going && (
        <div className="card overview-progress row" role="status">
          <span className="spacer">
            {progress?.total
              ? t("desktop.health.judging", { index: progress.index, total: progress.total, item: progress.item, cost: cost(progress.costUsd) })
              : t("desktop.health.judging_start")}
          </span>
          <progress max={progress?.total || 1} value={Math.max(0, (progress?.index ?? 1) - 1)} />
          <Button small icon={mdiStop} onClick={() => void stopCheck(workspace)}>
            {t("desktop.evals.stop")}
          </Button>
        </div>
      )}
      {health.skipped && (
        <div className="card warn row small-card">
          <Icon path={mdiAlertCircleOutline} size="sm" /> {t("desktop.health.skipped", { why: health.skipped })}
        </div>
      )}

      <div className="overview-grid">
        <Card id="score" title={t("desktop.health.score")} open={open.includes("score")} onToggle={toggle} wide>
          <div className="overview-score row">
            <ScoreRing score={health.overall} grade={grade(health.overall)} size={112} label={scoreLabel(health.overall)} sub={health.overall ? health.overall.toFixed(1) : ""} />
            <div className="stack overview-score-text">
              <span className="t-title-md">{health.overall ? t("desktop.health.score_is", { grade: grade(health.overall), score: health.overall.toFixed(1) }) : t("desktop.health.not_judged")}</span>
              {delta(health.overall, health.previousOverall) && <span className="t-body-sm muted">{t("desktop.health.since_last", { delta: delta(health.overall, health.previousOverall) })}</span>}
              <span className="t-body-sm muted">{tn("desktop.health.needs_n", needs.length)}</span>
            </div>
            <div className="overview-kinds">
              {healthKinds.map((k) => {
                const s = kindSummary(items, k);
                const score = health.byKind[k] ?? 0;
                return (
                  <button key={k} type="button" className="overview-kind" onClick={() => expand(k)} title={t(`desktop.health.kind.${k}`)}>
                    <ScoreRing score={score} grade={grade(score)} size={52} label={`${t(`desktop.health.kind.${k}`)}: ${scoreLabel(score)}`} />
                    <span className="t-label">{t(`desktop.health.kind.${k}`)}</span>
                    <span className="t-body-sm muted">
                      {s.count}
                      {s.problems > 0 && <span className="error-text"> · {s.problems}!</span>}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>
          {open.includes("score") && (
            <table className="run-table overview-table">
              <thead>
                <tr>
                  <th>{t("desktop.health.kind_col")}</th>
                  <th className="num">{t("desktop.health.items")}</th>
                  <th className="num">{t("desktop.health.judged")}</th>
                  <th>{t("desktop.health.grades")}</th>
                  <th className="num">{t("desktop.health.flagged")}</th>
                </tr>
              </thead>
              <tbody>
                {healthKinds.map((k) => {
                  const s = kindSummary(items, k);
                  return (
                    <tr key={k} onClick={() => expand(k)}>
                      <td>{t(`desktop.health.kind.${k}`)}</td>
                      <td className="num">{s.count}</td>
                      <td className="num">{s.judged}</td>
                      <td>
                        {(["A", "B", "C", "D"] as const).map((g) =>
                          s.grades[g] ? (
                            <span key={g} className={`grade-badge ${gradeTone(g)}`} title={t("desktop.health.grade_is", { grade: g })}>
                              {g}×{s.grades[g]}
                            </span>
                          ) : null,
                        )}
                      </td>
                      <td className="num">{s.flagged + s.problems}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </Card>

        <Card id="runs" title={t("desktop.health.runs")} open={open.includes("runs")} onToggle={toggle}>
          <div className="row overview-stats">
            <Stat value={String(totals.runs)} label={t("desktop.health.runs_n")} />
            <Stat value={totals.runs ? `${Math.round(totals.rate * 100)}%` : "—"} label={t("desktop.health.success")} />
            <Stat value={cost(totals.cost)} label={t("desktop.health.cost")} />
          </div>
          <Columns columns={dayColumns(health.days, (d) => t("desktop.health.day_title", { date: d.date, done: d.done, failed: d.failed, stopped: d.stopped, cost: cost(d.costUsd) }))} label={t("desktop.health.runs_chart", { runs: totals.runs, cost: cost(totals.cost) })} />
          <div className="row chart-legend t-body-sm muted">
            <span className="legend done">{t("desktop.flows.run_status.done")}</span>
            <span className="legend failed">{t("desktop.flows.run_status.failed")}</span>
            <span className="legend stopped">{t("desktop.flows.run_status.stopped")}</span>
            <span className="legend line">{t("desktop.health.cost")}</span>
          </div>
          {open.includes("runs") && (
            <div className="overview-split">
              <TopTable title={t("desktop.health.top_cost")} items={topRuns(items, "cost")} value={(it) => cost(it.runs?.costUsd)} onOpen={onOpen} />
              <TopTable title={t("desktop.health.top_failed")} items={topRuns(items, "failed")} value={(it) => `${it.runs?.failed}/${it.runs?.runs}`} onOpen={onOpen} />
            </div>
          )}
        </Card>

        <Card id="coverage" title={t("desktop.health.coverage")} open={open.includes("coverage")} onToggle={toggle}>
          <div className="row overview-stats">
            <Stat value={`${covered.length}/${agents.length}`} label={t("desktop.health.covered")} />
            <Stat value={String(agents.filter((a) => a.flags.includes("stale")).length)} label={t("desktop.health.stale_n")} />
          </div>
          <Bars
            label={t("desktop.health.coverage")}
            bars={agents.filter((a) => !a.problem).slice(0, open.includes("coverage") ? undefined : 6).map((a) => ({
              label: a.name,
              value: a.evals?.cases ? a.evals.passed / a.evals.cases : 0,
              text: a.evals?.cases ? `${a.evals.passed}/${a.evals.cases}` : t("desktop.health.no_evals"),
              tone: !a.evals?.cases ? "none" : a.evals.passed === a.evals.cases ? "good" : a.evals.passed * 2 >= a.evals.cases ? "warn" : "bad",
            }))}
          />
        </Card>

        <Card id="attention" title={`${t("desktop.health.attention")} (${needs.length})`} open={open.includes("attention")} onToggle={toggle}>
          {needs.length === 0 ? (
            <p className="t-body-sm muted">{t("desktop.health.all_clear")}</p>
          ) : (
            <ul className="overview-attention">
              {needs.slice(0, open.includes("attention") ? undefined : 6).map((n, i) => {
                const it = items.find((x) => itemKey(x) === n.key);
                return (
                  <li key={`${n.key}-${n.why}-${i}`} className={`attention-${n.level}`}>
                    <Icon path={n.level === "info" ? mdiInformationOutline : mdiAlertCircleOutline} size="sm" />
                    <span className="spacer">
                      <b>{n.name}</b> {t(`desktop.health.why.${n.why}`, n.values)}
                    </span>
                    {it && <IconButton icon={mdiOpenInNew} small label={t("desktop.health.open")} onClick={() => onOpen(it.kind as HealthKind, it.name, it.path)} />}
                  </li>
                );
              })}
            </ul>
          )}
        </Card>

        {/* A kind's card shows once its ring opens it: closed, the ring above says it all. */}
        {healthKinds.filter((k) => open.includes(k)).map((k) => (
          <KindCard key={k} kind={k} items={items.filter((it) => it.kind === k)} score={health.byKind[k] ?? 0} open={open.includes(k)} onToggle={toggle} shown={shown} onShow={setShown} onOpen={onOpen}
            onRejudge={(key) => void ask({ items: [key], force: true })}
            onRecheck={() => void ask({ kinds: [k], force: false })}
            onImprove={(it, fix) => {
              compose({ dir: workspace, text: t("desktop.health.improve_prompt", { kind: t(`desktop.health.kind1.${it.kind}`), name: it.name, path: it.path, fix }) });
              onClose();
            }}
          />
        ))}
      </div>

      {asking &&
        createPortal(
          <Dialog
            title={t(asking.force ? "desktop.health.recheck_all" : "desktop.health.recheck")}
            icon={mdiChartDonut}
            onClose={() => setAsking(null)}
            footer={
              <>
                <Button onClick={() => setAsking(null)}>{t("desktop.cancel")}</Button>
                <Button
                  variant="filled"
                  onClick={() => {
                    const scope = asking.scope;
                    setAsking(null);
                    startCheck(workspace, scope).then(onStarted, (e) => snack(message(e), { error: true }));
                  }}
                >
                  {t("desktop.health.judge_go")}
                </Button>
              </>
            }
          >
            <p>{t(asking.priced ? "desktop.health.estimate" : "desktop.health.estimate_unpriced", { items: asking.items, cost: cost(asking.cost) })}</p>
            <p className="t-body-sm muted">{t(asking.force ? "desktop.health.estimate_model_all" : "desktop.health.estimate_model", { model: health.model || t("desktop.health.default_model") })}</p>
          </Dialog>,
          document.body,
        )}
      {settings && createPortal(<HealthSettings workspace={workspace} health={health} onClose={() => setSettings(false)} />, document.body)}
    </div>
  );
}

const scoreLabel = (score: number) => (score ? t("desktop.health.score_is", { grade: grade(score), score: score.toFixed(1) }) : t("desktop.health.not_judged"));

/** A dashboard card: its title, a button that opens it into its detail, and what it shows. */
function Card({ id, title, open, onToggle, wide, children }: { id: string; title: string; open: boolean; onToggle: (id: string) => void; wide?: boolean; children: ReactNode }) {
  return (
    <section id={`health-${id}`} className={`card overview-card ${wide ? "wide" : ""} ${open ? "is-open" : ""}`} aria-label={title}>
      <button type="button" className="row overview-card-head" aria-expanded={open} onClick={() => onToggle(id)}>
        <span className="t-title-sm spacer">{title}</span>
        <Icon path={open ? mdiChevronDown : mdiChevronRight} size="sm" />
      </button>
      {children}
    </section>
  );
}

function Stat({ value, label }: { value: string; label: string }) {
  return (
    <div className="overview-stat">
      <span className="t-title-lg">{value}</span>
      <span className="t-body-sm muted">{label}</span>
    </div>
  );
}

function TopTable({ title, items, value, onOpen }: { title: string; items: HealthItem[]; value: (it: HealthItem) => string; onOpen: (kind: HealthKind, name: string, path: string) => void }) {
  return (
    <div className="stack" style={{ gap: 4 }}>
      <span className="t-label">{title}</span>
      {items.length === 0 ? (
        <span className="t-body-sm muted">{t("desktop.none_found")}</span>
      ) : (
        items.map((it) => (
          <button key={itemKey(it)} type="button" className="row overview-top" onClick={() => onOpen(it.kind as HealthKind, it.name, it.path)}>
            <span className="spacer ellipsis">{it.kind === "workflow" ? `/${it.name}` : it.name}</span>
            <span className="t-body-sm">{value(it)}</span>
          </button>
        ))
      )}
    </div>
  );
}

/** A kind's card: its ring and counts; open, its items as a table, each opening into its verdict. */
function KindCard({ kind, items, score, open, onToggle, shown, onShow, onOpen, onRejudge, onRecheck, onImprove }: {
  kind: HealthKind;
  items: HealthItem[];
  score: number;
  open: boolean;
  onToggle: (id: string) => void;
  shown: string;
  onShow: (key: string) => void;
  onOpen: (kind: HealthKind, name: string, path: string) => void;
  onRejudge: (key: string) => void;
  onRecheck: () => void;
  onImprove: (it: HealthItem, fix: string) => void;
}) {
  const s = kindSummary(items, kind);
  const runs = kind === "workflow" || kind === "worker";
  return (
    <Card id={kind} title={`${t(`desktop.health.kind.${kind}`)} (${s.count})`} open={open} onToggle={onToggle} wide={open}>
      <div className="row overview-kind-row">
        <ScoreRing score={score} grade={grade(score)} size={48} label={scoreLabel(score)} />
        <span className="t-body-sm muted spacer">
          {[tn("desktop.health.judged_n", s.judged), s.problems ? tn("desktop.health.problems_n", s.problems) : "", s.flagged ? tn("desktop.health.flagged_n", s.flagged) : ""].filter(Boolean).join(" · ")}
        </span>
        <Button small variant="text" icon={mdiRefresh} disabled={s.count === 0} onClick={onRecheck}>
          {t("desktop.health.recheck")}
        </Button>
      </div>
      {open && (
        <table className="run-table overview-table">
          <thead>
            <tr>
              <th>{t("desktop.health.name")}</th>
              <th>{t("desktop.health.grade")}</th>
              <th>{t("desktop.health.flags")}</th>
              <th className="num">{runs ? t("desktop.health.runs_n") : t("desktop.health.uses")}</th>
              <th>{t("desktop.health.trend")}</th>
              <th className="num">{runs ? t("desktop.health.cost") : t("desktop.health.evals")}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {items.length === 0 && (
              <tr>
                <td colSpan={7} className="muted">
                  {t("desktop.none_found")}
                </td>
              </tr>
            )}
            {items.map((it) => {
              const key = itemKey(it);
              const isShown = shown === key;
              return (
                <ItemRows key={key} it={it} runs={runs} isShown={isShown} onShow={() => onShow(isShown ? "" : key)} onOpen={() => onOpen(kind, it.name, it.path)} onRejudge={() => onRejudge(key)} onImprove={(fix) => onImprove(it, fix)} />
              );
            })}
          </tbody>
        </table>
      )}
    </Card>
  );
}

function ItemRows({ it, runs, isShown, onShow, onOpen, onRejudge, onImprove }: { it: HealthItem; runs: boolean; isShown: boolean; onShow: () => void; onOpen: () => void; onRejudge: () => void; onImprove: (fix: string) => void }) {
  const v = it.verdict;
  const history = it.history.length > 1 ? it.history : [];
  return (
    <>
      <tr className={`overview-item ${isShown ? "is-shown" : ""} ${it.problem ? "has-problem" : ""}`} onClick={onShow} aria-expanded={isShown}>
        <td className="ellipsis" title={it.path}>
          <Icon path={isShown ? mdiChevronDown : mdiChevronRight} size="sm" /> {it.kind === "workflow" ? `/${it.name}` : it.name}
          {it.detail && <span className="t-body-sm muted"> · {it.detail}</span>}
        </td>
        <td>
          {it.problem ? (
            <span className="grade-badge grade-d" title={it.problem}>
              !
            </span>
          ) : v ? (
            <span className={`grade-badge ${gradeTone(v.grade)} ${v.current ? "" : "is-old"}`} title={v.current ? t("desktop.health.grade_is", { grade: v.grade }) : t("desktop.health.changed_since")}>
              {v.grade}
            </span>
          ) : (
            <span className="muted">—</span>
          )}
        </td>
        <td>
          {it.flags.map((f) => (
            <span key={f} className={`flag-chip flag-${f}`}>
              {t(`desktop.health.flag.${f}`)}
            </span>
          ))}
        </td>
        <td className="num">
          {runs ? (it.runs?.runs ?? 0) : it.uses}
          <Sparkline values={it.daily} label={t("desktop.health.daily")} width={56} height={16} />
        </td>
        <td>{history.length ? <Sparkline values={history} label={t("desktop.health.trend")} width={56} height={16} /> : <span className="muted">—</span>}</td>
        <td className="num">{runs ? cost(it.runs?.costUsd) : it.evals?.cases ? `${it.evals.passed}/${it.evals.cases}` : "—"}</td>
        <td>
          <IconButton
            icon={mdiOpenInNew}
            small
            label={t("desktop.health.open")}
            onClick={(e) => {
              e.stopPropagation();
              onOpen();
            }}
          />
        </td>
      </tr>
      {isShown && (
        <tr className="overview-verdict-row">
          <td colSpan={7}>
            <ItemDetail it={it} runs={runs} verdict={v} onRejudge={onRejudge} onImprove={onImprove} />
          </td>
        </tr>
      )}
    </>
  );
}

/** An item's facts and its verdict: each rubric line's score and reason, the summary, the fixes. */
function ItemDetail({ it, runs, verdict, onRejudge, onImprove }: { it: HealthItem; runs: boolean; verdict?: HealthVerdict; onRejudge: () => void; onImprove: (fix: string) => void }) {
  const facts = useMemo(() => {
    const out: string[] = [];
    if (it.problem) out.push(it.problem);
    if (runs && it.runs?.runs) {
      out.push(t("desktop.health.fact_runs", { runs: it.runs.runs, done: it.runs.done, failed: it.runs.failed, cost: cost(it.runs.costUsd), median: took(1, 1 + Number(it.runs.medianMs)) }));
      if (it.runs.failingStep) out.push(t("desktop.health.fact_failing", { step: it.runs.failingStep }));
    }
    if (it.evals?.cases) out.push(t("desktop.health.fact_evals", { passed: it.evals.passed, cases: it.evals.cases, when: when(it.evals.last) }));
    if (it.kind === "rule" || it.kind === "guardrail") out.push(t("desktop.health.fact_fired", { uses: it.uses, fired: it.fired }));
    if (it.kind === "skill") out.push(tn("desktop.health.fact_activated", it.uses));
    return out;
  }, [it, runs]);
  return (
    <div className="overview-detail stack">
      {facts.length > 0 && (
        <ul className="t-body-sm overview-facts">
          {facts.map((f) => (
            <li key={f}>{f}</li>
          ))}
        </ul>
      )}
      {verdict ? (
        <>
          <p className="t-body-md">
            {verdict.summary}{" "}
            <span className="t-body-sm muted">
              {t("desktop.health.judged_by", { when: when(verdict.at), model: verdict.model || t("desktop.health.default_model"), cost: cost(verdict.costUsd) })}
              {!verdict.current && ` · ${t("desktop.health.changed_since")}`}
            </span>
          </p>
          <div className="overview-scores">
            {verdict.scores.map((s) => (
              <div key={s.line} className="overview-score-line">
                <span className="score-pips" aria-label={t("desktop.health.score_of", { score: s.score })}>
                  {[1, 2, 3, 4, 5].map((n) => (
                    <span key={n} className={n <= s.score ? `pip on ${gradeTone(grade(s.score))}` : "pip"} />
                  ))}
                </span>
                <span className="stack" style={{ gap: 0 }}>
                  <span className="t-label">{s.line}</span>
                  <span className="t-body-sm muted">{s.reason}</span>
                </span>
              </div>
            ))}
          </div>
          {verdict.fixes.length > 0 && (
            <div className="stack" style={{ gap: 6 }}>
              <span className="t-label">{t("desktop.health.fixes")}</span>
              {verdict.fixes.map((f) => (
                <div key={f} className="row overview-fix">
                  <span className="spacer t-body-sm">{f}</span>
                  {it.path && (
                    <Chip icon={mdiWrenchOutline} onClick={() => onImprove(f)}>
                      {t("desktop.health.improve")}
                    </Chip>
                  )}
                </div>
              ))}
            </div>
          )}
        </>
      ) : (
        !it.problem && <p className="t-body-sm muted">{t("desktop.health.not_judged_item")}</p>
      )}
      {!it.problem && (
        <div className="row">
          <Button small variant="text" icon={mdiRefresh} onClick={onRejudge}>
            {t("desktop.health.rejudge")}
          </Button>
        </div>
      )}
    </div>
  );
}

/** The judge's model, the schedule and a scheduled check's cost limit, and the reflection's schedule and limit, kept in the workspace's own settings. */
function HealthSettings({ workspace, health, onClose }: { workspace: string; health: WorkspaceHealth; onClose: () => void }) {
  const snack = useSnackbar();
  const [schedule, setSchedule] = useState(health.schedule);
  const [limit, setLimit] = useState(health.maxCostUsd ? String(health.maxCostUsd) : "");
  const [model, setModel] = useState(health.model);
  const [reflect, setReflect] = useState(health.reflectSchedule);
  const [reflectLimit, setReflectLimit] = useState(health.reflectMaxCostUsd ? String(health.reflectMaxCostUsd) : "");
  const [threshold, setThreshold] = useState(String(health.reflectThreshold));
  const [busy, setBusy] = useState(false);
  const save = async () => {
    setBusy(true);
    try {
      for (const [key, value] of [
        ["health.schedule", schedule.trim() || "off"],
        ["health.max_cost_usd", limit.trim() || "0"],
        ["health.model", model.trim()],
        ["harness.reflect_schedule", reflect.trim() || "off"],
        ["harness.max_cost_usd", reflectLimit.trim() || "0"],
        ["harness.reflect_threshold", threshold.trim() || "0"],
      ] as const) {
        await config.setConfigValue({ workspace, key, value });
      }
      snack(t("desktop.health.settings_saved"));
      onClose();
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t("desktop.health.settings")}
      icon={mdiCogOutline}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" disabled={busy} onClick={() => void save()}>
            {t("desktop.rules.save")}
          </Button>
        </>
      }
    >
      <div className="stack" style={{ gap: 12 }}>
        <Field label={t("desktop.health.schedule")} supporting={t("desktop.health.schedule_hint")}>
          {(id) => <input id={id} className="input" value={schedule} placeholder={t("desktop.health.schedule_placeholder")} onChange={(e) => setSchedule(e.target.value)} />}
        </Field>
        <Field label={t("desktop.health.max_cost")} supporting={t("desktop.health.max_cost_hint")}>
          {(id) => <input id={id} className="input" inputMode="decimal" value={limit} placeholder="1" onChange={(e) => setLimit(e.target.value)} />}
        </Field>
        <Field label={t("desktop.health.model")} supporting={t("desktop.health.model_hint")}>
          {(id) => <input id={id} className="input mono" value={model} placeholder={t("desktop.health.default_model")} onChange={(e) => setModel(e.target.value)} />}
        </Field>
        <Field label={t("desktop.health.reflect_schedule")} supporting={health.reflectSkipped ? t("desktop.health.reflect_skipped", { why: health.reflectSkipped }) : t("desktop.health.reflect_schedule_hint")}>
          {(id) => <input id={id} className="input" value={reflect} placeholder={t("desktop.health.reflect_schedule_placeholder")} onChange={(e) => setReflect(e.target.value)} />}
        </Field>
        <Field label={t("desktop.health.reflect_max_cost")} supporting={t("desktop.health.reflect_max_cost_hint")}>
          {(id) => <input id={id} className="input" inputMode="decimal" value={reflectLimit} placeholder="0.5" onChange={(e) => setReflectLimit(e.target.value)} />}
        </Field>
        <Field label={t("desktop.health.reflect_threshold")} supporting={t("desktop.health.reflect_threshold_hint")}>
          {(id) => <input id={id} className="input" inputMode="numeric" value={threshold} placeholder="5" onChange={(e) => setThreshold(e.target.value)} />}
        </Field>
      </div>
    </Dialog>
  );
}
