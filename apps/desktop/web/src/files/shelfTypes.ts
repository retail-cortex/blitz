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

import {
  mdiEyeOffOutline,
  mdiEyeOutline,
  mdiMinusBoxOutline,
  mdiPlusBoxOutline,
  mdiSourceBranch,
  mdiUndoVariant,
} from "@mdi/js";
import { GitAction, type FileEntry } from "../gen/blitz/v1/file_pb";
import { parentOf, type Tree } from "./tree";
import type { DropItem } from "./upload";

/** slowOpen is how long opening goes before the list says what it waits for. */
export const slowOpen = 1500;

/** How often a watched tree lists everything again anyway (FIL-33b). */
export const liveRelistMs = 60_000;

/** How long a drag rests on a closed folder before it opens. */
export const dragOpenMs = 600;

/** Material icons for each git file action. */
export const gitIcons: Record<GitAction, string> = {
  [GitAction.UNSPECIFIED]: mdiSourceBranch,
  [GitAction.STAGE]: mdiPlusBoxOutline,
  [GitAction.UNSTAGE]: mdiMinusBoxOutline,
  [GitAction.DISCARD]: mdiUndoVariant,
  [GitAction.IGNORE]: mdiEyeOffOutline,
  [GitAction.UNIGNORE]: mdiEyeOutline,
};

/** Single-letter badges for git status states in the tree. */
export const gitLetters: Record<string, string> = { modified: "M", added: "A", deleted: "D", renamed: "R", untracked: "U", conflicted: "!" };

/** p with forward slashes, to compare a path on Windows with the workspace's. */
export const slashes = (p: string) => p.replace(/\\/g, "/");

/**
 * Files and folders dropped from the computer, waiting on the user
 * (FIL-36): how (copy or move, in the app) and, when names are taken,
 * what to do with them.
 */
export type Incoming = {
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
export type Naming = { kind: "file" | "folder"; dir: string } | { kind: "rename"; path: string };

/** Looks up a file entry in the loaded tree by its workspace-relative path. */
export function findEntry(tree: Tree, path: string): FileEntry | undefined {
  return tree.children.get(parentOf(path))?.find((e) => e.path === path);
}
