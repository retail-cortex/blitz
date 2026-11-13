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

import { fromPCM16, level, toPCM16k } from "./voice";

// The microphone and the speaker (spec_063_voice VOX-30): the microphone's
// audio in 20 ms frames of 16 kHz PCM, the voice's 24 kHz PCM played in
// order, dropped when the person speaks over it.

// The worklet hands the main thread the microphone's samples as they
// come, in blocks of 128.
const worklet = `
class Tap extends AudioWorkletProcessor {
  process(inputs) {
    const ch = inputs[0] && inputs[0][0];
    if (ch) this.port.postMessage(ch.slice(0));
    return true;
  }
}
registerProcessor("blitz-mic-tap", Tap);
`;

/** The microphone, open: frames go to onFrame, its level to onLevel. */
export type Mic = { close: () => void; deviceId: string };

/** Whether the system has a microphone and the page may use it ("prompt": it will ask). */
export async function micPermission(): Promise<"granted" | "denied" | "prompt" | "none"> {
  try {
    const devices = await navigator.mediaDevices?.enumerateDevices();
    if (devices && !devices.some((d) => d.kind === "audioinput")) return "none";
    const p = await navigator.permissions?.query({ name: "microphone" as PermissionName });
    return (p?.state as "granted" | "denied" | "prompt") ?? "prompt";
  } catch {
    return "prompt";
  }
}

/** The system's microphones, for Settings › Voice (their names show once the page may use one). */
export async function microphones(): Promise<{ id: string; label: string }[]> {
  try {
    const devices = await navigator.mediaDevices.enumerateDevices();
    return devices.filter((d) => d.kind === "audioinput").map((d, i) => ({ id: d.deviceId, label: d.label || `Microphone ${i + 1}` }));
  } catch {
    return [];
  }
}

/**
 * Opens the microphone (deviceId "" for the system's): audio only, echo
 * cancellation and noise suppression on (VOX-30). Each 20 ms of audio goes
 * to onFrame as 16 kHz PCM, with its level.
 */
export async function openMic(deviceId: string, onFrame: (pcm: Uint8Array, level: number) => void): Promise<Mic> {
  const stream = await navigator.mediaDevices.getUserMedia({
    audio: { deviceId: deviceId ? { exact: deviceId } : undefined, echoCancellation: true, noiseSuppression: true, autoGainControl: true, channelCount: 1 },
    video: false,
  });
  const ctx = new AudioContext();
  const url = URL.createObjectURL(new Blob([worklet], { type: "text/javascript" }));
  try {
    await ctx.audioWorklet.addModule(url);
  } finally {
    URL.revokeObjectURL(url);
  }
  const source = ctx.createMediaStreamSource(stream);
  const tap = new AudioWorkletNode(ctx, "blitz-mic-tap");
  const frame = Math.round(ctx.sampleRate / 50); // 20 ms
  let buf = new Float32Array(0);
  tap.port.onmessage = (e: MessageEvent<Float32Array>) => {
    const next = new Float32Array(buf.length + e.data.length);
    next.set(buf);
    next.set(e.data, buf.length);
    buf = next;
    while (buf.length >= frame) {
      const chunk = buf.slice(0, frame);
      buf = buf.slice(frame);
      onFrame(toPCM16k(chunk, ctx.sampleRate), level(chunk));
    }
  };
  source.connect(tap);
  const track = stream.getAudioTracks()[0];
  return {
    deviceId: track?.getSettings().deviceId ?? deviceId,
    close: () => {
      tap.port.onmessage = null;
      source.disconnect();
      tap.disconnect();
      for (const t of stream.getTracks()) t.stop();
      void ctx.close();
    },
  };
}

/** Plays the voice's audio in order; flush drops what hasn't played (an interruption, VOX-20). */
export class Player {
  private ctx: AudioContext | null = null;
  private at = 0;
  private playing = new Set<AudioBufferSourceNode>();

  /** Queues 24 kHz 16-bit PCM after what's queued. */
  play(pcm: Uint8Array) {
    if (pcm.length < 2) return;
    this.ctx ??= new AudioContext({ sampleRate: 24000 });
    const samples = fromPCM16(pcm);
    const buf = this.ctx.createBuffer(1, samples.length, 24000);
    buf.copyToChannel(samples as Float32Array<ArrayBuffer>, 0);
    const src = this.ctx.createBufferSource();
    src.buffer = buf;
    src.connect(this.ctx.destination);
    const start = Math.max(this.ctx.currentTime + 0.02, this.at);
    src.start(start);
    this.at = start + buf.duration;
    this.playing.add(src);
    src.onended = () => this.playing.delete(src);
  }

  /** Stops what's playing and drops what's queued. */
  flush() {
    for (const s of this.playing) {
      try {
        s.stop();
      } catch {
        // already ended
      }
    }
    this.playing.clear();
    this.at = 0;
  }

  /** Whether audio is playing or queued. */
  get busy() {
    return this.playing.size > 0;
  }

  close() {
    this.flush();
    void this.ctx?.close();
    this.ctx = null;
  }
}
