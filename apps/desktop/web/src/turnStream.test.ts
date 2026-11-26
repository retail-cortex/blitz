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

import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import { RunTurnResponseSchema, type RunTurnResponse } from "./gen/blitz/v1/session_pb";
import { ErrorInfoSchema } from "./gen/blitz/v1/turn_pb";
import { giveUpMs, newTurnId, resumableTurn, retryDelay, type Clock } from "./turnStream";

const text = (seq: number, t: string): RunTurnResponse => create(RunTurnResponseSchema, { seq: BigInt(seq), event: { kind: { case: "text", value: { text: t } } } });
const finished = (seq: number): RunTurnResponse => create(RunTurnResponseSchema, { seq: BigInt(seq), event: { kind: { case: "finished", value: {} } } });
const keepalive = create(RunTurnResponseSchema, {});
const networkError = () => new ConnectError("network error", Code.Unknown);

/** A step of a scripted stream: a message, or an error thrown in its place. */
type Step = RunTurnResponse | Error;

/** A service whose n-th stream sends scripts[n]; it notes the afterSeq of each ask. */
function service(scripts: Step[][]) {
  const asked: bigint[] = [];
  const open = (afterSeq: bigint) => {
    const steps = scripts[asked.length] ?? [];
    asked.push(afterSeq);
    return (async function* () {
      for (const s of steps) {
        if (s instanceof Error) throw s;
        yield s;
      }
    })();
  };
  return { asked, open };
}

/** A clock that passes the time waited, at once. */
function clock(): Clock & { waits: number[] } {
  let t = 1000;
  const waits: number[] = [];
  return {
    waits,
    now: () => t,
    wait: async (ms) => {
      t += ms;
      waits.push(ms);
    },
  };
}

const read = async (g: AsyncGenerator<RunTurnResponse>) => {
  const got: string[] = [];
  for await (const r of g) got.push(r.event?.kind.case === "text" ? r.event.kind.value.text : (r.event?.kind.case ?? "keepalive"));
  return got;
};

describe("resumableTurn", () => {
  it("reads a turn whose stream holds", async () => {
    const s = service([[text(1, "a"), keepalive, text(2, "b"), finished(3)]]);
    expect(await read(resumableTurn(s.open, new AbortController().signal, clock()))).toEqual(["a", "keepalive", "b", "finished"]);
    expect(s.asked).toEqual([0n]);
  });

  it("asks again from the last event when the stream drops (issue #128)", async () => {
    const s = service([
      [text(1, "a"), text(2, "b"), networkError()],
      [new ConnectError("fetch failed", Code.Unavailable)],
      // The service sends one again: it isn't shown twice.
      [text(2, "b"), text(3, "c"), new TypeError("network error")],
      [], // ended without the turn's last event: a drop as well
      [finished(4)],
    ]);
    const c = clock();
    expect(await read(resumableTurn(s.open, new AbortController().signal, c))).toEqual(["a", "b", "c", "finished"]);
    expect(s.asked).toEqual([0n, 2n, 2n, 3n, 3n]);
    // Sooner again after an event came through.
    expect(c.waits).toEqual([100, 250, 100, 250]);
  });

  it("throws what the service refused, without asking again", async () => {
    const gone = new ConnectError("no such turn", Code.NotFound, undefined, [{ desc: ErrorInfoSchema, value: create(ErrorInfoSchema, { reason: "UNKNOWN_TURN" }) }]);
    const s = service([[text(1, "a"), networkError()], [gone]]);
    await expect(read(resumableTurn(s.open, new AbortController().signal, clock()))).rejects.toBe(gone);
    expect(s.asked).toEqual([0n, 1n]);
  });

  it("never asks a service that doesn't number its events again", async () => {
    // An older service: the turn ended with the stream, and asking again would run the prompt twice.
    const e = networkError();
    const s = service([[text(0, "a"), e]]);
    await expect(read(resumableTurn(s.open, new AbortController().signal, clock()))).rejects.toBe(e);
    expect(s.asked).toEqual([0n]);
  });

  it("gives up once the service has been gone too long", async () => {
    const e = networkError();
    const s = service([[text(1, "a"), e], ...Array.from({ length: 100 }, () => [e])]);
    const c = clock();
    await expect(read(resumableTurn(s.open, new AbortController().signal, c))).rejects.toBe(e);
    const waited = c.waits.reduce((a, b) => a + b, 0);
    expect(waited).toBeGreaterThanOrEqual(giveUpMs);
    expect(waited).toBeLessThan(giveUpMs + 2 * retryDelay(99));
  });

  it("stops asking when the turn is stopped while it waits", async () => {
    const ctl = new AbortController();
    const e = networkError();
    const s = service([[text(1, "a"), e], [finished(2)]]);
    const c = clock();
    c.wait = async () => ctl.abort(); // Stop, while the page waits to ask again
    await expect(read(resumableTurn(s.open, ctl.signal, c))).rejects.toBe(e);
    expect(s.asked).toEqual([0n]);
  });

  it("waits by the real clock, no longer than a stop", async () => {
    const ctl = new AbortController();
    const e = networkError();
    // The second ask comes after a real 100 ms; the stop then cuts the next wait short.
    const s = service([[text(1, "a"), e], [text(2, "b"), e], [e], [e], [e], [e], [e], [e]]);
    const got: string[] = [];
    const started = Date.now();
    const done = (async () => {
      for await (const r of resumableTurn(s.open, ctl.signal)) {
        if (r.event?.kind.case === "text") got.push(r.event.kind.value.text);
        if (got.length === 2) setTimeout(() => ctl.abort(), 10);
      }
    })();
    await expect(done).rejects.toBe(e);
    expect(got).toEqual(["a", "b"]);
    expect(Date.now() - started).toBeLessThan(5000);
  });
});

describe("a turn's ID", () => {
  it("is one the service takes, and new each time", () => {
    const a = newTurnId();
    expect(a).toMatch(/^[A-Za-z0-9_-]{1,64}$/);
    expect(newTurnId()).not.toBe(a);
  });
});
