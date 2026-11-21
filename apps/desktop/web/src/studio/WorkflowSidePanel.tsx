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

import type { Dispatch, RefObject, SetStateAction } from "react";
import { mdiChevronDoubleLeft, mdiChevronDoubleRight, mdiDeleteOutline } from "@mdi/js";
import { Field, Icon, IconButton, MultiSelect, ResizeHandle } from "@blitz/webui";
import type { SkillInfo, WorkflowFile, WorkflowRunStep } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";
import { flowModes, looser, modeOf } from "../options";
import { sideDefault, sideKey, store, typeIcons } from "./WorkflowCanvasParts";
import { WorkflowSchedule } from "./WorkflowSchedule";
import { WorkflowStepFields } from "./WorkflowStepFields";
import { linkId, removeStep, setLink, startsOnInput, type LinkKind, type StepForm, type WorkflowForm } from "./workflowForm";

/** The triggers a workflow can run on besides /name (WF-30). */
const flowTriggers = ["schedule", "git.pre_commit", "git.pre_push"];

/** A workflow's permission mode selector, noting when the workspace's mode holds a looser choice back (WF-21). */
export function FlowModeField({ value, workspaceMode, onChange }: { value: string; workspaceMode: string; onChange: (mode: string) => void }) {
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

/** Collapsible inspector panel beside the workflow canvas for step, link, or workflow settings. */
export function WorkflowSidePanel({
  workspace,
  workspaceMode,
  file,
  dirty,
  form,
  setForm,
  step,
  link,
  selectedLink,
  stepRuns,
  agents,
  skills,
  rules,
  mcpServers,
  list,
  sideOpen,
  setSideOpen,
  sideWidth,
  setSideWidth,
  canvasRef,
  setStep,
}: {
  workspace: string;
  workspaceMode: string;
  file?: WorkflowFile;
  dirty: boolean;
  form: WorkflowForm;
  setForm: Dispatch<SetStateAction<WorkflowForm>>;
  step?: StepForm;
  link?: WorkflowForm["links"][number];
  selectedLink: string;
  stepRuns: Record<string, WorkflowRunStep>;
  agents: string[];
  skills: SkillInfo[];
  rules: string[];
  mcpServers: string[];
  list: WorkflowFile[];
  sideOpen: boolean;
  setSideOpen: (open: boolean) => void;
  sideWidth: number;
  setSideWidth: (w: number) => void;
  canvasRef: RefObject<HTMLDivElement | null>;
  setStep: (id: string, patch: Partial<StepForm>) => void;
}) {
  const titleOf = (id: string) => form.steps.find((s) => s.id === id)?.title || id;
  const fromType = (id: string) => form.steps.find((s) => s.id === id)?.type;

  return (
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
              <WorkflowStepFields
                workspace={workspace}
                workflowName={form.name}
                step={step}
                stepRun={stepRuns[step.id]}
                agents={agents}
                skills={skills}
                rules={rules}
                mcpServers={mcpServers}
                otherWorkflows={list.map((w) => w.name)}
                onChange={(patch) => setStep(step.id, patch)}
              />
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
                <Field label={t("desktop.flows.triggers")} supporting={t("desktop.flows.triggers_hint")}>
                  {(id) => (
                    <MultiSelect
                      id={id}
                      label={t("desktop.flows.triggers")}
                      none={t("desktop.flows.trigger.manual")}
                      options={flowTriggers.map((tr) => ({ value: tr, detail: t(`desktop.flows.trigger.${tr}`) }))}
                      chosen={form.triggers}
                      onChange={(v) => setForm((f) => ({ ...f, triggers: v }))}
                    />
                  )}
                </Field>
                <WorkflowSchedule workspace={workspace} workflow={file?.name ?? ""} disabled={!file || dirty} permissions={file?.schedulePermissions ?? []} needsInput={startsOnInput(form)} />
                <span className="t-body-sm muted">{t("desktop.flows.canvas_hint")}</span>
              </div>
            )}
          </div>
        </>
      )}
    </aside>
  );
}
