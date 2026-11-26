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
import { isPlain, localeName, pitchLabel, plainTuning, speakingRate, speedLabel, speedOf, tuningProblem, tuningSummary, voiceLocales } from "./tuning";

describe("a voice's tuning (PER-41)", () => {
  it.each([
    ["its own", plainTuning, ""],
    ["unset speed is its own", { locale: "", speed: 0, pitch: 0 }, ""],
    ["slower, in a locale", { locale: "en-GB", speed: 0.9, pitch: -2 }, ""],
    ["a locale the voices don't speak", { locale: "en-ZZ", speed: 1, pitch: 0 }, "en-ZZ isn't a locale the voices speak."],
    ["too slow", { locale: "", speed: 0.2, pitch: 0 }, "Speed is from 0.5 to 2."],
    ["too fast", { locale: "", speed: 2.5, pitch: 0 }, "Speed is from 0.5 to 2."],
    ["too low", { locale: "", speed: 1, pitch: -7 }, "Pitch is from -6 to 6 semitones."],
    ["not a number", { locale: "", speed: 1, pitch: NaN }, "Pitch is from -6 to 6 semitones."],
    ["each in range, together beyond the voice", { locale: "", speed: 2, pitch: -6 }, "This speed and pitch together are beyond the voice: bring one of them back."],
  ])("%s", (_, tn, want) => {
    expect(tuningProblem(tn)).toBe(want);
  });

  it("asks Text-to-Speech for the rate that comes out at the speed once the pitch is made", () => {
    expect(speakingRate(plainTuning)).toBe(1);
    expect(speakingRate({ locale: "", speed: 0.9, pitch: 0 })).toBeCloseTo(0.9);
    // An octave's half lower is played that much slower, so asked for that much faster.
    expect(speakingRate({ locale: "", speed: 1, pitch: -6 })).toBeCloseTo(Math.SQRT2);
    expect(speakingRate({ locale: "", speed: 1.5, pitch: 6 })).toBeCloseTo(1.5 / Math.SQRT2);
  });

  it("checks a locale against the service's list when it has one", () => {
    expect(tuningProblem({ locale: "en-GB", speed: 1, pitch: 0 }, ["fr-CA"])).not.toBe("");
    expect(voiceLocales).toContain("en-GB");
    expect([...voiceLocales]).toEqual([...voiceLocales].sort());
  });

  it("shows a speed, a pitch and a locale", () => {
    expect(speedLabel(0.9)).toBe("0.90×");
    expect(speedLabel(0)).toBe("1.00×");
    expect(speedOf(0)).toBe(1);
    expect(pitchLabel(0)).toBe("0");
    expect(pitchLabel(1.5)).toBe("+1.5");
    expect(pitchLabel(-2)).toBe("−2");
    expect(localeName("en-GB", "en-US")).toMatch(/English/);
    expect(localeName("cmn-CN", "en-US")).toMatch(/Chinese/);
    expect(localeName("not a tag", "en-US")).toBe("not a tag");
  });

  it("sums a tuning up for a personality's row", () => {
    expect(isPlain(plainTuning)).toBe(true);
    expect(isPlain({ locale: "", speed: 0, pitch: 0 })).toBe(true);
    expect(isPlain({ locale: "en-GB", speed: 1, pitch: 0 })).toBe(false);
    expect(tuningSummary(plainTuning, "en-US")).toBe("");
    expect(tuningSummary({ locale: "en-GB", speed: 0.9, pitch: -2 }, "en-US")).toMatch(/^.*English.* · 0\.90× · pitch −2$/);
    expect(tuningSummary({ locale: "", speed: 1, pitch: 3 }, "en-US")).toBe("pitch +3");
  });
});
