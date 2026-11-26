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
import { language, t } from "../i18n";
import { useModelCatalog } from "../ModelInput";
import { modelOptions } from "../models";
import { localeName } from "../personality/tuning";
import { configChanged, openSettings } from "../events";
import { useApp } from "../state";
import { Button, Icon, Segmented, Switch, useSnackbar } from "@blitz/webui";
import { micPermission, microphones, openMic, Player, type Mic } from "./audio";
import { voiceErrorText } from "./useVoice";
import { hearChoice, hearChoices } from "./voice";

/**
 * Settings › Personality › Voice (spec_063_voice VOX-40, VOX-41): the switch, and with
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
  // As set: automatic unless the person chose one (VOX-70a).
  const engine = d.engine === "cloud" || d.engine === "live" ? d.engine : "auto";
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
          <label className="field">
            <span className="t-label">{t("desktop.voice.settings.engine")}</span>
            <Segmented<string>
              label={t("desktop.voice.settings.engine")}
              value={engine}
              onChange={(v) => void set("voice.engine", v === "auto" ? "" : v)}
              options={[
                { value: "auto", label: t("desktop.voice.settings.engine.auto") },
                { value: "live", label: t("desktop.voice.settings.engine.live") },
                { value: "cloud", label: t("desktop.voice.settings.engine.cloud") },
              ]}
            />
            <span className="t-body-sm muted">
              {engine === "cloud" ? t("desktop.voice.settings.engine.cloud_detail") : engine === "live" ? t("desktop.voice.settings.engine.live_detail") : t("desktop.voice.settings.engine.auto_detail")}
            </span>
          </label>
          {engine !== "live" && <CloudEngine d={d} workspace={workspace} busy={busy} onSet={(key, v) => set(key, v)} />}
          {engine !== "cloud" &&
            (d.notSetUp ? (
            <div className="card warn row" style={{ gap: 8 }}>
              <span className="spacer t-body-sm">{t("desktop.voice.settings.not_set_up")}</span>
              <Button small variant="tonal" onClick={() => openSettings({ section: "providers" })}>
                {t("desktop.voice.settings.set_up")}
              </Button>
            </div>
          ) : (
            <label className="field">
              <span className="t-label">{t("desktop.voice.settings.live_model")}</span>
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
          ))}
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
          {d.listen !== "hold" && (
            <>
              <label className="field">
                <span className="t-label">{t("desktop.voice.settings.noise")}</span>
                <Segmented<string>
                  label={t("desktop.voice.settings.noise")}
                  value={d.noise || "normal"}
                  onChange={(v) => void set("voice.noise", v)}
                  options={[
                    { value: "normal", label: t("desktop.voice.settings.noise.normal") },
                    { value: "noisy", label: t("desktop.voice.settings.noise.noisy") },
                  ]}
                />
                <span className="t-body-sm muted">{t("desktop.voice.settings.noise_detail")}</span>
              </label>
              <MsField label={t("desktop.voice.settings.pause")} detail={t("desktop.voice.settings.pause_detail", { min: 200, max: 5000 })} value={d.pauseMs} min={200} max={5000} busy={busy} onSet={(v) => void set("voice.pause_ms", v)} />
              {engine !== "cloud" && <MsField label={t("desktop.voice.settings.start")} detail={t("desktop.voice.settings.start_detail", { min: 20, max: 2000 })} value={d.startMs} min={20} max={2000} busy={busy} onSet={(v) => void set("voice.start_ms", v)} />}
            </>
          )}
          <MsField label={t("desktop.voice.settings.buffer")} detail={t("desktop.voice.settings.buffer_detail", { min: 20, max: 2000 })} value={d.bufferMs} min={20} max={2000} busy={busy} onSet={(v) => void set("voice.buffer_ms", v)} />
          {engine !== "cloud" && <p className="t-body-sm muted">{t("desktop.voice.settings.cost")}</p>}
          {engine !== "live" && <p className="t-body-sm muted">{t("desktop.voice.settings.cost_cloud")}</p>}
        </>
      )}
    </div>
  );
}

/**
 * The cloud engine's settings (VOX-70, VOX-74): what it's missing, in the
 * service's words (they say what to run), else the Google Cloud project
 * it works in; the language you speak, for Speech-to-Text; the model
 * that writes what's said, from those the providers list; and how it
 * hears, from the Speech-to-Text models tried (VOX-73a). Menus, not
 * names to type.
 */
