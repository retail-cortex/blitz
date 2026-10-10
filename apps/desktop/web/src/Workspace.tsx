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

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { SandboxNotice } from "./SandboxNotice";
import { useWorkspaceSettings } from "./workspaceSettings";
import { mdiMagnify, mdiFileTreeOutline } from "@mdi/js";
import { ChangesDialog, type ChangesSource } from "./Changes";
import { CommitDialog } from "./CommitDialog";
import { Conversation } from "./Conversation";
import { filesTouchedEvent, goToFile, openFileEvent, revealInTreeEvent, viewEvent, type OpenFileDetail, type ViewDetail } from "./events";
import { EditorPane } from "./files/EditorPane";
import { FilesShelf } from "./files/FilesShelf";
import { FileLinksProvider } from "./files/links";
import { reportUnsaved } from "./files/unsaved";
import { useEditor } from "./files/useEditor";
import { renderMarkdown } from "./files/languageUi";
import { workspaceColor } from "./palette";
import { displayName, type WorkspacePrefs } from "./prefs";
import { waiting } from "./project";
import { ProjectDialog } from "./ProjectDialog";
import { useApp } from "./state";
import { publishStatus } from "./status";
import { t } from "./i18n";
import { IconButton, useSnackbar } from "./ui/controls";
import { panelWidth, useWindowWidth } from "./ui/layout";
import { ResizeHandle } from "./ui/ResizeHandle";
import { Brand, MenuBar } from "./MenuBar";
import { intent as newIntent, type ItemIntent } from "./intent";
import type { Section } from "./SettingsDialog";
import { WorkerModal } from "./Workers";
import { AgentEditor } from "./AgentEditor";
import { AgentScope } from "./gen/blitz/v1/workspace_pb";

/**
 * One open workspace, laid out like an IDE: the top bar (the brand over
 * the Files shelf, then the menus from the center panel's left edge), the
 * Files shelf on the left, the
 * editor in the middle (Changes, an agent or a worker open over it, in a
 * dialog, from the menus), the chat on the
 * right, and the run settings panel beside it. Kept mounted while another
 * is shown, so a turn keeps streaming.
 */
