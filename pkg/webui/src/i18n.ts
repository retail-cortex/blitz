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

/*
 * The controls' words come from the app that uses them: it hands its
 * translator to setTranslator at start (Blitz's desktop page its i18n's t;
 * the enterprise console its own, over the same catalogs). Until then a
 * key is shown as itself.
 */

type Translator = (key: string, params?: Record<string, string | number>) => string;

let translate: Translator = (key) => key;

/** setTranslator makes translate the controls' words. */
export function setTranslator(translator: Translator) {
  translate = translator;
}

/** t is a key's words, in the app's language. */
export function t(key: string, params?: Record<string, string | number>): string {
  return translate(key, params);
}
