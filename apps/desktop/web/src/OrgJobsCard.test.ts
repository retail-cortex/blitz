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

import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { orgJobDetail } from "./OrgJobsCard";

// spec_053 EN-45: one of the organization's jobs, as Settings › Jobs lists it.
describe("the organization's jobs", () => {
  const when = () => "Monday";
  it("says what it does, when it runs, and when next", () => {
    expect(orgJobDetail({ description: "The digest", schedule: "Mondays at 8:00", enabled: true, nextRun: timestampFromDate(new Date()) }, when)).toBe(
      "The digest · Mondays at 8:00 · next Monday",
    );
  });
  it("says when it runs only on request, or is off", () => {
    expect(orgJobDetail({ description: "", schedule: "", enabled: true }, when)).toBe("On request");
    expect(orgJobDetail({ description: "X", schedule: "Daily", enabled: false, nextRun: timestampFromDate(new Date()) }, when)).toBe("X · Daily · Off");
  });
});
