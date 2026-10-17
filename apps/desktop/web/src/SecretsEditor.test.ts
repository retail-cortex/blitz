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
import { globalURN, secretPlaceholder, validSecretKey } from "./SecretsEditor";

describe("secrets", () => {
  it.each([
    { key: "github_token", ok: true },
    { key: "a.b-c", ok: true },
    { key: "", ok: false },
    { key: "has space", ok: false },
    { key: "a".repeat(65), ok: false },
  ])("$key is a name: $ok", ({ key, ok }) => expect(validSecretKey(key)).toBe(ok));

  it("names a secret with its scope", () => {
    expect(secretPlaceholder("db")).toBe("{secrets:global:db}");
    expect(secretPlaceholder("db", "/w/shop")).toBe("{secrets:workspace:db}");
    expect(globalURN("db")).toBe("urn:blitz:global:secrets:db");
  });
});
