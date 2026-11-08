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

/** The tree's state: the folders loaded so far, and which are open. */
export interface Tree {
  /** Each loaded folder's entries, by the folder's path. */
  children: ReadonlyMap<string, FileEntry[]>;
  /** The open folders. */
  expanded: ReadonlySet<string>;
}

/** A tree with nothing loaded. */
export const emptyTree: Tree = { children: new Map(), expanded: new Set() };

/** A row shown in the tree: an entry and how deep it is. */
export interface Row {
  entry: FileEntry;
  depth: number;
  /**
   * Compact folders (FIL-47): the folders a single-child chain shows on
   * this row, the first one first; entry is the last. Absent for a row
   * of its own.
   */
  chain?: FileEntry[];
  /** The files nested under this one (FIL-48): it opens like a folder. */
  nests?: number;
}

/** Whether an entry is a folder. */
export const isFolder = (e: Pick<FileEntry, "kind">) => e.kind === FileKind.FOLDER;

/** How a folder's entries are ordered (FIL-48). */
export type SortOrder = "folders" | "mixed" | "type";

/** How the rows are shown (FIL-47, FIL-48). */
export interface RowOptions {
  sort?: SortOrder;
  /** Related files under the one they belong to (tests under code, locks under manifests). */
  nest?: boolean;
  /** Single-child folder chains on one row. */
  compact?: boolean;
}

const byName = (a: FileEntry, b: FileEntry) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" });
const extOf = (name: string) => {
  const i = name.lastIndexOf(".");
  return i > 0 ? name.slice(i + 1).toLowerCase() : "";
};

/** entries in the order sort says: folders first (the default), mixed, or by type. */
export function sortEntries(entries: FileEntry[], sort: SortOrder = "folders"): FileEntry[] {
  const out = [...entries];
  out.sort((a, b) => {
    if (sort !== "mixed" && isFolder(a) !== isFolder(b)) return isFolder(a) ? -1 : 1;
    if (sort === "type" && !isFolder(a) && !isFolder(b)) {
      const c = extOf(a.name).localeCompare(extOf(b.name));
      if (c !== 0) return c;
    }
    return byName(a, b);
  });
  return out;
}

// What nests under what (FIL-48): a file's tests and generated companions
// under it, a manifest's lock files under the manifest.
const nestRules: { parent: RegExp; children: (m: RegExpMatchArray) => string[] }[] = [
  { parent: /^(.+)\.(ts|tsx|js|jsx|mjs|cjs)$/, children: (m) => [`${m[1]}.test.${m[2]}`, `${m[1]}.spec.${m[2]}`, `${m[1]}.d.ts`, `${m[1]}.${m[2]}.map`] },
  { parent: /^(.+)\.go$/, children: (m) => [`${m[1]}_test.go`] },
  { parent: /^(.+)\.py$/, children: (m) => [`${m[1]}_test.py`, `test_${m[1]}.py`] },
  { parent: /^package\.json$/, children: () => ["package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb", "pnpm-workspace.yaml", ".npmrc"] },
  { parent: /^go\.mod$/, children: () => ["go.sum", "go.work", "go.work.sum"] },
  { parent: /^MODULE\.bazel$/, children: () => ["MODULE.bazel.lock"] },
  { parent: /^pyproject\.toml$/, children: () => ["uv.lock", "poetry.lock", "requirements.txt"] },
  { parent: /^Cargo\.toml$/, children: () => ["Cargo.lock"] },
  { parent: /^Gemfile$/, children: () => ["Gemfile.lock"] },
];

/**
 * The file each file of a folder nests under, by name (FIL-48): only
 * files, under a file of the same folder; a file that has nested files
 * isn't nested itself.
 */
export function nestParents(entries: FileEntry[]): Map<string, string> {
  const files = new Set(entries.filter((e) => !isFolder(e)).map((e) => e.name));
  const out = new Map<string, string>();
  for (const name of files) {
    for (const rule of nestRules) {
      const m = name.match(rule.parent);
      if (!m) continue;
      for (const child of rule.children(m)) if (child !== name && files.has(child) && !out.has(child)) out.set(child, name);
    }
  }
  for (const parent of new Set(out.values())) out.delete(parent);
  return out;
}

