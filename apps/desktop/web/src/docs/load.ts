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

// The docs the app carries, loaded the first time something asks (the
// viewer, the palette, /docs): their own chunk, never the page's.
import { loadDocs } from "./index";

/** The docs, their pages and navigation, and the pictures they show. */
export type AppDocs = ReturnType<typeof loadDocs> & { pictures: Record<string, string> };

let loaded: Promise<AppDocs> | null = null;

/** The docs the app carries, loaded once. */
export function loadAppDocs(): Promise<AppDocs> {
  loaded ??= import("./content").then((m) => ({ ...loadDocs(m.docFiles), pictures: m.docPictures }));
  return loaded;
}
