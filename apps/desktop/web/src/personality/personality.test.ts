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
import { agentWithPersonality, copyName, maxPersonalityText, personalityDescription, personalityLabel, personalityProblem, personalitySource, validPersonalityName } from "./personality";
import { geminiVoices } from "./voices";
import { personalityCheck } from "../dev/fakeChecks";

describe("personalities", () => {
  const mentor = { name: "mentor", description: "Explains the why, one idea at a time", voice: "Sadaltager", source: "built-in" };
  const coach = { name: "coach", description: "Cheers you on", voice: "Puck", source: "own" };

  it("shows a built-in's name and description in the page's language, the person's own as written", () => {
    expect(personalityLabel(mentor)).toBe("Mentor");
    expect(personalityDescription(mentor)).toBe("Explains the why, one idea at a time");
    expect(personalityLabel(coach)).toBe("coach");
    expect(personalityDescription(coach)).toBe("Cheers you on");
    expect(personalityLabel({ ...mentor, source: "own" })).toBe("mentor");
    expect(personalitySource(coach)).toBe("Yours");
    expect(personalitySource(mentor)).toBe("Built in");
    expect(personalitySource({ ...coach, source: "organization" })).toBe("Organization");
  });

  it.each([
    { d: { name: "coach", description: "", text: "Cheer." }, own: [], editing: "", want: "" },
    { d: { name: "", description: "", text: "Cheer." }, own: [], editing: "", want: "Give it a name." },
    { d: { name: "My Coach", description: "", text: "Cheer." }, own: [], editing: "", want: "lower-case" },
    { d: { name: "default", description: "", text: "Cheer." }, own: [], editing: "", want: "Blitz's own" },
    { d: { name: "coach", description: "", text: "Cheer." }, own: ["coach"], editing: "", want: "You have a personality named coach." },
    { d: { name: "coach", description: "", text: "Cheer." }, own: ["coach"], editing: "coach", want: "" },
    { d: { name: "coach", description: "a\nb", text: "Cheer." }, own: [], editing: "", want: "one line" },
    { d: { name: "coach", description: "", text: "  " }, own: [], editing: "", want: "how it should sound" },
    { d: { name: "coach", description: "", text: "é".repeat(maxPersonalityText + 1) }, own: [], editing: "", want: "At most 2000" },
    { d: { name: "coach", description: "", text: "é".repeat(maxPersonalityText) }, own: [], editing: "", want: "" },
  ])("checks $d.name with $own (editing $editing)", ({ d, own, editing, want }) => {
    const got = personalityProblem(d, own, editing);
    if (want) expect(got).toContain(want);
    else expect(got).toBe("");
  });

  it("checks what the service checks, as the fake does", () => {
    expect(validPersonalityName("calm-2")).toBe(true);
    expect(validPersonalityName("-calm")).toBe(false);
    expect(validPersonalityName("a".repeat(41))).toBe(false);
    expect(personalityCheck({ name: "coach", description: "", voice: "Puck", text: "Cheer." })).toBe("");
    expect(personalityCheck({ name: "coach", description: "", voice: "Bob", text: "Cheer." })).toContain("Gemini's voices");
    expect(personalityCheck({ name: "default", description: "", voice: "", text: "Cheer." })).toContain("Blitz's own");
    expect(geminiVoices).toContain("Kore");
  });

  it("names a copy that isn't taken", () => {
    expect(copyName("mentor", ["mentor"])).toBe("mentor-2");
    expect(copyName("mentor-2", ["mentor", "mentor-2", "mentor-3"])).toBe("mentor-4");
    expect(copyName("a".repeat(40), []).length).toBeLessThanOrEqual(40);
  });

  it("puts the personality beside the agent, but not Blitz's own", () => {
    expect(agentWithPersonality("blitz", mentor)).toBe("blitz · Mentor");
    expect(agentWithPersonality("blitz", { ...mentor, name: "default" })).toBe("blitz");
    expect(agentWithPersonality("blitz", undefined)).toBe("blitz");
  });
});
