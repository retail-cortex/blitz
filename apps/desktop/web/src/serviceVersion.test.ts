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
import { staleReason } from "./serviceVersion";

describe("staleReason", () => {
  const info = { version: "1.4.0", executable: "/usr/lib/blitz-desktop/blitzd", pid: 42 };
  it("accepts the service the app expects", () => {
    expect(staleReason("1.4.0", info, true)).toBe("");
    expect(staleReason("dev", { ...info, version: "dev" }, true)).toBe("");
  });
  it("flags a service too old to describe itself", () => {
    expect(staleReason("1.4.0", undefined, true)).toBe("old");
  });
  it("flags a service whose program is gone, before its version", () => {
    expect(staleReason("1.4.0", info, false)).toBe("missing");
    expect(staleReason("1.5.0", info, false)).toBe("missing");
  });
  it("flags another version", () => {
    expect(staleReason("1.5.0", info, true)).toBe("mismatch");
    expect(staleReason("dev", info, true)).toBe("mismatch");
  });
});
