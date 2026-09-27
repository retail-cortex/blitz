import { useCallback, useEffect, useRef, useState } from "react";
import { mdiCalendarClock, mdiChatOutline, mdiClose, mdiCodeBraces, mdiDotsVertical, mdiFileCompare, mdiFileSearchOutline, mdiFileTreeOutline, mdiPencilOutline, mdiTuneVariant } from "@mdi/js";
import { workspaces } from "./api";
import { Changes } from "./Changes";
import { Conversation } from "./Conversation";
import { configChangedEvent, filesTouchedEvent, goToFileEvent, openFileEvent, viewEvent, type ConfigChangedDetail, type OpenFileDetail, type ViewDetail } from "./events";
import { EditorPane } from "./files/EditorPane";
import { FilesShelf } from "./files/FilesShelf";
import { GoToFile } from "./files/GoToFile";
import { FileLinksProvider } from "./files/links";
import { reportUnsaved } from "./files/unsaved";
import { useEditor } from "./files/useEditor";
import { message } from "./errors";
import type { GetSettingsResponse } from "./gen/blitz/v1/workspace_pb";
import { workspaceColor } from "./palette";
import { displayName, type WorkspacePrefs } from "./prefs";
import { RunSettings } from "./RunSettings";
import { useApp } from "./state";
import { t } from "./i18n";
import { IconButton, Menu, Segmented } from "./ui/controls";
import { Workers } from "./Workers";

type View = "chat" | "changes" | "workers";

/**
 * One open workspace: its top bar, the chat, changes or workers view, and
 * the run settings panel. Kept mounted while another is shown, so a turn
 * keeps streaming.
 */
