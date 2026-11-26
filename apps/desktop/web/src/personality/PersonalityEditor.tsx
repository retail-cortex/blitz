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

import { useEffect, useState } from "react";
import { mdiEmoticonOutline, mdiPlayOutline, mdiRestore, mdiTestTube } from "@mdi/js";
import { voices, workspaces } from "../api";
import { message } from "../errors";
import type { DescribeVoiceResponse } from "../gen/blitz/v1/voice_pb";
import { language, t } from "../i18n";
import { Player } from "../voice/audio";
import { voiceErrorText } from "../voice/useVoice";
import { MarkdownField } from "../studio/MarkdownField";
import { Button, Dialog } from "@blitz/webui";
import { personalityProblem } from "./personality";
import { isPlain, localeName, pitchLabel, pitchRange, speedLabel, speedOf, speedRange, voiceLocales } from "./tuning";
import { geminiVoices } from "./voices";

/** A personality as the editor has it: its voice's locale ("": the voice's own), speed (1: its own) and pitch in semitones (PER-41). */
export type Draft = { name: string; description: string; voice: string; text: string; locale: string; speed: number; pitch: number };

const same = (a: Draft, b: Draft) => a.name === b.name && a.description === b.description && a.voice === b.voice && a.text === b.text && a.locale === b.locale && speedOf(a.speed) === speedOf(b.speed) && a.pitch === b.pitch;

/**
 * The personality editor (PER-31): its name, description and text (a
 * Markdown field, with the count), its voice when voice is on, with the
 * voice's locale, speed and pitch and Play to hear them (PER-41), Try it
 * on the text as it stands, and Save, which says why it can't before it's
 * pressed. original is the name of the person's own being edited ("" for
 * a new one); closing with changes asks first.
 */
