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

import { describe, expect, it } from "vitest";
import { answerWatch, follow, type LinkState, type TermChunk } from "./follow";

const enc = new TextEncoder();
const out = (text: string, offset: number): TermChunk => ({ data: enc.encode(text), offset: BigInt(offset), skipped: 0n, exited: false });
const beat: TermChunk = { data: new Uint8Array(), offset: 0n, skipped: 0n, exited: false };
const exit = (offset: number): TermChunk => ({ data: new Uint8Array(), offset: BigInt(offset), skipped: 0n, exited: true });
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// A stream: its chunks, then (end) an error, the end of the stream, or
// nothing more until it's aborted (a connection gone quiet).
type Script = { chunks: TermChunk[]; end: "error" | "gone" | "close" | "stall" };
function streams(scripts: Script[]) {
  const froms: bigint[] = [];
  const watch = (from: bigint, signal: AbortSignal) => {
    froms.push(from);
    const s = scripts.shift() ?? { chunks: [], end: "stall" };
    return (async function* () {
      for (const c of s.chunks) {
        await sleep(1);
        yield c;
      }
      if (s.end === "error") throw new Error("connection reset");
      if (s.end === "gone") throw Object.assign(new Error("no such terminal"), { gone: true });
      if (s.end === "stall") await new Promise((_, reject) => signal.addEventListener("abort", () => reject(new Error("aborted"))));
    })();
  };
  return { watch, froms };
}

function run(scripts: Script[], stallMs = 30) {
  const { watch, froms } = streams(scripts);
  const text: string[] = [];
  const states: LinkState[] = [];
  let exited = false;
  const stop = new AbortController();
  const f = follow(
    {
      watch,
      onChunk: (c) => text.push(new TextDecoder().decode(c.data)),
      onExit: () => (exited = true),
      onState: (s) => states.push(s),
      isGone: (e) => !!(e as { gone?: boolean }).gone,
      stallMs,
      backoffMs: [5],
      lostAfter: 2,
    },
    stop.signal,
  );
  return { f, froms, text, states, stop, exited: () => exited };
}

// TRM-27: a broken stream is watched again from where it was read up to.
describe("following a terminal's output", () => {
  it("watches again from where it read up to after a break, and stops at the exit", async () => {
    const r = run([
      { chunks: [beat, out("hello ", 6)], end: "error" },
      { chunks: [beat, out("world", 11), exit(11)], end: "close" },
    ]);
    await r.f.done;
    expect(r.text.join("")).toBe("hello world");
    expect(r.froms).toEqual([0n, 6n]);
    expect(r.states).toEqual(["ok", "reconnecting", "ok"]);
    expect(r.exited()).toBe(true);
  });

  it("takes a heartbeat's offset for nothing", async () => {
    const r = run([
      { chunks: [out("a", 1), beat], end: "error" },
      { chunks: [exit(1)], end: "close" },
    ]);
    await r.f.done;
    expect(r.froms).toEqual([0n, 1n]);
  });

  it("takes a stream with no message, not even a heartbeat, for broken", async () => {
    const r = run([
      { chunks: [beat], end: "stall" },
      { chunks: [exit(0)], end: "close" },
    ]);
    await r.f.done;
    expect(r.states).toEqual(["ok", "reconnecting", "ok"]);
    expect(r.froms).toHaveLength(2);
  });

  it("says the connection is lost after a few failed tries, and back once it is", async () => {
    const r = run([{ chunks: [], end: "error" }, { chunks: [], end: "error" }, { chunks: [], end: "error" }, { chunks: [beat, exit(0)], end: "close" }]);
    await r.f.done;
    expect(r.states).toEqual(["reconnecting", "lost", "ok"]);
  });

  it("stops when the service no longer has the terminal: gone", async () => {
    const r = run([{ chunks: [beat], end: "gone" }]);
    await r.f.done;
    expect(r.states).toEqual(["ok", "gone"]);
  });

  it("tries again at once on retry, and stops on stop", async () => {
    const { watch, froms } = streams([{ chunks: [], end: "error" }]);
    const stop = new AbortController();
    const f = follow({ watch, onChunk: () => {}, onExit: () => {}, onState: () => {}, isGone: () => false, stallMs: 1000, backoffMs: [60_000] }, stop.signal);
    await sleep(10);
    f.retry();
    await sleep(10);
    expect(froms).toHaveLength(2);
    stop.abort();
    await f.done;
  });
});

describe("watching typed keys for an answer", () => {
  it("says the shell isn't answering when typed keys get nothing back, and takes it back on output", async () => {
    const said: boolean[] = [];
    const w = answerWatch(20, (q) => said.push(q));
    w.typed();
    w.answered(); // echoed at once
    await sleep(30);
    expect(said).toEqual([]);
    w.typed();
    w.typed();
    await sleep(30);
    expect(said).toEqual([true]);
    w.answered();
    expect(said).toEqual([true, false]);
    w.stop();
  });
});
