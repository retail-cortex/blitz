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
import { count, limitText, rateText, usedShare } from "./OrgUsage";

// spec_053 EN-24: the person's limits as they read them.
describe("organization usage", () => {
  it("says counts as people read them", () => {
    expect(count(512)).toBe("512");
    expect(count(340_000)).toBe("340K");
    expect(count(1_200_000n)).toBe("1.2M");
  });

  it("says a budget's use and what's left", () => {
    const l = create(OrgLimitSchema, { scopeKind: "person", period: "day", unit: "usd", amountUsd: 5, used: create(OrgUsedSchema, { costUsd: 4.75 }) });
    expect(limitText(l)).toBe("Your daily budget on every model: $4.75 of $5.00 used, $0.25 left");
    expect(usedShare(l)).toBeCloseTo(0.95);
  });

  it("says a token quota's use", () => {
    const l = create(OrgLimitSchema, { scopeKind: "group", period: "month", unit: "tokens", model: "claude", tokens: 1_000_000n, used: create(OrgUsedSchema, { inputTokens: 300_000n, outputTokens: 200_000n }) });
    expect(limitText(l)).toBe("Your group's monthly token quota on claude: 500K of 1.0M used");
    expect(usedShare(l)).toBeCloseTo(0.5);
    expect(usedShare(create(OrgLimitSchema, { unit: "tokens" }))).toBe(0);
    expect(usedShare(create(OrgLimitSchema, { unit: "usd" }))).toBe(0);
  });

  it("says a model's rate limits", () => {
    expect(rateText(create(OrgRateLimitSchema, { model: "claude", personRpm: 10, tpm: 50_000 }))).toBe("claude: 10 requests a minute for you, 50K tokens a minute for everyone");
  });
});
