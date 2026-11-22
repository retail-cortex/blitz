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

import { type RefObject } from "react";
import { files } from "../api";
import { message } from "../errors";
import { GitAction, type FileEntry } from "../gen/blitz/v1/file_pb";
import { gitKeys } from "./gitMenu";
import { t, tn } from "../i18n";
import { findEntry, type Naming } from "./shelfTypes";
import {
  copyName,
  isFolder,
  joinPath,
  moveTarget,
  nameOf,
  parentOf,
  setExpanded,
  topmost,
  validName,
  validNewName,
  type Tree,
} from "./tree";

/** Copied or cut shelf items. */
export type ShelfClip = { paths: string[]; cut: boolean } | null;

/** Actions for the Files shelf tree (file creation, deletion, copy, paste, git actions). */
export function useFilesShelfActions({
  dir,
  tree,
  setTree,
  selected,
  setSelected,
  setLead,
  setPicked,
  naming,
  setNaming,
  clip,
  setClip,
  list,
  load,
  show,
  onMoved,
  onOpen,
  onChanged,
  snack,
}: {
  dir: string;
  tree: Tree;
  setTree: React.Dispatch<React.SetStateAction<Tree>>;
  selected: string | null;
  setSelected: (p: string | null) => void;
  setLead: React.Dispatch<React.SetStateAction<string | null>>;
  setPicked: React.Dispatch<React.SetStateAction<ReadonlySet<string>>>;
  naming: Naming | null;
  setNaming: (n: Naming | null) => void;
  clip: ShelfClip;
  setClip: (c: ShelfClip) => void;
  list: RefObject<HTMLDivElement | null>;
  load: (folder: string) => Promise<FileEntry[]>;
  show: (path: string, select?: boolean) => Promise<void>;
  onMoved: (from: string, to: string | null) => void;
  onOpen: (path: string) => void;
  onChanged: () => void;
  snack: (msg: string, opts?: { error?: boolean; action?: { label: string; run: () => void } }) => void;
}) {
  const targetFolder = () => {
    if (!selected) return "";
    const e = findEntry(tree, selected);
    return e && isFolder(e) ? e.path : parentOf(selected);
  };

  const startNew = async (kind: "file" | "folder", folder = targetFolder()) => {
    if (folder) {
      setTree((tr) => setExpanded(tr, folder, true));
      if (!tree.children.has(folder)) await load(folder).catch(() => undefined);
    }
    setNaming({ kind, dir: folder });
  };

  const focusRow = (path: string) =>
    requestAnimationFrame(() =>
      list.current?.querySelector<HTMLElement>(`[data-path="${CSS.escape(path)}"]`)?.focus(),
    );

  const finishNaming = async (name: string) => {
    const n = naming;
    setNaming(null);
    if (!n || !(n.kind === "rename" ? validName(name) : validNewName(name))) return;
    try {
      if (n.kind === "rename") {
        const to = joinPath(parentOf(n.path), name.trim());
        if (to === n.path) return;
        await files.renameFile({ workspace: dir, from: n.path, to });
        onMoved(n.path, to);
        await load(parentOf(n.path));
        setSelected(to);
        focusRow(to);
        return;
      }
      const path = joinPath(n.dir, name.trim());
      if (n.kind === "folder") await files.createFolder({ workspace: dir, path });
      else {
        if (parentOf(path) !== n.dir) await files.createFolder({ workspace: dir, path: parentOf(path) }).catch(() => undefined);
        await files.writeFile({ workspace: dir, path, text: "", version: "" });
      }
      await load(n.dir);
      await show(path);
      if (n.kind === "file") onOpen(path);
      else focusRow(path);
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const remove = async (paths: string[]) => {
    const gone: { path: string; id: string }[] = [];
    try {
      for (const path of topmost(paths)) {
        const res = await files.deleteFile({ workspace: dir, path, undoable: true });
        gone.push({ path, id: res.restoreId });
        onMoved(path, null);
      }
    } catch (err) {
      snack(message(err), { error: true });
    }
    if (gone.length === 0) return;
    const under = (p: string) => gone.some((g) => p === g.path || p.startsWith(g.path + "/"));
    setLead((s) => (s && under(s) ? null : s));
    setPicked(new Set());
    await Promise.all([...new Set(gone.map((g) => parentOf(g.path)))].map((f) => load(f).catch(() => undefined)));
    const undo = async () => {
      try {
        for (const g of [...gone].reverse()) if (g.id) await files.restoreFile({ workspace: dir, restoreId: g.id, path: g.path });
        await Promise.all([...new Set(gone.map((g) => parentOf(g.path)))].map((f) => load(f).catch(() => undefined)));
        onChanged();
        snack(gone.length === 1 ? t("desktop.files.restored", { name: nameOf(gone[0].path) }) : tn("desktop.files.restored_n", gone.length));
      } catch (err) {
        snack(message(err), { error: true });
      }
    };
    const text = gone.length === 1 ? t("desktop.files.deleted", { name: nameOf(gone[0].path) }) : tn("desktop.files.deleted_n", gone.length);
    snack(text, gone.every((g) => g.id) ? { action: { label: t("desktop.files.undo"), run: () => void undo() } } : undefined);
  };

  const paste = async (into = targetFolder()) => {
    if (!clip) return;
    try {
      const listing = await files.listDir({ workspace: dir, path: into, showHidden: true, limit: 0 });
      const taken = new Set(listing.entries.map((e) => e.name));
      const done: string[] = [];
      for (const from of topmost(clip.paths)) {
        if (clip.cut) {
          if (moveTarget(from, into) === null) continue;
          const name = copyName(nameOf(from), taken);
          const to = joinPath(into, name);
          await files.renameFile({ workspace: dir, from, to });
          onMoved(from, to);
          await load(parentOf(from)).catch(() => undefined);
          taken.add(name);
          done.push(to);
        } else {
          if (into === from || into.startsWith(from + "/")) continue;
          const name = copyName(nameOf(from), taken);
          const to = joinPath(into, name);
          await files.copyFile({ workspace: dir, from, to });
          taken.add(name);
          done.push(to);
        }
      }
      if (clip.cut) setClip(null);
      if (into) setTree((tr) => setExpanded(tr, into, true));
      await load(into);
      if (done[0]) {
        setSelected(done[0]);
        if (done.length > 1) setPicked(new Set(done.slice(1)));
      }
      onChanged();
      if (done.length > 0) snack(tn(clip.cut ? "desktop.files.moved_n" : "desktop.files.pasted", done.length, { name: nameOf(done[0]) }));
    } catch (err) {
      snack(message(err), { error: true });
      await load(into).catch(() => undefined);
    }
  };

  const duplicate = async (paths: string[]) => {
    try {
      const done: string[] = [];
      for (const from of topmost(paths)) {
        const folder = parentOf(from);
        const listing = await files.listDir({ workspace: dir, path: folder, showHidden: true, limit: 0 });
        const to = joinPath(folder, copyName(nameOf(from), new Set(listing.entries.map((e) => e.name))));
        await files.copyFile({ workspace: dir, from, to });
        await load(folder);
        done.push(to);
      }
      if (done[0]) setSelected(done[0]);
      onChanged();
    } catch (err) {
      snack(message(err), { error: true });
    }
  };

  const runGit = async (e: FileEntry, action: GitAction) => {
    try {
      await files.gitFileAction({ workspace: dir, path: e.path, action });
      snack(t(gitKeys[action].done, { name: e.name }));
      onChanged();
    } catch (err) {
      snack(message(err), { error: true });
    }
  };

  return { startNew, finishNaming, remove, paste, duplicate, runGit, targetFolder };
}