export function Workspace({ ws, visible, onEdit, onClose }: { ws: WorkspacePrefs; visible: boolean; onEdit: () => void; onClose: () => void }) {
  const { prefs, update, theme } = useApp();
  const [view, setView] = useState<View>("chat");
  const [settings, setSettings] = useState<GetSettingsResponse>();
  const [modelProblem, setModelProblem] = useState("");
  const [settingsError, setSettingsError] = useState("");
  const dir = ws.dir;

  const refreshSettings = useCallback(async () => {
    try {
      const [s, m] = await Promise.all([workspaces.getSettings({ workspace: dir }), workspaces.getModel({ workspace: dir })]);
      setSettings(s);
      setModelProblem(m.unavailable);
      setSettingsError("");
    } catch (e) {
      setSettingsError(message(e));
    }
  }, [dir]);
  useEffect(() => {
    refreshSettings();
  }, [refreshSettings]);

  // Keys and providers set in the settings: is the model usable now?
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<ConfigChangedDetail>).detail;
      if (d.dir === "" || d.dir === dir) refreshSettings();
    };
    window.addEventListener(configChangedEvent, f);
    return () => window.removeEventListener(configChangedEvent, f);
  }, [dir, refreshSettings]);

  // The command palette can switch the view.
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<ViewDetail>).detail;
      if (d.dir === dir) setView(d.view);
    };
    window.addEventListener(viewEvent, f);
    return () => window.removeEventListener(viewEvent, f);
  }, [dir]);

  // Files (spec_files_029): the shelf, the editor, Go to file.
  const editor = useEditor(dir);
  const [editorShown, setEditorShown] = useState(true);
  const [goTo, setGoTo] = useState(false);
  const [reveal, setReveal] = useState<{ path: string } | null>(null);
  const [touched, setTouched] = useState(0);
  const editorRef = useRef(editor);
  editorRef.current = editor;
  const showsFiles = prefs.files || (editorShown && editor.tabs.length > 0);

  const open = useCallback((path: string, line?: number, column?: number) => {
    editorRef.current.open(path, line, column);
    setEditorShown(true);
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

  const editorWidth = prefs.editor_width || Math.max(420, Math.round(window.innerWidth * 0.4));

  const name = displayName(ws);
  return (
    <section className="workspace" hidden={!visible} aria-label={name} style={{ ["--ws-color" as string]: workspaceColor(ws.color, theme) }}>
      <header className="topbar drag-region">
        <span className="ws-dot" />
        <div className="topbar-title">
          <button className="title-button no-drag" onClick={onEdit} title={t("desktop.ws.edit_title")}>
            <span className="t-title-lg ellipsis">{name}</span>
            <IconButtonGlyph />
          </button>
          <span className="t-body-sm muted ellipsis" title={dir}>
            {ws.description || dir}
          </span>
        </div>
        <div className="no-drag">
          <Segmented<View>
            label={t("desktop.ws.view")}
            small
            value={view}
            onChange={setView}
            options={[
              { value: "chat", label: t("desktop.view.chat"), icon: mdiChatOutline },
              { value: "changes", label: t("desktop.view.changes"), icon: mdiFileCompare },
              { value: "workers", label: t("desktop.view.workers"), icon: mdiCalendarClock },
            ]}
          />
        </div>
        <div className="topbar-actions no-drag">
          <IconButton icon={mdiFileTreeOutline} label={t("desktop.files.title")} selected={prefs.files} onClick={() => update((p) => ({ ...p, files: !p.files }))} />
          <IconButton icon={mdiFileSearchOutline} label={t("desktop.files.go_to")} onClick={() => setGoTo(true)} />
          {editor.tabs.length > 0 && !editorShown && <IconButton icon={mdiCodeBraces} label={t("desktop.files.show_editor")} onClick={() => setEditorShown(true)} />}
          <IconButton icon={mdiTuneVariant} label={t("desktop.run_settings")} selected={prefs.run_settings} onClick={() => update((p) => ({ ...p, run_settings: !p.run_settings }))} />
          <Menu
            placement="down end"
            trigger={(p) => <IconButton icon={mdiDotsVertical} label={t("desktop.more")} {...p} />}
            items={[
              { label: t("desktop.edit_details"), icon: mdiPencilOutline, onSelect: onEdit },
              { label: t("desktop.ws.close"), icon: mdiClose, onSelect: onClose },
            ]}
          />
        </div>
      </header>
      <div className="workspace-body">
        {prefs.files && (
          <FilesShelf
            dir={dir}
            showHidden={prefs.show_hidden}
            onToggleHidden={() => update((p) => ({ ...p, show_hidden: !p.show_hidden }))}
            active={editorShown ? editor.active : null}
            refresh={touched}
            reveal={reveal}
            onOpen={(p) => {
              open(p);
              // Narrow, the shelf floats over the editor: make way.
              if (window.matchMedia("(max-width: 1100px)").matches) update((x) => ({ ...x, files: false }));
            }}
            onMoved={editor.moved}
            onClose={() => update((p) => ({ ...p, files: false }))}
          />
        )}
        <div className="workspace-view">
          <div hidden={view !== "chat"} className="view-fill">
            <FileLinksProvider dir={dir} refresh={touched}>
            <Conversation
              dir={dir}
              name={name}
              visible={visible && view === "chat"}
              settings={settings}
              modelProblem={modelProblem}
              onSettingsChanged={refreshSettings}
              onOpenView={setView}
            />
            </FileLinksProvider>
          </div>
          {view === "changes" && <Changes dir={dir} />}
          {view === "workers" && <Workers dir={dir} />}
        </div>
        {editorShown && editor.tabs.length > 0 && (
          <EditorPane
            model={editor}
            width={editorWidth}
            onResize={(w) => update((p) => ({ ...p, editor_width: w }))}
            onHide={() => setEditorShown(false)}
            onReveal={(path) => {
              if (!prefs.files) update((p) => ({ ...p, files: true }));
              setReveal({ path });
            }}
          />
        )}
        {prefs.run_settings && <RunSettings dir={dir} settings={settings} error={settingsError} onChanged={refreshSettings} />}
      </div>
      {goTo && <GoToFile dir={dir} onOpen={open} onClose={() => setGoTo(false)} />}
    </section>
  );
}

// A small pencil that shows on hover beside the workspace's name.
function IconButtonGlyph() {
  return (
    <svg className="icon sm title-edit" viewBox="0 0 24 24" aria-hidden="true">
      <path d={mdiPencilOutline} />
    </svg>
  );
}
