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
import { EvalFileSchema, EvalResultSchema } from "../gen/blitz/v1/workspace_pb";
import { agentsToRefine, evalNameFor, evalsForAgent, evalsForSkill, refineRequest } from "./evalCoverage";

describe("eval coverage", () => {
  const evals = [create(EvalFileSchema, { name: "a", agents: ["support"], skills: ["tone"] }), create(EvalFileSchema, { name: "b", agents: ["qa", "support"] }), create(EvalFileSchema, { name: "c" })];

  it("finds the evals of an agent or a skill", () => {
    expect(evalsForAgent(evals, "support").map((e) => e.name)).toEqual(["a", "b"]);
    expect(evalsForAgent(evals, "nobody")).toEqual([]);
    expect(evalsForSkill(evals, "tone").map((e) => e.name)).toEqual(["a"]);
  });

  it("names an agent's first eval", () => {
    expect(evalNameFor("support", ["qa"])).toBe("support");
    expect(evalNameFor("support", ["support"])).toBe("support-evals");
    expect(evalNameFor("support", ["support", "support-evals"])).toBe("support-evals-2");
  });

  it("asks to refine from the cases missed", () => {
    const r = (agent: string, status: string, extra = {}) => create(EvalResultSchema, { agent, status, prompt: "How long?", expected: "30 days", output: "Whenever", ...extra });
    const results = [r("support", "pass"), r("support", "warn", { reason: "No number." }), r("qa", "error"), r("qa", "fail", { output: "" })];
    expect(agentsToRefine(results)).toEqual(["support", "qa"]);
    const ask = refineRequest(results, "support");
    expect(ask).toContain("- Prompt: How long?\n  Expected: 30 days\n  It answered: Whenever\n  Why: No number.");
    expect(ask.match(/- Prompt/g)).toHaveLength(1);
    expect(refineRequest(results, "qa")).toContain("It answered: (nothing)");
    expect(refineRequest([r("x", "error")], "x")).toBe("");
  });
});
