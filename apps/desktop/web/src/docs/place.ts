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

// Where the user guide was left (DSK-103): its page and how far down, so
// Help › User guide opens there again; Tutorials the first time. Kept in
// this window's storage, a convenience: without it (a private window,
// storage cleared or refused) the guide opens on Tutorials.

/** A place in the docs: a page by its site path, and the scroll in it. */
export interface DocsPlace {
  page: string;
  scroll?: number;
}

/** Where the guide opens the first time. */
export const firstPage = "tutorials";

const key = "blitz.desktop.docs";

/** Where the guide was left, or Tutorials. */
export function lastPlace(storage: Pick<Storage, "getItem"> | undefined = globalThis.localStorage): DocsPlace {
  try {
    const v = JSON.parse(storage?.getItem(key) ?? "null") as Partial<DocsPlace> | null;
    if (v && typeof v.page === "string") return { page: v.page, scroll: typeof v.scroll === "number" && v.scroll > 0 ? v.scroll : undefined };
  } catch {
    // unreadable: start over
  }
  return { page: firstPage };
}

/** Remembers where the guide is. */
export function savePlace(place: DocsPlace, storage: Pick<Storage, "setItem"> | undefined = globalThis.localStorage) {
  try {
    storage?.setItem(key, JSON.stringify({ page: place.page, scroll: Math.round(place.scroll ?? 0) }));
  } catch {
    // storage refused: the guide starts on Tutorials next time
  }
}
