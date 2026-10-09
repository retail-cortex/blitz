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

// A workspace's open files (spec_files_029 §6): their tabs, and the
// editor state of each, kept while the workspace is open so unsaved
// changes survive switching views and workspaces.
import { useCallback, useMemo, useRef, useState } from "react";
import type { EditorState } from "@codemirror/state";
import { files } from "../api";
import { errorMeta, message, reason } from "../errors";
import { fileState, languageSupport } from "./codemirror";
import { languageExtension, languageSession, type LanguageHooks } from "./language";
import { nameOf } from "./tree";

/** An open file: its tab, the version its text is based on, and what the editor knows about it. */
export interface Tab {
  path: string;
  name: string;
  loading: boolean;
  error?: string;
  /** The version the editor's text is based on. */
  version: string;
  size: number;
  binary: boolean;
  tooLarge: boolean;
  agentRule: string;
  dirty: boolean;
  /** Changed or deleted on disk while the tab has unsaved changes. */
  disk?: "changed" | "deleted";
  /** A save was refused: the file changed since (its version now). */
  conflict?: string;
}

/** Where to put the cursor when a tab is shown next. */
export interface Target {
  path: string;
  line?: number;
  column?: number;
  /** Words a search found at the line, to mark in the preview. */
  find?: string[];
}

/**
 * The open files and what can be done with them, for the editor pane, the
 * shelf and the workspace.
 */
export interface EditorModel {
  dir: string;
  tabs: Tab[];
  active: string | null;
  target: Target | null;
  wrap: boolean;
  setWrap: (wrap: boolean) => void;
  /** The editor state of a tab (and to keep the view's latest). */
  state: (path: string) => EditorState | undefined;
  keepState: (path: string, s: EditorState) => void;
  clearTarget: () => void;
  open: (path: string, line?: number, column?: number, find?: string[]) => void;
  activate: (path: string) => void;
  close: (path: string) => void;
  save: (path: string, overwrite?: boolean) => Promise<boolean>;
  reload: (path: string) => Promise<void>;
  /** Asks the service whether open files changed elsewhere. */
  check: () => Promise<void>;
  /** A file was renamed or deleted in the shelf. */
  moved: (from: string, to: string | null) => void;
  dirtyCount: number;
}

/**
 * A workspace's open files: loading, saving (refused over someone else's
 * change), reloading, and noticing changes made elsewhere.
 */
