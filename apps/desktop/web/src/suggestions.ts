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

// The welcome screen's tiles: what GetSuggestions returns, worded in the
// window's language (the state tiles carry data, a model's ideas carry
// text), the setup first when the workspace has no agent instructions, and
// the canned tiles when there's nothing else to go on.
import { mdiAutoFix, mdiCalendarAlert, mdiFileCompare, mdiFormatListChecks, mdiHistory, mdiLightbulbOnOutline, mdiLightbulbOutline, mdiMagnify, mdiWrenchOutline } from "@mdi/js";
import { SuggestionKind, type GetSuggestionsResponse } from "./gen/blitz/v1/workspace_pb";
import { t, tn } from "./i18n";

/** What choosing a tile does. */
export type TileAction =
  | { type: "setup" } // run /setup
  | { type: "session"; id: string } // open a conversation
  | { type: "send"; prompt: string } // send a prompt
  | { type: "draft"; text: string }; // put text in the composer

/** One welcome tile. */
export interface Tile {
  key: string;
  icon: string;
  text: string;
  detail?: string;
  /** Shown apart from the others (the setup). */
  highlight?: boolean;
  action: TileAction;
}

/** The most tiles shown. */
export const maxTiles = 4;

/** The canned tiles: put in the composer, to edit before sending. */
export function cannedTiles(): Tile[] {
  return [
    { key: "explain", icon: mdiMagnify },
    { key: "bug", icon: mdiWrenchOutline },
    { key: "tests", icon: mdiFormatListChecks },
    { key: "improve", icon: mdiLightbulbOutline },
  ].map(({ key, icon }) => ({ key, icon, text: t(`desktop.suggest.${key}`), action: { type: "draft", text: t(`desktop.suggest.${key}`) } }));
}

/** The setup tile. */
export function setupTile(): Tile {
  return { key: "setup", icon: mdiAutoFix, text: t("desktop.suggest.setup"), detail: t("desktop.suggest.setup_detail"), highlight: true, action: { type: "setup" } };
}

/**
 * The tiles to show for a GetSuggestions response (undefined: none yet, or
 * it failed): the setup first when it's offered, then the service's tiles
 * in its order, at most maxTiles; the canned ones when it had none.
 */
export function welcomeTiles(res: GetSuggestionsResponse | undefined): Tile[] {
  const setup = !!res && (res.harnessMissing || res.suggestions.some((s) => s.kind === SuggestionKind.SETUP));
  const out: Tile[] = setup ? [setupTile()] : [];
  const own: Tile[] = [];
  for (const [i, s] of (res?.suggestions ?? []).entries()) {
    const key = `${s.kind}-${i}`;
    switch (s.kind) {
      case SuggestionKind.CONTINUE:
        if (s.sessionId) own.push({ key, icon: mdiHistory, text: t("desktop.suggest.continue", { title: s.title.trim() || t("desktop.untitled") }), action: { type: "session", id: s.sessionId } });
        break;
      case SuggestionKind.CHANGES:
        if (s.count > 0) {
          const text = tn("desktop.suggest.changes", s.count);
          own.push({ key, icon: mdiFileCompare, text, action: { type: "send", prompt: text } });
        }
        break;
      case SuggestionKind.WORKER_FAILED:
        if (s.worker) {
          const detail = s.detail.trim();
          own.push({
            key,
            icon: mdiCalendarAlert,
            text: t("desktop.suggest.worker_failed", { worker: s.worker }),
            detail: detail || undefined,
            action: { type: "send", prompt: detail ? t("desktop.suggest.worker_failed_prompt", { worker: s.worker, detail }) : t("desktop.suggest.worker_failed", { worker: s.worker }) },
          });
        }
        break;
      case SuggestionKind.IDEA:
        if (s.title.trim() && s.prompt.trim()) own.push({ key, icon: mdiLightbulbOnOutline, text: s.title.trim(), action: { type: "send", prompt: s.prompt.trim() } });
        break;
    }
  }
  out.push(...(own.length ? own : cannedTiles()));
  return out.slice(0, maxTiles);
}

/** How many times a pending response is asked for again. */
export const pollAttempts = 10;

/** How long to wait before asking again. */
export const pollMs = 3000;

/** Whether to ask again: the ideas are being written, and attempts remain. */
export function shouldPoll(res: GetSuggestionsResponse | undefined, attempts: number): boolean {
  return !!res?.pending && attempts < pollAttempts;
}
