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
import { reloadSummary } from "./reload";

describe("reloadSummary", () => {
  it.each([
    [2, 1, 4, "Reloaded 2 agents, 1 skill and 4 instruction files and rules."],
    [1, 0, 1, "Reloaded 1 agent, 0 skills and 1 instruction file or rule."],
  ])("%d agents, %d skills, %d files", (agents, skills, files, want) => {
    expect(reloadSummary({ agents, skills, files: Array(files).fill("AGENTS.md") })).toBe(want);
  });
});
