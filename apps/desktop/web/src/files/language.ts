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

// Language servers in the editor (spec_visual_editor_037 §5): the
// workspace's documents on the service's LanguageService, and the
// CodeMirror extension that gives a file completion, problems, hover and
// go to definition from them. The service shares the agent's servers; a
// window's documents last while it keeps them (every 30 s).
import { snippet, type Completion, type CompletionContext, type CompletionResult } from "@codemirror/autocomplete";
import { lintGutter, setDiagnostics, type Diagnostic } from "@codemirror/lint";
import { EditorSelection, EditorState, type Extension, type Text } from "@codemirror/state";
import { EditorView, hoverTooltip, keymap, ViewPlugin, type ViewUpdate } from "@codemirror/view";
import { language as api } from "../api";
import { reason } from "../errors";
import { ServerState, type CompletionItem, type SourceLocation, type TextEdit } from "../gen/blitz/v1/language_pb";

/** This window's ID: its documents are its own, and close when it goes. */
export const clientId = typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `w-${Date.now()}-${Math.random()}`;

/** How a file's language server is, for the status bar. */
export interface LanguageStatus {
  language: string;
  state: ServerState;
  /** Why it's missing, failed or untrusted. */
  detail: string;
  /** What to install, when missing. */
  install: string;
  errors: number;
  warnings: number;
}

/** A problem in a file, in 1-based lines and columns. */
export interface Problem {
  from: { line: number; column: number };
  to: { line: number; column: number };
  severity: "error" | "warning" | "info" | "hint";
  message: string;
  source: string;
}

interface Doc {
  path: string;
  id: string;
  language: string;
  state: ServerState;
  detail: string;
  install: string;
  text: string;
  version: number;
  sent: number;
  timer?: ReturnType<typeof setTimeout>;
  opening?: Promise<void>;
  problems?: { version: number; list: Problem[] };
}

// How long typing waits before the text goes to the server, and how often
// the window keeps its documents.
const changeDelay = 150;
const keepEvery = 30_000;

const sessions = new Map<string, LanguageSession>();

/** The language session of the workspace in dir. */
export function languageSession(dir: string): LanguageSession {
  let s = sessions.get(dir);
  if (!s) {
    s = new LanguageSession(dir);
    sessions.set(dir, s);
  }
  return s;
}

/** A workspace's documents on its language servers. */
export class LanguageSession {
  private docs = new Map<string, Doc>();
  private listeners = new Map<string, Set<() => void>>();
  private keeper?: ReturnType<typeof setInterval>;
  private watching?: AbortController;

  constructor(readonly dir: string) {}

  /** Calls f when path's problems or server state change. */
  subscribe(path: string, f: () => void): () => void {
    let set = this.listeners.get(path);
    if (!set) this.listeners.set(path, (set = new Set()));
    set.add(f);
    return () => set.delete(f);
  }

  private notify(path: string) {
    for (const f of this.listeners.get(path) ?? []) f();
  }

  /** How path's server is, and its problems' counts (undefined: not open). */
  status(path: string): LanguageStatus | undefined {
    const d = this.docs.get(path);
    if (!d || d.state === ServerState.NONE) return undefined;
    const list = d.problems?.list ?? [];
    return {
      language: d.language,
      state: d.state,
      detail: d.detail,
      install: d.install,
      errors: list.filter((p) => p.severity === "error").length,
      warnings: list.filter((p) => p.severity === "warning").length,
    };
  }

  /** path's problems for the text the editor has now (none while older). */
  problems(path: string): Problem[] | undefined {
    const d = this.docs.get(path);
    if (!d?.problems || d.problems.version !== d.version) return undefined;
    return d.problems.list;
  }

  /** Opens path with text, once; later calls only change its text. */
  open(path: string, text: string): Promise<void> {
    const d = this.docs.get(path);
    if (d) {
      if (d.text !== text) this.change(path, text);
      return d.opening ?? Promise.resolve();
    }
    const doc: Doc = { path, id: "", language: "", state: ServerState.STARTING, detail: "", install: "", text, version: 1, sent: 1 };
    this.docs.set(path, doc);
    doc.opening = this.openOnServer(doc);
    return doc.opening;
  }

