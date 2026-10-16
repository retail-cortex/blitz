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
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it, vi } from "vitest";
import { RunStatus, WorkerRunSchema } from "./gen/blitz/v1/worker_pb";
import { runningEntries } from "./runningWorkers";

const run = (id: string, workspace: string, worker: string) =>
  create(WorkerRunSchema, { id, workspace, worker, status: RunStatus.RUNNING, started: timestampFromDate(new Date(2026, 9, 16, 14, 5)) });

describe("runningEntries", () => {
  const actions = () => ({ dir: "/w/here", workspaceName: (d: string) => d.split("/").pop()!, onShow: vi.fn(), onStop: vi.fn() });

  it("is nothing when no run is going", () => {
    expect(runningEntries([], actions())).toEqual([]);
  });

  it("heads the runs, naming another workspace's, each with Stop", () => {
    const a = actions();
    const runs = [run("1", "/w/here", "nightly"), run("2", "/w/notes", "news")];
    const entries = runningEntries(runs, a);
    expect(entries[0]).toEqual({ heading: "Running now" });
    const [, here, there] = entries as { label: string; detail: string; onSelect: () => void; actions: { label: string; onSelect: () => void }[] }[];
    expect(here.label).toBe("nightly");
    expect(here.detail).toMatch(/^since /);
    expect(there.detail).toMatch(/^notes · since /);
    expect(there.actions[0].label).toBe("Stop news");

    there.actions[0].onSelect();
    expect(a.onStop).toHaveBeenCalledWith(runs[1]);
    here.onSelect();
    expect(a.onShow).toHaveBeenCalledWith(runs[0]);
  });
});