function CloudEngine({ d, workspace, busy, onSet }: { d: DescribeVoiceResponse; workspace: string; busy: boolean; onSet: (key: string, value: string) => Promise<void> }) {
  // The models the providers list, for the one that writes the replies.
  const catalog = useModelCatalog(workspace);
  const models = modelOptions(catalog, d.textModel ? [d.textModel] : []);
  const heard = hearChoice(d.hearModel, d.hearLocation);
  const hearLabels: Record<(typeof hearChoices)[number]["id"], string> = {
    default: t("desktop.voice.settings.hear.default"),
    telephony: t("desktop.voice.settings.hear.telephony"),
    chirp_3: t("desktop.voice.settings.hear.chirp_3"),
    chirp_2: t("desktop.voice.settings.hear.chirp_2"),
  };
  // Both settings, one after the other: a model and where it runs go together.
  const hear = async (id: string) => {
    const c = hearChoices.find((x) => x.id === id);
    if (!c) return;
    await onSet("voice.hear_model", c.model);
    await onSet("voice.hear_location", c.location);
  };
  return (
    <>
      {d.cloudNotSetUp ? (
        <div className="card warn stack" style={{ gap: 4 }}>
          <span className="t-title-sm">{t("desktop.voice.settings.cloud_not_set_up")}</span>
          <span className="t-body-sm">{d.cloudNotSetUp}</span>
        </div>
      ) : (
        <p className="t-body-sm muted">{t("desktop.voice.settings.cloud_project", { project: d.cloudProject })}</p>
      )}
      <label className="field">
        <span className="t-label">{t("desktop.voice.settings.language")}</span>
        <select className="select" value={d.language} disabled={busy} onChange={(e) => void onSet("voice.language", e.target.value)}>
          <option value="">{t("desktop.voice.settings.language_default")}</option>
          {[...d.locales]
            .map((code) => ({ code, name: localeName(code, language()) }))
            .sort((a, b) => a.name.localeCompare(b.name, language()))
            .map((l) => (
              <option key={l.code} value={l.code}>
                {l.name === l.code ? l.code : `${l.name} (${l.code})`}
              </option>
            ))}
        </select>
        <span className="t-body-sm muted">{t("desktop.voice.settings.language_detail")}</span>
      </label>
      <label className="field">
        <span className="t-label">{t("desktop.voice.settings.text_model")}</span>
        <select className="select" value={d.textModel} disabled={busy} onChange={(e) => void onSet("voice.text_model", e.target.value)}>
          <option value="">{t("desktop.voice.settings.text_model_default", { model: d.textModelInUse || "…" })}</option>
          {models.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
        <span className="t-body-sm muted">{t("desktop.voice.settings.text_model_detail")}</span>
      </label>
      <label className="field">
        <span className="t-label">{t("desktop.voice.settings.hear_model")}</span>
        <select className="select" value={heard?.id ?? "custom"} disabled={busy} onChange={(e) => void hear(e.target.value)}>
          {!heard && <option value="custom">{t("desktop.voice.settings.hear.custom", { model: d.hearModel || "long", location: d.hearLocation || "global" })}</option>}
          {hearChoices.map((c) => (
            <option key={c.id} value={c.id}>
              {hearLabels[c.id]}
            </option>
          ))}
        </select>
        <span className="t-body-sm muted">{t("desktop.voice.settings.hear_model_detail")}</span>
      </label>
    </>
  );
}

/**
 * A setting in milliseconds (VOX-08), its label and detail in words: empty
 * is the default (0); a number within min and max is saved when the field
 * is left.
 */
function MsField({ label, detail, value, min, max, busy, onSet }: { label: string; detail: string; value: number; min: number; max: number; busy: boolean; onSet: (v: string) => void }) {
  return (
    <label className="field">
      <span className="t-label">{label}</span>
      <input
        className="input"
        type="number"
        min={min}
        max={max}
        step={10}
        placeholder={t("desktop.voice.settings.ms_default")}
        defaultValue={value || ""}
        disabled={busy}
        onBlur={(e) => {
          const raw = e.target.value.trim();
          if (raw === "") {
            if (value) onSet("");
            return;
          }
          const n = Math.round(Number(raw));
          if (n !== value && n >= min && n <= max) onSet(String(n));
        }}
      />
      <span className="t-body-sm muted">{detail}</span>
    </label>
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