export function PersonalityEditor({
  draft: start,
  original,
  own,
  workspace,
  voiceOn,
  onClose,
  onSave,
}: {
  draft: Draft;
  original: string;
  own: readonly string[];
  workspace: string;
  voiceOn: boolean;
  onClose: () => void;
  onSave: (d: Draft) => Promise<boolean>;
}) {
  const [d, setD] = useState(start);
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [sample, setSample] = useState("");
  const [sampleError, setSampleError] = useState("");
  // Voice's settings, for the locales its voices speak, which engine
  // speaks (the tuning is the cloud engine's), and whether Play can.
  const [voice, setVoice] = useState<DescribeVoiceResponse | null>(null);
  const [playing, setPlaying] = useState(false);
  const [playError, setPlayError] = useState("");
  useEffect(() => {
    if (!voiceOn) return;
    let gone = false;
    voices.describeVoice({ workspace }).then(
      (v) => !gone && setVoice(v),
      () => undefined,
    );
    return () => {
      gone = true;
    };
  }, [voiceOn, workspace]);
  const locales: readonly string[] = voice?.locales.length ? voice.locales : voiceLocales;
  // The tuning is heard with the cloud engine: set, or what automatic comes to for a tuned voice (VOX-70a).
  const live = voice?.engine === "live";
  const cloud = !!voice && !live && !voice.cloudNotSetUp;
  const problem = personalityProblem(d, own, original, locales);
  // A short sample in the voice as it's tuned here, saved or not (one short call).
  const play = async () => {
    setPlaying(true);
    setPlayError("");
    const player = new Player();
    try {
      for await (const m of voices.converse({ workspace, sampleVoice: d.voice || voice?.voice || "Kore", sampleTuned: true, sampleLocale: d.locale, sampleSpeed: speedOf(d.speed), samplePitch: d.pitch })) {
        if (m.audio.length) player.play(m.audio);
        if (m.error) setPlayError(m.error);
        if (m.ended) break;
      }
    } catch (e) {
      setPlayError(voiceErrorText(e));
    } finally {
      setTimeout(() => player.close(), 15000);
      setPlaying(false);
    }
  };
  const count = [...d.text].length;
  const close = () => (same(d, start) ? onClose() : setAsking(true));
  const tryIt = async () => {
    setBusy(true);
    setSampleError("");
    try {
      setSample((await workspaces.tryPersonality({ workspace, text: d.text })).answer);
    } catch (e) {
      setSampleError(message(e));
    } finally {
      setBusy(false);
    }
  };
  const save = async () => {
    setBusy(true);
    await onSave(d);
    setBusy(false);
  };
  return (
    <Dialog
      title={original ? t("desktop.personality.edit_title", { name: original }) : t("desktop.personality.new_title")}
      icon={mdiEmoticonOutline}
      large
      onClose={close}
      footer={
        <>
          <Button onClick={close}>{t("desktop.cancel")}</Button>
          <Button variant="filled" disabled={busy || !!problem} onClick={() => void save()}>
            {t("desktop.personality.save")}
          </Button>
        </>
      }
    >
      <div className="stack personality-editor" style={{ gap: 12 }}>
        <label className="field">
          <span className="t-label">{t("desktop.personality.name")}</span>
          <input className="input mono" value={d.name} disabled={!!original || busy} spellCheck={false} placeholder={t("desktop.personality.name_placeholder")} onChange={(e) => setD({ ...d, name: e.target.value.trim().toLowerCase() })} />
          <span className="t-body-sm muted">{t("desktop.personality.name_hint")}</span>
        </label>
        <label className="field">
          <span className="t-label">{t("desktop.personality.description")}</span>
          <input className="input" value={d.description} disabled={busy} placeholder={t("desktop.personality.description_placeholder")} onChange={(e) => setD({ ...d, description: e.target.value })} />
        </label>
        <MarkdownField
          label={t("desktop.personality.text")}
          supporting={<span>{t("desktop.personality.count", { count })}</span>}
          value={d.text}
          onValue={(text) => setD({ ...d, text })}
          dir={workspace}
          path="personality.md"
          workspace={workspace}
          placeholder={t("desktop.personality.text_placeholder")}
          rows={8}
          disabled={busy}
          tools={
            <Button small icon={mdiTestTube} disabled={busy || !workspace || !d.text.trim()} onClick={() => void tryIt()}>
              {t("desktop.personality.try")}
            </Button>
          }
        />
        {(sample || sampleError) && (
          <section className="card outlined stack personality-try" aria-live="polite" style={{ gap: 6 }}>
            {sampleError ? <p className="error-text">{sampleError}</p> : <p className="t-body">{sample}</p>}
          </section>
        )}
        {voiceOn && (
          <label className="field">
            <span className="t-label">{t("desktop.personality.voice")}</span>
            <select className="select" value={d.voice} disabled={busy} onChange={(e) => setD({ ...d, voice: e.target.value })}>
              <option value="">{t("desktop.personality.voice_setting")}</option>
              {geminiVoices.map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
            <span className="t-body-sm muted">{t("desktop.personality.voice_hint")}</span>
          </label>
        )}
        {voiceOn && (
          <section className="stack personality-tuning" style={{ gap: 12 }}>
            <label className="field">
              <span className="t-label">{t("desktop.personality.locale")}</span>
              <select className="select" value={d.locale} disabled={busy} onChange={(e) => setD({ ...d, locale: e.target.value })}>
                <option value="">{t("desktop.personality.locale_own")}</option>
                {d.locale && !locales.includes(d.locale) && <option value={d.locale}>{d.locale}</option>}
                {[...locales]
                  .map((code) => ({ code, name: localeName(code, language()) }))
                  .sort((a, b) => a.name.localeCompare(b.name, language()))
                  .map((l) => (
                    <option key={l.code} value={l.code}>
                      {l.name === l.code ? l.code : `${l.name} (${l.code})`}
                    </option>
                  ))}
              </select>
              <span className="t-body-sm muted">{t("desktop.personality.locale_hint")}</span>
            </label>
            <label className="field">
              <span className="row" style={{ gap: 8 }}>
                <span className="t-label spacer">{t("desktop.personality.speed")}</span>
                <output className="t-body-sm mono">{speedLabel(d.speed)}</output>
              </span>
              <input type="range" aria-label={t("desktop.personality.speed")} min={speedRange.min} max={speedRange.max} step={speedRange.step} value={speedOf(d.speed)} disabled={busy} onChange={(e) => setD({ ...d, speed: Number(e.target.value) })} />
              <span className="t-body-sm muted">{t("desktop.personality.speed_hint")}</span>
            </label>
            <label className="field">
              <span className="row" style={{ gap: 8 }}>
                <span className="t-label spacer">{t("desktop.personality.pitch")}</span>
                <output className="t-body-sm mono">{t("desktop.personality.pitch_value", { pitch: pitchLabel(d.pitch) })}</output>
              </span>
              <input type="range" aria-label={t("desktop.personality.pitch")} min={pitchRange.min} max={pitchRange.max} step={pitchRange.step} value={d.pitch} disabled={busy} onChange={(e) => setD({ ...d, pitch: Number(e.target.value) })} />
              <span className="t-body-sm muted">{t("desktop.personality.pitch_hint")}</span>
            </label>
            <div className="row" style={{ gap: 8 }}>
              <Button small icon={mdiPlayOutline} disabled={busy || playing || !workspace || !cloud || !!problem} onClick={() => void play()}>
                {t(playing ? "desktop.voice.settings.playing" : "desktop.voice.settings.play")}
              </Button>
              <Button small icon={mdiRestore} disabled={busy || isPlain(d)} onClick={() => setD({ ...d, locale: "", speed: 1, pitch: 0 })}>
                {t("desktop.personality.tuning_reset")}
              </Button>
            </div>
            {live && <p className="t-body-sm muted">{t("desktop.personality.tuning_live")}</p>}
            {voice && !live && voice.cloudNotSetUp && <p className="t-body-sm muted">{t("desktop.personality.tuning_not_set_up")}</p>}
            {playError && <p className="t-body-sm error-text">{playError}</p>}
          </section>
        )}
        {problem && (d.name || d.text) && <p className="t-body-sm error-text">{problem}</p>}
      </div>
      {asking && (
        <Dialog
          title={t("desktop.personality.discard_title")}
          onClose={() => setAsking(false)}
          footer={
            <>
              <Button onClick={() => setAsking(false)}>{t("desktop.personality.keep_editing")}</Button>
              <Button variant="filled" danger onClick={onClose}>
                {t("desktop.personality.discard")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.personality.discard_body")}</p>
        </Dialog>
      )}
    </Dialog>
  );
}
