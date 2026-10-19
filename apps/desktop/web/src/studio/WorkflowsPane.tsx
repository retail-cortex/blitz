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

import { GradeBadge } from "./GradeBadge";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Background, Controls, Handle, Position, ReactFlow, type Connection, type Edge, type EdgeChange, type Node, type NodeChange, type NodeProps, type ReactFlowInstance } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  mdiAlertCircleOutline,
  mdiArrowLeft,
  mdiChevronDoubleLeft,
  mdiChevronDoubleRight,
  mdiChevronDown,
  mdiChevronUp,
  mdiCodeBraces,
  mdiHistory,
  mdiShapePlusOutline,
  mdiContentSaveOutline,
  mdiAccountCheckOutline,
  mdiAccountNetworkOutline,
  mdiTimerSandComplete,
  mdiApi,
  mdiCircleOutline,
  mdiPowerPlugOutline,
  mdiCloseThick,
  mdiDatabaseOutline,
  mdiPlusThick,
  mdiTableArrowRight,
  mdiDeleteOutline,
  mdiEmailOutline,
  mdiFileDocumentOutline,
  mdiFileImportOutline,
  mdiFileExportOutline,
  mdiPlay,
  mdiPlus,
  mdiRobotOutline,
  mdiSitemapOutline,
  mdiStop,
  mdiTextBoxEditOutline,
  mdiTrayFull,
  mdiUndoVariant,
  mdiWeb,
  mdiAutoFix,
} from "@mdi/js";
import { A2uiForm } from "../a2ui/A2uiForm";
import { TaskCard } from "../a2ui/TaskCard";
import type { FormAction } from "../a2ui/model";
import { answerTask, useTasks } from "../humanTasks";
import { parseForm, starterForm } from "../a2ui/model";
import { sessions, workspaces } from "../api";
import { DraftButton } from "./Draft";
import { ApprovalCard } from "../Conversation";
import { message } from "../errors";
import { Decision, type ApprovalRequest } from "../gen/blitz/v1/turn_pb";
import type { HumanTask, SkillInfo, WorkflowFile, WorkflowRun, WorkflowRunStep } from "../gen/blitz/v1/workspace_pb";
import type { ItemIntent } from "../intent";
import { ResizeHandle } from "../ui/ResizeHandle";
import { flowModes, looser, modeOf } from "../options";
import { useWorkspaceStatus } from "../status";
import { cost, goingRun, latestSteps, ms, progress, seeRun, took, useWorkflowRuns, watchedRuns } from "../workflowRuns";
import { dated, RunPanel, statusIcon } from "./RunPanel";
import { WorkflowSchedule } from "./WorkflowSchedule";
import { ComposeDialog } from "./ComposeDialog";
import { t, tn } from "../i18n";
import { SuggestInput, SuggestTextarea } from "../SecretSuggest";
import { Button, Dialog, Field, Icon, IconButton, Menu, MultiSelect, Segmented, useSnackbar } from "../ui/controls";
import { schedulePermissions, startsOnInput, addLink, agentTypes, comingSoon, emptyWorkflowForm, formFromWorkflow, fromTemplate, gatewayTypes, linkId, pauseTypes, linkText, newStep, removeStep, sameWorkflowForm, setLink, specFromForm, methods, stepGroups, stepNeeds, templates, type AuthForm, type FieldForm, type PairForm, type LinkKind, type StepForm, type StepType, type Template, type WorkflowForm } from "./workflowForm";

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
  human_task: mdiAccountCheckOutline,
  timer: mdiTimerSandComplete,
};

type StepData = { step: StepForm; run?: WorkflowRunStep };

/** What dragging a step type from the toolbox carries. */
const stepDrag = "application/x-blitz-step";

