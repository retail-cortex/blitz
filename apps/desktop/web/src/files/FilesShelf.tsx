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
import { createPortal } from "react-dom";
import {
  mdiChevronDown,
  mdiChevronRight,
  mdiChevronDoubleLeft,
  mdiCollapseAllOutline,
  mdiContentCopy,
  mdiDeleteOutline,
  mdiEyeOffOutline,
  mdiEyeOutline,
  mdiHistory,
  mdiFilePlusOutline,
  mdiFileReplaceOutline,
  mdiFolderOpenOutline,
  mdiFolderOutline,
  mdiFolderPlusOutline,
  mdiLinkVariant,
  mdiLoading,
  mdiLockOutline,
  mdiRefresh,
  mdiRenameOutline,
  mdiAt,
  mdiMessagePlusOutline,
  mdiMinusBoxOutline,
  mdiOpenInNew,
  mdiPlusBoxMultipleOutline,
  mdiPlusBoxOutline,
  mdiSourceBranch,
  mdiSourceCommit,
  mdiTrayArrowDown,
  mdiUndoVariant,
  mdiWeb,
  mdiBrushVariant,
  mdiClose,
  mdiContentCut,
  mdiContentDuplicate,
  mdiContentPaste,
  mdiFilterVariant,
  mdiFormTextbox,
  mdiArrowCollapseUp,
} from "@mdi/js";
import { canCall, canDragOut, droppedPaths, dragOut, fileManager, openURL, revealPath, servePage, trashDropped } from "../desktop";
import { addToContext, openWeb, showHistory, showView } from "../events";
import { includeAll } from "../includeAll";
import { files } from "../api";
import { message } from "../errors";
import { FileKind, GitAction, type FileEntry } from "../gen/blitz/v1/file_pb";
import { gitActions, gitKeys, stagedKey } from "./gitMenu";
import { t, tn } from "../i18n";
import { Button, ContextMenu, Dialog, Icon, IconButton, useSnackbar, type MenuEntry } from "@blitz/webui";
import { ResizeHandle } from "@blitz/webui";
import { fileIcon } from "./icons";
import { dropEntries, importDrop, readDrop, someNames, type DropItem, type OnConflict } from "./upload";
import {
  ancestors,
  copyName,
  dragType,
  emptyTree,
  isFolder,
  joinPath,
  moveTarget,
  nameOf,
  nextMatch,
  parentOf,
  problemsUp,
  rangeOf,
  rowLabel,
  rows,
  setExpanded,
  shownFolders,
  topmost,
  treeDrag,
  validName,
  validNewName,
  withChildren,
  type Row,
  type Tree,
} from "./tree";
import { languageSession } from "./language";
import { isWebPath } from "./webTabs";
import { isTermPath } from "../terminal/terminals";
import { useAdvanced, useApp } from "../state";
import { useOrgBranding } from "../orgBranding";

/** slowOpen is how long opening goes before the list says what it waits for. */
const slowOpen = 1500;

const gitIcons: Record<GitAction, string> = {
  [GitAction.UNSPECIFIED]: mdiSourceBranch,
  [GitAction.STAGE]: mdiPlusBoxOutline,
  [GitAction.UNSTAGE]: mdiMinusBoxOutline,
  [GitAction.DISCARD]: mdiUndoVariant,
  [GitAction.IGNORE]: mdiEyeOffOutline,
  [GitAction.UNIGNORE]: mdiEyeOutline,
};

const gitLetters: Record<string, string> = { modified: "M", added: "A", deleted: "D", renamed: "R", untracked: "U", conflicted: "!" };

/** p with forward slashes, to compare a path on Windows with the workspace's. */
const slashes = (p: string) => p.replace(/\\/g, "/");


/** How long a drag rests on a closed folder before it opens. */
const dragOpenMs = 600;

/**
 * Files and folders dropped from the computer, waiting on the user
 * (FIL-36): how (copy or move, in the app) and, when names are taken,
 * what to do with them.
 */
type Incoming = {
  into: string;
  items: DropItem[];
  /** The dropped files' paths on the computer, by item index ("" where unknown). */
  paths: string[];
  /** The names in into. */
  taken: Set<string>;
  /** The items' names already in into. */
  conflicts: string[];
  /** Move can be offered: the app knows where the originals are. */
  canMove: boolean;
  move: boolean;
  step: "how" | "conflict";
};

/** Something being named in the tree: a new file or folder, or a rename. */
type Naming = { kind: "file" | "folder"; dir: string } | { kind: "rename"; path: string };

/**
 * The Files shelf (spec_029_files §5): the workspace as a tree, loaded a
 * folder at a time, with git status, the agent's rules, and file
 * operations.
 */
