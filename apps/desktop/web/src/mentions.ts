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

// @ mentions in the composer: finding the one being typed, and putting a
// chosen path in its place. The service adds what they name to the prompt.

/** The @mention being typed: where its @ is, and what follows it so far. */
export interface MentionToken {
  start: number; // the @
  end: number; // the caret
  query: string;
}

/**
 * The mention the caret is in: an @ at the start or after whitespace,
 * followed by no whitespace (or, after @", anything but a quote) up to the
 * caret. Null when the caret isn't in one.
 */
export function mentionAt(text: string, caret: number): MentionToken | null {
  const before = text.slice(0, caret);
  const m = /(^|\s)@("[^"]*|[^\s"]*)$/.exec(before);
  if (!m) return null;
  const start = m.index + m[1].length;
  return { start, end: caret, query: m[2].replace(/^"/, "") };
}

/** A path as a mention: quoted when it has a space. */
export function mentionText(path: string): string {
  return /\s/.test(path) ? `@"${path}"` : `@${path}`;
}

/**
 * Replaces the mention being typed with path, and a space after it unless
 * one follows already or it's a folder (so typing goes on inside it);
 * returns the text and where the caret goes.
 */
export function insertMention(text: string, token: MentionToken, path: string): { text: string; caret: number } {
  const after = text.slice(token.end).replace(/^[^\s]*/, ""); // the rest of the word being replaced
  const folder = path.endsWith("/") && !/\s/.test(path);
  const mention = mentionText(path) + (after.startsWith(" ") || folder ? "" : " ");
  const out = text.slice(0, token.start) + mention + after;
  return { text: out, caret: token.start + mention.length };
}

/** Adds a mention of path to a draft (at its end), once. */
export function appendMention(draft: string, path: string): string {
  const m = mentionText(path);
  if (new RegExp(`(^|\\s)${m.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}(\\s|$)`).test(draft)) return draft;
  const sep = draft === "" || /\s$/.test(draft) ? "" : " ";
  return `${draft}${sep}${m} `;
}

/** Whether a path is an image the model can be sent (as pkg/images's IsImagePath). */
export function isImagePath(path: string): boolean {
  return /\.(png|jpe?g|gif|webp)$/i.test(path);
}
