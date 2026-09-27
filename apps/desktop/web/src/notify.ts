// System notifications: when the agent finishes a longer turn, or waits
// for an approval or an answer, while the user is looking elsewhere.
import { notifyNative } from "./desktop";

export type NotifyKind = "finished" | "waiting";

/** A finished turn notifies only if it took at least this long. */
export const finishedAfterMs = 10_000;

/**
 * Whether to notify: notifications are on, and the user isn't looking at
 * this workspace (the window isn't focused, or another workspace is
 * shown). A finished turn must also have taken a while.
 */
export function shouldNotify(kind: NotifyKind, s: { enabled: boolean; focused: boolean; shown: boolean; elapsedMs: number }): boolean {
  if (!s.enabled || (s.focused && s.shown)) return false;
  return kind === "waiting" || s.elapsedMs >= finishedAfterMs;
}

/** Sends a notification; a click opens dir's workspace. Failures are ignored. */
export function notify(title: string, body: string, dir: string) {
  notifyNative(title, body.length > 180 ? body.slice(0, 179) + "…" : body, dir).catch(() => {});
}