/** The rows shown: the workspace's entries, and each open folder's under it. */
export function rows(tree: Tree, dir = "", depth = 0, opts: RowOptions = {}): Row[] {
  const out: Row[] = [];
  const entries = sortEntries(tree.children.get(dir) ?? [], opts.sort);
  const parents = opts.nest ? nestParents(entries) : new Map<string, string>();
  const nested = new Map<string, FileEntry[]>();
  for (const e of entries) {
    const p = parents.get(e.name);
    if (p) nested.set(p, [...(nested.get(p) ?? []), e]);
  }
  for (const first of entries) {
    if (parents.has(first.name)) continue;
    if (!isFolder(first)) {
      const kids = nested.get(first.name);
      out.push(kids ? { entry: first, depth, nests: kids.length } : { entry: first, depth });
      if (kids && tree.expanded.has(first.path)) for (const k of kids) out.push({ entry: k, depth: depth + 1 });
      continue;
    }
    // A chain of folders each holding only the next (once loaded and open).
    let entry = first;
    const chain = [first];
    while (opts.compact && tree.expanded.has(entry.path)) {
      const kids = tree.children.get(entry.path);
      if (!kids || kids.length !== 1 || !isFolder(kids[0])) break;
      entry = kids[0];
      chain.push(entry);
    }
    out.push(chain.length > 1 ? { entry, depth, chain } : { entry, depth });
    if (tree.expanded.has(entry.path)) out.push(...rows(tree, entry.path, depth + 1, opts));
  }
  return out;
}

/** A row's label: its name, or a compacted chain's names joined by "/". */
export function rowLabel(r: Row): string {
  return r.chain ? r.chain.map((e) => e.name).join("/") : r.entry.name;
}

/** Whether name can name a new file or folder in a folder: a path like a/b/c.md, each part a valid name (FIL-49). */
export function validNewName(name: string): boolean {
  const parts = name.trim().split("/");
  return parts.every((p) => validName(p));
}

/**
 * The row type-to-find goes to (FIL-73): the next one after from whose
 * name starts with prefix (case aside), wrapping round; -1 for none.
 */
export function nextMatch(shown: Row[], from: number, prefix: string): number {
  const q = prefix.toLowerCase();
  if (!q) return -1;
  for (let k = 1; k <= shown.length; k++) {
    const i = (from + k + shown.length) % shown.length;
    if (rowLabel(shown[i]).toLowerCase().startsWith(q)) return i;
  }
  return -1;
}

/** The paths of the rows from a to b (either order), for Shift's selection (FIL-74). */
export function rangeOf(shown: Row[], a: string, b: string): string[] {
  const i = shown.findIndex((r) => r.entry.path === a);
  const j = shown.findIndex((r) => r.entry.path === b);
  if (i < 0 || j < 0) return j >= 0 ? [b] : [];
  return shown.slice(Math.min(i, j), Math.max(i, j) + 1).map((r) => r.entry.path);
}

/**
 * The selected paths an action takes (FIL-74): without the ones under a
 * selected folder (the folder takes them with it).
 */
export function topmost(paths: Iterable<string>): string[] {
  const all = [...new Set(paths)].sort();
  return all.filter((p) => !all.some((q) => q !== p && p.startsWith(q + "/")));
}

/**
 * The name a copy of name takes beside the names taken (FIL-38):
 * "a copy.md", then "a copy 2.md", …
 */
export function copyName(name: string, taken: ReadonlySet<string>): string {
  if (!taken.has(name)) return name;
  const dot = name.lastIndexOf(".");
  const [base, ext] = dot > 0 ? [name.slice(0, dot), name.slice(dot)] : [name, ""];
  for (let n = 1; ; n++) {
    const c = `${base} copy${n > 1 ? ` ${n}` : ""}${ext}`;
    if (!taken.has(c)) return c;
  }
}

/**
 * Each file's and folder's worst problem (FIL-46): the files' own, and
 * each folder the worst of what's under it.
 */
export function problemsUp(files: ReadonlyMap<string, "error" | "warning">): Map<string, "error" | "warning"> {
  const out = new Map(files);
  for (const [path, sev] of files) {
    for (const folder of ancestors(path)) if (out.get(folder) !== "error") out.set(folder, sev === "error" ? "error" : (out.get(folder) ?? "warning"));
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

/** The path of name in the folder dir ("" is the workspace). */
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

/** Opens or closes a folder; closing one closes the folders under it too. */
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

/**
 * Where moving path into the folder into puts it, or null when it can't go
 * there: onto itself, under itself, or the folder it's already in.
 */
export function moveTarget(path: string, into: string): string | null {
  if (!path || into === path || into.startsWith(path + "/") || parentOf(path) === into) return null;
  return joinPath(into, nameOf(path));
}

/** The folders that must be open to show path. */
export function ancestors(path: string): string[] {
  const parts = path.split("/");
  return parts.slice(0, -1).map((_, i) => parts.slice(0, i + 1).join("/"));
}

/** The drag data type of a workspace path dragged from the tree (the editor takes it too). */
export const dragType = "application/x-blitz-path";

/**
 * What the tree is dragging, for the chat to take as context (FIL-76): in
 * the app a row's drag is the computer's own (its files), which says
 * nothing of the workspace, so the tree says here what it is. Paths are
 * the context's: a folder's ends in "/".
 */
export const treeDrag: { current: { dir: string; paths: string[] } | null } = { current: null };
