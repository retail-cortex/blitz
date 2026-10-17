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

import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { openSearchPanel } from "@codemirror/search";
import { mdiAlertCircleOutline, mdiArrowExpandHorizontal, mdiAt, mdiChevronRight, mdiClose, mdiContentSave, mdiExport, mdiFilePdfBox, mdiFileWordBoxOutline, mdiHistory, mdiLockOutline, mdiMagnify, mdiMessagePlusOutline, mdiWeb, mdiWebPlus, mdiWrap } from "@mdi/js";
import { files } from "../api";
import { canCall, canPreview, printPDF } from "../desktop";
import { message } from "../errors";
import { addToContext, findInFileEvent, openFile, openWeb, showHistory } from "../events";
import { FindBar } from "./FindBar";
import { EditorView } from "@codemirror/view";
import { t, tn } from "../i18n";
import { Button, ContextMenu, Dialog, Icon, IconButton, Menu, Segmented, useSnackbar, type MenuEntry } from "../ui/controls";
import { Preview } from "./Preview";
import { WebPreview } from "./WebPreview";
import { isWebPath } from "./webTabs";
// The visual editor (TipTap) loads with the first Markdown file shown in it.
const VisualEditor = lazy(() => import("./visual/VisualEditor").then((m) => ({ default: m.VisualEditor })));
import { defaultView, previewKind, previewOnly, type View } from "./previewKind";
import type { Cursor } from "../status";
import { ServerState } from "../gen/blitz/v1/language_pb";
import { languageSession, type LanguageStatus } from "./language";
import { goToLine, languageOf, setWrap } from "./codemirror";
import { fileIcon } from "./icons";
import type { EditorModel, Tab } from "./useEditor";
import { tabsToClose, type CloseWhich } from "./tabMenu";
import { useEditorZoom } from "./zoom";
import { useAdvanced, useApp } from "../state";
import { exportPath, paperFor, printablePage } from "./printPage";
import { drawAsPicture, drawForPrint, mermaidSources } from "../mermaid";

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
  // Tabs waiting on the unsaved-changes question, and the tab menu (FIL-40a).
  const [closing, setClosing] = useState<Tab[] | null>(null);
  const [tabMenu, setTabMenu] = useState<{ x: number; y: number; path: string } | null>(null);
  const tab = model.tabs.find((x) => x.path === model.active);
  // A web page's tab (EL-100) shows its page, not a file.
  const web = tab?.web ? (tab as Tab & { web: NonNullable<Tab["web"]> }) : undefined;
  const ready = tab && !web && !tab.loading && !tab.error && !tab.binary && !tab.tooLarge;
  // Images and PDFs show as previews; Markdown and SVG can, and Markdown
  // does when it opens (FIL-54). views keeps each tab's choice.
  const kind = tab && !web ? previewKind(tab.path) : null;
  const [views, setViews] = useState<ReadonlyMap<string, View>>(new Map());
  const atLine = !!tab && model.target?.path === tab.path && !!model.target.line;
  // A search hit in Markdown shows in the preview, its words marked.
  const found = useMemo(
    () => (atLine && kind === "markdown" && model.target?.find?.length ? { line: model.target.line ?? 0, terms: model.target.find } : undefined),
    [atLine, kind, model.target],
  );
  const shownAs = tab && (found ? "preview" : atLine && kind !== "table" ? "source" : (views.get(tab.path) ?? defaultView(kind, { atLine, empty: tab.size === 0 })));
  // A table too large for the editor, or not UTF-8, still shows as a table.
  const tableOnly = !!tab && kind === "table" && (tab.tooLarge || tab.binary);
  const showPreview = !!tab && !tab.loading && !tab.error && !!kind && (previewOnly(kind) || tableOnly || (ready && shownAs === "preview"));
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
  const { prefs, theme, update } = useApp();
  const [exporting, setExporting] = useState(false);
  // Markdown in advanced mode is edited where it shows (spec_visual_editor_037
  // VE-01): Visual in Preview's place, except while it prints (Export as
  // PDF prints the preview) or shows a search hit. MDX keeps Preview.
  const visual = advanced && kind === "markdown" && !!tab && !/\.mdx$/i.test(tab.path);
  const showVisual = visual && !!ready && showPreview && !found && !exporting;
  // An export that would replace a file waits for the user to say so.
  type Export = { kind: "pdf" | "docx"; path: string; out: string; version: string };
  const [replacing, setReplacing] = useState<Export | null>(null);
  const runExport = (e: Export) => void (e.kind === "pdf" ? exportPdf(e.path, e.out, e.version) : exportDocx(e.path, e.out, e.version));
  const exported = (out: string) =>
    snack(t("desktop.files.exported", { name: out.slice(out.lastIndexOf("/") + 1) }), {
      action: { label: t("desktop.files.open_pdf"), run: () => openFile({ dir: model.dir, path: out }) },
    });
  // Export as Word (FS-75): the editor's text, written by the service.
  const exportDocx = async (path: string, out: string, version: string) => {
    setExporting(true);
    try {
      const markdown = model.state(path)?.doc.toString() ?? "";
      // Its diagrams drawn here, where Mermaid runs; one that won't draw
      // stays as its code.
      const diagrams = [];
      for (const source of new Set(mermaidSources(markdown))) {
        try {
          diagrams.push({ source, ...(await drawAsPicture(source)) });
        } catch {
          // kept as code
        }
      }
      const { data } = await files.renderDocx({ workspace: model.dir, path, markdown, diagrams });
      await files.writeBinaryFile({ workspace: model.dir, path: out, data, version });
      exported(out);
    } catch (e) {
      snack(t("desktop.files.export_docx_failed", { error: message(e) }), { error: true });
    } finally {
      setExporting(false);
    }
  };
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
      exported(pdf);
    } catch (e) {
      snack(t("desktop.files.export_failed", { error: message(e) }), { error: true });
    } finally {
      setExporting(false);
    }
  };
  const startExport = async (kind: Export["kind"]) => {
    if (!tab) return;
    // A PDF prints the preview.
    if (kind === "pdf" && shownAs !== "preview") setView(tab.path, "preview");
    const out = exportPath(tab.path, kind === "pdf" ? ".pdf" : ".docx");
    try {
      const { versions } = await files.statFiles({ workspace: model.dir, paths: [out] });
      const e = { kind, path: tab.path, out, version: versions[out] ?? "" };
      if (e.version) setReplacing(e);
      else runExport(e);
    } catch (e) {
      snack(t(kind === "pdf" ? "desktop.files.export_failed" : "desktop.files.export_docx_failed", { error: message(e) }), { error: true });
    }
  };
  // Find in file (FIL-72): Cmd/Ctrl+F, the toolbar, or the palette; the
  // preview's find bar, or CodeMirror's in the source.
  const previewText = showPreview && ready && tab ? model.state(tab.path)?.doc.toString() : undefined;
  const findsPreview = showPreview && kind === "markdown" && previewText !== undefined;
  const findsSource = !!ready && !showPreview;
  const [finding, setFinding] = useState(0); // 0: closed; each ask, one more
  useEffect(() => setFinding(0), [tab?.path, showPreview]);
  const startFind = useCallback(() => {
    if (findsPreview) setFinding((n) => n + 1);
    else if (findsSource && view.current) openSearchPanel(view.current);
  }, [findsPreview, findsSource]);
  const findBox = useCallback(() => pane.current?.querySelector<HTMLElement>(".preview-markdown .markdown") ?? null, []);
  useEffect(() => {
    const onFind = (e: Event) => (e as CustomEvent<{ dir: string }>).detail.dir === model.dir && startFind();
    const key = (e: KeyboardEvent) => {
      const p = pane.current;
      const focused = !!p && (p.contains(document.activeElement) || document.activeElement === document.body);
      const inSource = !!document.activeElement?.closest(".cm-editor"); // CodeMirror's own keys
      if (focused && !inSource && p.offsetParent !== null && (e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "f" && (findsPreview || findsSource)) {
        e.preventDefault();
        startFind();
      }
    };
    window.addEventListener(findInFileEvent, onFind);
    window.addEventListener("keydown", key, true);
    return () => {
      window.removeEventListener(findInFileEvent, onFind);
      window.removeEventListener("keydown", key, true);
    };
  }, [model.dir, startFind, findsPreview, findsSource]);

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
    const ls = languageSession(model.dir).status(path);
    cursorTo.current?.({
      line: line.number,
      column: sel.head - line.from + 1,
      selected: sel.to - sel.from,
      language: languageOf(path)?.name ?? "",
      errors: ls?.errors,
      warnings: ls?.warnings,
      server: serverNote(ls),
    });
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

  // The file's problems and its server's state, as they change.
  useEffect(() => {
    if (!tab || showPreview) return;
    return languageSession(model.dir).subscribe(tab.path, () => {
      const v = view.current;
      if (v && shown.current === tab.path) report(v);
    });
    // report reads refs.
  }, [model.dir, tab?.path, showPreview]);

  // Go to a line when asked (a link, Go to file).
  useEffect(() => {
    const v = view.current;
    if (!v || !state || model.target?.path !== tab?.path || !model.target?.line || found) return;
    goToLine(v, model.target.line, model.target.column);
    model.clearTarget();
  }, [state, tab?.path, model, found]);

  useEffect(() => {
    if (view.current && state) setWrap(view.current, model.wrap);
  }, [model.wrap, state]);

  // The pane's zoom (zoom.ts): CodeMirror measures its lines again.
  const [zoom, setZoom] = useEditorZoom(pane);
  useEffect(() => view.current?.requestMeasure(), [zoom]);

  // Closing tabs with unsaved changes asks first, for all of them at once.
  const closeTabs = (ts: Tab[]) => {
    if (!ts.length) return;
    if (ts.some((x) => x.dirty)) setClosing(ts);
    else model.closeMany(ts.map((x) => x.path));
  };
  const closeTab = (x: Tab) => closeTabs([x]);
  const tabMenuItems = (path: string): MenuEntry[] => {
    const item = (which: CloseWhich, key: string): MenuEntry => {
      const ts = tabsToClose(model.tabs, path, which);
      return { label: t(key), disabled: !ts.length, onSelect: () => closeTabs(ts) };
    };
    return [
      item("this", "desktop.files.tab.close"),
      item("others", "desktop.files.tab.close_others"),
      item("right", "desktop.files.tab.close_right"),
      "divider",
      item("saved", "desktop.files.tab.close_saved"),
      item("all", "desktop.files.tab.close_all"),
      // A web page has no revisions.
      ...(isWebPath(path) ? [] : ["divider" as const, { label: t("desktop.files.revisions.open"), icon: mdiHistory, onSelect: () => showHistory({ dir: model.dir, path, folder: false }) }]),
    ];
  };

  return (
    <section className="editor-pane" ref={pane} aria-label={t("desktop.files.editor")} style={{ "--zoom": zoom / 100 } as CSSProperties} data-doc-width={prefs.doc_width}>
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
            onContextMenu={(e) => {
              e.preventDefault();
              setTabMenu({ x: e.clientX, y: e.clientY, path: x.path });
            }}
          >
            <Icon path={x.web ? mdiWeb : fileIcon(x.name)} size="sm" />
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
        {canPreview() && <IconButton icon={mdiWebPlus} label={t("desktop.web.open")} small onClick={() => openWeb({ dir: model.dir, url: "" })} />}
        {zoom !== 100 && (
          <button type="button" className="zoom-badge t-label" title={t("desktop.files.zoom_reset")} onClick={() => (setZoom(100), view.current?.focus())}>
            {t("desktop.files.zoom", { percent: zoom })}
          </button>
        )}
        {tab && /\.(md|markdown|mdx)$/i.test(tab.path) && (
          <IconButton
            icon={mdiArrowExpandHorizontal}
            label={t(prefs.doc_width === "full" ? "desktop.files.doc_width_readable" : "desktop.files.doc_width_full")}
            small
            selected={prefs.doc_width === "full"}
            onClick={() => update((p) => ({ ...p, doc_width: p.doc_width === "full" ? "readable" : "full" }))}
          />
        )}
        {advanced && <IconButton icon={mdiWrap} label={t("desktop.files.wrap")} small selected={model.wrap} onClick={() => model.setWrap(!model.wrap)} />}
      </div>
      {web && <WebPreview key={web.path} tab={web} model={model} />}
      {tab && !web && (
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
                      { value: "preview", label: visual ? t("desktop.files.visual") : t("desktop.files.preview") },
                      { value: "source", label: t("desktop.files.source") },
                    ]
                  : [
                      { value: "source", label: t("desktop.files.source") },
                      { value: "preview", label: t("desktop.files.preview") },
                    ]
              }
            />
          )}
          {(findsPreview || findsSource) && <IconButton icon={mdiMagnify} label={t("desktop.files.find")} small selected={finding > 0} onClick={startFind} />}
          {kind === "markdown" && ready && (
            <Menu
              trigger={(props) => <IconButton {...props} icon={mdiExport} label={t("desktop.files.export")} small disabled={exporting} />}
              placement="down end"
              items={[
                // Printing a PDF needs the app's web engine.
                ...(canCall("PrintPDF") ? [{ label: t("desktop.files.export_pdf"), icon: mdiFilePdfBox, onSelect: () => void startExport("pdf") }] : []),
                { label: t("desktop.files.export_docx"), icon: mdiFileWordBoxOutline, onSelect: () => void startExport("docx") },
              ]}
            />
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
      {tab && !web && <Bars tab={tab} model={model} />}
      {finding > 0 && findsPreview && <FindBar box={findBox} text={previewText ?? ""} focus={finding} onClose={() => setFinding(0)} />}
      <div className="editor-body" hidden={!!web}>
        <div className="editor-host" ref={host} hidden={!ready || showPreview} />
        {tab?.loading && <p className="editor-note muted">{t("desktop.checking")}</p>}
        {tab?.error && <p className="editor-note error-text">{tab.error}</p>}
        {showVisual && tab && previewText !== undefined && (
          <div className="preview preview-markdown visual-host">
            <Suspense fallback={null}>
              <VisualEditor key={tab.path} dir={model.dir} path={tab.path} text={previewText} onChange={(md) => model.setText(tab.path, md)} onSave={() => void model.save(tab.path)} language={model.language} />
            </Suspense>
          </div>
        )}
        {showPreview && !showVisual && tab && <Preview dir={model.dir} path={tab.path} kind={kind} text={previewText} line={atLine ? model.target?.line : undefined} find={found} onFound={model.clearTarget} />}
        {!showPreview && tab?.binary && <p className="editor-note muted">{t("desktop.files.binary", { size: formatSize(tab.size) })}</p>}
        {!showPreview && tab?.tooLarge && <p className="editor-note muted">{t("desktop.files.too_large", { size: formatSize(tab.size) })}</p>}
      </div>
      {tabMenu && <ContextMenu x={tabMenu.x} y={tabMenu.y} items={tabMenuItems(tabMenu.path)} onClose={() => setTabMenu(null)} />}
      {closing && (
        <UnsavedDialog
          names={closing.filter((x) => x.dirty).map((x) => x.name)}
          onSave={async () => {
            // Saves each unsaved tab, then closes the ones that saved and
            // those that had nothing to save; a failed save stays open.
            const ok = await Promise.all(closing.map((x) => (x.dirty ? model.save(x.path) : true)));
            model.closeMany(closing.filter((_, i) => ok[i]).map((x) => x.path));
            setClosing(null);
          }}
          onDiscard={() => {
            model.closeMany(closing.map((x) => x.path));
            setClosing(null);
          }}
          onCancel={() => setClosing(null)}
        />
      )}
      {replacing && (
        <Dialog
          title={t("desktop.files.replace_pdf", { name: replacing.out.slice(replacing.out.lastIndexOf("/") + 1) })}
          icon={replacing.kind === "pdf" ? mdiFilePdfBox : mdiFileWordBoxOutline}
          onClose={() => setReplacing(null)}
          footer={
            <>
              <Button onClick={() => setReplacing(null)}>{t("desktop.cancel")}</Button>
              <Button
                variant="filled"
                onClick={() => {
                  const r = replacing;
                  setReplacing(null);
                  runExport(r);
                }}
              >
                {t("desktop.files.replace")}
              </Button>
            </>
          }
        >
          <p>{t(replacing.kind === "pdf" ? "desktop.files.replace_pdf_body" : "desktop.files.replace_docx_body")}</p>
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

/** Why a file has no language server's help, for the status bar ("" when it has). */
function serverNote(ls: LanguageStatus | undefined): string {
  switch (ls?.state) {
    case ServerState.MISSING:
      return t("desktop.files.server_missing", { install: ls.install || ls.detail });
    case ServerState.FAILED:
      return t("desktop.files.server_failed", { error: ls.detail });
    case ServerState.UNTRUSTED:
      return t("desktop.files.server_untrusted");
  }
  return "";
}
