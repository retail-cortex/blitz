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

// Pictures (spec_058_pictures): what the panel shows for a model, and what
// it keeps when the model changes. The service checks every request
// against the same catalog (PIC-62); these keep the panel from offering
// what would be refused.
import type { PictureModel, PictureTurn } from "../gen/blitz/v1/workspace_pb";

/** A reference picture's role (PIC-23). */
export type Role = "character" | "object" | "style";
/** The roles, in the order the panel offers them. */
export const roles: Role[] = ["character", "object", "style"];

/** The panel's settings: everything but the prompt and the references. */
export interface Settings {
  model: string;
  count: number;
  ratio: string;
  size: string;
  thinking: string;
  search: boolean;
  searchImages: boolean;
  avoid: string;
  people: string;
  format: string;
  quality: number;
  seed: string;
  enhance: boolean;
}

/** The settings before any are chosen. */
export const defaultSettings: Settings = { model: "", count: 1, ratio: "1:1", size: "1K", thinking: "MEDIUM", search: false, searchImages: false, avoid: "", people: "", format: "image/png", quality: 90, seed: "", enhance: false };

/** The panoramic ratios, listed after the standard ones (PIC-25). */
export const panoramic = new Set(["1:4", "4:1", "1:8", "8:1"]);

/** Which controls a model shows (PIC-21..29): the visibility matrix as data. */
export interface Controls {
  references: boolean;
  countLocked: boolean;
  sizes: boolean;
  thinking: boolean;
  search: boolean;
  searchImages: boolean;
  avoid: boolean;
  people: boolean;
  seed: boolean;
  enhance: boolean;
  formats: boolean;
}

/** What m lets the panel offer. */
export function controls(m: PictureModel | undefined): Controls {
  return {
    references: !!m && m.maxReferences > 0,
    countLocked: !m || m.maxPictures <= 1,
    sizes: !!m && m.sizes.length > 1,
    thinking: !!m && m.thinking.length > 0,
    search: !!m && m.search !== "",
    searchImages: m?.search === "image",
    avoid: !!m && m.negativePrompt,
    people: !!m && m.people,
    seed: !!m && m.seed,
    enhance: !!m && m.enhance,
    formats: !!m && m.formats.length > 1,
  };
}

/** A setting a model change put back, said to the person (PIC-21). */
export interface Reset {
  setting: "ratio" | "size" | "count" | "thinking" | "search" | "avoid" | "people" | "format" | "seed" | "enhance";
  from: string;
  to: string;
}

/**
 * The settings for model m: every one it allows kept, the rest back to its
 * defaults, with what was put back.
 */
export function fitSettings(s: Settings, m: PictureModel): { settings: Settings; resets: Reset[] } {
  const out = { ...s, model: m.ref };
  const resets: Reset[] = [];
  const put = <K extends keyof Settings>(k: K, v: Settings[K], setting: Reset["setting"]) => {
    if (out[k] !== v) {
      if (String(out[k]) !== "" && out[k] !== false) resets.push({ setting, from: String(out[k]), to: String(v) });
      out[k] = v;
    }
  };
  if (!m.ratios.includes(out.ratio)) put("ratio", m.ratios.includes("1:1") ? "1:1" : (m.ratios[0] ?? ""), "ratio");
  if (m.sizes.length === 0) out.size = "";
  else if (!m.sizes.includes(out.size)) put("size", m.sizes.includes("1K") ? "1K" : m.sizes[0], "size");
  if (out.count > Math.max(m.maxPictures, 1)) put("count", Math.max(m.maxPictures, 1), "count");
  if (m.thinking.length === 0) out.thinking = "";
  else if (!m.thinking.includes(out.thinking)) put("thinking", m.thinking.includes("MEDIUM") ? "MEDIUM" : m.thinking[0], "thinking");
  if (!m.search) put("search", false, "search");
  if (m.search !== "image") out.searchImages = false;
  if (!m.negativePrompt) put("avoid", "", "avoid");
  if (!m.people) put("people", "", "people");
  if (!m.seed) put("seed", "", "seed");
  if (!m.enhance) put("enhance", false, "enhance");
  if (!m.formats.includes(out.format)) put("format", m.formats[0] ?? "image/png", "format");
  return { settings: out, resets };
}

