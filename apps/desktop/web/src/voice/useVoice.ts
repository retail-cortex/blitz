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

import { useCallback, useEffect, useRef, useState } from "react";
import { voices } from "../api";
import { message, reason } from "../errors";
import { configChangedEvent } from "../events";
import type { ConverseResponse } from "../gen/blitz/v1/voice_pb";
import { t } from "../i18n";
import { openMic, micPermission, Player, type Mic } from "./audio";
import { currentConversation, extend, sameWaiting, setConversation, setEnder, nothingWaiting, type Conversation, type VoiceSetup, type Waiting } from "./voice";

/** Whether voice is on and set up, and the microphone's permission (VOX-03, VOX-50); again when the settings change. */
export function useVoiceSetup(dir: string): VoiceSetup & { refresh: () => void } {
  const [setup, setSetup] = useState<VoiceSetup>({ enabled: false, notSetUp: "", mic: "unknown" });
  const refresh = useCallback(() => {
    voices.describeVoice({ workspace: dir }).then(
      async (d) => setSetup({ enabled: d.enabled, notSetUp: d.notSetUp, summaries: d.summaries, mic: d.enabled ? await micPermission().then((p) => (p === "prompt" ? "unknown" : p)) : "unknown" }),
      () => setSetup((s) => ({ ...s, enabled: false })),
    );
  }, [dir]);
  useEffect(() => {
    refresh();
    window.addEventListener(configChangedEvent, refresh);
    return () => window.removeEventListener(configChangedEvent, refresh);
  }, [refresh]);
  return { ...setup, refresh };
}

/** What a conversation asks the window to do: the work, as typed (VOX-11), and the spoken lines for the conversation (VOX-22). */
export type VoiceHandlers = {
  /** A message to send (or, while a turn runs, to steer with). */
  send: (text: string) => void;
  stop: () => void;
  answer: (text: string) => void;
  /** The approval showing, once or denied; ignored unless it's id. */
  approve: (id: string, decision: "once" | "deny") => void;
  spoken: (who: "person" | "voice", text: string) => void;
  error: (text: string) => void;
};

// How much of the microphone goes in one call: five 20 ms frames.
const framesPerSend = 5;

/**
 * Runs the window's voice conversation for a workspace's session (VOX-20 to
 * VOX-24): start and end, mute, hold to talk, and what's waiting, which
 * the service hears of as it changes.
 */
