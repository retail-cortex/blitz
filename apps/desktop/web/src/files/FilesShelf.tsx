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
  mdiFilePlusOutline,
  mdiFolderOpenOutline,
  mdiFolderOutline,
  mdiFolderPlusOutline,
  mdiLinkVariant,
  mdiLockOutline,
  mdiRefresh,
  mdiRenameOutline,
  mdiAt,
  mdiMessagePlusOutline,
  mdiMinusBoxOutline,
  mdiPlusBoxOutline,
  mdiSourceBranch,
  mdiUndoVariant,
} from "@mdi/js";
import { fileManager, revealPath } from "../desktop";
import { addToContext } from "../events";
import { files } from "../api";
import { message } from "../errors";
import { FileKind, GitAction, type FileEntry } from "../gen/blitz/v1/file_pb";
import { gitActions, gitKeys } from "./gitMenu";
import { t } from "../i18n";
import { Button, ContextMenu, Dialog, Icon, IconButton, useSnackbar, type MenuEntry } from "../ui/controls";
import { filesFloat, useFloatingDismiss } from "../ui/layout";
import { ResizeHandle } from "../ui/ResizeHandle";
import { fileIcon } from "./icons";
import { ancestors, emptyTree, isFolder, joinPath, moveTarget, parentOf, rows, setExpanded, shownFolders, validName, withChildren, type Tree } from "./tree";
import { useAdvanced } from "../state";

const gitIcons: Record<GitAction, string> = {
  [GitAction.UNSPECIFIED]: mdiSourceBranch,
  [GitAction.STAGE]: mdiPlusBoxOutline,
  [GitAction.UNSTAGE]: mdiMinusBoxOutline,
  [GitAction.DISCARD]: mdiUndoVariant,
  [GitAction.IGNORE]: mdiEyeOffOutline,
};

const gitLetters: Record<string, string> = { modified: "M", added: "A", deleted: "D", renamed: "R", untracked: "U", conflicted: "!" };

/** The drag data type of a path dragged in the tree. */
const dragType = "application/x-blitz-path";

/** How long a drag rests on a closed folder before it opens. */
const dragOpenMs = 600;

/** Something being named in the tree: a new file or folder, or a rename. */
type Naming = { kind: "file" | "folder"; dir: string } | { kind: "rename"; path: string };

