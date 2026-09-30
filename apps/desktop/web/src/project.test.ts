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
import { needsDecision, projectItemText, projectReason, projectState, waiting } from "./project";

describe("project settings in words", () => {
  it.each([
    [{ kind: "hook", key: "stop", value: "./stop.sh", file: "" }, "hook stop runs ./stop.sh"],
    [{ kind: "mcp", key: "db", value: "npx db-mcp", file: "" }, "MCP server db: npx db-mcp"],
    [{ kind: "allow", key: "", value: "shell(make test)", file: "" }, "allow rule shell(make test)"],
    [{ kind: "skill_scripts", key: "tidy", value: "1", file: "skills/tidy" }, "skill tidy runs its scripts (skills/tidy)"],
    [{ kind: "worker_limit", key: "workers.policy.max_turns", value: "10", file: "" }, "workers.policy.max_turns = 10"],
    [{ kind: "setting", key: "ui.theme", value: "", file: "" }, "ui.theme"],
  ])("%j", (it_, want) => {
    expect(projectItemText(it_)).toBe(want);
  });

  it("says why a setting was ignored", () => {
    expect(projectReason("never")).toBe("never from a project");
  });

  it.each([
    [{ state: "new", loaded: false }, "Not reviewed yet: not loaded.", true],
    [{ state: "changed", loaded: false }, "Changed since they were reviewed: not loaded.", true],
    [{ state: "trusted", loaded: true }, "Trusted: in force.", false],
    [{ state: "trusted", loaded: false }, "Trusted: in force when the workspace opens again.", false],
    [{ state: "declined", loaded: false }, "Declined: not loaded.", false],
    [{ state: "new", loaded: true }, "Trusted for this run only.", false],
  ])("%j", (p, text, decide) => {
    expect(projectState(p)).toBe(text);
    expect(needsDecision(p)).toBe(decide);
  });

  it("counts what waits for trust", () => {
    const pending = [{ kind: "hook" }, { kind: "mcp" }] as never[];
    expect(waiting({ loaded: false, pending })).toBe(2);
    expect(waiting({ loaded: true, pending })).toBe(0);
    expect(waiting(undefined)).toBe(0);
  });
});
