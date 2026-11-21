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
import {
  mdiChevronDoubleLeft,
  mdiClose,
  mdiCollapseAllOutline,
  mdiEyeOffOutline,
  mdiEyeOutline,
  mdiFilePlusOutline,
  mdiFilterVariant,
  mdiFolderOutline,
  mdiFolderPlusOutline,
  mdiLoading,
  mdiRefresh,
} from "@mdi/js";
import { ContextMenu, Icon, IconButton, ResizeHandle, useSnackbar } from "@blitz/webui";
import { files } from "../api";
import { fileManager } from "../desktop";
import { message } from "../errors";
import { type FileEntry } from "../gen/blitz/v1/file_pb";
import { t } from "../i18n";
import { useOrgBranding } from "../orgBranding";
import { useAdvanced, useApp } from "../state";
import { fileIcon } from "./icons";
import { languageSession } from "./language";
import { NameInput, ShelfDialogs } from "./ShelfDialogs";
import { handleShelfKeyDown } from "./shelfKeyHandler";
import { buildShelfMenuItems } from "./shelfMenus";
import { ShelfTreeRow } from "./ShelfTreeRow";
import { findEntry, liveRelistMs, slowOpen, type Naming } from "./shelfTypes";
import {
  ancestors,
  emptyTree,
  isFolder,
  nameOf,
  parentOf,
  problemsUp,
  rows,
  setExpanded,
  shownFolders,
  topmost,
  withChildren,
  type Tree,
} from "./tree";
import { useShelfDragDrop } from "./useShelfDragDrop";
import { useFilesShelfActions } from "./useFilesShelfActions";
import { watchFolders } from "./watch";
import { isWebPath } from "./webTabs";

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
  poll = 0,
  reveal,
  onOpen,
  onMoved,
  onChanged,
  onDiskChange,
  onCommit,
  onTerminal,
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
  /**
   * Changes every few seconds: the tree lists again on it only while the
   * service can't stream its folders' changes (FIL-33b).
   */
  poll?: number;
  /** The service saw files change in the open folders (open tabs may have). */
  onDiskChange?: () => void;
  /** A path to show (its folders opened), then cleared. */
  reveal: { path: string } | null;
  onOpen: (path: string) => void;
  onMoved: (from: string, to: string | null) => void;
  /** Files changed on disk outside the editor (a git action): list again, reload open tabs. */
  onChanged: () => void;
  /** Opens the commit dialog (the git menu's Commit…). */
  onCommit?: () => void;
  /** Opens a terminal in a folder of the workspace ("" for it), spec_062 TRM-31. */
  onTerminal?: (folder: string) => void;
  onClose: () => void;
}) {
  // Simple mode: no refresh, collapse, hidden files, git actions or relative paths.
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

  // The service watches the open folders and says which changed, so the
  // tree lists just those (FIL-33b); while it can't, the tree lists on the
  // poll, and when it can, once a minute anyway.
  const [live, setLive] = useState(false);
  const watched = shownFolders(tree).join("\n");
  const onDiskRef = useRef(onDiskChange);
  onDiskRef.current = onDiskChange;
  useEffect(() => {
    const shown = new Set(watched.split("\n"));
    return watchFolders(dir, [...shown], {
      changed: (folders) => {
        for (const f of folders) if (shown.has(f)) void load(f).catch(() => undefined);
        onDiskRef.current?.();
      },
      live: setLive,
    });
  }, [dir, watched, load]);
  useEffect(() => {
    if (poll && !live) void reloadRef.current();
  }, [poll, live]);
  useEffect(() => {
    if (!live) return;
    const timer = setInterval(() => void reloadRef.current(), liveRelistMs);
    return () => clearInterval(timer);
  }, [live]);

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
    if (follow && active && opened && !isWebPath(active)) void show(active);
  }, [active, follow, opened, show]);

  // The folder new files go in: the selected folder, or the selected file's.
  const { startNew, finishNaming, remove, paste, duplicate, runGit } = useFilesShelfActions({
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
  });

  // Show in Finder / Dolphin / …, in the app only.
  const [manager, setManager] = useState<string | null>(null);
  useEffect(() => {
    void fileManager().then(setManager);
  }, []);

  const copy = (text: string) => navigator.clipboard?.writeText(text).then(() => snack(t("desktop.files.copied")));
  const absolute = (p: string) => (p ? `${dir.replace(/\/+$/, "")}/${p}` : dir);

  // The context paths of paths: a folder's ends in "/".
  const contextPaths = (paths: string[]) =>
    paths.map((p) => {
      const e = findEntry(treeRef.current, p);
      return e && isFolder(e) ? `${p}/` : p;
    });

  const drag = useShelfDragDrop({
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
  });

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
      <div hidden={found === null} className="files-tree" role="list" aria-label={t("desktop.files.filter_results")}>
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
        className={`files-tree ${drag.dropInto === "" ? "drop-target" : ""}`}
        role="tree"
        ref={list}
        onKeyDown={(ev) =>
          handleShelfKeyDown(ev, { naming, shown, selected, picked, clip, tree, anchor, list, typed, selection, setSelected, setLead, setPicked, setNaming, setClip, toggle, onOpen, remove, paste, duplicate })
        }
        // A click on the background clears the selection.
        onClick={(e) => {
          if (e.target === e.currentTarget) setSelected(null);
        }}
        onDragOver={drag.onDragOver}
        onDragLeave={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
            drag.setDropInto(null);
            drag.stopOpenTimer();
          }
        }}
        onDrop={(e) => void drag.onDrop(e)}
        onContextMenu={(e) => {
          if (e.target === e.currentTarget) {
            e.preventDefault();
            setMenu({ x: e.clientX, y: e.clientY, entry: null });
          }
        }}
      >
        {error && <p className="error-text t-body-sm files-note">{error}</p>}
        {newRow("", 0)}
        {shown.map((r, index) => (
          <ShelfTreeRow
            key={r.entry.path}
            row={r}
            shown={shown}
            tree={tree}
            selected={selected}
            picked={picked}
            active={active}
            dropInto={drag.dropInto}
            naming={naming}
            problems={problems}
            clip={clip}
            more={moreAfter.get(index)}
            anchor={anchor}
            isPicked={isPicked}
            onSetSelected={setSelected}
            onSetLead={setLead}
            onSetPicked={setPicked}
            onToggle={toggle}
            onOpen={onOpen}
            onFinishNaming={finishNaming}
            onStartRowDrag={drag.startRowDrag}
            onEndDrag={drag.endDrag}
            onContextMenu={setMenu}
            onShowMore={showMore}
            renderNewRow={newRow}
          />
        ))}
        {!opened && !error && waiting && (
          <p className="muted t-body-sm files-note row" role="status">
            <Icon path={mdiLoading} size="sm" spin />
            {waiting === "slow" ? (org?.name ? t("desktop.files.opening_org", { org: org.name }) : t("desktop.files.opening_slow")) : t("desktop.files.opening")}
          </p>
        )}
        {opened && shown.length === 0 && !error && !naming && <p className="muted t-body-sm files-note">{t("desktop.files.empty")}</p>}
      </div>
      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          items={buildShelfMenuItems(menu.entry, {
            dir,
            advanced,
            repo,
            manager,
            tree,
            treeRef,
            clip,
            view,
            follow,
            selection,
            isPicked,
            contextPaths,
            absolute,
            copy,
            setClip,
            paste,
            duplicate,
            remove,
            startNew,
            setNaming,
            setTree,
            setDiscarding,
            runGit,
            onOpen,
            onChanged,
            onCommit,
            onTerminal,
            updatePrefs: update,
            snack,
          })}
          onClose={() => setMenu(null)}
        />
      )}
      <ShelfDialogs
        discarding={discarding}
        onCancelDiscard={() => setDiscarding(null)}
        onConfirmDiscard={(e, a) => void runGit(e, a)}
        incoming={drag.incoming}
        onCancelIncoming={() => drag.setIncoming(null)}
        onChooseHow={drag.chooseHow}
        onBringIn={(inc, c) => void drag.bringIn(inc, c)}
      />
    </aside>
  );
}
