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

import { useSyncExternalStore } from "react";
import type { Pending } from "../waits";

// Voice in the window (spec_063_voice): what the page decides on its own.
// The conversation itself is the service's; the page carries audio and
// does the work the voice hands it, as typed (VOX-11).

/** The microphone button's states (VOX-20). */
export type ButtonState = "hidden" | "not_set_up" | "no_mic" | "ready" | "connecting" | "listening" | "speaking" | "muted";

/** What the page knows of voice: its settings' switch, whether it's set up, the microphone's permission. */
export type VoiceSetup = { enabled: boolean; notSetUp: string; mic: "unknown" | "granted" | "denied" | "none"; summaries?: string };

/** The conversation as the window shows it: one a window (VOX-15). */
export type Conversation = {
  dir: string;
  sessionId: string;
  id: string; // the service's, once started
  phase: "connecting" | "open";
  state: "listening" | "speaking";
  muted: boolean;
  holding: boolean;
  listen: "tap" | "hold";
  model: string;
  voice: string;
  heard: string; // the person's line being heard
  said: string; // the voice's line being said
  error: string;
};

/** The button's state for setup and the conversation, if it's this workspace's (VOX-20). */
export function buttonState(setup: VoiceSetup, conv: Conversation | null): ButtonState {
  if (!setup.enabled) return "hidden";
  if (conv) {
    if (conv.phase === "connecting") return "connecting";
    if (conv.muted) return "muted";
    return conv.state;
  }
  if (setup.notSetUp) return "not_set_up";
  if (setup.mic === "denied" || setup.mic === "none") return "no_mic";
  return "ready";
}

/** What waits on the person, as the service hears of it (VOX-13): the approval card showing, or the question asked. */
export type Waiting = { approvalId: string; approval: string; question: string; options: string[] };

/** Nothing waiting. */
export const nothingWaiting: Waiting = { approvalId: "", approval: "", question: "", options: [] };

// What an approval would do, in words the voice can say after "I need
// permission to" (VOX-13): the tool's action and its target.
const actions: Record<string, string> = {
  run_shell_command: "run",
  create_file: "create",
  replace_in_file: "edit",
  apply_patch: "change",
  delete_file: "delete",
  web_fetch: "open",
  browser: "browse to",
};

/** What an approval would do, in plain words: "run make deploy", "edit cart.go". */
export function actionWords(tool: string, detail: string): string {
  const verb = actions[tool];
  const what = verb ? (detail ? `${verb} ${detail}` : verb) : detail ? `use ${tool} for ${detail}` : `use ${tool}`;
  return what.length > 300 ? what.slice(0, 300) + "…" : what;
}

/**
 * What's waiting, from the conversation's first pending request, else a
 * background task's or a sub-agent's (id: the request's): the voice asks
 * for either.
 */
export function waitingOf(pending: Pending | null, task?: { id: string; approval?: { tool: string; detail: string }; question?: { question: string; options: string[] } }): Waiting {
  if (pending?.kind === "approval") return { ...nothingWaiting, approvalId: pending.req.requestId, approval: actionWords(pending.req.tool, pending.req.detail) };
  if (pending?.kind === "question") return { ...nothingWaiting, question: pending.q.question, options: [...pending.q.options] };
  if (task?.approval) return { ...nothingWaiting, approvalId: task.id, approval: actionWords(task.approval.tool, task.approval.detail) };
  if (task?.question) return { ...nothingWaiting, question: task.question.question, options: [...task.question.options] };
  return nothingWaiting;
}

/** Whether two waits are the same (so the service hears of each change once). */
export const sameWaiting = (a: Waiting, b: Waiting) => a.approvalId === b.approvalId && a.question === b.question && a.approval === b.approval && a.options.join("\n") === b.options.join("\n");

/**
 * The microphone's samples at rate as 16 kHz 16-bit PCM (the Live API's
 * input, VOX-30): averaged down, clipped, little-endian.
 */
export function toPCM16k(input: Float32Array, rate: number): Uint8Array {
  const ratio = rate / 16000;
  const n = Math.floor(input.length / ratio);
  const out = new DataView(new ArrayBuffer(n * 2));
  for (let i = 0; i < n; i++) {
    const from = Math.floor(i * ratio);
    const to = Math.max(from + 1, Math.floor((i + 1) * ratio));
    let sum = 0;
    for (let j = from; j < to && j < input.length; j++) sum += input[j];
    const s = Math.max(-1, Math.min(1, sum / (to - from)));
    out.setInt16(i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true);
  }
  return new Uint8Array(out.buffer);
}

/** The voice's 16-bit little-endian PCM as samples for the speaker. */
export function fromPCM16(bytes: Uint8Array): Float32Array {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength - (bytes.byteLength % 2));
  const out = new Float32Array(view.byteLength / 2);
  for (let i = 0; i < out.length; i++) out[i] = view.getInt16(i * 2, true) / 0x8000;
  return out;
}

/** The input's level, 0 to 1, for the button's pulse and the settings' meter. */
export function level(samples: Float32Array): number {
  let sum = 0;
  for (const s of samples) sum += s * s;
  return Math.min(1, Math.sqrt(sum / Math.max(1, samples.length)) * 4);
}

/** A transcript line with its next piece (pieces bring their own spaces). */
export const extend = (line: string, piece: string) => (line + piece).slice(-400);

// The window's one conversation, shared by the composer, the voice bar and
// the status bar (VOX-15, VOX-23).
let current: Conversation | null = null;
const listeners = new Set<() => void>();

/** The conversation open in the window, or null. */
export const currentConversation = () => current;

/** Replaces the conversation (null: none) and tells who's watching. */
export function setConversation(next: Conversation | null | ((c: Conversation | null) => Conversation | null)) {
  current = typeof next === "function" ? next(current) : next;
  for (const l of listeners) l();
}

/** The conversation open in the window, as a hook. */
export function useConversation(): Conversation | null {
  return useSyncExternalStore(
    (f) => {
      listeners.add(f);
      return () => listeners.delete(f);
    },
    () => current,
  );
}

// Ending the window's conversation, set by whoever runs it (VOX-23: the
// status bar ends it too).
let ender: (() => void) | null = null;
/** Sets how the window's conversation is ended (null: none open). */
export const setEnder = (f: (() => void) | null) => void (ender = f);
/** Ends the window's conversation, if one is open. */
export const endConversation = () => ender?.();