/** The settings panel's width as the user left it (0: the default). */
const sideKey = "blitz.flows.side";
const sideDefault = 340;
const stored = (k: string) => {
  try {
    return localStorage.getItem(k) ?? "";
  } catch {
    return "";
  }
};
const store = (k: string, v: string) => {
  try {
    localStorage.setItem(k, v);
  } catch {
    // a private window: the default next time
  }
};

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
              : s.type === "timer"
                ? s.wait || s.until
                : s.type === "human_task"
                  ? t("desktop.flows.form_ready")
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
        {data.run && <RunMeta run={data.run} />}
      </span>
      {status && <StatusMark run={data.run!} />}
      {s.type !== "save_file" && <Handle type="source" position={Position.Right} />}
    </div>
  );
}
const nodeTypes = { step: StepNode };

/** What a step's run took and spent, once it's ended. */
const runMeta = (run: WorkflowRunStep) => (run.ended && run.status !== "skipped" ? [took(ms(run.started), ms(run.ended)), run.usage?.calls ? cost(run.usage.costUsd, run.usage.priced) : ""].filter(Boolean).join(" · ") : "");

/** A step's state in the run shown. */
function StatusMark({ run }: { run: WorkflowRunStep }) {
  const icon = statusIcon(run.status);
  return (
    <span className="flow-node-status" title={[t(`desktop.flows.run_status.${run.status}`), runMeta(run), run.error].filter(Boolean).join(" · ")}>
      <Icon path={icon.path} spin={icon.spin} className={icon.className} size="sm" />
    </span>
  );
}

/** Under a step's title: what its run took and spent. */
function RunMeta({ run }: { run: WorkflowRunStep }) {
  const meta = runMeta(run);
  return meta ? <small className="flow-node-meta">{meta}</small> : null;
}

/**
 * The canvas's step toolbox: every step type by group, each dragged onto
 * the canvas (or clicked, to land in view); it folds to one button.
 */
