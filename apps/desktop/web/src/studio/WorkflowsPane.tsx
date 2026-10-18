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

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Background, Controls, Handle, Position, ReactFlow, type Connection, type Edge, type EdgeChange, type Node, type NodeChange, type NodeProps, type ReactFlowInstance } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  mdiAlertCircleOutline,
  mdiArrowLeft,
  mdiCheckCircleOutline,
  mdiCloseCircleOutline,
  mdiCodeBraces,
  mdiContentSaveOutline,
  mdiAccountNetworkOutline,
  mdiApi,
  mdiCircleOutline,
  mdiPowerPlugOutline,
  mdiCloseThick,
  mdiDatabaseOutline,
  mdiPlusThick,
  mdiTableArrowRight,
  mdiSkipNextCircleOutline,
  mdiDeleteOutline,
  mdiEmailOutline,
  mdiFileDocumentOutline,
  mdiFileImportOutline,
  mdiFileExportOutline,
  mdiLoading,
  mdiPlay,
  mdiPlus,
  mdiRefresh,
  mdiRobotOutline,
  mdiSitemapOutline,
  mdiStop,
  mdiTextBoxEditOutline,
  mdiTrayFull,
  mdiUndoVariant,
  mdiWeb,
} from "@mdi/js";
import { sessions, workspaces } from "../api";
import { ApprovalCard } from "../Conversation";
import { message } from "../errors";
import { Decision, type ApprovalRequest } from "../gen/blitz/v1/turn_pb";
import type { SkillInfo, WorkflowFile } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";
import { SuggestInput, SuggestTextarea } from "../SecretSuggest";
import { Button, Chip, Dialog, Field, Icon, IconButton, Segmented, useSnackbar } from "../ui/controls";
import { addLink, agentTypes, comingSoon, emptyWorkflowForm, formFromWorkflow, fromTemplate, gatewayTypes, linkId, linkText, newStep, removeStep, sameWorkflowForm, setLink, specFromForm, methods, stepGroups, stepNeeds, templates, type AuthForm, type FieldForm, type PairForm, type LinkKind, type StepForm, type StepType, type Template, type WorkflowForm } from "./workflowForm";

/** Each step type's icon. */
const typeIcons: Record<StepType, string> = {
  agent: mdiRobotOutline,
  refine: mdiTextBoxEditOutline,
  summarize: mdiFileDocumentOutline,
  accumulate: mdiTrayFull,
  browser: mdiWeb,
  script: mdiCodeBraces,
  read_file: mdiFileImportOutline,
  save_file: mdiFileExportOutline,
  email: mdiEmailOutline,
  data: mdiDatabaseOutline,
  extract: mdiTableArrowRight,
  exclusive: mdiCloseThick,
  inclusive: mdiCircleOutline,
  parallel: mdiPlusThick,
  api_call: mdiApi,
  mcp_call: mdiPowerPlugOutline,
  a2a_call: mdiAccountNetworkOutline,
  workflow: mdiSitemapOutline,
};

/** A step's state in a run. */
type StepRun = { status: string; output: string; error: string };
/** A run from the canvas: its background run, its steps, what it waits on. */
type Live = { id: string; state: "running" | "done" | "failed" | "stopped"; steps: Record<string, StepRun>; approval: ApprovalRequest | null; error: string };

type StepData = { step: StepForm; run?: StepRun };

/** A step on the canvas: its icon, title, what it runs or needs, and its state in a run. */
function StepNode({ data, selected }: NodeProps<Node<StepData>>) {
  const s = data.step;
  const needs = stepNeeds(s);
  const status = data.run?.status;
  if (gatewayTypes.includes(s.type)) {
    // A gateway: a diamond, its kind's mark inside, its title under it.
    return (
      <div className={`flow-gateway ${selected ? "selected" : ""} ${status ? `is-${status}` : ""}`} title={t(`desktop.flows.type_hint.${s.type}`)}>
        <Handle type="target" position={Position.Left} />
        <span className="flow-diamond">
          <Icon path={typeIcons[s.type]} size="sm" />
        </span>
        <small className="flow-gateway-title ellipsis">{s.title || t(`desktop.flows.type.${s.type}`)}</small>
        <Handle type="source" position={Position.Right} />
      </div>
    );
  }
  const sub = needs
    ? t(`desktop.flows.needs.${needs}`)
    : agentTypes.includes(s.type)
      ? s.agent || t("desktop.flows.active_agent")
      : s.type === "api_call"
        ? `${s.method} ${hostOf(s.url)}`
        : s.type === "a2a_call"
          ? hostOf(s.url)
          : s.type === "mcp_call"
            ? `${s.server} › ${s.tool}`
            : s.type === "workflow"
              ? `/${s.workflow}`
            : s.path || (s.type === "script" ? `${s.skill} › ${s.script}` : t(`desktop.flows.type.${s.type}`));
  return (
    <div className={`flow-node ${selected ? "selected" : ""} ${status ? `is-${status}` : ""} ${comingSoon.includes(s.type) ? "soon" : ""}`}>
      {s.type !== "read_file" && <Handle type="target" position={Position.Left} />}
      <Icon path={typeIcons[s.type]} />
      <span className="lines spacer">
        <span className="ellipsis">
          {s.title || t(`desktop.flows.type.${s.type}`)}
          {s.forEach && <span className="flow-repeat" title={t("desktop.flows.repeats", { expr: s.forEach })}> ×N</span>}
        </span>
        <small className={`ellipsis ${needs && needs !== "coming_soon" ? "warn-text" : ""}`}>{sub}</small>
      </span>
      {status && <Icon path={status === "running" ? mdiLoading : status === "done" ? mdiCheckCircleOutline : status === "skipped" ? mdiSkipNextCircleOutline : mdiCloseCircleOutline} spin={status === "running"} className={status === "failed" ? "error-text" : "muted"} size="sm" />}
      {s.type !== "save_file" && <Handle type="source" position={Position.Right} />}
    </div>
  );
}
const nodeTypes = { step: StepNode };

