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

import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { OwnItemSchema, SubmissionSchema } from "../gen/blitz/v1/config_pb";
import { shareable, verdictLine } from "./OwnShares";

describe("yours, with your organization (EN-93, EN-95)", () => {
  it("offers to share the kinds the organization takes, and none that can't sync", () => {
    const own = [
      create(OwnItemSchema, { kind: "skill", name: "tone" }),
      create(OwnItemSchema, { kind: "agent", name: "reviewer" }),
      create(OwnItemSchema, { kind: "skill", name: "Big Notes", skipped: "its name" }),
    ];
    expect(shareable(own, ["skill"]).map((o) => o.name)).toEqual(["tone"]);
    expect(shareable(own, [])).toEqual([]);
  });
  it("says a submission's state, version and why", () => {
    expect(verdictLine(create(SubmissionSchema, { state: "rejected", reason: "security: it calls curl" }))).toBe("Rejected · security: it calls curl");
    expect(verdictLine(create(SubmissionSchema, { state: "accepted", publishedVersion: "1.0.0" }))).toBe("Accepted · 1.0.0");
    expect(verdictLine(create(SubmissionSchema, { state: "judging" }))).toBe("Being judged");
  });
});
