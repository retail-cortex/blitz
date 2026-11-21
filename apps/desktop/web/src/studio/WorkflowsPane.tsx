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
import { Background, Controls, ReactFlow, type Connection, type Edge, type EdgeChange, type Node, type NodeChange, type ReactFlowInstance } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  mdiAlertCircleOutline,
  mdiArrowLeft,
  mdiContentSaveOutline,
  mdiDeleteOutline,
  mdiHistory,
  mdiPlay,
  mdiPlus,
  mdiSitemapOutline,
  mdiStop,
  mdiUndoVariant,
} from "@mdi/js";
import { Button, Icon, IconButton, Menu, useSnackbar } from "@blitz/webui";
import { starterForm } from "../a2ui/model";
import { TaskCard } from "../a2ui/TaskCard";
import { workspaces } from "../api";
import { ApprovalCard } from "../Conversation";
import { message } from "../errors";
import type { SkillInfo, WorkflowFile } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";
import type { ItemIntent } from "../intent";
import { useWorkspaceStatus } from "../status";
import { cost, goingRun, latestSteps, ms, progress, took } from "../workflowRuns";
import { ComposeDialog } from "./ComposeDialog";
import { GradeBadge } from "./GradeBadge";
import { ConfirmDialog, useDiscardGuard } from "./paneParts";
import { dated, RunPanel, statusIcon } from "./RunPanel";
import { useWorkflowRunState } from "./useWorkflowRunState";
import { nodeTypes, sideDefault, sideKey, stepDrag, StepToolbox, store, stored, type StepData } from "./WorkflowCanvasParts";
import { NewWorkflowDialog, RunWorkflowDialog } from "./WorkflowDialogs";
import { WorkflowSidePanel } from "./WorkflowSidePanel";
import { addLink, emptyWorkflowForm, formFromWorkflow, linkId, linkText, newStep, removeStep, sameWorkflowForm, specFromForm, unknownSteps, untakenName, type StepForm, type StepType, type WorkflowForm } from "./workflowForm";

/**
 * The workspace's workflows (Studio › Workflows, spec_043_workflows):
 * listed, and each opened on a canvas: its steps from the palette, joined
 * by dragging from one's right edge to another's left, the selected step's
 * settings beside it. Run runs it in the background as /name; the run
 * goes on with Studio closed, and its receipt (each step's state, times,
 * tokens and cost, its events and what its steps streamed) shows under the
 * canvas, whether it was started here, in a chat or by a worker.
 */