/**
 * The workspace's workflows (Studio › Workflows, spec_workflows_043):
 * listed, and each opened on a canvas: its steps from the palette, joined
 * by dragging from one's right edge to another's left, the selected step's
 * settings beside it. Run runs it in the background as /name, each step
 * marked as it goes, approvals asked for on the canvas.
 */
export function WorkflowsPane({ workspace, onDirty }: { workspace: string; onDirty?: (dirty: boolean) => void }) {
  const snack = useSnackbar();
  const [list, setList] = useState<WorkflowFile[]>([]);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const [form, setForm] = useState<WorkflowForm>(emptyWorkflowForm);
  const [base, setBase] = useState<WorkflowForm>(emptyWorkflowForm);
  const [selected, setSelected] = useState("");
  const [selectedLink, setSelectedLink] = useState("");
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [choosing, setChoosing] = useState(false);
  const [asking, setAsking] = useState(false);
  const [input, setInput] = useState("");
  const [agents, setAgents] = useState<string[]>([]);
  const [skills, setSkills] = useState<SkillInfo[]>([]);
  const [rules, setRules] = useState<string[]>([]);
  const [mcpServers, setMcpServers] = useState<string[]>([]);
  const [live, setLive] = useState<Live | null>(null);
  const stop = useRef<AbortController | null>(null);

  const refresh = useCallback(async () => {
    try {
      setList((await workspaces.listWorkflows({ workspace })).workflows);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => {
    void refresh();
    workspaces.listAgents({ workspace }).then((r) => setAgents(r.agents.map((a) => a.name)), () => {});
    workspaces.listSkills({ workspace }).then((r) => setSkills(r.skills), () => {});
    workspaces.listRules({ workspace }).then((r) => setRules(r.rules.map((x) => x.name)), () => {});
    workspaces.listMCPServers({ workspace }).then((r) => setMcpServers(r.servers.map((x) => x.name)), () => {});
  }, [workspace, refresh]);
  useEffect(() => () => stop.current?.abort(), []);

  const file = open ? list.find((w) => w.name === open) : undefined;
  const isNew = open === "";
  const dirty = open !== null && !sameWorkflowForm(form, base);
  const running = live?.state === "running";
  useEffect(() => onDirty?.(dirty || running), [dirty, running, onDirty]);

  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());
  const show = (name: string, f: WorkflowForm, saved: boolean) =>
    leave(() => {
      setForm(f);
      setBase(saved ? f : emptyWorkflowForm);
      setOpen(name);
      setSelected("");
      setProblems([]);
      setLive(null);
    });
  const close = () => {
    if (running) return;
    leave(() => {
      setOpen(null);
      void refresh();
    });
  };
  const step = form.steps.find((s) => s.id === selected);
  const setStep = (id: string, patch: Partial<StepForm>) => setForm((f) => ({ ...f, steps: f.steps.map((s) => (s.id === id ? { ...s, ...patch } : s)) }));
  const add = (type: StepType) => {
    const right = form.steps.reduce((m, s) => Math.max(m, s.x), -280);
    const s = newStep(type, form.steps, right + 280, 80);
    setForm((f) => ({ ...f, steps: [...f.steps, s] }));
    setSelected(s.id);
  };

  // The canvas's nodes and links, from the form (its source of truth).
  const nodes: Node<StepData>[] = useMemo(() => form.steps.map((s) => ({ id: s.id, type: "step", position: { x: s.x, y: s.y }, data: { step: s, run: live?.steps[s.id] }, selected: s.id === selected })), [form.steps, live, selected]);
  const edges: Edge[] = useMemo(
    () =>
      form.links.map((l) => ({
        id: linkId(l),
        source: l.from,
        target: l.to,
        label: linkText(l, (k) => t(`desktop.flows.link_text.${k}`)) || undefined,
        className: `flow-link is-${l.kind}`,
        selected: linkId(l) === selectedLink,
        animated: live?.steps[l.from]?.status !== undefined && live?.steps[l.to]?.status === "running",
      })),
    [form.links, live, selectedLink],
  );
  const onNodesChange = (changes: NodeChange<Node<StepData>>[]) => {
    for (const c of changes) {
      if (c.type === "position" && c.position) setStep(c.id, { x: c.position.x, y: c.position.y });
      else if (c.type === "remove") setForm((f) => removeStep(f, c.id));
      else if (c.type === "select") {
        setSelected((cur) => (c.selected ? c.id : cur === c.id ? "" : cur));
        if (c.selected) setSelectedLink("");
      }
    }
  };
  const onEdgesChange = (changes: EdgeChange[]) => {
    for (const c of changes) {
      if (c.type === "remove") setForm((f) => ({ ...f, links: f.links.filter((l) => linkId(l) !== c.id) }));
      else if (c.type === "select") {
        setSelectedLink((cur) => (c.selected ? c.id : cur === c.id ? "" : cur));
        if (c.selected) setSelected("");
      }
    }
  };
  const link = form.links.find((l) => linkId(l) === selectedLink);

  const save = async () => {
    if (busy) return;
    setBusy(true);
    setProblems([]);
    try {
      const r = await workspaces.saveWorkflow({ workspace, previousName: file ? file.name : "", workflow: specFromForm(form) });
      if (r.problems.length || !r.workflow) return setProblems(r.problems);
      snack(t("desktop.flows.saved", { name: r.workflow.name }));
      const f = formFromWorkflow(r.workflow);
      setForm(f);
      setBase(f);
      setOpen(r.workflow.name);
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
      await workspaces.deleteWorkflow({ workspace, name: file?.name ?? "" });
      snack(t("desktop.flows.deleted", { name: file?.name ?? "" }));
      setOpen(null);
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  // Runs the workflow as saved, in the background, as /name input.
  const run = async (name: string) => {
    setAsking(false);
    const ctl = new AbortController();
    stop.current = ctl;
    try {
      const started = await sessions.startBackground({ workspace, turn: { text: `/${name} ${input}`.trim(), command: true } });
      const id = started.run?.id ?? "";
      setLive({ id, state: "running", steps: {}, approval: null, error: "" });
      for await (const res of sessions.watchBackground({ id, follow: true }, { signal: ctl.signal })) {
        const k = res.event?.kind;
        if (!k) continue;
        if (k.case === "flowStep") setLive((l) => l && { ...l, steps: { ...l.steps, [k.value.id]: { status: k.value.status, output: k.value.output, error: k.value.error } } });
        else if (k.case === "approvalRequest") setLive((l) => l && { ...l, approval: k.value });
        else if (k.case === "finished") {
          const err = k.value.error;
          setLive((l) => l && { ...l, state: err ? "failed" : "done", approval: null, error: err ? err.message || err.reason : "" });
          snack(err ? t("desktop.flows.run_failed", { name }) : t("desktop.flows.run_done", { name }), { error: !!err });
        }
      }
    } catch (e) {
      if (!ctl.signal.aborted) {
        setLive((l) => (l ? { ...l, state: "failed", error: message(e) } : null));
        snack(message(e), { error: true });
      }
    } finally {
      stop.current = null;
    }
  };
  const halt = async () => {
    if (!live) return;
    try {
      await sessions.stopBackground({ id: live.id });
    } catch {
      // It may have ended.
    }
    stop.current?.abort();
    setLive((l) => l && { ...l, state: "stopped", approval: null });
  };
  const decide = async (d: Decision) => {
    const req = live?.approval;
    if (!req) return;
    setLive((l) => l && { ...l, approval: null });
    try {
      await sessions.approve({ workspace, requestId: req.requestId, decision: d });
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const title = form.name || open || t("desktop.flows.new");
  const canRun = !!file && !dirty && !file.problem;
  const dark = document.documentElement.dataset.theme === "dark";
  const titleOf = (id: string) => form.steps.find((s) => s.id === id)?.title || id;
  const fromType = (id: string) => form.steps.find((s) => s.id === id)?.type;
  const scriptsOf = (skill: string) => skills.find((s) => s.name === skill)?.scripts.map((x) => x.name) ?? [];

  return (
    <div className={`slide-pane workflows-pane ${open !== null ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        <div className="row side-head">
          <span className="t-title-sm spacer">{t("desktop.flows.title")}</span>
          <IconButton icon={mdiPlus} label={t("desktop.flows.new")} small onClick={() => setChoosing(true)} />
          <IconButton icon={mdiRefresh} label={t("desktop.refresh")} small onClick={() => void refresh()} />
        </div>
        <p className="t-body-sm muted agent-dir">{t("desktop.workers.in", { path: ".agents/workflows" })}</p>
        <p className="t-body-sm muted">{t("desktop.flows.intro")}</p>
        {error && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
          </div>
        )}
        {list.length === 0 && !error && <p className="t-body-sm muted">{t("desktop.flows.none")}</p>}
        <div className="list">
          {list.map((w) => (
            <div key={w.path} className="list-item">
              <button type="button" className="row-open" onClick={() => show(w.name, formFromWorkflow(w), true)}>
                <Icon path={w.problem ? mdiAlertCircleOutline : mdiSitemapOutline} className={w.problem ? "error-text" : undefined} />
                <span className="lines spacer">
                  <span className="ellipsis">/{w.name}</span>
                  <small className={`ellipsis ${w.problem ? "error-text" : ""}`}>{w.problem || [tn("desktop.flows.steps", w.nodes.length), w.description].filter(Boolean).join(" · ")}</small>
                </span>
              </button>
              <span className="trailing">
                <IconButton
                  icon={mdiPlay}
                  label={t("desktop.flows.run_name", { name: w.name })}
                  small
                  disabled={!!w.problem}
                  onClick={() => {
                    show(w.name, formFromWorkflow(w), true);
                    setInput("");
                    setAsking(true);
                  }}
                />
              </span>
            </div>
          ))}
        </div>
      </aside>
      <section className="slide-detail flow-detail" inert={open === null}>
        {open !== null && (
          <>
            <div className="row slide-back">
              <IconButton icon={mdiArrowLeft} label={t("desktop.flows.back")} onClick={close} disabled={running} />
              <h2 className="t-headline spacer ellipsis">{title}</h2>
              {dirty && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
              {running ? (
                <Button icon={mdiStop} onClick={() => void halt()}>
                  {t("desktop.evals.stop")}
                </Button>
              ) : (
                <Button
                  variant="tonal"
                  icon={mdiPlay}
                  disabled={!canRun}
                  title={canRun ? undefined : t(dirty || isNew ? "desktop.flows.save_first" : "desktop.flows.cannot_run")}
                  onClick={() => {
                    setInput("");
                    setAsking(true);
                  }}
                >
                  {t("desktop.evals.run")}
                </Button>
              )}
            </div>
            <div className="flow-body">
              <div className="flow-main">
              <div className="flow-palette" role="toolbar" aria-label={t("desktop.flows.palette")}>
                {stepGroups.map(({ group, types }) => (
                  <div key={group} className="flow-palette-group" role="group" aria-label={t(`desktop.flows.group.${group}`)}>
                    <small className="muted flow-palette-label">{t(`desktop.flows.group.${group}`)}</small>
                    {types.map((type) => (
                      <button key={type} type="button" className="flow-palette-item" title={t(`desktop.flows.type_hint.${type}`)} disabled={comingSoon.includes(type)} onClick={() => add(type)}>
                        <Icon path={typeIcons[type]} size="sm" />
                        <span className="ellipsis">{t(`desktop.flows.type.${type}`)}</span>
                      </button>
                    ))}
                  </div>
                ))}
              </div>
              <div className="flow-canvas">
                <ReactFlow
                  key={open}
                  nodes={nodes}
                  edges={edges}
                  nodeTypes={nodeTypes}
                  onNodesChange={onNodesChange}
                  onEdgesChange={onEdgesChange}
                  onConnect={(c: Connection) => c.source && c.target && setForm((f) => addLink(f, c.source, c.target))}
                  onPaneClick={() => {
                    setSelected("");
                    setSelectedLink("");
                  }}
                  colorMode={dark ? "dark" : "light"}
                  deleteKeyCode={["Backspace", "Delete"]}
                  // Fitted once the pane has slid in and has its width.
                  onInit={(flow: ReactFlowInstance<Node<StepData>, Edge>) => setTimeout(() => void flow.fitView({ padding: 0.15, maxZoom: 1, minZoom: 0.7 }), 400)}
                  proOptions={{ hideAttribution: true }}
                >
                  <Background gap={20} />
                  <Controls showInteractive={false} />
                </ReactFlow>
              </div>
              </div>
              <div className="flow-side stack">
                {live && (
                  <div className={`card small-card stack ${live.state === "failed" ? "error" : ""}`} style={{ gap: 4 }}>
                    <span className="row t-label" style={{ gap: 6 }}>
                      <Icon path={live.state === "running" ? mdiLoading : live.state === "done" ? mdiCheckCircleOutline : mdiCloseCircleOutline} spin={live.state === "running"} size="sm" />
                      {t(`desktop.flows.run_state.${live.state}`, { done: Object.values(live.steps).filter((s) => s.status === "done").length, total: form.steps.length })}
                    </span>
                    {live.error && <span className="t-body-sm">{live.error}</span>}
                  </div>
                )}
                {live?.approval && <ApprovalCard req={live.approval} onDecide={(d) => void decide(d)} />}
                {step ? (
                  <div className="stack flow-step-form" style={{ gap: 10 }}>
                    <div className="row" style={{ gap: 6 }}>
                      <Icon path={typeIcons[step.type]} />
                      <span className="t-title-sm spacer">{t(`desktop.flows.type.${step.type}`)}</span>
                      <IconButton icon={mdiDeleteOutline} label={t("desktop.flows.remove_step")} small onClick={() => setForm((f) => removeStep(f, step.id))} />
                    </div>
                    <span className="t-body-sm muted">{t(`desktop.flows.type_hint.${step.type}`)}</span>
                    <Field label={t("desktop.flows.step_title")}>{(id) => <input id={id} className="input" value={step.title} placeholder={t(`desktop.flows.type.${step.type}`)} onChange={(e) => setStep(step.id, { title: e.target.value })} />}</Field>
                    {agentTypes.includes(step.type) && (
                      <>
                        <Field label={t("desktop.flows.agent")}>
                          {(id) => (
                            <select id={id} className="select" value={step.agent} onChange={(e) => setStep(step.id, { agent: e.target.value })}>
                              <option value="">{t("desktop.flows.active_agent")}</option>
                              {[...new Set([...agents, step.agent].filter(Boolean))].map((a) => (
                                <option key={a} value={a}>
                                  {a}
                                </option>
                              ))}
                            </select>
                          )}
                        </Field>
                        <Field label={t(`desktop.flows.instructions.${step.type}`)} supporting={t("desktop.flows.template_hint")}>
                          {(id) => <textarea id={id} className="input" rows={5} value={step.instructions} placeholder={t(`desktop.flows.instructions_placeholder.${step.type}`)} onChange={(e) => setStep(step.id, { instructions: e.target.value })} />}
                        </Field>
                        <Pick label={t("desktop.flows.skills")} hint={t("desktop.flows.skills_hint")} all={skills.map((s) => s.name)} chosen={step.skills} onChange={(v) => setStep(step.id, { skills: v })} />
                        <Pick label={t("desktop.flows.rules")} hint={t("desktop.flows.rules_hint")} all={rules} chosen={step.rules} onChange={(v) => setStep(step.id, { rules: v })} />
                      </>
                    )}
                    {step.type === "script" && (
                      <>
                        <Field label={t("desktop.flows.skill")}>
                          {(id) => (
                            <select id={id} className="select" value={step.skill} onChange={(e) => setStep(step.id, { skill: e.target.value, script: "" })}>
                              <option value="">{t("desktop.flows.choose")}</option>
                              {skills
                                .filter((s) => s.scripts.length > 0)
                                .map((s) => (
                                  <option key={s.name} value={s.name}>
                                    {s.name}
                                  </option>
                                ))}
                            </select>
                          )}
                        </Field>
                        <Field label={t("desktop.flows.script")}>
                          {(id) => (
                            <select id={id} className="select" value={step.script} disabled={!step.skill} onChange={(e) => setStep(step.id, { script: e.target.value })}>
                              <option value="">{t("desktop.flows.choose")}</option>
                              {[...new Set([...scriptsOf(step.skill), step.script].filter(Boolean))].map((s) => (
                                <option key={s} value={s}>
                                  {s}
                                </option>
                              ))}
                            </select>
                          )}
                        </Field>
                        <Field label={t("desktop.flows.args")} supporting={t("desktop.flows.args_hint")}>
                          {(id) => <input id={id} className="input mono" value={step.args} onChange={(e) => setStep(step.id, { args: e.target.value })} />}
                        </Field>
                      </>
                    )}
                    {(step.type === "read_file" || step.type === "save_file") && (
                      <Field label={t("desktop.flows.path")} supporting={t(`desktop.flows.path_hint.${step.type}`)}>
                        {(id) => <input id={id} className="input mono" value={step.path} placeholder={t("desktop.flows.path_placeholder")} onChange={(e) => setStep(step.id, { path: e.target.value })} />}
                      </Field>
                    )}
                    {step.type === "api_call" && (
                      <>
                        <div className="row flow-method" style={{ gap: 6 }}>
                          <select className="select" aria-label={t("desktop.flows.method")} value={step.method} onChange={(e) => setStep(step.id, { method: e.target.value })}>
                            {methods.map((m) => (
                              <option key={m} value={m}>
                                {m}
                              </option>
                            ))}
                          </select>
                          <SuggestInput className="input mono" aria-label={t("desktop.flows.url")} placeholder={t("desktop.flows.url_placeholder")} value={step.url} onValue={(v) => setStep(step.id, { url: v })} workspace={workspace} />
                        </div>
                        <span className="t-body-sm muted">{t("desktop.flows.call_hint")}</span>
                        <Pairs label={t("desktop.flows.query")} pairs={step.query} workspace={workspace} onChange={(v) => setStep(step.id, { query: v })} />
                        <Pairs label={t("desktop.flows.headers")} pairs={step.headers} workspace={workspace} onChange={(v) => setStep(step.id, { headers: v })} />
                        {step.method !== "GET" && step.method !== "HEAD" && (
                          <div className="stack" style={{ gap: 4 }}>
                            <span className="t-label">{t("desktop.flows.body")}</span>
                            <Segmented
                              label={t("desktop.flows.body")}
                              value={step.bodyKind}
                              options={(["none", "text", "json"] as const).map((k) => ({ value: k, label: t(`desktop.flows.body_kinds.${k}`) }))}
                              onChange={(v) => setStep(step.id, { bodyKind: v as StepForm["bodyKind"] })}
                            />
                            {step.bodyKind === "none" ? (
                              <span className="t-body-sm muted">{t("desktop.flows.body_none")}</span>
                            ) : (
                              <SuggestTextarea className="input mono" rows={5} aria-label={t("desktop.flows.body")} value={step.body} placeholder={step.bodyKind === "json" ? t("desktop.flows.json_placeholder") : ""} onValue={(v) => setStep(step.id, { body: v })} workspace={workspace} />
                            )}
                          </div>
                        )}
                        <AuthFields auth={step.auth} workspace={workspace} onChange={(a) => setStep(step.id, { auth: a })} />
                      </>
                    )}
                    {step.type === "mcp_call" && (
                      <>
                        <Field label={t("desktop.flows.mcp_server")}>
                          {(id) => (
                            <select id={id} className="select" value={step.server} onChange={(e) => setStep(step.id, { server: e.target.value })}>
                              <option value="">{t("desktop.flows.choose")}</option>
                              {[...new Set([...mcpServers, step.server].filter(Boolean))].map((s) => (
                                <option key={s} value={s}>
                                  {s}
                                </option>
                              ))}
                            </select>
                          )}
                        </Field>
                        <Field label={t("desktop.flows.mcp_tool")} supporting={t("desktop.flows.mcp_tool_hint")}>
                          {(id) => <input id={id} className="input mono" value={step.tool} onChange={(e) => setStep(step.id, { tool: e.target.value })} />}
                        </Field>
                        <Field label={t("desktop.flows.arguments")} supporting={t("desktop.flows.arguments_hint")}>
                          {(id) => <SuggestTextarea id={id} className="input mono" rows={5} value={step.argumentsJson} placeholder={t("desktop.flows.json_placeholder")} onValue={(v) => setStep(step.id, { argumentsJson: v })} workspace={workspace} />}
                        </Field>
                      </>
                    )}
                    {step.type === "a2a_call" && (
                      <>
                        <Field label={t("desktop.flows.agent_url")} supporting={t("desktop.flows.agent_url_hint")}>
                          {(id) => <SuggestInput id={id} className="input mono" value={step.url} placeholder={t("desktop.flows.agent_url_placeholder")} onValue={(v) => setStep(step.id, { url: v })} workspace={workspace} />}
                        </Field>
                        <Field label={t("desktop.flows.message")} supporting={t("desktop.flows.message_hint")}>
                          {(id) => <SuggestTextarea id={id} className="input" rows={4} value={step.instructions} onValue={(v) => setStep(step.id, { instructions: v })} workspace={workspace} />}
                        </Field>
                        <Pairs label={t("desktop.flows.headers")} pairs={step.headers} workspace={workspace} onChange={(v) => setStep(step.id, { headers: v })} />
                      </>
                    )}
                    {step.type === "workflow" && (
                      <Field label={t("desktop.flows.call_workflow")} supporting={t("desktop.flows.call_workflow_hint")}>
                        {(id) => (
                          <select id={id} className="select" value={step.workflow} onChange={(e) => setStep(step.id, { workflow: e.target.value })}>
                            <option value="">{t("desktop.flows.choose")}</option>
                            {[...new Set([...list.map((w) => w.name).filter((n) => n !== form.name), step.workflow].filter(Boolean))].map((n) => (
                              <option key={n} value={n}>
                                /{n}
                              </option>
                            ))}
                          </select>
                        )}
                      </Field>
                    )}
                    {step.type === "extract" && (
                      <div className="stack" style={{ gap: 6 }}>
                        <span className="t-label">{t("desktop.flows.fields")}</span>
                        <span className="t-body-sm muted">{t("desktop.flows.fields_hint", { id: step.id })}</span>
                        {step.fields.map((fl, i) => (
                          <div key={i} className="row flow-field" style={{ gap: 6 }}>
                            <input className="input mono" aria-label={t("desktop.flows.field_name")} placeholder={t("desktop.flows.field_name")} value={fl.name} onChange={(e) => setStep(step.id, { fields: step.fields.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)) })} />
                            <select className="select" aria-label={t("desktop.flows.field_type")} value={fl.type} onChange={(e) => setStep(step.id, { fields: step.fields.map((x, j) => (j === i ? { ...x, type: e.target.value as FieldForm["type"] } : x)) })}>
                              {(["string", "number", "boolean", "list"] as const).map((ty) => (
                                <option key={ty} value={ty}>
                                  {t(`desktop.flows.field_types.${ty}`)}
                                </option>
                              ))}
                            </select>
                            <IconButton icon={mdiDeleteOutline} label={t("desktop.flows.field_remove")} small onClick={() => setStep(step.id, { fields: step.fields.filter((_, j) => j !== i) })} />
                          </div>
                        ))}
                        <div className="row">
                          <Button small icon={mdiPlus} onClick={() => setStep(step.id, { fields: [...step.fields, { name: "", type: "string", description: "" }] })}>
                            {t("desktop.flows.field_add")}
                          </Button>
                        </div>
                      </div>
                    )}
                    {!gatewayTypes.includes(step.type) && (
                      <div className="stack" style={{ gap: 6 }}>
                        <span className="t-label">{t("desktop.flows.repeat")}</span>
                        <span className="t-body-sm muted">{t("desktop.flows.repeat_hint")}</span>
                        <div className="row flow-field" style={{ gap: 6 }}>
                          <input className="input mono" aria-label={t("desktop.flows.for_each")} placeholder={t("desktop.flows.for_each_placeholder")} value={step.forEach} onChange={(e) => setStep(step.id, { forEach: e.target.value })} />
                          <input className="input flow-concurrency" type="number" min={1} max={32} aria-label={t("desktop.flows.concurrency")} title={t("desktop.flows.concurrency")} placeholder="4" disabled={!step.forEach.trim()} value={step.concurrency} onChange={(e) => setStep(step.id, { concurrency: e.target.value })} />
                        </div>
                      </div>
                    )}
                    {!gatewayTypes.includes(step.type) && (
                      <div className="stack" style={{ gap: 6 }}>
                        <span className="t-label">{t("desktop.flows.if_it_fails")}</span>
                        <span className="t-body-sm muted">{t("desktop.flows.if_it_fails_hint")}</span>
                        <div className="flow-retry">
                          <Field label={t("desktop.flows.retry_attempts")}>{(id) => <input id={id} className="input" type="number" min={1} max={10} value={step.retryAttempts} placeholder="1" onChange={(e) => setStep(step.id, { retryAttempts: e.target.value })} />}</Field>
                          <Field label={t("desktop.flows.retry_delay")}>{(id) => <input id={id} className="input mono" value={step.retryDelay} placeholder="1s" onChange={(e) => setStep(step.id, { retryDelay: e.target.value })} />}</Field>
                          <Field label={t("desktop.flows.timeout")}>{(id) => <input id={id} className="input mono" value={step.timeout} placeholder="5m" onChange={(e) => setStep(step.id, { timeout: e.target.value })} />}</Field>
                        </div>
                      </div>
                    )}
                    {live?.steps[step.id] && (live.steps[step.id].output || live.steps[step.id].error) && (
                      <div className="stack" style={{ gap: 2 }}>
                        <span className="t-label">{t(live.steps[step.id].error ? "desktop.flows.step_error" : "desktop.flows.step_output")}</span>
                        <pre className="eval-text">{live.steps[step.id].error || live.steps[step.id].output}</pre>
                      </div>
                    )}
                  </div>
                ) : link ? (
                  <div className="stack flow-link-form" style={{ gap: 10 }}>
                    <div className="row" style={{ gap: 6 }}>
                      <span className="t-title-sm spacer">{t("desktop.flows.link_title", { from: titleOf(link.from), to: titleOf(link.to) })}</span>
                      <IconButton icon={mdiDeleteOutline} label={t("desktop.flows.remove_link")} small onClick={() => setForm((f) => ({ ...f, links: f.links.filter((l) => linkId(l) !== selectedLink) }))} />
                    </div>
                    <Field label={t("desktop.flows.link_kind")}>
                      {(id) => (
                        <select id={id} className="select" value={link.kind} onChange={(e) => setForm((f) => setLink(f, selectedLink, { kind: e.target.value as LinkKind }))}>
                          {(["always", "when", "if", "default", "error"] as const).map((k) => (
                            <option key={k} value={k} disabled={k !== "always" && k !== "error" && fromType(link.from) === "parallel"}>
                              {t(`desktop.flows.link_kinds.${k}`)}
                            </option>
                          ))}
                        </select>
                      )}
                    </Field>
                    <span className="t-body-sm muted">{t(`desktop.flows.link_hints.${link.kind}`)}</span>
                    {(link.kind === "when" || link.kind === "if") && (
                      <Field label={t(`desktop.flows.condition.${link.kind}`)} supporting={t(`desktop.flows.condition_hint.${link.kind}`)}>
                        {(id) => (
                          <textarea
                            id={id}
                            className={`input ${link.kind === "when" ? "mono" : ""}`}
                            rows={3}
                            value={link.condition}
                            placeholder={t(`desktop.flows.condition_placeholder.${link.kind}`)}
                            onChange={(e) => setForm((f) => setLink(f, selectedLink, { condition: e.target.value }))}
                          />
                        )}
                      </Field>
                    )}
                    <Field label={t("desktop.flows.link_label")} supporting={t("desktop.flows.link_label_hint")}>
                      {(id) => <input id={id} className="input" value={link.label} onChange={(e) => setForm((f) => setLink(f, selectedLink, { label: e.target.value }))} />}
                    </Field>
                  </div>
                ) : (
                  <div className="stack" style={{ gap: 10 }}>
                    <span className="t-title-sm">{t("desktop.flows.settings")}</span>
                    <Field label={t("desktop.flows.name")} supporting={t("desktop.flows.name_hint", { name: form.name || "name" })}>
                      {(id) => <input id={id} className="input mono" value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />}
                    </Field>
                    <Field label={t("desktop.rules.description")}>{(id) => <input id={id} className="input" value={form.description} onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))} />}</Field>
                    <span className="t-body-sm muted">{t("desktop.flows.canvas_hint")}</span>
                  </div>
                )}
              </div>
            </div>
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
              <div className="card warn row small-card">
                <Icon path={mdiAlertCircleOutline} size="sm" /> {file.problem}
              </div>
            )}
            <div className="row wrap agent-actions slide-actions">
              <span className="t-body-sm muted spacer ellipsis" title={file?.path}>
                {t("desktop.agents.where", { path: file?.path || `.agents/workflows/${form.name.trim() || "…"}.flow.yaml` })}
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
      {choosing &&
        createPortal(
          <Dialog title={t("desktop.flows.new")} icon={mdiSitemapOutline} onClose={() => setChoosing(false)} footer={<Button onClick={() => setChoosing(false)}>{t("desktop.cancel")}</Button>}>
            <div className="list">
              {templates.map((tpl: Template) => (
                <button
                  key={tpl}
                  type="button"
                  className="list-item"
                  onClick={() => {
                    setChoosing(false);
                    const f = fromTemplate(tpl, (k) => t(`desktop.flows.template.${k}`));
                    const taken = list.map((w) => w.name);
                    let name = f.name;
                    for (let n = 2; name && taken.includes(name); n++) name = `${f.name}-${n}`;
                    show("", { ...f, name }, false);
                  }}
                >
                  <Icon path={mdiSitemapOutline} />
                  <span className="lines">
                    <span className="ellipsis">{t(`desktop.flows.template.${tpl}.title`)}</span>
                    <small className="ellipsis">{t(`desktop.flows.template.${tpl}.about`)}</small>
                  </span>
                </button>
              ))}
            </div>
          </Dialog>,
          document.body,
        )}
      {asking &&
        createPortal(
          <Dialog
            title={t("desktop.flows.run_title", { name: form.name })}
            icon={mdiPlay}
            onClose={() => setAsking(false)}
            footer={
              <>
                <Button onClick={() => setAsking(false)}>{t("desktop.cancel")}</Button>
                <Button variant="filled" icon={mdiPlay} onClick={() => void run(form.name)}>
                  {t("desktop.evals.run")}
                </Button>
              </>
            }
          >
            <div className="stack" style={{ gap: 8 }}>
              <p className="t-body-sm muted">{t("desktop.flows.run_hint", { name: form.name })}</p>
              <Field label={t("desktop.flows.input")}>{(id) => <textarea id={id} className="input" rows={3} autoFocus value={input} placeholder={t("desktop.flows.input_placeholder")} onChange={(e) => setInput(e.target.value)} />}</Field>
            </div>
          </Dialog>,
          document.body,
        )}
      {deleting &&
        createPortal(
          <Dialog
            title={t("desktop.flows.delete_title", { name: title })}
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
            <p>{t("desktop.agents.discard_body", { name: title })}</p>
          </Dialog>,
          document.body,
        )}
    </div>
  );
}

/** Chips to choose some of all (those chosen that no longer exist kept). */
function Pick({ label, hint, all, chosen, onChange }: { label: string; hint: string; all: string[]; chosen: string[]; onChange: (v: string[]) => void }) {
  return (
    <div className="stack" style={{ gap: 4 }}>
      <span className="t-label">{label}</span>
      <span className="t-body-sm muted">{hint}</span>
      <div className="row wrap" style={{ gap: 6 }}>
        {[...new Set([...all, ...chosen])].map((x) => {
          const on = chosen.includes(x);
          return (
            <Chip key={x} selected={on} aria-pressed={on} onClick={() => onChange(on ? chosen.filter((y) => y !== x) : [...chosen, x])}>
              {x}
            </Chip>
          );
        })}
        {all.length === 0 && chosen.length === 0 && <span className="t-body-sm muted">—</span>}
      </div>
    </div>
  );
}

/** A URL's host, for a step's card ("" when it isn't one yet). */
function hostOf(raw: string): string {
  try {
    return new URL(raw.replace(/\{\{.*?\}\}/g, "x")).host;
  } catch {
    return raw;
  }
}

/** Names and values (headers, query parameters), each completing {secrets:…}. */
function Pairs({ label, pairs, workspace, onChange }: { label: string; pairs: PairForm[]; workspace: string; onChange: (v: PairForm[]) => void }) {
  return (
    <div className="stack" style={{ gap: 4 }}>
      <span className="t-label">{label}</span>
      {pairs.map((p, i) => (
        <div key={i} className="row flow-field" style={{ gap: 6 }}>
          <input className="input mono" aria-label={t("desktop.flows.pair_name")} placeholder={t("desktop.flows.pair_name")} value={p.name} onChange={(e) => onChange(pairs.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))} />
          <SuggestInput className="input mono" aria-label={t("desktop.flows.pair_value")} placeholder={t("desktop.flows.pair_value")} value={p.value} onValue={(v) => onChange(pairs.map((x, j) => (j === i ? { ...x, value: v } : x)))} workspace={workspace} />
          <IconButton icon={mdiDeleteOutline} label={t("desktop.flows.pair_remove")} small onClick={() => onChange(pairs.filter((_, j) => j !== i))} />
        </div>
      ))}
      <div className="row">
        <Button small icon={mdiPlus} onClick={() => onChange([...pairs, { name: "", value: "" }])}>
          {t("desktop.flows.pair_add")}
        </Button>
      </div>
    </div>
  );
}

/** An API call's auth: its type, then that type's values (secrets by name). */
function AuthFields({ auth, workspace, onChange }: { auth: AuthForm; workspace: string; onChange: (a: AuthForm) => void }) {
  const set = (patch: Partial<AuthForm>) => onChange({ ...auth, ...patch });
  const secret = (key: keyof AuthForm, label: string) => (
    <Field label={label}>{(id) => <SuggestInput id={id} className="input mono" value={String(auth[key])} placeholder={t("desktop.flows.secret_placeholder")} onValue={(v) => set({ [key]: v } as Partial<AuthForm>)} workspace={workspace} />}</Field>
  );
  return (
    <div className="stack" style={{ gap: 6 }}>
      <Field label={t("desktop.flows.auth")} supporting={t("desktop.flows.auth_hint")}>
        {(id) => (
          <select id={id} className="select" value={auth.type} onChange={(e) => set({ type: e.target.value as AuthForm["type"] })}>
            {(["", "bearer", "basic", "api_key", "oauth2"] as const).map((k) => (
              <option key={k} value={k}>
                {t(`desktop.flows.auth_types.${k || "none"}`)}
              </option>
            ))}
          </select>
        )}
      </Field>
      {auth.type === "bearer" && secret("token", t("desktop.flows.auth_token"))}
      {auth.type === "basic" && (
        <>
          {secret("username", t("desktop.flows.auth_username"))}
          {secret("password", t("desktop.flows.auth_password"))}
        </>
      )}
      {auth.type === "api_key" && (
        <>
          <div className="row" style={{ gap: 6 }}>
            <Field label={t("desktop.flows.auth_key_name")}>{(id) => <input id={id} className="input mono" value={auth.name} placeholder={t("desktop.flows.auth_key_name_placeholder")} onChange={(e) => set({ name: e.target.value })} />}</Field>
            <Field label={t("desktop.flows.auth_key_in")}>
              {(id) => (
                <select id={id} className="select" value={auth.in} onChange={(e) => set({ in: e.target.value as AuthForm["in"] })}>
                  <option value="header">{t("desktop.flows.auth_in.header")}</option>
                  <option value="query">{t("desktop.flows.auth_in.query")}</option>
                </select>
              )}
            </Field>
          </div>
          {secret("value", t("desktop.flows.auth_key_value"))}
        </>
      )}
      {auth.type === "oauth2" && (
        <>
          <Field label={t("desktop.flows.auth_token_url")}>{(id) => <SuggestInput id={id} className="input mono" value={auth.tokenUrl} placeholder={t("desktop.flows.auth_token_url_placeholder")} onValue={(v) => set({ tokenUrl: v })} workspace={workspace} />}</Field>
          {secret("clientId", t("desktop.flows.auth_client_id"))}
          {secret("clientSecret", t("desktop.flows.auth_client_secret"))}
          <Field label={t("desktop.flows.auth_scopes")} supporting={t("desktop.flows.auth_scopes_hint")}>{(id) => <input id={id} className="input mono" value={auth.scopes} onChange={(e) => set({ scopes: e.target.value })} />}</Field>
          <Field label={t("desktop.flows.auth_audience")}>{(id) => <input id={id} className="input mono" value={auth.audience} onChange={(e) => set({ audience: e.target.value })} />}</Field>
        </>
      )}
    </div>
  );
}
