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

import { useEffect } from "react";
import { onTasksOpen } from "./desktop";
import { onArrived, showTasks, useTasks } from "./humanTasks";
import { t, tn } from "./i18n";
import { notify } from "./notify";
import { useSnackbar } from "./ui/controls";

/**
 * Announces workflows' human tasks as they arrive, as [ui] task_alerts
 * says (spec_workflows_043 WF-18): open shows the tasks dialog; notify
 * shows a snackbar with Open, or a system notification when the window
 * isn't in front (clicking it opens the dialog); off, nothing (the status
 * bar still shows them). The tray's "tasks waiting" opens the dialog too.
 */
export function TaskAlerts() {
  const { alerts } = useTasks();
  const snack = useSnackbar();
  useEffect(() => onTasksOpen(showTasks), []);
  useEffect(
    () =>
      onArrived((fresh) => {
        if (alerts === "off") return;
        if (alerts === "open") return showTasks();
        const first = fresh[0];
        const text = fresh.length === 1 ? first.title : tn("desktop.human_tasks.arrived_more", fresh.length - 1, { title: first.title });
        if (document.hasFocus()) snack(t("desktop.human_tasks.arrived", { workflow: first.workflow, title: text }), { action: { label: t("desktop.human_tasks.open"), run: showTasks } });
        else notify(t("desktop.human_tasks.notify_title", { workflow: first.workflow }), text, "");
      }),
    [alerts, snack],
  );
  return null;
}
