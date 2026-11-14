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

import { mdiClose, mdiMicrophone, mdiMicrophoneOff, mdiMicrophoneOutline, mdiWaveform } from "@mdi/js";
import { t } from "../i18n";
import { IconButton } from "@blitz/webui";
import type { ButtonState, Conversation } from "./voice";

const icons: Record<Exclude<ButtonState, "hidden">, string> = {
  ready: mdiMicrophoneOutline,
  connecting: mdiMicrophoneOutline,
  listening: mdiMicrophone,
  speaking: mdiWaveform,
  muted: mdiMicrophoneOff,
  not_set_up: mdiMicrophoneOutline,
  no_mic: mdiMicrophoneOff,
};

/** The shortcut that starts and ends a conversation (VOX-21). */
export const voiceShortcut = "⌥⌘V";

/**
 * The composer's microphone (VOX-20): Talk to Blitz, and its states. With
 * listen = "hold", pressing it talks while held.
 */
export function VoiceButton({ state, hold, onToggle, onHold }: { state: ButtonState; hold: boolean; onToggle: () => void; onHold: (down: boolean) => void }) {
  if (state === "hidden") return null;
  const open = state === "listening" || state === "speaking" || state === "muted" || state === "connecting";
  const label = t(`desktop.voice.button.${state}`, { key: voiceShortcut });
  const holding = hold && open && state !== "connecting";
  return (
    <span
      className="voice-button"
      onPointerDown={holding ? (e) => (e.currentTarget.setPointerCapture?.(e.pointerId), onHold(true)) : undefined}
      onPointerUp={holding ? () => onHold(false) : undefined}
      onPointerCancel={holding ? () => onHold(false) : undefined}
    >
      <IconButton
        title=""
        icon={icons[state]}
        label={label}
        small={!open}
        variant={open ? "tonal" : undefined}
        disabled={state === "not_set_up" || state === "no_mic" || state === "connecting"}
        onClick={holding ? () => {} : onToggle}
      />
    </span>
  );
}

/**
 * The voice bar over the composer (VOX-22): the live transcript, mute and
 * end, the model and voice. The finished lines go into the conversation.
 */
export function VoiceBar({ conv, onMute, onEnd }: { conv: Conversation; onMute: (m: boolean) => void; onEnd: () => void }) {
  const line = conv.said || conv.heard;
  const who = conv.said ? t("desktop.voice.bar.voice") : conv.heard ? t("desktop.voice.bar.you") : "";
  return (
    <div className="card outlined row voice-bar" role="status" aria-live="polite" style={{ gap: 8, padding: "6px 10px", marginBottom: 6 }}>
      <span className="t-label muted">{t(conv.phase === "connecting" ? "desktop.voice.bar.connecting" : conv.muted ? "desktop.voice.bar.muted" : conv.state === "speaking" ? "desktop.voice.bar.speaking" : conv.listen === "hold" && !conv.holding ? "desktop.voice.bar.hold" : "desktop.voice.bar.listening")}</span>
      <span className="spacer ellipsis t-body-sm">
        {who && <strong>{who}: </strong>}
        {line}
      </span>
      {conv.model && <small className="muted ellipsis">{t("desktop.voice.bar.model", { model: conv.model, voice: conv.voice })}</small>}
      <IconButton icon={conv.muted ? mdiMicrophoneOff : mdiMicrophone} label={t(conv.muted ? "desktop.voice.unmute" : "desktop.voice.mute")} small onClick={() => onMute(!conv.muted)} />
      <IconButton icon={mdiClose} label={t("desktop.voice.end")} small onClick={onEnd} />
    </div>
  );
}
