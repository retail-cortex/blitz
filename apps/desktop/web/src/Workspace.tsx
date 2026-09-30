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

import { useCallback, useEffect, useRef, useState } from "react";
import { useWorkspaceSettings } from "./workspaceSettings";
import { useWorkerFailures } from "./workerFailures";
import { InboxButton } from "./RunsInbox";
import { mdiCalendarClock, mdiCodeBraces, mdiCogOutline, mdiFileCompare, mdiFileSearchOutline, mdiFileTreeOutline, mdiTuneVariant } from "@mdi/js";
import { Changes } from "./Changes";
import { Conversation } from "./Conversation";
import { filesTouchedEvent, goToFileEvent, openFileEvent, viewEvent, type OpenFileDetail, type ViewDetail } from "./events";
import { EditorPane } from "./files/EditorPane";
import { FilesShelf } from "./files/FilesShelf";
import { GoToFile } from "./files/GoToFile";
import { FileLinksProvider } from "./files/links";
import { reportUnsaved } from "./files/unsaved";
import { useEditor } from "./files/useEditor";
import { workspaceColor } from "./palette";
import { displayName, type WorkspacePrefs } from "./prefs";
import { waiting } from "./project";
import { ProjectDialog } from "./ProjectDialog";
import { RunSettings } from "./RunSettings";
import { useApp } from "./state";
import { publishStatus } from "./status";
import { t } from "./i18n";
import { IconButton, Segmented } from "./ui/controls";
import { filesFloat, panelWidth, runSettingsFloat, useWindowWidth } from "./ui/layout";
import { ResizeHandle } from "./ui/ResizeHandle";
import { Brand, WorkspaceSwitcher } from "./WorkspaceSwitcher";
import { Workers } from "./Workers";

type View = "editor" | "changes" | "workers";

/**
 * One open workspace, laid out like an IDE: the top bar (the workspace
 * dropdown, the view, actions, settings), the Files shelf on the left, the
 * editor (or the Changes or Workers view) in the middle, the chat on the
 * right, and the run settings panel beside it. Kept mounted while another
 * is shown, so a turn keeps streaming.
 */
