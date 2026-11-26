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
// How a personality's voice is tuned (spec_064_personality PER-41), with
// the cloud voice engine (spec_063_voice VOX-70): the locale it speaks,
// its speed and its pitch. The service checks what's saved
// (config.VoiceTuning.Check); the page checks the same as the sliders
// move, so Save says why before it's pressed.
import { t } from "../i18n";

/** A voice's tuning: its locale ("": the voice's own), speed (1: its own) and pitch in semitones (0: its own). */
export type Tuning = { locale: string; speed: number; pitch: number };

/** A voice as it comes: nothing changed. */
export const plainTuning: Tuning = { locale: "", speed: 1, pitch: 0 };

/** The speed slider's range and step (config.MinVoiceSpeed, MaxVoiceSpeed). */
export const speedRange = { min: 0.5, max: 2, step: 0.05 } as const;
/** The pitch slider's, in semitones (config.MinVoicePitch, MaxVoicePitch). */
export const pitchRange = { min: -6, max: 6, step: 0.5 } as const;

/** How far Text-to-Speech's speaking rate goes (config.MinSpeakingRate, MaxSpeakingRate). */
const rateRange = { min: 0.25, max: 2 } as const;

/** The locales the voices speak (config.VoiceLocales): what the service lists, else these. */
export const voiceLocales = [
  "ar-XA", "bg-BG", "bn-IN", "cmn-CN", "cs-CZ", "da-DK", "de-DE", "el-GR", "en-AU", "en-GB", "en-IN", "en-US",
  "es-ES", "es-US", "et-EE", "fi-FI", "fr-CA", "fr-FR", "gu-IN", "he-IL", "hi-IN", "hr-HR", "hu-HU", "id-ID",
  "it-IT", "ja-JP", "kn-IN", "ko-KR", "lt-LT", "lv-LV", "ml-IN", "mr-IN", "nb-NO", "nl-BE", "nl-NL", "pa-IN",
  "pl-PL", "pt-BR", "ro-RO", "ru-RU", "sk-SK", "sl-SI", "sr-RS", "sv-SE", "sw-KE", "ta-IN", "te-IN", "th-TH",
  "tr-TR", "uk-UA", "ur-IN", "vi-VN", "yue-HK",
] as const;

/** A speed as the service keeps it: 0 (unset) is the voice's own, 1. */
export const speedOf = (speed: number) => (speed > 0 ? speed : 1);

/**
 * The rate Text-to-Speech is asked for (config.VoiceTuning.SpeakingRate):
 * the voices take a speaking rate and no pitch, so a pitch is made by
 * playing the speech faster or slower, and the speech is asked for that
 * much slower or faster.
 */
export const speakingRate = (tn: Tuning) => speedOf(tn.speed) / 2 ** (tn.pitch / 12);

/** Why a tuning can't be spoken, in the page's words, or "" (config.VoiceTuning.Check). */
export function tuningProblem(tn: Tuning, locales: readonly string[] = voiceLocales): string {
  if (tn.locale && !locales.includes(tn.locale)) return t("desktop.personality.problem.locale", { locale: tn.locale });
  const speed = speedOf(tn.speed);
  if (!(speed >= speedRange.min && speed <= speedRange.max)) return t("desktop.personality.problem.speed", { min: speedRange.min, max: speedRange.max });
  if (!(tn.pitch >= pitchRange.min && tn.pitch <= pitchRange.max)) return t("desktop.personality.problem.pitch", { min: pitchRange.min, max: pitchRange.max });
  const rate = speakingRate(tn);
  if (rate < rateRange.min - 1e-9 || rate > rateRange.max + 1e-9) return t("desktop.personality.problem.together");
  return "";
}

/** Whether a tuning changes nothing. */
export const isPlain = (tn: Tuning) => !tn.locale && speedOf(tn.speed) === 1 && tn.pitch === 0;

/** A speed to show: "0.90×". */
export const speedLabel = (speed: number) => `${speedOf(speed).toFixed(2)}×`;

/** A pitch to show: "+1.5", "−2", "0" (semitones). */
export const pitchLabel = (pitch: number) => (pitch === 0 ? "0" : `${pitch > 0 ? "+" : "−"}${Math.abs(pitch).toFixed(1).replace(/\.0$/, "")}`);

/** A locale's name in the page's language: "English (United Kingdom)", or its code when the browser has none. */
export function localeName(code: string, language: string): string {
  // Google's own codes for Mandarin, Cantonese and Arabic, which browsers name differently or not at all.
  const tag = { "cmn-CN": "zh-CN", "yue-HK": "yue-HK", "ar-XA": "ar" }[code] ?? code;
  try {
    const name = new Intl.DisplayNames([language], { type: "language" }).of(tag);
    return name && name !== tag ? name : code;
  } catch {
    return code;
  }
}

/** A tuning in a few words, for a personality's row: "English (United Kingdom) · 0.90× · −2". */
export function tuningSummary(tn: Tuning, language: string): string {
  return [tn.locale ? localeName(tn.locale, language) : "", speedOf(tn.speed) !== 1 ? speedLabel(tn.speed) : "", tn.pitch !== 0 ? t("desktop.personality.pitch_named", { pitch: pitchLabel(tn.pitch) }) : ""].filter(Boolean).join(" · ");
}
