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

import { useCallback, useEffect, useState } from "react";
import { mdiAccountCheckOutline, mdiOpenInApp } from "@mdi/js";
import { workspaces } from "./api";
import { TaskCard } from "./a2ui/TaskCard";
import { canOpenApp, openApp } from "./desktop";
import { message } from "./errors";
import type { HumanTask } from "./gen/blitz/v1/workspace_pb";
import { dropTask, hideTasks, refreshTasks, useTasks } from "./humanTasks";
import { t, tn } from "./i18n";
import { ErrorBoundary } from "./ErrorBoundary";
import { AppStateProvider } from "./state";
import { Button, Dialog, Icon, SnackbarProvider, useSnackbar } from "./ui/controls";

/**
 * Workflows' human tasks (spec_workflows_043 WF-18): with more than one, a
 * list to choose from; the chosen task's form, whose buttons answer it and
 * let its run go on. done: there are none left.
 */
export function TasksPanel({ onDone }: { onDone?: () => void }) {
  const { tasks, loaded } = useTasks();
  const snack = useSnackbar();
  const [chosen, setChosen] = useState("");
  const [busy, setBusy] = useState(false);
  const task = tasks.find((x) => x.id === chosen) ?? tasks[0];
  useEffect(() => {
    if (loaded && tasks.length === 0) onDone?.();
  }, [loaded, tasks.length, onDone]);

  const answer = async (x: HumanTask, values: Record<string, unknown>) => {
    setBusy(true);
    try {
      await workspaces.answerHumanTask({ workspace: x.workspace, taskId: x.id, answerJson: JSON.stringify(values) });
      dropTask(x.id);
      snack(t("desktop.human_tasks.answered", { workflow: x.workflow }));
      void refreshTasks();
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy(false);
    }
  };

  if (!task) return <p className="t-body-sm muted tasks-empty">{t(loaded ? "desktop.human_tasks.none" : "desktop.human_tasks.loading")}</p>;
  return (
    <div className="tasks-panel">
      {tasks.length > 1 && (
        <div className="list tasks-list" role="listbox" aria-label={t("desktop.human_tasks.list")}>
          {tasks.map((x) => (
            <button key={x.id} role="option" aria-selected={x.id === task.id} className={`list-item ${x.id === task.id ? "active" : ""}`} onClick={() => setChosen(x.id)}>
              <Icon path={mdiAccountCheckOutline} size="sm" />
              <span className="stack ellipsis" style={{ gap: 0 }}>
                <span className="ellipsis">{x.title}</span>
                <small className="muted ellipsis">{x.workflow}</small>
              </span>
            </button>
          ))}
        </div>
      )}
      <TaskCard key={task.id} task={task} busy={busy} onAnswer={(v) => void answer(task, v)} />
    </div>
  );
}

/** The tasks in a dialog over the app (the status bar's button, the tray, a notification). */
export function TasksDialog() {
  const { shown, tasks } = useTasks();
  if (!shown) return null;
  return (
    <Dialog
      title={tn("desktop.human_tasks.title", tasks.length)}
      icon={mdiAccountCheckOutline}
      onClose={hideTasks}
      className="tasks-dialog"
      footer={
        <Button variant="text" onClick={hideTasks}>
          {t("desktop.human_tasks.later")}
        </Button>
      }
    >
      <TasksPanel onDone={hideTasks} />
    </Dialog>
  );
}

/** The page as the tasks window: the preferences (theme, language) and only the tasks. */
export function TasksApp() {
  return (
    <ErrorBoundary>
      <AppStateProvider>
        <SnackbarProvider>
          <TasksWindow />
        </SnackbarProvider>
      </AppStateProvider>
    </ErrorBoundary>
  );
}

/**
 * The Electron app's tasks window (?tasks, spec_electron_038 EL-110): only
 * the tasks, with Later (closing it) and Open Blitz; it closes when the
 * last one is answered.
 */
export function TasksWindow() {
  const { tasks } = useTasks();
  const close = () => window.close();
  // After the last answer, long enough to read that it was sent.
  const done = useCallback(() => void setTimeout(close, 1500), []);
  return (
    <div className="tasks-window">
      <header className="tasks-window-head row">
        <Icon path={mdiAccountCheckOutline} />
        <span className="t-title-md spacer">{tn("desktop.human_tasks.title", Math.max(tasks.length, 1))}</span>
      </header>
      <div className="tasks-window-body">
        <TasksPanel onDone={done} />
      </div>
      <footer className="tasks-window-foot row">
        {canOpenApp() && (
          <Button variant="text" icon={mdiOpenInApp} onClick={() => void openApp().then(close)}>
            {t("desktop.human_tasks.open_app")}
          </Button>
        )}
        <span className="spacer" />
        <Button variant="text" onClick={close}>
          {t("desktop.human_tasks.later")}
        </Button>
      </footer>
    </div>
  );
}
