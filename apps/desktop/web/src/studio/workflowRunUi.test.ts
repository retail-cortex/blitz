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
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { UsageSchema } from "../gen/blitz/v1/turn_pb";
import { WorkflowRunSchema } from "../gen/blitz/v1/workspace_pb";
import { receiptText, statusIcon } from "./RunPanel";
import { scheduleName } from "./WorkflowSchedule";

// A run's time (any but 0, which is "unset").
const T = 1_760_000_000_000;

describe("a run's receipt", () => {
  it("is text to keep: the envelope, then a line a step with its time, model, tokens and cost", () => {
    const r = create(WorkflowRunSchema, {
      id: "flow-1",
      workflow: "brief",
      status: "failed",
      origin: "worker",
      input: "the notes",
      started: timestampFromMs(T),
      ended: timestampFromMs(T + 12_000),
      usage: create(UsageSchema, { calls: 2, input: 9400n, output: 1210n, costUsd: 0.0164, priced: true }),
      error: "stopped at Save",
      steps: [
        { id: "notes", title: "Read the notes", status: "done", started: timestampFromMs(T), ended: timestampFromMs(T + 700) },
        { id: "draft", title: "Draft", status: "failed", started: timestampFromMs(T + 700), ended: timestampFromMs(T + 3500), models: ["m1"], usage: { calls: 2, input: 9400n, output: 1210n, costUsd: 0.0164, priced: true }, error: "not approved" },
      ],
    });
    const lines = receiptText(r).split("\n");
    expect(lines[0]).toBe("/brief · flow-1");
    expect(lines[1]).toContain("12s");
    expect(lines[1]).toContain("$0.016");
    expect(lines[2]).toContain("the notes");
    expect(lines[4]).toContain("Read the notes");
    expect(lines[4]).toContain("0.7s");
    expect(lines[5]).toContain("m1");
    expect(lines[5]).toContain("9,400 in, 1,210 out");
    expect(lines[5]).toContain("not approved");
    expect(lines.at(-1)).toBe("stopped at Save");
  });

  it.each([
    ["running", true],
    ["done", false],
    ["failed", false],
    ["waiting", false],
    ["skipped", false],
    ["stopped", false],
    ["odd", false],
  ])("marks a %s step (spinning: %s)", (status, spin) => {
    const icon = statusIcon(status);
    expect(icon.path).toBeTruthy();
    expect(!!icon.spin).toBe(spin);
  });
});

describe("a workflow's schedule", () => {
  it.each([
    { workflow: "brief", taken: [], want: "brief-schedule" },
    { workflow: "brief", taken: ["brief-schedule"], want: "brief-schedule-2" },
    { workflow: "brief", taken: ["brief-schedule", "brief-schedule-2"], want: "brief-schedule-3" },
    { workflow: "My Brief", taken: [], want: "-y--rief-schedule" },
  ])("names its worker $want", ({ workflow, taken, want }) => expect(scheduleName(workflow, taken)).toBe(want));
});
