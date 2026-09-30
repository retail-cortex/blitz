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

import { useEffect, useState } from "react";
import { Conversation } from "./Conversation";
import { ErrorBoundary } from "./ErrorBoundary";
import { addToContext, compose, filesTouchedEvent, openFileEvent, type OpenFileDetail } from "./events";
import { FileLinksProvider } from "./files/links";
import { editorConfig, onEditorMessage, toEditor } from "./host";
import { t } from "./i18n";
import { waiting } from "./project";
import { ProjectDialog } from "./ProjectDialog";
import { AppStateProvider, useApp } from "./state";
import { SnackbarProvider } from "./ui/controls";
import { useWorkspaceSettings } from "./workspaceSettings";

/**
 * The page inside an editor (spec_parity_027 PAR-INT-02): one workspace's
 * conversation, the rest being the editor's. Files open in the editor,
 * approvals' diffs show in its diff view, and the editor's selection and
 * files come into the composer.
 */
export function EditorPanel() {
  return (
    <ErrorBoundary>
      <AppStateProvider>
        <SnackbarProvider>
          <Panel />
        </SnackbarProvider>
      </AppStateProvider>
    </ErrorBoundary>
  );
}

function Panel() {
  const dir = editorConfig()!.dir;
  const { loaded } = useApp();
  const { settings, modelProblem, project, reviewing, setReviewing, refreshSettings } = useWorkspaceSettings(dir);
  const [touched, setTouched] = useState(0);

  // The editor's selection and files come into the composer.
  useEffect(() => {
    const stop = onEditorMessage((m) => {
      if (m.type === "compose") compose({ dir, text: m.text, append: true });
      else addToContext({ dir, path: m.path });
    });
    toEditor({ type: "ready" }); // what the extension held back comes now
    return stop;
  }, [dir]);
  // Links to files in the chat open them in the editor; tools that ran
  // refresh which paths are files.
  useEffect(() => {
    const open = (e: Event) => {
      const d = (e as CustomEvent<OpenFileDetail>).detail;
      if (d.dir === dir) toEditor({ type: "open", path: d.path, line: d.line, column: d.column });
    };
    const touch = (e: Event) => (e as CustomEvent<{ dir: string }>).detail.dir === dir && setTouched((n) => n + 1);
    window.addEventListener(openFileEvent, open);
    window.addEventListener(filesTouchedEvent, touch);
    return () => {
      window.removeEventListener(openFileEvent, open);
      window.removeEventListener(filesTouchedEvent, touch);
    };
  }, [dir]);

  if (!loaded) return <p className="muted editor-panel-loading">{t("desktop.app.starting")}</p>;
  const name = dir.slice(dir.lastIndexOf("/") + 1) || dir;
  return (
    <main className="editor-panel" aria-label={t("desktop.view.chat")}>
      <FileLinksProvider dir={dir} refresh={touched}>
        <Conversation
          dir={dir}
          name={name}
          visible
          settings={settings}
          modelProblem={modelProblem}
          projectWaiting={waiting(project)}
          onReviewProject={() => setReviewing(true)}
          onSettingsChanged={refreshSettings}
          onOpenView={() => {}}
        />
      </FileLinksProvider>
      {reviewing && <ProjectDialog dir={dir} onClose={() => setReviewing(false)} onDecided={refreshSettings} />}
    </main>
  );
}