export function Workspace({
  ws,
  visible,
  onOpenWorkspace,
  onCloseWorkspace,
  onSettings,
  onSearch,
}: {
  ws: WorkspacePrefs;
  visible: boolean;
  onOpenWorkspace: () => void;
  onCloseWorkspace: (dir: string) => void;
  /** Opens the Settings dialog on a section (for the settings file, this workspace's). */
  onSettings: (section: Section, scope?: string) => void;
  /** Opens the command palette (the top bar's search). */
  onSearch: () => void;
}) {
  const { prefs, update, theme } = useApp();
  // What's open over the editor: the changes (this session's or git's),
  // an agent or a worker, as the menus asked.
  const [modal, setModal] = useState<{ kind: "changes"; source: ChangesSource } | { kind: "agent" | "worker"; intent: ItemIntent } | { kind: "commit" } | null>(null);
  const dir = ws.dir;
  const { settings, modelProblem, settingsError, settingsReason, project, reviewing, setReviewing, refreshSettings } = useWorkspaceSettings(dir);

  // Showing a worker sees the workers' failed runs (BL-WK-11).
  const seeWorkers = useCallback(() => update((p) => ({ ...p, workspaces: p.workspaces.map((w) => (w.dir === dir ? { ...w, workers_seen: Date.now() } : w)) })), [dir, update]);
  useEffect(() => {
    if (visible && modal?.kind === "worker") seeWorkers();
  }, [visible, modal, seeWorkers]);
  useEffect(() => publishStatus(dir, { settings }), [dir, settings]);

  // The command palette, the status bar and the chat ask for a view: the
  // editor (what's over it closes), or the changes or workers over it.
  const showView = useCallback((v: ViewDetail["view"]) => {
    if (v === "changes") setModal({ kind: "changes", source: "session" });
    else if (v === "workers") setModal({ kind: "worker", intent: newIntent("show") });
    else setModal(null);
  }, []);
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<ViewDetail>).detail;
      if (d.dir === dir) showView(d.view);
    };
    window.addEventListener(viewEvent, f);
    return () => window.removeEventListener(viewEvent, f);
  }, [dir, showView]);

  // Files (spec_files_029): the shelf, the editor, Go to file.
  const snack = useSnackbar();
  const editor = useEditor(dir, {
    // A definition outside the workspace (the standard library, a module) can't open here.
    outside: (loc) => snack(t("desktop.files.definition_outside", { path: loc.path, line: loc.line })),
    markdown: renderMarkdown,
  });
  const [reveal, setReveal] = useState<{ path: string } | null>(null);
  const [touched, setTouched] = useState(0);
  const editorRef = useRef(editor);
  editorRef.current = editor;
  const showsFiles = prefs.files || editor.tabs.length > 0;

  const open = useCallback((path: string, line?: number, column?: number, find?: string[]) => {
    editorRef.current.open(path, line, column, find);
    setModal(null);
  }, []);

  useEffect(() => {
    const onOpen = (e: Event) => {
      const d = (e as CustomEvent<OpenFileDetail>).detail;
      if (d.dir === dir) open(d.path, d.line, d.column, d.find);
    };
    const onReveal = (e: Event) => {
      const d = (e as CustomEvent<{ dir: string; path: string }>).detail;
      if (d.dir === dir) revealRef.current(d.path);
    };
    // A tool ran or a turn ended: look again soon (tools come in bursts).
    let timer: ReturnType<typeof setTimeout> | undefined;
    const onTouched = (e: Event) => {
      if ((e as CustomEvent<{ dir: string }>).detail.dir !== dir) return;
      clearTimeout(timer);
      timer = setTimeout(() => setTouched((n) => n + 1), 300);
    };
    window.addEventListener(openFileEvent, onOpen);
    window.addEventListener(revealInTreeEvent, onReveal);
    window.addEventListener(filesTouchedEvent, onTouched);
    return () => {
      clearTimeout(timer);
      window.removeEventListener(openFileEvent, onOpen);
      window.removeEventListener(revealInTreeEvent, onReveal);
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


  useEffect(() => reportUnsaved(dir, editor.dirtyCount), [dir, editor.dirtyCount]);
  useEffect(() => () => reportUnsaved(dir, 0), [dir]);

  // Panel widths follow the window (resized, or moved to another display):
  // a width saved on a larger display is kept to what fits this one.
  const win = useWindowWidth();
  const filesWidth = panelWidth(prefs.files_width, 272, 200, Math.min(560, win - 600));
  // The chat leaves the editor 400 px beside the shelf (or the rail). The
  // shelf never floats over the others: in a narrow window they all shrink
  // to their minimums, and it can be minimized to the rail.
  const chatMax = (w: number) => w - (prefs.files ? filesWidth : 56) - 400;
  const chatWidth = panelWidth(prefs.chat_width, Math.max(380, Math.min(520, Math.round(win * 0.32))), 320, chatMax(win));
  const chatCenter = editor.tabs.length === 0;
  const revealInTree = (path: string) => {
    if (!prefs.files) update((p) => ({ ...p, files: true }));
    setReveal({ path });
  };
  const revealRef = useRef(revealInTree);
  revealRef.current = revealInTree;

  // The menus start at the center panel's left edge (the editor's, or the
  // chat's at center stage), following the Files shelf as it opens,
  // closes or is resized; the brand takes the room before it.
  const bodyRef = useRef<HTMLDivElement>(null);
  const [lead, setLead] = useState(0);
  useLayoutEffect(() => {
    const body = bodyRef.current;
    if (!body || !visible) return;
    const measure = () => {
      const center = body.querySelector<HTMLElement>(":scope > .workspace-center, :scope > .chat-panel.center");
      if (center) setLead(Math.round(center.getBoundingClientRect().left));
    };
    measure();
    const seen = new ResizeObserver(measure);
    seen.observe(body);
    for (const el of Array.from(body.children)) seen.observe(el);
    return () => seen.disconnect();
  }, [visible, chatCenter, prefs.files]);

  const name = displayName(ws);
  return (
    <section className="workspace" hidden={!visible} aria-label={name} style={{ ["--ws-color" as string]: workspaceColor(ws.color, theme) }}>
      <header className="topbar drag-region">
        <div className={`topbar-lead ${lead > 0 && lead < 180 ? "narrow" : ""}`} style={lead > 0 ? { width: lead } : undefined}>
          <Brand />
        </div>
        <MenuBar
          dir={dir}
          onChanges={(source) => setModal({ kind: "changes", source })}
          onCommit={() => setModal({ kind: "commit" })}
          onAgent={(intent) => setModal({ kind: "agent", intent })}
          onWorker={(intent) => setModal({ kind: "worker", intent })}
          onSearch={onSearch}
          onSettings={onSettings}
          onOpenWorkspace={onOpenWorkspace}
          onCloseWorkspace={onCloseWorkspace}
        />
      </header>
      {settingsReason === "SANDBOX_UNAVAILABLE" && <SandboxNotice error={settingsError} onFixed={refreshSettings} />}
      <div className="workspace-body" ref={bodyRef}>
        {prefs.files ? (
          <FilesShelf
            dir={dir}
            width={filesWidth}
            onResize={(w) => update((p) => ({ ...p, files_width: w }))}
            showHidden={prefs.show_hidden}
            onToggleHidden={() => update((p) => ({ ...p, show_hidden: !p.show_hidden }))}
            onCommit={() => setModal({ kind: "commit" })}
            active={editor.active}
            refresh={touched}
            reveal={reveal}
            onOpen={open}
            onMoved={editor.moved}
            onChanged={() => setTouched((n) => n + 1)}
            onClose={() => update((p) => ({ ...p, files: false }))}
          />
        ) : (
          <nav className="files-rail" aria-label={t("desktop.files.title")}>
            <IconButton icon={mdiFileTreeOutline} label={t("desktop.files.show")} onClick={() => update((p) => ({ ...p, files: true }))} />
            <IconButton icon={mdiMagnify} label={t("desktop.files.go_to")} onClick={() => goToFile({ dir })} />
          </nav>
        )}
        {/* With no file open, the chat takes the middle (center stage). */}
        {!chatCenter && (
          <div className="workspace-center">
            <EditorPane model={editor} onReveal={revealInTree} onCursor={(cursor) => publishStatus(dir, { cursor })} />
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
              onOpenView={showView}
            />
          </FileLinksProvider>
        </aside>
      </div>
      {visible && modal?.kind === "commit" && <CommitDialog dir={dir} onClose={() => setModal(null)} onCommitted={() => setTouched((n) => n + 1)} />}
      {visible && modal?.kind === "changes" && <ChangesDialog dir={dir} initial={modal.source} onClose={() => setModal(null)} />}
      {visible && modal?.kind === "agent" && <AgentEditor key={modal.intent.n} workspace={dir} scope={AgentScope.WORKSPACE} intent={modal.intent} dialog={{ onClose: () => setModal(null) }} />}
      {visible && modal?.kind === "worker" && <WorkerModal key={modal.intent.n} dir={dir} intent={modal.intent} onClose={() => setModal(null)} />}
      {reviewing && visible && <ProjectDialog dir={dir} onClose={() => setReviewing(false)} onDecided={refreshSettings} />}
    </section>
  );
}
