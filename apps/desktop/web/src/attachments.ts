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

// Images and PDFs attached to the next prompt: pasted, dropped or chosen,
// uploaded to the service at once (AddImage), sent with the turn by ID.
import { language, t, tn } from "./i18n";

/** The largest image the window uploads (the service may scale it down). */
export const maxImageBytes = 20 << 20;

/** The largest PDF the window uploads: what fits a service request. */
export const maxDocumentBytes = 30 << 20;

const pdf = "application/pdf";

/**
 * An image or PDF attached to the next prompt: shown at once from a local
 * URL, uploaded to the service in the background.
 */
export interface Attachment {
  key: string; // local, for the list
  name: string;
  url: string; // a local object URL, for an image's thumbnail ("" for none)
  document?: boolean; // a PDF
  id?: string; // the service's ID once uploaded
  detail?: string; // size and dimensions, once uploaded
  error?: string;
}

/** Whether a file is a PDF, by its type or (dropped from some apps) its name. */
export const isDocument = (f: Pick<File, "type" | "name">) => f.type === pdf || (!f.type && /\.pdf$/i.test(f.name));

/** The image and PDF files among files (paste and drop carry other kinds too). */
export function attachableFiles(files: Iterable<File> | ArrayLike<File> | null | undefined): File[] {
  return Array.from(files ?? []).filter((f) => f.type.startsWith("image/") || isDocument(f));
}

/** Why a file can't be attached ("" if it can). */
export function rejectReason(f: Pick<File, "type" | "size" | "name">): string {
  if (isDocument(f)) return f.size > maxDocumentBytes ? t("desktop.attach.too_large", { name: f.name, size: maxDocumentBytes >> 20 }) : "";
  if (!f.type.startsWith("image/")) return t("desktop.attach.not_image", { name: f.name });
  if (f.size > maxImageBytes) return t("desktop.attach.too_large", { name: f.name, size: maxImageBytes >> 20 });
  return "";
}

/** A short description of an uploaded image or PDF. */
export function describeImage(i: { width: number; height: number; size: bigint | number; resized: boolean; mimeType?: string; pages?: number }): string {
  const bytes = Number(i.size);
  const fmt = (n: number, digits: number) => n.toLocaleString(language(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
  const size = bytes >= 1 << 20 ? t("desktop.attach.mb", { size: fmt(bytes / (1 << 20), 1) }) : t("desktop.attach.kb", { size: fmt(Math.max(1, Math.round(bytes / 1024)), 0) });
  if (i.mimeType === pdf) return `${tn("desktop.attach.pages", i.pages ?? 0)} · ${size}`;
  return `${i.width}×${i.height} · ${size}${i.resized ? ` · ${t("desktop.attach.scaled")}` : ""}`;
}

/** The IDs to send: uploaded images only. */
export const readyIds = (list: Attachment[]) => list.filter((a) => a.id && !a.error).map((a) => a.id!);

/** Whether any image is still uploading. */
export const uploading = (list: Attachment[]) => list.some((a) => !a.id && !a.error);
