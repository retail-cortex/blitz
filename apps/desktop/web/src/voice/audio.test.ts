/*
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

// The voice's player (spec_063_voice VOX-20): what's queued plays in
// order; a goodbye plays out before the player closes.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Player } from "./audio";

class FakeSource {
  buffer: { duration: number } | null = null;
  onended: (() => void) | null = null;
  stopped = false;
  connect() {}
  start() {}
  stop() {
    this.stopped = true;
  }
}

class FakeContext {
  static made: FakeContext[] = [];
  currentTime = 0;
  closed = false;
  destination = {};
  sources: FakeSource[] = [];
  constructor() {
    FakeContext.made.push(this);
  }
  createBuffer(_ch: number, n: number, rate: number) {
    return { duration: n / rate, copyToChannel() {} };
  }
  createBufferSource() {
    const s = new FakeSource();
    this.sources.push(s);
    return s;
  }
  close() {
    this.closed = true;
    return Promise.resolve();
  }
}

/** A second of 24 kHz 16-bit audio. */
const second = new Uint8Array(48000);

describe("the voice's player", () => {
  beforeEach(() => {
    FakeContext.made = [];
    vi.stubGlobal("AudioContext", FakeContext);
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("knows how long what's queued has left", () => {
    const p = new Player();
    expect(p.remainingMs).toBe(0);
    p.play(second);
    p.play(second);
    expect(p.remainingMs).toBeCloseTo(2020, -1);
    expect(p.busy).toBe(true);
  });

  it("plays a goodbye out before closing", async () => {
    const p = new Player();
    p.play(second);
    const done = p.closeAfterPlaying();
    const ctx = FakeContext.made[0];
    vi.advanceTimersByTime(500);
    expect(ctx.closed).toBe(false);
    expect(ctx.sources[0].stopped).toBe(false, "not cut off");
    vi.advanceTimersByTime(1000);
    await done;
    expect(ctx.closed).toBe(true);
  });

  it("holds a reply's start for the buffer, then plays the rest on", () => {
    const p = new Player(300);
    p.play(second);
    expect(p.remainingMs).toBeCloseTo(1300, -1);
    p.play(second);
    expect(p.remainingMs).toBeCloseTo(2300, -1, "the second piece follows the first, not another buffer");
    expect(new Player(5).remainingMs).toBe(0);
  });

  it("waits at most the limit", async () => {
    const p = new Player();
    for (let i = 0; i < 30; i++) p.play(second);
    const done = p.closeAfterPlaying(2000);
    vi.advanceTimersByTime(2000);
    await done;
    expect(FakeContext.made[0].closed).toBe(true);
  });

  it("closes at once with nothing to play, and flush stops what plays", async () => {
    const p = new Player();
    const done = p.closeAfterPlaying();
    vi.advanceTimersByTime(100);
    await done;
    const q = new Player();
    q.play(second);
    q.flush();
    expect(FakeContext.made[0].sources[0].stopped).toBe(true);
    expect(q.busy).toBe(false);
  });
});