  private async openOnServer(doc: Doc) {
    try {
      const r = await api.openDocument({ workspace: this.dir, client: clientId, path: doc.path, text: doc.text, version: BigInt(doc.version) });
      Object.assign(doc, { id: r.document, language: r.language, state: r.state, detail: r.detail, install: r.install, sent: doc.version });
    } catch {
      doc.state = ServerState.NONE; // no language service here (an older service, the fake): the editor goes on without
    }
    doc.opening = undefined;
    if (doc.id) {
      this.start();
      if (doc.version !== doc.sent) void this.flush(doc.path);
      if (doc.state === ServerState.STARTING) this.awaitReady(doc);
    }
    this.notify(doc.path);
  }

  // A server being started: its state is asked for again until it settles.
  private awaitReady(doc: Doc, tries = 30) {
    setTimeout(async () => {
      if (this.docs.get(doc.path) !== doc || tries === 0) return;
      try {
        const st = await api.getLanguageStatus({ workspace: this.dir });
        const server = st.servers.find((s) => s.language === doc.language);
        if (server && server.state !== ServerState.STARTING && server.state !== ServerState.IDLE) {
          Object.assign(doc, { state: server.state, detail: server.error, install: server.install });
          this.notify(doc.path);
          return;
        }
      } catch {
        // asked again below
      }
      this.awaitReady(doc, tries - 1);
    }, 1000);
  }

  /** The editor's text changed: it goes to the server shortly. */
  change(path: string, text: string) {
    const d = this.docs.get(path);
    if (!d || d.text === text) return;
    d.text = text;
    d.version++;
    clearTimeout(d.timer);
    d.timer = setTimeout(() => void this.flush(path), changeDelay);
  }

  /** Sends path's latest text now (before a request about it). */
  async flush(path: string): Promise<Doc | undefined> {
    const d = this.docs.get(path);
    if (!d) return undefined;
    await d.opening;
    clearTimeout(d.timer);
    if (!d.id || d.sent === d.version) return d;
    const version = d.version;
    try {
      await api.changeDocument({ workspace: this.dir, document: d.id, version: BigInt(version), text: d.text });
      d.sent = Math.max(d.sent, version);
    } catch (e) {
      if (reason(e) === "UNKNOWN_DOCUMENT") await this.reopen(d);
    }
    return d;
  }

  // A document the service closed (the window was away too long): opened
  // again with the text it has now.
  private async reopen(d: Doc) {
    this.docs.delete(d.path);
    await this.open(d.path, d.text);
  }

  /** The editor closed path. */
  close(path: string) {
    const d = this.docs.get(path);
    if (!d) return;
    clearTimeout(d.timer);
    this.docs.delete(path);
    if (d.id) void api.closeDocument({ workspace: this.dir, document: d.id }).catch(() => {});
    if (this.docs.size === 0) this.stop();
  }

  /** Renames an open document's path (moved in the Files shelf). */
  moved(from: string, to: string) {
    const d = this.docs.get(from);
    if (!d) return;
    this.close(from);
    void this.open(to, d.text);
  }

  /** The document and its version, ready for a request (undefined: no server). */
  async ready(path: string): Promise<{ id: string; version: bigint } | undefined> {
    const d = await this.flush(path);
    if (!d?.id || (d.state !== ServerState.READY && d.state !== ServerState.STARTING)) return undefined;
    return { id: d.id, version: BigInt(d.version) };
  }

  // Keeping the documents, and watching their problems, while any is open.
  private start() {
    this.keeper ??= setInterval(() => void this.keep(), keepEvery);
    if (!this.watching) void this.watch();
  }

  private stop() {
    clearInterval(this.keeper);
    this.keeper = undefined;
    this.watching?.abort();
    this.watching = undefined;
  }

  private async keep() {
    try {
      const r = await api.keepDocuments({ workspace: this.dir, client: clientId });
      const open = new Set(r.documents);
      for (const d of [...this.docs.values()]) if (d.id && !open.has(d.id)) await this.reopen(d);
    } catch {
      // the service is away: the next keep will tell
    }
  }

  private async watch() {
    const ctl = new AbortController();
    this.watching = ctl;
    while (!ctl.signal.aborted) {
      try {
        for await (const m of api.watchDiagnostics({ workspace: this.dir, client: clientId }, { signal: ctl.signal })) {
          const d = [...this.docs.values()].find((x) => x.id === m.document);
          if (!d) continue;
          d.problems = {
            version: Number(m.version),
            list: m.diagnostics.map((x) => ({
              from: { line: x.range?.start?.line ?? 1, column: x.range?.start?.column ?? 1 },
              to: { line: x.range?.end?.line ?? 1, column: x.range?.end?.column ?? 1 },
              severity: severityOf(x.severity),
              message: x.message,
              source: x.source,
            })),
          };
          this.notify(d.path);
        }
      } catch {
        // the stream broke (the service restarted): watch again shortly
      }
      if (!ctl.signal.aborted) await new Promise((r) => setTimeout(r, 2000));
    }
  }
}

