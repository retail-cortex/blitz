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

import { createPortal } from "react-dom";
import { mdiAutoFix, mdiPlay, mdiSitemapOutline } from "@mdi/js";
import { Button, Dialog, Field, Icon } from "@blitz/webui";
import { t } from "../i18n";
import { FlowModeField } from "./WorkflowSidePanel";
import { fromTemplate, templates, type Template, type WorkflowForm } from "./workflowForm";

/** Dialog for picking a template or composing a new workflow with AI. */
export function NewWorkflowDialog({
  newMode,
  workspaceMode,
  takenNames,
  onModeChange,
  onCompose,
  onPickTemplate,
  onClose,
}: {
  newMode: string;
  workspaceMode: string;
  takenNames: string[];
  onModeChange: (mode: string) => void;
  onCompose: () => void;
  onPickTemplate: (form: WorkflowForm) => void;
  onClose: () => void;
}) {
  return createPortal(
    <Dialog title={t("desktop.flows.new")} icon={mdiSitemapOutline} onClose={onClose} footer={<Button onClick={onClose}>{t("desktop.cancel")}</Button>}>
      <FlowModeField value={newMode} workspaceMode={workspaceMode} onChange={onModeChange} />
      <div className="list">
        <button type="button" className="list-item compose-entry" onClick={onCompose}>
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
              const f = fromTemplate(tpl, (k) => t(`desktop.flows.template.${k}`), newMode);
              let name = f.name;
              for (let n = 2; name && takenNames.includes(name); n++) name = `${f.name}-${n}`;
              onPickTemplate({ ...f, name });
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
  );
}

/** Dialog asking for optional input before starting a workflow run. */
export function RunWorkflowDialog({
  name,
  input,
  onInputChange,
  onRun,
  onClose,
}: {
  name: string;
  input: string;
  onInputChange: (v: string) => void;
  onRun: () => void;
  onClose: () => void;
}) {
  return createPortal(
    <Dialog
      title={t("desktop.flows.run_title", { name })}
      icon={mdiPlay}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" icon={mdiPlay} onClick={onRun}>
            {t("desktop.evals.run")}
          </Button>
        </>
      }
    >
      <div className="stack" style={{ gap: 8 }}>
        <p className="t-body-sm muted">{t("desktop.flows.run_hint", { name })}</p>
        <Field label={t("desktop.flows.input")}>{(id) => <textarea id={id} className="input" rows={3} autoFocus value={input} placeholder={t("desktop.flows.input_placeholder")} onChange={(e) => onInputChange(e.target.value)} />}</Field>
      </div>
    </Dialog>,
    document.body,
  );
}
