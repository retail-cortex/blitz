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
import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";
import { isUnavailable, reason } from "./errors";
import { ErrorInfoSchema } from "./gen/blitz/v1/turn_pb";

const withReason = (code: Code, why: string) => new ConnectError("x", code, undefined, [{ desc: ErrorInfoSchema, value: create(ErrorInfoSchema, { reason: why }) }]);

describe("isUnavailable", () => {
  it.each([
    ["unreachable", new ConnectError("fetch failed", Code.Unavailable), true],
    ["stopping", withReason(Code.Unavailable, "SHUTTING_DOWN"), true],
    // An older service's language server failure: the service is fine.
    ["a language server's failure", withReason(Code.Unavailable, "SERVER_FAILED"), false],
    ["another code", withReason(Code.Unknown, "SERVER_FAILED"), false],
    ["not a Connect error", new Error("x"), false],
  ])("%s", (_, e, want) => {
    expect(isUnavailable(e)).toBe(want);
  });
  it("reads the reason", () => {
    expect(reason(withReason(Code.Unknown, "SERVER_FAILED"))).toBe("SERVER_FAILED");
  });
});
