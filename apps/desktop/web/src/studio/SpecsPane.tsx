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

import { useCallback, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { mdiAlertCircleOutline, mdiAlertOutline, mdiArrowLeft, mdiAutoFix, mdiCheckDecagramOutline, mdiFormatListBulleted, mdiMagnifyScan, mdiTableLarge, mdiCheckCircleOutline, mdiCompare, mdiContentSaveOutline, mdiDeleteOutline, mdiFileDocumentOutline, mdiFormatListNumbered, mdiInformationOutline, mdiLoading, mdiPlus, mdiRobotOutline, mdiShieldSearch, mdiUndoVariant } from "@mdi/js";
import { Button, Dialog, Field, Icon, IconButton, useSnackbar } from "@blitz/webui";
import { files, workspaces } from "../api";
import { message, reason } from "../errors";
import type { SpecFinding, SpecRequirement, SpecSummary } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";
import { cost } from "../workflowRuns";
import { ComposeDialog } from "./ComposeDialog";
import { DraftButton } from "./Draft";
import { MarkdownField } from "./MarkdownField";
import { ConfirmDialog, useDiscardGuard } from "./paneParts";
import { applyHunks, lineHunks, newSpecFile, newSpecText, specsDir, specsIndex } from "./specs";
import { RequirementTable } from "./SpecTrace";

/** The spec open in the form: its summary, the text read and its version. */
interface OpenSpec {
  spec: SpecSummary;
  base: string;
  version: string;
}

/** How long typing pauses before the checks run again. */
const reviewDelay = 500;

/** A spec as the list names it: "Index", "001 · Overview", or its title. */
function specLabel(s: SpecSummary): string {
  if (s.file === specsIndex) return t("desktop.specs.index");
  const title = s.title || s.file.replace(/\.md$/i, "");
  return s.number ? `${String(s.number).padStart(3, "0")} · ${title}` : title;
}

/**
 * The project's specification (spec_040_studio STU-11, spec_057_spec_driven):
 * the specs /spec writes in .agents/specs, the index first, then in order,
 * each with its status and what the checks found (ListSpecs). A spec opens
 * over the list in a Markdown field; the checks run as it's edited
 * (ReviewSpec), **Review with AI** adds the model's, **Refine with AI** is
 * a conversation whose changes are kept or dropped one by one, and Redraft
 * rewrites it. Save writes it back, refused when it changed on disk since
 * it was read.
 */
export function SpecsPane({ workspace, onDirty }: { workspace: string; onDirty?: (dirty: boolean) => void }) {
  const snack = useSnackbar();
  const [specs, setSpecs] = useState<SpecSummary[]>([]);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<OpenSpec | null>(null);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  const [adding, setAdding] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [checks, setChecks] = useState<SpecFinding[]>([]);
  const [judged, setJudged] = useState<{ findings: SpecFinding[]; cost: number; problem: string } | null>(null);
  const [judging, setJudging] = useState(false);
  const [refining, setRefining] = useState(false);
  const [proposal, setProposal] = useState<string | null>(null);
  // The requirements as traced (SDD-10), and every spec's (the matrix).
  const [reqs, setReqs] = useState<SpecRequirement[]>([]);
  const [matrix, setMatrix] = useState<SpecRequirement[] | null>(null);
  const [notReady, setNotReady] = useState("");
  const [tracing, setTracing] = useState<"" | "approve" | "verify" | "judge">("");
  const [estimate, setEstimate] = useState<{ usd: number; priced: boolean } | null>(null);

  const refresh = useCallback(async () => {
    try {
      setSpecs((await workspaces.listSpecs({ workspace })).specs);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => void refresh(), [refresh]);

  const dirty = open !== null && text !== open.base;
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);
  const { leave, discard } = useDiscardGuard(dirty);

  // The checks, as the spec is edited (SDD-02).
  const path = open?.spec.path ?? "";
  useEffect(() => {
    if (!path) return;
    const timer = setTimeout(() => {
      workspaces.reviewSpec({ workspace, path, text, judge: false }).then(
        (r) => setChecks(r.findings),
        () => setChecks([]),
      );
    }, reviewDelay);
    return () => clearTimeout(timer);
  }, [workspace, path, text]);

  const trace = (p: string) =>
    workspaces.traceSpecs({ workspace, path: p }).then(
      (r) => setReqs(r.requirements),
      (e) => snack(message(e), { error: true }),
    );
  const begin = (spec: SpecSummary, body: string, version: string) => {
    setOpen({ spec, base: body, version });
    setText(body);
    setProblem("");
    setChecks([]);
    setJudged(null);
    setReqs([]);
    if (spec.file !== specsIndex) void trace(spec.path);
  };
  const showMatrix = async () => {
    try {
      setMatrix((await workspaces.traceSpecs({ workspace, path: "" })).requirements);
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  // Approve (SDD-11): the saved spec; with errors or open questions it asks first.
  const approve = async (force: boolean) => {
    if (!open) return;
    setNotReady("");
    setTracing("approve");
    try {
      const a = await workspaces.approveSpec({ workspace, path: open.spec.path, force });
      const r = await files.readFile({ workspace, path: open.spec.path });
      setOpen({ ...open, base: r.text, version: r.version });
      setText(r.text);
      snack(a.diff ? t("desktop.specs.approved_diff", { name: open.spec.file, path: a.diff }) : t("desktop.specs.approved", { name: open.spec.file }));
      await Promise.all([trace(open.spec.path), refresh()]);
    } catch (e) {
      if (reason(e) === "SPEC_NOT_READY") setNotReady(message(e));
      else snack(message(e), { error: true });
    } finally {
      setTracing("");
    }
  };
  // Verify (SDD-12): the check at once; the model's after its estimate.
  const verify = async (judge: boolean, spend: boolean) => {
    if (!open) return;
    setEstimate(null);
    setTracing(judge ? "judge" : "verify");
    try {
      const v = await workspaces.verifySpec({ workspace, path: open.spec.path, judge, estimate: judge && !spend });
      if (judge && !spend) {
        if (v.problem) snack(v.problem, { error: true });
        else setEstimate({ usd: v.estimatedUsd, priced: v.priced });
        return;
      }
      setReqs(v.requirements);
      if (v.problem) snack(t("desktop.specs.verify_problem", { problem: v.problem }), { error: true });
      else snack(judge ? t("desktop.specs.verified_cost", { cost: cost(v.costUsd) }) : t("desktop.specs.verified"));
      void refresh();
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setTracing("");
    }
  };
  const show = (spec: SpecSummary) =>
    leave(async () => {
      try {
        const r = await files.readFile({ workspace, path: spec.path });
        begin(spec, r.text, r.version);
      } catch (e) {
        snack(message(e), { error: true });
      }
    });
  const close = () =>
    leave(() => {
      setOpen(null);
      void refresh();
    });

  const save = async () => {
    if (!open || busy) return;
    setBusy(true);
    setProblem("");
    try {
      const r = await files.writeFile({ workspace, path: open.spec.path, text, version: open.version });
      setOpen({ ...open, base: text, version: r.version });
      snack(t("desktop.specs.saved", { name: open.spec.file }));
    } catch (e) {
      setProblem(message(e));
    } finally {
      setBusy(false);
    }
  };
  const numbers = specs.map((s) => s.number);
  const add = async (title: string) => {
    const file = newSpecFile(title, numbers);
    if (!file) return;
    setAdding(null);
    const p = `${specsDir}/${file}`;
    try {
      const body = newSpecText(title, file);
      const r = await files.writeFile({ workspace, path: p, text: body, version: "" });
      const list = (await workspaces.listSpecs({ workspace })).specs;
      setSpecs(list);
      const spec = list.find((s) => s.path === p);
      if (spec) begin(spec, body, r.version);
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const remove = async () => {
    setDeleting(false);
    if (!open) return;
    try {
      await files.deleteFile({ workspace, path: open.spec.path });
      snack(t("desktop.specs.deleted", { name: open.spec.file }));
      setOpen(null);
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  // The model's review (SDD-03): it costs, so only when asked.
  const judge = async () => {
    if (!open) return;
    setJudging(true);
    try {
      const r = await workspaces.reviewSpec({ workspace, path: open.spec.path, text, judge: true });
      setChecks(r.findings.filter((f) => !f.judged));
      setJudged({ findings: r.findings.filter((f) => f.judged), cost: r.costUsd, problem: r.problem });
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setJudging(false);
    }
  };

  const isIndex = open?.spec.file === specsIndex;
  return (
    <div className={`slide-pane specs-pane ${open ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        <div className="row side-head">
          <span className="t-title-sm spacer">{t("desktop.specs.title")}</span>
          <IconButton icon={matrix ? mdiFormatListBulleted : mdiTableLarge} label={t(matrix ? "desktop.specs.show_list" : "desktop.specs.show_matrix")} small selected={!!matrix} onClick={() => (matrix ? setMatrix(null) : void showMatrix())} />
          <IconButton icon={mdiPlus} label={t("desktop.specs.new")} small onClick={() => setAdding("")} />
        </div>
        <p className="t-body-sm muted agent-dir">{t("desktop.workers.in", { path: specsDir })}</p>
        {error && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
          </div>
        )}
        {!error && specs.length === 0 && <p className="t-body-sm muted">{t("desktop.specs.none")}</p>}
        {matrix && (
          <RequirementTable
            requirements={matrix}
            matrix
            onOpen={(file) => {
              const s = specs.find((x) => x.file === file);
              if (s) void show(s);
            }}
          />
        )}
        {!matrix && (
        <div className="list">
          {specs.map((s) => (
            <button key={s.path} className="list-item" title={s.path} onClick={() => void show(s)}>
              <Icon path={s.file === specsIndex ? mdiFormatListNumbered : mdiFileDocumentOutline} />
              <span className="lines spacer">
                <span className="ellipsis">{specLabel(s)}</span>
                <small className="ellipsis">{[s.file, s.status, s.items ? tn("desktop.specs.items", s.items) : "", s.approved && s.items ? t("desktop.specs.traced_of", { traced: s.traced, count: s.items }) : ""].filter(Boolean).join(" · ")}</small>
              </span>
              {s.changed > 0 && (
                <span className="row t-body-sm warn-text" style={{ gap: 4 }} title={tn("desktop.specs.changed", s.changed)}>
                  <Icon path={mdiAlertCircleOutline} size="sm" /> {tn("desktop.specs.changed", s.changed)}
                </span>
              )}
              {s.errors > 0 ? (
                <span className="row t-body-sm error-text" style={{ gap: 4 }} title={tn("desktop.specs.errors", s.errors)}>
                  <Icon path={mdiAlertCircleOutline} size="sm" /> {s.errors}
                </span>
              ) : s.warnings > 0 ? (
                <span className="row t-body-sm muted" style={{ gap: 4 }} title={tn("desktop.specs.warnings", s.warnings)}>
                  <Icon path={mdiAlertOutline} size="sm" /> {s.warnings}
                </span>
              ) : null}
            </button>
          ))}
        </div>
        )}
      </aside>
      <section className="slide-detail" inert={open === null}>
        {open && (
          <>
            <div className="row slide-back">
              <IconButton icon={mdiArrowLeft} label={t("desktop.specs.back")} onClick={close} />
              <h2 className="t-headline spacer ellipsis">{specLabel(open.spec)}</h2>
              {dirty && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
            </div>
            <div className="worker agent-form">
              <MarkdownField
                key={open.spec.path}
                label={open.spec.file}
                supporting={t("desktop.specs.hint")}
                value={text}
                onValue={setText}
                dir={workspace}
                path={open.spec.path}
                workspace={workspace}
                rows={18}
                height={440}
                onSave={() => void save()}
                tools={
                  <>
                    {!isIndex && (
                      <Button small icon={judging ? mdiLoading : mdiShieldSearch} iconSpin={judging} disabled={judging} title={t("desktop.specs.judge_hint")} onClick={() => void judge()}>
                        {judging ? t("desktop.specs.judging") : t("desktop.specs.judge")}
                      </Button>
                    )}
                    {!isIndex && (
                      <Button small icon={mdiAutoFix} title={t("desktop.specs.refine_hint")} onClick={() => setRefining(true)}>
                        {t("desktop.specs.refine")}
                      </Button>
                    )}
                    <DraftButton workspace={workspace} kind="spec" name={open.spec.file.replace(/\.md$/i, "")} description={open.spec.title} current={text} onUse={setText} />
                  </>
                }
              />
              {problem && (
                <div className="card error row small-card" style={{ gap: 8 }}>
                  <Icon path={mdiAlertCircleOutline} size="sm" />
                  <span className="t-body-sm">{problem}</span>
                </div>
              )}
              <Findings checks={checks} judged={judged} />
              {!isIndex && (
                <div className="card outlined stack spec-trace" style={{ gap: 8 }}>
                  <div className="row wrap" style={{ gap: 8 }}>
                    <span className="t-title-sm spacer">{t("desktop.specs.requirements")}</span>
                    {tracing === "approve" || tracing === "verify" || tracing === "judge" ? <Icon path={mdiLoading} spin size="sm" /> : null}
                    <Button small icon={mdiCheckDecagramOutline} disabled={dirty || !!tracing} title={dirty ? t("desktop.specs.save_first") : t("desktop.specs.approve_hint")} onClick={() => void approve(false)}>
                      {t("desktop.specs.approve")}
                    </Button>
                    <Button small icon={mdiMagnifyScan} disabled={dirty || !!tracing} title={dirty ? t("desktop.specs.save_first") : t("desktop.specs.verify_hint")} onClick={() => void verify(false, false)}>
                      {t("desktop.specs.verify")}
                    </Button>
                    <Button small icon={mdiRobotOutline} disabled={dirty || !!tracing} title={dirty ? t("desktop.specs.save_first") : t("desktop.specs.judge_verify_hint")} onClick={() => void verify(true, false)}>
                      {t("desktop.specs.judge_verify")}
                    </Button>
                  </div>
                  <RequirementTable requirements={reqs} />
                </div>
              )}
            </div>
            <div className="row wrap agent-actions slide-actions">
              <span className="t-body-sm muted spacer ellipsis" title={open.spec.path}>
                {t("desktop.agents.where", { path: open.spec.path })}
              </span>
              <Button danger icon={mdiDeleteOutline} onClick={() => setDeleting(true)}>
                {t("desktop.specs.delete")}
              </Button>
              {dirty && (
                <Button icon={mdiUndoVariant} onClick={() => setText(open.base)}>
                  {t("desktop.agents.revert")}
                </Button>
              )}
              <Button onClick={close}>{t("desktop.close")}</Button>
              <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || !dirty} onClick={() => void save()}>
                {t("desktop.specs.save")}
              </Button>
            </div>
          </>
        )}
      </section>
      {adding !== null && (
        <Dialog
          title={t("desktop.specs.new")}
          icon={mdiPlus}
          onClose={() => setAdding(null)}
          footer={
            <>
              <Button onClick={() => setAdding(null)}>{t("desktop.cancel")}</Button>
              <Button variant="filled" disabled={!newSpecFile(adding, numbers)} onClick={() => void add(adding)}>
                {t("desktop.specs.create")}
              </Button>
            </>
          }
        >
          <Field label={t("desktop.specs.new_title")} supporting={newSpecFile(adding, numbers) ? t("desktop.workers.in", { path: `${specsDir}/${newSpecFile(adding, numbers)}` }) : t("desktop.specs.new_hint")}>
            {(id) => (
              <input
                id={id}
                className="input"
                autoFocus
                value={adding}
                onChange={(e) => setAdding(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && newSpecFile(adding, numbers)) void add(adding);
                }}
              />
            )}
          </Field>
        </Dialog>
      )}
      {deleting && open && (
        <ConfirmDialog title={t("desktop.specs.delete_title", { name: open.spec.file })} icon={mdiDeleteOutline} confirm={t("desktop.specs.delete")} onConfirm={() => void remove()} onClose={() => setDeleting(false)}>
          <p>{t("desktop.specs.delete_body", { path: open.spec.path })}</p>
        </ConfirmDialog>
      )}
      {notReady && (
        <ConfirmDialog title={t("desktop.specs.not_ready_title")} icon={mdiCheckDecagramOutline} confirm={t("desktop.specs.approve_anyway")} danger={false} onConfirm={() => void approve(true)} onClose={() => setNotReady("")}>
          <p>{notReady}</p>
        </ConfirmDialog>
      )}
      {estimate && (
        <ConfirmDialog title={t("desktop.specs.judge_verify")} icon={mdiRobotOutline} confirm={t("desktop.specs.spend")} danger={false} onConfirm={() => void verify(true, true)} onClose={() => setEstimate(null)}>
          <p>{estimate.priced ? t("desktop.specs.estimate", { cost: cost(estimate.usd) }) : t("desktop.specs.estimate_unpriced")}</p>
        </ConfirmDialog>
      )}
      {refining && open && (
        <ComposeDialog
          workspace={workspace}
          kind="spec"
          current={text}
          onClose={() => setRefining(false)}
          onDraft={(d) => {
            setRefining(false);
            setProposal(d.file);
          }}
        />
      )}
      {proposal !== null &&
        createPortal(
          <SpecChanges
            before={text}
            after={proposal}
            onUse={(merged) => {
              setText(merged);
              setProposal(null);
            }}
            onClose={() => setProposal(null)}
          />,
          document.body,
        )}
      {discard(open?.spec.file ?? "")}
    </div>
  );
}

/** What the checks found, then the model's review when it was asked for. */
function Findings({ checks, judged }: { checks: SpecFinding[]; judged: { findings: SpecFinding[]; cost: number; problem: string } | null }) {
  const icon = (f: SpecFinding) => (f.judged ? mdiRobotOutline : f.severity === "error" ? mdiAlertCircleOutline : f.severity === "warning" ? mdiAlertOutline : mdiInformationOutline);
  const rows = [...checks, ...(judged?.findings ?? [])];
  return (
    <div className="card outlined stack spec-findings" style={{ gap: 6 }}>
      <span className="row t-title-sm" style={{ gap: 6 }}>
        {rows.length === 0 && <Icon path={mdiCheckCircleOutline} size="sm" />}
        {rows.length === 0 ? t("desktop.specs.sound") : tn("desktop.specs.found", rows.length)}
      </span>
      {rows.map((f, i) => (
        <span key={i} className={`row t-body-sm spec-finding ${f.severity === "error" ? "error-text" : ""}`} style={{ gap: 6, alignItems: "flex-start" }}>
          <Icon path={icon(f)} size="sm" />
          <span>
            {f.line > 0 && <span className="muted">{t("desktop.specs.line", { line: f.line })} · </span>}
            {f.message}
          </span>
        </span>
      ))}
      {judged && <span className="t-body-sm muted">{judged.problem ? t("desktop.specs.judge_problem", { problem: judged.problem }) : t("desktop.specs.judged", { cost: cost(judged.cost) })}</span>}
    </div>
  );
}

/**
 * A refined spec's changes (SDD-04), each kept or dropped: the removed
 * lines and the ones that take their place, every change kept at first;
 * Use puts the result in the form, unsaved.
 */
function SpecChanges({ before, after, onUse, onClose }: { before: string; after: string; onUse: (text: string) => void; onClose: () => void }) {
  const hunks = useMemo(() => lineHunks(before, after), [before, after]);
  const [keep, setKeep] = useState(() => hunks.map(() => true));
  return (
    <Dialog
      title={t("desktop.specs.changes")}
      icon={mdiCompare}
      wide
      onClose={onClose}
      footer={
        <>
          <span className="t-body-sm muted spacer">{t("desktop.specs.changes_kept", { kept: keep.filter(Boolean).length, count: hunks.length })}</span>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" disabled={!keep.some(Boolean)} onClick={() => onUse(applyHunks(before, hunks, keep))}>
            {t("desktop.specs.use_changes")}
          </Button>
        </>
      }
    >
      {hunks.length === 0 ? (
        <p className="t-body-md">{t("desktop.specs.no_changes")}</p>
      ) : (
        <div className="diff">
          {hunks.map((h, k) => (
            <div key={k} className="diff-file">
              <label className="diff-file-head">
                <input type="checkbox" checked={keep[k]} onChange={(e) => setKeep((v) => v.map((x, j) => (j === k ? e.target.checked : x)))} />
                <span className="spacer">{t("desktop.specs.line", { line: h.at + 1 })}</span>
              </label>
              <pre className="diff-lines">
                {h.removed.map((l, i) => (
                  <div key={`r${i}`} className="dl del">
                    <span className="dl-sign">-</span>
                    {l || " "}
                  </div>
                ))}
                {h.added.map((l, i) => (
                  <div key={`a${i}`} className="dl add">
                    <span className="dl-sign">+</span>
                    {l || " "}
                  </div>
                ))}
              </pre>
            </div>
          ))}
        </div>
      )}
    </Dialog>
  );
}
