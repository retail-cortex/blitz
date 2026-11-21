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

import { useEffect, useRef, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import { talkEvent } from "../events";
import { Decision } from "../gen/blitz/v1/turn_pb";
import type { Entry } from "../turns";
import { speakSummary, useVoice, useVoiceSetup } from "../voice/useVoice";
import { buttonState, useConversation, waitingOf, type ButtonState } from "../voice/voice";
import type { Pending } from "../waits";
import type { TaskRequest } from "./useBackgroundTasks";

/**
 * Connects voice conversation controls, summaries, command-palette talk events,
 * and Option+Cmd+V keyboard shortcuts to the workspace conversation.
 */
export function useConversationVoice({
  dir,
  sessionId,
  visible,
  running,
  micId,
  pending,
  taskRequests,
  submitRef,
  stop,
  decide,
  answer,
  answerTask,
  setEntries,
  snack,
}: {
  dir: string;
  sessionId: string;
  visible: boolean;
  running: boolean;
  micId: string;
  pending: Pending | null;
  taskRequests: TaskRequest[];
  submitRef: MutableRefObject<(text: string) => Promise<void> | void>;
  stop: () => Promise<void>;
  decide: (d: Decision) => Promise<void>;
  answer: (text: string) => Promise<void>;
  answerTask: (r: TaskRequest, reply: { decision?: Decision; text?: string }) => Promise<void>;
  setEntries: Dispatch<SetStateAction<Entry[]>>;
  snack: (text: string, opts?: { error?: boolean }) => void;
}) {
  // Talking to Blitz (spec_063_voice): the voice hands the work to this
  // conversation, which runs it as typed; its lines join the conversation.
  const voiceSetup = useVoiceSetup(dir);
  const conversationNow = useConversation();
  const myVoice = conversationNow?.dir === dir ? conversationNow : null;
  const voice = useVoice(
    dir,
    sessionId,
    micId,
    {
      send: (text) => void submitRef.current(text),
      stop: () => void stop(),
      // The conversation's own request first, else a background task's or
      // a sub-agent's (spec_063_voice VOX-13).
      answer: (text) => {
        if (pending?.kind === "question") return void answer(text);
        const r = taskRequests.find((x) => x.question);
        if (r) void answerTask(r, { text });
      },
      approve: (id, decision) => {
        const d = decision === "once" ? Decision.ONCE : Decision.DENY;
        if (pending?.kind === "approval" && pending.req.requestId === id) return void decide(d);
        const r = taskRequests.find((x) => x.id === id && x.approval);
        if (r) void answerTask(r, { decision: d });
      },
      spoken: (who, text) => setEntries((e) => [...e, { kind: "spoken", who, text }]),
      error: (text) => snack(text, { error: true }),
    },
    waitingOf(pending, taskRequests[0]),
  );
  const voiceState: ButtonState = conversationNow && !myVoice ? "hidden" : buttonState(voiceSetup, myVoice);
  const toggleVoice = () => (myVoice ? voice.end() : void voice.start());
  // With summaries = "always", a turn that ends with no conversation open
  // is summarised aloud, while this workspace is the one showing (VOX-17).
  const wasRunning = useRef(false);
  useEffect(() => {
    const ended = wasRunning.current && !running;
    wasRunning.current = running;
    if (ended && visible && voiceSetup.enabled && voiceSetup.summaries === "always" && !conversationNow && sessionId && !document.hidden) void speakSummary(dir, sessionId);
  }, [running, visible, voiceSetup.enabled, voiceSetup.summaries, conversationNow, sessionId, dir]);
  // The command palette's Talk to Blitz (VOX-21).
  useEffect(() => {
    const f = (e: Event) => {
      if ((e as CustomEvent<{ dir: string }>).detail.dir === dir && !myVoice && voiceState === "ready") void voice.start();
    };
    window.addEventListener(talkEvent, f);
    return () => window.removeEventListener(talkEvent, f);
  });
  // ⌥⌘V starts and ends it; with hold to talk, holding it talks (VOX-21).
  useEffect(() => {
    if (!visible || voiceState === "hidden") return;
    const isKey = (e: KeyboardEvent) => e.altKey && e.metaKey && e.code === "KeyV";
    const down = (e: KeyboardEvent) => {
      if (!isKey(e)) return;
      e.preventDefault();
      if (e.repeat) return;
      if (myVoice?.listen === "hold" && myVoice.phase === "open") voice.hold(true);
      else if (voiceState !== "not_set_up" && voiceState !== "no_mic") toggleVoice();
    };
    // Released when any of its keys comes up: macOS sends no key-up for V
    // while ⌘ is held, so ⌘'s or ⌥'s ends it too; and when the window
    // loses the keys.
    const up = (e: KeyboardEvent) => {
      if (["KeyV", "MetaLeft", "MetaRight", "AltLeft", "AltRight"].includes(e.code) && myVoice?.listen === "hold") voice.hold(false);
    };
    const blur = () => myVoice?.listen === "hold" && voice.hold(false);
    document.addEventListener("keydown", down);
    document.addEventListener("keyup", up);
    window.addEventListener("blur", blur);
    return () => {
      document.removeEventListener("keydown", down);
      document.removeEventListener("keyup", up);
      window.removeEventListener("blur", blur);
    };
  });

  return { myVoice, voice, voiceState, toggleVoice };
}
