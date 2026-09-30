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

import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { RunStatus, type Worker, type WorkerRun } from "./gen/blitz/v1/worker_pb";
import { unseenFailures } from "./workerFailures";

const worker = (status: RunStatus | undefined, at: number) =>
  ({ lastRun: status === undefined ? undefined : ({ status, started: timestampFromDate(new Date(at)) } as WorkerRun) }) as Pick<Worker, "lastRun">;

describe("unseenFailures", () => {
  it.each([
    ["failed since", [worker(RunStatus.FAILED, 2000)], 1000, 1],
    ["limited since", [worker(RunStatus.LIMITED, 2000)], 1000, 1],
    ["failed before", [worker(RunStatus.FAILED, 500)], 1000, 0],
    ["succeeded", [worker(RunStatus.SUCCEEDED, 2000)], 1000, 0],
    ["never ran", [worker(undefined, 0)], 0, 0],
    ["several", [worker(RunStatus.FAILED, 2000), worker(RunStatus.LIMITED, 3000), worker(RunStatus.SUCCEEDED, 4000)], 0, 2],
  ])("%s", (_, list, seen, want) => expect(unseenFailures(list, seen)).toBe(want));
});