/**
 * The Files shelf (spec_files_029 §5): the workspace as a tree, loaded a
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
  onClose: () => void;
}) {  // Simple mode: no refresh, collapse, hidden files, git actions or relative paths.
  const advanced = useAdvanced();

  const snack = useSnackbar();
  // Floating over the editor, a click outside or Escape minimizes it.
  const shelf = useRef<HTMLElement>(null);
  useFloatingDismiss(true, shelf, `(max-width: ${filesFloat}px)`, "files", onClose);
  const [tree, setTree] = useState<Tree>(emptyTree);
  const [selected, setSelected] = useState<string | null>(null);
  const [naming, setNaming] = useState<Naming | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number; entry: FileEntry | null } | null>(null);
  const [deleting, setDeleting] = useState<FileEntry | null>(null);
  const [discarding, setDiscarding] = useState<FileEntry | null>(null);
  // The workspace is a git repository (from its listing): git actions are offered.
  const [repo, setRepo] = useState(false);
  const [error, setError] = useState("");
  const list = useRef<HTMLDivElement>(null);
  const treeRef = useRef(tree);
  treeRef.current = tree;

  const load = useCallback(
    async (folder: string) => {
      const res = await files.listDir({ workspace: dir, path: folder, showHidden });
      setTree((tr) => withChildren(tr, folder, res.entries));
      if (folder === "") setRepo(res.repo);
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

  const toggle = async (e: FileEntry, open = !tree.expanded.has(e.path)) => {
    setTree((tr) => setExpanded(tr, e.path, open));
    if (open && !tree.children.has(e.path)) await load(e.path).catch((err) => snack(message(err), { error: true }));
  };

  // Show a path: open its folders, select it, scroll to it.
  useEffect(() => {
    if (!reveal) return;
    (async () => {
      for (const folder of ancestors(reveal.path)) {
        setTree((tr) => setExpanded(tr, folder, true));
        if (!treeRef.current.children.has(folder)) await load(folder).catch(() => undefined);
      }
      setSelected(reveal.path);
      requestAnimationFrame(() => list.current?.querySelector(`[data-path="${CSS.escape(reveal.path)}"]`)?.scrollIntoView({ block: "nearest" }));
    })();
  }, [reveal, load]);

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
    if (!n || !validName(name)) return;
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
      else await files.writeFile({ workspace: dir, path, text: "", version: "" });
      await load(n.dir);
      setSelected(path);
      if (n.kind === "file") onOpen(path);
      else focusRow(path);
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const remove = async (e: FileEntry) => {
    setDeleting(null);
    try {
      await files.deleteFile({ workspace: dir, path: e.path });
      onMoved(e.path, null);
      setSelected((s) => (s === e.path || s?.startsWith(e.path + "/") ? null : s));
      await load(parentOf(e.path));
      snack(t("desktop.files.deleted", { name: e.name }));
    } catch (err) {
      snack(message(err), { error: true });
    }
  };

  // Dragging an entry onto a folder (or a file in it) moves it there; onto
  // the tree's background, to the workspace. A closed folder opens when
  // the drag rests on it.
  const dragging = useRef<string | null>(null);
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
    setDropInto(null);
    stopOpenTimer();
  };

  const onDragOver = (ev: React.DragEvent) => {
    const from = dragging.current;
    if (!from || !ev.dataTransfer.types.includes(dragType)) return;
    const into = folderAt(ev.target);
    if (moveTarget(from, into) === null) {
      setDropInto(null);
      stopOpenTimer();
      return;
    }
    ev.preventDefault();
    ev.dataTransfer.dropEffect = "move";
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
    const to = from === null ? null : moveTarget(from, into);
    if (from === null || to === null) return;
    ev.preventDefault();
    try {
      await files.renameFile({ workspace: dir, from, to });
      onMoved(from, to);
      await load(parentOf(from));
      if (into) setTree((tr) => setExpanded(tr, into, true));
      await load(into);
      setSelected(to);
    } catch (e) {
      snack(message(e), { error: true });
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

  const menuItems = (e: FileEntry | null): MenuEntry[] => {
    const folder = e ? (isFolder(e) ? e.path : parentOf(e.path)) : "";
    const items: MenuEntry[] = [];
    if (e && !isFolder(e)) items.push({ label: t("desktop.files.open"), icon: fileIcon(e.name), onSelect: () => onOpen(e.path) });
    items.push(
      { label: t("desktop.files.new_file"), icon: mdiFilePlusOutline, onSelect: () => void startNew("file", folder) },
      { label: t("desktop.files.new_folder"), icon: mdiFolderPlusOutline, onSelect: () => void startNew("folder", folder) },
    );
    if (e && e.agentRule !== "blocked") {
      const path = isFolder(e) ? `${e.path}/` : e.path;
      items.push(
        "divider",
        { label: t(isFolder(e) ? "desktop.files.add_folder_to_context" : "desktop.files.add_file_to_context"), icon: mdiAt, onSelect: () => addToContext({ dir, path }) },
        { label: t(isFolder(e) ? "desktop.files.new_chat_about_folder" : "desktop.files.new_chat_about"), icon: mdiMessagePlusOutline, onSelect: () => addToContext({ dir, path, fresh: true }) },
      );
    }
    if (e) {
      items.push(
        "divider",
        { label: t("desktop.files.rename"), icon: mdiRenameOutline, detail: "F2", onSelect: () => setNaming({ kind: "rename", path: e.path }) },
        { label: t("desktop.files.delete"), icon: mdiDeleteOutline, danger: true, onSelect: () => setDeleting(e) },
      );
      const git = gitActions({ folder: isFolder(e), git: e.git }, repo);
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
      }
      items.push(
        "divider",
        { label: t("desktop.files.copy_path"), icon: mdiContentCopy, onSelect: () => void copy(absolute(e.path)) },
      );
      if (advanced) items.push({ label: t("desktop.files.copy_relative"), icon: mdiLinkVariant, onSelect: () => void copy(e.path) });
    }
    if (manager !== null) {
      if (!e) items.push("divider");
      items.push(revealItem(e?.path ?? ""));
    }
    return items;
  };

  const shown = rows(tree);

  // Arrows move, Right and Left open and close folders, Enter opens, F2
  // renames, Delete deletes, Escape clears the selection (so new files go
  // in the workspace).
  const onKey = (ev: React.KeyboardEvent) => {
    if (naming) return;
    const i = shown.findIndex((r) => r.entry.path === selected);
    const row = shown[i]?.entry;
    const go = (j: number) => {
      const r = shown[Math.max(0, Math.min(shown.length - 1, j))];
      if (!r) return;
      setSelected(r.entry.path);
      list.current?.querySelector<HTMLElement>(`[data-path="${CSS.escape(r.entry.path)}"]`)?.focus();
    };
    switch (ev.key) {
      case "ArrowDown":
        go(i + 1);
        break;
      case "ArrowUp":
        go(i - 1);
        break;
      case "ArrowRight":
        if (row && isFolder(row)) void toggle(row, true);
        break;
      case "ArrowLeft":
        if (row && isFolder(row) && tree.expanded.has(row.path)) void toggle(row, false);
        else if (row) go(shown.findIndex((r) => r.entry.path === parentOf(row.path)));
        break;
      case "Enter":
        if (row) (isFolder(row) ? void toggle(row) : onOpen(row.path));
        break;
      case "F2":
        if (row) setNaming({ kind: "rename", path: row.path });
        break;
      case "Delete":
      case "Backspace":
        if (row && (ev.key === "Delete" || ev.metaKey)) setDeleting(row);
        break;
      case "Escape":
        if (!selected) return; // the floating shelf's Escape minimizes it
        setSelected(null);
        break;
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
    <aside className="files-shelf" aria-label={t("desktop.files.title")} style={{ width }} ref={shelf}>
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
        <IconButton icon={mdiChevronDoubleLeft} label={t("desktop.files.hide")} small onClick={onClose} />
      </div>
      <div
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
        {shown.map(({ entry: e, depth }) => {
          const open = tree.expanded.has(e.path);
          const renaming = naming?.kind === "rename" && naming.path === e.path;
          return (
            <div key={e.path} role="none">
              {renaming ? (
                <NameInput depth={depth} icon={isFolder(e) ? mdiFolderOutline : fileIcon(e.name)} initial={e.name} label={t("desktop.files.rename")} onDone={finishNaming} />
              ) : (
                <button
                  role="treeitem"
                  aria-expanded={isFolder(e) ? open : undefined}
                  aria-selected={e.path === selected}
                  data-path={e.path}
                  className={`tree-row ${e.path === selected ? "selected" : ""} ${e.path === active ? "active" : ""} ${e.path === dropInto ? "drop-target" : ""} ${e.hidden ? "hidden-entry" : ""} git-${e.git || "clean"}`}
                  style={{ paddingLeft: 8 + depth * 14 }}
                  title={e.agentRule ? `${e.path}\n${t(`desktop.files.rule.${e.agentRule}.detail`)}` : e.path}
                  tabIndex={e.path === selected || (!selected && shown[0]?.entry.path === e.path) ? 0 : -1}
                  draggable
                  onDragStart={(ev) => {
                    dragging.current = e.path;
                    ev.dataTransfer.setData(dragType, e.path);
                    ev.dataTransfer.effectAllowed = "move";
                  }}
                  onDragEnd={endDrag}
                  onClick={() => {
                    setSelected(e.path);
                    if (isFolder(e)) void toggle(e);
                    else onOpen(e.path);
                  }}
                  onContextMenu={(ev) => {
                    ev.preventDefault();
                    setSelected(e.path);
                    setMenu({ x: ev.clientX, y: ev.clientY, entry: e });
                  }}
                >
                  <span className="tree-twisty">{isFolder(e) && <Icon path={open ? mdiChevronDown : mdiChevronRight} size="sm" />}</span>
                  <Icon path={isFolder(e) ? (open ? mdiFolderOpenOutline : mdiFolderOutline) : fileIcon(e.name)} size="sm" className="tree-icon" />
                  <span className="ellipsis tree-name">{e.name}</span>
                  {e.kind === FileKind.SYMLINK && <Icon path={mdiLinkVariant} size="sm" className="muted" />}
                  {e.agentRule && <Icon path={mdiLockOutline} size="sm" className="muted" />}
                  {e.git === "changed" ? <span className="git-dot" /> : e.git && <span className="git-letter">{gitLetters[e.git] ?? ""}</span>}
                </button>
              )}
              {isFolder(e) && open && newRow(e.path, depth + 1)}
            </div>
          );
        })}
        {shown.length === 0 && !error && !naming && <p className="muted t-body-sm files-note">{t("desktop.files.empty")}</p>}
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
      {deleting &&
        createPortal(
          <Dialog
            title={t("desktop.files.delete_title", { name: deleting.name })}
            icon={mdiDeleteOutline}
            onClose={() => setDeleting(null)}
            footer={
              <>
                <Button onClick={() => setDeleting(null)}>{t("desktop.cancel")}</Button>
                <Button variant="filled" danger onClick={() => void remove(deleting)}>
                  {t("desktop.files.delete")}
                </Button>
              </>
            }
          >
            <p>{t(isFolder(deleting) ? "desktop.files.delete_folder_body" : "desktop.files.delete_body", { path: deleting.path })}</p>
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

