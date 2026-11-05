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

// Sizes in bytes for people, in the page's language: the one way the
// window writes them (attachments, the editor, a service job's files).
import { language, t } from "./i18n";

/** A size in bytes: whole KB under a megabyte (at least 1), MB with one decimal from there, digits as the language writes them. */
export function formatBytes(n: bigint | number): string {
  const bytes = Number(n);
  const fmt = (v: number, digits: number) => v.toLocaleString(language(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
  return bytes >= 1 << 20 ? t("desktop.attach.mb", { size: fmt(bytes / (1 << 20), 1) }) : t("desktop.attach.kb", { size: fmt(Math.max(1, Math.round(bytes / 1024)), 0) });
}
