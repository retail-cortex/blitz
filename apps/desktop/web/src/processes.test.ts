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
import { WorkspaceProcessSchema } from "./gen/blitz/v1/workspace_pb";
import { countLabel, cpu, duration, flaggedOf, flagLabel, inScope, isFlagged, lastLines, memory, processKey, runningFor, startsAll } from "./processes";

const proc = (workspace: string, id: number, flags: string[] = []) => create(WorkspaceProcessSchema, { workspace, process: { id, command: `npm run dev:${id}`, running: true, flags } });

describe("the processes shown", () => {
  const list = [proc("/w/shop", 1, ["cpu"]), proc("/w/shop", 2), proc("/w/notes", 1, ["left", "memory"])];
  it.each([
    ["this workspace's", "/w/shop", false, ["/w/shop#1", "/w/shop#2"]],
    ["another workspace's", "/w/notes", false, ["/w/notes#1"]],
    ["a workspace with none", "/w/empty", false, []],
    ["every workspace's", "/w/shop", true, ["/w/shop#1", "/w/shop#2", "/w/notes#1"]],
  ])("%s", (_, dir, all, want) => {
    expect(inScope(list, dir, all).map(processKey)).toEqual(want);
  });

  it("stop flagged takes those with any flag", () => {
    expect(flaggedOf(list).map(processKey)).toEqual(["/w/shop#1", "/w/notes#1"]);
    expect(flaggedOf(inScope(list, "/w/shop", false)).map(processKey)).toEqual(["/w/shop#1"]);
  });

  it.each([
    ["flagged", proc("/w", 1, ["cpu"]), true],
    ["fine", proc("/w", 2), false],
    ["no process", create(WorkspaceProcessSchema, { workspace: "/w" }), false],
  ])("%s: isFlagged", (_, p, want) => {
    expect(isFlagged(p)).toBe(want);
  });

  it.each([
    ["this workspace has some", [proc("/w/shop", 1)], "/w/shop", false],
    ["only another has some", [proc("/w/notes", 1)], "/w/shop", true],
    ["none anywhere", [], "/w/shop", false],
  ])("starts with every workspace's: %s", (_, l, dir, want) => {
    expect(startsAll(l, dir)).toBe(want);
  });
});

describe("running time", () => {
  it.each([
    ["at receipt", 5000n, 1000, 1000, 5000],
    ["counted on since", 5000n, 1000, 4000, 8000],
    ["a clock behind the receipt counts nothing", 5000n, 1000, 500, 5000],
  ])("%s", (_, runtime, at, now, want) => {
    expect(runningFor(runtime, at, now)).toBe(want);
  });

  it.each([
    [0, "0s"],
    [12_400, "12s"],
    [200_000, "3m 20s"],
    [3_900_000, "1h 5m"],
    [90_000_000, "1d 1h"],
    [-5, "0s"],
  ])("%d ms is %s", (ms, want) => {
    expect(duration(ms)).toBe(want);
  });
});

describe("usage", () => {
  it.each([
    [512n, "512 B"],
    [2048n, "2 KB"],
    [12_900_000n, "12.3 MB"],
    [1_610_612_736n, "1.5 GB"],
  ])("%d bytes is %s", (b, want) => {
    expect(memory(b)).toBe(want);
  });

  it.each([
    [0, "0%"],
    [0.42, "0.4%"],
    [3.6, "4%"],
    [180, "180%"],
  ])("%d%% of a core is %s", (p, want) => {
    expect(cpu(p)).toBe(want);
  });
});

describe("flag badges", () => {
  it.each([
    ["cpu", 0n, "Busy"],
    ["memory", 0n, "Memory"],
    ["left", 0n, "Left running"],
    ["left", 7_200_000n, "Left running · idle 2h 0m"],
    ["new", 0n, "new"],
  ])("%s (idle %d ms)", (flag, idle, want) => {
    expect(flagLabel(flag, idle)).toBe(want);
  });

  it("counts processes in the status bar", () => {
    expect(countLabel(1)).toBe("1 process");
    expect(countLabel(2)).toBe("2 processes");
  });
});

describe("lastLines", () => {
  it.each([
    ["short output whole", "a\nb\n", 3, "a\nb"],
    ["long output's end", "1\n2\n3\n4\n5", 2, "4\n5"],
    ["nothing", "", 3, ""],
  ])("%s", (_, text, n, want) => {
    expect(lastLines(text, n)).toBe(want);
  });
});
