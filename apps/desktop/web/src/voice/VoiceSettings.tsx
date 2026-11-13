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
import { mdiMicrophoneOutline, mdiPlayOutline } from "@mdi/js";
import { config, voices } from "../api";
import { message } from "../errors";
import type { DescribeVoiceResponse } from "../gen/blitz/v1/voice_pb";
import { t } from "../i18n";
import { configChanged, openSettings } from "../events";
import { useApp } from "../state";
import { Button, Icon, Segmented, Switch, useSnackbar } from "@blitz/webui";
import { micPermission, microphones, openMic, Player, type Mic } from "./audio";
import { voiceErrorText } from "./useVoice";

/**
 * Settings › Voice (spec_063_voice VOX-40, VOX-41): the switch, and with
 * voice on, the model, the voice with a sample, the microphone with its
 * level, how it listens, when work is summarised aloud and how long a
 * quiet conversation lasts. Every setting is [voice]'s, the person's
 * (SetConfigValue with no workspace); the microphone is the window's.
 */
export function VoiceSettings() {
  const snack = useSnackbar();
  const { prefs, update } = useApp();
  const workspace = prefs.workspaces.find((w) => w.dir === prefs.active && w.open)?.dir ?? "";
  const [d, setD] = useState<DescribeVoiceResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    try {
      setD(await voices.describeVoice({ workspace, listModels: true }));
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => void load(), [load]);
  const set = async (key: string, value: string, done?: string) => {
    setBusy(true);
    try {
      await config.setConfigValue({ workspace: "", key, value });
      configChanged({ dir: "" });
      if (done) snack(done);
      await load();
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy(false);
    }
  };
  if (!d) return error ? <p className="error-text">{error}</p> : null;
  return (
    <div className="stack voice-settings" style={{ gap: 12 }}>
      <div className="setting">
        <div className="stack" style={{ gap: 2 }}>
          <span className="t-title-sm">{t("desktop.voice.settings.switch")}</span>
          <span className="t-body-sm muted">{t("desktop.voice.settings.switch_detail")}</span>
        </div>
        <div className="setting-control">
          <Switch label={t("desktop.voice.settings.switch")} checked={d.enabled} disabled={busy || d.orgOff} onChange={(on) => void set("voice.enabled", on ? "true" : "false", t(on ? "desktop.voice.settings.on" : "desktop.voice.settings.off"))} />
        </div>
      </div>
      {d.enabled && (
        <>
          {d.notSetUp ? (
            <div className="card warn row" style={{ gap: 8 }}>
              <span className="spacer t-body-sm">{t("desktop.voice.settings.not_set_up")}</span>
              <Button small variant="tonal" onClick={() => openSettings({ section: "providers" })}>
                {t("desktop.voice.settings.set_up")}
              </Button>
            </div>
          ) : (
            <label className="field">
              <span className="t-label">{t("desktop.voice.settings.model")}</span>
              <select className="select" value={d.model} disabled={busy} onChange={(e) => void set("voice.model", e.target.value)}>
                <option value="">{t("desktop.voice.settings.model_default", { model: (d.modelInUse || "").replace(/^gemini\//, "") || "Gemini Live" })}</option>
                {d.model && !d.models.includes(d.model.replace(/^gemini\//, "")) && <option value={d.model}>{d.model.replace(/^gemini\//, "")}</option>}
                {d.models.map((m) => (
                  <option key={m} value={`gemini/${m}`}>
                    {m}
                  </option>
                ))}
              </select>
              <span className="t-body-sm muted">{t(d.backend === "vertex" ? "desktop.voice.settings.on_vertex" : "desktop.voice.settings.on_gemini")}</span>
            </label>
          )}
          <VoicePicker d={d} workspace={workspace} busy={busy} onSet={(v) => void set("voice.voice", v)} />
          <MicPicker device={prefs.voice_mic} onDevice={(id) => update((p) => ({ ...p, voice_mic: id }))} />
          <label className="field">
            <span className="t-label">{t("desktop.voice.settings.listen")}</span>
            <Segmented<string>
              label={t("desktop.voice.settings.listen")}
              value={d.listen}
              onChange={(v) => void set("voice.listen", v)}
              options={[
                { value: "tap", label: t("desktop.voice.settings.listen.tap") },
                { value: "hold", label: t("desktop.voice.settings.listen.hold") },
              ]}
            />
            <span className="t-body-sm muted">{t(`desktop.voice.settings.listen.${d.listen === "hold" ? "hold" : "tap"}_detail`)}</span>
          </label>
          <label className="field">
            <span className="t-label">{t("desktop.voice.settings.summaries")}</span>
            <select className="select" value={d.summaries} disabled={busy} onChange={(e) => void set("voice.summaries", e.target.value)}>
              {(["voice", "always", "off"] as const).map((v) => (
                <option key={v} value={v}>
                  {t(`desktop.voice.settings.summaries.${v}`)}
                </option>
              ))}
            </select>
            <span className="t-body-sm muted">{t("desktop.voice.settings.summaries_detail")}</span>
          </label>
          <label className="field">
            <span className="t-label">{t("desktop.voice.settings.idle")}</span>
            <input
              className="input"
              type="number"
              min={1}
              max={60}
              defaultValue={d.idleMinutes}
              disabled={busy}
              onBlur={(e) => {
                const n = Number(e.target.value);
                if (n !== d.idleMinutes && n >= 1 && n <= 60) void set("voice.idle_minutes", String(Math.round(n)));
              }}
            />
            <span className="t-body-sm muted">{t("desktop.voice.settings.idle_detail")}</span>
          </label>
          <p className="t-body-sm muted">{t("desktop.voice.settings.cost")}</p>
        </>
      )}
    </div>
  );
}

/** The voice, with Play: a short sample in it (one short live call, VOX-41). */
function VoicePicker({ d, workspace, busy, onSet }: { d: DescribeVoiceResponse; workspace: string; busy: boolean; onSet: (v: string) => void }) {
  const snack = useSnackbar();
  const [playing, setPlaying] = useState(false);
  const [played, setPlayed] = useState(false);
  const shown = d.voice || "";
  const play = async () => {
    const voice = shown || d.personalityVoice || "Kore";
    setPlaying(true);
    const player = new Player();
    try {
      for await (const m of voices.converse({ workspace, sampleVoice: voice })) {
        if (m.audio.length) player.play(m.audio);
        if (m.error) snack(m.error, { error: true });
        if (m.ended) break;
      }
      setPlayed(true);
    } catch (e) {
      snack(voiceErrorText(e), { error: true });
    } finally {
      setTimeout(() => player.close(), 15000);
      setPlaying(false);
    }
  };
  return (
    <label className="field">
      <span className="t-label">{t("desktop.voice.settings.voice")}</span>
      <span className="row" style={{ gap: 8 }}>
        <select className="select spacer" value={shown} disabled={busy} onChange={(e) => onSet(e.target.value)}>
          <option value="">{d.personalityVoice ? t("desktop.voice.settings.voice_personality", { voice: d.personalityVoice }) : t("desktop.voice.settings.voice_default")}</option>
          {d.voices.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
        <Button small icon={mdiPlayOutline} disabled={playing || !workspace || !!d.notSetUp} onClick={() => void play()}>
          {t(playing ? "desktop.voice.settings.playing" : "desktop.voice.settings.play")}
        </Button>
      </span>
      {!played && <span className="t-body-sm muted">{t("desktop.voice.settings.play_costs")}</span>}
    </label>
  );
}

/**
 * The microphone (the window's choice) with a level meter while this is
 * shown and the page may use it, and Allow when it may not yet (VOX-50).
 */
function MicPicker({ device, onDevice }: { device: string; onDevice: (id: string) => void }) {
  const [permission, setPermission] = useState<"granted" | "denied" | "prompt" | "none">("prompt");
  const [devices, setDevices] = useState<{ id: string; label: string }[]>([]);
  const [lvl, setLvl] = useState(0);
  const mic = useRef<Mic | null>(null);
  const listen = useCallback(async () => {
    mic.current?.close();
    try {
      mic.current = await openMic(device, (_pcm, l) => setLvl(l));
      setPermission("granted");
      setDevices(await microphones());
    } catch {
      setPermission(await micPermission());
    }
  }, [device]);
  useEffect(() => {
    void micPermission().then((p) => {
      setPermission(p);
      void microphones().then(setDevices);
      if (p === "granted") void listen();
    });
    return () => {
      mic.current?.close();
      mic.current = null;
    };
  }, [listen]);
  return (
    <label className="field">
      <span className="t-label">{t("desktop.voice.settings.mic")}</span>
      {permission === "granted" ? (
        <span className="row" style={{ gap: 8 }}>
          <select className="select spacer" value={device} onChange={(e) => onDevice(e.target.value)}>
            <option value="">{t("desktop.voice.settings.mic_system")}</option>
            {devices
              .filter((x) => x.id && x.id !== "default")
              .map((x) => (
                <option key={x.id} value={x.id}>
                  {x.label}
                </option>
              ))}
          </select>
          <span className="voice-level" aria-label={t("desktop.voice.settings.level")} role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(lvl * 100)}>
            <Icon path={mdiMicrophoneOutline} size="sm" />
            <progress max={100} value={Math.round(lvl * 100)} />
          </span>
        </span>
      ) : permission === "none" ? (
        <span className="t-body-sm muted">{t("desktop.voice.settings.mic_none")}</span>
      ) : (
        <span className="row" style={{ gap: 8 }}>
          <span className="spacer t-body-sm muted">{t(permission === "denied" ? "desktop.voice.mic_denied" : "desktop.voice.settings.mic_ask")}</span>
          {permission === "prompt" && (
            <Button small variant="tonal" onClick={() => void listen()}>
              {t("desktop.voice.settings.mic_allow")}
            </Button>
          )}
        </span>
      )}
    </label>
  );
}
