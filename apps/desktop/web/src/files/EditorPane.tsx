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

import { useEffect, useRef, useState } from "react";
import { mdiAlertCircleOutline, mdiAt, mdiChevronRight, mdiClose, mdiContentSave, mdiFilePdfBox, mdiLockOutline, mdiMessagePlusOutline, mdiWrap } from "@mdi/js";
import { files } from "../api";
import { inApp, printPDF } from "../desktop";
import { message } from "../errors";
import { addToContext, openFile } from "../events";
import { EditorView } from "@codemirror/view";
import { t, tn } from "../i18n";
import { Button, Dialog, Icon, IconButton, Segmented, useSnackbar } from "../ui/controls";
import { Preview } from "./Preview";
import { defaultView, previewKind, previewOnly, type View } from "./previewKind";
import type { Cursor } from "../status";
import { goToLine, languageOf, setWrap } from "./codemirror";
import { fileIcon } from "./icons";
import type { EditorModel, Tab } from "./useEditor";
import { useAdvanced, useApp } from "../state";
import { paperFor, pdfPath, printablePage } from "./printPage";
import { drawForPrint } from "../mermaid";

/**
 * The editor beside the conversation (spec_files_029 §6): the open files'
 * tabs, the active one's path, notes about it, and CodeMirror. One view
 * shows each tab's own state in turn.
 */
