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

// Events between parts of the window that don't share state: the command
// palette asks a workspace's composer to take text or run a command, or a
// workspace to show a view.
import type { WebBlock } from "./attachments";

/** The event that asks a workspace's composer to take text, or run a command. */
export const composeEvent = "blitz:compose";
/** What the composer is asked to take. */
export interface ComposeDetail {
  dir: string;
  text: string;
  /** Run it at once (a command, or a prompt, which is sent) instead of filling the composer. */
  run?: boolean;
  /** Add it after what's typed (a paragraph apart) instead of replacing it. */
  append?: boolean;
  /** With run: in a new chat, not the one shown. */
  fresh?: boolean;
}
// Text for a composer that isn't there yet (its workspace is opening),
// taken when it mounts.
const pendingCompose = new Map<string, ComposeDetail>();

/**
 * Asks a workspace's composer to take text (or run a command). Text for a
 * workspace whose composer isn't there yet waits for it.
 */
export function compose(detail: ComposeDetail) {
  const taken = !window.dispatchEvent(new CustomEvent(composeEvent, { detail, cancelable: true }));
  if (!taken && !detail.run) pendingCompose.set(detail.dir, detail);
}

/** The guided setup's command: it interviews the user and writes the agent harness. */
export const setupCommand = "/setup";

/** Runs the guided setup in a new chat in a workspace, showing the chat. */
export function startSetup(dir: string) {
  showView({ dir, view: "chat" });
  compose({ dir, text: setupCommand, run: true, fresh: true });
}

/** The text waiting for a workspace's composer, once. */
export function takePendingCompose(dir: string): ComposeDetail | undefined {
  const d = pendingCompose.get(dir);
  pendingCompose.delete(dir);
  return d;
}

/** The event that opens a web page in a workspace's editor (spec_electron_038 EL-100). */
export const openWebEvent = "blitz:open-web";
/** A web page to open in a workspace's editor ("" asks for its address). */
export interface OpenWebDetail {
  dir: string;
  url: string;
}
/** Opens a web page in a workspace's editor, in a tab of its own. */
export function openWeb(detail: OpenWebDetail) {
  window.dispatchEvent(new CustomEvent(openWebEvent, { detail }));
}

/** The event that adds a web page's content to a workspace's next prompt (EL-104). */
export const addWebEvent = "blitz:add-web";
/** What Add to chat adds: a page's content (block), or a screenshot of it (image, a PNG). */
export interface AddWebDetail {
  dir: string;
  block?: WebBlock;
  image?: { name: string; data: Uint8Array };
}
/** Adds a web page's content, or a screenshot of it, to a workspace's composer. */
export function addWeb(detail: AddWebDetail) {
  window.dispatchEvent(new CustomEvent(addWebEvent, { detail }));
}

/** The event that adds a file or folder to a workspace's next prompt. */
export const addToContextEvent = "blitz:add-to-context";
/** A file or folder for a workspace's next prompt. */
export interface AddToContextDetail {
  dir: string;
  /** Relative to the workspace; a folder ends with a slash. */
  path: string;
  /** Start a new conversation about it first. */
  fresh?: boolean;
}
/** Mentions a path in a workspace's composer (@path), in a new conversation with fresh. */
export function addToContext(detail: AddToContextDetail) {
  window.dispatchEvent(new CustomEvent(addToContextEvent, { detail }));
}

/** The event that asks a workspace for a view: the editor, or the changes or the workers opened over it. */
export const viewEvent = "blitz:view";
/** Which workspace, and which view to show ("chat" and "editor" close what's over the editor). */
export interface ViewDetail {
  dir: string;
  view: "chat" | "editor" | "changes" | "workers" | "workflows";
  /** Which changes: this session's (the default) or git's. */
  source?: "session" | "git";
  /** Studio › Workflows: the workflow to open, and the run to show on it. */
  workflow?: string;
  run?: string;
  /** Studio › Jobs: the job whose details to open. */
  worker?: string;
  /** Closes the view instead when it's the one shown (the status bar's buttons). */
  toggle?: boolean;
}
/**
 * The drawer after a request for the changes: none when it's a toggle and
 * the changes are shown, else the changes from source.
 */
export function changesAfter<D extends { kind: string }, S>(cur: D | null, toggle: boolean | undefined, source: S): D | { kind: "changes"; source: S } | null {
  return toggle && cur?.kind === "changes" ? null : { kind: "changes", source };
}

// Views for a workspace that isn't there yet (it's opening), shown when it
// mounts.
const pendingViews = new Map<string, ViewDetail>();

/** Asks a workspace to show a view. One for a workspace that isn't there yet waits for it. */
export function showView(detail: ViewDetail) {
  const taken = !window.dispatchEvent(new CustomEvent(viewEvent, { detail, cancelable: true }));
  if (!taken) pendingViews.set(detail.dir, detail);
}

/** The view waiting to be shown in a workspace, once. */
export function takePendingView(dir: string): ViewDetail | undefined {
  const v = pendingViews.get(dir);
  pendingViews.delete(dir);
  return v;
}

/** The event that loads a session in a workspace. */
export const loadSessionEvent = "blitz:load-session";
/** Which workspace, and the session's ID. */
export interface LoadSessionDetail {
  dir: string;
  id: string;
}
// Sessions for a workspace that isn't there yet (it's opening), loaded
// when it mounts.
const pendingLoads = new Map<string, string>();

/**
 * Asks a workspace to load a session. One for a workspace that isn't there
 * yet waits for it.
 */
export function loadSession(detail: LoadSessionDetail) {
  const taken = !window.dispatchEvent(new CustomEvent(loadSessionEvent, { detail, cancelable: true }));
  if (!taken) pendingLoads.set(detail.dir, detail.id);
}

