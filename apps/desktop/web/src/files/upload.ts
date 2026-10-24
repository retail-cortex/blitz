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

// Files and folders dropped on the Files shelf from the computer
// (spec_files_029 FIL-36): read from the drop, then uploaded into a
// workspace folder in chunks, since a request to the service carries at
// most 32 MiB. The bytes go through the API, so this works in the app, in
// a browser and with a service elsewhere.
import { joinPath } from "./tree";

/** What was dropped: a top-level file or folder, and what's in it. */
export interface DropItem {
  /** Its place in the drop's files (dropEntries' files). */
  index: number;
  /** Its name, as dropped. */
  name: string;
  /** The file; undefined for a folder. */
  file?: File;
  /** A folder's files and folders, relative to it ("a/b.txt"); folders end in "/". */
  inside: { path: string; file?: File }[];
}

/** How big a chunk of a file goes in one UploadFile call. */
export const chunkBytes = 8 << 20;

/** What a conflict with a name already in the folder does. */
export type OnConflict = "replace" | "keep" | "skip";

/** The calls an upload makes (FileService's). */
export interface UploadClient {
  uploadFile(r: { workspace: string; path: string; uploadId: string; data: Uint8Array; done: boolean; replace: boolean }): Promise<{ uploadId: string; version: string }>;
  createFolder(r: { workspace: string; path: string }): Promise<unknown>;
  deleteFile(r: { workspace: string; path: string }): Promise<unknown>;
}

/**
 * The drop's entries, read while its event lasts (a DataTransfer is
 * empty once the handler returns); null when nothing dropped is a file
 * from the computer.
 */
export function dropEntries(dt: DataTransfer): { entries: (FileSystemEntry | File)[]; files: File[] } | null {
  const files = Array.from(dt.files);
  if (files.length === 0) return null;
  const items = Array.from(dt.items ?? []).filter((i) => i.kind === "file");
  const entries = items.map((i) => i.webkitGetAsEntry?.() ?? null);
  // Without entries (a browser without them), the files themselves: a
  // folder then fails as it's read.
  if (entries.length !== files.length || entries.some((e) => e === null)) return { entries: files, files };
  return { entries: entries as FileSystemEntry[], files };
}

/** Reads the dropped entries into items, folders walked. */
export async function readDrop(entries: (FileSystemEntry | File)[]): Promise<DropItem[]> {
  const out: DropItem[] = [];
  for (const [index, e] of entries.entries()) {
    if (e instanceof File) out.push({ index, name: e.name, file: e, inside: [] });
    else if (e.isFile) out.push({ index, name: e.name, file: await fileOf(e as FileSystemFileEntry), inside: [] });
    else if (e.isDirectory) out.push({ index, name: e.name, inside: await walk(e as FileSystemDirectoryEntry, "") });
  }
  return out;
}

const fileOf = (e: FileSystemFileEntry) => new Promise<File>((resolve, reject) => e.file(resolve, reject));

async function walk(dir: FileSystemDirectoryEntry, prefix: string): Promise<{ path: string; file?: File }[]> {
  const reader = dir.createReader();
  const out: { path: string; file?: File }[] = [];
  // readEntries gives a batch at a time, and none at the end.
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
    if (batch.length === 0) return out;
    for (const e of batch) {
      const path = prefix + e.name;
      if (e.isFile) out.push({ path, file: await fileOf(e as FileSystemFileEntry) });
      else if (e.isDirectory) out.push({ path: path + "/" }, ...(await walk(e as FileSystemDirectoryEntry, path + "/")));
    }
  }
}

/** The first few of names, for a dialog: "a, b, c, …". */
export function someNames(names: string[], max = 4): string {
  return names.length > max ? `${names.slice(0, max).join(", ")}, …` : names.join(", ");
}

/** name, or "name 2.ext", "name 3.ext"… : the first not in taken. */
export function keepBoth(name: string, taken: ReadonlySet<string>): string {
  if (!taken.has(name)) return name;
  const dot = name.lastIndexOf(".");
  const [stem, ext] = dot > 0 ? [name.slice(0, dot), name.slice(dot)] : [name, ""];
  for (let n = 2; ; n++) {
    const next = `${stem} ${n}${ext}`;
    if (!taken.has(next)) return next;
  }
}

/** Uploads file to path in chunks; returns its version. */
export async function uploadFile(c: UploadClient, workspace: string, path: string, file: Blob, replace: boolean): Promise<string> {
  let uploadId = "";
  let offset = 0;
  for (;;) {
    const end = Math.min(offset + chunkBytes, file.size);
    const data = new Uint8Array(await file.slice(offset, end).arrayBuffer());
    const done = end >= file.size;
    const res = await c.uploadFile({ workspace, path, uploadId, data, done, replace });
    if (done) return res.version;
    uploadId = res.uploadId;
    offset = end;
  }
}

/**
 * Puts the items in the workspace folder into, whose names are taken: a
 * name already there is replaced (a folder with everything in it), kept
 * beside as "name 2", or skipped. Returns the paths added, and the items
 * that were (all their files uploaded).
 */
export async function importDrop(c: UploadClient, workspace: string, into: string, items: DropItem[], taken: ReadonlySet<string>, onConflict: OnConflict): Promise<{ added: string[]; done: DropItem[] }> {
  const names = new Set(taken);
  const added: string[] = [];
  const done: DropItem[] = [];
  for (const item of items) {
    let name = item.name;
    const exists = names.has(name);
    if (exists && onConflict === "skip") continue;
    if (exists && onConflict === "keep") name = keepBoth(name, names);
    names.add(name);
    const path = joinPath(into, name);
    const replace = exists && onConflict === "replace";
    if (item.file) {
      await uploadFile(c, workspace, path, item.file, replace);
    } else {
      if (replace) await c.deleteFile({ workspace, path });
      await c.createFolder({ workspace, path });
      for (const x of item.inside) {
        const sub = joinPath(path, x.path.replace(/\/$/, ""));
        if (x.file) await uploadFile(c, workspace, sub, x.file, false);
        else await c.createFolder({ workspace, path: sub });
      }
    }
    added.push(path);
    done.push(item);
  }
  return { added, done };
}