export function EditorPane({ model, onReveal, onCursor }: { model: EditorModel; onReveal: (path: string) => void; onCursor?: (c: Cursor | undefined) => void }) {
  const advanced = useAdvanced(); // the wrap toggle is advanced
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const shown = useRef<string | null>(null);
  const [closing, setClosing] = useState<Tab | null>(null);
  const tab = model.tabs.find((x) => x.path === model.active);
  const ready = tab && !tab.loading && !tab.error && !tab.binary && !tab.tooLarge;
  // Images and PDFs show as previews; Markdown and SVG can, and Markdown
  // does when it opens (FIL-54). views keeps each tab's choice.
  const kind = tab ? previewKind(tab.path) : null;
  const [views, setViews] = useState<ReadonlyMap<string, View>>(new Map());
  const atLine = !!tab && model.target?.path === tab.path && !!model.target.line;
  const shownAs = tab && (atLine ? "source" : (views.get(tab.path) ?? defaultView(kind, { atLine, empty: tab.size === 0 })));
  const showPreview = !!tab && !tab.loading && !tab.error && !!kind && (previewOnly(kind) || (ready && shownAs === "preview"));
  const setView = (path: string, v: View) => setViews((m) => (m.get(path) === v ? m : new Map(m).set(path, v)));

  // Keep the view a tab opened in (a jump to a line shows its source), and
  // forget closed tabs'.
  useEffect(() => {
    if (tab && ready && kind && !previewOnly(kind) && shownAs && views.get(tab.path) !== shownAs) setView(tab.path, shownAs);
  }, [tab, ready, kind, shownAs, views]);
  useEffect(() => {
    if ([...views.keys()].some((p) => !model.tabs.some((x) => x.path === p))) setViews((m) => new Map([...m].filter(([p]) => model.tabs.some((x) => x.path === p))));
  }, [model.tabs, views]);

  // ⌘⇧V / Ctrl+Shift+V: switch between the source and the preview, while
  // the pane is shown and has the focus (or nothing has: a click in the
  // preview); elsewhere, such as the composer, it still pastes plain text.
  const pane = useRef<HTMLElement>(null);
  const canToggle = !!tab && !!kind && !previewOnly(kind) && ready;

  // Export as PDF: the preview, printed by the app's web engine, written
  // beside the file; an existing PDF is replaced only when the user says.
  const snack = useSnackbar();
  const { prefs, theme } = useApp();
  const [exporting, setExporting] = useState(false);
  const [replacing, setReplacing] = useState<{ path: string; pdf: string; version: string } | null>(null);
  const exportPdf = async (path: string, pdf: string, version: string) => {
    setExporting(true);
    try {
      const markdown = await renderedPreview(pane.current);
      const html = await printablePage({
        markdown,
        docPath: path,
        title: path.slice(path.lastIndexOf("/") + 1),
        userCss: prefs.markdown_css_light,
        read: (p) => files.readPreview({ workspace: model.dir, path: p }),
        redraw: theme === "dark" ? drawForPrint : undefined,
      });
      const data = await printPDF(html, paperFor(navigator.language));
      await files.writeBinaryFile({ workspace: model.dir, path: pdf, data, version });
      snack(t("desktop.files.exported", { name: pdf.slice(pdf.lastIndexOf("/") + 1) }), {
        action: { label: t("desktop.files.open_pdf"), run: () => openFile({ dir: model.dir, path: pdf }) },
      });
    } catch (e) {
      snack(t("desktop.files.export_failed", { error: message(e) }), { error: true });
    } finally {
      setExporting(false);
    }
  };
  const startExport = async () => {
    if (!tab) return;
    if (shownAs !== "preview") setView(tab.path, "preview");
    const pdf = pdfPath(tab.path);
    try {
      const { versions } = await files.statFiles({ workspace: model.dir, paths: [pdf] });
      const version = versions[pdf] ?? "";
      if (version) setReplacing({ path: tab.path, pdf, version });
      else await exportPdf(tab.path, pdf, "");
    } catch (e) {
      snack(t("desktop.files.export_failed", { error: message(e) }), { error: true });
    }
  };
  useEffect(() => {
    if (!canToggle || !tab) return;
    const key = (e: KeyboardEvent) => {
      const p = pane.current;
      const focused = !!p && (p.contains(document.activeElement) || document.activeElement === document.body);
      if (focused && p.offsetParent !== null && (e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === "v") {
        e.preventDefault();
        setView(tab.path, shownAs === "preview" ? "source" : "preview");
      }
    };
    window.addEventListener("keydown", key, true);
    return () => window.removeEventListener("keydown", key, true);
  }, [canToggle, tab, shownAs]);

  // Where the cursor is, for the status bar.
  const cursorTo = useRef(onCursor);
  cursorTo.current = onCursor;
  const report = (v: EditorView) => {
    const path = shown.current;
    if (!path) return cursorTo.current?.(undefined);
    const sel = v.state.selection.main;
    const line = v.state.doc.lineAt(sel.head);
    cursorTo.current?.({ line: line.number, column: sel.head - line.from + 1, selected: sel.to - sel.from, language: languageOf(path)?.name ?? "" });
  };

  // One view, made once; every change is kept as its tab's state.
  useEffect(() => {
    const v = new EditorView({
      parent: host.current!,
      dispatchTransactions: (trs, v) => {
        v.update(trs);
        if (shown.current) model.keepState(shown.current, v.state);
        if (trs.some((tr) => tr.selection || tr.docChanged)) report(v);
      },
    });
    view.current = v;
    return () => {
      v.destroy();
      cursorTo.current?.(undefined);
    };
    // The model's functions read refs; the view lives as long as the pane.
  }, []);

  // Show the active tab's state (once it has loaded, and again when it's
  // reloaded); focus the editor when the tab changes, not on every render.
  const state = ready ? model.state(tab.path) : undefined;
  useEffect(() => {
    const v = view.current;
    if (!v || !state || !tab) return;
    if (v.state === state) return;
    const switched = shown.current !== tab.path;
    v.setState(state);
    shown.current = tab.path;
    setWrap(v, model.wrap);
    if (switched) v.focus();
  }, [state, tab, model.wrap]);

  useEffect(() => {
    const v = view.current;
    if (v && ready && !showPreview && shown.current === tab?.path) report(v);
    else cursorTo.current?.(undefined);
    // report reads refs.
  }, [state, ready, showPreview, tab?.path]);

  // Go to a line when asked (a link, Go to file).
  useEffect(() => {
    const v = view.current;
    if (!v || !state || model.target?.path !== tab?.path || !model.target?.line) return;
    goToLine(v, model.target.line, model.target.column);
    model.clearTarget();
  }, [state, tab?.path, model]);

  useEffect(() => {
    if (view.current && state) setWrap(view.current, model.wrap);
  }, [model.wrap, state]);

  const closeTab = (x: Tab) => (x.dirty ? setClosing(x) : model.close(x.path));

  return (
    <section className="editor-pane" ref={pane} aria-label={t("desktop.files.editor")}>
      <div className="editor-tabs" role="tablist">
        {model.tabs.map((x) => (
          <div
            key={x.path}
            role="tab"
            aria-selected={x.path === model.active}
            className={`editor-tab ${x.path === model.active ? "active" : ""}`}
            title={x.path}
            onClick={() => model.activate(x.path)}
            onAuxClick={(e) => e.button === 1 && closeTab(x)}
          >
            <Icon path={fileIcon(x.name)} size="sm" />
            <span className="ellipsis">{x.name}</span>
            {x.dirty && <span className="dirty-dot" aria-label={t("desktop.files.unsaved")} />}
            <IconButton
              icon={mdiClose}
              label={t("desktop.files.close_tab", { name: x.name })}
              small
              onClick={(e) => {
                e.stopPropagation();
                closeTab(x);
              }}
            />
          </div>
        ))}
        <span className="spacer" />
        {advanced && <IconButton icon={mdiWrap} label={t("desktop.files.wrap")} small selected={model.wrap} onClick={() => model.setWrap(!model.wrap)} />}
      </div>
      {tab && (
        <div className="editor-head">
          <nav className="crumbs ellipsis" aria-label={t("desktop.files.path")}>
            {tab.path.split("/").map((part, i, all) => (
              <span key={i} className="crumb">
                {i > 0 && <Icon path={mdiChevronRight} size="sm" />}
                <button className="link-button" onClick={() => onReveal(all.slice(0, i + 1).join("/"))}>
                  {part}
                </button>
              </span>
            ))}
          </nav>
          {tab.agentRule && (
            <span className="chip static small" title={t(`desktop.files.rule.${tab.agentRule}.detail`)}>
              <Icon path={mdiLockOutline} size="sm" /> {t(`desktop.files.rule.${tab.agentRule}`)}
            </span>
          )}
          {canToggle && (
            <Segmented<View>
              small
              label={t("desktop.files.view_as")}
              value={shownAs ?? "source"}
              onChange={(v) => setView(tab.path, v)}
              options={
                // The view a file opens in comes first: Markdown's preview.
                kind === "markdown"
                  ? [
                      { value: "preview", label: t("desktop.files.preview") },
                      { value: "source", label: t("desktop.files.source") },
                    ]
                  : [
                      { value: "source", label: t("desktop.files.source") },
                      { value: "preview", label: t("desktop.files.preview") },
                    ]
              }
            />
          )}
          {kind === "markdown" && ready && inApp() && (
            <IconButton icon={mdiFilePdfBox} label={t("desktop.files.export_pdf")} small disabled={exporting} onClick={() => void startExport()} />
          )}
          {tab.agentRule !== "blocked" && (
            <>
              <IconButton icon={mdiAt} label={t("desktop.files.add_to_context")} small onClick={() => addToContext({ dir: model.dir, path: tab.path })} />
              <IconButton icon={mdiMessagePlusOutline} label={t("desktop.files.new_chat_about")} small onClick={() => addToContext({ dir: model.dir, path: tab.path, fresh: true })} />
            </>
          )}
          <Button small variant={tab.dirty ? "tonal" : "text"} icon={mdiContentSave} disabled={!ready || !tab.dirty} onClick={() => model.save(tab.path)}>
            {t("desktop.files.save")}
          </Button>
        </div>
      )}
      {tab && <Bars tab={tab} model={model} />}
      <div className="editor-body">
        <div className="editor-host" ref={host} hidden={!ready || showPreview} />
        {tab?.loading && <p className="editor-note muted">{t("desktop.checking")}</p>}
        {tab?.error && <p className="editor-note error-text">{tab.error}</p>}
        {showPreview && tab && <Preview dir={model.dir} path={tab.path} kind={kind} text={ready ? model.state(tab.path)?.doc.toString() : undefined} />}
        {!showPreview && tab?.binary && <p className="editor-note muted">{t("desktop.files.binary", { size: formatSize(tab.size) })}</p>}
        {!showPreview && tab?.tooLarge && <p className="editor-note muted">{t("desktop.files.too_large", { size: formatSize(tab.size) })}</p>}
      </div>
      {closing && (
        <UnsavedDialog
          names={[closing.name]}
          onSave={async () => {
            if (await model.save(closing.path)) model.close(closing.path);
            setClosing(null);
          }}
          onDiscard={() => {
            model.close(closing.path);
            setClosing(null);
          }}
          onCancel={() => setClosing(null)}
        />
      )}
      {replacing && (
        <Dialog
          title={t("desktop.files.replace_pdf", { name: replacing.pdf.slice(replacing.pdf.lastIndexOf("/") + 1) })}
          icon={mdiFilePdfBox}
          onClose={() => setReplacing(null)}
          footer={
            <>
              <Button onClick={() => setReplacing(null)}>{t("desktop.cancel")}</Button>
              <Button
                variant="filled"
                onClick={() => {
                  const r = replacing;
                  setReplacing(null);
                  void exportPdf(r.path, r.pdf, r.version);
                }}
              >
                {t("desktop.files.replace")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.files.replace_pdf_body")}</p>
        </Dialog>
      )}
    </section>
  );
}

/**
 * The preview's rendered document in pane, once it has settled: shown
 * (the pane may just have switched to it) and no longer changing, as
 * Mermaid diagrams finish drawing. Gives up waiting after 4 seconds.
 */
async function renderedPreview(pane: HTMLElement | null): Promise<HTMLElement> {
  let last = -1;
  for (let i = 0; i < 40; i++) {
    const el = pane?.querySelector<HTMLElement>(".preview-markdown .markdown");
    if (el) {
      const size = el.innerHTML.length;
      if (size === last) return el;
      last = size;
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  const el = pane?.querySelector<HTMLElement>(".preview-markdown .markdown");
  if (!el) throw new Error(t("desktop.files.no_preview"));
  return el;
}

// Notes about the file: a refused save, changes on disk.
function Bars({ tab, model }: { tab: Tab; model: EditorModel }) {
  if (tab.conflict !== undefined) {
    return (
      <div className="editor-bar warn" role="alert">
        <Icon path={mdiAlertCircleOutline} size="sm" />
        <span className="spacer">{tab.conflict ? t("desktop.files.conflict") : t("desktop.files.conflict_deleted")}</span>
        <Button small onClick={() => model.save(tab.path, true)}>
          {t("desktop.files.overwrite")}
        </Button>
        <Button small onClick={() => model.reload(tab.path)}>
          {t("desktop.files.reload")}
        </Button>
      </div>
    );
  }
  if (tab.disk) {
    return (
      <div className="editor-bar warn" role="status">
        <Icon path={mdiAlertCircleOutline} size="sm" />
        <span className="spacer">{t(tab.disk === "deleted" ? "desktop.files.deleted_on_disk" : "desktop.files.changed_on_disk")}</span>
        {tab.disk === "changed" && (
          <Button small onClick={() => model.reload(tab.path)}>
            {t("desktop.files.reload")}
          </Button>
        )}
      </div>
    );
  }
  return null;
}

/** Asks what to do with unsaved changes. */
export function UnsavedDialog({ names, count, onSave, onDiscard, onCancel }: { names: string[]; count?: number; onSave?: () => void; onDiscard: () => void; onCancel: () => void }) {
  return (
    <Dialog
      title={t("desktop.files.unsaved_title")}
      icon={mdiAlertCircleOutline}
      onClose={onCancel}
      footer={
        <>
          <Button onClick={onCancel}>{t("desktop.cancel")}</Button>
          <Button danger onClick={onDiscard}>
            {t("desktop.files.discard")}
          </Button>
          {onSave && (
            <Button variant="filled" onClick={onSave}>
              {t("desktop.files.save")}
            </Button>
          )}
        </>
      }
    >
      <p>{count ? tn("desktop.files.unsaved_workspace", count, { name: names.join(", ") }) : t("desktop.files.unsaved_body", { names: names.join(", ") })}</p>
    </Dialog>
  );
}

/** A size in bytes for people: B, kB or MB. */
export function formatSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} kB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
