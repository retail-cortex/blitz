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

import type { MutableRefObject } from "react";
import {
  mdiArrowCollapseUp,
  mdiAt,
  mdiBrushVariant,
  mdiConsole,
  mdiContentCopy,
  mdiContentCut,
  mdiContentDuplicate,
  mdiContentPaste,
  mdiDeleteOutline,
  mdiFilePlusOutline,
  mdiFolderOpenOutline,
  mdiFolderPlusOutline,
  mdiFolderSearchOutline,
  mdiFormTextbox,
  mdiHistory,
  mdiLinkVariant,
  mdiMessagePlusOutline,
  mdiOpenInNew,
  mdiPlusBoxMultipleOutline,
  mdiRenameOutline,
  mdiSourceCommit,
  mdiWeb,
} from "@mdi/js";
import type { MenuEntry } from "@blitz/webui";
import { canCall, openURL, revealPath, servePage } from "../desktop";
import { message } from "../errors";
import { addToContext, openWeb, searchWorkspace, showHistory, showView } from "../events";
import { GitAction, type FileEntry } from "../gen/blitz/v1/file_pb";
import { t, tn } from "../i18n";
import { includeAll } from "../includeAll";
import type { Prefs } from "../prefs";
import { gitActions, gitKeys } from "./gitMenu";
import { fileIcon } from "./icons";
import { findEntry, gitIcons, type Naming } from "./shelfTypes";
import { isFolder, parentOf, type Tree } from "./tree";

/** Dependencies needed to build context menu entries for the Files shelf. */
export interface ShelfMenuContext {
  dir: string;
  advanced: boolean;
  repo: boolean;
  manager: string | null;
  tree: Tree;
  treeRef: MutableRefObject<Tree>;
  clip: { paths: string[]; cut: boolean } | null;
  view: { sort: Prefs["files_sort"]; nest: boolean; compact: boolean };
  follow: boolean;
  selection: () => string[];
  isPicked: (path: string) => boolean;
  contextPaths: (paths: string[]) => string[];
  absolute: (path: string) => string;
  copy: (text: string) => void;
  setClip: (clip: { paths: string[]; cut: boolean } | null) => void;
  paste: (into?: string) => Promise<void>;
  duplicate: (paths: string[]) => Promise<void>;
  remove: (paths: string[]) => Promise<void>;
  startNew: (kind: "file" | "folder", folder?: string) => Promise<void>;
  setNaming: (naming: Naming | null) => void;
  setTree: (updater: (tr: Tree) => Tree) => void;
  setDiscarding: (entry: FileEntry | null) => void;
  runGit: (entry: FileEntry, action: GitAction) => Promise<void>;
  onOpen: (path: string) => void;
  onChanged: () => void;
  onCommit?: () => void;
  onTerminal?: (folder: string) => void;
  updatePrefs: (fn: (p: Prefs) => Prefs) => void;
  snack: (text: string, opts?: { error?: boolean }) => void;
}

/** Builds the right-click context menu entries for a file row or the shelf background. */
export function buildShelfMenuItems(e: FileEntry | null, ctx: ShelfMenuContext): MenuEntry[] {
  const {
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
    updatePrefs,
    snack,
  } = ctx;

  const revealItem = (p: string): MenuEntry => ({
    label: manager ? t("desktop.files.reveal_in", { name: manager }) : t("desktop.files.reveal_in_file_manager"),
    icon: mdiFolderOpenOutline,
    onSelect: () => void revealPath(absolute(p)).catch((err) => snack(message(err), { error: true })),
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
    ...(["folders", "mixed", "type"] as const).map((s) => ({ label: t(`desktop.files.sort.${s}`), on: view.sort === s, onSelect: () => updatePrefs((p) => ({ ...p, files_sort: s })) })),
    { heading: t("desktop.files.view.heading") },
    { label: t("desktop.files.view.nest"), on: view.nest, onSelect: () => updatePrefs((p) => ({ ...p, files_nest: !p.files_nest })) },
    { label: t("desktop.files.view.compact"), on: view.compact, onSelect: () => updatePrefs((p) => ({ ...p, files_compact: p.files_compact === "on" ? "off" : "on" })) },
    { label: t("desktop.files.view.follow"), on: follow, onSelect: () => updatePrefs((p) => ({ ...p, files_follow: p.files_follow === "on" ? "off" : "on" })) },
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
  // Search within the folder (FIL-77, SRCH-18).
  if (e && isFolder(e) && e.agentRule !== "blocked") {
    items.push({ label: t("desktop.files.find_in_folder"), icon: mdiFolderSearchOutline, onSelect: () => searchWorkspace("", undefined, e.path) });
  }
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
  // A terminal in the folder, or the file's folder (TRM-31).
  if (onTerminal) items.push({ label: t("desktop.terminal.open_here"), icon: mdiConsole, onSelect: () => onTerminal(folder) });
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
}