/** The session waiting to be loaded in a workspace, once. */
export function takePendingLoad(dir: string): string | undefined {
  const id = pendingLoads.get(dir);
  pendingLoads.delete(dir);
  return id;
}

// The settings changed (a key, a provider, a settings file): workspaces
// ask again whether their model works. dir is the workspace whose own
// settings changed, or "" for the global ones (every workspace).
export const configChangedEvent = "blitz:config-changed";
/** The workspace whose settings changed, or "" for the global ones. */
export interface ConfigChangedDetail {
  dir: string;
}
/** Tells the workspaces their settings changed. */
export function configChanged(detail: ConfigChangedDetail) {
  window.dispatchEvent(new CustomEvent(configChangedEvent, { detail }));
}

// Files (spec_files_029): open a file in a workspace's editor (at a line),
// ask for Go to file, or say that files may have changed (a tool ran, a
// turn ended) so the shelf and editor look again.
export const openFileEvent = "blitz:open-file";
/** The workspace and file to open, and where to put the cursor. */
export interface OpenFileDetail {
  dir: string;
  path: string;
  line?: number;
  column?: number;
  /** Words a search found there: a Markdown file shows its preview, at
   * the line, with them marked. */
  find?: string[];
}
/** Asks a workspace to open a file in its editor. */
export function openFile(detail: OpenFileDetail) {
  window.dispatchEvent(new CustomEvent(openFileEvent, { detail }));
}

/** The event that opens Find in file in a workspace's editor. */
export const findInFileEvent = "blitz:find-in-file";
/** Asks a workspace's editor to find in the file it shows. */
export function findInFile(detail: { dir: string }) {
  window.dispatchEvent(new CustomEvent(findInFileEvent, { detail }));
}

// Headings to scroll to when a Markdown file's preview shows (a link to
// another document's #section), by workspace and path; taken once.
const pendingAnchors = new Map<string, string>();

/** Opens a Markdown file at a heading (its id), shown in its preview. */
export function openFileAt(detail: OpenFileDetail, anchor: string) {
  pendingAnchors.set(`${detail.dir}\0${detail.path}`, anchor);
  openFile(detail);
}

/** The heading a file's preview should scroll to, once. */
export function takeAnchor(dir: string, path: string): string | undefined {
  const key = `${dir}\0${path}`;
  const a = pendingAnchors.get(key);
  pendingAnchors.delete(key);
  return a;
}

/** The event that shows a path in a workspace's file tree. */
export const revealInTreeEvent = "blitz:reveal-in-tree";
/** Asks a workspace to show a file or folder in its tree. */
export function revealInTree(detail: { dir: string; path: string }) {
  window.dispatchEvent(new CustomEvent(revealInTreeEvent, { detail }));
}

/** The event that opens a history (View revisions, FIL-59b). */
export const historyEvent = "blitz:history";
/** What a history is of: a file, a folder, or the workspace (path ""). */
export interface HistoryDetail {
  dir: string;
  path: string;
  folder: boolean;
}
/** Asks a workspace to show the history of a file, a folder or itself. */
export function showHistory(detail: HistoryDetail) {
  window.dispatchEvent(new CustomEvent(historyEvent, { detail }));
}

/** The event that opens Go to file. */
export const goToFileEvent = "blitz:go-to-file";
/** Asks a workspace to open Go to file. */
export function goToFile(detail: { dir: string }) {
  window.dispatchEvent(new CustomEvent(goToFileEvent, { detail }));
}

/** The event that opens workspace search. */
export const searchWorkspaceEvent = "blitz:search-workspace";
/** Opens workspace search on the active workspace, with query typed. */
export function searchWorkspace(query = "") {
  window.dispatchEvent(new CustomEvent(searchWorkspaceEvent, { detail: { query } }));
}

/** The event that says a workspace's files may have changed. */
export const filesTouchedEvent = "blitz:files-touched";
/** Tells a workspace its files may have changed (a tool ran, a turn ended). */
export function filesTouched(detail: { dir: string }) {
  window.dispatchEvent(new CustomEvent(filesTouchedEvent, { detail }));
}

// Show the license dialog (Settings › About, or /license), on one of its texts.
export const showLicenseEvent = "blitz:show-license";
/** Opens the Licenses dialog on one of its texts. */
export function showLicense(detail: { which: "notice" | "full" | "third-party" }) {
  window.dispatchEvent(new CustomEvent(showLicenseEvent, { detail }));
}

/** The event that opens the user docs on a page (docs/DocsDrawer.tsx). */
export const openDocsEvent = "blitz:open-docs";

/**
 * Opens the user docs on a page, by its site path ("" the home page,
 * "guide/models"), at a heading; with no page, where they were left.
 */
export function openDocs(detail: { page?: string; anchor?: string }) {
  window.dispatchEvent(new CustomEvent(openDocsEvent, { detail }));
}

/** The event that opens the Settings dialog on a section (and scope). */
export const openSettingsEvent = "blitz:open-settings";

/** Opens Settings on a section; for the settings file or a workspace's, scope is its directory. */
export function openSettings(detail: { section: string; scope?: string }) {
  window.dispatchEvent(new CustomEvent(openSettingsEvent, { detail }));
}

/** The event that brings a workspace's waiting request (an approval, a question) into view. */
export const showPendingEvent = "blitz:show-pending";

/** Asks a workspace's conversation to show what waits for the user. */
export function showPending(detail: { dir: string }) {
  window.dispatchEvent(new CustomEvent(showPendingEvent, { detail }));
}
