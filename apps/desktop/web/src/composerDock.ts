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

// The pinned composer: with prefs.composer "pinned", the shown workspace's
// composer (and its task list) renders into a bar across the window's foot,
// above the status bar, instead of under its conversation. The
// conversation still owns it (a portal), so drafts, steering, @ completion
// and attachments work the same in either place.
import { createContext } from "react";

/** The bar the pinned composer renders into; null when it's with the conversation. */
export const ComposerDock = createContext<HTMLElement | null>(null);

/** Asks the shown workspace's composer to take the keyboard (⌘L / Ctrl+L). */
export const focusComposerEvent = "blitz:focus-composer";

/** Whether a key press is the shortcut that focuses the composer. */
export function isFocusComposerKey(e: Pick<KeyboardEvent, "key" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey">): boolean {
  return (e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "l";
}