export function useEditor(dir: string, language?: Omit<LanguageHooks, "open">): EditorModel {
  const [tabs, setTabs] = useState<Tab[]>([]);
  const [active, setActive] = useState<string | null>(null);
  const [target, setTarget] = useState<Target | null>(null);
  const [wrap, setWrap] = useState(false);
  const states = useRef(new Map<string, EditorState>());
  const saved = useRef(new Map<string, string>());
  const tabsRef = useRef(tabs);
  tabsRef.current = tabs;

  const patch = useCallback((path: string, p: Partial<Tab>) => setTabs((ts) => ts.map((t) => (t.path === path ? { ...t, ...p } : t))), []);
  // The language servers' hooks; open is the model's own (defined below).
  const hooks = useRef(language);
  hooks.current = language;
  const openRef = useRef<EditorModel["open"]>(() => {});

  // A new editor state for text; its changes mark the tab dirty.
  const makeState = useCallback(
    async (path: string, text: string) => {
      const lang = await languageSupport(path);
      saved.current.set(path, text);
      const h = hooks.current;
      const servers = h ? languageExtension(dir, path, { ...h, open: (p, line, column) => openRef.current(p, line, column) }) : [];
      const s = fileState(text, lang, {
        save: () => void save(path),
        changed: (st) => {
          const dirty = st.doc.toString() !== saved.current.get(path);
          if (tabsRef.current.find((t) => t.path === path)?.dirty !== dirty) patch(path, { dirty });
        },
      }, false, servers);
      states.current.set(path, s);
      return s;
    },
    // save is defined below; it's stable while dir is.
    [dir, patch],
  );

  const load = useCallback(
    async (path: string) => {
      try {
        const f = await files.readFile({ workspace: dir, path });
        const info = { version: f.version, size: Number(f.size), binary: f.binary, tooLarge: f.tooLarge, agentRule: f.agentRule };
        if (!f.binary && !f.tooLarge) await makeState(path, f.text);
        patch(path, { ...info, loading: false, error: undefined, dirty: false, disk: undefined, conflict: undefined });
      } catch (e) {
        patch(path, { loading: false, error: message(e) });
      }
    },
    [dir, makeState, patch],
  );

  const open = useCallback(
    (path: string, line?: number, column?: number, find?: string[]) => {
      setActive(path);
      setTarget(line ? { path, line, column, find } : null);
      if (tabsRef.current.some((t) => t.path === path)) return;
      setTabs((ts) => [...ts, { path, name: nameOf(path), loading: true, version: "", size: 0, binary: false, tooLarge: false, agentRule: "", dirty: false }]);
      void load(path);
    },
    [load],
  );

  openRef.current = open;

  const close = useCallback((path: string) => {
    languageSession(dir).close(path);
    states.current.delete(path);
    saved.current.delete(path);
    setTabs((ts) => {
      const i = ts.findIndex((t) => t.path === path);
      const next = ts.filter((t) => t.path !== path);
      setActive((a) => (a === path ? (next[Math.min(i, next.length - 1)]?.path ?? null) : a));
      return next;
    });
  }, [dir]);

  const save = useCallback(
    async (path: string, overwrite = false): Promise<boolean> => {
      const tab = tabsRef.current.find((t) => t.path === path);
      const s = states.current.get(path);
      if (!tab || !s) return false;
      const text = s.doc.toString();
      const version = overwrite && tab.conflict !== undefined ? tab.conflict : tab.version;
      try {
        const res = await files.writeFile({ workspace: dir, path, text, version });
        saved.current.set(path, text);
        const now = states.current.get(path)?.doc.toString();
        patch(path, { version: res.version, dirty: now !== text, disk: undefined, conflict: undefined, size: new TextEncoder().encode(text).length });
        return true;
      } catch (e) {
        if (reason(e) === "FILE_CHANGED") {
          patch(path, { conflict: errorMeta(e, "current_version") });
          return false;
        }
        patch(path, { error: message(e) });
        return false;
      }
    },
    [dir, patch],
  );

  const reload = useCallback(async (path: string) => {
    patch(path, { loading: true });
    await load(path);
  }, [load, patch]);

  const check = useCallback(async () => {
    const open = tabsRef.current.filter((t) => !t.loading && !t.error);
    if (open.length === 0) return;
    let versions: Record<string, string>;
    try {
      versions = (await files.statFiles({ workspace: dir, paths: open.map((t) => t.path) })).versions;
    } catch {
      return; // the service is away; the next check will tell
    }
    for (const t of open) {
      const v = versions[t.path] ?? "";
      if (v === t.version || (t.conflict !== undefined && v === t.conflict)) continue;
      if (v === "") patch(t.path, { disk: "deleted" });
      else if (!t.dirty) void load(t.path);
      else patch(t.path, { disk: "changed" });
    }
  }, [dir, load, patch]);

  const moved = useCallback(
    (from: string, to: string | null) => {
      const affected = tabsRef.current.filter((t) => t.path === from || t.path.startsWith(from + "/"));
      for (const t of affected) {
        if (to === null) {
          patch(t.path, { disk: "deleted" });
          continue;
        }
        const next = to + t.path.slice(from.length);
        languageSession(dir).moved(t.path, next);
        const s = states.current.get(t.path);
        if (s) states.current.set(next, s);
        const sv = saved.current.get(t.path);
        if (sv !== undefined) saved.current.set(next, sv);
        states.current.delete(t.path);
        saved.current.delete(t.path);
        patch(t.path, { path: next, name: nameOf(next) });
        setActive((a) => (a === t.path ? next : a));
      }
    },
    [dir, patch],
  );

  return useMemo(
    () => ({
      dir,
      tabs,
      active,
      target,
      wrap,
      setWrap,
      state: (p: string) => states.current.get(p),
      keepState: (p: string, s: EditorState) => void states.current.set(p, s),
      clearTarget: () => setTarget(null),
      open,
      activate: setActive,
      close,
      save,
      reload,
      check,
      moved,
      dirtyCount: tabs.filter((t) => t.dirty).length,
    }),
    [dir, tabs, active, target, wrap, open, close, save, reload, check, moved],
  );
}
