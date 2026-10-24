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

// Files dropped or pasted into a Markdown document (spec_visual_editor_037
// VE-11a): a workspace file is linked where it is, relative to the
// document; a file from the computer is saved beside the document first
// (in its images/ folder if it has one). Pictures become images, other
// files links.
import { files } from "../api";
import { droppedPaths } from "../desktop";
import { FileKind } from "../gen/blitz/v1/file_pb";
import { keepBoth, uploadFile, type UploadClient } from "./upload";
import { dragType, joinPath, parentOf } from "./tree";

/** The picture file extensions an image can be made of. */
export const imageExtensions = ["png", "jpg", "jpeg", "gif", "webp", "svg", "avif", "bmp", "ico"];

/** Whether the file at path is a picture, by its extension. */
export function isImagePath(path: string): boolean {
  const dot = path.lastIndexOf(".");
  return dot >= 0 && imageExtensions.includes(path.slice(dot + 1).toLowerCase());
}

/** The link from the document at docPath to the workspace file target, both workspace-relative. */
export function relativeLink(docPath: string, target: string): string {
  const from = parentOf(docPath).split("/").filter(Boolean);
  const to = target.split("/").filter(Boolean);
  let same = 0;
  while (same < from.length && same < to.length - 1 && from[same] === to[same]) same++;
  const up = from.slice(same).map(() => "..");
  return [...up, ...to.slice(same)].map(encodeURIComponent).join("/");
}

/** A picture's description from its file name: "team-photo_2.png" is "team photo 2". */
export function altText(name: string): string {
  const base = name.split("/").pop() ?? name;
  const dot = base.lastIndexOf(".");
  return (dot > 0 ? base.slice(0, dot) : base).replace(/[-_]+/g, " ").trim();
}

/** What a dropped file becomes in the document. */
export interface Insert {
  kind: "image" | "link";
  /** The link, relative to the document. */
  src: string;
  /** An image's description, or a link's text. */
  text: string;
}

/** ins as Markdown, one per line. */
export function insertMarkdown(ins: Insert[]): string {
  const esc = (t: string) => t.replace(/[[\]\\]/g, "\\$&");
  return ins.map((i) => `${i.kind === "image" ? "!" : ""}[${esc(i.text)}](${i.src})`).join("\n");
}

/** text with a space before and after where its neighbours aren't space (or the line's ends) already. */
export function spaced(before: string, text: string, after: string): string {
  const gap = (c: string) => c !== "" && !/\s/.test(c);
  return (gap(before) ? " " : "") + text + (gap(after) ? " " : "");
}

/** A name for a picture pasted without one (a screenshot): "pasted-2026-10-23-0915.png". */
export function pastedName(type: string, now = new Date()): string {
  const ext = { "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp", "image/svg+xml": "svg" }[type] ?? "png";
  const p = (n: number) => String(n).padStart(2, "0");
  return `pasted-${now.getFullYear()}-${p(now.getMonth() + 1)}-${p(now.getDate())}-${p(now.getHours())}${p(now.getMinutes())}.${ext}`;
}

/**
 * What was dropped, read while the event lasts: workspace paths dragged
 * from the tree (in a browser), and files (from the computer, or from
 * the tree in the app, whose drags are the computer's).
 */
export function readDocDrop(dt: DataTransfer): { paths: string[]; files: File[] } | null {
  const path = dt.getData(dragType);
  if (path) return { paths: [path], files: [] };
  const files = Array.from(dt.files);
  return files.length > 0 ? { paths: [], files } : null;
}

/**
 * The workspace path of the file at abs on the computer, if it's in the
 * workspace dir; null otherwise.
 */
export function inWorkspace(dir: string, abs: string): string | null {
  const slash = (p: string) => p.replace(/\\/g, "/").replace(/\/+$/, "");
  const root = slash(dir);
  const p = slash(abs);
  return p.startsWith(root + "/") ? p.slice(root.length + 1) : null;
}

/** The folder a file from the computer is saved in for the document at docPath: its images/ folder if it has one, else its own. */
export async function saveFolder(list: (folder: string) => Promise<{ name: string; folder: boolean }[]>, docPath: string): Promise<{ folder: string; taken: Set<string> }> {
  const here = parentOf(docPath);
  const entries = await list(here);
  if (entries.some((e) => e.name === "images" && e.folder)) {
    const inside = joinPath(here, "images");
    return { folder: inside, taken: new Set((await list(inside)).map((e) => e.name)) };
  }
  return { folder: here, taken: new Set(entries.map((e) => e.name)) };
}

/**
 * What a drop (or a paste: files only) into the document at docPath
 * inserts: workspace files linked where they are, the computer's saved
 * beside the document first. names gives a pasted file without a name
 * its name.
 */
export async function insertsFor(
  c: UploadClient,
  list: (folder: string) => Promise<{ name: string; folder: boolean }[]>,
  dir: string,
  docPath: string,
  drop: { paths: string[]; files: File[] },
  where = droppedPaths,
): Promise<Insert[]> {
  const linked = [...drop.paths];
  const outside: File[] = [];
  const abs = drop.files.length > 0 ? await where(drop.files) : [];
  drop.files.forEach((f, i) => {
    const rel = abs[i] ? inWorkspace(dir, abs[i]) : null;
    if (rel !== null) linked.push(rel);
    else outside.push(f);
  });
  const saved: string[] = [];
  if (outside.length > 0) {
    const { folder, taken } = await saveFolder(list, docPath);
    for (const f of outside) {
      const name = keepBoth(f.name && f.name !== "image.png" ? f.name : pastedName(f.type), taken);
      taken.add(name);
      const path = joinPath(folder, name);
      await uploadFile(c, dir, path, f, false);
      saved.push(path);
    }
  }
  return [...linked, ...saved].map((p) => {
    const name = p.split("/").pop() ?? p;
    return isImagePath(p) ? { kind: "image", src: relativeLink(docPath, p), text: altText(name) } : { kind: "link", src: relativeLink(docPath, p), text: name };
  });
}

/** Lists a workspace folder's names for saveFolder, hidden ones included. */
export function folderLister(dir: string) {
  return async (folder: string) => (await files.listDir({ workspace: dir, path: folder, showHidden: true })).entries.map((e) => ({ name: e.name, folder: e.kind === FileKind.FOLDER }));
}

/** insertsFor in the workspace dir, through the service. */
export const insertsIn = (dir: string, docPath: string, drop: { paths: string[]; files: File[] }) => insertsFor(files, folderLister(dir), dir, docPath, drop);
