// Events between parts of the window that don't share state: the command
// palette asks a workspace's composer to take text or run a command, or a
// workspace to show a view.

export const composeEvent = "blitz:compose";
export interface ComposeDetail {
  dir: string;
  text: string;
  /** Run it at once (a command without arguments) instead of filling the composer. */
  run?: boolean;
}
export function compose(detail: ComposeDetail) {
  window.dispatchEvent(new CustomEvent(composeEvent, { detail }));
}

export const viewEvent = "blitz:view";
export interface ViewDetail {
  dir: string;
  view: "chat" | "editor" | "changes" | "workers";
}
export function showView(detail: ViewDetail) {
  window.dispatchEvent(new CustomEvent(viewEvent, { detail }));
}

export const loadSessionEvent = "blitz:load-session";
export interface LoadSessionDetail {
  dir: string;
  id: string;
}
export function loadSession(detail: LoadSessionDetail) {
  window.dispatchEvent(new CustomEvent(loadSessionEvent, { detail }));
}

// The settings changed (a key, a provider, a settings file): workspaces
// ask again whether their model works. dir is the workspace whose own
// settings changed, or "" for the global ones (every workspace).
export const configChangedEvent = "blitz:config-changed";
export interface ConfigChangedDetail {
  dir: string;
}
export function configChanged(detail: ConfigChangedDetail) {
  window.dispatchEvent(new CustomEvent(configChangedEvent, { detail }));
}

// Files (spec_files_029): open a file in a workspace's editor (at a line),
// ask for Go to file, or say that files may have changed (a tool ran, a
// turn ended) so the shelf and editor look again.
export const openFileEvent = "blitz:open-file";
export interface OpenFileDetail {
  dir: string;
  path: string;
  line?: number;
  column?: number;
}
export function openFile(detail: OpenFileDetail) {
  window.dispatchEvent(new CustomEvent(openFileEvent, { detail }));
}

export const goToFileEvent = "blitz:go-to-file";
export function goToFile(detail: { dir: string }) {
  window.dispatchEvent(new CustomEvent(goToFileEvent, { detail }));
}

export const filesTouchedEvent = "blitz:files-touched";
export function filesTouched(detail: { dir: string }) {
  window.dispatchEvent(new CustomEvent(filesTouchedEvent, { detail }));
}
