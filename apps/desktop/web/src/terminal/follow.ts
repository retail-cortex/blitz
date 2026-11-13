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

// A terminal's output followed through a broken connection
// (spec_062_terminal TRM-27): the stream is watched again from where it
// was read up to, and one that goes quiet longer than the service's
// heartbeat (TRM-12a) is taken for broken, since a quiet shell still
// beats. Apart from xterm.js and the API, so it can be tested.

/** One message of a terminal's stream (WatchTerminalResponse's fields). */
export interface TermChunk {
  data: Uint8Array;
  offset: bigint;
  skipped: bigint;
  exited: boolean;
}

/**
 * How the terminal's connection is: ok; reconnecting after a break;
 * lost (reconnecting has failed for a while: offer a restart); gone (the
 * service no longer has the terminal: only a restart helps).
 */
export type LinkState = "ok" | "reconnecting" | "lost" | "gone";

/** What follow watches with, and tells. */
export interface FollowOptions {
  /** The stream from offset from, ended by signal. */
  watch: (from: bigint, signal: AbortSignal) => AsyncIterable<TermChunk>;
  /** A message with output (or skipped output). */
  onChunk: (c: TermChunk) => void;
  /** The shell exited: the stream's last message. */
  onExit: () => void;
  /** The connection changed. */
  onState: (s: LinkState) => void;
  /** Whether err says the terminal is gone (UNKNOWN_TERMINAL). */
  isGone: (err: unknown) => boolean;
  /** No message for this long: the stream is broken (the service beats every 10 s). */
  stallMs?: number;
  /** The waits between tries, the last repeated. */
  backoffMs?: number[];
  /** Failed tries before the connection is lost. */
  lostAfter?: number;
}

/** Following a terminal: retry tries again now, done ends with it. */
export interface Following {
  retry: () => void;
  done: Promise<void>;
}

/** Follows a terminal's output until stop, its exit or its going (TRM-27). */
export function follow(o: FollowOptions, stop: AbortSignal): Following {
  const stallMs = o.stallMs ?? 25_000;
  const backoff = o.backoffMs ?? [500, 1000, 2000, 4000];
  const lostAfter = o.lostAfter ?? 4;
  let wake = () => {};
  let state: LinkState | null = null;
  const tell = (s: LinkState) => {
    if (s !== state) o.onState((state = s));
  };
  const done = (async () => {
    let from = 0n;
    let tries = 0;
    while (!stop.aborted) {
      const ctl = new AbortController();
      const abort = () => ctl.abort();
      stop.addEventListener("abort", abort);
      let timer: ReturnType<typeof setTimeout> | undefined;
      const arm = () => {
        clearTimeout(timer);
        timer = setTimeout(() => ctl.abort(), stallMs);
      };
      try {
        arm();
        for await (const c of o.watch(from, ctl.signal)) {
          arm();
          tries = 0;
          tell("ok");
          // An empty message is a heartbeat: its offset isn't where output ends.
          if (c.data.length || c.skipped) {
            from = c.offset;
            o.onChunk(c);
          }
          if (c.exited) {
            o.onExit();
            return;
          }
        }
        // Ended without the shell's exit: watched again below.
      } catch (e) {
        if (stop.aborted) return;
        if (o.isGone(e)) {
          tell("gone");
          return;
        }
      } finally {
        clearTimeout(timer);
        stop.removeEventListener("abort", abort);
      }
      if (stop.aborted) return;
      tries++;
      tell(tries > lostAfter ? "lost" : "reconnecting");
      await new Promise<void>((resolve) => {
        const t = setTimeout(resolve, backoff[Math.min(tries - 1, backoff.length - 1)]);
        wake = () => {
          clearTimeout(t);
          resolve();
        };
        stop.addEventListener("abort", wake, { once: true });
      });
    }
  })();
  return { retry: () => wake(), done };
}

/**
 * Watches typed input for an answer (TRM-27): a key the terminal echoes,
 * or anything it prints, answers it; input with no answer for quietMs is
 * the shell not answering (ssh on a dropped network, a hung program), and
 * onQuiet says so; the next output takes it back.
 */
export function answerWatch(quietMs: number, onQuiet: (quiet: boolean) => void) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let quiet = false;
  return {
    /** Input was sent. */
    typed() {
      if (timer === undefined) timer = setTimeout(() => onQuiet((quiet = true)), quietMs);
    },
    /** Output came. */
    answered() {
      clearTimeout(timer);
      timer = undefined;
      if (quiet) onQuiet((quiet = false));
    },
    stop() {
      clearTimeout(timer);
    },
  };
}