function severityOf(s: string): Problem["severity"] {
  return s === "error" || s === "warning" || s === "hint" ? s : "info";
}

/** A position (1-based line and column) in doc, clamped to it. */
export function offsetOf(doc: Text, line: number, column: number): number {
  const l = doc.line(Math.min(Math.max(1, line), doc.lines));
  return Math.min(l.from + Math.max(0, column - 1), l.to);
}

/** pos in doc as a 1-based line and column. */
export function positionOf(doc: Text, pos: number): { line: number; column: number } {
  const l = doc.lineAt(pos);
  return { line: l.number, column: pos - l.from + 1 };
}

/** The editor's problems for doc: CodeMirror's diagnostics. */
export function diagnosticsOf(doc: Text, problems: Problem[]): Diagnostic[] {
  return problems.map((p) => {
    const from = offsetOf(doc, p.from.line, p.from.column);
    const to = Math.max(from, offsetOf(doc, p.to.line, p.to.column));
    return { from, to, severity: p.severity, message: p.message, source: p.source || undefined };
  });
}

/** LSP's snippet syntax ($1, ${2:name}, $0) as CodeMirror's (${1}, ${2:name}, ${0}). */
export function snippetTemplate(text: string): string {
  return text.replace(/\$(\d+)/g, "${$1}");
}

// LSP's completion kinds as CodeMirror's icon types.
const completionTypes: Record<string, string> = {
  function: "function",
  method: "method",
  constructor: "function",
  field: "property",
  property: "property",
  variable: "variable",
  class: "class",
  struct: "class",
  interface: "interface",
  module: "namespace",
  keyword: "keyword",
  constant: "constant",
  enum: "enum",
  "enum member": "constant",
  "type parameter": "type",
  snippet: "text",
};

/** A server's completion item as CodeMirror's: ranked first, in the server's order. */
export function completionOf(item: CompletionItem, index: number, count: number, info?: (text: string) => Node): Completion {
  return {
    label: item.label,
    detail: item.detail || undefined,
    type: completionTypes[item.kind],
    info: item.documentation && info ? () => info(item.documentation) : undefined,
    // The server's order, above the file's words (CodeMirror sorts by
    // boost, -99 to 99, before the match score).
    boost: Math.round(99 - (98 * index) / Math.max(1, count)),
    apply: (view, _c, from, to) => applyCompletion(view, item, from, to),
  };
}

// Applies a completion: its extra edits (an import) first, then its text,
// a snippet's fields to Tab through.
function applyCompletion(view: EditorView, item: CompletionItem, from: number, to: number) {
  const doc = view.state.doc;
  const extra = item.additionalEdits.map((e) => editChange(doc, e));
  let [start, end] = [from, to];
  let text = item.insertText;
  if (item.edit) {
    const c = editChange(doc, item.edit);
    [start, end, text] = [c.from, Math.max(c.to, to), item.edit.text];
  }
  if (extra.length > 0) {
    const tr = view.state.update({ changes: extra });
    view.dispatch(tr);
    [start, end] = [tr.changes.mapPos(start, 1), tr.changes.mapPos(end, 1)];
  }
  if (item.snippet) {
    snippet(snippetTemplate(text))(view, { label: item.label }, start, end);
    return;
  }
  view.dispatch({ changes: { from: start, to: end, insert: text }, selection: EditorSelection.cursor(start + text.length), scrollIntoView: true, userEvent: "input.complete" });
}

function editChange(doc: Text, e: TextEdit) {
  const s = e.range?.start, n = e.range?.end;
  const from = offsetOf(doc, s?.line ?? 1, s?.column ?? 1);
  return { from, to: Math.max(from, offsetOf(doc, n?.line ?? 1, n?.column ?? 1)), insert: e.text };
}

/** What the editor asks the page to do for the language extension. */
export interface LanguageHooks {
  /** Opens a workspace file at a line and column. */
  open: (path: string, line: number, column: number) => void;
  /** Says where a definition outside the workspace is. */
  outside: (loc: SourceLocation) => void;
  /** Renders Markdown (hover, documentation) into a new element. */
  markdown: (text: string) => { dom: HTMLElement; destroy: () => void };
}

