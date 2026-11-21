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

import {
  mdiClipboardCheckOutline,
  mdiConsole,
  mdiConsoleLine,
  mdiDatabaseOutline,
  mdiFileDocumentOutline,
  mdiFileEditOutline,
  mdiFileMusicOutline,
  mdiFilePdfBox,
  mdiFileVideoOutline,
  mdiFileWordBoxOutline,
  mdiFormatListChecks,
  mdiHelpCircleOutline,
  mdiImageOutline,
  mdiMagnify,
  mdiRobotOutline,
  mdiTableSearch,
  mdiVolumeHigh,
  mdiWeb,
  mdiWrenchOutline,
} from "@mdi/js";

/** A web content chip's icon: the console's, or a page's. */
export const webIcon = (kind: string) => (kind === "console" ? mdiConsoleLine : kind === "database" ? mdiDatabaseOutline : kind === "data_query" ? mdiTableSearch : mdiWeb);

/** An attachment's icon by its kind. */
export function kindIcon(kind: string): string {
  return { document: mdiFilePdfBox, text: mdiFileDocumentOutline, audio: mdiFileMusicOutline, video: mdiFileVideoOutline }[kind] ?? mdiImageOutline;
}

/** A sent attachment's kind, by its name (a sent prompt keeps only names). */
export function kindOfName(name: string): string {
  if (/\.pdf$/i.test(name)) return "document";
  if (/\.(mp3|wav|aac|flac|ogg|oga|opus|m4a|weba)$/i.test(name)) return "audio";
  if (/\.(mp4|m4v|mov|mpe?g|webm|avi|wmv|flv|3gp)$/i.test(name)) return "video";
  if (/\.(png|jpe?g|gif|webp|bmp|heic|heif)$/i.test(name)) return "image";
  return "text";
}

/** Icon for a tool call row based on the tool's name. */
export function toolIcon(name: string): string {
  if (name === "export_pdf") return mdiFilePdfBox;
  if (name === "export_docx") return mdiFileWordBoxOutline;
  if (name === "generate_audio") return mdiVolumeHigh;
  if (name === "generate_image") return mdiImageOutline;
  if (name === "view_media") return mdiFileVideoOutline;
  if (/^(read_file|list_files|view_image|view_document)$/.test(name)) return mdiFileDocumentOutline;
  if (/^(glob|grep|list_or_search_skills)$/.test(name)) return mdiMagnify;
  if (/(create|replace|delete|edit|patch|notebook)/.test(name)) return mdiFileEditOutline;
  if (/shell|process/.test(name)) return mdiConsole;
  if (/^web_/.test(name)) return mdiWeb;
  if (/agent/.test(name)) return mdiRobotOutline;
  if (name === "todo") return mdiFormatListChecks;
  if (name === "ask_user_question") return mdiHelpCircleOutline;
  if (/plan_mode/.test(name)) return mdiClipboardCheckOutline;
  return mdiWrenchOutline;
}