export function WorkflowsPane({
  workspace,
  onDirty,
  onCanvas,
  intent,
  run: runToShow,
}: {
  workspace: string;
  onDirty?: (dirty: boolean) => void;
  /** Called with whether a workflow's canvas is open (it wants the room). */
  onCanvas?: (open: boolean) => void;
  /** A workflow to open (intent "show", its name the ID). */
  intent?: ItemIntent;
  /** With intent: the run to show on its canvas. */
  run?: string;
}) {
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
  const [deleting, setDeleting] = useState(false);
  const [choosing, setChoosing] = useState(false);
  // The composer making a workflow from a conversation (spec_045_composer).
  const [composing, setComposing] = useState(false);
  // The mode a new workflow runs in, chosen as it's made (WF-21).
  const [newMode, setNewMode] = useState("auto");
  const workspaceMode = useWorkspaceStatus(workspace).settings?.permissionMode ?? "auto";
  const [agents, setAgents] = useState<string[]>([]);
  const [skills, setSkills] = useState<SkillInfo[]>([]);
  const [rules, setRules] = useState<string[]>([]);
  const [mcpServers, setMcpServers] = useState<string[]>([]);
  // The settings panel (folded to give the canvas the room, its width the
  // user's) and the toolbox.
  const [sideOpen, setSideOpen] = useState(true);
  const [sideWidth, setSideWidth] = useState(() => Number(stored(sideKey)) || sideDefault);
  const [toolbox, setToolbox] = useState(() => stored("blitz.flows.toolbox") === "open");

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
    // The lists the step settings offer: a failure is said, not taken for none.
    const fail = (e: unknown) => snack(message(e), { error: true });
    workspaces.listAgents({ workspace }).then((r) => setAgents(r.agents.map((a) => a.name)), fail);
    workspaces.listSkills({ workspace }).then((r) => setSkills(r.skills), fail);
    workspaces.listRules({ workspace }).then((r) => setRules(r.rules.map((x) => x.name)), fail);
    workspaces.listMCPServers({ workspace }).then((r) => setMcpServers(r.servers.map((x) => x.name)), fail);
  }, [workspace, refresh, snack]);

  const file = open ? list.find((w) => w.name === open) : undefined;
  const isNew = open === "";
  const dirty = open !== null && !sameWorkflowForm(form, base);
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);
  useEffect(() => onCanvas?.(open !== null), [open, onCanvas]);
  useEffect(() => () => onCanvas?.(false), [onCanvas]);

  const flowRef = useRef<ReactFlowInstance<Node<StepData>, Edge> | null>(null);
  const canvasRef = useRef<HTMLDivElement>(null);

  // Discarding puts the canvas back to the workflow as saved before going on.
  const { leave, discard } = useDiscardGuard(dirty, () => setForm(base));
  const showRef = useRef<(name: string, f: WorkflowForm, saved: boolean) => void>(() => {});
  const runState = useWorkflowRunState({
    workspace,
    open,
    list,
    intent,
    runToShow,
    flowRef,
    show: (name, f, saved) => showRef.current(name, f, saved),
    snack,
  });
  const show = (name: string, f: WorkflowForm, saved: boolean) =>
    leave(() => {
      setForm(f);
      setBase(saved ? f : emptyWorkflowForm);
      setOpen(name);
      setSelected("");
      setProblems([]);
      runState.setShownRun("");
    });
  showRef.current = show;
  const close = () => {
    leave(() => {
      setOpen(null);
      void refresh();
    });
  };
  const step = form.steps.find((s) => s.id === selected);
  const setStep = (id: string, patch: Partial<StepForm>) => setForm((f) => ({ ...f, steps: f.steps.map((s) => (s.id === id ? { ...s, ...patch } : s)) }));
  const add = (type: StepType, dropped?: { x: number; y: number }) => {
    // Where it's dropped, else where it's seen: the middle of the canvas
    // as it's shown (moved on while a step is there already), else right
    // of the last step.
    const right = form.steps.reduce((m, s) => Math.max(m, s.x), -280);
    let at = { x: right + 280, y: 80 };
    const box = canvasRef.current?.getBoundingClientRect();
    if (flowRef.current && dropped) {
      const p = flowRef.current.screenToFlowPosition(dropped);
      at = { x: Math.round(p.x - 90), y: Math.round(p.y - 24) };
    } else if (flowRef.current && box) {
      const mid = flowRef.current.screenToFlowPosition({ x: box.left + box.width / 2, y: box.top + box.height / 2 });
      at = { x: Math.round(mid.x - 90), y: Math.round(mid.y - 24) };
      // Clear of the steps there (a step is about 180 by 50): below them.
      for (let i = 0; i < 40 && form.steps.some((o) => Math.abs(o.x - at.x) < 200 && Math.abs(o.y - at.y) < 70); i++) at = { x: at.x, y: at.y + 70 };
    }
    const s = newStep(type, form.steps, at.x, at.y);
    if (type === "human_task") s.formJson = JSON.stringify(starterForm(), null, 2);
    setForm((f) => ({ ...f, steps: [...f.steps, s] }));
    setSelected(s.id);
    setSideOpen(true);
  };
  // Opening the toolbox moves the steps under it into view, right of it.
  const clearToolbox = () => {
    const flow = flowRef.current;
    const box = canvasRef.current?.getBoundingClientRect();
    if (!flow || !box || !form.steps.length) return;
    const left = Math.min(...form.steps.map((s) => flow.flowToScreenPosition({ x: s.x, y: s.y }).x));
    const need = box.left + 230 - left;
    if (need > 0) {
      const vp = flow.getViewport();
      void flow.setViewport({ ...vp, x: vp.x + need }, { duration: 200 });
    }
  };
  const drop = (e: React.DragEvent) => {
    const type = e.dataTransfer.getData(stepDrag) as StepType;
    if (!type) return;
    e.preventDefault();
    add(type, { x: e.clientX, y: e.clientY });
  };

  // The canvas's nodes and links, from the form (its source of truth).
  const stepRuns = useMemo(() => (runState.receipt ? latestSteps(runState.receipt.steps) : {}), [runState.receipt]);
  const nodes: Node<StepData>[] = useMemo(() => form.steps.map((s) => ({ id: s.id, type: "step", position: { x: s.x, y: s.y }, data: { step: s, run: stepRuns[s.id] }, selected: s.id === selected })), [form.steps, stepRuns, selected]);
  const edges: Edge[] = useMemo(
    () =>
      form.links.map((l) => ({
        id: linkId(l),
        source: l.from,
        target: l.to,
        label: linkText(l, (k) => t(`desktop.flows.link_text.${k}`)) || undefined,
        className: `flow-link is-${l.kind}`,
        selected: linkId(l) === selectedLink,
        animated: stepRuns[l.from] !== undefined && stepRuns[l.to]?.status === "running",
      })),
    [form.links, stepRuns, selectedLink],
  );
  const onNodesChange = (changes: NodeChange<Node<StepData>>[]) => {
    for (const c of changes) {
      if (c.type === "position" && c.position) setStep(c.id, { x: c.position.x, y: c.position.y });
      else if (c.type === "remove") setForm((f) => removeStep(f, c.id));
      else if (c.type === "select") {
        setSelected((cur) => (c.selected ? c.id : cur === c.id ? "" : cur));
        if (c.selected) {
          setSelectedLink("");
          setSideOpen(true);
        }
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

  // A workflow with steps the canvas doesn't know isn't saved from here.
  const unknown = file ? unknownSteps(file) : [];
  const save = async () => {
    if (busy || unknown.length) return;
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

  const title = form.name || open || t("desktop.flows.new");
  const canRun = !!file && !dirty && !file.problem;
  const dark = document.documentElement.dataset.theme === "dark";

  return (
    <div className={`slide-pane workflows-pane ${open !== null ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        <div className="row side-head">
          <span className="t-title-sm spacer">{t("desktop.flows.title")}</span>
          <IconButton icon={mdiPlus} label={t("desktop.flows.new")} small onClick={() => setChoosing(true)} />
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
          {list.map((w) => {
            const live = goingRun(runState.runs, workspace, w.name);
            const icon = live ? statusIcon(live.status) : null;
            return (
              <div key={w.path} className="list-item">
                <button type="button" className="row-open" onClick={() => show(w.name, formFromWorkflow(w), true)}>
                  {icon ? <Icon path={icon.path} spin={icon.spin} className={icon.className} /> : <Icon path={w.problem ? mdiAlertCircleOutline : mdiSitemapOutline} className={w.problem ? "error-text" : undefined} />}
                  <span className="lines spacer">
                    <span className="ellipsis">
                      /{w.name} <GradeBadge kind="workflow" name={w.name} />
                    </span>
                    <small className={`ellipsis ${w.problem ? "error-text" : ""}`}>
                      {live
                        ? [t(`desktop.flows.run_status.${live.status}`), t("desktop.flow_runs.steps", progress(live)), cost(live.usage?.costUsd, live.usage?.priced)].join(" · ")
                        : w.problem || [tn("desktop.flows.steps", w.nodes.length), w.description].filter(Boolean).join(" · ")}
                    </small>
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
                      runState.setInput("");
                      runState.setAsking(true);
                    }}
                  />
                </span>
              </div>
            );
          })}
        </div>
      </aside>
      <section className="slide-detail flow-detail" inert={open === null}>
        {open !== null && (
          <>
            <div className="row slide-back">
              <IconButton icon={mdiArrowLeft} label={t("desktop.flows.back")} onClick={close} />
              <h2 className="t-headline spacer ellipsis">{title}</h2>
              {dirty && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
              {file && (
                <Menu
                  placement="down end"
                  className="flow-history-menu"
                  trigger={(p) => <IconButton icon={mdiHistory} label={t("desktop.flows.history")} {...p} />}
                  items={
                    runState.history.length
                      ? [
                          { heading: t("desktop.flows.history") },
                          ...runState.history.map((r) => ({
                            label: dated(ms(r.started)),
                            detail: [t(`desktop.flows.run_status.${r.status}`), took(ms(r.started), ms(r.ended) || Date.now()), cost(r.usage?.costUsd, r.usage?.priced), t(`desktop.flows.origin.${r.origin || "chat"}`)].join(" · "),
                            icon: statusIcon(r.status).path,
                            on: r.id === runState.shownRun,
                            onSelect: () => runState.showHistoryRun(r.id),
                          })),
                        ]
                      : [{ heading: t("desktop.flows.history_none") }]
                  }
                />
              )}
              {runState.running ? (
                <Button icon={mdiStop} onClick={() => void runState.halt()}>
                  {t("desktop.evals.stop")}
                </Button>
              ) : (
                <Button
                  variant="tonal"
                  icon={mdiPlay}
                  disabled={!canRun}
                  title={canRun ? undefined : t(dirty || isNew ? "desktop.flows.save_first" : "desktop.flows.cannot_run")}
                  onClick={() => {
                    runState.setInput("");
                    runState.setAsking(true);
                  }}
                >
                  {t("desktop.evals.run")}
                </Button>
              )}
            </div>
            <div className="flow-body">
              <div className="flow-main">
                <div
                  className="flow-canvas"
                  ref={canvasRef}
                  onDragOver={(e) => {
                    if (e.dataTransfer.types.includes(stepDrag)) {
                      e.preventDefault();
                      e.dataTransfer.dropEffect = "copy";
                    }
                  }}
                  onDrop={drop}
                >
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
                    onInit={(flow: ReactFlowInstance<Node<StepData>, Edge>) => {
                      flowRef.current = flow;
                      setTimeout(() => void flow.fitView({ padding: 0.15, maxZoom: 1, minZoom: 0.55 }), 400);
                    }}
                    proOptions={{ hideAttribution: true }}
                  >
                    <Background gap={20} />
                    <Controls showInteractive={false} position="bottom-right" />
                  </ReactFlow>
                  <StepToolbox
                    open={toolbox}
                    onToggle={() => {
                      store("blitz.flows.toolbox", toolbox ? "closed" : "open");
                      setToolbox(!toolbox);
                      if (!toolbox) clearToolbox();
                    }}
                    onAdd={(type) => add(type)}
                  />
                </div>
                {runState.receipt && (
                  <RunPanel run={runState.receipt} titleOf={(id) => form.steps.find((s) => s.id === id)?.title || ""} step={selected} onStep={setSelected} onStop={() => void runState.halt()} onClose={runState.closeRun}>
                    {runState.approval && <ApprovalCard req={runState.approval} onDecide={(d) => void runState.decide(d)} />}
                    {runState.task && <TaskCard task={runState.task} onAnswer={(v) => void runState.answer(runState.task!, v)} />}
                  </RunPanel>
                )}
              </div>
              <WorkflowSidePanel
                workspace={workspace}
                workspaceMode={workspaceMode}
                file={file}
                dirty={dirty}
                form={form}
                setForm={setForm}
                step={step}
                link={link}
                selectedLink={selectedLink}
                stepRuns={stepRuns}
                agents={agents}
                skills={skills}
                rules={rules}
                mcpServers={mcpServers}
                list={list}
                sideOpen={sideOpen}
                setSideOpen={setSideOpen}
                sideWidth={sideWidth}
                setSideWidth={setSideWidth}
                canvasRef={canvasRef}
                setStep={setStep}
              />
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
            {unknown.length > 0 && (
              <div className="card warn row small-card">
                <Icon path={mdiAlertCircleOutline} size="sm" /> {t("desktop.flows.unknown_steps", { steps: unknown.join(", ") })}
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
                <Button danger icon={mdiDeleteOutline} onClick={() => setDeleting(true)}>
                  {t("desktop.rules.delete")}
                </Button>
              )}
              {dirty && file && (
                <Button icon={mdiUndoVariant} onClick={() => setForm(base)}>
                  {t("desktop.agents.revert")}
                </Button>
              )}
              <Button onClick={close}>{t("desktop.cancel")}</Button>
              <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || !form.name.trim() || (!dirty && !isNew) || unknown.length > 0} onClick={() => void save()}>
                {t("desktop.rules.save")}
              </Button>
            </div>
          </>
        )}
      </section>
      {composing && (
        <ComposeDialog
          workspace={workspace}
          kind="workflow"
          onClose={() => setComposing(false)}
          onDraft={(d) => {
            setComposing(false);
            const f = d.workflow ? formFromWorkflow(d.workflow) : emptyWorkflowForm;
            const taken = list.map((w) => w.name);
            const name = untakenName(d.name || f.name, taken);
            show("", { ...f, name, description: f.description || d.summary }, false);
          }}
        />
      )}
      {choosing && (
        <NewWorkflowDialog
          newMode={newMode}
          workspaceMode={workspaceMode}
          takenNames={list.map((w) => w.name)}
          onModeChange={setNewMode}
          onCompose={() => {
            setChoosing(false);
            setComposing(true);
          }}
          onPickTemplate={(f) => {
            setChoosing(false);
            show("", f, false);
          }}
          onClose={() => setChoosing(false)}
        />
      )}
      {runState.asking && (
        <RunWorkflowDialog
          name={form.name}
          input={runState.input}
          onInputChange={runState.setInput}
          onRun={() => void runState.run(form.name)}
          onClose={() => runState.setAsking(false)}
        />
      )}
      {deleting && (
        <ConfirmDialog title={t("desktop.flows.delete_title", { name: title })} icon={mdiDeleteOutline} confirm={t("desktop.rules.delete")} onConfirm={() => void remove()} onClose={() => setDeleting(false)}>
          <p>{t("desktop.rules.delete_body", { path: file?.path ?? "" })}</p>
        </ConfirmDialog>
      )}
      {discard(title)}
    </div>
  );
}
