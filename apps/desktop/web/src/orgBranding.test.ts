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
import { accentRules, appName, contrast, logoFor } from "./orgBranding";
import type { Branding } from "./gen/blitz/v1/config_pb";

describe("the organization's branding", () => {
  it("measures contrast as WCAG does", () => {
    expect(contrast("#000000", "#ffffff")).toBeCloseTo(21, 5);
    expect(contrast("#ffffff", "#ffffff")).toBeCloseTo(1, 5);
    expect(contrast("#1a56db", "#ffffff")).toBeGreaterThan(4.5);
  });

  it.each([
    ["a dark blue on light", "#1a56db", "light", true],
    ["yellow on light: too faint", "#ffff00", "light", false],
    ["a light violet on dark", "#c4a5ff", "dark", true],
    ["navy on dark: too faint", "#0b1f4b", "dark", false],
    ["not a colour", "blue", "light", false],
    ["a short colour", "#fff", "light", false],
  ] as const)("keeps an accent only where it reads well enough: %s", (_, accent, theme, kept) => {
    const css = accentRules(accent, theme);
    expect(css !== "").toBe(kept);
    if (kept) {
      expect(css).toContain(`[data-theme="${theme}"]`);
      expect(css).toContain(`--md-primary: ${accent}`);
    }
  });

  it("puts text on the accent in whichever of white and black reads better", () => {
    expect(accentRules("#1a56db", "light")).toContain("--md-on-primary: #ffffff");
    expect(accentRules("#c4a5ff", "dark")).toContain("--md-on-primary: #000000");
  });

  it("names the app and picks the logo for the theme", () => {
    const org = { name: "Acme Assist", images: { "logo.svg": "data:light", "logo-dark.svg": "data:dark" } } as unknown as Branding;
    expect(appName(org)).toBe("Acme Assist");
    expect(appName(null)).not.toBe("");
    expect(logoFor(org, false)).toBe("data:light");
    expect(logoFor(org, true)).toBe("data:dark");
    expect(logoFor({ ...org, images: { "icon.png": "data:icon" } } as unknown as Branding, true)).toBe("data:icon");
    expect(logoFor(null, false)).toBe("");
  });
});
