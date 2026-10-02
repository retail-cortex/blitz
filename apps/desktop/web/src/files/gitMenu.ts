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

// Which git actions the Files tree's menu offers for an entry, from its git
// status as the listing gives it: a file's "modified", "added", "deleted",
// "renamed", "untracked" or "conflicted", or "changed" for a folder with
// changes under it ("" when clean).
import { GitAction } from "../gen/blitz/v1/file_pb";

/** What a git status means for the menu. */
const tracked = new Set(["modified", "deleted", "renamed"]);

/**
 * The git actions for an entry, in menu order: stage what changed, unstage
 * what may be staged, discard tracked changes (a confirmed action), and
 * ignore anything. None outside a repository.
 */
export function gitActions(entry: { folder: boolean; git: string }, repo: boolean): GitAction[] {
  if (!repo) return [];
  const changed = entry.git !== "";
  const out: GitAction[] = [];
  if (changed) out.push(GitAction.STAGE);
  if (changed && entry.git !== "untracked") out.push(GitAction.UNSTAGE);
  if (entry.folder ? entry.git === "changed" : tracked.has(entry.git)) out.push(GitAction.DISCARD);
  out.push(GitAction.IGNORE);
  return out;
}

/** The i18n key of an action's menu label and of its confirmation. */
export const gitKeys: Record<GitAction, { item: string; done: string }> = {
  [GitAction.UNSPECIFIED]: { item: "", done: "" },
  [GitAction.STAGE]: { item: "desktop.files.git.stage", done: "desktop.files.git.staged" },
  [GitAction.UNSTAGE]: { item: "desktop.files.git.unstage", done: "desktop.files.git.unstaged" },
  [GitAction.DISCARD]: { item: "desktop.files.git.discard", done: "desktop.files.git.discarded" },
  [GitAction.IGNORE]: { item: "desktop.files.git.ignore", done: "desktop.files.git.ignored" },
};