/**
 * The extension that puts a file on its language server: its text kept in
 * step, completion beside the editor's own, problems underlined and in the
 * gutter, hover after 400 ms, and ⌘-click or F12 to a definition.
 */
export function languageExtension(dir: string, path: string, hooks: LanguageHooks): Extension {
  const session = languageSession(dir);

  const complete = async (ctx: CompletionContext): Promise<CompletionResult | null> => {
    const word = ctx.matchBefore(/[\w$]*/);
    const before = ctx.state.sliceDoc(Math.max(0, ctx.pos - 1), ctx.pos);
    const trigger = ".:>".includes(before) && before !== "" ? before : "";
    if (!ctx.explicit && !trigger && (!word || word.from === word.to)) return null;
    const doc = await session.ready(path);
    if (!doc || ctx.aborted) return null;
    const pos = positionOf(ctx.state.doc, ctx.pos);
    try {
      const r = await api.complete({ workspace: dir, document: doc.id, version: doc.version, position: pos, trigger });
      if (r.items.length === 0) return null;
      const info = (text: string) => {
        const m = hooks.markdown(text);
        return m.dom;
      };
      return {
        from: word ? word.from : ctx.pos,
        options: r.items.map((item, i) => completionOf(item, i, r.items.length, info)),
        validFor: r.incomplete ? undefined : /^[\w$]*$/,
      };
    } catch {
      return null; // a stale version, or the server's away: the editor's own completion still shows
    }
  };

  const hover = hoverTooltip(
    async (view, pos) => {
      const doc = await session.ready(path);
      if (!doc) return null;
      const word = view.state.wordAt(pos);
      try {
        const r = await api.hover({ workspace: dir, document: doc.id, version: doc.version, position: positionOf(view.state.doc, pos) });
        if (!r.text.trim()) return null;
        return {
          pos: word?.from ?? pos,
          end: word?.to ?? pos,
          above: true,
          create: () => {
            const m = hooks.markdown(r.text);
            m.dom.classList.add("cm-lsp-hover");
            return { dom: m.dom, destroy: m.destroy };
          },
        };
      } catch {
        return null;
      }
    },
    { hoverTime: 400 },
  );

  const goToDefinition = (view: EditorView, pos: number) => {
    void (async () => {
      const doc = await session.ready(path);
      if (!doc) return;
      try {
        const r = await api.definition({ workspace: dir, document: doc.id, version: doc.version, position: positionOf(view.state.doc, pos) });
        const loc = r.locations[0];
        if (!loc) return;
        if (loc.outside) hooks.outside(loc);
        else if (loc.path === path) {
          const at = offsetOf(view.state.doc, loc.line, loc.column);
          view.dispatch({ selection: EditorSelection.cursor(at), effects: EditorView.scrollIntoView(at, { y: "center" }) });
        } else hooks.open(loc.path, loc.line, loc.column);
      } catch {
        // nothing to go to
      }
    })();
    return true;
  };

  // The view's text goes to the session, and the session's problems come
  // back to the view, for the file it shows.
  const sync = ViewPlugin.define((view) => {
    const show = () => {
      const problems = session.problems(path);
      if (problems) view.dispatch(setDiagnostics(view.state, diagnosticsOf(view.state.doc, problems)));
    };
    void session.open(path, view.state.doc.toString());
    const off = session.subscribe(path, () => queueMicrotask(show));
    queueMicrotask(show); // what came while another file was shown
    return {
      update(u: ViewUpdate) {
        if (u.docChanged) session.change(path, u.state.doc.toString());
      },
      destroy: off,
    };
  });

  return [
    EditorState.languageData.of(() => [{ autocomplete: complete }]),
    sync,
    lintGutter(),
    hover,
    keymap.of([{ key: "F12", run: (v) => goToDefinition(v, v.state.selection.main.head) }]),
    EditorView.domEventHandlers({
      mousedown: (e, view) => {
        // Only with a server: otherwise ⌘-click adds a cursor, as before.
        if (!(e.metaKey || e.ctrlKey) || e.button !== 0 || session.status(path)?.state !== ServerState.READY) return false;
        const pos = view.posAtCoords({ x: e.clientX, y: e.clientY });
        if (pos === null) return false;
        e.preventDefault();
        return goToDefinition(view, pos);
      },
    }),
  ];
}
