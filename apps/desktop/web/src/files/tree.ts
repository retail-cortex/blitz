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

// The Files shelf's tree, as pure functions over its state: the folders
// loaded so far and which are open. Paths are workspace-relative with "/";
// "" is the workspace itself.
import { FileKind, type FileEntry } from "../gen/blitz/v1/file_pb";

export interface Tree {
  /** Each loaded folder's entries, by the folder's path. */
  children: ReadonlyMap<string, FileEntry[]>;
  /** The open folders. */
  expanded: ReadonlySet<string>;
}

export const emptyTree: Tree = { children: new Map(), expanded: new Set() };

export interface Row {
  entry: FileEntry;
  depth: number;
}

export const isFolder = (e: Pick<FileEntry, "kind">) => e.kind === FileKind.FOLDER;

/** The rows shown: the workspace's entries, and each open folder's under it. */
export function rows(tree: Tree, dir = "", depth = 0): Row[] {
  const out: Row[] = [];
  for (const entry of tree.children.get(dir) ?? []) {
    out.push({ entry, depth });
    if (isFolder(entry) && tree.expanded.has(entry.path)) out.push(...rows(tree, entry.path, depth + 1));
  }
  return out;
}

/** The folder a path is in ("" for the workspace). */
export function parentOf(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? "" : path.slice(0, i);
}

/** The last part of a path. */
export function nameOf(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

export function joinPath(dir: string, name: string): string {
  return dir ? `${dir}/${name}` : name;
}

/** Whether name can name a file or folder (no separators, not . or ..). */
export function validName(name: string): boolean {
  const n = name.trim();
  return n !== "" && n !== "." && n !== ".." && !/[/\\\0]/.test(n);
}

/** The tree with a folder's entries (re)loaded. */
export function withChildren(tree: Tree, dir: string, entries: FileEntry[]): Tree {
  const children = new Map(tree.children);
  children.set(dir, entries);
  // Folders that are gone are forgotten, with what was under them.
  const folders = new Set(entries.filter(isFolder).map((e) => e.path));
  const prefix = dir ? dir + "/" : "";
  // A path under dir is gone when the folder of dir's it's in is.
  const gone = (p: string) => p !== dir && p.startsWith(prefix) && !folders.has(prefix + p.slice(prefix.length).split("/")[0]);
  for (const p of [...children.keys()]) if (gone(p)) children.delete(p);
  const expanded = new Set([...tree.expanded].filter((p) => !gone(p)));
  return { children, expanded };
}

export function setExpanded(tree: Tree, path: string, open: boolean): Tree {
  const expanded = new Set(tree.expanded);
  if (open) expanded.add(path);
  else for (const p of [...expanded]) if (p === path || p.startsWith(path + "/")) expanded.delete(p);
  return { ...tree, expanded };
}

/** The folders to list again on a refresh: the workspace and every open folder shown. */
export function shownFolders(tree: Tree): string[] {
  const out = [""];
  const visit = (dir: string) => {
    for (const e of tree.children.get(dir) ?? []) {
      if (isFolder(e) && tree.expanded.has(e.path)) {
        out.push(e.path);
        visit(e.path);
      }
    }
  };
  visit("");
  return out;
}

/** The folders that must be open to show path. */
export function ancestors(path: string): string[] {
  const parts = path.split("/");
  return parts.slice(0, -1).map((_, i) => parts.slice(0, i + 1).join("/"));
}