export function FilesShelf({
  dir,
  showHidden,
  onToggleHidden,
  active,
  refresh,
  reveal,
  onOpen,
  onMoved,
  onChanged,
  onCommit,
  onClose,
  width,
  onResize,
}: {
  dir: string;
  /** Its width in pixels; dragging its edge sets another. */
  width: number;
  onResize: (w: number) => void;
  showHidden: boolean;
  onToggleHidden: () => void;
  /** The file the editor shows. */
  active: string | null;
  /** Changes when the tree should be listed again. */
  refresh: number;
  /** A path to show (its folders opened), then cleared. */
  reveal: { path: string } | null;
  onOpen: (path: string) => void;
  onMoved: (from: string, to: string | null) => void;
  /** Files changed on disk outside the editor (a git action): list again, reload open tabs. */
  onChanged: () => void;
  /** Opens the commit dialog (the git menu's Commit…). */
  onCommit?: () => void;
  onClose: () => void;
}) {  // Simple mode: no refresh, collapse, hidden files, git actions or relative paths.
  const advanced = useAdvanced();

  const snack = useSnackbar();
  const { prefs, update } = useApp();
  const view = { sort: prefs.files_sort, nest: prefs.files_nest, compact: prefs.files_compact === "on" };
  const [tree, setTree] = useState<Tree>(emptyTree);
  // The selection: the row with the keyboard (lead), and the others
  // picked with it (FIL-74); anchor is where Shift's range starts.
  const [selected, setLead] = useState<string | null>(null);
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set());
  const anchor = useRef<string | null>(null);
  const setSelected = (p: string | null) => {
    setLead(p);
    setPicked(new Set());
    anchor.current = p;
  };
  const isPicked = (p: string) => p === selected || picked.has(p);
  // The selected paths an action takes, folders without what's under them.
  const selection = () => topmost([...picked, ...(selected ? [selected] : [])]);
  const [naming, setNaming] = useState<Naming | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number; entry: FileEntry | null } | null>(null);
  const [discarding, setDiscarding] = useState<FileEntry | null>(null);
  // Copied or cut rows, waiting to be pasted (FIL-38).
  const [clip, setClip] = useState<{ paths: string[]; cut: boolean } | null>(null);
  // Big folders listed further (FIL-39): how many each lists, and which are cut short.
  const limits = useRef(new Map<string, number>());
  const [truncated, setTruncated] = useState<ReadonlySet<string>>(new Set());
  // The language servers' problems, each file's and folder's worst (FIL-46).
  const [problems, setProblems] = useState<ReadonlyMap<string, "error" | "warning">>(new Map());
  useEffect(() => {
    const session = languageSession(dir);
    const show = () => setProblems(problemsUp(session.worstProblems()));
    show();
    return session.subscribeAll(show);
  }, [dir]);
  // The workspace is a git repository (from its listing): git actions are offered.
  const [repo, setRepo] = useState(false);
  const [error, setError] = useState("");
  // Until the workspace has opened (the service opens it on the first
  // call, its model included), the list says so, and after a moment what
  // it's waiting for (EN-37): never an empty list that isn't.
  const [opened, setOpened] = useState(false);
  const [waiting, setWaiting] = useState<"" | "opening" | "slow">("");
  const org = useOrgBranding();
  useEffect(() => {
    setOpened(false);
    setWaiting("");
    // Shown only when opening takes a while, so a quick one doesn't flash.
    const ids = [window.setTimeout(() => setWaiting("opening"), 300), window.setTimeout(() => setWaiting("slow"), slowOpen)];
    return () => ids.forEach((id) => window.clearTimeout(id));
  }, [dir]);
  const list = useRef<HTMLDivElement>(null);
  const treeRef = useRef(tree);
  treeRef.current = tree;

  const load = useCallback(
    async (folder: string) => {
      const res = await files.listDir({ workspace: dir, path: folder, showHidden, limit: limits.current.get(folder) ?? 0 });
      setTree((tr) => withChildren(tr, folder, res.entries));
      setTruncated((s) => {
        if (s.has(folder) === res.truncated) return s;
        const next = new Set(s);
        if (res.truncated) next.add(folder);
        else next.delete(folder);
        return next;
      });
      if (folder === "") setRepo(res.repo);
      return res.entries;
    },
    [dir, showHidden],
  );

  // Everything shown, listed again: at first, when hidden files are
  // toggled, and on a refresh.
  const reloadAll = useCallback(async () => {
    const [root, ...rest] = shownFolders(treeRef.current);
    try {
      await load(root);
      setError("");
    } catch (e) {
      setError(message(e));
      return;
    } finally {
      setOpened(true);
    }
    await Promise.all(rest.map((f) => load(f).catch(() => undefined)));
  }, [load]);
  const reloadRef = useRef(reloadAll);
  reloadRef.current = reloadAll;
  useEffect(() => {
    void reloadAll();
  }, [reloadAll]);
  useEffect(() => {
    if (refresh) void reloadRef.current();
  }, [refresh]);

  // Opens or closes a folder (or a file with files nested under it). With
  // compact folders, opening one that holds only a folder opens that too,
  // so the chain shows on one row (FIL-47).
  const toggle = async (e: FileEntry, open = !tree.expanded.has(e.path)) => {
    setTree((tr) => setExpanded(tr, e.path, open));
    if (!open || !isFolder(e)) return;
    let at = e.path;
    for (let depth = 0; depth < 32; depth++) {
      let kids = treeRef.current.children.get(at);
      if (!kids) {
        try {
          kids = await load(at);
        } catch (err) {
          snack(message(err), { error: true });
          return;
        }
      }
      if (!view.compact || kids.length !== 1 || !isFolder(kids[0])) return;
      at = kids[0].path;
      const next = at;
      setTree((tr) => setExpanded(tr, next, true));
    }
  };

  // Shows a path: opens its folders, selects it, scrolls to it.
  const show = useCallback(
    async (path: string, select = true) => {
      for (const folder of ancestors(path)) {
        setTree((tr) => setExpanded(tr, folder, true));
        if (!treeRef.current.children.has(folder)) await load(folder).catch(() => undefined);
      }
      if (select) {
        setLead(path);
        setPicked(new Set());
        anchor.current = path;
      }
      requestAnimationFrame(() => list.current?.querySelector(`[data-path="${CSS.escape(path)}"]`)?.scrollIntoView({ block: "nearest" }));
    },
    [load],
  );
  useEffect(() => {
    if (reveal) void show(reveal.path);
  }, [reveal, show]);
  // The tree follows the open tab (FIL-75).
  const follow = prefs.files_follow === "on";
  useEffect(() => {
    if (follow && active && opened && !isWebPath(active) && !isTermPath(active)) void show(active);
  }, [active, follow, opened, show]);

  // The folder new files go in: the selected folder, or the selected file's.
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

  // Gives the keyboard back to a row once it's drawn (after naming, the
  // field it replaced had it).
  const focusRow = (path: string) => requestAnimationFrame(() => list.current?.querySelector<HTMLElement>(`[data-path="${CSS.escape(path)}"]`)?.focus());

  const finishNaming = async (name: string) => {
    const n = naming;
    setNaming(null);
    // A new name may be a path, its folders made (a/b/c.md, FIL-49); a rename can't.
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
        // The folders a nested name needs first (writing doesn't make them).
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

  // Deletes paths to the workspace's trash, with Undo in the snackbar
  // (FIL-37): no question first, since it can be undone.
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

  // Pastes what was copied or cut into the folder into (FIL-38): a copy
  // whose name is taken gets " copy" (then " copy 2"); a cut moves, and
  // isn't moved onto itself.
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

  // Copies each path beside itself (FIL-38).
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

  // The folder a drag over el would drop into: the row's folder, or its
  // file's, or the workspace off the rows.
  const folderAt = (el: EventTarget) => {
    const path = el instanceof Element ? el.closest<HTMLElement>("[data-path]")?.dataset.path : undefined;
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
        if (moved.length > 1) setPicked(new Set(moved.slice(1)));
      }
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  // Files dropped from the computer: asks how (copy or move) where the
  // app can move them, and what to do with names already there; an item
  // dropped onto itself is left alone.
  const [incoming, setIncoming] = useState<Incoming | null>(null);
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

  // A git action on an entry; the tree and open tabs are refreshed after.
  const runGit = async (e: FileEntry, action: GitAction) => {
    try {
      await files.gitFileAction({ workspace: dir, path: e.path, action });
      snack(t(gitKeys[action].done, { name: e.name }));
      onChanged();
    } catch (err) {
      snack(message(err), { error: true });
    }
  };

  // Show in Finder / Dolphin / …, in the app only.
  const [manager, setManager] = useState<string | null>(null);
  useEffect(() => {
    void fileManager().then(setManager);
  }, []);
  const revealItem = (p: string): MenuEntry => ({
    label: manager ? t("desktop.files.reveal_in", { name: manager }) : t("desktop.files.reveal_in_file_manager"),
    icon: mdiFolderOpenOutline,
    onSelect: () => void revealPath(absolute(p)).catch((err) => snack(message(err), { error: true })),
  });

  const copy = (text: string) => navigator.clipboard?.writeText(text).then(() => snack(t("desktop.files.copied")));
  const absolute = (p: string) => (p ? `${dir.replace(/\/+$/, "")}/${p}` : dir);

  // The context paths of paths: a folder's ends in "/".
  const contextPaths = (paths: string[]) =>
    paths.map((p) => {
      const e = findEntry(treeRef.current, p);
      return e && isFolder(e) ? `${p}/` : p;
    });

  // Copy, cut and paste (FIL-38).
  const clipItems = (paths: string[], into: string | null): MenuEntry[] => {
    const items: MenuEntry[] = [];
    if (paths.length > 0) {
      items.push(
        { label: t("desktop.files.cut"), icon: mdiContentCut, detail: "⌘X", onSelect: () => setClip({ paths, cut: true }) },
        { label: t("desktop.files.copy"), icon: mdiContentCopy, detail: "⌘C", onSelect: () => setClip({ paths, cut: false }) },
      );
    }
    if (into !== null && clip) items.push({ label: t("desktop.files.paste"), icon: mdiContentPaste, detail: "⌘V", onSelect: () => void paste(into) });
    if (paths.length > 0) items.push({ label: t("desktop.files.duplicate"), icon: mdiContentDuplicate, detail: "⌘D", onSelect: () => void duplicate(paths) });
    return items;
  };

  // How the tree shows its rows (FIL-47, FIL-48, FIL-75).
  const viewItems = (): MenuEntry[] => [
    { heading: t("desktop.files.sort.heading") },
    ...(["folders", "mixed", "type"] as const).map((s) => ({ label: t(`desktop.files.sort.${s}`), on: view.sort === s, onSelect: () => update((p) => ({ ...p, files_sort: s })) })),
    { heading: t("desktop.files.view.heading") },
    { label: t("desktop.files.view.nest"), on: view.nest, onSelect: () => update((p) => ({ ...p, files_nest: !p.files_nest })) },
    { label: t("desktop.files.view.compact"), on: view.compact, onSelect: () => update((p) => ({ ...p, files_compact: p.files_compact === "on" ? "off" : "on" })) },
    { label: t("desktop.files.view.follow"), on: follow, onSelect: () => update((p) => ({ ...p, files_follow: p.files_follow === "on" ? "off" : "on" })) },
  ];

  // The menu of a selection of several (FIL-74): what acts on them all.
  const selectionItems = (paths: string[]): MenuEntry[] => {
    const shared = paths.filter((p) => findEntry(treeRef.current, p)?.agentRule !== "blocked");
    const items: MenuEntry[] = [];
    if (shared.length > 0) {
      items.push(
        { label: tn("desktop.files.add_n_to_context", shared.length), icon: mdiAt, onSelect: () => contextPaths(shared).forEach((path) => addToContext({ dir, path })) },
        "divider",
      );
    }
    items.push(...clipItems(paths, null), "divider", { label: tn("desktop.files.delete_n", paths.length), icon: mdiDeleteOutline, danger: true, detail: "⌫", onSelect: () => void remove(paths) });
    items.push("divider", { label: t("desktop.files.copy_paths"), icon: mdiContentCopy, onSelect: () => void copy(paths.map(absolute).join("\n")) });
    return items;
  };

  const menuItems = (e: FileEntry | null): MenuEntry[] => {
    const many = selection();
    if (e && many.length > 1 && isPicked(e.path)) return selectionItems(many);
    const folder = e ? (isFolder(e) ? e.path : parentOf(e.path)) : "";
    const items: MenuEntry[] = [];
    if (e && !isFolder(e)) items.push({ label: t("desktop.files.open"), icon: fileIcon(e.name), onSelect: () => onOpen(e.path) });
    // A page also opens in a browser, where its scripts run (the preview runs none; EL-102).
    if (e && !isFolder(e) && /\.html?$/i.test(e.name) && canCall("ServePage")) {
      const served = (then: (url: string) => unknown) => () =>
        void servePage(dir, e.path)
          .then(then)
          .catch((err: unknown) => snack(message(err), { error: true }));
      items.push(
        { label: t("desktop.files.open_in_browser"), icon: mdiWeb, onSelect: served((url) => openWeb({ dir, url })) },
        { label: t("desktop.files.open_in_system_browser"), icon: mdiOpenInNew, onSelect: served((url) => openURL(url)) },
      );
    }
    items.push(
      { label: t("desktop.files.new_file"), icon: mdiFilePlusOutline, onSelect: () => void startNew("file", folder) },
      { label: t("desktop.files.new_folder"), icon: mdiFolderPlusOutline, onSelect: () => void startNew("folder", folder) },
    );
    // Pictures (spec_058 PIC-10): a picture to start from, or a folder to save in.
    if (e && e.agentRule !== "blocked" && (isFolder(e) || /\.(png|jpe?g|webp)$/i.test(e.name))) {
      items.push(
        isFolder(e)
          ? { label: t("desktop.files.picture_here"), icon: mdiBrushVariant, onSelect: () => showView({ dir, view: "pictures", picture: { folder: e.path } }) }
          : { label: t("desktop.files.edit_in_pictures"), icon: mdiBrushVariant, onSelect: () => showView({ dir, view: "pictures", picture: { reference: e.path } }) },
      );
    }
    if (e && e.agentRule !== "blocked") {
      const path = isFolder(e) ? `${e.path}/` : e.path;
      items.push(
        "divider",
        { label: t(isFolder(e) ? "desktop.files.add_folder_to_context" : "desktop.files.add_file_to_context"), icon: mdiAt, onSelect: () => addToContext({ dir, path }) },
        { label: t(isFolder(e) ? "desktop.files.new_chat_about_folder" : "desktop.files.new_chat_about"), icon: mdiMessagePlusOutline, onSelect: () => addToContext({ dir, path, fresh: true }) },
      );
    }
    items.push("divider", ...clipItems(e ? [e.path] : [], folder));
    if (e && isFolder(e) && [...tree.expanded].some((p) => p.startsWith(e.path + "/"))) {
      items.push({ label: t("desktop.files.collapse_inside"), icon: mdiArrowCollapseUp, onSelect: () => setTree((tr) => ({ ...tr, expanded: new Set([...tr.expanded].filter((p) => !p.startsWith(e.path + "/"))) })) });
    }
    if (e) {
      items.push(
        "divider",
        { label: t("desktop.files.rename"), icon: mdiRenameOutline, detail: "F2", onSelect: () => setNaming({ kind: "rename", path: e.path }) },
        { label: t("desktop.files.delete"), icon: mdiDeleteOutline, danger: true, detail: "⌫", onSelect: () => void remove([e.path]) },
      );
      const git = gitActions({ folder: isFolder(e), git: e.git, staged: e.staged, ignored: e.ignored }, repo);
      if (advanced && git.length > 0) {
        items.push("divider", { heading: t("desktop.files.git.heading") });
        for (const a of git) {
          items.push({
            label: t(gitKeys[a].item),
            icon: gitIcons[a],
            danger: a === GitAction.DISCARD,
            onSelect: () => (a === GitAction.DISCARD ? setDiscarding(e) : void runGit(e, a)),
          });
        }
        if (!e.ignored && e.git !== "untracked" && e.git !== "added") {
          items.push({ label: t("desktop.files.revisions.open"), icon: mdiHistory, onSelect: () => showHistory({ dir, path: e.path, folder: isFolder(e) }) });
        }
        if (onCommit) items.push({ label: t("desktop.commit.open"), icon: mdiSourceCommit, onSelect: onCommit });
      }
      items.push(
        "divider",
        { label: t("desktop.files.copy_name"), icon: mdiFormTextbox, onSelect: () => void copy(e.name) },
        { label: t("desktop.files.copy_path"), icon: mdiContentCopy, onSelect: () => void copy(absolute(e.path)) },
      );
      if (advanced) items.push({ label: t("desktop.files.copy_relative"), icon: mdiLinkVariant, onSelect: () => void copy(e.path) });
    }
    if (!e && advanced && repo) {
      items.push("divider", { heading: t("desktop.files.git.heading") }, {
        label: t("desktop.commit.stage_all"),
        icon: mdiPlusBoxMultipleOutline,
        onSelect: () =>
          void includeAll(dir).then(
            () => (snack(t("desktop.commit.included_all")), onChanged()),
            (err: unknown) => snack(message(err), { error: true }),
          ),
      });
      if (onCommit) items.push({ label: t("desktop.commit.open"), icon: mdiSourceCommit, onSelect: onCommit });
    }
    if (manager !== null) {
      if (!e) items.push("divider");
      items.push(revealItem(e?.path ?? ""));
    }
    if (!e) items.push("divider", ...viewItems());
    return items;
  };

  const shown = rows(tree, "", 0, view);
  // Where a big folder's "Show more" goes: after the last row under it (FIL-39).
  const moreAfter = new Map<number, string>();
  for (const folder of truncated) {
    if (folder !== "" && !tree.expanded.has(folder)) continue;
    let last = -1;
    shown.forEach((r, i) => {
      if (folder === "" || r.entry.path.startsWith(folder + "/")) last = i;
    });
    if (last >= 0) moreAfter.set(last, folder);
  }
  const showMore = (folder: string) => {
    const listed = treeRef.current.children.get(folder)?.length ?? 0;
    limits.current.set(folder, Math.max(listed, 5000) * 2);
    void load(folder).catch((err) => snack(message(err), { error: true }));
  };

  // Type-to-find (FIL-73): letters typed in a row go to the next row whose
  // name starts with them; a pause starts again.
  const typed = useRef({ text: "", at: 0 });

  // The filter (FIL-73): the workspace's files and folders whose paths
  // match what's typed (FindFiles' fuzzy match), in place of the tree.
  const [filter, setFilter] = useState<string | null>(null);
  const [found, setFound] = useState<string[] | null>(null);
  useEffect(() => {
    const q = filter?.trim() ?? "";
    if (!q) {
      setFound(null);
      return;
    }
    let live = true;
    const id = window.setTimeout(() => {
      files.findFiles({ workspace: dir, query: q, folders: true, limit: 200 }).then(
        (r) => live && setFound(r.paths),
        (err: unknown) => live && (setFound([]), snack(message(err), { error: true })),
      );
    }, 150);
    return () => {
      live = false;
      window.clearTimeout(id);
    };
    // snack is stable; a new search only for a new query or workspace.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter, dir]);
  // A match chosen: a file opens; a folder shows in the tree.
  const openFound = (p: string) => {
    const path = p.endsWith("/") ? p.slice(0, -1) : p;
    setFilter(null);
    if (p.endsWith("/")) void show(path).then(() => findEntry(treeRef.current, path) && toggle(findEntry(treeRef.current, path)!, true));
    else {
      void show(path);
      onOpen(path);
    }
  };

  // Arrows move (with Shift, select the rows passed), Right and Left open
  // and close folders, Enter opens, F2 renames, Delete deletes (all
  // selected), Escape clears the selection (so new files go in the
  // workspace); ⌘A, ⌘C, ⌘X, ⌘V and ⌘D select all, copy, cut, paste and
  // duplicate; letters find.
  const onKey = (ev: React.KeyboardEvent) => {
    if (naming || (ev.target as HTMLElement).tagName === "INPUT") return;
    const i = shown.findIndex((r) => r.entry.path === selected);
    const cur = shown[i];
    const row = cur?.entry;
    const go = (j: number, extend = false) => {
      const r = shown[Math.max(0, Math.min(shown.length - 1, j))];
      if (!r) return;
      if (extend) {
        const from = anchor.current ?? selected ?? r.entry.path;
        setLead(r.entry.path);
        setPicked(new Set(rangeOf(shown, from, r.entry.path)));
        anchor.current = from;
      } else setSelected(r.entry.path);
      list.current?.querySelector<HTMLElement>(`[data-path="${CSS.escape(r.entry.path)}"]`)?.focus();
    };
    const mod = ev.metaKey || ev.ctrlKey;
    const opens = (r: Row | undefined) => !!r && (isFolder(r.entry) || !!r.nests);
    switch (true) {
      case ev.key === "ArrowDown":
        go(i + 1, ev.shiftKey);
        break;
      case ev.key === "ArrowUp":
        go(i - 1, ev.shiftKey);
        break;
      case ev.key === "ArrowRight":
        if (cur && opens(cur)) void toggle(cur.entry, true);
        break;
      case ev.key === "ArrowLeft": {
        if (cur && opens(cur) && tree.expanded.has(cur.entry.path)) void toggle(cur.entry, false);
        else if (cur) {
          // Up to the row it's under: a compacted chain's parent is the chain's first's.
          const up = parentOf((cur.chain?.[0] ?? cur.entry).path);
          const j = shown.findIndex((r) => r.entry.path === up || r.chain?.some((c) => c.path === up));
          const nester = shown.slice(0, i).reverse().find((r) => r.nests && r.depth === cur.depth - 1);
          if (j >= 0) go(j);
          else if (nester) go(shown.indexOf(nester));
        }
        break;
      }
      case ev.key === "Enter":
        if (row) (isFolder(row) ? void toggle(row) : onOpen(row.path));
        break;
      case ev.key === "F2":
        if (row) setNaming({ kind: "rename", path: row.path });
        break;
      case ev.key === "Delete" || (ev.key === "Backspace" && ev.metaKey):
        if (selection().length > 0) void remove(selection());
        break;
      case mod && ev.key.toLowerCase() === "a":
        if (shown.length === 0) return;
        setPicked(new Set(shown.map((r) => r.entry.path)));
        if (!selected) setLead(shown[0].entry.path);
        break;
      case mod && ev.key.toLowerCase() === "c" && !ev.shiftKey:
        if (selection().length === 0) return;
        setClip({ paths: selection(), cut: false });
        break;
      case mod && ev.key.toLowerCase() === "x":
        if (selection().length === 0) return;
        setClip({ paths: selection(), cut: true });
        break;
      case mod && ev.key.toLowerCase() === "v":
        if (!clip) return;
        void paste();
        break;
      case mod && ev.key.toLowerCase() === "d":
        if (selection().length === 0) return;
        void duplicate(selection());
        break;
      case ev.key === "Escape":
        if (clip) setClip(null);
        else if (selected || picked.size > 0) setSelected(null);
        else return;
        break;
      case ev.key.length === 1 && !mod && !ev.altKey && ev.key !== " ": {
        const now = Date.now();
        typed.current = { text: (now - typed.current.at < 800 ? typed.current.text : "") + ev.key, at: now };
        // One letter again steps to the next match; more letters refine from here.
        const from = typed.current.text.length === 1 ? i : i - 1;
        const j = nextMatch(shown, from, typed.current.text);
        if (j >= 0) go(j);
        break;
      }
      default:
        return;
    }
    ev.preventDefault();
  };

  const newRow = (folder: string, depth: number) =>
    naming && naming.kind !== "rename" && naming.dir === folder ? (
      <NameInput key="new" depth={depth} icon={naming.kind === "folder" ? mdiFolderOutline : mdiFilePlusOutline} initial="" label={t(naming.kind === "folder" ? "desktop.files.new_folder" : "desktop.files.new_file")} onDone={finishNaming} />
    ) : null;

  return (
    <aside className="files-shelf" aria-label={t("desktop.files.title")} style={{ width }}>
      <ResizeHandle width={width} edge="right" min={200} max={() => Math.min(560, window.innerWidth - 600)} label={t("desktop.files.resize")} onResize={onResize} />
      <div className="panel-head files-head">
        <span className="t-title-sm spacer">{t("desktop.files.title")}</span>
        <IconButton icon={mdiFilePlusOutline} label={t("desktop.files.new_file")} small onClick={() => void startNew("file")} />
        <IconButton icon={mdiFolderPlusOutline} label={t("desktop.files.new_folder")} small onClick={() => void startNew("folder")} />
        {advanced && (
          <>
            <IconButton icon={mdiRefresh} label={t("desktop.files.refresh")} small onClick={() => void reloadAll()} />
            <IconButton icon={mdiCollapseAllOutline} label={t("desktop.files.collapse")} small onClick={() => setTree((tr) => ({ ...tr, expanded: new Set() }))} />
            <IconButton icon={showHidden ? mdiEyeOutline : mdiEyeOffOutline} label={t("desktop.files.show_hidden")} small selected={showHidden} onClick={onToggleHidden} />
          </>
        )}
        <IconButton icon={mdiFilterVariant} label={t("desktop.files.filter")} small selected={filter !== null} onClick={() => setFilter((f) => (f === null ? "" : null))} />
        <IconButton icon={mdiChevronDoubleLeft} label={t("desktop.files.hide")} small onClick={onClose} />
      </div>
      {filter !== null && (
        <div className="files-filter">
          <input
            className="input"
            autoFocus
            aria-label={t("desktop.files.filter")}
            placeholder={t("desktop.files.filter_placeholder")}
            value={filter}
            spellCheck={false}
            onChange={(e) => setFilter(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setFilter(null);
              if (e.key === "ArrowDown") e.currentTarget.closest(".files-shelf")?.querySelector<HTMLElement>('[role="listitem"]')?.focus();
              if (e.key === "Enter" && found?.[0]) openFound(found[0]);
            }}
          />
          <IconButton icon={mdiClose} label={t("desktop.files.filter_close")} small onClick={() => setFilter(null)} />
        </div>
      )}
      <div
        hidden={found === null}
        className="files-tree"
        role="list"
        aria-label={t("desktop.files.filter_results")}
      >
        {found?.map((p) => {
          const folder = p.endsWith("/");
          const path = folder ? p.slice(0, -1) : p;
          return (
            <button key={p} role="listitem" data-path={path} className={`tree-row ${path === active ? "active" : ""}`} style={{ paddingLeft: 8 }} title={path} onClick={() => openFound(p)}>
              <span className="tree-twisty" />
              <Icon path={folder ? mdiFolderOutline : fileIcon(nameOf(path))} size="sm" className="tree-icon" />
              <span className="ellipsis tree-name">{nameOf(path)}</span>
              <span className="ellipsis tree-where">{parentOf(path)}</span>
            </button>
          );
        })}
        {found?.length === 0 && <p className="muted t-body-sm files-note">{t("desktop.files.filter_none")}</p>}
      </div>
      <div
        hidden={found !== null}
        className={`files-tree ${dropInto === "" ? "drop-target" : ""}`}
        role="tree"
        ref={list}
        onKeyDown={onKey}
        // A click on the background clears the selection.
        onClick={(e) => {
          if (e.target === e.currentTarget) setSelected(null);
        }}
        onDragOver={onDragOver}
        onDragLeave={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
            setDropInto(null);
            stopOpenTimer();
          }
        }}
        onDrop={(e) => void onDrop(e)}
        onContextMenu={(e) => {
          if (e.target === e.currentTarget) {
            e.preventDefault();
            setMenu({ x: e.clientX, y: e.clientY, entry: null });
          }
        }}
      >
        {error && <p className="error-text t-body-sm files-note">{error}</p>}
        {newRow("", 0)}
        {shown.map((r, index) => {
          const { entry: e, depth } = r;
          const open = tree.expanded.has(e.path);
          const opens = isFolder(e) || !!r.nests;
          const renaming = naming?.kind === "rename" && naming.path === e.path;
          const problem = problems.get(e.path);
          const cut = clip?.cut && clip.paths.some((p) => e.path === p || e.path.startsWith(p + "/"));
          const more = moreAfter.get(index);
          return (
            <div key={e.path} role="none">
              {renaming ? (
                <NameInput depth={depth} icon={isFolder(e) ? mdiFolderOutline : fileIcon(e.name)} initial={e.name} label={t("desktop.files.rename")} onDone={finishNaming} />
              ) : (
                <button
                  role="treeitem"
                  aria-expanded={opens ? open : undefined}
                  aria-selected={isPicked(e.path)}
                  data-path={e.path}
                  className={`tree-row ${e.path === selected ? "selected" : ""} ${picked.has(e.path) ? "picked" : ""} ${cut ? "cut" : ""} ${e.path === active ? "active" : ""} ${e.path === dropInto ? "drop-target" : ""} ${e.hidden ? "hidden-entry" : ""} git-${e.git || "clean"} ${problem ? `problem-${problem}` : ""}`}
                  style={{ paddingLeft: 8 + depth * 14 }}
                  title={[e.path, e.agentRule && t(`desktop.files.rule.${e.agentRule}.detail`), problem && t(`desktop.files.problem.${problem}`)].filter(Boolean).join("\n")}
                  tabIndex={e.path === selected || (!selected && shown[0]?.entry.path === e.path) ? 0 : -1}
                  draggable
                  onDragStart={(ev) => {
                    // A row of the selection drags the selection (FIL-74).
                    const paths = isPicked(e.path) && selection().length > 1 ? selection() : [e.path];
                    dragging.current = paths;
                    treeDrag.current = { dir, paths: contextPaths(paths) };
                    // In the app, the computer's own drag: it can leave the
                    // window with a copy (FIL-36), and back in the tree it's
                    // a move.
                    if (canDragOut()) {
                      ev.preventDefault();
                      void dragOut(paths.map(absolute)).catch((err) => snack(message(err), { error: true }));
                      return;
                    }
                    ev.dataTransfer.setData(dragType, paths.join("\n"));
                    ev.dataTransfer.effectAllowed = "copyMove";
                  }}
                  onDragEnd={endDrag}
                  onClick={(ev) => {
                    // ⌘ or Ctrl adds the row to the selection or takes it out;
                    // Shift selects the rows from the last one clicked (FIL-74).
                    if (ev.metaKey || ev.ctrlKey) {
                      const all = new Set([...picked, ...(selected ? [selected] : [])]);
                      if (all.has(e.path)) all.delete(e.path);
                      else all.add(e.path);
                      const lead = all.has(e.path) ? e.path : ([...all].pop() ?? null);
                      if (lead) all.delete(lead);
                      setLead(lead);
                      setPicked(all);
                      anchor.current = e.path;
                      return;
                    }
                    if (ev.shiftKey && (anchor.current ?? selected)) {
                      const from = anchor.current ?? selected!;
                      setLead(e.path);
                      setPicked(new Set(rangeOf(shown, from, e.path)));
                      anchor.current = from;
                      return;
                    }
                    setSelected(e.path);
                    if (isFolder(e)) void toggle(e);
                    else onOpen(e.path);
                  }}
                  onContextMenu={(ev) => {
                    ev.preventDefault();
                    if (!isPicked(e.path)) setSelected(e.path);
                    setMenu({ x: ev.clientX, y: ev.clientY, entry: e });
                  }}
                >
                  <span
                    className="tree-twisty"
                    onClick={(ev) => {
                      // A file's twisty opens its nested files; the file itself opens with the row.
                      if (!r.nests) return;
                      ev.stopPropagation();
                      void toggle(e);
                    }}
                  >
                    {opens && <Icon path={open ? mdiChevronDown : mdiChevronRight} size="sm" />}
                  </span>
                  <Icon path={isFolder(e) ? (open ? mdiFolderOpenOutline : mdiFolderOutline) : fileIcon(e.name)} size="sm" className="tree-icon" />
                  <span className="ellipsis tree-name">
                    {r.chain
                      ? r.chain.map((c, k) => (
                          <span key={c.path}>
                            {k > 0 && <span className="tree-chain-sep">/</span>}
                            {c.name}
                          </span>
                        ))
                      : rowLabel(r)}
                  </span>
                  {e.kind === FileKind.SYMLINK && <Icon path={mdiLinkVariant} size="sm" className="muted" />}
                  {e.agentRule && <Icon path={mdiLockOutline} size="sm" className="muted" />}
                  {problem && !isFolder(e) && <span className="tree-problem" aria-label={t(`desktop.files.problem.${problem}`)} />}
                  {e.git === "changed" ? <span className="git-dot" /> : e.git && (
                      <span className={`git-letter ${e.staged ? `staged-${e.staged}` : ""}`} title={t(stagedKey(e.staged))}>
                        {gitLetters[e.git] ?? ""}
                      </span>
                    )}
                </button>
              )}
              {isFolder(e) && open && newRow(e.path, depth + 1)}
              {more !== undefined && (
                <button className="tree-row tree-more" style={{ paddingLeft: 8 + (more === "" ? 0 : (shown.find((x) => x.entry.path === more)?.depth ?? 0) + 1) * 14 }} onClick={() => showMore(more)}>
                  <span className="tree-twisty" />
                  <span className="ellipsis tree-name">{t("desktop.files.show_more", { n: (tree.children.get(more)?.length ?? 0).toLocaleString() })}</span>
                </button>
              )}
            </div>
          );
        })}
        {!opened && !error && waiting && (
          <p className="muted t-body-sm files-note row" role="status">
            <Icon path={mdiLoading} size="sm" spin />
            {waiting === "slow" ? (org?.name ? t("desktop.files.opening_org", { org: org.name }) : t("desktop.files.opening_slow")) : t("desktop.files.opening")}
          </p>
        )}
        {opened && shown.length === 0 && !error && !naming && <p className="muted t-body-sm files-note">{t("desktop.files.empty")}</p>}
      </div>
      {menu && <ContextMenu x={menu.x} y={menu.y} items={menuItems(menu.entry)} onClose={() => setMenu(null)} />}
      {/* At the page's level: the shelf's glass blur would otherwise be the
          frame its dialogs centre in. */}
      {discarding &&
        createPortal(
          <Dialog
            title={t("desktop.files.git.discard_title", { name: discarding.name })}
            icon={mdiUndoVariant}
            onClose={() => setDiscarding(null)}
            footer={
              <>
                <Button onClick={() => setDiscarding(null)}>{t("desktop.cancel")}</Button>
                <Button
                  variant="filled"
                  danger
                  onClick={() => {
                    const e = discarding;
                    setDiscarding(null);
                    void runGit(e, GitAction.DISCARD);
                  }}
                >
                  {t("desktop.files.git.discard_confirm")}
                </Button>
              </>
            }
          >
            <p>{t(isFolder(discarding) ? "desktop.files.git.discard_folder_body" : "desktop.files.git.discard_body", { path: discarding.path })}</p>
          </Dialog>,
          document.body,
        )}
      {incoming?.step === "how" &&
        createPortal(
          <Dialog
            title={t("desktop.files.drop.title", { folder: folderName(incoming.into) })}
            icon={mdiTrayArrowDown}
            onClose={() => setIncoming(null)}
            footer={
              <>
                <Button onClick={() => setIncoming(null)}>{t("desktop.cancel")}</Button>
                <Button onClick={() => chooseHow(incoming, true)}>{t("desktop.files.drop.move")}</Button>
                <Button variant="filled" onClick={() => chooseHow(incoming, false)}>
                  {t("desktop.files.drop.copy")}
                </Button>
              </>
            }
          >
            <p>{tn("desktop.files.drop.body", incoming.items.length, { names: someNames(incoming.items.map((i) => i.name)), folder: folderName(incoming.into) })}</p>
            <p className="muted t-body-sm">{t("desktop.files.drop.move_detail")}</p>
          </Dialog>,
          document.body,
        )}
      {incoming?.step === "conflict" &&
        createPortal(
          <Dialog
            title={tn("desktop.files.drop.conflict_title", incoming.conflicts.length, { names: someNames(incoming.conflicts), folder: folderName(incoming.into) })}
            icon={mdiFileReplaceOutline}
            onClose={() => setIncoming(null)}
            footer={
              <>
                <Button onClick={() => setIncoming(null)}>{t("desktop.cancel")}</Button>
                <Button onClick={() => void bringIn(incoming, "skip")}>{t("desktop.files.drop.skip")}</Button>
                <Button onClick={() => void bringIn(incoming, "keep")}>{t("desktop.files.drop.keep_both")}</Button>
                <Button variant="filled" danger onClick={() => void bringIn(incoming, "replace")}>
                  {t("desktop.files.drop.replace")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.files.drop.conflict_body")}</p>
          </Dialog>,
          document.body,
        )}
    </aside>
  );
}

function findEntry(tree: Tree, path: string): FileEntry | undefined {
  return tree.children.get(parentOf(path))?.find((e) => e.path === path);
}

// Naming a new file or folder, or renaming one: Enter keeps it, Escape or
// leaving the field drops it.
function NameInput({ depth, icon, initial, label, onDone }: { depth: number; icon: string; initial: string; label: string; onDone: (name: string) => void }) {
  const [value, setValue] = useState(initial);
  const ref = useRef<HTMLInputElement>(null);
  const done = useRef(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.focus();
    // Select the name without its extension.
    const dot = initial.lastIndexOf(".");
    el.setSelectionRange(0, dot > 0 ? dot : initial.length);
  }, [initial]);
  const finish = (name: string) => {
    if (done.current) return;
    done.current = true;
    onDone(name);
  };
  return (
    <div className="tree-row naming" style={{ paddingLeft: 8 + depth * 14 }}>
      <span className="tree-twisty" />
      <Icon path={icon} size="sm" className="tree-icon" />
      <input
        ref={ref}
        className={`input tree-input ${value && !validName(value) ? "invalid" : ""}`}
        aria-label={label}
        value={value}
        spellCheck={false}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          e.stopPropagation();
          if (e.key === "Enter") finish(value);
          if (e.key === "Escape") finish("");
        }}
        onBlur={() => finish("")}
      />
    </div>
  );
}

