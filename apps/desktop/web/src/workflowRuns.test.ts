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

import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { WorkflowRunSchema } from "./gen/blitz/v1/workspace_pb";
import { cost, ended, failedStep, goingRun, isGoing, latestSteps, ms, progress, seeRun, tokens, took, unseenFailures } from "./workflowRuns";

const run = (id: string, status: string, extra: MessageInitShape<typeof WorkflowRunSchema> = {}) => create(WorkflowRunSchema, { id, status, workspace: "/w", workflow: "brief", ...extra });

describe("workflowRuns", () => {
  it.each([
    ["running", true],
    ["waiting", true],
    ["done", false],
    ["failed", false],
    ["stopped", false],
  ])("takes a %s run for going: %s", (status, want) => expect(isGoing({ status })).toBe(want));

  it.each([
    { name: "one that was running", prev: [["a", "running"]], next: [run("a", "failed")], want: ["a"] },
    { name: "one that was waiting", prev: [["a", "waiting"]], next: [run("a", "done")], want: ["a"] },
    { name: "one ended before", prev: [["a", "done"]], next: [run("a", "done")], want: [] },
    { name: "one still going", prev: [["a", "running"]], next: [run("a", "running")], want: [] },
    { name: "a new one that ended since", prev: [], next: [run("b", "failed", { ended: timestampFromMs(2000) })], want: ["b"] },
    { name: "a new one that ended before", prev: [], next: [run("b", "failed", { ended: timestampFromMs(500) })], want: [] },
  ])("finds the runs that ended: $name", ({ prev, next, want }) => {
    const before = prev.map(([id, status]) => ({ id, status }));
    expect(ended(before, next, 1000).map((r) => r.id)).toEqual(want);
  });

  it("reads a receipt's steps: each one's latest run, how far it's got, where it failed", () => {
    const r = run("a", "failed", {
      steps: [
        { id: "a", status: "done" },
        { id: "b", status: "failed", error: "first" },
        { id: "b", status: "done" },
        { id: "c", status: "skipped" },
        { id: "d", status: "failed", error: "last" },
      ],
    });
    const latest = latestSteps(r.steps);
    expect(Object.keys(latest)).toEqual(["a", "b", "c", "d"]);
    expect(latest.b.status).toBe("done");
    expect(progress(r)).toEqual({ done: 3, started: 4 });
    expect(failedStep(r)?.error).toBe("last");
    expect(failedStep(run("x", "done"))).toBeUndefined();
  });

  it("finds a workflow's run going in a workspace", () => {
    const runs = [run("1", "done"), run("2", "running", { workspace: "/v" }), run("3", "waiting"), run("4", "running")];
    expect(goingRun(runs, "/w", "brief")?.id).toBe("3");
    expect(goingRun(runs, "/v", "brief")?.id).toBe("2");
    expect(goingRun(runs, "/w", "other")).toBeUndefined();
  });

  it("drops a failure from the status bar once it's seen", () => {
    const runs = [run("f1", "failed"), run("f2", "failed"), run("ok", "done")];
    expect(unseenFailures(runs).map((r) => r.id)).toEqual(["f1", "f2"]);
    seeRun("f1");
    seeRun("f1");
    expect(unseenFailures(runs).map((r) => r.id)).toEqual(["f2"]);
  });

  it.each([
    { start: 0, end: 5000, want: "" },
    { start: 1000, end: 1400, want: "0.4s" },
    { start: 1000, end: 13_400, want: "12s" },
    { start: 0.5, end: 200_500, want: "3m 20s" },
    { start: 1, end: 3_900_001, want: "1h 5m" },
  ])("says $start to $end took $want", ({ start, end, want }) => expect(took(start, end)).toBe(want));

  it.each([
    { usd: 0, priced: true, want: "$0" },
    { usd: 0, priced: false, want: "—" },
    { usd: 0.00421, priced: true, want: "$0.0042" },
    { usd: 0.0164, priced: true, want: "$0.016" },
    { usd: 2.5, priced: true, want: "$2.50" },
    { usd: 0.02, priced: false, want: "$0.020+" },
  ])("prices $usd (priced $priced) as $want", ({ usd, priced, want }) => expect(cost(usd, priced)).toBe(want));

  it.each([
    { n: 950n, want: "950" },
    { n: 12_345n, want: "12.3k" },
    { n: 1_250_000, want: "1.3M" },
    { n: undefined, want: "0" },
  ])("counts $n tokens as $want", ({ n, want }) => expect(tokens(n)).toBe(want));

  it("reads a timestamp's milliseconds", () => {
    expect(ms(undefined)).toBe(0);
    expect(ms(timestampFromMs(1234))).toBe(1234);
  });
});
