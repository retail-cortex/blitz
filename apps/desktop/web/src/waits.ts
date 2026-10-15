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

// A turn's requests waiting for an answer. The tool calls of one model
// response run in parallel and each waits for its own answer, so several
// can wait at once: they're kept in order and shown one at a time. Kept in
// one slot, each request replaced the last, and the calls whose requests
// were lost waited until the turn was stopped.

import type { ApprovalRequest, Question } from "./gen/blitz/v1/turn_pb";

/** An approval request or a question, waiting. */
export type Pending = { kind: "approval"; req: ApprovalRequest } | { kind: "question"; q: Question };

/** The ID the service knows a waiting request by. */
export const pendingId = (p: Pending) => (p.kind === "approval" ? p.req.requestId : p.q.requestId);

/** waits with p added last (once: a request seen again isn't added twice). */
export const addWait = (waits: Pending[], p: Pending): Pending[] => (waits.some((w) => pendingId(w) === pendingId(p)) ? waits : [...waits, p]);

/** waits without the request answered, by its ID. */
export const dropWait = (waits: Pending[], id: string): Pending[] => waits.filter((w) => pendingId(w) !== id);
