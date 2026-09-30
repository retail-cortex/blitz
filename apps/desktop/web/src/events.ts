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

export const composeEvent = "blitz:compose";
/** What the composer is asked to take. */
export interface ComposeDetail {
  dir: string;
  text: string;
  /** Run it at once (a command without arguments) instead of filling the composer. */
  run?: boolean;
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

/** The text waiting for a workspace's composer, once. */
export function takePendingCompose(dir: string): ComposeDetail | undefined {
  const d = pendingCompose.get(dir);
  pendingCompose.delete(dir);
  return d;
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

/** The event that switches a workspace's view. */
export const viewEvent = "blitz:view";
/** Which workspace, and which view to show ("chat" is always shown, beside the others). */
export interface ViewDetail {
  dir: string;
  view: "chat" | "editor" | "changes" | "workers";
}
/** Asks a workspace to show a view. */
export function showView(detail: ViewDetail) {
  window.dispatchEvent(new CustomEvent(viewEvent, { detail }));
}

/** The event that loads a session in a workspace. */
export const loadSessionEvent = "blitz:load-session";
/** Which workspace, and the session's ID. */
export interface LoadSessionDetail {
  dir: string;
  id: string;
}
/** Asks a workspace to load a session. */
export function loadSession(detail: LoadSessionDetail) {
  window.dispatchEvent(new CustomEvent(loadSessionEvent, { detail }));
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
}
/** Asks a workspace to open a file in its editor. */
export function openFile(detail: OpenFileDetail) {
  window.dispatchEvent(new CustomEvent(openFileEvent, { detail }));
}

/** The event that opens Go to file. */
export const goToFileEvent = "blitz:go-to-file";
/** Asks a workspace to open Go to file. */
export function goToFile(detail: { dir: string }) {
  window.dispatchEvent(new CustomEvent(goToFileEvent, { detail }));
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