/** A reference as the panel holds it, before it's sent. */
export interface Ref {
  key: string;
  role: Role;
  name: string;
  mime: string;
  /** A workspace file, a picture of the session, or bytes from the computer. */
  path?: string;
  picture?: string;
  data?: Uint8Array;
  /** What the thumbnail shows. */
  url: string;
}

/** The reference types image models take. */
export const referenceTypes = ["image/png", "image/jpeg", "image/webp"];
/** The most a reference picture may be, in bytes. */
export const maxReferenceBytes = 7 << 20;

/** Why refs (with one more of role) can't go to m, or "" when they can. */
export function refProblem(m: PictureModel | undefined, refs: Ref[], role: Role): "none" | "full" | "role" | "" {
  if (!m || m.maxReferences === 0) return "none";
  if (refs.length >= m.maxReferences) return "full";
  const of = refs.filter((r) => r.role === role).length;
  if ((role === "character" && m.maxCharacters > 0 && of >= m.maxCharacters) || (role === "object" && m.maxObjects > 0 && of >= m.maxObjects)) return "role";
  return "";
}

/** A ratio's shape for its icon, the longer side 18: [width, height]. */
export function ratioShape(ratio: string): [number, number] {
  const [a, b] = ratio.split(":").map(Number);
  if (!a || !b) return [18, 18];
  return a >= b ? [18, Math.max(3, Math.round((18 * b) / a))] : [Math.max(3, Math.round((18 * a) / b)), 18];
}

/** A picture's file extension for its type. */
export function extensionOf(mime: string): string {
  return mime === "image/jpeg" ? "jpg" : mime === "image/webp" ? "webp" : "png";
}

// The panel's settings are kept per workspace for the next time (PIC-30):
// a convenience, in this window's storage.
const storeKey = (dir: string) => `blitz.pictures.${dir}`;

/** The settings kept for dir (the defaults when none, or storage refuses). */
export function loadSettings(dir: string): Settings {
  try {
    const raw = localStorage.getItem(storeKey(dir));
    return raw ? { ...defaultSettings, ...(JSON.parse(raw) as Partial<Settings>) } : { ...defaultSettings };
  } catch {
    return { ...defaultSettings };
  }
}

/** Keeps dir's settings for next time. */
export function keepSettings(dir: string, s: Settings) {
  try {
    localStorage.setItem(storeKey(dir), JSON.stringify(s));
  } catch {
    // storage refused: the panel opens on its defaults next time
  }
}

/** The picture models whose turns stand alone (PIC-42): no conversation, so a turn can be made again. */
export function standsAlone(kind: string): boolean {
  return kind === "imagen" || kind === "openai";
}

/** A seed for Vary: any of the model's, from 1. */
export function newSeed(): number {
  const n = new Uint32Array(1);
  crypto.getRandomValues(n);
  return (n[0] % 2147483646) + 1;
}

/**
 * The request that makes turn again in session (PIC-42): Try again its
 * prompt, references and settings as they were (its seed too, if it had
 * one); Vary the same with seed instead. What a turn doesn't keep
 * (prompt enhancement, JPEG quality) comes from the panel's settings now.
 */
export function againRequest(workspace: string, session: string, turn: PictureTurn, now: Pick<Settings, "enhance" | "quality">, seed?: number) {
  return {
    workspace,
    session,
    model: turn.model,
    prompt: turn.prompt,
    references: turn.references.map((r) => ({ picture: r.file, mime: r.mime, name: r.name, role: r.role })),
    count: Math.max(turn.count, 1),
    aspectRatio: turn.aspectRatio,
    size: turn.size,
    thinking: turn.thinking,
    search: turn.search !== "",
    searchImages: turn.search === "image",
    avoid: turn.avoid,
    people: turn.people,
    seed: seed ?? turn.seed,
    useSeed: seed !== undefined || turn.hasSeed,
    enhance: now.enhance,
    format: turn.format,
    quality: turn.format === "image/jpeg" ? now.quality : 0,
    startFrom: "",
    estimate: false,
  };
}