export function Workspace({
  ws,
  visible,
  onOpenWorkspace,
  onEditWorkspace,
  onCloseWorkspace,
  onSettings,
}: {
  ws: WorkspacePrefs;
  visible: boolean;
  onOpenWorkspace: () => void;
  onEditWorkspace: (dir: string) => void;
  onCloseWorkspace: (dir: string) => void;
  onSettings: () => void;
}) {
  const { prefs, update, theme } = useApp();
  const [view, setView] = useState<View>("editor");
  const dir = ws.dir;
  const { settings, modelProblem, settingsError, project, reviewing, setReviewing, refreshSettings } = useWorkspaceSettings(dir);

  // Showing the workers sees their failed runs (BL-WK-11).
  useEffect(() => {
    if (visible && view === "workers") update((p) => ({ ...p, workspaces: p.workspaces.map((w) => (w.dir === dir ? { ...w, workers_seen: Date.now() } : w)) }));
  }, [visible, view, dir, update]);
  const failed = useWorkerFailures()[dir] ?? 0;
  useEffect(() => publishStatus(dir, { settings }), [dir, settings]);

  // The command palette can switch the view.
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<ViewDetail>).detail;
      // The chat is always shown, beside the editor.
      if (d.dir === dir && d.view !== "chat") setView(d.view);
    };
    window.addEventListener(viewEvent, f);
    return () => window.removeEventListener(viewEvent, f);
  }, [dir]);

  // Files (spec_files_029): the shelf, the editor, Go to file.
  const editor = useEditor(dir);
  const [goTo, setGoTo] = useState(false);
  const [reveal, setReveal] = useState<{ path: string } | null>(null);
  const [touched, setTouched] = useState(0);
  const editorRef = useRef(editor);
  editorRef.current = editor;
  const showsFiles = prefs.files || editor.tabs.length > 0;

  const open = useCallback((path: string, line?: number, column?: number) => {
    editorRef.current.open(path, line, column);
    setView("editor");
  }, []);

  useEffect(() => {
    const onOpen = (e: Event) => {
      const d = (e as CustomEvent<OpenFileDetail>).detail;
      if (d.dir === dir) open(d.path, d.line, d.column);
    };
    const onGoTo = (e: Event) => (e as CustomEvent<{ dir: string }>).detail.dir === dir && setGoTo(true);
    // A tool ran or a turn ended: look again soon (tools come in bursts).
    let timer: ReturnType<typeof setTimeout> | undefined;
    const onTouched = (e: Event) => {
      if ((e as CustomEvent<{ dir: string }>).detail.dir !== dir) return;
      clearTimeout(timer);
      timer = setTimeout(() => setTouched((n) => n + 1), 300);
    };
    window.addEventListener(openFileEvent, onOpen);
    window.addEventListener(goToFileEvent, onGoTo);
    window.addEventListener(filesTouchedEvent, onTouched);
    return () => {
      clearTimeout(timer);
      window.removeEventListener(openFileEvent, onOpen);
      window.removeEventListener(goToFileEvent, onGoTo);
      window.removeEventListener(filesTouchedEvent, onTouched);
    };
  }, [dir, open]);

  // While files are shown, look for changes made elsewhere every 5 seconds.
  useEffect(() => {
    if (!visible || !showsFiles) return;
    const timer = setInterval(() => setTouched((n) => n + 1), 5000);
    return () => clearInterval(timer);
  }, [visible, showsFiles]);
  useEffect(() => {
    if (touched) void editorRef.current.check();
  }, [touched]);

  // ⌘P: Go to file.
  useEffect(() => {
    if (!visible) return;
    const key = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.shiftKey && e.key.toLowerCase() === "p") {
        e.preventDefault();
        setGoTo(true);
      }
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [visible]);

  useEffect(() => reportUnsaved(dir, editor.dirtyCount), [dir, editor.dirtyCount]);
  useEffect(() => () => reportUnsaved(dir, 0), [dir]);

  // Panel widths follow the window (resized, or moved to another display):
  // a width saved on a larger display is kept to what fits this one.
  const win = useWindowWidth();
  const filesWidth = panelWidth(prefs.files_width, 272, 200, Math.min(560, win - 600));
  const runWidth = panelWidth(prefs.run_settings_width, 420, 340, Math.min(720, win - 420));
  // The chat leaves the editor 360 px beside the panels that sit inline
  // (narrower windows float the run settings and the shelf over it).
  const chatMax = (w: number) => w - (prefs.files && w > filesFloat ? filesWidth : 56) - (prefs.run_settings && w > runSettingsFloat ? runWidth : 0) - 400;
  const chatWidth = panelWidth(prefs.chat_width, Math.max(380, Math.min(520, Math.round(win * 0.32))), 320, chatMax(win));
  const chatCenter = view === "editor" && editor.tabs.length === 0;
  const revealInTree = (path: string) => {
    if (!prefs.files) update((p) => ({ ...p, files: true }));
    setReveal({ path });
  };

  const name = displayName(ws);
  return (
    <section className="workspace" hidden={!visible} aria-label={name} style={{ ["--ws-color" as string]: workspaceColor(ws.color, theme) }}>
      <header className="topbar drag-region">
        <Brand />
        <WorkspaceSwitcher onOpen={onOpenWorkspace} onClose={onCloseWorkspace} onEdit={onEditWorkspace} />
        <div className="no-drag">
          <Segmented<View>
            label={t("desktop.ws.view")}
            small
            value={view}
            onChange={setView}
            options={[
              { value: "editor", label: t("desktop.view.editor"), icon: mdiCodeBraces },
              { value: "changes", label: t("desktop.view.changes"), icon: mdiFileCompare },
              { value: "workers", label: failed > 0 ? `${t("desktop.view.workers")} (${failed})` : t("desktop.view.workers"), icon: mdiCalendarClock },
            ]}
          />
        </div>
        <span className="spacer" />
        <div className="topbar-actions no-drag">
          <InboxButton />
          <IconButton icon={mdiFileSearchOutline} label={t("desktop.files.go_to")} onClick={() => setGoTo(true)} />
          <IconButton icon={mdiTuneVariant} label={t("desktop.run_settings")} data-toggles="run-settings" selected={prefs.run_settings} onClick={() => update((p) => ({ ...p, run_settings: !p.run_settings }))} />
          <IconButton icon={mdiCogOutline} label={t("desktop.settings")} onClick={onSettings} />
        </div>
      </header>
      <div className="workspace-body">
        {prefs.files ? (
          <FilesShelf
            dir={dir}
            width={filesWidth}
            onResize={(w) => update((p) => ({ ...p, files_width: w }))}
            showHidden={prefs.show_hidden}
            onToggleHidden={() => update((p) => ({ ...p, show_hidden: !p.show_hidden }))}
            active={view === "editor" ? editor.active : null}
            refresh={touched}
            reveal={reveal}
            onOpen={(p) => {
              open(p);
              // Narrow, the shelf floats over the editor: make way.
              if (window.matchMedia(`(max-width: ${filesFloat}px)`).matches) update((x) => ({ ...x, files: false }));
            }}
            onMoved={editor.moved}
            onClose={() => update((p) => ({ ...p, files: false }))}
          />
        ) : (
          <nav className="files-rail" aria-label={t("desktop.files.title")}>
            <IconButton icon={mdiFileTreeOutline} label={t("desktop.files.show")} onClick={() => update((p) => ({ ...p, files: true }))} />
            <IconButton icon={mdiFileSearchOutline} label={t("desktop.files.go_to")} onClick={() => setGoTo(true)} />
          </nav>
        )}
        {/* With no file open, the chat takes the middle (center stage). */}
        {!chatCenter && (
          <div className="workspace-center">
            {view === "editor" && <EditorPane model={editor} onReveal={revealInTree} onCursor={(cursor) => publishStatus(dir, { cursor })} />}
            {view === "changes" && <Changes dir={dir} />}
            {view === "workers" && <Workers dir={dir} />}
          </div>
        )}
        <aside className={`chat-panel ${chatCenter ? "center" : ""}`} style={chatCenter ? undefined : { width: chatWidth }} aria-label={t("desktop.view.chat")}>
          {!chatCenter && (
            <ResizeHandle
              width={chatWidth}
              edge="left"
              min={320}
              max={() => chatMax(window.innerWidth)}
              label={t("desktop.chat.resize")}
              onResize={(w) => update((p) => ({ ...p, chat_width: w }))}
            />
          )}
          <FileLinksProvider dir={dir} refresh={touched}>
            <Conversation
              dir={dir}
              name={name}
              visible={visible}
              settings={settings}
              modelProblem={modelProblem}
              projectWaiting={waiting(project)}
              onReviewProject={() => setReviewing(true)}
              onSettingsChanged={refreshSettings}
              onOpenView={setView}
            />
          </FileLinksProvider>
        </aside>
        {prefs.run_settings && <RunSettings dir={dir} settings={settings} error={settingsError} onChanged={refreshSettings} width={runWidth} onResize={(w) => update((p) => ({ ...p, run_settings_width: w }))} />}
      </div>
      {goTo && <GoToFile dir={dir} onOpen={open} onClose={() => setGoTo(false)} />}
      {reviewing && visible && <ProjectDialog dir={dir} onClose={() => setReviewing(false)} onDecided={refreshSettings} />}
    </section>
  );
}
