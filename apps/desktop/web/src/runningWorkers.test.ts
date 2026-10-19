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
import { describe, expect, it } from "vitest";
import { RunStatus, WorkerRunSchema } from "./gen/blitz/v1/worker_pb";
import { runDetail } from "./runningWorkers";

const run = (id: string, workspace: string, worker: string, started = true) =>
  create(WorkerRunSchema, { id, workspace, worker, status: RunStatus.RUNNING, started: started ? timestampFromDate(new Date(2026, 9, 16, 14, 5)) : undefined });

describe("runDetail", () => {
  const name = (d: string) => d.split("/").pop()!;
  it.each([
    ["this workspace's: since when", run("1", "/w/here", "nightly"), "/w/here", /^since /],
    ["another workspace's: its name first", run("2", "/w/notes", "news"), "/w/here", /^notes · since /],
    ["every workspace's (no dir): its name", run("3", "/w/here", "nightly"), "", /^here · since /],
    ["not started yet: the workspace only", run("4", "/w/notes", "news", false), "/w/here", /^notes$/],
  ])("%s", (_, r, dir, want) => {
    expect(runDetail(r, dir, name)).toMatch(want);
  });
});
