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
  view: "chat" | "changes" | "workers";
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
