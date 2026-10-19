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

import { mdiAccountCheckOutline } from "@mdi/js";
import type { HumanTask } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";
import { Icon } from "../ui/controls";
import { A2uiForm } from "./A2uiForm";
import { parseMessages, type FormAction } from "./model";

/** A human task a run waits on: its title, message and form, answered by the form's buttons. */
export function TaskCard({ task, onAnswer, busy }: { task: HumanTask; onAnswer: (action: FormAction) => void; busy?: boolean }) {
  // The form as the service sent it: A2UI's messages, filled from the run.
  const parsed = parseMessages(task.a2uiJson);
  return (
    <div className="card outlined stack task-card" style={{ gap: 8 }} aria-busy={busy || undefined}>
      <span className="row t-title-sm" style={{ gap: 6 }}>
        <Icon path={mdiAccountCheckOutline} size="sm" />
        {task.title}
      </span>
      <small className="muted">{t("desktop.human_tasks.from", { workflow: task.workflow })}</small>
      {task.message && <p className="t-body task-message">{task.message}</p>}
      {parsed.form ? <A2uiForm form={parsed.form} onAction={onAnswer} disabled={busy} /> : <span className="t-body-sm error-text">{t("desktop.flows.form_problem", { problem: parsed.problem ?? "" })}</span>}
    </div>
  );
}
