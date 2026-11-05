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
import { EvalResultSchema, EvalRunSchema } from "../gen/blitz/v1/workspace_pb";
import { isEnded, liveAfter, type LiveRun } from "./evalFollow";

describe("following an eval run", () => {
  it("adds what each event says", () => {
    const first = create(EvalResultSchema, { case: "a", status: "pass" });
    const second = create(EvalResultSchema, { case: "b", status: "fail" });
    let l: LiveRun = { run: null, results: [], total: 2 };
    l = liveAfter(l, { case: "started", value: create(EvalRunSchema, { id: "r", status: "running", results: [first] }) });
    expect(l.run?.id).toBe("r");
    expect(l.results).toEqual([first]);
    l = liveAfter(l, { case: "result", value: second });
    expect(l.results).toEqual([first, second]);
    expect(isEnded(l.run?.status)).toBe(false);
    l = liveAfter(l, { case: "finished", value: create(EvalRunSchema, { id: "r", status: "failed", results: [first, second] }) });
    expect(isEnded(l.run?.status)).toBe(true);
    expect(l.total).toBe(2);
  });

  it.each([
    ["running", false],
    ["passed", true],
    ["failed", true],
    ["stopped", true],
    [undefined, false],
  ])("%s is an end: %s", (status, want) => {
    expect(isEnded(status)).toBe(want);
  });
});
