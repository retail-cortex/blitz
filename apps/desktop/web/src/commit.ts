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

// The commit dialog's rules: what a staged file's status letter says, how
// long the subject line is, and when Commit can be pressed.

/** The longest subject line git's tools show whole. */
export const subjectLimit = 72;

/** The i18n key for a staged file's git status letter (A, M, D, …). */
export function statusKey(letter: string): string {
  switch (letter) {
    case "A":
      return "desktop.commit.status.added";
    case "D":
      return "desktop.commit.status.deleted";
    case "R":
      return "desktop.commit.status.renamed";
    case "T":
      return "desktop.commit.status.type";
    default:
      return "desktop.commit.status.modified";
  }
}

/** The message's first line, trimmed. */
export function subject(message: string): string {
  return message.trimStart().split("\n", 1)[0].trimEnd();
}

/** Whether Commit can be pressed: something staged, a message, nothing under way. */
export function canCommit(s: { files: number; message: string; busy: boolean }): boolean {
  return s.files > 0 && s.message.trim() !== "" && !s.busy;
}
