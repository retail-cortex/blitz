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
import { whyNot } from "./ServiceJob";

// spec_053 EN-42: a job the service hasn't got says why.
describe("why the service hasn't got a job", () => {
  const r = (reason: string, detail = "", syncState = "") => whyNot({ reason, detail, syncState });
  it("names where the service looks", () => {
    expect(r("outside", ".agents/workers/digest/WORKER.md")).toContain(".agents/workers/digest/WORKER.md");
  });
  it("says what it's waiting on", () => {
    expect(r("not_pushed")).toContain("main");
    expect(r("saving")).toContain("Sync");
    expect(r("saving", "", "offline")).toContain("can't reach");
    expect(r("saving", "", "signed_out")).toContain("Sign in");
    expect(r("saving", "", "conflicts")).toContain("both versions");
    expect(r("saving", "", "saved")).toBe(r("saving"));
    expect(r("saving", "remote unpack failed", "refused")).toContain("remote unpack failed");
  });
  it("says why the service doesn't run it", () => {
    expect(r("not_service", "", "")).toContain("doesn't run it");
    expect(r("not_service", "its WORKER.md doesn't read")).toContain("doesn't read");
  });
  it("says the service hasn't read it, with why when it can't", () => {
    expect(r("not_read")).not.toContain(":");
    expect(r("not_read", "the service is down")).toContain("the service is down");
    expect(r("")).toBe(r("not_read"));
  });
});
