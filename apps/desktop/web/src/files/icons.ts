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

// Icons for files by their name.
import {
  mdiCodeJson,
  mdiConsole,
  mdiDatabaseOutline,
  mdiDocker,
  mdiFileCogOutline,
  mdiFileDocumentOutline,
  mdiFileImageOutline,
  mdiFileMusicOutline,
  mdiFileOutline,
  mdiFilePdfBox,
  mdiFileVideoOutline,
  mdiLanguageC,
  mdiLanguageCpp,
  mdiLanguageCss3,
  mdiLanguageGo,
  mdiLanguageHtml5,
  mdiLanguageJava,
  mdiLanguageJavascript,
  mdiLanguageKotlin,
  mdiLanguageMarkdownOutline,
  mdiLanguagePhp,
  mdiLanguagePython,
  mdiLanguageRuby,
  mdiLanguageRust,
  mdiLanguageSwift,
  mdiLanguageTypescript,
} from "@mdi/js";

const byExtension: Record<string, string> = {
  go: mdiLanguageGo,
  ts: mdiLanguageTypescript,
  tsx: mdiLanguageTypescript,
  js: mdiLanguageJavascript,
  jsx: mdiLanguageJavascript,
  mjs: mdiLanguageJavascript,
  cjs: mdiLanguageJavascript,
  py: mdiLanguagePython,
  rs: mdiLanguageRust,
  java: mdiLanguageJava,
  kt: mdiLanguageKotlin,
  swift: mdiLanguageSwift,
  rb: mdiLanguageRuby,
  php: mdiLanguagePhp,
  c: mdiLanguageC,
  h: mdiLanguageC,
  cc: mdiLanguageCpp,
  cpp: mdiLanguageCpp,
  hpp: mdiLanguageCpp,
  html: mdiLanguageHtml5,
  css: mdiLanguageCss3,
  scss: mdiLanguageCss3,
  md: mdiLanguageMarkdownOutline,
  json: mdiCodeJson,
  yaml: mdiFileCogOutline,
  yml: mdiFileCogOutline,
  toml: mdiFileCogOutline,
  bazel: mdiFileCogOutline,
  bzl: mdiFileCogOutline,
  mod: mdiFileCogOutline,
  sh: mdiConsole,
  bash: mdiConsole,
  zsh: mdiConsole,
  sql: mdiDatabaseOutline,
  png: mdiFileImageOutline,
  jpg: mdiFileImageOutline,
  jpeg: mdiFileImageOutline,
  gif: mdiFileImageOutline,
  svg: mdiFileImageOutline,
  webp: mdiFileImageOutline,
  pdf: mdiFilePdfBox,
  wav: mdiFileMusicOutline,
  mp3: mdiFileMusicOutline,
  m4a: mdiFileMusicOutline,
  aac: mdiFileMusicOutline,
  ogg: mdiFileMusicOutline,
  opus: mdiFileMusicOutline,
  flac: mdiFileMusicOutline,
  mp4: mdiFileVideoOutline,
  m4v: mdiFileVideoOutline,
  mov: mdiFileVideoOutline,
  webm: mdiFileVideoOutline,
  avi: mdiFileVideoOutline,
  wmv: mdiFileVideoOutline,
  flv: mdiFileVideoOutline,
  mpeg: mdiFileVideoOutline,
  mpg: mdiFileVideoOutline,
  "3gp": mdiFileVideoOutline,
  txt: mdiFileDocumentOutline,
};

/** The icon for a file, by its name or extension (a plain page if unknown). */
export function fileIcon(name: string): string {
  if (/^dockerfile/i.test(name)) return mdiDocker;
  if (/^(makefile|build|workspace|module)(\.bazel)?$/i.test(name)) return mdiFileCogOutline;
  const ext = name.includes(".") ? name.slice(name.lastIndexOf(".") + 1).toLowerCase() : "";
  return byExtension[ext] ?? mdiFileOutline;
}