function StepToolbox({ open, onToggle, onAdd }: { open: boolean; onToggle: () => void; onAdd: (type: StepType) => void }) {
  return (
    <div className={`flow-toolbox ${open ? "is-open" : ""}`} role="toolbar" aria-label={t("desktop.flows.palette")}>
      <button type="button" className="flow-toolbox-head" aria-expanded={open} onClick={onToggle}>
        <Icon path={mdiShapePlusOutline} size="sm" />
        <span className="spacer">{t("desktop.flows.toolbox")}</span>
        <Icon path={open ? mdiChevronUp : mdiChevronDown} size="sm" />
      </button>
      {open && (
        <div className="flow-toolbox-body">
          <span className="t-body-sm muted flow-toolbox-hint">{t("desktop.flows.toolbox_hint")}</span>
          {stepGroups.map(({ group, types }) => (
            <div key={group} className="flow-toolbox-group">
              <span className="menu-label">{t(`desktop.flows.group.${group}`)}</span>
              {types.map((type) => {
                const soon = comingSoon.includes(type);
                return (
                  <button
                    key={type}
                    type="button"
                    className="flow-tool"
                    draggable={!soon}
                    disabled={soon}
                    title={t(`desktop.flows.type_hint.${type}`)}
                    onDragStart={(e) => {
                      e.dataTransfer.setData(stepDrag, type);
                      e.dataTransfer.effectAllowed = "copy";
                    }}
                    onClick={() => onAdd(type)}
                  >
                    <Icon path={typeIcons[type]} size="sm" />
                    <span className="ellipsis">{t(`desktop.flows.type.${type}`)}</span>
                  </button>
                );
              })}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * The workspace's workflows (Studio › Workflows, spec_workflows_043):
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
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [choosing, setChoosing] = useState(false);
  // The composer making a workflow from a conversation (spec_composer_045).
  const [composing, setComposing] = useState(false);
  // The mode a new workflow runs in, chosen as it's made (WF-21).
  const [newMode, setNewMode] = useState("auto");
  const workspaceMode = useWorkspaceStatus(workspace).settings?.permissionMode ?? "auto";
  const [asking, setAsking] = useState(false);
  const [input, setInput] = useState("");
  const [agents, setAgents] = useState<string[]>([]);
  const [skills, setSkills] = useState<SkillInfo[]>([]);
  const [rules, setRules] = useState<string[]>([]);
  const [mcpServers, setMcpServers] = useState<string[]>([]);
  // The run shown under the canvas (its receipt, followed while it runs),
  // the approval its background run waits on, and the runs closed here.
  const [shownRun, setShownRun] = useState("");
  const [receipt, setReceipt] = useState<WorkflowRun | null>(null);
  const [approval, setApproval] = useState<ApprovalRequest | null>(null);
  const dismissed = useRef(new Set<string>());
  const [history, setHistory] = useState<WorkflowRun[]>([]);
  const { runs } = useWorkflowRuns();
  const { tasks } = useTasks();
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
    workspaces.listAgents({ workspace }).then((r) => setAgents(r.agents.map((a) => a.name)), () => {});
    workspaces.listSkills({ workspace }).then((r) => setSkills(r.skills), () => {});
    workspaces.listRules({ workspace }).then((r) => setRules(r.rules.map((x) => x.name)), () => {});
    workspaces.listMCPServers({ workspace }).then((r) => setMcpServers(r.servers.map((x) => x.name)), () => {});
  }, [workspace, refresh]);

  const file = open ? list.find((w) => w.name === open) : undefined;
  const isNew = open === "";
  const dirty = open !== null && !sameWorkflowForm(form, base);
  const running = receipt?.status === "running";
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);
  useEffect(() => onCanvas?.(open !== null), [open, onCanvas]);
  useEffect(() => () => onCanvas?.(false), [onCanvas]);

  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());
  const show = (name: string, f: WorkflowForm, saved: boolean) =>
    leave(() => {
      setForm(f);
      setBase(saved ? f : emptyWorkflowForm);
      setOpen(name);
      setSelected("");
      setProblems([]);
      setShownRun("");
    });
  const close = () => {
    leave(() => {
      setOpen(null);
      void refresh();
    });
  };
  const step = form.steps.find((s) => s.id === selected);
  const setStep = (id: string, patch: Partial<StepForm>) => setForm((f) => ({ ...f, steps: f.steps.map((s) => (s.id === id ? { ...s, ...patch } : s)) }));
  const flowRef = useRef<ReactFlowInstance<Node<StepData>, Edge> | null>(null);
  const canvasRef = useRef<HTMLDivElement>(null);
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
  const stepRuns = useMemo(() => (receipt ? latestSteps(receipt.steps) : {}), [receipt]);
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

  // A workflow asked for (the status bar, an alert, a notification): open
  // it, and the run named on it.
  const handled = useRef(0);
  useEffect(() => {
    if (!intent || intent.n === handled.current || !intent.id) return;
    const w = list.find((x) => x.name === intent.id);
    if (!w) return;
    handled.current = intent.n;
    show(w.name, formFromWorkflow(w), true);
    if (runToShow) {
      dismissed.current.delete(runToShow);
      setShownRun(runToShow);
    }
    // show is the pane's own, made each render
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [intent, list, runToShow]);

  // A workflow opened while a run of it goes (started anywhere) shows that
  // run, unless it was closed here.
  const going = open ? goingRun(runs, workspace, open) : undefined;
  useEffect(() => {
    if (going && !shownRun && !dismissed.current.has(going.id)) setShownRun(going.id);
  }, [going, shownRun]);

  // Follows the shown run's receipt while it runs; a paused run that
  // resumes (its task answered, its timer fired) is followed again.
  const storeStatus = runs.find((r) => r.id === shownRun)?.status;
  useEffect(() => {
    if (!shownRun) {
      setReceipt(null);
      return;
    }
    const ctl = new AbortController();
    void (async () => {
      try {
        for await (const r of workspaces.watchWorkflowRun({ workspace, id: shownRun }, { signal: ctl.signal })) if (r.run) setReceipt(r.run);
      } catch (e) {
        if (!ctl.signal.aborted) snack(message(e), { error: true });
      }
    })();
    return () => ctl.abort();
  }, [shownRun, workspace, storeStatus, snack]);

  // A run shown takes room under the canvas: the workflow is fitted again
  // in what's left.
  const hasRun = !!receipt;
  useEffect(() => {
    if (!hasRun) return;
    const id = setTimeout(() => void flowRef.current?.fitView({ padding: 0.15, maxZoom: 1, minZoom: 0.55, duration: 200 }), 300);
    return () => clearTimeout(id);
  }, [hasRun, shownRun]);
  // While it's shown here, the app doesn't also announce its end.
  useEffect(() => {
    if (!shownRun) return;
    watchedRuns.add(shownRun);
    return () => void watchedRuns.delete(shownRun);
  }, [shownRun]);
  // A failure seen here leaves the status bar.
  const shownFailed = receipt?.status === "failed" ? receipt.id : "";
  useEffect(() => {
    if (shownFailed) seeRun(shownFailed);
  }, [shownFailed]);

  // The approvals the run's background run waits on, asked for here.
  const bgId = receipt?.status === "running" ? receipt.backgroundId : "";
  useEffect(() => {
    setApproval(null);
    if (!bgId) return;
    const ctl = new AbortController();
    void (async () => {
      try {
        for await (const res of sessions.watchBackground({ id: bgId, follow: true }, { signal: ctl.signal })) {
          const k = res.event?.kind;
          if (k?.case === "approvalRequest") setApproval(k.value);
          else if (k?.case === "finished") setApproval(null);
        }
      } catch {
        // it ended, or the service is going: the receipt says how
      }
    })();
    return () => ctl.abort();
  }, [bgId]);

  // The workflow's past runs, for the history menu, again as runs end.
  const endedCount = runs.filter((r) => r.workflow === open && r.status !== "running").length;
  useEffect(() => {
    if (!open) return setHistory([]);
    workspaces.listWorkflowRuns({ workspace, workflow: open, limit: 15 }).then(
      (r) => setHistory(r.runs),
      () => setHistory([]),
    );
  }, [open, workspace, endedCount, receipt?.status]);

  // Runs the workflow as saved, in the background, as /name input: its
  // receipt shows as its first step starts. It goes on with Studio closed.
  const run = async (name: string) => {
    setAsking(false);
    try {
      const started = await sessions.startBackground({ workspace, turn: { text: `/${name} ${input}`.trim(), command: true } });
      for await (const res of sessions.watchBackground({ id: started.run?.id ?? "", follow: true })) {
        const k = res.event?.kind;
        if (k?.case === "flowStep" && k.value.run) {
          dismissed.current.delete(k.value.run);
          setShownRun(k.value.run);
          return;
        }
        if (k?.case === "finished") {
          const err = k.value.error;
          if (err) snack(err.message || err.reason, { error: true });
          return;
        }
      }
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  // Answers the human task the run waits on: it resumes in the background.
  const answer = async (task: HumanTask, action: FormAction) => {
    try {
      await answerTask(task, action);
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const halt = async () => {
    try {
      await workspaces.stopWorkflowRun({ workspace, id: shownRun });
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const decide = async (d: Decision) => {
    const req = approval;
    if (!req) return;
    setApproval(null);
    try {
      await sessions.approve({ workspace, requestId: req.requestId, decision: d });
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const closeRun = () => {
    dismissed.current.add(shownRun);
    setShownRun("");
  };
  const task = receipt?.status === "waiting" ? tasks.find((x) => x.run === receipt.id) : undefined;

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
            const live = goingRun(runs, workspace, w.name);
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
                    setInput("");
                    setAsking(true);
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
                    history.length
                      ? [
                          { heading: t("desktop.flows.history") },
                          ...history.map((r) => ({
                            label: dated(ms(r.started)),
                            detail: [t(`desktop.flows.run_status.${r.status}`), took(ms(r.started), ms(r.ended) || Date.now()), cost(r.usage?.costUsd, r.usage?.priced), t(`desktop.flows.origin.${r.origin || "chat"}`)].join(" · "),
                            icon: statusIcon(r.status).path,
                            on: r.id === shownRun,
                            onSelect: () => {
                              dismissed.current.delete(r.id);
                              setShownRun(r.id);
                            },
                          })),
                        ]
                      : [{ heading: t("desktop.flows.history_none") }]
                  }
                />
              )}
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
                {receipt && (
                  <RunPanel run={receipt} titleOf={(id) => form.steps.find((s) => s.id === id)?.title || ""} step={selected} onStep={setSelected} onStop={() => void halt()} onClose={closeRun}>
                    {approval && <ApprovalCard req={approval} onDecide={(d) => void decide(d)} />}
                    {task && <TaskCard task={task} onAnswer={(v) => void answer(task, v)} />}
                  </RunPanel>
                )}
              </div>
              <aside className={`flow-side ${sideOpen ? "" : "is-closed"}`} style={sideOpen ? { width: sideWidth } : undefined} aria-label={t("desktop.flows.settings_panel")}>
                {!sideOpen ? (
                  <button type="button" className="flow-side-rail" onClick={() => setSideOpen(true)} title={t("desktop.flows.side_show")} aria-label={t("desktop.flows.side_show")}>
                    <Icon path={mdiChevronDoubleLeft} size="sm" />
                    <span>{step ? step.title || t(`desktop.flows.type.${step.type}`) : link ? t("desktop.flows.link") : t("desktop.flows.settings")}</span>
                  </button>
                ) : (
                  <>
                    <ResizeHandle
                      width={sideWidth}
                      edge="left"
                      min={260}
                      max={() => (canvasRef.current?.parentElement?.parentElement?.getBoundingClientRect().width ?? 1200) * 0.5}
                      label={t("desktop.flows.side_resize")}
                      onResize={(w) => {
                        const v = w || sideDefault;
                        setSideWidth(v);
                        store(sideKey, String(w));
                      }}
                    />
                    <div className="row flow-side-head">
                      {step ? <Icon path={typeIcons[step.type]} /> : null}
                      <span className="t-title-sm spacer ellipsis">{step ? t(`desktop.flows.type.${step.type}`) : link ? t("desktop.flows.link_title", { from: titleOf(link.from), to: titleOf(link.to) }) : t("desktop.flows.settings")}</span>
                      {step && <IconButton icon={mdiDeleteOutline} label={t("desktop.flows.remove_step")} small onClick={() => setForm((f) => removeStep(f, step.id))} />}
                      {link && <IconButton icon={mdiDeleteOutline} label={t("desktop.flows.remove_link")} small onClick={() => setForm((f) => ({ ...f, links: f.links.filter((l) => linkId(l) !== selectedLink) }))} />}
                      <IconButton icon={mdiChevronDoubleRight} label={t("desktop.flows.side_hide")} small onClick={() => setSideOpen(false)} />
                    </div>
                    <div className="flow-side-body stack">
                {step ? (
                  <div className="stack flow-step-form" style={{ gap: 10 }}>
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
                        <Field label={t("desktop.flows.skills")} supporting={t("desktop.flows.skills_hint")}>
                          {(id) => <MultiSelect id={id} label={t("desktop.flows.skills")} none={t("desktop.flows.none_chosen")} options={skills.map((s) => ({ value: s.name, detail: s.description }))} chosen={step.skills} onChange={(v) => setStep(step.id, { skills: v })} />}
                        </Field>
                        <Field label={t("desktop.flows.rules")} supporting={t("desktop.flows.rules_hint")}>
                          {(id) => <MultiSelect id={id} label={t("desktop.flows.rules")} none={t("desktop.flows.none_chosen")} options={rules.map((r) => ({ value: r }))} chosen={step.rules} onChange={(v) => setStep(step.id, { rules: v })} />}
                        </Field>
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
                    {step.type === "human_task" && <HumanTaskFields step={step} workspace={workspace} onChange={(patch) => setStep(step.id, patch)} />}
                    {step.type === "timer" && (
                      <>
                        <Field label={t("desktop.flows.wait")} supporting={t("desktop.flows.wait_hint")}>
                          {(id) => <input id={id} className="input mono" value={step.wait} placeholder="2h" onChange={(e) => setStep(step.id, { wait: e.target.value, until: "" })} />}
                        </Field>
                        <Field label={t("desktop.flows.until")} supporting={t("desktop.flows.until_hint")}>
                          {(id) => <input id={id} className="input mono" value={step.until} placeholder="2026-10-20T09:00:00Z" onChange={(e) => setStep(step.id, { until: e.target.value, wait: "" })} />}
                        </Field>
                      </>
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
                    {!gatewayTypes.includes(step.type) && !pauseTypes.includes(step.type) && (
                      <div className="stack" style={{ gap: 6 }}>
                        <span className="t-label">{t("desktop.flows.repeat")}</span>
                        <span className="t-body-sm muted">{t("desktop.flows.repeat_hint")}</span>
                        <div className="row flow-field" style={{ gap: 6 }}>
                          <input className="input mono" aria-label={t("desktop.flows.for_each")} placeholder={t("desktop.flows.for_each_placeholder")} value={step.forEach} onChange={(e) => setStep(step.id, { forEach: e.target.value })} />
                          <input className="input flow-concurrency" type="number" min={1} max={32} aria-label={t("desktop.flows.concurrency")} title={t("desktop.flows.concurrency")} placeholder="4" disabled={!step.forEach.trim()} value={step.concurrency} onChange={(e) => setStep(step.id, { concurrency: e.target.value })} />
                        </div>
                      </div>
                    )}
                    {!gatewayTypes.includes(step.type) && !pauseTypes.includes(step.type) && (
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
                    {stepRuns[step.id] && (stepRuns[step.id].output || stepRuns[step.id].error) && (
                      <div className="stack" style={{ gap: 2 }}>
                        <span className="t-label">{t(stepRuns[step.id].error ? "desktop.flows.step_error" : "desktop.flows.step_output")}</span>
                        <pre className="eval-text">{stepRuns[step.id].error || stepRuns[step.id].output}</pre>
                      </div>
                    )}
                  </div>
                ) : link ? (
                  <div className="stack flow-link-form" style={{ gap: 10 }}>
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
                    <Field label={t("desktop.flows.name")} supporting={t("desktop.flows.name_hint", { name: form.name || "name" })}>
                      {(id) => <input id={id} className="input mono" value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />}
                    </Field>
                    <Field label={t("desktop.rules.description")}>{(id) => <input id={id} className="input" value={form.description} onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))} />}</Field>
                    <FlowModeField value={form.mode} workspaceMode={workspaceMode} onChange={(mode) => setForm((f) => ({ ...f, mode }))} />
                    <WorkflowSchedule workspace={workspace} workflow={file?.name ?? ""} disabled={!file || dirty} permissions={schedulePermissions(form.steps)} needsInput={startsOnInput(form)} />
                    <span className="t-body-sm muted">{t("desktop.flows.canvas_hint")}</span>
                  </div>
                )}
                    </div>
                  </>
                )}
              </aside>
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
                <Button danger icon={mdiDeleteOutline} onClick={() => setDeleting(true)}>
                  {t("desktop.rules.delete")}
                </Button>
              )}
              {dirty && file && (
                <Button icon={mdiUndoVariant} onClick={() => setForm(base)}>
                  {t("desktop.agents.revert")}
                </Button>
              )}
              <Button onClick={close}>
                {t("desktop.cancel")}
              </Button>
              <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || !form.name.trim() || (!dirty && !isNew)} onClick={() => void save()}>
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
            let name = d.name || f.name;
            for (let n = 2; name && taken.includes(name); n++) name = `${d.name}-${n}`;
            show("", { ...f, name, description: f.description || d.summary }, false);
          }}
        />
      )}
      {choosing &&
        createPortal(
          <Dialog title={t("desktop.flows.new")} icon={mdiSitemapOutline} onClose={() => setChoosing(false)} footer={<Button onClick={() => setChoosing(false)}>{t("desktop.cancel")}</Button>}>
            <FlowModeField value={newMode} workspaceMode={workspaceMode} onChange={setNewMode} />
            <div className="list">
              <button
                type="button"
                className="list-item compose-entry"
                onClick={() => {
                  setChoosing(false);
                  setComposing(true);
                }}
              >
                <Icon path={mdiAutoFix} />
                <span className="lines">
                  <span className="ellipsis">{t("desktop.compose.entry")}</span>
                  <small className="ellipsis">{t("desktop.compose.entry_about")}</small>
                </span>
              </button>
              {templates.map((tpl: Template) => (
                <button
                  key={tpl}
                  type="button"
                  className="list-item"
                  onClick={() => {
                    setChoosing(false);
                    const f = fromTemplate(tpl, (k) => t(`desktop.flows.template.${k}`), newMode);
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

/** A human task's settings: the message above its form, and the form (A2UI JSON) beside a preview of it. */
function HumanTaskFields({ step, workspace, onChange }: { step: StepForm; workspace: string; onChange: (patch: Partial<StepForm>) => void }) {
  const parsed = parseForm(step.formJson);
  return (
    <>
      <Field label={t("desktop.flows.task_message")} supporting={t("desktop.flows.task_message_hint")}>
        {(id) => <SuggestTextarea id={id} className="input" rows={3} value={step.instructions} onValue={(v) => onChange({ instructions: v })} workspace={workspace} />}
      </Field>
      <div className="row field-tools">
        <span className="t-label spacer">{t("desktop.flows.form")}</span>
        <DraftButton workspace={workspace} kind="form" name={step.title || step.id} description={step.instructions} current={step.formJson} onUse={(v) => onChange({ formJson: v })} />
      </div>
      <textarea className="input mono flow-form-json" rows={10} aria-label={t("desktop.flows.form")} value={step.formJson} spellCheck={false} onChange={(e) => onChange({ formJson: e.target.value })} />
      <span className={`t-body-sm ${parsed.problem ? "error-text" : "muted"}`}>{parsed.problem ? t("desktop.flows.form_problem", { problem: parsed.problem }) : t("desktop.flows.form_hint")}</span>
      {parsed.form && (
        <div className="card outlined flow-form-preview">
          <span className="t-label">{t("desktop.flows.form_preview")}</span>
          <A2uiForm form={parsed.form} />
        </div>
      )}
    </>
  );
}

/** A workflow's permission mode, and what the workspace's mode makes of it: a looser one is held back (WF-21). */
function FlowModeField({ value, workspaceMode, onChange }: { value: string; workspaceMode: string; onChange: (mode: string) => void }) {
  const held = looser(value, workspaceMode);
  return (
    <Field label={t("desktop.flows.mode")} supporting={held ? t("desktop.flows.mode_held", { mode: modeOf(workspaceMode).label }) : modeOf(value).detail}>
      {(id) => (
        <select id={id} className="select" value={value} onChange={(e) => onChange(e.target.value)}>
          {flowModes().map((m) => (
            <option key={m.value} value={m.value}>
              {m.value === "auto" ? t("desktop.flows.mode_default", { mode: m.label }) : m.label}
            </option>
          ))}
        </select>
      )}
    </Field>
  );
}
