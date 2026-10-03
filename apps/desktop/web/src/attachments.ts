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

// Files attached to the next prompt: pasted, dropped or chosen, uploaded
// to the service at once (AddImage), sent with the turn by ID. Which files
// can be is the active agent's model's to say (GetSettings
// accepted_media): the picker, paste and drop offer only those.
import { language, t, tn } from "./i18n";

/** The largest file the window uploads: what fits a service request (larger ones attach from the workspace). */
export const maxUploadBytes = 30 << 20;

const pdf = "application/pdf";

/** A kind of attachment the model takes (the service's AcceptedMedia). */
export interface Accepted {
  kind: string;
  mimeTypes: string[];
  extensions: string[];
  maxBytes: bigint | number;
}

/**
 * A file attached to the next prompt: shown at once from a local URL (an
 * image's thumbnail), uploaded to the service in the background.
 */
export interface Attachment {
  key: string; // local, for the list
  name: string;
  url: string; // a local object URL, for an image's thumbnail ("" for none)
  kind?: string; // image, document, text, audio or video
  document?: boolean; // a PDF
  id?: string; // the service's ID once uploaded
  detail?: string; // size and dimensions, once uploaded
  error?: string;
}

const extOf = (name: string) => {
  const i = name.lastIndexOf(".");
  return i < 0 ? "" : name.slice(i).toLowerCase();
};

/** The kind the model takes a file as, by its name, else its type; undefined when it doesn't take it. */
export function acceptedAs(list: Accepted[], f: Pick<File, "type" | "name">): Accepted | undefined {
  const ext = extOf(f.name);
  return (ext && list.find((a) => a.extensions.includes(ext))) || list.find((a) => f.type && a.mimeTypes.includes(f.type));
}

/** Whether a file is a PDF, by its type or its name. */
export const isDocument = (f: Pick<File, "type" | "name">) => f.type === pdf || /\.pdf$/i.test(f.name);

/** The file picker's accept list: every extension and type the model takes. */
export function acceptAttribute(list: Accepted[]): string {
  return [...new Set(list.flatMap((a) => [...a.extensions, ...a.mimeTypes]))].join(",");
}

/** The files among files the model takes (paste and drop carry others too). */
export function attachableFiles(files: Iterable<File> | ArrayLike<File> | null | undefined, list: Accepted[]): File[] {
  return Array.from(files ?? []).filter((f) => !!acceptedAs(list, f));
}

/** Why a file can't be attached ("" if it can): a type the model doesn't take, or too large. */
export function rejectReason(f: Pick<File, "type" | "size" | "name">, list: Accepted[], model: string): string {
  const a = acceptedAs(list, f);
  if (!a) return t("desktop.attach.unsupported", { name: f.name, model: model || "this model" });
  const limit = Math.min(Number(a.maxBytes), maxUploadBytes);
  if (f.size > limit) return f.size > maxUploadBytes && Number(a.maxBytes) > maxUploadBytes ? t("desktop.attach.from_workspace", { name: f.name }) : t("desktop.attach.too_large", { name: f.name, size: Math.round(limit / (1 << 20)) });
  return "";
}

/** m:ss (or h:mm:ss) for a length in seconds. */
export function clock(seconds: number): string {
  const s = Math.round(seconds);
  const mm = String(Math.floor(s / 60) % 60).padStart(s >= 3600 ? 2 : 1, "0");
  const ss = String(s % 60).padStart(2, "0");
  return s >= 3600 ? `${Math.floor(s / 3600)}:${mm}:${ss}` : `${mm}:${ss}`;
}

/** A short description of an uploaded file: dimensions, pages or length, and size. */
export function describeImage(i: { width: number; height: number; size: bigint | number; resized: boolean; mimeType?: string; pages?: number; kind?: string; seconds?: number }): string {
  const bytes = Number(i.size);
  const fmt = (n: number, digits: number) => n.toLocaleString(language(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
  const size = bytes >= 1 << 20 ? t("desktop.attach.mb", { size: fmt(bytes / (1 << 20), 1) }) : t("desktop.attach.kb", { size: fmt(Math.max(1, Math.round(bytes / 1024)), 0) });
  if (i.mimeType === pdf) return `${tn("desktop.attach.pages", i.pages ?? 0)} · ${size}`;
  if (i.kind === "audio" || i.kind === "video") return i.seconds ? `${clock(i.seconds)} · ${size}` : size;
  if (i.kind === "text" || !i.width) return size;
  return `${i.width}×${i.height} · ${size}${i.resized ? ` · ${t("desktop.attach.scaled")}` : ""}`;
}

/** The IDs to send: uploaded images only. */
export const readyIds = (list: Attachment[]) => list.filter((a) => a.id && !a.error).map((a) => a.id!);

/** Whether any image is still uploading. */
export const uploading = (list: Attachment[]) => list.some((a) => !a.id && !a.error);
