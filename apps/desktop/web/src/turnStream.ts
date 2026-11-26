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

// A turn's stream that survives its connection (spec_024_desktop DSK-21b,
// issue #128): on Linux, Chromium ends every connection, the loopback's
// too, when a network interface comes or goes (a container starting, a
// VPN, Wi-Fi roaming), and the chat showed "network error" while the
// service ended a healthy turn. The page names each turn (turn_id), so
// the service keeps it running, and asks for it again from the last
// event it received (spec_021_service SVC-20a).
import type { RunTurnResponse } from "./gen/blitz/v1/session_pb";
import { isStreamDrop } from "./errors";

/** A new turn's ID: a UUID. */
export const newTurnId = () => (typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `t-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`);

/**
 * How long the page keeps asking for a turn whose stream dropped (ms):
 * less than the minute the service keeps a turn nobody watches.
 */
export const giveUpMs = 45_000;

/** How long to wait before asking again, the n-th time in a row (ms). */
export const retryDelay = (n: number) => [100, 250, 500, 1000][n] ?? 2000;

/** Waits ms, or until signal aborts. */
const pause = (ms: number, signal: AbortSignal) =>
  new Promise<void>((done) => {
    if (signal.aborted) return done();
    const end = () => {
      clearTimeout(timer);
      signal.removeEventListener("abort", end);
      done();
    };
    const timer = setTimeout(end, ms);
    signal.addEventListener("abort", end);
  });

/** What resumableTurn waits and tells the time with (tests replace them). */
export interface Clock {
  now: () => number;
  wait: (ms: number, signal: AbortSignal) => Promise<void>;
}

/**
 * A turn's events, through its stream's drops: open(afterSeq) asks the
 * service for the turn's events after seq afterSeq (RunTurn with the
 * turn's ID), and is asked again, from the last event received, when the
 * stream drops (isStreamDrop) or ends without the turn's last event.
 * It gives up, with the stream's error, giveUpMs after the last sign of
 * the service; an error the service sent, and one after signal aborted
 * (Stop), are thrown at once. A service that doesn't number its events
 * (an older one, whose turn ended with the stream) is never asked again:
 * that would run the prompt twice.
 */
export async function* resumableTurn(
  open: (afterSeq: bigint, signal: AbortSignal) => AsyncIterable<RunTurnResponse>,
  signal: AbortSignal,
  clock: Clock = { now: Date.now, wait: pause },
): AsyncGenerator<RunTurnResponse> {
  let last = 0n;
  let lostAt = 0;
  let tries = 0;
  for (;;) {
    let dropped: unknown;
    try {
      for await (const res of open(last, signal)) {
        lostAt = 0;
        tries = 0;
        if (res.seq > 0n) {
          if (res.seq <= last) continue; // sent again: already shown
          last = res.seq;
        }
        yield res;
        if (res.event?.kind.case === "finished") return;
      }
      dropped = new Error("the turn's stream ended before the turn did");
    } catch (e) {
      if (signal.aborted || !isStreamDrop(e)) throw e;
      dropped = e;
    }
    if (signal.aborted || last === 0n) throw dropped;
    lostAt ||= clock.now();
    if (clock.now() - lostAt >= giveUpMs) throw dropped;
    await clock.wait(retryDelay(tries++), signal);
    if (signal.aborted) throw dropped;
  }
}
