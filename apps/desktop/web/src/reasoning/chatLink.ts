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

import type { Entry } from "../turns";

/**
 * The live call's reasoning for the graph's live card while a turn runs:
 * the open thought's text, "" while the model works without one, and
 * undefined when nothing runs (spec_066_reasoning RSN-12).
 */
export function liveThought(entries: Entry[], running: boolean): string | undefined {
  if (!running) return undefined;
  const last = entries[entries.length - 1];
  return last?.kind === "thought" && last.open ? last.text : "";
}

/** Scrolls the chat to its turn-th prompt (from 0) and marks it a moment; false when it isn't shown. */
export function showPrompt(scroller: HTMLElement | null, turn: number): boolean {
  const el = scroller?.querySelectorAll<HTMLElement>("[data-prompt]")[turn];
  if (!el) return false;
  el.scrollIntoView({ block: "start", behavior: "smooth" });
  el.classList.remove("is-pointed");
  void el.offsetWidth; // restarts the mark's animation
  el.classList.add("is-pointed");
  setTimeout(() => el.classList.remove("is-pointed"), 1600);
  return true;
}
