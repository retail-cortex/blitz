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
import { listEvery, listingDelay } from "./links";

describe("listing a workspace's files for links", () => {
  it.each([
    { name: "the first time: at once", last: { dir: "", at: 0 }, dir: "/w", now: 5_000, want: 0 },
    { name: "another workspace: at once", last: { dir: "/v", at: 4_000 }, dir: "/w", now: 5_000, want: 0 },
    { name: "soon after the last: waits, not dropped", last: { dir: "/w", at: 4_000 }, dir: "/w", now: 5_000, want: listEvery - 1_000 },
    { name: "long after the last: at once", last: { dir: "/w", at: 4_000 }, dir: "/w", now: 4_000 + listEvery, want: 0 },
  ])("$name", ({ last, dir, now, want }) => expect(listingDelay(last, dir, now)).toBe(want));
});
