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


import { describe, expect, it } from "vitest";
import type { ApprovalRequest, Question } from "./gen/blitz/v1/turn_pb";
import { addWait, dropWait, type Pending } from "./waits";

const approval = (id: string): Pending => ({ kind: "approval", req: { requestId: id } as ApprovalRequest });
const question = (id: string): Pending => ({ kind: "question", q: { requestId: id } as Question });
const ids = (w: Pending[]) => w.map((p) => (p.kind === "approval" ? p.req.requestId : p.q.requestId));

describe("waits", () => {
  // The tool calls of one response ask together: every request waits, in
  // order, until it's answered (one slot kept only the last, and the turn
  // never ended).
  it("keeps every request that arrives together, oldest first", () => {
    let w: Pending[] = [];
    for (const id of ["a1", "a2", "a3"]) w = addWait(w, approval(id));
    expect(ids(w)).toEqual(["a1", "a2", "a3"]);
  });
  it("drops only the request answered, and the next one shows", () => {
    const w = [approval("a1"), question("q2"), approval("a3")];
    expect(ids(dropWait(w, "a1"))).toEqual(["q2", "a3"]);
    expect(ids(dropWait(w, "q2"))).toEqual(["a1", "a3"]);
    expect(ids(dropWait(w, "zz"))).toEqual(["a1", "q2", "a3"]);
  });
  it("doesn't add a request it already has", () => {
    const w = addWait([approval("a1")], approval("a1"));
    expect(ids(w)).toEqual(["a1"]);
  });
});
