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

import { Code, ConnectError } from "@connectrpc/connect";
import { ErrorInfoSchema } from "../gen/blitz/v1/turn_pb";
import { geminiVoices } from "../personality/voices";

// The fake VoiceService (spec_063_voice), for ?fake and the page's tests:
// voice's settings as the service keeps them, and a scripted conversation.
// It hears "Run the tests", hands that to the window, and says so; an
// approval the window says is waiting is read out and allowed once after a
// spoken yes; the end, the sample and the refusals are the service's.

/** [voice] as the fake keeps it. */
export const fakeVoiceSettings = { enabled: false, model: "", voice: "", listen: "tap", summaries: "voice", idle: 5 };

/** Applies one of Settings › Voice's settings, as config.SetValue does; true when key was voice's. */
export function setFakeVoice(workspace: string, key: string, value: string): boolean {
  if (!key.startsWith("voice.")) return false;
  if (workspace) throw new ConnectError(`${key} is the person's own setting: a workspace can't change it (spec_063_voice VOX-04)`, Code.InvalidArgument);
  const s = fakeVoiceSettings;
  if (key === "voice.enabled") s.enabled = value === "true";
  if (key === "voice.model") s.model = value;
  if (key === "voice.voice") s.voice = value;
  if (key === "voice.listen") s.listen = value || "tap";
  if (key === "voice.summaries") s.summaries = value || "voice";
  if (key === "voice.idle_minutes") s.idle = Number(value) || 5;
  if (!s.enabled) for (const c of open.values()) c.end("off");
  return true;
}

function refused(reason: string, msg: string, code = Code.FailedPrecondition) {
  const err = new ConnectError(msg, code);
  err.details.push({ desc: ErrorInfoSchema, value: { reason } });
  return err;
}

type Msg = Record<string, unknown>;
type Conv = { id: string; dir: string; out: Msg[]; wake: Set<() => void>; push: (m: Msg) => void; end: (why: string) => void; ended: boolean; approval: string; heard: boolean };
const open = new Map<string, Conv>();
let next = 1;

const pcm = (ms: number) => new Uint8Array(Math.round((24000 * ms) / 1000) * 2); // silence, 24 kHz

/** The service's handlers, scripted. */
export function fakeVoice(personalityVoice: (workspace: string) => string) {
  return {
    describeVoice: ({ workspace, listModels }: { workspace: string; listModels: boolean }) => ({
      enabled: fakeVoiceSettings.enabled,
      model: fakeVoiceSettings.model,
      voice: fakeVoiceSettings.voice,
      listen: fakeVoiceSettings.listen,
      summaries: fakeVoiceSettings.summaries,
      idleMinutes: fakeVoiceSettings.idle,
      notSetUp: "",
      modelInUse: fakeVoiceSettings.model || (listModels ? "gemini/gemini-2.5-flash-native-audio-preview-09-2025" : ""),
      backend: "gemini",
      voices: [...geminiVoices],
      models: listModels ? ["gemini-2.5-flash-native-audio-preview-09-2025", "gemini-live-2.5-flash-preview"] : [],
      personalityVoice: personalityVoice(workspace),
      orgOff: false,
    }),
    async *converse({ workspace, sampleVoice }: { workspace: string; sessionId: string; summaryOnly: boolean; sampleVoice: string }, ctx: { signal: AbortSignal }) {
      if (!fakeVoiceSettings.enabled) throw refused("VOICE_OFF", "voice is off: turn it on in Settings › Voice");
      if (!sampleVoice && [...open.values()].some((c) => c.dir === workspace && !c.ended)) throw refused("VOICE_BUSY", "a voice conversation is already open in this workspace");
      const voice = sampleVoice || fakeVoiceSettings.voice || personalityVoice(workspace) || "Kore";
      const c: Conv = {
        id: `voice-${next++}`,
        dir: workspace,
        out: [],
        wake: new Set(),
        ended: false,
        approval: "",
        heard: false,
        push: (m) => {
          c.out.push(m);
          for (const w of [...c.wake]) w();
        },
        end: (why) => {
          if (c.ended) return;
          c.ended = true;
          c.push({ ended: why });
        },
      };
      if (!sampleVoice) open.set(c.id, c);
      yield { started: { id: c.id, model: "gemini-2.5-flash-native-audio-preview-09-2025", voice, listen: fakeVoiceSettings.listen } };
      if (sampleVoice) {
        c.push({ state: "speaking" });
        c.push({ said: { text: "Hi, I'm Blitz. Tell me what you'd like done.", final: false } });
        c.push({ audio: pcm(400) });
        c.push({ said: { text: "", final: true } });
        c.end("summary");
      } else {
        c.push({ state: "listening" });
      }
      let sent = 0;
      try {
        while (!ctx.signal.aborted) {
          while (sent < c.out.length) {
            const m = c.out[sent++];
            yield m;
            if (m.ended) return;
          }
          await new Promise<void>((resolve) => {
            const done = () => (c.wake.delete(done), resolve());
            c.wake.add(done);
            ctx.signal.addEventListener("abort", done, { once: true });
          });
        }
      } finally {
        open.delete(c.id);
      }
    },
    sendVoice: ({ workspace, id, audio, end, waiting }: { workspace: string; id: string; audio: Uint8Array; speaking?: boolean; waiting?: { approvalId: string; approval: string; question: string; options: string[] }; end: boolean }) => {
      const c = open.get(id);
      if (!c || c.dir !== workspace || c.ended) throw refused("UNKNOWN_VOICE", "no such voice conversation: it has ended", Code.NotFound);
      if (end) c.end("closed");
      else if (waiting?.approvalId && waiting.approvalId !== c.approval) {
        c.approval = waiting.approvalId;
        c.push({ state: "speaking" });
        c.push({ said: { text: `It wants to run ${waiting.approval}. Allow it?`, final: false } });
        c.push({ said: { text: "", final: true } });
        c.push({ heard: { text: "Yes, go ahead.", final: false } });
        c.push({ heard: { text: "", final: true } });
        c.push({ approve: { id: c.approval, decision: "once" } });
        c.push({ state: "listening" });
      } else if (audio?.length && !c.heard) {
        // The first audio is heard as a request, handed to the window and said.
        c.heard = true;
        c.push({ heard: { text: "Run the tests, please.", final: false } });
        c.push({ heard: { text: "", final: true } });
        c.push({ runTask: "Run the test suite and fix what fails." });
        c.push({ state: "speaking" });
        c.push({ said: { text: "On it: I've passed that to your agent.", final: false } });
        c.push({ audio: pcm(300) });
        c.push({ said: { text: "", final: true } });
        c.push({ state: "listening" });
      }
      return {};
    },
  };
}
