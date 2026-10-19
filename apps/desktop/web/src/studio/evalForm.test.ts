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
import { EvalFileSchema, EvalResultSchema, EvalRunSchema } from "../gen/blitz/v1/workspace_pb";
import { duplicateIds, money, resultsCost, seconds, emptyEvalForm, formFromEval, moveCase, newCase, nextCaseId, resultsByCase, runCounts, sameEvalForm, specFromForm } from "./evalForm";

describe("the eval form", () => {
  const e = create(EvalFileSchema, {
    name: "shop",
    description: "Shop answers",
    agents: ["blitz"],
    near: "embedding",
    allow: ["mcp"],
    cases: [
      { id: "c1", prompt: "2+2?", output: "4", type: "exact", scope: "fail" },
      { id: "c2", name: "Greets", prompt: "Hi", output: "Hello", type: "near", threshold: 0.7, scope: "warn" },
    ],
  });

  it("reads an eval and writes it back", () => {
    const f = formFromEval(e);
    expect(f).toMatchObject({ near: "embedding", allowNetwork: false, allowMcp: true });
    expect(f.cases[1]).toMatchObject({ id: "c2", type: "near", threshold: "0.7", scope: "warn" });
    expect(f.cases[0].key).not.toBe(f.cases[1].key);
    expect(specFromForm({ ...f, allowNetwork: true, name: " shop " })).toEqual({
      name: "shop",
      description: "Shop answers",
      agents: ["blitz"],
      skills: [],
      near: "embedding",
      allow: ["network", "mcp"],
      cases: [
        { id: "c1", name: "", prompt: "2+2?", output: "4", type: "exact", threshold: 0, scope: "fail" },
        { id: "c2", name: "Greets", prompt: "Hi", output: "Hello", type: "near", threshold: 0.7, scope: "warn" },
      ],
    });
    expect(sameEvalForm(f, formFromEval(e))).toBe(true);
    expect(sameEvalForm(f, { ...f, cases: moveCase(f.cases, 0, 1) })).toBe(false);
  });

  it("adds, numbers and moves cases", () => {
    const one = newCase([]);
    expect(one).toMatchObject({ id: "c1", type: "near", threshold: "0.8", scope: "fail" });
    const cases = [one, { ...newCase([one]), id: "c3" }];
    expect(nextCaseId(cases)).toBe("c4");
    expect(nextCaseId([{ ...one, id: "c2" }])).toBe("c3");
    expect(moveCase(cases, 0, 1).map((c) => c.id)).toEqual(["c3", "c1"]);
    expect(moveCase(cases, 0, -1)).toBe(cases);
    expect([...duplicateIds([one, { ...one, key: 9 }, { ...one, id: "x" }])]).toEqual(["c1"]);
    expect(emptyEvalForm.cases).toEqual([]);
  });

  it("sums up runs", () => {
    const r = (c: string, status: string) => create(EvalResultSchema, { case: c, status, agent: "blitz" });
    const live = create(EvalRunSchema, { results: [r("c1", "pass"), r("c2", "fail"), r("c1", "pass")] });
    expect(runCounts(live)).toEqual({ pass: 2, fail: 1 });
    expect(runCounts(create(EvalRunSchema, { counts: { warn: 2 } }))).toEqual({ warn: 2 });
    expect(resultsByCase(live.results).get("c1")).toHaveLength(2);
    expect(money(0.0012)).toBe("$0.0012");
    expect(money(0.0142)).toBe("$0.0142");
    expect(money(1.5)).toBe("$1.50");
    expect(resultsCost([{ costUsd: 0.25 }, { costUsd: 0.5 }])).toBe(0.75);
    expect(seconds({ seconds: 2n, nanos: 500_000_000 })).toBe("2.5 s");
    expect(seconds({ seconds: 125n, nanos: 0 })).toBe("2 min 5 s");
    expect(seconds(undefined)).toBe("");
  });
});