export function useVoice(dir: string, sessionId: string, mic: string, handlers: VoiceHandlers, waiting: Waiting) {
  const h = useRef(handlers);
  h.current = handlers;
  const live = useRef<{ abort: AbortController; mic: Mic | null; player: Player; id: string; sent: Waiting; heard: string; said: string; queue: Uint8Array[] } | null>(null);

  const end = useCallback((why?: string) => {
    const l = live.current;
    live.current = null;
    if (!l) return;
    if (l.id) void voices.sendVoice({ workspace: dir, id: l.id, end: true }).catch(() => {});
    l.abort.abort();
    l.mic?.close();
    l.player.close();
    setConversation(null);
    setEnder(null);
    if (why === "idle") h.current.error(t("desktop.voice.ended_idle"));
  }, [dir]);

  const flushAudio = (l: NonNullable<typeof live.current>) => {
    if (!l.id || l.queue.length === 0) return;
    const parts = l.queue.splice(0);
    const all = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
    let at = 0;
    for (const p of parts) {
      all.set(p, at);
      at += p.length;
    }
    void voices.sendVoice({ workspace: dir, id: l.id, audio: all }).catch(() => {});
  };

  const onMessage = (l: NonNullable<typeof live.current>, m: ConverseResponse) => {
    const c = currentConversation();
    if (m.started) {
      l.id = m.started.id;
      setConversation((x) => x && { ...x, id: l.id, phase: "open", model: m.started!.model, voice: m.started!.voice, listen: m.started!.listen === "hold" ? "hold" : "tap" });
      l.sent = nothingWaiting;
      return;
    }
    if (m.audio.length) l.player.play(m.audio);
    if (m.heard) {
      if (m.heard.text) l.heard = extend(l.heard, m.heard.text);
      if (m.heard.final && l.heard.trim()) {
        h.current.spoken("person", l.heard.trim());
        l.heard = "";
      }
      setConversation((x) => x && { ...x, heard: l.heard });
    }
    if (m.said) {
      if (m.said.text) l.said = extend(l.said, m.said.text);
      if (m.said.final && l.said.trim()) {
        h.current.spoken("voice", l.said.trim());
        l.said = "";
      }
      setConversation((x) => x && { ...x, said: l.said });
    }
    if (m.state === "listening" || m.state === "speaking") setConversation((x) => x && { ...x, state: m.state as "listening" | "speaking" });
    if (m.interrupted) l.player.flush();
    if (m.runTask) h.current.send(m.runTask);
    if (m.steer) h.current.send(m.steer);
    if (m.stop) h.current.stop();
    if (m.answer) h.current.answer(m.answer);
    if (m.approve && (m.approve.decision === "once" || m.approve.decision === "deny")) h.current.approve(m.approve.id, m.approve.decision);
    if (m.error) {
      h.current.error(m.error);
      if (c) setConversation({ ...c, error: m.error });
    }
    if (m.ended) end(m.ended);
  };

  const start = useCallback(async () => {
    if (live.current || currentConversation()) return;
    const abort = new AbortController();
    const l = { abort, mic: null as Mic | null, player: new Player(), id: "", sent: nothingWaiting, heard: "", said: "", queue: [] as Uint8Array[] };
    live.current = l;
    const conv: Conversation = { dir, sessionId, id: "", phase: "connecting", state: "listening", muted: false, holding: false, listen: "tap", model: "", voice: "", heard: "", said: "", error: "" };
    setConversation(conv);
    setEnder(() => end());
    try {
      l.mic = await openMic(mic, (pcm) => {
        const c = currentConversation();
        if (!c || c.muted || (c.listen === "hold" && !c.holding)) return;
        l.queue.push(pcm);
        if (l.queue.length >= framesPerSend) flushAudio(l);
      });
    } catch (e) {
      end();
      h.current.error(e instanceof DOMException && (e.name === "NotAllowedError" || e.name === "SecurityError") ? t("desktop.voice.mic_denied") : t("desktop.voice.mic_failed", { error: message(e) }));
      return;
    }
    try {
      for await (const m of voices.converse({ workspace: dir, sessionId }, { signal: abort.signal })) {
        if (live.current !== l) break;
        onMessage(l, m);
      }
    } catch (e) {
      if (!abort.signal.aborted) h.current.error(voiceErrorText(e));
    }
    if (live.current === l) end();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dir, sessionId, mic, end]);

  // What waits on the person, each time it changes (VOX-13).
  useEffect(() => {
    const l = live.current;
    if (!l?.id || sameWaiting(l.sent, waiting)) return;
    l.sent = waiting;
    void voices.sendVoice({ workspace: dir, id: l.id, waiting }).catch(() => {});
  });

  // The conversation ends with the workspace's view (VOX-14).
  useEffect(() => () => end(), [end]);

  const mute = (muted: boolean) => setConversation((c) => c && { ...c, muted });
  const hold = (holding: boolean) => {
    const l = live.current;
    setConversation((c) => c && { ...c, holding });
    if (!l?.id) return;
    if (!holding) flushAudio(l);
    void voices.sendVoice({ workspace: dir, id: l.id, speaking: holding }).catch(() => {});
  };
  return { start, end: () => end(), mute, hold };
}

/** A failed conversation in a sentence (VOX-24, VOX-31). */
export function voiceErrorText(e: unknown): string {
  switch (reason(e)) {
    case "VOICE_OFF":
      return t("desktop.voice.error.off");
    case "VOICE_NOT_SET_UP":
      return t("desktop.voice.error.not_set_up");
    case "VOICE_MODEL":
      return t("desktop.voice.error.model");
    case "VOICE_BUSY":
      return t("desktop.voice.error.busy");
  }
  return t("desktop.voice.error.other", { error: message(e) });
}

/**
 * Says a finished turn's summary aloud with no conversation open
 * (summaries = "always", VOX-17): one short live call, played, then done.
 */
export async function speakSummary(dir: string, sessionId: string) {
  const player = new Player();
  try {
    for await (const m of voices.converse({ workspace: dir, sessionId, summaryOnly: true })) {
      if (m.audio.length) player.play(m.audio);
      if (m.ended) break;
    }
  } catch {
    // a summary not said is no error of the turn's
  } finally {
    setTimeout(() => player.close(), 20000);
  }
}
