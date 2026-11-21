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

import { useEffect, useRef, useState, type MutableRefObject } from "react";
import { files } from "../api";
import { canCall, dragOut, droppedPaths, trashDropped } from "../desktop";
import { message } from "../errors";
import type { FileEntry } from "../gen/blitz/v1/file_pb";
import { t, tn } from "../i18n";
import { dragOpenMs, findEntry, slashes, type Incoming } from "./shelfTypes";
import { dragType, isFolder, joinPath, moveTarget, parentOf, setExpanded, topmost, treeDrag, type Tree } from "./tree";
import { dropEntries, importDrop, readDrop, type OnConflict } from "./upload";

/**
 * Manages internal row drag-to-move and external file/folder drops from the
 * computer into the Files shelf tree.
 */
export function useShelfDragDrop({
  dir,
  tree,
  setTree,
  load,
  toggle,
  absolute,
  contextPaths,
  selection,
  isPicked,
  setSelected,
  setLead,
  setPicked,
  anchor,
  onMoved,
  onChanged,
  snack,
}: {
  dir: string;
  tree: Tree;
  setTree: (updater: (tr: Tree) => Tree) => void;
  load: (folder: string) => Promise<FileEntry[]>;
  toggle: (e: FileEntry, open?: boolean) => Promise<void>;
  absolute: (p: string) => string;
  contextPaths: (paths: string[]) => string[];
  selection: () => string[];
  isPicked: (p: string) => boolean;
  setSelected: (p: string | null) => void;
  setLead: (p: string | null) => void;
  setPicked: (p: ReadonlySet<string>) => void;
  anchor: MutableRefObject<string | null>;
  onMoved: (from: string, to: string | null) => void;
  onChanged: () => void;
  snack: (text: string, opts?: { error?: boolean }) => void;
}) {
  // Dragging an entry onto a folder (or a file in it) moves it there; onto
  // the tree's background, to the workspace. A closed folder opens when
  // the drag rests on it.
  const dragging = useRef<string[] | null>(null);
  // Every dragged path can move into the folder into.
  const canMove = (from: string[] | null, into: string) => from !== null && from.length > 0 && from.every((p) => moveTarget(p, into) !== null);
  const [dropInto, setDropInto] = useState<string | null>(null);
  const openTimer = useRef<{ path: string; id: number } | null>(null);
  const stopOpenTimer = () => {
    if (openTimer.current) window.clearTimeout(openTimer.current.id);
    openTimer.current = null;
  };
  useEffect(() => stopOpenTimer, []);

  // The folder a drag over el would drop into: the row's folder (in a
  // compacted chain, the one of its names it's over, #110), or its file's,
  // or the workspace off the rows.
  const folderAt = (el: EventTarget) => {
    if (!(el instanceof Element)) return "";
    const part = el.closest<HTMLElement>("[data-folder]")?.dataset.folder;
    if (part) return part;
    const path = el.closest<HTMLElement>("[data-path]")?.dataset.path;
    if (path === undefined) return "";
    const e = findEntry(tree, path);
    return e && isFolder(e) ? e.path : parentOf(path);
  };

  const endDrag = () => {
    dragging.current = null;
    treeDrag.current = null;
    setDropInto(null);
    stopOpenTimer();
  };

  // Moves paths into the folder into (a drag in the tree).
  const moveInTree = async (paths: string[], into: string) => {
    const moved: string[] = [];
    try {
      for (const from of topmost(paths)) {
        const to = moveTarget(from, into);
        if (to === null) continue;
        await files.renameFile({ workspace: dir, from, to });
        onMoved(from, to);
        await load(parentOf(from));
        moved.push(to);
      }
      if (into) setTree((tr) => setExpanded(tr, into, true));
      await load(into);
      if (moved[0]) {
        setSelected(moved[0]);
        if (moved.length > 1) {
          setLead(moved[0]);
          setPicked(new Set(moved.slice(1)));
          anchor.current = moved[0];
        }
      }
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  // Files dropped from the computer: asks how (copy or move) where the
  // app can move them, and what to do with names already there; an item
  // dropped onto itself is left alone.
  const [incoming, setIncoming] = useState<Incoming | null>(null);
  const folderName = (into: string) => (into ? into.split("/").pop()! : t("desktop.files.drop.workspace"));
  const bringIn = async (inc: Incoming, onConflict: OnConflict) => {
    setIncoming(null);
    const where = folderName(inc.into);
    try {
      const { added, done } = await importDrop(files, dir, inc.into, inc.items, inc.taken, onConflict);
      let failed: string[] = [];
      if (inc.move) failed = await trashDropped(done.map((d) => inc.paths[d.index]).filter(Boolean));
      if (inc.into) setTree((tr) => setExpanded(tr, inc.into, true));
      await load(inc.into);
      if (added[0]) setSelected(added[0]);
      onChanged();
      if (failed.length > 0) snack(t("desktop.files.drop.not_trashed", { names: failed.map((p) => slashes(p).split("/").pop()).join(", ") }), { error: true });
      else if (added.length > 0) snack(tn(inc.move ? "desktop.files.drop.moved" : "desktop.files.drop.copied", added.length, { name: added[0].split("/").pop()!, folder: where }));
    } catch (e) {
      snack(message(e), { error: true });
      await load(inc.into).catch(() => undefined);
    }
  };

  const receive = async (into: string, entries: Parameters<typeof readDrop>[0], paths: string[]) => {
    try {
      const all = await readDrop(entries);
      const items = all.filter((i) => !paths[i.index] || slashes(paths[i.index]) !== slashes(absolute(joinPath(into, i.name))));
      if (items.length === 0) return;
      const listing = await files.listDir({ workspace: dir, path: into, showHidden: true });
      const taken = new Set(listing.entries.map((e) => e.name));
      const conflicts = items.filter((i) => taken.has(i.name)).map((i) => i.name);
      const canMove = canCall("TrashDropped") && items.every((i) => !!paths[i.index]);
      const inc: Incoming = { into, items, paths, taken, conflicts, canMove, move: false, step: canMove ? "how" : "conflict" };
      if (!canMove && conflicts.length === 0) await bringIn(inc, "skip");
      else setIncoming(inc);
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  // The user chose copy or move.
  const chooseHow = (inc: Incoming, move: boolean) => {
    if (inc.conflicts.length > 0) setIncoming({ ...inc, move, step: "conflict" });
    else void bringIn({ ...inc, move }, "skip");
  };

  const onDragOver = (ev: React.DragEvent) => {
    const from = dragging.current;
    const types = ev.dataTransfer.types;
    const into = folderAt(ev.target);
    // Files from the computer are taken anywhere; in the app a row's own
    // drag is one too (DragOut), told apart at the drop.
    const fromComputer = types.includes("Files");
    const move = (types.includes(dragType) || fromComputer) && canMove(from, into);
    if (!move && !fromComputer) {
      setDropInto(null);
      stopOpenTimer();
      return;
    }
    ev.preventDefault();
    ev.dataTransfer.dropEffect = move ? "move" : "copy";
    setDropInto(into);
    if (into && !tree.expanded.has(into) && openTimer.current?.path !== into) {
      stopOpenTimer();
      const e = findEntry(tree, into);
      if (e) openTimer.current = { path: into, id: window.setTimeout(() => void toggle(e, true), dragOpenMs) };
    }
  };

  const onDrop = async (ev: React.DragEvent) => {
    const from = dragging.current;
    const into = folderAt(ev.target);
    endDrag();
    // Read while the event lasts: its data is gone after.
    const drop = ev.dataTransfer.types.includes(dragType) ? null : dropEntries(ev.dataTransfer);
    if (drop) {
      ev.preventDefault();
      const paths = await droppedPaths(drop.files);
      // Rows dragged out (DragOut) and dropped back in the tree: a move.
      const own = from !== null && paths.length === from.length && from.every((p) => paths.some((q) => slashes(q) === slashes(absolute(p))));
      if (own) {
        if (canMove(from, into)) await moveInTree(from, into);
      } else await receive(into, drop.entries, paths);
      return;
    }
    if (!canMove(from, into)) return;
    ev.preventDefault();
    await moveInTree(from!, into);
  };

  const startRowDrag = (ev: React.DragEvent, path: string) => {
    // A row of the selection drags the selection (FIL-74).
    const paths = isPicked(path) && selection().length > 1 ? selection() : [path];
    dragging.current = paths;
    treeDrag.current = { dir, paths: contextPaths(paths) };
    // In the app, the computer's own drag: it can leave the
    // window with a copy (FIL-36), and back in the tree it's
    // a move.
    if (canCall("DragOut")) {
      ev.preventDefault();
      void dragOut(paths.map(absolute)).catch((err) => snack(message(err), { error: true }));
      return;
    }
    ev.dataTransfer.setData(dragType, paths.join("\n"));
    ev.dataTransfer.effectAllowed = "copyMove";
  };

  return {
    dropInto,
    setDropInto,
    incoming,
    setIncoming,
    stopOpenTimer,
    endDrag,
    onDragOver,
    onDrop,
    startRowDrag,
    chooseHow,
    bringIn,
  };
}
