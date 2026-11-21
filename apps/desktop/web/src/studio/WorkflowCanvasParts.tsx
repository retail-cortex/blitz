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

import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import {
  mdiAccountCheckOutline,
  mdiAccountNetworkOutline,
  mdiApi,
  mdiChevronDown,
  mdiChevronUp,
  mdiCircleOutline,
  mdiCloseThick,
  mdiCodeBraces,
  mdiDatabaseOutline,
  mdiEmailOutline,
  mdiFileDocumentOutline,
  mdiFileExportOutline,
  mdiFileImportOutline,
  mdiPlusThick,
  mdiPowerPlugOutline,
  mdiRobotOutline,
  mdiShapePlusOutline,
  mdiSitemapOutline,
  mdiTableArrowRight,
  mdiTextBoxEditOutline,
  mdiTimerSandComplete,
  mdiTrayFull,
  mdiWeb,
} from "@mdi/js";
import { Icon } from "@blitz/webui";
import type { WorkflowRunStep } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";
import { cost, ms, took } from "../workflowRuns";
import { statusIcon } from "./RunPanel";
import { agentTypes, comingSoon, gatewayTypes, stepGroups, stepNeeds, type StepForm, type StepType } from "./workflowForm";

/** Each step type's icon. */
export const typeIcons: Record<StepType, string> = {
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

/** Data payload carried by each ReactFlow step node. */
export type StepData = { step: StepForm; run?: WorkflowRunStep };

/** What dragging a step type from the toolbox carries. */
export const stepDrag = "application/x-blitz-step";

/** The settings panel's width as the user left it (0: the default). */
export const sideKey = "blitz.flows.side";

/** Default width in pixels of the workflow inspector side panel. */
export const sideDefault = 340;

/** Reads a persisted UI preference from localStorage safely. */
export const stored = (k: string) => {
  try {
    return localStorage.getItem(k) ?? "";
  } catch {
    return "";
  }
};

/** Writes a UI preference to localStorage safely. */
export const store = (k: string, v: string) => {
  try {
    localStorage.setItem(k, v);
  } catch {
    // a private window: the default next time
  }
};

/** A URL's host, for a step's card ("" when it isn't one yet). */
function hostOf(raw: string): string {
  try {
    return new URL(raw.replace(/\{\{.*?\}\}/g, "x")).host;
  } catch {
    return raw;
  }
}

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
            : s.type === "data"
              ? `${s.connection} › ${s.dataFrom === "saved" ? s.savedQuery : "SQL"}`
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

/** ReactFlow node type registry for workflow canvas steps. */
export const nodeTypes = { step: StepNode };

/**
 * The canvas's step toolbox: every step type by group, each dragged onto
 * the canvas (or clicked, to land in view); it folds to one button.
 */
export function StepToolbox({ open, onToggle, onAdd }: { open: boolean; onToggle: () => void; onAdd: (type: StepType) => void }) {
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
