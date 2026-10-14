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

// /loop in the window (spec_desktop_024 DSK-102), as the terminal has it:
// a prompt sent again every interval while the window stays open, per
// workspace. A loop whose time comes while a turn runs waits for the next.
// Workers are the way to run something after the window closes.

/** The shortest interval, as in the terminal. */
export const minLoopMs = 60_000;

/** A running loop. */
export interface Loop {
  n: number;
  prompt: string;
  everyMs: number;
  next: number; // when it fires next (ms since the epoch)
}

type Running = Loop & { timer: ReturnType<typeof setInterval> };

const byDir = new Map<string, Running[]>();
const counters = new Map<string, number>();

/**
 * Reads an interval as the terminal does: a number of minutes, or 1h30m,
 * 10m, 90s and the like; undefined when it isn't one or is under a minute.
 */
export function parseInterval(s: string): number | undefined {
  if (/^\d+$/.test(s)) return Number(s) * 60_000 || undefined;
  const m = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/.exec(s);
  if (!m || !s) return undefined;
  const ms = (Number(m[1] ?? 0) * 3600 + Number(m[2] ?? 0) * 60 + Number(m[3] ?? 0)) * 1000;
  return ms >= minLoopMs ? ms : undefined;
}

/** An interval as the terminal writes it: 1h30m, 10m, 90s. */
export function formatInterval(ms: number): string {
  const s = Math.round(ms / 1000);
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), r = s % 60;
  return `${h ? `${h}h` : ""}${m ? `${m}m` : ""}${r ? `${r}s` : ""}` || "0s";
}

/**
 * Starts a loop in dir that calls fire(prompt) every everyMs, unless busy()
 * says a turn is running then; returns its number.
 */
export function startLoop(dir: string, prompt: string, everyMs: number, fire: (prompt: string) => void, busy: () => boolean, now = Date.now): number {
  const n = (counters.get(dir) ?? 0) + 1;
  counters.set(dir, n);
  const loop: Running = {
    n,
    prompt,
    everyMs,
    next: now() + everyMs,
    timer: setInterval(() => {
      loop.next = now() + everyMs;
      if (!busy()) fire(prompt);
    }, everyMs),
  };
  byDir.set(dir, [...(byDir.get(dir) ?? []), loop]);
  return n;
}

/** dir's loops, in the order they started. */
export function listLoops(dir: string): Loop[] {
  return (byDir.get(dir) ?? []).map(({ n, prompt, everyMs, next }) => ({ n, prompt, everyMs, next }));
}

/** Stops dir's loop n; false when there's none. */
export function stopLoop(dir: string, n: number): boolean {
  const list = byDir.get(dir) ?? [];
  const loop = list.find((l) => l.n === n);
  if (!loop) return false;
  clearInterval(loop.timer);
  byDir.set(dir, list.filter((l) => l !== loop));
  return true;
}

/** Stops every loop (tests; a closed workspace). */
export function stopAllLoops(dir?: string) {
  for (const [d, list] of byDir) {
    if (dir && d !== dir) continue;
    list.forEach((l) => clearInterval(l.timer));
    byDir.delete(d);
    counters.delete(d);
  }
}
