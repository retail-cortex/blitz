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

import { describe, expect, it } from "vitest";
import { applyUserLanguage, language, languages, setLanguage, t } from "./i18n";

// The user's catalogs and [ui] locale, from the service (BL-DSK-41).
describe("applyUserLanguage", () => {
  it("adds catalogs, overrides translations, and makes System follow the setting", () => {
    const de = JSON.stringify({ meta: { locale: "de", name: "Deutsch", english_name: "German" }, messages: { "desktop.settings": "Einstellungen" } });
    const fr = JSON.stringify({ meta: { locale: "fr-CA" }, messages: { "desktop.settings": "Réglages perso" } });
    setLanguage("system");
    applyUserLanguage("de", [de, fr, "not json", JSON.stringify({ meta: {} })]);
    expect(languages.map((l) => l.tag)).toContain("de");
    expect(language()).toBe("de");
    expect(t("desktop.settings")).toBe("Einstellungen");
    expect(t("desktop.done")).toBe("Done"); // what de lacks is English
    setLanguage("fr-CA");
    expect(t("desktop.settings")).toBe("Réglages perso");
    setLanguage("system");
    expect(language()).toBe("de");
  });
});
