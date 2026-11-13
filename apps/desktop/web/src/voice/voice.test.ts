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
import { fakeVoice, fakeVoiceSettings, setFakeVoice } from "../dev/fakeVoice";
import { buttonState, currentConversation, extend, fromPCM16, level, nothingWaiting, sameWaiting, setConversation, toPCM16k, waitingOf, type Conversation } from "./voice";

const conv: Conversation = { dir: "/w", sessionId: "s", id: "v1", phase: "open", state: "listening", muted: false, holding: false, listen: "tap", model: "m", voice: "Kore", heard: "", said: "", error: "" };

describe("voice", () => {
  it.each([
    { setup: { enabled: false, notSetUp: "", mic: "granted" as const }, c: null, want: "hidden" },
    { setup: { enabled: true, notSetUp: "no Gemini", mic: "granted" as const }, c: null, want: "not_set_up" },
    { setup: { enabled: true, notSetUp: "", mic: "denied" as const }, c: null, want: "no_mic" },
    { setup: { enabled: true, notSetUp: "", mic: "none" as const }, c: null, want: "no_mic" },
    { setup: { enabled: true, notSetUp: "", mic: "unknown" as const }, c: null, want: "ready" },
    { setup: { enabled: true, notSetUp: "", mic: "granted" as const }, c: { ...conv, phase: "connecting" as const }, want: "connecting" },
    { setup: { enabled: true, notSetUp: "", mic: "granted" as const }, c: { ...conv, muted: true }, want: "muted" },
    { setup: { enabled: true, notSetUp: "", mic: "granted" as const }, c: { ...conv, state: "speaking" as const }, want: "speaking" },
    { setup: { enabled: true, notSetUp: "", mic: "granted" as const }, c: conv, want: "listening" },
  ])("shows the microphone as $want", ({ setup, c, want }) => {
    expect(buttonState(setup, c)).toBe(want);
  });

  it("tells the service what waits: the approval showing, or the question", () => {
    expect(waitingOf(null)).toEqual(nothingWaiting);
    const approval = waitingOf({ kind: "approval", req: { requestId: "r1", tool: "run_shell_command", detail: "make deploy" } as never });
    expect(approval).toEqual({ ...nothingWaiting, approvalId: "r1", approval: "run_shell_command: make deploy" });
    expect(waitingOf({ kind: "approval", req: { requestId: "r2", tool: "x", detail: "y".repeat(400) } as never }).approval.length).toBe(301);
    const q = waitingOf({ kind: "question", q: { requestId: "q", question: "Which?", options: ["A", "B"] } as never });
    expect(q).toEqual({ ...nothingWaiting, question: "Which?", options: ["A", "B"] });
    expect(sameWaiting(q, { ...q, options: ["A", "B"] })).toBe(true);
    expect(sameWaiting(q, approval)).toBe(false);
  });

  it("sends 16 kHz PCM and plays 24 kHz PCM", () => {
    const at48 = new Float32Array(960).fill(0.5); // 20 ms at 48 kHz
    const pcm = toPCM16k(at48, 48000);
    expect(pcm.length).toBe(320 * 2);
    expect(new DataView(pcm.buffer).getInt16(0, true)).toBe(Math.floor(0.5 * 0x7fff));
    expect(toPCM16k(new Float32Array([2, -2, 0]), 16000)).toEqual(new Uint8Array([0xff, 0x7f, 0x00, 0x80, 0, 0]));
    const back = fromPCM16(new Uint8Array([0x00, 0x40, 0x00, 0xc0, 0x01]));
    expect([...back]).toEqual([0.5, -0.5]);
    expect(level(new Float32Array(10))).toBe(0);
    expect(level(new Float32Array(10).fill(1))).toBe(1);
    expect(extend("Run the ", "tests")).toBe("Run the tests");
    expect(extend("x".repeat(400), "y").length).toBe(400);
  });

  it("shares the window's one conversation", () => {
    setConversation(conv);
    expect(currentConversation()).toBe(conv);
    setConversation((c) => c && { ...c, muted: true });
    expect(currentConversation()?.muted).toBe(true);
    setConversation(null);
    expect(currentConversation()).toBeNull();
  });
});

// The fake service's voice, as the window drives it: refused while off;
// once on, the first audio is heard, handed over as a message and said; an
// approval waiting is read out and allowed once; one conversation a
// workspace; off ends it at once; voice is the person's setting.
describe("the fake voice", () => {
  const svc = fakeVoice(() => "Sadaltager");
  const signal = new AbortController().signal;
  const take = async (it: AsyncGenerator<Record<string, unknown>>, until: (m: Record<string, unknown>) => boolean) => {
    const got: Record<string, unknown>[] = [];
    for (;;) {
      const r = await it.next();
      if (r.done) return got;
      got.push(r.value);
      if (until(r.value)) return got;
    }
  };

  it("runs a conversation", async () => {
    await expect(svc.converse({ workspace: "/w", sessionId: "s", summaryOnly: false, sampleVoice: "" }, { signal }).next()).rejects.toThrow("voice is off");
    expect(() => setFakeVoice("/w", "voice.enabled", "true")).toThrow("the person's own setting");
    setFakeVoice("", "voice.enabled", "true");
    expect(svc.describeVoice({ workspace: "/w", listModels: true })).toMatchObject({ enabled: true, personalityVoice: "Sadaltager", backend: "gemini" });

    const it = svc.converse({ workspace: "/w", sessionId: "s", summaryOnly: false, sampleVoice: "" }, { signal });
    const first = (await it.next()).value as { started: { id: string; voice: string } };
    expect(first.started.voice).toBe("Sadaltager");
    const id = first.started.id;
    await expect(svc.converse({ workspace: "/w", sessionId: "s", summaryOnly: false, sampleVoice: "" }, { signal }).next()).rejects.toThrow("already open");

    svc.sendVoice({ workspace: "/w", id, audio: new Uint8Array(640), end: false });
    const said = await take(it, (m) => !!m.runTask);
    expect(said.find((m) => m.runTask)).toEqual({ runTask: "Run the test suite and fix what fails." });

    svc.sendVoice({ workspace: "/w", id, audio: new Uint8Array(0), end: false, waiting: { approvalId: "ap1", approval: "make deploy", question: "", options: [] } });
    const allowed = await take(it, (m) => !!m.approve);
    expect(allowed.at(-1)).toEqual({ approve: { id: "ap1", decision: "once" } });

    expect(() => svc.sendVoice({ workspace: "/other", id, audio: new Uint8Array(1), end: false })).toThrow("no such voice conversation");
    setFakeVoice("", "voice.enabled", "false");
    const end = await take(it, (m) => !!m.ended);
    expect(end.at(-1)).toEqual({ ended: "off" });
    expect(fakeVoiceSettings.enabled).toBe(false);
  });

  it("plays a sample and ends", async () => {
    setFakeVoice("", "voice.enabled", "true");
    const got = await take(svc.converse({ workspace: "/w", sessionId: "", summaryOnly: false, sampleVoice: "Puck" }, { signal }), (m) => !!m.ended);
    expect((got[0] as { started: { voice: string } }).started.voice).toBe("Puck");
    expect(got.some((m) => m.audio)).toBe(true);
    expect(got.at(-1)).toEqual({ ended: "summary" });
    setFakeVoice("", "voice.enabled", "false");
  });
});
