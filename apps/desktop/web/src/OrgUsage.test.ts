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
import { create } from "@bufbuild/protobuf";
import { OrgLimitSchema, OrgRateLimitSchema, OrgUsedSchema } from "./gen/blitz/v1/config_pb";
import { count, limitCaps, rateText } from "./OrgUsage";

// spec_053 EN-24: the person's limits as they read them.
describe("organization usage", () => {
  it("says counts as people read them", () => {
    expect(count(512)).toBe("512");
    expect(count(340_000)).toBe("340K");
    expect(count(1_200_000n)).toBe("1.2M");
  });

  it("says a budget's use and what's left", () => {
    const l = create(OrgLimitSchema, { scopeKind: "person", period: "day", unit: "usd", amountUsd: 5, used: create(OrgUsedSchema, { costUsd: 4.75 }) });
    const [c] = limitCaps(l);
    expect(c.text).toBe("Your daily budget on every model: $4.75 of $5.00 used, $0.25 left");
    expect(c.share).toBeCloseTo(0.95);
    expect(limitCaps(create(OrgLimitSchema, { unit: "usd" }))[0].share).toBe(0);
  });

  it("says a token quota's use and what's left", () => {
    const l = create(OrgLimitSchema, { scopeKind: "group", period: "month", unit: "tokens", model: "claude", tokens: 1_000_000n, used: create(OrgUsedSchema, { inputTokens: 300_000n, outputTokens: 200_000n }) });
    expect(limitCaps(l)).toEqual([{ text: "Your group's monthly token quota on claude: 500K of 1.0M used, 500K left", share: 0.5 }]);
    expect(limitCaps(create(OrgLimitSchema, { unit: "tokens" }))).toEqual([]);
  });

  // N26 desktop-1#14: input and output caps were taken as one, against
  // input + output, and only the input one was shown.
  it("counts input and output caps apart, as the gateway does", () => {
    const l = create(OrgLimitSchema, { scopeKind: "person", period: "month", unit: "tokens", inputTokens: 1_000_000n, outputTokens: 800_000n, used: create(OrgUsedSchema, { inputTokens: 600_000n, outputTokens: 600_000n }) });
    expect(limitCaps(l)).toEqual([
      { text: "Your monthly input token quota on every model: 600K of 1.0M used, 400K left", share: 0.6 },
      { text: "Your monthly output token quota on every model: 600K of 800K used, 200K left", share: 0.75 },
    ]);
  });

  it("says a model's rate limits", () => {
    expect(rateText(create(OrgRateLimitSchema, { model: "claude", personRpm: 10, tpm: 50_000 }))).toBe("claude: 10 requests a minute for you, 50K tokens a minute for everyone");
  });
});
